package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

const maxAgentRuntimeStoredJSONBytes = 1024 * 1024

type runtimeQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadV1JobByID(
	ctx context.Context,
	queryer runtimeQueryer,
	jobID string,
) (application.AgentJobRecordV1, error) {
	return readV1Job(queryer.QueryRowContext(ctx, `
		SELECT jobs.id, jobs.fingerprint, jobs.event_id, jobs.agent_name,
		       jobs.agent_version, jobs.status, jobs.payload_json,
		       jobs.created_at, jobs.started_at, jobs.finished_at,
		       jobs.configuration_digest
		  FROM jobs
		 WHERE jobs.id = ?
		   AND jobs.payload_schema_version = ?
	`, jobID, domain.AgentJobPayloadSchemaVersion))
}

func loadV1Run(
	ctx context.Context,
	queryer runtimeQueryer,
	jobID string,
) (application.V1AgentRunRecord, bool, error) {
	var run application.V1AgentRunRecord
	var agentName, agentVersion, status, evidenceJSON, outputJSON, failureCategory string
	var startedAt, finishedAt string
	err := queryer.QueryRowContext(ctx, `
		SELECT id, job_id, agent_name, agent_version, status, evidence_json,
		       output_json, error, started_at, finished_at
		  FROM agent_runs
		 WHERE job_id = ?
	`, jobID).Scan(
		&run.ID,
		&run.JobID,
		&agentName,
		&agentVersion,
		&status,
		&evidenceJSON,
		&outputJSON,
		&failureCategory,
		&startedAt,
		&finishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return application.V1AgentRunRecord{}, false, nil
	}
	if err != nil {
		return application.V1AgentRunRecord{}, false, err
	}
	var evidence []domain.EvidenceRef
	if err := decodeStrictStoredJSON(evidenceJSON, &evidence); err != nil || len(evidence) != 0 ||
		len(outputJSON) == 0 || len(outputJSON) > maxAgentRuntimeStoredJSONBytes ||
		decodeStrictStoredJSON(outputJSON, &run.Result) != nil {
		return application.V1AgentRunRecord{}, false, ErrAgentRuntimeDataInvalid
	}
	run.Agent = domain.AgentIdentity{Name: agentName, Version: agentVersion}
	run.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return application.V1AgentRunRecord{}, false, ErrAgentRuntimeDataInvalid
	}
	run.FinishedAt, err = parseTime(finishedAt)
	if err != nil ||
		status != run.Result.Outcome ||
		failureCategory != resultFailureCategory(run.Result) ||
		application.ValidateV1AgentRunRecord(run) != nil {
		return application.V1AgentRunRecord{}, false, ErrAgentRuntimeDataInvalid
	}
	return run, true, nil
}

func loadV1RunArtifacts(
	ctx context.Context,
	queryer runtimeQueryer,
	run application.V1AgentRunRecord,
) ([]domain.Artifact, error) {
	artifacts := make([]domain.Artifact, 0, len(run.Result.ArtifactIDs))
	for _, artifactID := range run.Result.ArtifactIDs {
		artifact, err := readV1Artifact(queryer.QueryRowContext(ctx, `
			SELECT id, fingerprint, kind, schema_version, payload_json, run_id,
			       event_id, job_fingerprint, inputs_json, claim_ids_json,
			       fact_ids_json, supporting_evidence_json,
			       contradicting_evidence_json, proposal_status, safety_status,
			       created_at
			  FROM artifacts
			 WHERE id = ? AND run_id = ?
		`, artifactID, run.ID))
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	var count int
	if err := queryer.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM artifacts WHERE run_id = ?",
		run.ID,
	).Scan(&count); err != nil {
		return nil, err
	}
	if count != len(artifacts) {
		return nil, ErrAgentRuntimeDataInvalid
	}
	return artifacts, nil
}

func readV1Artifact(row rowScanner) (domain.Artifact, error) {
	var artifact domain.Artifact
	var payload, inputs, claimIDs, factIDs, supporting, contradicting, createdAt string
	if err := row.Scan(
		&artifact.ID,
		&artifact.Fingerprint,
		&artifact.Kind,
		&artifact.SchemaVersion,
		&payload,
		&artifact.RunID,
		&artifact.TriggerEventID,
		&artifact.JobFingerprint,
		&inputs,
		&claimIDs,
		&factIDs,
		&supporting,
		&contradicting,
		&artifact.ProposalStatus,
		&artifact.SafetyStatus,
		&createdAt,
	); err != nil {
		return domain.Artifact{}, err
	}
	for _, value := range []string{payload, inputs, claimIDs, factIDs, supporting, contradicting} {
		if len(value) == 0 || len(value) > maxAgentRuntimeStoredJSONBytes {
			return domain.Artifact{}, ErrAgentRuntimeDataInvalid
		}
	}
	artifact.PayloadJSON = json.RawMessage(payload)
	if err := decodeStrictStoredJSON(inputs, &artifact.Inputs); err != nil ||
		decodeStrictStoredJSON(claimIDs, &artifact.ClaimIDs) != nil ||
		decodeStrictStoredJSON(factIDs, &artifact.FactIDs) != nil ||
		decodeStrictStoredJSON(supporting, &artifact.SupportingEvidence) != nil ||
		decodeStrictStoredJSON(contradicting, &artifact.ContradictingEvidence) != nil {
		return domain.Artifact{}, ErrAgentRuntimeDataInvalid
	}
	var err error
	artifact.CreatedAt, err = parseTime(createdAt)
	if err != nil || artifact.Validate() != nil {
		return domain.Artifact{}, ErrAgentRuntimeDataInvalid
	}
	return artifact, nil
}

func decodeStrictStoredJSON(value string, target any) error {
	if len(value) == 0 || len(value) > maxAgentRuntimeStoredJSONBytes {
		return ErrAgentRuntimeDataInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(value)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("stored JSON has trailing data")
	}
	return nil
}
