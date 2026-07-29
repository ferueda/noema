package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

// CreateOrReuseV1Job atomically persists the complete V1 job record.
// An exact fingerprint reuses the existing durable job at any lifecycle stage.
func (store *Store) CreateOrReuseV1Job(
	ctx context.Context,
	job application.AgentJobRecordV1,
) (bool, error) {
	if job.Status != domain.JobPending ||
		application.ValidateAgentJobRecordV1(job) != nil {
		return false, ErrAgentRuntimeDataInvalid
	}
	payload, err := json.Marshal(job.Payload)
	if err != nil || len(payload) == 0 || len(payload) > maxAgentJobPayloadBytes {
		return false, ErrAgentRuntimeDataInvalid
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin V1 job transaction: %w", err)
	}
	defer transaction.Rollback()

	result, err := transaction.ExecContext(ctx, `
		INSERT INTO jobs (
			id, fingerprint, event_id, agent_name, agent_version, status,
			payload_schema_version, configuration_digest, payload_json, error,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?)
		ON CONFLICT(fingerprint) DO NOTHING
	`,
		job.ID,
		job.Fingerprint,
		job.EventID,
		job.Agent.Name,
		job.Agent.Version,
		job.Status,
		job.Payload.SchemaVersion,
		job.Payload.Configuration.Digest,
		string(payload),
		formatTime(job.CreatedAt),
	)
	if err != nil {
		return false, fmt.Errorf("insert V1 job: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check V1 job insertion: %w", err)
	}
	if inserted == 1 {
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf("commit V1 job: %w", err)
		}
		return true, nil
	}
	if inserted != 0 {
		return false, ErrAgentRuntimeDataInvalid
	}

	existing, err := loadV1JobByFingerprint(ctx, transaction, job.Fingerprint)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrAgentRuntimeDataInvalid
		}
		return false, err
	}
	if existing.ID != job.ID ||
		existing.EventID != job.EventID ||
		existing.Agent != job.Agent ||
		!reflect.DeepEqual(existing.Payload, job.Payload) {
		return false, ErrAgentRuntimeDataInvalid
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit reused V1 job: %w", err)
	}
	return false, nil
}

// ListV1Jobs returns jobs using the supported payload version.
func (store *Store) ListV1Jobs(
	ctx context.Context,
) ([]application.AgentJobRecordV1, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT jobs.id, jobs.fingerprint, jobs.event_id, jobs.agent_name,
		       jobs.agent_version, jobs.status, jobs.payload_json,
		       jobs.created_at, jobs.started_at, jobs.finished_at,
		       jobs.configuration_digest
		  FROM jobs
		 WHERE jobs.payload_schema_version = ?
		 ORDER BY jobs.created_at, jobs.id
	`, domain.AgentJobPayloadSchemaVersion)
	if err != nil {
		return nil, fmt.Errorf("list V1 jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]application.AgentJobRecordV1, 0)
	for rows.Next() {
		job, err := readV1Job(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate V1 jobs: %w", err)
	}
	return jobs, nil
}

// LoadV1Job ignores unsupported payload versions.
func (store *Store) LoadV1Job(
	ctx context.Context,
	jobID string,
) (application.AgentJobRecordV1, bool, error) {
	if !validRuntimeIdentifier(jobID) {
		return application.AgentJobRecordV1{}, false, ErrAgentRuntimeDataInvalid
	}
	job, err := readV1Job(store.database.QueryRowContext(ctx, `
		SELECT jobs.id, jobs.fingerprint, jobs.event_id, jobs.agent_name,
		       jobs.agent_version, jobs.status, jobs.payload_json,
		       jobs.created_at, jobs.started_at, jobs.finished_at,
		       jobs.configuration_digest
		  FROM jobs
		 WHERE jobs.id = ?
		   AND jobs.payload_schema_version = ?
	`, jobID, domain.AgentJobPayloadSchemaVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return application.AgentJobRecordV1{}, false, nil
	}
	if err != nil {
		return application.AgentJobRecordV1{}, false, err
	}
	return job, true, nil
}

func loadV1JobByFingerprint(
	ctx context.Context,
	queryer *sql.Tx,
	fingerprint string,
) (application.AgentJobRecordV1, error) {
	return readV1Job(queryer.QueryRowContext(ctx, `
		SELECT jobs.id, jobs.fingerprint, jobs.event_id, jobs.agent_name,
		       jobs.agent_version, jobs.status, jobs.payload_json,
		       jobs.created_at, jobs.started_at, jobs.finished_at,
		       jobs.configuration_digest
		  FROM jobs
		 WHERE jobs.fingerprint = ?
		   AND jobs.payload_schema_version = ?
	`, fingerprint, domain.AgentJobPayloadSchemaVersion))
}

func readV1Job(row rowScanner) (application.AgentJobRecordV1, error) {
	var job application.AgentJobRecordV1
	var agentName, agentVersion, payloadJSON, createdAt, configurationDigest string
	var startedAt, finishedAt sql.NullString
	if err := row.Scan(
		&job.ID,
		&job.Fingerprint,
		&job.EventID,
		&agentName,
		&agentVersion,
		&job.Status,
		&payloadJSON,
		&createdAt,
		&startedAt,
		&finishedAt,
		&configurationDigest,
	); err != nil {
		return application.AgentJobRecordV1{}, err
	}
	if !runtimeDigestPattern.MatchString(configurationDigest) ||
		len(payloadJSON) == 0 || len(payloadJSON) > maxAgentJobPayloadBytes {
		return application.AgentJobRecordV1{}, ErrAgentRuntimeDataInvalid
	}
	job.Agent = domain.AgentIdentity{Name: agentName, Version: agentVersion}
	if err := decodeStrictV1JobPayload(payloadJSON, &job.Payload); err != nil ||
		job.Payload.Configuration.Digest != configurationDigest {
		return application.AgentJobRecordV1{}, ErrAgentRuntimeDataInvalid
	}
	var err error
	job.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return application.AgentJobRecordV1{}, ErrAgentRuntimeDataInvalid
	}
	job.StartedAt, err = parseNullableTime(startedAt)
	if err != nil {
		return application.AgentJobRecordV1{}, ErrAgentRuntimeDataInvalid
	}
	job.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil || application.ValidateAgentJobRecordV1(job) != nil {
		return application.AgentJobRecordV1{}, ErrAgentRuntimeDataInvalid
	}
	return job, nil
}

func decodeStrictV1JobPayload(
	value string,
	target *domain.AgentJobPayloadV1,
) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(value)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("V1 job payload has trailing data")
	}
	return target.Validate()
}
