package sessionfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
)

func TestExtractorBuildsOnlyMechanicallySupportedFacts(t *testing.T) {
	document := evidenceDocument(
		entry(0, "tool-call", "model", nil, "exec_command", `{"cmd":"go test ./...","yield_time_ms":30000}`),
		entry(1, "tool-result", "tool", intPointer(0), "exec_command", "Process exited with code 0\n1 passed\n"),
		entry(2, "message", "model", nil, "", "The tests should pass now."),
	)
	facts, omissions, err := (Extractor{}).Extract(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	wantKinds := []string{"tool-call", "command", "test-command", "tool-result", "exit-code", "test-result"}
	if len(facts) != len(wantKinds) {
		t.Fatalf("fact count = %d, want %d: %#v", len(facts), len(wantKinds), facts)
	}
	for index, want := range wantKinds {
		if facts[index].Kind != want {
			t.Fatalf("fact %d kind = %q, want %q", index, facts[index].Kind, want)
		}
	}
	if facts[4].Outcome != domain.FactOutcomeSuccess || facts[5].Outcome != domain.FactOutcomeSuccess {
		t.Fatalf("exit/test outcomes = %q/%q", facts[4].Outcome, facts[5].Outcome)
	}
	if facts[5].Value.Test.Passed == nil || *facts[5].Value.Test.Passed != 1 {
		t.Fatalf("test result = %#v", facts[5].Value.Test)
	}
	if omissions.UnknownLineage != true {
		t.Fatal("unknown lineage was not retained")
	}
}

func TestExtractorCapsSelectedFactText(t *testing.T) {
	longCommand := strings.Repeat("x", maxTextValueBytes+500)
	document := evidenceDocument(entry(0, "tool-call", "model", nil, "exec_command", fmt.Sprintf(`{"cmd":%q}`, longCommand)))
	facts, _, err := (Extractor{}).Extract(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	command := facts[1].Value.Command
	if command == nil || command.EmittedUTF8Bytes != maxTextValueBytes || !command.Truncated ||
		command.OriginalUTF8Bytes != len(longCommand) {
		t.Fatalf("bounded command = %#v", command)
	}
}

func TestExtractorCapsAnalysisTextFactCount(t *testing.T) {
	entries := make([]domain.EvidenceEntry, 0, maxTextFactValues+1)
	for index := 0; index < maxTextFactValues+1; index++ {
		entries = append(entries, entry(index, "tool-call", "model", nil, "exec_command", fmt.Sprintf(`{"cmd":"echo %d"}`, index)))
	}
	facts, omissions, err := (Extractor{}).Extract(evidenceDocument(entries...))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	lastCommand := facts[len(facts)-1].Value.Command
	if lastCommand == nil || lastCommand.EmittedUTF8Bytes != 0 || omissions.OmittedTextFactCount != 1 {
		t.Fatalf("last command/omissions = %#v/%#v", lastCommand, omissions)
	}
}

func TestExtractorDoesNotTreatCompoundShellExitAsTestOutcome(t *testing.T) {
	for _, command := range []string{
		"go test ./... || true",
		"go test ./...; echo done",
		"go test ./... | tee results.txt",
		"go test ./... && echo done",
	} {
		t.Run(command, func(t *testing.T) {
			document := evidenceDocument(
				entry(0, "tool-call", "model", nil, "exec_command", fmt.Sprintf(`{"cmd":%q}`, command)),
				entry(1, "tool-result", "tool", intPointer(0), "", "Process exited with code 0\nFAIL package/example\n"),
			)
			facts, _, err := (Extractor{}).Extract(document)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			for _, fact := range facts {
				if fact.Kind == "test-command" || fact.Kind == "test-result" {
					t.Fatalf("compound command produced %s fact", fact.Kind)
				}
			}
		})
	}
}

func TestExtractorMarksContradictoryPassingExitAndFailureCountUnknown(t *testing.T) {
	document := evidenceDocument(
		entry(0, "tool-call", "model", nil, "exec_command", `{"cmd":"pytest"}`),
		entry(1, "tool-result", "tool", intPointer(0), "", "Process exited with code 0\n1 failed, 2 passed\n"),
	)
	facts, _, err := (Extractor{}).Extract(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, fact := range facts {
		if fact.Kind == "test-result" {
			if fact.Outcome != domain.FactOutcomeUnknown {
				t.Fatalf("test outcome = %q, want unknown", fact.Outcome)
			}
			return
		}
	}
	t.Fatal("test-result fact is absent")
}

func TestExtractorMarksPassingExitWithExplicitFailureMarkerUnknown(t *testing.T) {
	document := evidenceDocument(
		entry(0, "tool-call", "model", nil, "exec_command", `{"cmd":"go test ./..."}`),
		entry(1, "tool-result", "tool", intPointer(0), "", "Process exited with code 0\nFAIL package/example\n"),
	)
	facts, _, err := (Extractor{}).Extract(document)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, fact := range facts {
		if fact.Kind == "test-result" {
			if fact.Outcome != domain.FactOutcomeUnknown {
				t.Fatalf("test outcome = %q, want unknown", fact.Outcome)
			}
			return
		}
	}
	t.Fatal("test-result fact is absent")
}

func evidenceDocument(entries ...domain.EvidenceEntry) domain.EvidenceDocument {
	segments := 0
	bytes := 0
	for _, item := range entries {
		segments += len(item.Content)
		for _, segment := range item.Content {
			if segment.Text != nil {
				bytes += segment.Text.EmittedUTF8Bytes
			}
		}
	}
	first, last := 0, len(entries)-1
	selection := domain.EvidenceSelection{
		Mode: "full", Relations: domain.CountSelection{},
		Entries:     domain.EntrySelection{Selected: len(entries), Total: len(entries)},
		Segments:    domain.CountSelection{Selected: segments, Total: segments},
		SegmentText: domain.ByteSelection{EmittedUTF8Bytes: bytes, OriginalUTF8Bytes: bytes},
		Coverage:    domain.CoverageCompleteRetainedSnapshot,
	}
	if len(entries) > 0 {
		selection.Entries.FirstOrdinal = &first
		selection.Entries.LastOrdinal = &last
	}
	return domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind: domain.EvidenceSourceSessions, CanonicalID: "synthetic@local:one",
			DocumentDigest:  domain.Digest{Scheme: "sha256-sessions-document-jcs-v1", Digest: strings.Repeat("d", 64)},
			LineageCoverage: "unknown",
		},
		Selection: selection, Entries: entries,
	}
}

func entry(ordinal int, kind, actor string, related *int, toolName, text string) domain.EvidenceEntry {
	hash := sha256.Sum256([]byte(text))
	return domain.EvidenceEntry{
		Ordinal: ordinal, Kind: kind, Actor: actor, RelatedEntryOrdinal: related,
		ToolName: toolName, ToolCallID: "call",
		Content: []domain.EvidenceSegment{{
			Ordinal: 0, Kind: "text", Origin: actor, OriginConfidence: "high",
			Text: &domain.SelectedText{
				Text: text, OriginalUTF8Bytes: len(text), EmittedUTF8Bytes: len(text),
				ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
			},
		}},
	}
}
