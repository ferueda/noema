package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
			id, fingerprint, type, subject_type, subject_id, payload_json,
			evidence_json, created_at
		) VALUES ('event-future', ?, 'analysis.completed', 'analysis',
		          'analysis-future', '{}', '[]', ?)
	`, strings.Repeat("b", 64), formatTime(now)); err != nil {
		t.Fatalf("insert future event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_schema_version, configuration_digest, payload_json, created_at
		) VALUES ('job-future', ?, 'event-future',
		          'content-scout', 'content-scout-v2', 'pending', 2, ?,
		          '{"schemaVersion":2}', ?)
	`, strings.Repeat("c", 64), digest, formatTime(now)); err != nil {
		t.Fatalf("insert future job: %v", err)
	}

	_, found, err := NewStore(database).InspectOldestPendingV1Job(ctx)
	if err != nil || found {
		t.Fatalf("inspect future-only queue = %v, %v", found, err)
	}
}

func TestPendingV1QueueClaimsExactIdentity(t *testing.T) {
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
			id, fingerprint, type, subject_type, subject_id, payload_json,
			evidence_json, created_at
		) VALUES ('event-runtime', ?, 'analysis.completed', 'analysis',
		          'analysis-runtime', '{}', '[]', ?)
	`, eventFingerprint, formatTime(now)); err != nil {
		t.Fatalf("insert queue event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_schema_version, configuration_digest, payload_json, created_at
		) VALUES ('job-runtime', ?, 'event-runtime',
		          'content-scout', 'content-scout-v1', 'pending', 1, ?,
		          '{"schemaVersion":1}', ?)
	`, digest, digest, formatTime(now)); err != nil {
		t.Fatalf("insert queue jobs: %v", err)
	}

	record, found, err := store.InspectOldestPendingV1Job(ctx)
	if err != nil || !found {
		t.Fatalf("inspect pending V1 job = %#v, %v, %v", record, found, err)
	}
	if record.ID != "job-runtime" || record.ConfigurationDigest != digest {
		t.Fatalf("inspected job = %#v", record)
	}
	identity := record.PendingV1JobIdentity
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
			id, fingerprint, type, subject_type, subject_id, payload_json,
			evidence_json, created_at
		) VALUES ('event-runtime-other', ?, 'analysis.completed', 'analysis',
		          'analysis-runtime-other', '{}', '[]', ?)
	`, strings.Repeat("e", 64), formatTime(now)); err != nil {
		t.Fatalf("insert alternate event: %v", err)
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
			id, fingerprint, type, subject_type, subject_id, payload_json,
			evidence_json, created_at
		) VALUES ('event-invalid-runtime', ?, 'analysis.completed', 'analysis',
		          'analysis-invalid-runtime', '{}', '[]', ?)
	`, strings.Repeat("c", 64), now); err != nil {
		t.Fatalf("insert invalid runtime event: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_schema_version, configuration_digest, payload_json, created_at
		) VALUES ('job-invalid-runtime', ?, 'event-invalid-runtime',
		          'content-scout', 'content-scout-v1', 'pending', 1,
		          'not-a-digest', '{}', ?)
	`, strings.Repeat("d", 64), now); err != nil {
		t.Fatalf("insert invalid runtime job: %v", err)
	}
	_, found, err := NewStore(database).InspectOldestPendingV1Job(ctx)
	if !errors.Is(err, ErrAgentRuntimeDataInvalid) || found {
		t.Fatalf("invalid V1 inspect = %v, %v", found, err)
	}
}
