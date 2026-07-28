package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

// CompleteV1Job atomically stores one successful run and its ordered generic
// artifacts, then completes the exact running job. An exact repeat is reused.
func (store *Store) CompleteV1Job(
	ctx context.Context,
	completion application.V1JobCompletion,
) (bool, error) {
	if application.ValidateV1JobCompletion(completion) != nil {
		return false, ErrAgentRuntimeDataInvalid
	}
	return store.finishV1Job(ctx, completion.Job, completion.Run, completion.Artifacts)
}

// FailV1Job atomically stores one safe failed result and fails the exact
// running job. Failed V1 runs never create artifacts.
func (store *Store) FailV1Job(
	ctx context.Context,
	job application.AgentJobRecordV1,
	run application.V1AgentRunRecord,
) (bool, error) {
	if application.ValidateV1JobFailure(job, run) != nil {
		return false, ErrAgentRuntimeDataInvalid
	}
	return store.finishV1Job(ctx, job, run, nil)
}

func (store *Store) finishV1Job(
	ctx context.Context,
	expected application.AgentJobRecordV1,
	run application.V1AgentRunRecord,
	artifacts []domain.Artifact,
) (bool, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin V1 completion transaction: %w", err)
	}
	defer transaction.Rollback()

	stored, err := loadV1JobByID(ctx, transaction, expected.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrAgentRuntimeDataInvalid
		}
		return false, err
	}
	if stored.Status != domain.JobRunning {
		reused, err := exactTerminalV1Result(ctx, transaction, stored, expected, run, artifacts)
		if err != nil || !reused {
			return false, err
		}
		if err := transaction.Commit(); err != nil {
			return false, fmt.Errorf("commit reused V1 completion: %w", err)
		}
		return false, nil
	}
	if !sameRunningV1Job(stored, expected) {
		return false, ErrAgentRuntimeDataInvalid
	}
	if err := insertV1Run(ctx, transaction, run); err != nil {
		return false, err
	}
	for _, artifact := range artifacts {
		if err := insertV1Artifact(ctx, transaction, artifact); err != nil {
			return false, err
		}
	}

	failureCategory := ""
	if run.Result.Failure != nil {
		failureCategory = run.Result.Failure.Category
	}
	payloadJSON, err := json.Marshal(expected.Payload)
	if err != nil || len(payloadJSON) == 0 || len(payloadJSON) > maxAgentJobPayloadBytes {
		return false, ErrAgentRuntimeDataInvalid
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE jobs
		   SET status = ?, finished_at = ?, error = ?
		 WHERE id = ?
		   AND fingerprint = ?
		   AND event_id = ?
		   AND agent_name = ?
		   AND agent_version = ?
		   AND payload_json = ?
		   AND status = 'running'
		   AND started_at = ?
		   AND EXISTS (
		       SELECT 1
		         FROM agent_job_details
		        WHERE agent_job_details.job_id = jobs.id
		          AND agent_job_details.payload_schema_version = ?
		          AND agent_job_details.configuration_digest = ?
		   )
	`,
		run.Result.Outcome,
		formatTime(run.FinishedAt),
		failureCategory,
		expected.ID,
		expected.Fingerprint,
		expected.EventID,
		expected.Agent.Name,
		expected.Agent.Version,
		string(payloadJSON),
		formatTime(*expected.StartedAt),
		expected.Payload.SchemaVersion,
		expected.Payload.Configuration.Digest,
	)
	if err != nil {
		return false, fmt.Errorf("finish V1 job: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check finished V1 job: %w", err)
	}
	if updated != 1 {
		return false, ErrAgentRuntimeDataInvalid
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit V1 completion: %w", err)
	}
	return true, nil
}

func exactTerminalV1Result(
	ctx context.Context,
	queryer runtimeQueryer,
	stored application.AgentJobRecordV1,
	expected application.AgentJobRecordV1,
	run application.V1AgentRunRecord,
	artifacts []domain.Artifact,
) (bool, error) {
	if !sameTerminalInputV1Job(stored, expected, run) ||
		stored.Status != run.Result.Outcome {
		return false, ErrAgentRuntimeDataInvalid
	}
	existingRun, found, err := loadV1Run(ctx, queryer, stored.ID)
	if err != nil {
		return false, err
	}
	if !found || !reflect.DeepEqual(existingRun, run) {
		return false, ErrAgentRuntimeDataInvalid
	}
	existingArtifacts, err := loadV1RunArtifacts(ctx, queryer, existingRun)
	if err != nil {
		return false, err
	}
	if !sameArtifacts(existingArtifacts, artifacts) {
		return false, ErrAgentRuntimeDataInvalid
	}
	return true, nil
}

func sameArtifacts(left, right []domain.Artifact) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !reflect.DeepEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}

func sameRunningV1Job(stored, expected application.AgentJobRecordV1) bool {
	return reflect.DeepEqual(stored, expected)
}

func sameTerminalInputV1Job(
	stored application.AgentJobRecordV1,
	expected application.AgentJobRecordV1,
	run application.V1AgentRunRecord,
) bool {
	return stored.ID == expected.ID &&
		stored.Fingerprint == expected.Fingerprint &&
		stored.EventID == expected.EventID &&
		stored.Agent == expected.Agent &&
		reflect.DeepEqual(stored.Payload, expected.Payload) &&
		stored.CreatedAt.Equal(expected.CreatedAt) &&
		stored.StartedAt != nil &&
		expected.StartedAt != nil &&
		stored.StartedAt.Equal(*expected.StartedAt) &&
		stored.FinishedAt != nil &&
		stored.FinishedAt.Equal(run.FinishedAt)
}

func insertV1Run(
	ctx context.Context,
	transaction *sql.Tx,
	run application.V1AgentRunRecord,
) error {
	output, err := json.Marshal(run.Result)
	if err != nil || len(output) == 0 || len(output) > maxAgentRuntimeStoredJSONBytes {
		return ErrAgentRuntimeDataInvalid
	}
	failureCategory := ""
	if run.Result.Failure != nil {
		failureCategory = run.Result.Failure.Category
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO agent_runs (
			id, job_id, agent_name, agent_version, status, evidence_json,
			output_json, error, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, '[]', ?, ?, ?, ?)
	`,
		run.ID,
		run.JobID,
		run.Agent.Name,
		run.Agent.Version,
		run.Result.Outcome,
		string(output),
		failureCategory,
		formatTime(run.StartedAt),
		formatTime(run.FinishedAt),
	); err != nil {
		return fmt.Errorf("insert V1 agent run: %w", err)
	}
	return nil
}

func insertV1Artifact(
	ctx context.Context,
	transaction *sql.Tx,
	artifact domain.Artifact,
) error {
	inputs, err := encodeJSON(artifact.Inputs)
	if err != nil {
		return err
	}
	claimIDs, err := encodeJSON(artifact.ClaimIDs)
	if err != nil {
		return err
	}
	factIDs, err := encodeJSON(artifact.FactIDs)
	if err != nil {
		return err
	}
	supporting, err := encodeJSON(artifact.SupportingEvidence)
	if err != nil {
		return err
	}
	contradicting, err := encodeJSON(artifact.ContradictingEvidence)
	if err != nil {
		return err
	}
	for _, value := range []string{
		string(artifact.PayloadJSON), inputs, claimIDs, factIDs, supporting, contradicting,
	} {
		if len(value) == 0 || len(value) > maxAgentRuntimeStoredJSONBytes {
			return ErrAgentRuntimeDataInvalid
		}
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, fingerprint, kind, schema_version, payload_json, run_id,
			event_id, job_fingerprint, inputs_json, claim_ids_json,
			fact_ids_json, supporting_evidence_json,
			contradicting_evidence_json, proposal_status, safety_status,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		artifact.ID,
		artifact.Fingerprint,
		artifact.Kind,
		artifact.SchemaVersion,
		string(artifact.PayloadJSON),
		artifact.RunID,
		artifact.TriggerEventID,
		artifact.JobFingerprint,
		inputs,
		claimIDs,
		factIDs,
		supporting,
		contradicting,
		artifact.ProposalStatus,
		artifact.SafetyStatus,
		formatTime(artifact.CreatedAt),
	); err != nil {
		return fmt.Errorf("insert V1 artifact: %w", err)
	}
	return nil
}

func resultFailureCategory(result domain.AgentRunResultV1) string {
	if result.Failure == nil {
		return ""
	}
	return result.Failure.Category
}
