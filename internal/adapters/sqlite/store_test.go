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

func TestOpenRejectsPreV1Database(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open pre-V1 database: %v", err)
	}
	if _, err := database.ExecContext(ctx, "CREATE TABLE scans (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("create pre-V1 table: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close pre-V1 database: %v", err)
	}

	if _, err := Open(ctx, path); err == nil ||
		!strings.Contains(err.Error(), "database schema is incompatible with V1") {
		t.Fatalf("open pre-V1 database error = %v", err)
	}
	database, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen rejected database: %v", err)
	}
	defer database.Close()
	var eventsTableCount int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'events'",
	).Scan(&eventsTableCount); err != nil {
		t.Fatalf("inspect rejected database: %v", err)
	}
	if eventsTableCount != 0 {
		t.Fatal("schema migration ran before rejecting pre-V1 database")
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
