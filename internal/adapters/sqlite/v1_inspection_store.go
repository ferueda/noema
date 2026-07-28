package sqlite

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

// InspectV1Job returns the durable job, trigger, optional run, and artifacts in
// the exact order recorded by the V1 run result.
func (store *Store) InspectV1Job(
	ctx context.Context,
	jobID string,
) (application.V1JobInspection, bool, error) {
	job, found, err := store.LoadV1Job(ctx, jobID)
	if err != nil || !found {
		return application.V1JobInspection{}, found, err
	}
	trigger, err := loadRuntimeEvent(ctx, store.database, job.EventID)
	if err != nil {
		return application.V1JobInspection{}, false, ErrAgentRuntimeDataInvalid
	}
	run, foundRun, err := loadV1Run(ctx, store.database, job.ID)
	if err != nil {
		return application.V1JobInspection{}, false, ErrAgentRuntimeDataInvalid
	}
	if !foundRun {
		if job.Status == domain.JobSucceeded || job.Status == domain.JobFailed {
			return application.V1JobInspection{}, false, ErrAgentRuntimeDataInvalid
		}
		return application.V1JobInspection{
			Job: job, TriggerEvent: trigger, Artifacts: []domain.Artifact{},
		}, true, nil
	}
	if job.Status != run.Result.Outcome ||
		job.StartedAt == nil || job.FinishedAt == nil ||
		!run.StartedAt.Equal(*job.StartedAt) ||
		!run.FinishedAt.Equal(*job.FinishedAt) {
		return application.V1JobInspection{}, false, ErrAgentRuntimeDataInvalid
	}
	artifacts, err := loadV1RunArtifacts(ctx, store.database, run)
	if err != nil {
		return application.V1JobInspection{}, false, ErrAgentRuntimeDataInvalid
	}
	for _, artifact := range artifacts {
		if artifact.TriggerEventID != job.EventID ||
			artifact.JobFingerprint != job.Fingerprint ||
			artifact.Inputs.AnalysisRunID != job.Payload.Inputs.AnalysisRunID ||
			!slices.Equal(artifact.Inputs.ClaimIDs, job.Payload.Inputs.ClaimIDs) {
			return application.V1JobInspection{}, false, ErrAgentRuntimeDataInvalid
		}
	}
	return application.V1JobInspection{
		Job: job, TriggerEvent: trigger, Run: &run, Artifacts: artifacts,
	}, true, nil
}

// ListV1ContentIdeaArtifacts reads the generic artifact store as the
// authoritative idea source.
func (store *Store) ListV1ContentIdeaArtifacts(
	ctx context.Context,
) ([]domain.Artifact, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT DISTINCT jobs.id
		  FROM jobs
		  JOIN agent_runs ON agent_runs.job_id = jobs.id
		  JOIN artifacts ON artifacts.run_id = agent_runs.id
		 WHERE jobs.payload_schema_version = ?
		   AND artifacts.kind = ?
		   AND artifacts.schema_version = ?
		 ORDER BY jobs.id
	`,
		domain.AgentJobPayloadSchemaVersion,
		domain.ArtifactKindContentIdea,
		domain.ContentIdeaSchemaVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("list V1 content idea jobs: %w", err)
	}
	jobIDs := make([]string, 0)
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read V1 content idea job: %w", err)
		}
		jobIDs = append(jobIDs, jobID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate V1 content idea jobs: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close V1 content idea jobs: %w", err)
	}

	artifacts := make([]domain.Artifact, 0)
	for _, jobID := range jobIDs {
		inspection, found, err := store.InspectV1Job(ctx, jobID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, ErrAgentRuntimeDataInvalid
		}
		for _, artifact := range inspection.Artifacts {
			if artifact.Kind == domain.ArtifactKindContentIdea &&
				artifact.SchemaVersion == domain.ContentIdeaSchemaVersion {
				artifacts = append(artifacts, artifact)
			}
		}
	}
	sort.Slice(artifacts, func(left, right int) bool {
		if !artifacts[left].CreatedAt.Equal(artifacts[right].CreatedAt) {
			return artifacts[left].CreatedAt.After(artifacts[right].CreatedAt)
		}
		leftRank := contentIdeaRank(artifacts[left])
		rightRank := contentIdeaRank(artifacts[right])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return artifacts[left].ID < artifacts[right].ID
	})
	return artifacts, nil
}

func loadRuntimeEvent(
	ctx context.Context,
	queryer runtimeQueryer,
	eventID string,
) (domain.Event, error) {
	var event domain.Event
	var payload, evidence, createdAt string
	if err := queryer.QueryRowContext(ctx, `
		SELECT events.id, events.fingerprint, events.type, events.subject_type,
		       events.subject_id,
		       events.payload_json, events.evidence_json, events.created_at
		  FROM events
		 WHERE events.id = ?
	`, eventID).Scan(
		&event.ID,
		&event.Fingerprint,
		&event.Type,
		&event.SubjectType,
		&event.SubjectID,
		&payload,
		&evidence,
		&createdAt,
	); err != nil {
		return domain.Event{}, err
	}
	if len(payload) == 0 || len(payload) > maxAgentRuntimeStoredJSONBytes ||
		len(evidence) == 0 || len(evidence) > maxAgentRuntimeStoredJSONBytes ||
		decodeStrictStoredJSON(payload, &event.Payload) != nil ||
		decodeStrictStoredJSON(evidence, &event.Evidence) != nil {
		return domain.Event{}, ErrAgentRuntimeDataInvalid
	}
	var err error
	event.CreatedAt, err = parseTime(createdAt)
	fingerprint, fingerprintErr := application.EventFingerprint(
		event.Type, event.SubjectType, event.SubjectID, event.Payload,
	)
	if err != nil ||
		fingerprintErr != nil ||
		event.ID != platform.DerivedID("evt_", fingerprint) ||
		event.Fingerprint != fingerprint {
		return domain.Event{}, ErrAgentRuntimeDataInvalid
	}
	return event, nil
}

func contentIdeaRank(artifact domain.Artifact) int {
	var idea domain.ContentIdeaV1
	if err := decodeStrictStoredJSON(string(artifact.PayloadJSON), &idea); err != nil {
		return int(^uint(0) >> 1)
	}
	return idea.Rank
}
