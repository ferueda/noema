package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestFactAnalysisMigrationPreservesFoundationData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	initial, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(initial)); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO scans (
			id, fingerprint, knowledge_fingerprint, source_kind, after_time,
			before_time, content_scope, coverage, status, skipped_count,
			observation_ids_json, job_id, created_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "scan-one", "scan-fingerprint", "knowledge-fingerprint", "synthetic",
		formatTime(now.Add(-time.Hour)), formatTime(now), "generic", "complete", "completed",
		0, "[]", "", formatTime(now), formatTime(now)); err != nil {
		t.Fatalf("insert legacy scan: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	database, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	if _, found, err := store.FindCompletedScan(ctx, "scan-fingerprint"); err != nil || !found {
		t.Fatalf("foundation scan after reopen = %v, %v", found, err)
	}
	for _, table := range []string{"analysis_runs", "facts"} {
		var found string
		if err := database.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&found); err != nil {
			t.Fatalf("find table %s: %v", table, err)
		}
	}
}

func TestSemanticMigrationBackfillsFoundationEventSubjectsWithoutRewritingEvents(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	for _, name := range []string{"001_initial.sql", "002_fact_analysis.sql"} {
		migration, readErr := migrationFiles.ReadFile("migrations/" + name)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", name, readErr)
		}
		if _, execErr := database.ExecContext(ctx, string(migration)); execErr != nil {
			t.Fatalf("apply migration %s: %v", name, execErr)
		}
	}
	now := formatTime(time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC))
	if _, err := database.ExecContext(ctx, `
		INSERT INTO scans (
			id, fingerprint, knowledge_fingerprint, source_kind, after_time,
			before_time, content_scope, coverage, status, skipped_count,
			observation_ids_json, job_id, created_at, finished_at
		) VALUES ('scan-one', 'scan-fp', 'knowledge-fp', 'synthetic', ?, ?,
		          'private', 'complete', 'completed', 0, '["observation-one"]',
		          'job-one', ?, ?);
		INSERT INTO observations (
			id, scan_id, fingerprint, distillation_key, kind, summary,
			confidence, evidence_json, created_at
		) VALUES ('observation-one', 'scan-one', 'observation-fp', 'distill-v0',
		          'lesson', 'A generic lesson.', 0.8, '[]', ?);
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES
			('event-observation', 'event-observation-fp', 'observation.created',
			 'observation-one', '{}', '[]', ?),
			('event-scan', 'event-scan-fp', 'scan.completed',
			 'scan-one', '{}', '[]', ?);
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES ('job-one', 'job-fp', 'event-scan', 'content-scout', 'v0',
		          'pending', '{}', ?)
	`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("insert milestone 1 fixture: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	database, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	defer database.Close()
	rows, err := database.QueryContext(ctx, `
		SELECT events.id, events.fingerprint, event_subject_types.subject_type
		  FROM events
		  JOIN event_subject_types ON event_subject_types.event_id = events.id
		 ORDER BY events.id
	`)
	if err != nil {
		t.Fatalf("query migrated events: %v", err)
	}
	defer rows.Close()
	got := make([]string, 0, 2)
	for rows.Next() {
		var id, fingerprint, subjectType string
		if err := rows.Scan(&id, &fingerprint, &subjectType); err != nil {
			t.Fatalf("scan migrated event: %v", err)
		}
		got = append(got, id+":"+fingerprint+":"+subjectType)
	}
	want := []string{
		"event-observation:event-observation-fp:observation",
		"event-scan:event-scan-fp:scan",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("migrated events = %v, want %v", got, want)
	}
	var eventID string
	if err := database.QueryRowContext(ctx, "SELECT event_id FROM jobs WHERE id = 'job-one'").Scan(&eventID); err != nil || eventID != "event-scan" {
		t.Fatalf("job event after migration = %q, %v", eventID, err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close migrated database: %v", err)
	}
	database, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("replay semantic migration: %v", err)
	}
	database.Close()
}

func TestSemanticMigrationRejectsUnknownExistingEventType(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	initial, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(initial)); err != nil {
		t.Fatalf("apply legacy schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('event-unknown', 'event-unknown-fp', 'unknown.event',
		          'subject-one', '{}', '[]', '2026-07-21T10:00:00Z')
	`); err != nil {
		t.Fatalf("insert unknown event: %v", err)
	}
	database.Close()

	_, err = Open(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "unsupported type unknown.event") {
		t.Fatalf("migration error = %v, want unsupported event type", err)
	}
}

func TestStoreCommitsAndReusesFactAnalysisAtomically(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	revision := domain.EvidenceRevision{
		SourceKind: domain.EvidenceSourceSessions, CanonicalID: "synthetic@local:one",
		DocumentDigest: domain.Digest{Scheme: "sha256-sessions-document-jcs-v1", Digest: strings.Repeat("d", 64)},
	}
	selection := domain.EvidenceSelection{Mode: "full", Coverage: domain.CoverageCompleteRetainedSnapshot}
	fact := domain.Fact{
		ID: "fact-one", Fingerprint: "fact-fingerprint", AnalysisRunID: "analysis-one",
		Kind: "tool-call", SchemaVersion: 1, Value: domain.FactValue{Tool: &domain.ToolFactValue{Kind: "call"}},
		Outcome: domain.FactOutcomeNotApplicable, ExtractorName: "test", ExtractorVersion: "1",
		ParseRule: "test-rule", Evidence: []domain.EvidenceRef{{
			ID: "evidence-one", SourceKind: domain.EvidenceSourceSessions,
			SourceIdentity: revision.CanonicalID, DocumentDigestScheme: revision.DocumentDigest.Scheme,
			DocumentDigest: revision.DocumentDigest.Digest,
		}}, CreatedAt: now,
	}
	analysis := domain.FactAnalysis{
		Run: domain.AnalysisRun{
			ID: "analysis-one", ProcessingKey: "processing-key", Stage: domain.AnalysisStageFacts,
			RequestedSourceIdentity: revision.CanonicalID, Revision: &revision, Selection: &selection,
			ExtractorName: "test", ExtractorVersion: "1", SchemaVersion: 1,
			FactIDs: []string{fact.ID}, Status: domain.AnalysisCompleted, StartedAt: now, FinishedAt: now,
		},
		Facts: []domain.Fact{fact},
	}
	if inserted, err := store.CommitFactAnalysis(ctx, analysis); err != nil || !inserted {
		t.Fatalf("commit analysis = %v, %v", inserted, err)
	}
	if inserted, err := store.CommitFactAnalysis(ctx, analysis); err != nil || inserted {
		t.Fatalf("duplicate analysis = %v, %v", inserted, err)
	}
	loaded, found, err := store.FindCompletedFactAnalysis(ctx, analysis.Run.ProcessingKey)
	if err != nil || !found || len(loaded.Facts) != 1 || loaded.Facts[0].ID != fact.ID {
		t.Fatalf("loaded analysis = %#v, %v, %v", loaded, found, err)
	}
	var factCount int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM facts").Scan(&factCount); err != nil || factCount != 1 {
		t.Fatalf("fact count = %d, %v", factCount, err)
	}
}

func TestStoreRecordsInspectableFailureWithoutRevision(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	run := domain.AnalysisRun{
		ID: "failed-one", Stage: domain.AnalysisStageFacts, RequestedSourceIdentity: "synthetic@local:missing",
		ExtractorName: "test", ExtractorVersion: "1", SchemaVersion: 1, FactIDs: []string{},
		Status: domain.AnalysisFailed, Error: "source-evidence-invalid", StartedAt: now, FinishedAt: now,
	}
	if err := store.RecordFailedAnalysis(ctx, run); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	loaded, err := store.LoadFactAnalysis(ctx, run.ID)
	if err != nil || loaded.Run.Revision != nil || loaded.Run.Selection != nil || loaded.Run.Error != run.Error {
		t.Fatalf("loaded failure = %#v, %v", loaded, err)
	}
}
