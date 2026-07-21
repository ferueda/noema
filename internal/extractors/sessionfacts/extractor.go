package sessionfacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/evidence"
)

const (
	extractorName        = "sessions-deterministic-facts"
	extractorVersion     = "1"
	factSchemaVersion    = 1
	maxTextValueBytes    = 2 * 1024
	maxTextFactValues    = 128
	maxAnalysisTextBytes = 64 * 1024
)

var (
	exitCodePattern = regexp.MustCompile(`(?m)^Process exited with code ([0-9]+)\s*$`)
	passedPattern   = regexp.MustCompile(`(?i)\b([0-9]+) passed\b`)
	failedPattern   = regexp.MustCompile(`(?i)\b([0-9]+) failed\b`)
	skippedPattern  = regexp.MustCompile(`(?i)\b([0-9]+) skipped\b`)
)

type Extractor struct{}

func (Extractor) Name() string       { return extractorName }
func (Extractor) Version() string    { return extractorVersion }
func (Extractor) SchemaVersion() int { return factSchemaVersion }

func (Extractor) Extract(document domain.EvidenceDocument) ([]domain.FactDraft, domain.AnalysisOmissions, error) {
	state := extractionState{
		document: document,
		commands: make(map[int]commandMatch),
		omissions: domain.AnalysisOmissions{
			CanonicalSegments: document.Selection.CanonicalOmittedSegments,
			UnknownLineage:    document.Revision.LineageCoverage == "unknown",
		},
	}
	for entryIndex := range document.Entries {
		if err := state.extractEntry(document.Entries[entryIndex]); err != nil {
			return nil, state.omissions, err
		}
	}
	return state.facts, state.omissions, nil
}

type extractionState struct {
	document  domain.EvidenceDocument
	facts     []domain.FactDraft
	commands  map[int]commandMatch
	textCount int
	textBytes int
	omissions domain.AnalysisOmissions
}

type commandMatch struct {
	evidence  domain.EvidenceRef
	framework string
}

func (state *extractionState) extractEntry(entry domain.EvidenceEntry) error {
	switch entry.Kind {
	case "tool-call":
		if err := state.addToolFact(entry, "call"); err != nil {
			return err
		}
		return state.extractCommand(entry)
	case "tool-result":
		if err := state.addToolFact(entry, "result"); err != nil {
			return err
		}
		return state.extractResult(entry)
	}
	return nil
}

func (state *extractionState) addToolFact(entry domain.EvidenceEntry, kind string) error {
	ref, err := evidence.SessionsReference(state.document, entry.Ordinal, nil)
	if err != nil {
		return err
	}
	state.facts = append(state.facts, domain.FactDraft{
		Kind: "tool-" + kind,
		Value: domain.FactValue{Tool: &domain.ToolFactValue{
			Kind: kind, Name: entry.ToolName, Namespace: entry.ToolNamespace,
			CallID: entry.ToolCallID, RelatedEntryOrdinal: entry.RelatedEntryOrdinal,
		}},
		Outcome: domain.FactOutcomeNotApplicable, ParseRule: "sessions-entry-tool-" + kind + "-v1",
		Evidence: []domain.EvidenceRef{ref},
	})
	return nil
}

func (state *extractionState) extractCommand(entry domain.EvidenceEntry) error {
	if !recognizedCommandTool(entry.ToolName) {
		return nil
	}
	for _, segment := range entry.Content {
		if segment.Text == nil {
			continue
		}
		command, ok := exactCommand(segment.Text.Text)
		if !ok {
			continue
		}
		segmentOrdinal := segment.Ordinal
		ref, err := evidence.SessionsReference(state.document, entry.Ordinal, &segmentOrdinal)
		if err != nil {
			return err
		}
		selected := state.selectText(command)
		state.commands[entry.Ordinal] = commandMatch{evidence: ref, framework: testFramework(command)}
		state.facts = append(state.facts, domain.FactDraft{
			Kind: "command", Value: domain.FactValue{Command: cloneText(selected)},
			Outcome: domain.FactOutcomeNotApplicable, ParseRule: "tool-call-command-json-v1",
			Evidence: []domain.EvidenceRef{ref},
		})
		if framework := testFramework(command); framework != "" {
			testText := state.selectText(command)
			state.facts = append(state.facts, domain.FactDraft{
				Kind:    "test-command",
				Value:   domain.FactValue{Test: &domain.TestFactValue{Framework: framework, Command: cloneText(testText)}},
				Outcome: domain.FactOutcomeUnknown, ParseRule: "recognized-test-command-v1",
				Evidence: []domain.EvidenceRef{ref},
			})
		}
		return nil
	}
	return nil
}

func (state *extractionState) extractResult(entry domain.EvidenceEntry) error {
	for _, segment := range entry.Content {
		if segment.Text == nil {
			continue
		}
		text := segment.Text.Text
		segmentOrdinal := segment.Ordinal
		ref, err := evidence.SessionsReference(state.document, entry.Ordinal, &segmentOrdinal)
		if err != nil {
			return err
		}
		if code, ok := exitCode(text); ok {
			outcome := domain.FactOutcomeSuccess
			if code != 0 {
				outcome = domain.FactOutcomeFailure
			}
			state.facts = append(state.facts, domain.FactDraft{
				Kind: "exit-code", Value: domain.FactValue{ExitCode: intPointer(code)}, Outcome: outcome,
				ParseRule: "process-exit-code-v1", Evidence: []domain.EvidenceRef{ref},
			})
			state.addTestResult(entry, ref, text, code)
		}
		if line, ok := explicitErrorLine(text); ok {
			selected := state.selectText(line)
			state.facts = append(state.facts, domain.FactDraft{
				Kind: "error-output", Value: domain.FactValue{Error: cloneText(selected)},
				Outcome: domain.FactOutcomeFailure, ParseRule: "explicit-error-line-v1",
				Evidence: []domain.EvidenceRef{ref},
			})
		}
	}
	return nil
}

func (state *extractionState) addTestResult(entry domain.EvidenceEntry, resultRef domain.EvidenceRef, output string, code int) {
	if entry.RelatedEntryOrdinal == nil {
		return
	}
	command, ok := state.commands[*entry.RelatedEntryOrdinal]
	if !ok || command.framework == "" {
		return
	}
	passed := countPointer(output, passedPattern)
	failed := countPointer(output, failedPattern)
	skipped := countPointer(output, skippedPattern)
	_, hasFailureMarker := explicitErrorLine(output)
	outcome := testOutcome(code, failed, hasFailureMarker)
	state.facts = append(state.facts, domain.FactDraft{
		Kind: "test-result",
		Value: domain.FactValue{Test: &domain.TestFactValue{
			Framework: command.framework, Passed: passed, Failed: failed, Skipped: skipped,
		}, ExitCode: intPointer(code)},
		Outcome: outcome, ParseRule: "linked-test-result-v1",
		Evidence: []domain.EvidenceRef{command.evidence, resultRef},
	})
}

func (state *extractionState) selectText(value string) domain.SelectedText {
	originalBytes := len([]byte(value))
	hash := sha256.Sum256([]byte(value))
	selected := domain.SelectedText{
		OriginalUTF8Bytes: originalBytes,
		ContentHash:       domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
	}
	if state.textCount >= maxTextFactValues || state.textBytes >= maxAnalysisTextBytes {
		selected.Truncated = originalBytes > 0
		state.omissions.OmittedTextFactCount++
		state.omissions.OmittedTextOriginalUTF8Bytes += originalBytes
		return selected
	}
	limit := maxTextValueBytes
	if remaining := maxAnalysisTextBytes - state.textBytes; remaining < limit {
		limit = remaining
	}
	selected.Text = truncateText(value, limit)
	selected.EmittedUTF8Bytes = len([]byte(selected.Text))
	selected.Truncated = selected.EmittedUTF8Bytes < originalBytes
	state.textCount++
	state.textBytes += selected.EmittedUTF8Bytes
	return selected
}

func exactCommand(value string) (string, bool) {
	decoder := json.NewDecoder(strings.NewReader(value))
	var payload map[string]json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return "", false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", false
	}
	rawCmd, hasCmd := payload["cmd"]
	rawCommand, hasCommand := payload["command"]
	if hasCmd == hasCommand {
		return "", false
	}
	if hasCmd {
		cmd, ok := decodeString(rawCmd)
		return cmd, ok && cmd != ""
	}
	command, ok := decodeString(rawCommand)
	return command, ok && command != ""
}

func decodeString(raw json.RawMessage) (string, bool) {
	if raw == nil {
		return "", false
	}
	var value string
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&value); err != nil {
		return "", false
	}
	return value, true
}

func recognizedCommandTool(name string) bool {
	return name == "exec_command" || name == "shell_command" || name == "run_command" || name == "terminal"
}

func testFramework(command string) string {
	trimmed := strings.TrimSpace(command)
	if hasShellControlOperator(trimmed) {
		return ""
	}
	for _, candidate := range []struct{ prefix, framework string }{
		{"go test", "go-test"}, {"pytest", "pytest"}, {"python -m pytest", "pytest"},
		{"npm test", "npm-test"}, {"npm run test", "npm-test"}, {"pnpm test", "pnpm-test"},
		{"yarn test", "yarn-test"}, {"cargo test", "cargo-test"}, {"make test", "make-test"},
	} {
		if trimmed == candidate.prefix || strings.HasPrefix(trimmed, candidate.prefix+" ") {
			return candidate.framework
		}
	}
	return ""
}

func hasShellControlOperator(command string) bool {
	var quote rune
	escaped := false
	for _, character := range command {
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			continue
		}
		if character == ';' || character == '|' || character == '&' || character == '\n' || character == '`' {
			return true
		}
	}
	return quote != 0 || escaped
}

func testOutcome(exitCode int, failed *int, hasFailureMarker bool) string {
	if exitCode != 0 {
		return domain.FactOutcomeFailure
	}
	if hasFailureMarker || (failed != nil && *failed > 0) {
		return domain.FactOutcomeUnknown
	}
	return domain.FactOutcomeSuccess
}

func exitCode(output string) (int, bool) {
	match := exitCodePattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.Atoi(match[1])
	return value, err == nil
}

func explicitErrorLine(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(trimmed, "ERROR") || strings.HasPrefix(trimmed, "FAIL") || strings.HasPrefix(lower, "error:") {
			return trimmed, true
		}
	}
	return "", false
}

func countPointer(output string, pattern *regexp.Regexp) *int {
	match := pattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return nil
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		return nil
	}
	return &value
}

func intPointer(value int) *int { return &value }

func cloneText(value domain.SelectedText) *domain.SelectedText {
	copy := value
	return &copy
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
