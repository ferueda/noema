package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestInspectionCommandsCreateAndReadEmptyDatabase(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "noema.db")
	for _, test := range []struct {
		name    string
		command string
	}{
		{name: "jobs", command: "jobs"},
		{name: "ideas", command: "ideas"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := run(
				context.Background(),
				[]string{test.command, "list", "--database", databasePath},
				&stdout,
				&stderr,
			)
			if err != nil {
				t.Fatalf("run %s list: %v", test.command, err)
			}
			if got := strings.TrimSpace(stdout.String()); got != "[]" {
				t.Fatalf("%s output = %q, want []", test.command, got)
			}
			if stderr.Len() != 0 {
				t.Fatalf("%s stderr = %q, want empty", test.command, stderr.String())
			}
		})
	}
}

func TestWorkerRefusesBeforeClaimingWithoutRemoteApproval(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "noema.db")
	var stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"worker", "--once", "--database", databasePath},
		&bytes.Buffer{},
		&stderr,
	)
	if err == nil || !strings.Contains(err.Error(), "--allow-remote") {
		t.Fatalf("worker error = %v, want explicit remote approval error", err)
	}
}

func TestSessionFactAnalysisEndToEnd(t *testing.T) {
	context := context.Background()
	temp := t.TempDir()
	databasePath := filepath.Join(temp, "noema.db")
	exportPath := filepath.Join(temp, "export.jsonl")
	executable := filepath.Join(temp, "sessions")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec /bin/cat \"$NOEMA_FAKE_EXPORT\"\n"), 0o700); err != nil {
		t.Fatalf("write fake Sessions executable: %v", err)
	}
	t.Setenv("NOEMA_SESSIONS_COMMAND", executable)
	t.Setenv("NOEMA_FAKE_EXPORT", exportPath)
	writeExportFixture(t, exportPath, strings.Repeat("d", 64))

	first := runScanForTest(t, context, databasePath)
	if first.Reused || first.FactCount != 6 || first.Coverage != domain.CoverageCompleteRetainedSnapshot {
		t.Fatalf("first scan = %#v", first)
	}
	shown := showAnalysisForTest(t, context, databasePath, first.AnalysisID)
	if shown.Run.Status != domain.AnalysisCompleted || len(shown.Facts) != 6 {
		t.Fatalf("shown analysis = %#v", shown)
	}
	second := runScanForTest(t, context, databasePath)
	if !second.Reused || second.AnalysisID != first.AnalysisID {
		t.Fatalf("second scan = %#v, want reuse of %s", second, first.AnalysisID)
	}
	var resolvedOutput, resolvedError bytes.Buffer
	if err := run(context, []string{
		"analyses", "show", first.AnalysisID, "--resolve", "--database", databasePath,
	}, &resolvedOutput, &resolvedError); err != nil {
		t.Fatalf("resolve matching revision: %v; stderr: %s", err, resolvedError.String())
	}
	var resolved application.ResolvedFactAnalysis
	if err := json.Unmarshal(resolvedOutput.Bytes(), &resolved); err != nil {
		t.Fatalf("decode resolved analysis: %v", err)
	}
	if len(resolved.Evidence) != 4 {
		t.Fatalf("resolved evidence count = %d, want 4", len(resolved.Evidence))
	}
	assertResolvedFixtureEvidence(t, shown, resolved)

	writeExportFixture(t, exportPath, strings.Repeat("e", 64))
	third := runScanForTest(t, context, databasePath)
	if third.Reused || third.AnalysisID == first.AnalysisID {
		t.Fatalf("changed revision scan = %#v", third)
	}
	var stdout, stderr bytes.Buffer
	err := run(context, []string{
		"analyses", "show", first.AnalysisID, "--resolve", "--database", databasePath,
	}, &stdout, &stderr)
	if !errors.Is(err, application.ErrSourceRevisionUnavailable) {
		t.Fatalf("resolve old revision error = %v", err)
	}

	if err := os.WriteFile(exportPath, []byte("not-json\nprivate transcript secret\n"), 0o600); err != nil {
		t.Fatalf("write malformed export: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	err = run(context, []string{"scan", "sessions", testSessionID, "--database", databasePath}, &stdout, &stderr)
	if err == nil || strings.Contains(err.Error(), "private transcript secret") {
		t.Fatalf("malformed scan error = %v", err)
	}
	match := regexp.MustCompile(`analysis ([a-f0-9]+) failed`).FindStringSubmatch(err.Error())
	if len(match) != 2 {
		t.Fatalf("malformed scan did not return analysis id: %v", err)
	}
	failed := showAnalysisForTest(t, context, databasePath, match[1])
	if failed.Run.Status != domain.AnalysisFailed || len(failed.Run.Error) > 1024 || len(failed.Facts) != 0 {
		t.Fatalf("failed analysis = %#v", failed)
	}

	database, err := sqlitestore.Open(context, databasePath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer database.Close()
	for _, table := range []string{"events", "jobs", "agent_runs", "content_ideas"} {
		var count int
		if err := database.QueryRowContext(context, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

func TestFactTextBudgetsPersistThroughCLI(t *testing.T) {
	for _, test := range []struct {
		name              string
		commands          []string
		wantSelectedCount int
		wantSelectedBytes int
		wantOmitted       bool
		forbidden         string
	}{
		{
			name: "per-value cap", commands: []string{strings.Repeat("x", 4096)},
			wantSelectedCount: 1, wantSelectedBytes: 2048, forbidden: strings.Repeat("x", 4096),
		},
		{
			name: "fact-count cap", commands: commandFixtures(129, 16),
			wantSelectedCount: 128, wantSelectedBytes: 128 * 16, wantOmitted: true,
		},
		{
			name: "aggregate-byte cap", commands: commandFixtures(40, 2048),
			wantSelectedCount: 32, wantSelectedBytes: 64 * 1024, wantOmitted: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			temp := t.TempDir()
			databasePath := filepath.Join(temp, "noema.db")
			exportPath := filepath.Join(temp, "export.jsonl")
			executable := filepath.Join(temp, "sessions")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\nexec /bin/cat \"$NOEMA_FAKE_EXPORT\"\n"), 0o700); err != nil {
				t.Fatalf("write fake Sessions executable: %v", err)
			}
			t.Setenv("NOEMA_SESSIONS_COMMAND", executable)
			t.Setenv("NOEMA_FAKE_EXPORT", exportPath)
			writeCommandOnlyFixture(t, exportPath, strings.Repeat("c", 64), test.commands)

			scan := runScanForTest(t, ctx, databasePath)
			analysis := showAnalysisForTest(t, ctx, databasePath, scan.AnalysisID)
			count, totalBytes := selectedFactTextStats(analysis)
			if count != test.wantSelectedCount || totalBytes != test.wantSelectedBytes {
				t.Fatalf("selected text stats = %d/%d, want %d/%d", count, totalBytes, test.wantSelectedCount, test.wantSelectedBytes)
			}
			if test.wantOmitted && analysis.Run.Omissions.OmittedTextFactCount == 0 {
				t.Fatalf("omissions = %#v, want omitted fact text", analysis.Run.Omissions)
			}
			for _, fact := range analysis.Facts {
				for _, reference := range fact.Evidence {
					if reference.Excerpt != "" {
						t.Fatalf("fact %s persisted evidence excerpt", fact.ID)
					}
				}
			}
			if test.forbidden != "" {
				assertFactTablesExclude(t, ctx, databasePath, test.forbidden)
			}
		})
	}
}

const testSessionID = "synthetic@local:session-one"

type scanOutput struct {
	AnalysisID string `json:"analysisId"`
	Reused     bool   `json:"reused"`
	Coverage   string `json:"coverage"`
	FactCount  int    `json:"factCount"`
}

func runScanForTest(t *testing.T, ctx context.Context, databasePath string) scanOutput {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{"scan", "sessions", testSessionID, "--database", databasePath}, &stdout, &stderr); err != nil {
		t.Fatalf("scan: %v; stderr: %s", err, stderr.String())
	}
	var output scanOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode scan output: %v", err)
	}
	return output
}

func showAnalysisForTest(t *testing.T, ctx context.Context, databasePath, id string) domain.FactAnalysis {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{"analyses", "show", id, "--database", databasePath}, &stdout, &stderr); err != nil {
		t.Fatalf("show analysis: %v; stderr: %s", err, stderr.String())
	}
	var analysis domain.FactAnalysis
	if err := json.Unmarshal(stdout.Bytes(), &analysis); err != nil {
		t.Fatalf("decode analysis: %v", err)
	}
	return analysis
}

func assertResolvedFixtureEvidence(t *testing.T, analysis domain.FactAnalysis, resolved application.ResolvedFactAnalysis) {
	t.Helper()
	command := `{"cmd":"go test ./...","yield_time_ms":30000}`
	result := "Process exited with code 0\n1 passed\n"
	expected := map[string]struct {
		text string
		hash string
	}{
		"0:entry": {},
		"1:entry": {},
		"0:0":     {text: command, hash: textDigest(command)},
		"1:0":     {text: result, hash: textDigest(result)},
	}
	byID := make(map[string]application.ResolvedEvidence, len(resolved.Evidence))
	for _, item := range resolved.Evidence {
		key := fmt.Sprintf("%d:entry", item.Reference.EntryOrdinal)
		if item.Reference.SegmentOrdinal != nil {
			key = fmt.Sprintf("%d:%d", item.Reference.EntryOrdinal, *item.Reference.SegmentOrdinal)
		}
		want, ok := expected[key]
		if !ok {
			t.Fatalf("unexpected resolved coordinate %s", key)
		}
		if item.Text != want.text || item.Reference.ContentHash != want.hash || item.Truncated {
			t.Fatalf("resolved %s = text %q, hash %q, truncated %v", key, item.Text, item.Reference.ContentHash, item.Truncated)
		}
		byID[item.Reference.ID] = item
	}
	for _, fact := range analysis.Facts {
		for _, reference := range fact.Evidence {
			item, ok := byID[reference.ID]
			if !ok || item.Reference.EntryOrdinal != reference.EntryOrdinal ||
				!sameOrdinal(item.Reference.SegmentOrdinal, reference.SegmentOrdinal) ||
				item.Reference.ContentHash != reference.ContentHash {
				t.Fatalf("fact %s reference %#v did not resolve exactly", fact.ID, reference)
			}
		}
	}
}

func sameOrdinal(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func selectedFactTextStats(analysis domain.FactAnalysis) (int, int) {
	count := 0
	totalBytes := 0
	for _, fact := range analysis.Facts {
		for _, text := range []*domain.SelectedText{fact.Value.Command, fact.Value.Error} {
			if text != nil && text.EmittedUTF8Bytes > 0 {
				count++
				totalBytes += text.EmittedUTF8Bytes
			}
		}
		if fact.Value.Test != nil && fact.Value.Test.Command != nil && fact.Value.Test.Command.EmittedUTF8Bytes > 0 {
			count++
			totalBytes += fact.Value.Test.Command.EmittedUTF8Bytes
		}
	}
	return count, totalBytes
}

func assertFactTablesExclude(t *testing.T, ctx context.Context, databasePath, forbidden string) {
	t.Helper()
	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open fact database: %v", err)
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, "SELECT value_json, evidence_json FROM facts")
	if err != nil {
		t.Fatalf("query fact storage: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value, evidence string
		if err := rows.Scan(&value, &evidence); err != nil {
			t.Fatalf("scan fact storage: %v", err)
		}
		if strings.Contains(value, forbidden) || strings.Contains(evidence, forbidden) {
			t.Fatal("fact storage retained the wholesale oversized source value")
		}
	}
}

func writeExportFixture(t *testing.T, path, documentDigest string) {
	t.Helper()
	command := `{"cmd":"go test ./...","yield_time_ms":30000}`
	result := "Process exited with code 0\n1 passed\n"
	session := map[string]any{
		"canonicalId": testSessionID,
		"source":      map[string]any{"kind": "synthetic", "instanceId": "local"},
		"nativeId":    "session-one",
	}
	digest := map[string]any{"scheme": "sha256-sessions-document-jcs-v1", "digest": documentDigest}
	header := func(recordType string) map[string]any {
		return map[string]any{"schemaVersion": 1, "command": "export", "type": recordType, "disposition": "untrusted-history"}
	}
	records := make([]map[string]any, 0, 3)
	sessionRecord := header("session")
	sessionRecord["snapshot"] = map[string]any{
		"session": session, "documentDigest": digest,
		"capturedAt": "2026-07-20T10:00:00.000Z", "sourceState": "present",
		"sourceObservedAt": "2026-07-20T09:59:00.000Z", "adapterVersion": "synthetic-v1",
		"freshness": "current", "lineageCoverage": "unknown",
		"selection": map[string]any{
			"mode": "full", "relations": map[string]any{"selected": 0, "total": 0, "truncated": false},
			"entries":                  map[string]any{"selected": 2, "total": 2, "truncated": false, "firstOrdinal": 0, "lastOrdinal": 1},
			"segments":                 map[string]any{"selected": 2, "total": 2, "truncated": false},
			"segmentText":              map[string]any{"emittedUtf8Bytes": len(command) + len(result), "originalUtf8Bytes": len(command) + len(result), "truncated": false},
			"canonicalOmittedSegments": 0, "truncatedTextSegments": 0,
		},
	}
	records = append(records, sessionRecord)
	call := header("entry")
	call["session"], call["documentDigest"] = session, digest
	call["entry"] = map[string]any{
		"ordinal": 0, "kind": "tool-call", "actor": "model", "toolCallId": "call-one",
		"toolName": "exec_command", "content": []any{fixtureTextSegment("model", command)}, "omittedSegmentCount": 0,
	}
	records = append(records, call)
	toolResult := header("entry")
	toolResult["session"], toolResult["documentDigest"] = session, digest
	toolResult["entry"] = map[string]any{
		"ordinal": 1, "kind": "tool-result", "actor": "tool", "relatedEntryOrdinal": 0,
		"toolCallId": "call-one",
		"content":    []any{fixtureTextSegment("tool", result)}, "omittedSegmentCount": 0,
	}
	records = append(records, toolResult)
	var output strings.Builder
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("encode fixture: %v", err)
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(output.String()), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func fixtureTextSegment(origin, text string) map[string]any {
	hash := sha256.Sum256([]byte(text))
	return map[string]any{
		"ordinal": 0, "kind": "text", "origin": origin, "originConfidence": "high",
		"text":        map[string]any{"text": text, "truncated": false, "originalUtf8Bytes": len(text), "emittedUtf8Bytes": len(text)},
		"contentHash": map[string]any{"scheme": "sha256-utf8-v1", "digest": hex.EncodeToString(hash[:])},
	}
}

func writeCommandOnlyFixture(t *testing.T, path, documentDigest string, commands []string) {
	t.Helper()
	session := map[string]any{
		"canonicalId": testSessionID,
		"source":      map[string]any{"kind": "synthetic", "instanceId": "local"},
		"nativeId":    "session-one",
	}
	digest := map[string]any{"scheme": "sha256-sessions-document-jcs-v1", "digest": documentDigest}
	header := func(recordType string) map[string]any {
		return map[string]any{"schemaVersion": 1, "command": "export", "type": recordType, "disposition": "untrusted-history"}
	}
	inputs := make([]string, len(commands))
	totalBytes := 0
	for index, command := range commands {
		encoded, err := json.Marshal(map[string]string{"cmd": command})
		if err != nil {
			t.Fatalf("encode command fixture: %v", err)
		}
		inputs[index] = string(encoded)
		totalBytes += len(encoded)
	}
	first, last := any(nil), any(nil)
	if len(commands) > 0 {
		first, last = 0, len(commands)-1
	}
	records := make([]map[string]any, 0, len(commands)+1)
	sessionRecord := header("session")
	sessionRecord["snapshot"] = map[string]any{
		"session": session, "documentDigest": digest,
		"capturedAt": "2026-07-20T10:00:00.000Z", "sourceState": "present",
		"sourceObservedAt": "2026-07-20T09:59:00.000Z", "adapterVersion": "synthetic-v1",
		"freshness": "current", "lineageCoverage": "complete",
		"selection": map[string]any{
			"mode": "full", "relations": map[string]any{"selected": 0, "total": 0, "truncated": false},
			"entries":                  map[string]any{"selected": len(commands), "total": len(commands), "truncated": false, "firstOrdinal": first, "lastOrdinal": last},
			"segments":                 map[string]any{"selected": len(commands), "total": len(commands), "truncated": false},
			"segmentText":              map[string]any{"emittedUtf8Bytes": totalBytes, "originalUtf8Bytes": totalBytes, "truncated": false},
			"canonicalOmittedSegments": 0, "truncatedTextSegments": 0,
		},
	}
	records = append(records, sessionRecord)
	for index, input := range inputs {
		record := header("entry")
		record["session"], record["documentDigest"] = session, digest
		record["entry"] = map[string]any{
			"ordinal": index, "kind": "tool-call", "actor": "model", "toolCallId": fmt.Sprintf("call-%d", index),
			"toolName": "exec_command", "content": []any{fixtureTextSegment("model", input)}, "omittedSegmentCount": 0,
		}
		records = append(records, record)
	}
	writeJSONLRecords(t, path, records)
}

func commandFixtures(count, size int) []string {
	commands := make([]string, count)
	for index := range commands {
		prefix := fmt.Sprintf("%04d", index)
		commands[index] = prefix + strings.Repeat("x", size-len(prefix))
	}
	return commands
}

func writeJSONLRecords(t *testing.T, path string, records []map[string]any) {
	t.Helper()
	var output strings.Builder
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("encode fixture: %v", err)
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(output.String()), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func textDigest(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}
