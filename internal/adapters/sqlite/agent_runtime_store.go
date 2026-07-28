package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

const maxAgentJobPayloadBytes = 256 * 1024

var runtimeDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var ErrAgentRuntimeDataInvalid = errors.New("agent-runtime-data-invalid")

// PendingV1JobRecord is the read-only first phase of the queue protocol.
// The sidecar declares the payload schema; callers must decode it strictly.
type PendingV1JobRecord struct {
	ID                   string
	Fingerprint          string
	EventID              string
	AgentName            string
	AgentVersion         string
	PayloadSchemaVersion int
	ConfigurationDigest  string
	PayloadJSON          []byte
	CreatedAt            time.Time
}

// PendingV1JobIdentity contains every value that must still match when a
// worker claims a previously inspected job.
type PendingV1JobIdentity struct {
	ID                   string
	Fingerprint          string
	AgentName            string
	AgentVersion         string
	PayloadSchemaVersion int
	ConfigurationDigest  string
}

// InspectOldestPendingV1Job ignores walking-skeleton rows without a V1
// sidecar. Milestone 3 does not decode or migrate those rows.
func (store *Store) InspectOldestPendingV1Job(
	ctx context.Context,
) (PendingV1JobRecord, bool, error) {
	var record PendingV1JobRecord
	var payload, createdAt string
	err := store.database.QueryRowContext(ctx, `
		SELECT jobs.id, jobs.fingerprint, jobs.event_id, jobs.agent_name,
		       jobs.agent_version, agent_job_details.payload_schema_version,
		       agent_job_details.configuration_digest, jobs.payload_json,
		       jobs.created_at
		  FROM jobs
		  JOIN agent_job_details ON agent_job_details.job_id = jobs.id
		 WHERE jobs.status = 'pending'
		   AND agent_job_details.payload_schema_version = ?
		 ORDER BY jobs.created_at, jobs.id
		 LIMIT 1
	`, domain.AgentJobPayloadSchemaVersion).Scan(
		&record.ID,
		&record.Fingerprint,
		&record.EventID,
		&record.AgentName,
		&record.AgentVersion,
		&record.PayloadSchemaVersion,
		&record.ConfigurationDigest,
		&payload,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PendingV1JobRecord{}, false, nil
	}
	if err != nil {
		return PendingV1JobRecord{}, false, err
	}
	if !validRuntimeIdentifier(record.ID) ||
		!runtimeDigestPattern.MatchString(record.Fingerprint) ||
		!validRuntimeIdentifier(record.EventID) ||
		!validRuntimeIdentifier(record.AgentName) ||
		!validRuntimeIdentifier(record.AgentVersion) ||
		record.PayloadSchemaVersion != domain.AgentJobPayloadSchemaVersion ||
		!runtimeDigestPattern.MatchString(record.ConfigurationDigest) ||
		len(payload) == 0 ||
		len(payload) > maxAgentJobPayloadBytes {
		return PendingV1JobRecord{}, false, ErrAgentRuntimeDataInvalid
	}
	record.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return PendingV1JobRecord{}, false, ErrAgentRuntimeDataInvalid
	}
	record.PayloadJSON = []byte(payload)
	return record, true, nil
}

// ClaimPendingV1Job performs the second queue phase. A lost race or changed
// identity returns claimed=false without consuming different work.
func (store *Store) ClaimPendingV1Job(
	ctx context.Context,
	expected PendingV1JobIdentity,
	startedAt time.Time,
) (bool, error) {
	if !validRuntimeIdentifier(expected.ID) ||
		!runtimeDigestPattern.MatchString(expected.Fingerprint) ||
		!validRuntimeIdentifier(expected.AgentName) ||
		!validRuntimeIdentifier(expected.AgentVersion) ||
		expected.PayloadSchemaVersion != domain.AgentJobPayloadSchemaVersion ||
		!runtimeDigestPattern.MatchString(expected.ConfigurationDigest) ||
		startedAt.IsZero() {
		return false, ErrAgentRuntimeDataInvalid
	}
	result, err := store.database.ExecContext(ctx, `
		UPDATE jobs
		   SET status = 'running', started_at = ?
		 WHERE id = ?
		   AND fingerprint = ?
		   AND agent_name = ?
		   AND agent_version = ?
		   AND status = 'pending'
		   AND EXISTS (
		       SELECT 1
		         FROM agent_job_details
		        WHERE agent_job_details.job_id = jobs.id
		          AND agent_job_details.payload_schema_version = ?
		          AND agent_job_details.configuration_digest = ?
		   )
	`,
		formatTime(startedAt),
		expected.ID,
		expected.Fingerprint,
		expected.AgentName,
		expected.AgentVersion,
		expected.PayloadSchemaVersion,
		expected.ConfigurationDigest,
	)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return updated == 1, nil
}

func validRuntimeIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 256
}
