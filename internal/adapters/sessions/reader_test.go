package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const testCanonicalID = "synthetic@local:session-one"

type fakeRunner struct {
	output     []byte
	err        error
	executable string
	args       []string
}

func (runner *fakeRunner) Run(_ context.Context, executable string, args ...string) ([]byte, error) {
	runner.executable = executable
	runner.args = append([]string(nil), args...)
	return runner.output, runner.err
}

func TestReaderAdmitsFullSchemaOneExport(t *testing.T) {
	runner := &fakeRunner{output: validExport(t)}
	document, err := (Reader{Executable: "/tmp/fake-sessions", Runner: runner}).Read(context.Background(), testCanonicalID)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if runner.executable != "/tmp/fake-sessions" {
		t.Fatalf("executable = %q", runner.executable)
	}
	wantArgs := []string{"export", testCanonicalID, "--format", "jsonl", "--full"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", runner.args, wantArgs)
	}
	if document.Revision.CanonicalID != testCanonicalID || len(document.Entries) != 2 {
		t.Fatalf("document identity/count = %q/%d", document.Revision.CanonicalID, len(document.Entries))
	}
	if document.Selection.Coverage != "complete-retained-snapshot" {
		t.Fatalf("coverage = %q", document.Selection.Coverage)
	}
}

func TestReaderRejectsContractDrift(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]map[string]any)
	}{
		{name: "unsupported schema", mutate: func(records []map[string]any) { records[0]["schemaVersion"] = 2 }},
		{name: "wrong disposition", mutate: func(records []map[string]any) { records[0]["disposition"] = "trusted" }},
		{name: "digest drift", mutate: func(records []map[string]any) {
			records[1]["documentDigest"] = map[string]any{"scheme": documentDigestScheme, "digest": strings.Repeat("e", 64)}
		}},
		{name: "truncated text", mutate: func(records []map[string]any) {
			snapshot := records[0]["snapshot"].(map[string]any)
			snapshot["selection"].(map[string]any)["truncatedTextSegments"] = 1
		}},
		{name: "missing required count", mutate: func(records []map[string]any) {
			snapshot := records[0]["snapshot"].(map[string]any)
			delete(snapshot["selection"].(map[string]any), "canonicalOmittedSegments")
		}},
		{name: "noncanonical timestamp", mutate: func(records []map[string]any) {
			records[0]["snapshot"].(map[string]any)["capturedAt"] = "2026-07-20T10:00:00Z"
		}},
		{name: "null text member", mutate: func(records []map[string]any) {
			entry := records[1]["entry"].(map[string]any)
			segment := entry["content"].([]any)[0].(map[string]any)
			segment["text"].(map[string]any)["text"] = nil
		}},
		{name: "null selection count", mutate: func(records []map[string]any) {
			selection := records[0]["snapshot"].(map[string]any)["selection"].(map[string]any)
			selection["segments"].(map[string]any)["selected"] = nil
		}},
		{name: "null optional timestamp", mutate: func(records []map[string]any) {
			records[1]["entry"].(map[string]any)["timestamp"] = nil
		}},
		{name: "null optional linkage", mutate: func(records []map[string]any) {
			records[2]["entry"].(map[string]any)["relatedEntryOrdinal"] = nil
		}},
		{name: "null optional tool metadata", mutate: func(records []map[string]any) {
			records[1]["entry"].(map[string]any)["toolName"] = nil
		}},
		{name: "unknown field", mutate: func(records []map[string]any) { records[0]["surprise"] = true }},
		{name: "bad entry ordinal", mutate: func(records []map[string]any) {
			records[1]["entry"].(map[string]any)["ordinal"] = 4
		}},
		{name: "bad content hash", mutate: func(records []map[string]any) {
			entry := records[1]["entry"].(map[string]any)
			segment := entry["content"].([]any)[0].(map[string]any)
			segment["contentHash"].(map[string]any)["digest"] = strings.Repeat("a", 64)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			records := validRecords()
			test.mutate(records)
			runner := &fakeRunner{output: encodeRecords(t, records)}
			_, err := (Reader{Runner: runner}).Read(context.Background(), testCanonicalID)
			if err == nil {
				t.Fatal("read succeeded, want contract failure")
			}
		})
	}
}

func TestReaderAdmitsCanonicalOmissionAndEmptySnapshot(t *testing.T) {
	t.Run("canonical omission", func(t *testing.T) {
		records := validRecords()
		entry := records[1]["entry"].(map[string]any)
		entry["content"] = []any{map[string]any{
			"ordinal": 0, "kind": "omitted", "origin": "tool", "originConfidence": "high",
			"contentClass": "structured", "sourceType": "application/json",
		}}
		selection := records[0]["snapshot"].(map[string]any)["selection"].(map[string]any)
		selection["canonicalOmittedSegments"] = 1
		selection["segmentText"].(map[string]any)["emittedUtf8Bytes"] = len("Process exited with code 0\n1 passed\n")
		selection["segmentText"].(map[string]any)["originalUtf8Bytes"] = len("Process exited with code 0\n1 passed\n")
		runner := &fakeRunner{output: encodeRecords(t, records)}
		document, err := (Reader{Runner: runner}).Read(context.Background(), testCanonicalID)
		if err != nil {
			t.Fatalf("read omitted segment: %v", err)
		}
		if document.Selection.CanonicalOmittedSegments != 1 {
			t.Fatalf("canonical omissions = %d", document.Selection.CanonicalOmittedSegments)
		}
	})

	t.Run("empty", func(t *testing.T) {
		records := validRecords()[:1]
		selection := records[0]["snapshot"].(map[string]any)["selection"].(map[string]any)
		selection["entries"] = map[string]any{"selected": 0, "total": 0, "truncated": false, "firstOrdinal": nil, "lastOrdinal": nil}
		selection["segments"] = map[string]any{"selected": 0, "total": 0, "truncated": false}
		selection["segmentText"] = map[string]any{"emittedUtf8Bytes": 0, "originalUtf8Bytes": 0, "truncated": false}
		runner := &fakeRunner{output: encodeRecords(t, records)}
		document, err := (Reader{Runner: runner}).Read(context.Background(), testCanonicalID)
		if err != nil || len(document.Entries) != 0 {
			t.Fatalf("empty export = %#v, %v", document, err)
		}
	})
}

func TestReaderPreservesOptionalToolMetadataWithoutKindAssumptions(t *testing.T) {
	records := validRecords()
	result := records[2]["entry"].(map[string]any)
	result["toolName"] = "reported-result-tool"
	result["toolNamespace"] = "synthetic"
	document, err := (Reader{Runner: &fakeRunner{output: encodeRecords(t, records)}}).Read(context.Background(), testCanonicalID)
	if err != nil {
		t.Fatalf("read optional tool metadata: %v", err)
	}
	if document.Entries[1].ToolName != "reported-result-tool" || document.Entries[1].ToolNamespace != "synthetic" {
		t.Fatalf("tool metadata = %q/%q", document.Entries[1].ToolName, document.Entries[1].ToolNamespace)
	}
}

func TestReaderSanitizesCommandFailure(t *testing.T) {
	runner := &fakeRunner{err: errors.New("secret transcript output")}
	_, err := (Reader{Runner: runner}).Read(context.Background(), testCanonicalID)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v, want sanitized failure", err)
	}
}

func validExport(t *testing.T) []byte {
	t.Helper()
	return encodeRecords(t, validRecords())
}

func validRecords() []map[string]any {
	digest := map[string]any{"scheme": documentDigestScheme, "digest": strings.Repeat("d", 64)}
	session := map[string]any{
		"canonicalId": testCanonicalID,
		"source":      map[string]any{"kind": "synthetic", "instanceId": "local"},
		"nativeId":    "session-one",
	}
	command := `{"cmd":"go test ./..."}`
	result := "Process exited with code 0\n1 passed\n"
	selection := map[string]any{
		"mode":                     "full",
		"relations":                map[string]any{"selected": 0, "total": 0, "truncated": false},
		"entries":                  map[string]any{"selected": 2, "total": 2, "truncated": false, "firstOrdinal": 0, "lastOrdinal": 1},
		"segments":                 map[string]any{"selected": 2, "total": 2, "truncated": false},
		"segmentText":              map[string]any{"emittedUtf8Bytes": len(command) + len(result), "originalUtf8Bytes": len(command) + len(result), "truncated": false},
		"canonicalOmittedSegments": 0, "truncatedTextSegments": 0,
	}
	header := func(recordType string) map[string]any {
		return map[string]any{"schemaVersion": 1, "command": "export", "type": recordType, "disposition": "untrusted-history"}
	}
	sessionRecord := header("session")
	sessionRecord["snapshot"] = map[string]any{
		"session": session, "documentDigest": digest,
		"capturedAt": "2026-07-20T10:00:00.000Z", "sourceState": "present",
		"sourceObservedAt": "2026-07-20T09:59:00.000Z", "adapterVersion": "synthetic-v1",
		"freshness": "current", "lineageCoverage": "unknown", "selection": selection,
	}
	call := header("entry")
	call["session"], call["documentDigest"] = session, digest
	call["entry"] = map[string]any{
		"ordinal": 0, "kind": "tool-call", "actor": "model", "toolCallId": "call-1",
		"toolName": "exec_command", "content": []any{textSegment(0, "model", command)}, "omittedSegmentCount": 0,
	}
	toolResult := header("entry")
	toolResult["session"], toolResult["documentDigest"] = session, digest
	toolResult["entry"] = map[string]any{
		"ordinal": 1, "kind": "tool-result", "actor": "tool", "relatedEntryOrdinal": 0,
		"toolCallId": "call-1",
		"content":    []any{textSegment(0, "tool", result)}, "omittedSegmentCount": 0,
	}
	return []map[string]any{sessionRecord, call, toolResult}
}

func textSegment(ordinal int, origin, text string) map[string]any {
	hash := sha256.Sum256([]byte(text))
	return map[string]any{
		"ordinal": ordinal, "kind": "text", "origin": origin, "originConfidence": "high",
		"text":        map[string]any{"text": text, "truncated": false, "originalUtf8Bytes": len(text), "emittedUtf8Bytes": len(text)},
		"contentHash": map[string]any{"scheme": contentDigestScheme, "digest": hex.EncodeToString(hash[:])},
	}
}

func encodeRecords(t *testing.T, records []map[string]any) []byte {
	t.Helper()
	var output strings.Builder
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("encode record: %v", err)
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	return []byte(output.String())
}
