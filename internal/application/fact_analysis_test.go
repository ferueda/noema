package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

type staticEvidenceSource struct{ document domain.EvidenceDocument }

func (source staticEvidenceSource) Read(context.Context, string) (domain.EvidenceDocument, error) {
	return source.document, nil
}

type staticFactExtractor struct{ drafts []domain.FactDraft }

func (staticFactExtractor) Name() string       { return "static" }
func (staticFactExtractor) Version() string    { return "1" }
func (staticFactExtractor) SchemaVersion() int { return 1 }
func (extractor staticFactExtractor) Extract(domain.EvidenceDocument) ([]domain.FactDraft, domain.AnalysisOmissions, error) {
	return extractor.drafts, domain.AnalysisOmissions{}, nil
}

type memoryFactStore struct {
	failed        []domain.AnalysisRun
	committed     []domain.FactAnalysis
	failRecordErr error
}

func (*memoryFactStore) FindCompletedFactAnalysis(context.Context, string) (domain.FactAnalysis, bool, error) {
	return domain.FactAnalysis{}, false, nil
}
func (store *memoryFactStore) CommitFactAnalysis(_ context.Context, analysis domain.FactAnalysis) (bool, error) {
	store.committed = append(store.committed, analysis)
	return true, nil
}
func (store *memoryFactStore) RecordFailedAnalysis(_ context.Context, run domain.AnalysisRun) error {
	if store.failRecordErr != nil {
		return store.failRecordErr
	}
	store.failed = append(store.failed, run)
	return nil
}
func (*memoryFactStore) LoadFactAnalysis(context.Context, string) (domain.FactAnalysis, error) {
	return domain.FactAnalysis{}, errors.New("not found")
}

func TestFactAnalyzerRejectsOversizedExtractorTextBeforePersistence(t *testing.T) {
	document, reference := applicationEvidence()
	value := strings.Repeat("x", maxSelectedFactTextBytes+1)
	hash := sha256.Sum256([]byte(value))
	store := &memoryFactStore{}
	analyzer := FactAnalyzer{
		Source: staticEvidenceSource{document: document},
		Extractor: staticFactExtractor{drafts: []domain.FactDraft{{
			Kind: "command", Value: domain.FactValue{Command: &domain.SelectedText{
				Text: value, OriginalUTF8Bytes: len(value), EmittedUTF8Bytes: len(value),
				ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
			}},
			Outcome: domain.FactOutcomeNotApplicable, ParseRule: "unsafe-test", Evidence: []domain.EvidenceRef{reference},
		}}},
		Store: store, NewID: func() (string, error) { return "failed-analysis", nil },
		Now: func() time.Time { return time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC) },
	}
	_, err := analyzer.Run(context.Background(), document.Revision.CanonicalID)
	var analysisError AnalysisError
	if !errors.As(err, &analysisError) || analysisError.AnalysisID != "failed-analysis" {
		t.Fatalf("analysis error = %v", err)
	}
	if len(store.committed) != 0 || len(store.failed) != 1 || store.failed[0].Error != "fact-validation-invalid" {
		t.Fatalf("store state = committed %#v, failed %#v", store.committed, store.failed)
	}
}

func TestFactAnalyzerDoesNotClaimFailureWasStoredWhenFailureWriteFails(t *testing.T) {
	document, reference := applicationEvidence()
	store := &memoryFactStore{failRecordErr: errors.New("disk unavailable")}
	analyzer := FactAnalyzer{
		Source: staticEvidenceSource{document: document},
		Extractor: staticFactExtractor{drafts: []domain.FactDraft{{
			Kind: "command", Outcome: "invalid", ParseRule: "invalid", Evidence: []domain.EvidenceRef{reference},
		}}},
		Store: store, NewID: func() (string, error) { return "not-stored", nil },
	}
	_, err := analyzer.Run(context.Background(), document.Revision.CanonicalID)
	var analysisError AnalysisError
	if err == nil || errors.As(err, &analysisError) || strings.Contains(err.Error(), "not-stored") {
		t.Fatalf("error = %v, want persistence failure without inspectable id", err)
	}
}

func applicationEvidence() (domain.EvidenceDocument, domain.EvidenceRef) {
	text := `{"cmd":"go test ./..."}`
	hash := sha256.Sum256([]byte(text))
	digest := hex.EncodeToString(hash[:])
	entry := domain.EvidenceEntry{
		Ordinal: 0, Kind: "tool-call", Actor: "model", ToolCallID: "call", ToolName: "exec_command",
		Content: []domain.EvidenceSegment{{
			Ordinal: 0, Kind: "text", Origin: "model", OriginConfidence: "high",
			Text: &domain.SelectedText{
				Text: text, OriginalUTF8Bytes: len(text), EmittedUTF8Bytes: len(text),
				ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: digest},
			},
		}},
	}
	first, last, segment := 0, 0, 0
	document := domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind: domain.EvidenceSourceSessions, CanonicalID: "synthetic@local:one",
			NativeSourceKind: "synthetic", SourceInstanceID: "local", NativeID: "one",
			DocumentDigest: domain.Digest{Scheme: "sha256-sessions-document-jcs-v1", Digest: strings.Repeat("d", 64)},
		},
		Selection: domain.EvidenceSelection{
			Mode: "full", Entries: domain.EntrySelection{Selected: 1, Total: 1, FirstOrdinal: &first, LastOrdinal: &last},
			Segments:    domain.CountSelection{Selected: 1, Total: 1},
			SegmentText: domain.ByteSelection{EmittedUTF8Bytes: len(text), OriginalUTF8Bytes: len(text)},
			Coverage:    domain.CoverageCompleteRetainedSnapshot,
		},
		Entries: []domain.EvidenceEntry{entry},
	}
	reference := domain.EvidenceRef{
		ID: "evidence", SourceKind: domain.EvidenceSourceSessions, SourceIdentity: document.Revision.CanonicalID,
		DocumentDigestScheme: document.Revision.DocumentDigest.Scheme, DocumentDigest: document.Revision.DocumentDigest.Digest,
		EntryOrdinal: 0, SegmentOrdinal: &segment, EntryKind: entry.Kind, Actor: entry.Actor,
		Origin: "model", OriginConfidence: "high", ToolCallID: entry.ToolCallID, ToolName: entry.ToolName,
		ContentHashScheme: "sha256-utf8-v1", ContentHash: digest,
	}
	return document, reference
}
