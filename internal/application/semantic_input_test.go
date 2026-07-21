package application

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
	noemaevidence "github.com/ferueda/noema/internal/evidence"
)

func TestBuildSemanticInputBuildsCompleteUnderBudgetSelection(t *testing.T) {
	analysis, document := semanticInputFixture(t)

	prepared, err := BuildSemanticInput(analysis, document, EntryBounds{})
	if err != nil {
		t.Fatalf("build semantic input: %v", err)
	}
	selection := prepared.ModelInput.Selection
	if prepared.ModelInput.SchemaVersion != SemanticInputSchemaVersion ||
		prepared.ModelInput.Disposition != semanticInputDisposition ||
		selection.Mode != "complete" || selection.Coverage != domain.CoverageCompleteRetainedSnapshot ||
		selection.SelectedEntries != 3 || selection.TotalEntries != 3 ||
		selection.FirstOrdinal == nil || *selection.FirstOrdinal != 0 ||
		selection.LastOrdinal == nil || *selection.LastOrdinal != 2 ||
		selection.TruncatedTextSegments != 0 {
		t.Fatalf("model input/selection = %#v / %#v", prepared.ModelInput, selection)
	}
	if len(prepared.ModelInput.Entries) != 3 || len(prepared.EvidenceByID) != 6 {
		t.Fatalf("entry/evidence counts = %d/%d", len(prepared.ModelInput.Entries), len(prepared.EvidenceByID))
	}
	if got := semanticFactIDs(prepared.ModelInput.Facts); !reflect.DeepEqual(got, analysis.Run.FactIDs) {
		t.Fatalf("fact IDs = %#v, want %#v", got, analysis.Run.FactIDs)
	}
}

func TestBuildSemanticInputBuildsExplicitContiguousRange(t *testing.T) {
	analysis, document := semanticInputFixture(t)
	first, last := 1, 2

	prepared, err := BuildSemanticInput(analysis, document, EntryBounds{First: &first, Last: &last})
	if err != nil {
		t.Fatalf("build range: %v", err)
	}
	selection := prepared.ModelInput.Selection
	if selection.Mode != "range" || selection.Coverage != semanticCoveragePartial ||
		selection.SelectedEntries != 2 || selection.TotalEntries != 3 || selection.ExcludedFactCount != 1 {
		t.Fatalf("range selection = %#v", selection)
	}
	entries := prepared.ModelInput.Entries
	if len(entries) != 2 || entries[0].Ordinal != 1 || entries[1].Ordinal != 2 ||
		entries[1].RelatedEntryOrdinal == nil || *entries[1].RelatedEntryOrdinal != 1 ||
		entries[1].ToolName != "exec_command" {
		t.Fatalf("range entries = %#v", entries)
	}
	if got := semanticFactIDs(prepared.ModelInput.Facts); !reflect.DeepEqual(got, []string{"fact-command", "fact-exit", "fact-test"}) {
		t.Fatalf("range fact IDs = %#v", got)
	}
}

func TestBuildSemanticInputExcludesCrossBoundaryFact(t *testing.T) {
	analysis, document := semanticInputFixture(t)
	first, last := 2, 2

	prepared, err := BuildSemanticInput(analysis, document, EntryBounds{First: &first, Last: &last})
	if err != nil {
		t.Fatalf("build result-only range: %v", err)
	}
	if got := semanticFactIDs(prepared.ModelInput.Facts); !reflect.DeepEqual(got, []string{"fact-exit"}) {
		t.Fatalf("result-only fact IDs = %#v", got)
	}
	if prepared.ModelInput.Selection.ExcludedFactCount != 3 {
		t.Fatalf("excluded fact count = %d", prepared.ModelInput.Selection.ExcludedFactCount)
	}
	if _, ok := prepared.FactsByID["fact-test"]; ok {
		t.Fatal("cross-boundary fact was available to candidate validation")
	}
}

func TestBuildSemanticInputRejectsInvalidBounds(t *testing.T) {
	for _, test := range []struct {
		name   string
		bounds EntryBounds
	}{
		{name: "only first", bounds: EntryBounds{First: testIntPointer(0)}},
		{name: "only last", bounds: EntryBounds{Last: testIntPointer(1)}},
		{name: "negative", bounds: EntryBounds{First: testIntPointer(-1), Last: testIntPointer(1)}},
		{name: "reversed", bounds: EntryBounds{First: testIntPointer(2), Last: testIntPointer(1)}},
		{name: "past end", bounds: EntryBounds{First: testIntPointer(1), Last: testIntPointer(3)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticInputFixture(t)
			if _, err := BuildSemanticInput(analysis, document, test.bounds); !errors.Is(err, ErrSemanticInputInvalid) {
				t.Fatalf("error = %v, want semantic input invalid", err)
			}
		})
	}
}

func TestBuildSemanticInputRejectsImplicitOversizeWithoutTruncation(t *testing.T) {
	for _, test := range []struct {
		name   string
		limits func(semanticInputLimits) semanticInputLimits
	}{
		{name: "entry count", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.entries = 2
			return limits
		}},
		{name: "text value", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.textValueBytes = 4
			return limits
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticInputFixture(t)
			prepared, err := buildSemanticInput(analysis, document, EntryBounds{}, test.limits(defaultSemanticInputLimits))
			if !errors.Is(err, ErrSemanticInputTooLarge) {
				t.Fatalf("error = %v, want semantic input too large", err)
			}
			if len(prepared.ModelInput.Entries) != 0 {
				t.Fatalf("failed input returned %d truncated entries", len(prepared.ModelInput.Entries))
			}
		})
	}
}

func TestBuildSemanticInputTruncatesExplicitTextAtUTF8Boundary(t *testing.T) {
	document := semanticDocument("ab🙂cd")
	analysis := semanticAnalysisForDocument(t, document, nil)
	first, last := 0, 0
	limits := defaultSemanticInputLimits
	limits.textValueBytes = 5

	prepared, err := buildSemanticInput(analysis, document, EntryBounds{First: &first, Last: &last}, limits)
	if err != nil {
		t.Fatalf("build explicit bounded text: %v", err)
	}
	text := prepared.ModelInput.Entries[0].Segments[0].Text
	if text == nil || text.Text != "ab" || !text.Truncated || text.OriginalUTF8Bytes != 8 ||
		text.EmittedUTF8Bytes != 2 || !json.Valid(mustJSON(t, prepared.ModelInput)) {
		t.Fatalf("bounded UTF-8 text = %#v", text)
	}
	if prepared.ModelInput.Selection.Coverage != semanticCoveragePartial ||
		prepared.ModelInput.Selection.TruncatedTextSegments != 1 {
		t.Fatalf("truncated selection = %#v", prepared.ModelInput.Selection)
	}
}

func TestBuildSemanticInputEnforcesInjectedCaps(t *testing.T) {
	for _, test := range []struct {
		name   string
		limits func(semanticInputLimits) semanticInputLimits
	}{
		{name: "evidence references", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.evidenceRefs = 1
			return limits
		}},
		{name: "fact count", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.facts = 1
			return limits
		}},
		{name: "encoded evidence section", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.evidenceSectionBytes = 1
			return limits
		}},
		{name: "encoded fact section", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.factSectionBytes = 1
			return limits
		}},
		{name: "complete encoded input", limits: func(limits semanticInputLimits) semanticInputLimits {
			limits.inputBytes = 1
			return limits
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticInputFixture(t)
			if _, err := buildSemanticInput(analysis, document, EntryBounds{}, test.limits(defaultSemanticInputLimits)); !errors.Is(err, ErrSemanticInputTooLarge) {
				t.Fatalf("error = %v, want semantic input too large", err)
			}
		})
	}
}

func TestBuildSemanticInputAcceptsEmptySnapshot(t *testing.T) {
	document := semanticDocument()
	analysis := semanticAnalysisForDocument(t, document, nil)

	prepared, err := BuildSemanticInput(analysis, document, EntryBounds{})
	if err != nil {
		t.Fatalf("build empty input: %v", err)
	}
	selection := prepared.ModelInput.Selection
	if selection.Mode != "complete" || selection.Coverage != domain.CoverageCompleteRetainedSnapshot ||
		selection.SelectedEntries != 0 || selection.FirstOrdinal != nil || selection.LastOrdinal != nil ||
		len(prepared.ModelInput.Entries) != 0 || len(prepared.ModelInput.Facts) != 0 {
		t.Fatalf("empty semantic input = %#v", prepared.ModelInput)
	}
}

func TestBuildSemanticInputRejectsInvalidSourceAndFactState(t *testing.T) {
	for _, test := range []struct {
		name   string
		want   error
		mutate func(*domain.FactAnalysis, *domain.EvidenceDocument)
	}{
		{name: "wrong stage", want: ErrSemanticInputInvalid, mutate: func(analysis *domain.FactAnalysis, _ *domain.EvidenceDocument) {
			analysis.Run.Stage = domain.AnalysisStageClaims
		}},
		{name: "wrong status", want: ErrSemanticInputInvalid, mutate: func(analysis *domain.FactAnalysis, _ *domain.EvidenceDocument) {
			analysis.Run.Status = domain.AnalysisFailed
		}},
		{name: "missing revision", want: ErrSemanticInputInvalid, mutate: func(analysis *domain.FactAnalysis, _ *domain.EvidenceDocument) {
			analysis.Run.Revision = nil
		}},
		{name: "digest mismatch", want: ErrSourceRevisionUnavailable, mutate: func(_ *domain.FactAnalysis, document *domain.EvidenceDocument) {
			document.Revision.DocumentDigest.Digest = strings.Repeat("e", 64)
		}},
		{name: "tampered fact evidence", want: ErrSemanticInputInvalid, mutate: func(analysis *domain.FactAnalysis, _ *domain.EvidenceDocument) {
			analysis.Facts[0].Evidence[0].ID = "eref_tampered"
		}},
		{name: "fact order", want: ErrSemanticInputInvalid, mutate: func(analysis *domain.FactAnalysis, _ *domain.EvidenceDocument) {
			analysis.Run.FactIDs[0], analysis.Run.FactIDs[1] = analysis.Run.FactIDs[1], analysis.Run.FactIDs[0]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticInputFixture(t)
			test.mutate(&analysis, &document)
			if _, err := BuildSemanticInput(analysis, document, EntryBounds{}); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestBuildSemanticInputIsDeterministic(t *testing.T) {
	analysis, document := semanticInputFixture(t)
	first, last := 1, 2

	firstBuild, err := BuildSemanticInput(analysis, document, EntryBounds{First: &first, Last: &last})
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	secondBuild, err := BuildSemanticInput(analysis, document, EntryBounds{First: &first, Last: &last})
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if !bytes.Equal(mustJSON(t, firstBuild.ModelInput), mustJSON(t, secondBuild.ModelInput)) ||
		!reflect.DeepEqual(firstBuild.EvidenceByID, secondBuild.EvidenceByID) ||
		!reflect.DeepEqual(firstBuild.OrderedFacts, secondBuild.OrderedFacts) {
		t.Fatal("same source and bounds produced different semantic input")
	}
}

func semanticInputFixture(t *testing.T) (domain.FactAnalysis, domain.EvidenceDocument) {
	t.Helper()
	document := semanticDocument(
		"Please verify the generic cache change.",
		`{"cmd":"go test ./..."}`,
		"Process exited with code 1\nFAIL example/package\n",
	)
	document.Entries[0].Actor = "human"
	document.Entries[0].Content[0].Origin = "human"
	document.Entries[1].Kind = "tool-call"
	document.Entries[1].ToolCallID = "call-one"
	document.Entries[1].ToolName = "exec_command"
	document.Entries[1].ToolNamespace = "terminal"
	document.Entries[2].Kind = "tool-result"
	document.Entries[2].Actor = "tool"
	document.Entries[2].Content[0].Origin = "tool"
	document.Entries[2].ToolCallID = "call-one"
	document.Entries[2].ToolName = "exec_command"
	document.Entries[2].ToolNamespace = "terminal"
	document.Entries[2].RelatedEntryOrdinal = testIntPointer(1)

	entryZero, err := noemaevidence.SessionsReference(document, 0, nil)
	if err != nil {
		t.Fatalf("entry zero reference: %v", err)
	}
	segmentOne := 0
	commandRef, err := noemaevidence.SessionsReference(document, 1, &segmentOne)
	if err != nil {
		t.Fatalf("command reference: %v", err)
	}
	segmentTwo := 0
	resultRef, err := noemaevidence.SessionsReference(document, 2, &segmentTwo)
	if err != nil {
		t.Fatalf("result reference: %v", err)
	}

	facts := []domain.Fact{
		semanticFactFixture("fact-note", "note", domain.FactOutcomeNotApplicable, []domain.EvidenceRef{entryZero}),
		semanticFactFixture("fact-command", "command", domain.FactOutcomeNotApplicable, []domain.EvidenceRef{commandRef}),
		semanticFactFixture("fact-exit", "exit-code", domain.FactOutcomeFailure, []domain.EvidenceRef{resultRef}),
		semanticFactFixture("fact-test", "test-result", domain.FactOutcomeFailure, []domain.EvidenceRef{commandRef, resultRef}),
	}
	facts[1].Value.Command = semanticDomainText("go test ./...")
	exitCode := 1
	facts[2].Value.ExitCode = &exitCode
	facts[3].Value.Test = &domain.TestFactValue{Framework: "go-test", Failed: testIntPointer(1)}
	facts[3].Value.ExitCode = &exitCode
	return semanticAnalysisForDocument(t, document, facts), document
}

func semanticDocument(texts ...string) domain.EvidenceDocument {
	entries := make([]domain.EvidenceEntry, 0, len(texts))
	totalBytes := 0
	for ordinal, text := range texts {
		selected := semanticDomainText(text)
		totalBytes += selected.EmittedUTF8Bytes
		entries = append(entries, domain.EvidenceEntry{
			Ordinal: ordinal, Kind: "message", Actor: "model",
			Content: []domain.EvidenceSegment{{
				Ordinal: 0, Kind: "text", Origin: "model", OriginConfidence: "high", Text: selected,
			}},
		})
	}
	selection := domain.EvidenceSelection{
		Mode:        "full",
		Entries:     domain.EntrySelection{Selected: len(entries), Total: len(entries)},
		Segments:    domain.CountSelection{Selected: len(entries), Total: len(entries)},
		SegmentText: domain.ByteSelection{EmittedUTF8Bytes: totalBytes, OriginalUTF8Bytes: totalBytes},
		Coverage:    domain.CoverageCompleteRetainedSnapshot,
	}
	if len(entries) > 0 {
		selection.Entries.FirstOrdinal = testIntPointer(0)
		selection.Entries.LastOrdinal = testIntPointer(len(entries) - 1)
	}
	return domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind: domain.EvidenceSourceSessions, CanonicalID: "synthetic@local:semantic-input",
			NativeSourceKind: "synthetic", SourceInstanceID: "local", NativeID: "semantic-input",
			DocumentDigest: domain.Digest{
				Scheme: "sha256-sessions-document-jcs-v1", Digest: strings.Repeat("d", 64),
			},
			LineageCoverage: "complete",
		},
		Selection: selection,
		Entries:   entries,
	}
}

func semanticAnalysisForDocument(t *testing.T, document domain.EvidenceDocument, facts []domain.Fact) domain.FactAnalysis {
	t.Helper()
	runID := "fact-analysis-one"
	factIDs := make([]string, len(facts))
	for index := range facts {
		facts[index].AnalysisRunID = runID
		factIDs[index] = facts[index].ID
	}
	revision := document.Revision
	selection := document.Selection
	return domain.FactAnalysis{
		Run: domain.AnalysisRun{
			ID: runID, Stage: domain.AnalysisStageFacts, Status: domain.AnalysisCompleted,
			RequestedSourceIdentity: document.Revision.CanonicalID, Revision: &revision, Selection: &selection,
			FactIDs: factIDs,
		},
		Facts: facts,
	}
}

func semanticFactFixture(id, kind, outcome string, refs []domain.EvidenceRef) domain.Fact {
	return domain.Fact{ID: id, Kind: kind, Outcome: outcome, Evidence: refs}
}

func semanticDomainText(text string) *domain.SelectedText {
	hash := sha256.Sum256([]byte(text))
	return &domain.SelectedText{
		Text: text, OriginalUTF8Bytes: len([]byte(text)), EmittedUTF8Bytes: len([]byte(text)),
		ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
	}
}

func semanticFactIDs(facts []SemanticFactInput) []string {
	ids := make([]string, len(facts))
	for index := range facts {
		ids[index] = facts[index].ID
	}
	return ids
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode JSON: %v", err)
	}
	return encoded
}

func testIntPointer(value int) *int { return &value }
