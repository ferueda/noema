package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

const (
	maxFailureDetailBytes    = 1024
	maxSelectedFactTextBytes = 2 * 1024
	maxSelectedFactTexts     = 128
	maxAnalysisFactTextBytes = 64 * 1024
)

type SessionEvidenceSource interface {
	Read(context.Context, string) (domain.EvidenceDocument, error)
}

type FactExtractor interface {
	Name() string
	Version() string
	SchemaVersion() int
	Extract(domain.EvidenceDocument) ([]domain.FactDraft, domain.AnalysisOmissions, error)
}

type FactAnalysisStore interface {
	FindCompletedFactAnalysis(context.Context, string) (domain.FactAnalysis, bool, error)
	CommitFactAnalysis(context.Context, domain.FactAnalysis) (bool, error)
	RecordFailedAnalysis(context.Context, domain.AnalysisRun) error
	LoadFactAnalysis(context.Context, string) (domain.FactAnalysis, error)
}

type FactAnalysisResult struct {
	Analysis domain.FactAnalysis `json:"analysis"`
	Reused   bool                `json:"reused"`
}

type AnalysisError struct {
	AnalysisID string
	Category   string
}

func (failure AnalysisError) Error() string {
	return fmt.Sprintf("analysis %s failed: %s", failure.AnalysisID, failure.Category)
}

type FactAnalyzer struct {
	Source    SessionEvidenceSource
	Extractor FactExtractor
	Store     FactAnalysisStore
	NewID     IDGenerator
	Now       func() time.Time
}

func (analyzer FactAnalyzer) Run(ctx context.Context, canonicalID string) (FactAnalysisResult, error) {
	startedAt := analyzer.now()
	document, err := analyzer.Source.Read(ctx, canonicalID)
	if err != nil {
		return FactAnalysisResult{}, analyzer.recordFailure(ctx, canonicalID, nil, nil, "source-evidence-invalid", startedAt)
	}
	processingKey, err := platform.Fingerprint(struct {
		Revision  domain.EvidenceRevisionIdentity
		Selection domain.EvidenceSelection
		Extractor string
		Version   string
		Schema    int
	}{document.Revision.Identity(), document.Selection, analyzer.Extractor.Name(), analyzer.Extractor.Version(), analyzer.Extractor.SchemaVersion()})
	if err != nil {
		return FactAnalysisResult{}, analyzer.recordFailure(ctx, canonicalID, &document.Revision, &document.Selection, "processing-key-invalid", startedAt)
	}
	existing, found, err := analyzer.Store.FindCompletedFactAnalysis(ctx, processingKey)
	if err != nil {
		return FactAnalysisResult{}, errors.New("fact analysis persistence unavailable")
	}
	if found {
		return FactAnalysisResult{Analysis: existing, Reused: true}, nil
	}
	drafts, omissions, err := analyzer.Extractor.Extract(document)
	if err != nil {
		return FactAnalysisResult{}, analyzer.recordFailure(ctx, canonicalID, &document.Revision, &document.Selection, "fact-extraction-invalid", startedAt)
	}
	analysis, err := analyzer.buildAnalysis(canonicalID, processingKey, document, drafts, omissions, startedAt)
	if err != nil {
		return FactAnalysisResult{}, analyzer.recordFailure(ctx, canonicalID, &document.Revision, &document.Selection, "fact-validation-invalid", startedAt)
	}
	inserted, err := analyzer.Store.CommitFactAnalysis(ctx, analysis)
	if err != nil {
		return FactAnalysisResult{}, errors.New("fact analysis persistence unavailable")
	}
	if !inserted {
		existing, found, err = analyzer.Store.FindCompletedFactAnalysis(ctx, processingKey)
		if err != nil || !found {
			return FactAnalysisResult{}, errors.New("fact analysis persistence conflict")
		}
		return FactAnalysisResult{Analysis: existing, Reused: true}, nil
	}
	return FactAnalysisResult{Analysis: analysis}, nil
}

func (analyzer FactAnalyzer) buildAnalysis(
	canonicalID, processingKey string,
	document domain.EvidenceDocument,
	drafts []domain.FactDraft,
	omissions domain.AnalysisOmissions,
	startedAt time.Time,
) (domain.FactAnalysis, error) {
	runID, err := analyzer.newID()
	if err != nil {
		return domain.FactAnalysis{}, err
	}
	createdAt := analyzer.now()
	facts := make([]domain.Fact, 0, len(drafts))
	selectedTextCount := 0
	selectedTextBytes := 0
	for index, draft := range drafts {
		textCount, textBytes, err := validateFactDraft(draft, document)
		if err != nil {
			return domain.FactAnalysis{}, fmt.Errorf("validate fact %d: %w", index, err)
		}
		selectedTextCount += textCount
		selectedTextBytes += textBytes
		if selectedTextCount > maxSelectedFactTexts || selectedTextBytes > maxAnalysisFactTextBytes {
			return domain.FactAnalysis{}, fmt.Errorf("validate fact %d: selected text budget exceeded", index)
		}
		fingerprint, err := platform.Fingerprint(struct {
			Revision  domain.EvidenceRevisionIdentity
			Kind      string
			Value     domain.FactValue
			Outcome   string
			Rule      string
			Extractor string
			Version   string
			Schema    int
			Evidence  []domain.EvidenceRef
		}{
			document.Revision.Identity(), draft.Kind, draft.Value, draft.Outcome, draft.ParseRule,
			analyzer.Extractor.Name(), analyzer.Extractor.Version(), analyzer.Extractor.SchemaVersion(), draft.Evidence,
		})
		if err != nil {
			return domain.FactAnalysis{}, err
		}
		facts = append(facts, domain.Fact{
			ID: platform.DerivedID("fact_", fingerprint), Fingerprint: fingerprint, AnalysisRunID: runID,
			Kind: draft.Kind, SchemaVersion: analyzer.Extractor.SchemaVersion(), Value: draft.Value,
			Outcome: draft.Outcome, ExtractorName: analyzer.Extractor.Name(), ExtractorVersion: analyzer.Extractor.Version(),
			ParseRule: draft.ParseRule, Evidence: draft.Evidence, CreatedAt: createdAt,
		})
	}
	factIDs := make([]string, len(facts))
	for index := range facts {
		factIDs[index] = facts[index].ID
	}
	run := domain.AnalysisRun{
		ID: runID, ProcessingKey: processingKey, Stage: domain.AnalysisStageFacts,
		RequestedSourceIdentity: canonicalID, Revision: &document.Revision, Selection: &document.Selection,
		ExtractorName: analyzer.Extractor.Name(), ExtractorVersion: analyzer.Extractor.Version(),
		SchemaVersion: analyzer.Extractor.SchemaVersion(), FactIDs: factIDs, Omissions: omissions,
		Status: domain.AnalysisCompleted, StartedAt: startedAt, FinishedAt: analyzer.now(),
	}
	return domain.FactAnalysis{Run: run, Facts: facts}, nil
}

func (analyzer FactAnalyzer) recordFailure(
	ctx context.Context,
	canonicalID string,
	revision *domain.EvidenceRevision,
	selection *domain.EvidenceSelection,
	category string,
	startedAt time.Time,
) error {
	id, err := analyzer.newID()
	if err != nil {
		return errors.New("fact analysis persistence unavailable")
	}
	category = sanitizeFailure(category)
	run := domain.AnalysisRun{
		ID: id, Stage: domain.AnalysisStageFacts, RequestedSourceIdentity: canonicalID,
		Revision: revision, Selection: selection, ExtractorName: analyzer.Extractor.Name(),
		ExtractorVersion: analyzer.Extractor.Version(), SchemaVersion: analyzer.Extractor.SchemaVersion(),
		FactIDs: []string{}, Status: domain.AnalysisFailed, Error: category,
		StartedAt: startedAt, FinishedAt: analyzer.now(),
	}
	if err := analyzer.Store.RecordFailedAnalysis(ctx, run); err != nil {
		return errors.New("fact analysis persistence unavailable")
	}
	return AnalysisError{AnalysisID: id, Category: category}
}

func validateFactDraft(fact domain.FactDraft, document domain.EvidenceDocument) (int, int, error) {
	if fact.Kind == "" || fact.ParseRule == "" || len(fact.Evidence) == 0 {
		return 0, 0, errors.New("kind, parse rule, and evidence are required")
	}
	if !oneOfOutcome(fact.Outcome) {
		return 0, 0, errors.New("invalid outcome")
	}
	textCount, textBytes, err := validateFactValue(fact.Value)
	if err != nil {
		return 0, 0, err
	}
	for _, ref := range fact.Evidence {
		if ref.ID == "" || ref.SourceKind != domain.EvidenceSourceSessions ||
			ref.SourceIdentity != document.Revision.CanonicalID ||
			ref.DocumentDigestScheme != document.Revision.DocumentDigest.Scheme ||
			ref.DocumentDigest != document.Revision.DocumentDigest.Digest ||
			ref.EntryOrdinal < 0 || ref.EntryOrdinal >= len(document.Entries) || ref.Excerpt != "" {
			return 0, 0, errors.New("invalid evidence reference")
		}
		entry := document.Entries[ref.EntryOrdinal]
		if ref.EntryKind != entry.Kind || ref.Actor != entry.Actor ||
			!sameOptionalInt(ref.RelatedEntryOrdinal, entry.RelatedEntryOrdinal) ||
			ref.ToolCallID != entry.ToolCallID || ref.ToolName != entry.ToolName || ref.ToolNamespace != entry.ToolNamespace {
			return 0, 0, errors.New("evidence entry metadata mismatch")
		}
		if ref.SegmentOrdinal != nil {
			if *ref.SegmentOrdinal < 0 || *ref.SegmentOrdinal >= len(entry.Content) {
				return 0, 0, errors.New("invalid segment reference")
			}
			segment := entry.Content[*ref.SegmentOrdinal]
			if segment.Text == nil || ref.Origin != segment.Origin || ref.OriginConfidence != segment.OriginConfidence ||
				segment.Text.ContentHash.Scheme != ref.ContentHashScheme || segment.Text.ContentHash.Digest != ref.ContentHash {
				return 0, 0, errors.New("evidence content metadata mismatch")
			}
		}
	}
	return textCount, textBytes, nil
}

func validateFactValue(value domain.FactValue) (int, int, error) {
	texts := []*domain.SelectedText{value.Command, value.Error}
	if value.Test != nil {
		texts = append(texts, value.Test.Command)
	}
	count := 0
	bytes := 0
	for _, selected := range texts {
		if selected == nil {
			continue
		}
		if selected.EmittedUTF8Bytes != len([]byte(selected.Text)) ||
			selected.EmittedUTF8Bytes > selected.OriginalUTF8Bytes ||
			selected.EmittedUTF8Bytes > maxSelectedFactTextBytes ||
			selected.ContentHash.Scheme != "sha256-utf8-v1" || len(selected.ContentHash.Digest) != 64 {
			return 0, 0, errors.New("invalid selected fact text")
		}
		if _, err := hex.DecodeString(selected.ContentHash.Digest); err != nil {
			return 0, 0, errors.New("invalid selected fact text hash")
		}
		if selected.EmittedUTF8Bytes > 0 {
			count++
			bytes += selected.EmittedUTF8Bytes
		}
	}
	if count > 1 {
		return 0, 0, errors.New("fact contains more than one selected text value")
	}
	return count, bytes, nil
}

func sameOptionalInt(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sanitizeFailure(value string) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value)
	if len(value) > maxFailureDetailBytes {
		value = value[:maxFailureDetailBytes]
	}
	if value == "" {
		return "analysis-failed"
	}
	return value
}

func oneOfOutcome(value string) bool {
	return value == domain.FactOutcomeSuccess || value == domain.FactOutcomeFailure ||
		value == domain.FactOutcomeUnknown || value == domain.FactOutcomeNotApplicable
}

func (analyzer FactAnalyzer) now() time.Time {
	if analyzer.Now != nil {
		return analyzer.Now().UTC()
	}
	return time.Now().UTC()
}

func (analyzer FactAnalyzer) newID() (string, error) {
	if analyzer.NewID != nil {
		return analyzer.NewID()
	}
	return platform.NewID()
}
