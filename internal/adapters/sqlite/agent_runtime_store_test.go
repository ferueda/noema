package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentRuntimeMigrationReplaysWithoutBackfillingFoundationJobs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open foundation database: %v", err)
	}
	for _, name := range []string{
		"001_initial.sql",
		"002_fact_analysis.sql",
		"003_semantic_claims.sql",
	} {
		migration, readErr := migrationFiles.ReadFile("migrations/" + name)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", name, readErr)
		}
		if _, execErr := database.ExecContext(ctx, string(migration)); execErr != nil {
			t.Fatalf("apply migration %s: %v", name, execErr)
		}
	}
	now := "2026-07-28T10:00:00Z"
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('event-old', 'event-old-fingerprint', 'scan.completed',
		          'scan-old', '{}', '[]', ?);
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES ('event-old', 'scan');
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES ('job-old', 'job-old-fingerprint', 'event-old',
		          'content-scout', 'v0', 'pending', '{}', ?)
	`, now, now); err != nil {
		t.Fatalf("insert foundation runtime row: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close foundation database: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		database, err = Open(ctx, path)
		if err != nil {
			t.Fatalf("open migrated database attempt %d: %v", attempt, err)
		}
		var details int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_job_details").Scan(&details); err != nil {
			t.Fatalf("count runtime details: %v", err)
		}
		if details != 0 {
			t.Fatalf("runtime details = %d, want no foundation backfill", details)
		}
		for _, table := range []string{"agent_job_details", "artifacts"} {
			var found string
			if err := database.QueryRowContext(
				ctx,
				"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
				table,
			).Scan(&found); err != nil {
				t.Fatalf("find table %s: %v", table, err)
			}
		}
		for _, column := range []string{"job_fingerprint", "claim_ids_json"} {
			var found string
			if err := database.QueryRowContext(
				ctx,
				"SELECT name FROM pragma_table_info('artifacts') WHERE name = ?",
				column,
			).Scan(&found); err != nil {
				t.Fatalf("find artifact column %s: %v", column, err)
			}
		}
		if err := database.Close(); err != nil {
			t.Fatalf("close migrated database: %v", err)
		}
	}
}

func TestPendingV1QueueIgnoresFuturePayloadVersions(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('event-future', ?, 'analysis.completed',
		          'analysis-future', '{}', '[]', ?)
	`, strings.Repeat("b", 64), formatTime(now)); err != nil {
		t.Fatalf("insert future event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES ('event-future', 'analysis')
	`); err != nil {
		t.Fatalf("insert future event subject: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES ('job-future', ?, 'event-future',
		          'content-scout', 'content-scout-v2', 'pending',
		          '{"schemaVersion":2}', ?)
	`, strings.Repeat("c", 64), formatTime(now)); err != nil {
		t.Fatalf("insert future job: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_job_details (
			job_id, payload_schema_version, configuration_digest
		) VALUES ('job-future', 2, ?)
	`, digest); err != nil {
		t.Fatalf("insert future job details: %v", err)
	}

	_, found, err := NewStore(database).InspectOldestPendingV1Job(ctx)
	if err != nil || found {
		t.Fatalf("inspect future-only queue = %v, %v", found, err)
	}
}

func TestPendingV1QueueIgnoresFoundationRowsAndClaimsExactIdentity(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	eventFingerprint := strings.Repeat("b", 64)
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('event-runtime', ?, 'analysis.completed',
		          'analysis-runtime', '{}', '[]', ?)
	`, eventFingerprint, formatTime(now)); err != nil {
		t.Fatalf("insert queue event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES ('event-runtime', 'analysis')
	`); err != nil {
		t.Fatalf("insert queue event subject: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES
			('job-foundation', 'foundation-fingerprint', 'event-runtime',
			 'content-scout', 'v0', 'pending', '{}', ?),
			('job-runtime', ?, 'event-runtime',
			 'content-scout', 'content-scout-v1', 'pending', '{"schemaVersion":1}', ?)
	`, formatTime(now.Add(-time.Minute)), digest, formatTime(now)); err != nil {
		t.Fatalf("insert queue jobs: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_job_details (
			job_id, payload_schema_version, configuration_digest
		) VALUES ('job-runtime', 1, ?)
	`, digest); err != nil {
		t.Fatalf("insert queue job details: %v", err)
	}

	record, found, err := store.InspectOldestPendingV1Job(ctx)
	if err != nil || !found {
		t.Fatalf("inspect pending V1 job = %#v, %v, %v", record, found, err)
	}
	if record.ID != "job-runtime" || record.ConfigurationDigest != digest {
		t.Fatalf("inspected job = %#v", record)
	}
	identity := PendingV1JobIdentity{
		ID: record.ID, Fingerprint: record.Fingerprint,
		EventID:   record.EventID,
		AgentName: record.AgentName, AgentVersion: record.AgentVersion,
		PayloadSchemaVersion: record.PayloadSchemaVersion,
		ConfigurationDigest:  record.ConfigurationDigest,
		PayloadJSON:          record.PayloadJSON,
	}
	if _, err := database.ExecContext(
		ctx,
		"UPDATE jobs SET payload_json = '{\"schemaVersion\":1,\"changed\":true}' WHERE id = ?",
		record.ID,
	); err != nil {
		t.Fatalf("change inspected payload: %v", err)
	}
	if claimed, err := store.ClaimPendingV1Job(ctx, identity, now.Add(time.Minute)); err != nil || claimed {
		t.Fatalf("claim changed payload = %v, %v", claimed, err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('event-runtime-other', ?, 'analysis.completed',
		          'analysis-runtime-other', '{}', '[]', ?)
	`, strings.Repeat("e", 64), formatTime(now)); err != nil {
		t.Fatalf("insert alternate event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES ('event-runtime-other', 'analysis')
	`); err != nil {
		t.Fatalf("insert alternate event subject: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		UPDATE jobs SET payload_json = ?, event_id = 'event-runtime-other'
		 WHERE id = ?
	`, string(record.PayloadJSON), record.ID); err != nil {
		t.Fatalf("change inspected event: %v", err)
	}
	if claimed, err := store.ClaimPendingV1Job(ctx, identity, now.Add(2*time.Minute)); err != nil || claimed {
		t.Fatalf("claim changed event = %v, %v", claimed, err)
	}
	if _, err := database.ExecContext(
		ctx,
		"UPDATE jobs SET event_id = ? WHERE id = ?",
		record.EventID,
		record.ID,
	); err != nil {
		t.Fatalf("restore inspected event: %v", err)
	}
	claimed, err := store.ClaimPendingV1Job(ctx, identity, now.Add(3*time.Minute))
	if err != nil || !claimed {
		t.Fatalf("claim exact V1 job = %v, %v", claimed, err)
	}
	if claimed, err := store.ClaimPendingV1Job(ctx, identity, now.Add(4*time.Minute)); err != nil || claimed {
		t.Fatalf("repeat claim = %v, %v", claimed, err)
	}
	var foundationStatus string
	if err := database.QueryRowContext(
		ctx,
		"SELECT status FROM jobs WHERE id = 'job-foundation'",
	).Scan(&foundationStatus); err != nil || foundationStatus != "pending" {
		t.Fatalf("foundation job status = %q, %v", foundationStatus, err)
	}
}

func TestPendingV1QueueRejectsInvalidDeclaredRuntimeData(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	now := formatTime(time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC))
	if _, err := database.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json, created_at
		) VALUES ('event-invalid-runtime', ?, 'analysis.completed',
		          'analysis-invalid-runtime', '{}', '[]', ?)
	`, strings.Repeat("c", 64), now); err != nil {
		t.Fatalf("insert invalid runtime event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES ('event-invalid-runtime', 'analysis')
	`); err != nil {
		t.Fatalf("insert invalid runtime event subject: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_json, created_at
		) VALUES ('job-invalid-runtime', ?, 'event-invalid-runtime',
		          'content-scout', 'content-scout-v1', 'pending', '{}', ?)
	`, strings.Repeat("d", 64), now); err != nil {
		t.Fatalf("insert invalid runtime job: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO agent_job_details (
			job_id, payload_schema_version, configuration_digest
		) VALUES ('job-invalid-runtime', 1, 'not-a-digest')
	`); err != nil {
		t.Fatalf("insert invalid runtime details: %v", err)
	}
	_, found, err := NewStore(database).InspectOldestPendingV1Job(ctx)
	if !errors.Is(err, ErrAgentRuntimeDataInvalid) || found {
		t.Fatalf("invalid V1 inspect = %v, %v", found, err)
	}
}
