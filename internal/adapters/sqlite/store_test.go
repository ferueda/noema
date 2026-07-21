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
