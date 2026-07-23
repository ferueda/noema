package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	noemaevidence "github.com/ferueda/noema/internal/evidence"
)

const (
	corpusSchemaVersion = 1
	corpusMaxBytes      = 1 << 20
	corpusDigestV1      = "9c5014491c4018b54c839d5313e594361b177f0396c3b892d06b36e69f8be83f"
	corpusDigestV2      = "1dbd77515436b86dde68abac07f2fa158394a9fcecf69258c3ab644ffa6ee07f"
)

var corpusIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type reviewedCorpus struct {
	CaseCount        int
	AdapterVersion   string
	SourceInstanceID string
}

var reviewedCorpora = map[string]reviewedCorpus{
	corpusDigestV1: {
		CaseCount: 12, AdapterVersion: "semantic-evaluation-corpus-v1",
		SourceInstanceID: "semantic-corpus-v1",
	},
	corpusDigestV2: {
		CaseCount: 20, AdapterVersion: "semantic-evaluation-corpus-v2",
		SourceInstanceID: "semantic-corpus-v2",
	},
}

type corpusFile struct {
	SchemaVersion int          `json:"schemaVersion"`
	Cases         []corpusCase `json:"cases"`
}

type corpusCase struct {
	ID                  string               `json:"id"`
	Intent              string               `json:"intent"`
	Entries             []corpusEntry        `json:"entries"`
	Facts               []corpusFact         `json:"facts"`
	MachineExpectations []machineExpectation `json:"machineExpectations"`
	HumanCriteria       []humanCriterion     `json:"humanCriteria"`
}

type corpusEntry struct {
	Ordinal             int             `json:"ordinal"`
	Kind                string          `json:"kind"`
	Actor               string          `json:"actor"`
	RelatedEntryOrdinal *int            `json:"relatedEntryOrdinal,omitempty"`
	ToolCallID          string          `json:"toolCallId,omitempty"`
	ToolName            string          `json:"toolName,omitempty"`
	ToolNamespace       string          `json:"toolNamespace,omitempty"`
	Segments            []corpusSegment `json:"segments"`
}

type corpusSegment struct {
	Ordinal int    `json:"ordinal"`
	Kind    string `json:"kind"`
	Origin  string `json:"origin"`
	Text    string `json:"text"`
}

type corpusFact struct {
	ID       string             `json:"id"`
	Kind     string             `json:"kind"`
	Outcome  string             `json:"outcome"`
	Evidence []corpusCoordinate `json:"evidence"`
	Value    corpusFactValue    `json:"value"`
}

type corpusCoordinate struct {
	EntryOrdinal   int  `json:"entryOrdinal"`
	SegmentOrdinal *int `json:"segmentOrdinal,omitempty"`
}

type corpusFactValue struct {
	ExitCode *int             `json:"exitCode,omitempty"`
	Error    string           `json:"error,omitempty"`
	Test     *corpusTestValue `json:"test,omitempty"`
}

type corpusTestValue struct {
	Framework string `json:"framework"`
	Passed    *int   `json:"passed,omitempty"`
	Failed    *int   `json:"failed,omitempty"`
	Skipped   *int   `json:"skipped,omitempty"`
}

type machineExpectation struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
}

type humanCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type evaluationCase struct {
	Definition   corpusCase
	Document     domain.EvidenceDocument
	FactAnalysis domain.FactAnalysis
}

type evaluationCorpus struct {
	Digest  string
	Profile reviewedCorpus
	Cases   []evaluationCase
}

func loadEvaluationCorpus(path string) (evaluationCorpus, error) {
	content, err := readBoundedFile(path, corpusMaxBytes)
	if err != nil {
		return evaluationCorpus{}, errors.New("evaluation corpus is unavailable")
	}
	digest := sha256Hex(content)
	profile, reviewed := reviewedCorpora[digest]
	if !reviewed {
		return evaluationCorpus{}, errors.New("evaluation corpus digest does not match the reviewed corpus")
	}
	var source corpusFile
	if err := decodeStrictJSON(content, &source); err != nil {
		return evaluationCorpus{}, errors.New("evaluation corpus is invalid")
	}
	if source.SchemaVersion != corpusSchemaVersion || len(source.Cases) != profile.CaseCount {
		return evaluationCorpus{}, errors.New("evaluation corpus is invalid")
	}

	seen := make(map[string]bool, len(source.Cases))
	cases := make([]evaluationCase, 0, len(source.Cases))
	for _, definition := range source.Cases {
		if seen[definition.ID] {
			return evaluationCorpus{}, errors.New("evaluation corpus is invalid")
		}
		seen[definition.ID] = true
		fixture, err := buildEvaluationCase(definition, profile)
		if err != nil {
			return evaluationCorpus{}, fmt.Errorf("evaluation corpus case %q is invalid", definition.ID)
		}
		cases = append(cases, fixture)
	}
	return evaluationCorpus{Digest: digest, Profile: profile, Cases: cases}, nil
}

func buildEvaluationCase(definition corpusCase, profile reviewedCorpus) (evaluationCase, error) {
	if !corpusIDPattern.MatchString(definition.ID) || !boundedText(definition.Intent, 512) ||
		len(definition.Entries) == 0 || len(definition.Entries) > 50 {
		return evaluationCase{}, errors.New("invalid case identity")
	}
	if err := validateCorpusDefinitions(definition); err != nil {
		return evaluationCase{}, err
	}

	documentDigest, err := caseDigest(definition)
	if err != nil {
		return evaluationCase{}, err
	}
	capturedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	revision := domain.EvidenceRevision{
		SourceKind: domain.EvidenceSourceSessions, CanonicalID: "evaluation:" + definition.ID,
		NativeSourceKind: "synthetic", SourceInstanceID: profile.SourceInstanceID, NativeID: definition.ID,
		SchemaVersion: 1, Disposition: "untrusted-history",
		DocumentDigest: domain.Digest{Scheme: "sha256-json-v1", Digest: documentDigest},
		AdapterVersion: profile.AdapterVersion, SourceState: "stable", Freshness: "captured",
		CapturedAt: capturedAt, SourceObservedAt: capturedAt, LineageCoverage: "complete",
	}

	entries := make([]domain.EvidenceEntry, len(definition.Entries))
	segmentCount, textBytes := 0, 0
	for index, sourceEntry := range definition.Entries {
		if sourceEntry.Ordinal != index {
			return evaluationCase{}, errors.New("entry order is invalid")
		}
		entry := domain.EvidenceEntry{
			Ordinal: index, Kind: sourceEntry.Kind, Actor: sourceEntry.Actor,
			RelatedEntryOrdinal: cloneInt(sourceEntry.RelatedEntryOrdinal),
			ToolCallID:          sourceEntry.ToolCallID, ToolName: sourceEntry.ToolName,
			ToolNamespace: sourceEntry.ToolNamespace,
			Content:       make([]domain.EvidenceSegment, len(sourceEntry.Segments)),
		}
		for segmentIndex, sourceSegment := range sourceEntry.Segments {
			if sourceSegment.Ordinal != segmentIndex {
				return evaluationCase{}, errors.New("segment order is invalid")
			}
			selected := selectedText(sourceSegment.Text)
			entry.Content[segmentIndex] = domain.EvidenceSegment{
				Ordinal: segmentIndex, Kind: sourceSegment.Kind, Origin: sourceSegment.Origin,
				OriginConfidence: "explicit", Text: &selected,
			}
			segmentCount++
			textBytes += selected.EmittedUTF8Bytes
		}
		entries[index] = entry
	}
	first, last := 0, len(entries)-1
	selection := domain.EvidenceSelection{
		Mode:      "full",
		Relations: domain.CountSelection{Selected: 0, Total: 0},
		Entries: domain.EntrySelection{
			Selected: len(entries), Total: len(entries), FirstOrdinal: &first, LastOrdinal: &last,
		},
		Segments: domain.CountSelection{Selected: segmentCount, Total: segmentCount},
		SegmentText: domain.ByteSelection{
			EmittedUTF8Bytes: textBytes, OriginalUTF8Bytes: textBytes,
		},
		Coverage: domain.CoverageCompleteRetainedSnapshot,
	}
	document := domain.EvidenceDocument{
		Revision: revision, Selection: selection, Relations: []domain.EvidenceRelation{}, Entries: entries,
	}

	runID := "evaluation-facts-" + definition.ID
	facts := make([]domain.Fact, len(definition.Facts))
	factIDs := make([]string, len(definition.Facts))
	for index, sourceFact := range definition.Facts {
		refs := make([]domain.EvidenceRef, len(sourceFact.Evidence))
		for refIndex, coordinate := range sourceFact.Evidence {
			ref, err := noemaevidence.SessionsReference(
				document, coordinate.EntryOrdinal, coordinate.SegmentOrdinal,
			)
			if err != nil {
				return evaluationCase{}, err
			}
			refs[refIndex] = ref
		}
		factID := definition.ID + "/" + sourceFact.ID
		value := domain.FactValue{ExitCode: cloneInt(sourceFact.Value.ExitCode)}
		if sourceFact.Value.Error != "" {
			errorText := selectedText(sourceFact.Value.Error)
			value.Error = &errorText
		}
		if sourceFact.Value.Test != nil {
			value.Test = &domain.TestFactValue{
				Framework: sourceFact.Value.Test.Framework,
				Passed:    cloneInt(sourceFact.Value.Test.Passed), Failed: cloneInt(sourceFact.Value.Test.Failed),
				Skipped: cloneInt(sourceFact.Value.Test.Skipped),
			}
		}
		fingerprint := sha256Hex([]byte(factID))
		facts[index] = domain.Fact{
			ID: factID, Fingerprint: fingerprint, AnalysisRunID: runID, Kind: sourceFact.Kind,
			SchemaVersion: 1, Value: value, Outcome: sourceFact.Outcome,
			ExtractorName: profile.AdapterVersion, ExtractorVersion: "1", ParseRule: profile.AdapterVersion,
			Evidence: refs, CreatedAt: capturedAt,
		}
		factIDs[index] = factID
	}
	revisionCopy, selectionCopy := revision, selection
	analysis := domain.FactAnalysis{
		Run: domain.AnalysisRun{
			ID: runID, Stage: domain.AnalysisStageFacts, RequestedSourceIdentity: revision.CanonicalID,
			Revision: &revisionCopy, Selection: &selectionCopy, ExtractorName: profile.AdapterVersion,
			ExtractorVersion: "1", SchemaVersion: 1, FactIDs: factIDs,
			Omissions: domain.AnalysisOmissions{}, Status: domain.AnalysisCompleted,
			StartedAt: capturedAt, FinishedAt: capturedAt,
		},
		Facts: facts,
	}
	return evaluationCase{Definition: definition, Document: document, FactAnalysis: analysis}, nil
}

func validateCorpusDefinitions(definition corpusCase) error {
	textValues := []string{definition.ID, definition.Intent}
	for index, entry := range definition.Entries {
		if entry.Ordinal != index || !oneOf(entry.Kind, "message", "system", "tool-call", "tool-result") ||
			!oneOf(entry.Actor, "human", "model", "tool", "system") || len(entry.Segments) == 0 {
			return errors.New("invalid entry")
		}
		if entry.RelatedEntryOrdinal != nil &&
			(*entry.RelatedEntryOrdinal < 0 || *entry.RelatedEntryOrdinal >= index) {
			return errors.New("invalid entry relation")
		}
		if entry.Kind == "tool-result" && entry.RelatedEntryOrdinal != nil {
			target := definition.Entries[*entry.RelatedEntryOrdinal]
			if target.Kind != "tool-call" || target.ToolCallID == "" || target.ToolCallID != entry.ToolCallID {
				return errors.New("invalid tool relation")
			}
		}
		textValues = append(textValues, entry.ToolCallID, entry.ToolName, entry.ToolNamespace)
		for segmentIndex, segment := range entry.Segments {
			if segment.Ordinal != segmentIndex || segment.Kind != "text" ||
				!oneOf(segment.Origin, "human", "model", "tool", "system") ||
				!boundedText(segment.Text, 8*1024) {
				return errors.New("invalid segment")
			}
			textValues = append(textValues, segment.Text)
		}
	}
	seenFacts := make(map[string]bool)
	for _, fact := range definition.Facts {
		if !corpusIDPattern.MatchString(fact.ID) || seenFacts[fact.ID] ||
			!oneOf(fact.Kind, "error-output", "exit-code", "test-result") ||
			!oneOf(fact.Outcome, domain.FactOutcomeSuccess, domain.FactOutcomeFailure, domain.FactOutcomeUnknown) ||
			len(fact.Evidence) == 0 {
			return errors.New("invalid fact")
		}
		seenFacts[fact.ID] = true
		valueCount := 0
		if fact.Value.ExitCode != nil {
			valueCount++
		}
		if fact.Value.Error != "" {
			valueCount++
			textValues = append(textValues, fact.Value.Error)
		}
		if fact.Value.Test != nil {
			valueCount++
			if !boundedText(fact.Value.Test.Framework, 128) {
				return errors.New("invalid test fact")
			}
			textValues = append(textValues, fact.Value.Test.Framework)
		}
		if (fact.Kind == "error-output" && valueCount != 1) ||
			(fact.Kind == "test-result" && (fact.Value.Test == nil || fact.Value.ExitCode == nil)) ||
			valueCount == 0 {
			return errors.New("invalid fact value")
		}
		for _, coordinate := range fact.Evidence {
			if coordinate.EntryOrdinal < 0 || coordinate.EntryOrdinal >= len(definition.Entries) {
				return errors.New("invalid fact coordinate")
			}
			if coordinate.SegmentOrdinal != nil &&
				(*coordinate.SegmentOrdinal < 0 ||
					*coordinate.SegmentOrdinal >= len(definition.Entries[coordinate.EntryOrdinal].Segments)) {
				return errors.New("invalid fact segment coordinate")
			}
		}
	}
	if err := validateExpectations(definition, &textValues); err != nil {
		return err
	}
	sanitized, _, err := (application.PrivacyPolicy{}).PreflightBatch(textValues)
	if err != nil || len(sanitized) != len(textValues) {
		return errors.New("corpus privacy preflight failed")
	}
	for index := range sanitized {
		if sanitized[index] != textValues[index] {
			return errors.New("corpus contains privacy-sensitive text")
		}
	}
	return nil
}

func validateExpectations(definition corpusCase, textValues *[]string) error {
	seen := make(map[string]bool)
	for _, expectation := range definition.MachineExpectations {
		if !corpusIDPattern.MatchString(expectation.ID) || seen[expectation.ID] ||
			!oneOf(expectation.Kind, "must-be-empty", "must-include-claim-type", "must-not-include-outcome") {
			return errors.New("invalid machine expectation")
		}
		if expectation.Kind == "must-be-empty" && expectation.Value != "" {
			return errors.New("invalid empty expectation")
		}
		if expectation.Kind == "must-include-claim-type" &&
			!domain.ClaimType(expectation.Value).Valid() {
			return errors.New("invalid claim type expectation")
		}
		if expectation.Kind == "must-not-include-outcome" &&
			!oneOf(expectation.Value, domain.FactOutcomeSuccess, domain.FactOutcomeFailure, domain.FactOutcomeUnknown) {
			return errors.New("invalid outcome expectation")
		}
		seen[expectation.ID] = true
		*textValues = append(*textValues, expectation.ID, expectation.Kind, expectation.Value)
	}
	for _, criterion := range definition.HumanCriteria {
		if !corpusIDPattern.MatchString(criterion.ID) || seen[criterion.ID] ||
			!boundedText(criterion.Description, 1024) {
			return errors.New("invalid human criterion")
		}
		seen[criterion.ID] = true
		*textValues = append(*textValues, criterion.ID, criterion.Description)
	}
	return nil
}

func selectedText(value string) domain.SelectedText {
	size := len([]byte(value))
	return domain.SelectedText{
		Text: value, OriginalUTF8Bytes: size, EmittedUTF8Bytes: size,
		ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: sha256Hex([]byte(value))},
	}
}

func caseDigest(value corpusCase) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("path is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(content) == 0 || int64(len(content)) > limit {
		return nil, errors.New("file is empty or too large")
	}
	return content, nil
}

func decodeStrictJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func boundedText(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && len([]byte(value)) <= maximum
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
