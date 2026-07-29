package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestEventsCommandsInspectAndPublishOneEvent(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "noema.db")
	outputPath := filepath.Join(t.TempDir(), "events.jsonl")
	event := seedCLIEvent(t, ctx, databasePath)

	var stdout, stderr bytes.Buffer
	if err := run(ctx, []string{
		"events", "list", "--status", "pending", "--database", databasePath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("list pending events: %v; stderr: %s", err, stderr.String())
	}
	var listed []sqlitestore.EventRecord
	if err := json.Unmarshal(stdout.Bytes(), &listed); err != nil {
		t.Fatalf("decode listed events: %v", err)
	}
	if len(listed) != 1 || listed[0].Event.ID != event.ID ||
		listed[0].Outbox.Status != domain.OutboxStatusPending {
		t.Fatalf("listed events = %#v", listed)
	}

	stdout.Reset()
	stderr.Reset()
	if err := run(ctx, []string{
		"events", "show", event.ID, "--database", databasePath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("show event: %v; stderr: %s", err, stderr.String())
	}
	var shown sqlitestore.EventRecord
	if err := json.Unmarshal(stdout.Bytes(), &shown); err != nil {
		t.Fatalf("decode shown event: %v", err)
	}
	if shown.Event.ID != event.ID || shown.Outbox.AttemptCount != 0 {
		t.Fatalf("shown event = %#v", shown)
	}

	stdout.Reset()
	stderr.Reset()
	if err := run(ctx, []string{
		"events", "publish", "--once", "--output", outputPath,
		"--database", databasePath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("publish event: %v; stderr: %s", err, stderr.String())
	}
	var result application.PublicationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode publication result: %v", err)
	}
	if result.Status != application.PublicationDelivered || result.EventID != event.ID {
		t.Fatalf("publication result = %#v", result)
	}
	published, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read published JSONL: %v", err)
	}
	var publishedEvent domain.DomainEvent
	if err := json.Unmarshal(bytes.TrimSpace(published), &publishedEvent); err != nil {
		t.Fatalf("decode published event: %v", err)
	}
	if publishedEvent.ID != event.ID {
		t.Fatalf("published event = %#v", publishedEvent)
	}

	stdout.Reset()
	stderr.Reset()
	if err := run(ctx, []string{
		"events", "publish", "--once", "--output", outputPath,
		"--database", databasePath,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("repeat publication: %v; stderr: %s", err, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode repeat result: %v", err)
	}
	if result.Status != application.PublicationNoWork {
		t.Fatalf("repeat publication result = %#v", result)
	}
	published, err = os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reread published JSONL: %v", err)
	}
	if lines := bytes.Count(published, []byte("\n")); lines != 1 {
		t.Fatalf("published line count = %d, want 1", lines)
	}
}

func TestEventsPublishReportsOnlySafeFailure(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "noema.db")
	event := seedCLIEvent(t, ctx, databasePath)
	privatePath := filepath.Join(t.TempDir(), "private-secret-directory")
	if err := os.Mkdir(privatePath, 0o700); err != nil {
		t.Fatalf("create invalid output target: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := run(ctx, []string{
		"events", "publish", "--once", "--output", privatePath,
		"--database", databasePath,
	}, &stdout, &stderr)
	if err == nil || err.Error() != domain.OutboxFailureTransport ||
		strings.Contains(err.Error(), privatePath) {
		t.Fatalf("publication error = %v", err)
	}
	var result application.PublicationResult
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
		t.Fatalf("decode failed publication: %v", decodeErr)
	}
	if result.Status != application.PublicationFailed ||
		result.EventID != event.ID ||
		result.LastFailureCategory != domain.OutboxFailureTransport {
		t.Fatalf("failed publication result = %#v", result)
	}

	database, openErr := sqlitestore.Open(ctx, databasePath)
	if openErr != nil {
		t.Fatalf("reopen database: %v", openErr)
	}
	defer database.Close()
	record, found, loadErr := sqlitestore.NewStore(database).LoadEvent(ctx, event.ID)
	if loadErr != nil || !found {
		t.Fatalf("load failed publication: found=%v err=%v", found, loadErr)
	}
	if record.Outbox.Status != domain.OutboxStatusPending ||
		record.Outbox.AttemptCount != 1 ||
		record.Outbox.LastFailureCategory != domain.OutboxFailureTransport {
		t.Fatalf("failed outbox = %#v", record.Outbox)
	}
}

func TestEventsPublishRequiresExplicitOneShotAndOutput(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"events", "publish"}, want: "--once"},
		{args: []string{"events", "publish", "--once"}, want: "--output"},
	} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), test.args, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("run %v error = %v, want %s", test.args, err, test.want)
		}
	}
}

func seedCLIEvent(
	t *testing.T,
	ctx context.Context,
	databasePath string,
) domain.DomainEvent {
	t.Helper()
	event, err := domain.NewDomainEvent(
		"analysis.completed",
		domain.EventReferenceAnalysis,
		"analysis_test",
		map[string]any{"analysisId": "analysis_test", "claimIds": []string{}},
		[]domain.EventReference{},
		time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("build event: %v", err)
	}
	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	references, err := json.Marshal(event.References)
	if err != nil {
		t.Fatalf("encode references: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, schema_version, type, subject_type, subject_id,
			payload_json, references_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.Fingerprint, event.SchemaVersion, event.Type,
		event.SubjectType, event.SubjectID, string(payload), string(references),
		event.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO event_outbox (
			event_id, status, attempt_count, last_failure_category
		) VALUES (?, 'pending', 0, '')
	`, event.ID); err != nil {
		t.Fatalf("insert outbox: %v", err)
	}
	return event
}
