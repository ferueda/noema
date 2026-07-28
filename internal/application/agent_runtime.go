package application

import (
	"errors"
	"slices"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

// V1JobKnowledgeInput is the typed, durable input selected by one V1 job.
// Agent-specific preparation remains responsible for deciding how to use it.
type V1JobKnowledgeInput struct {
	Job          AgentJobRecordV1        `json:"job"`
	TriggerEvent domain.Event            `json:"triggerEvent"`
	Analysis     domain.SemanticAnalysis `json:"analysis"`
	Facts        []domain.Fact           `json:"facts"`
}

// V1AgentRunRecord is the generic durable envelope for one V1 job attempt.
// The versioned result owns executor and policy metadata.
type V1AgentRunRecord struct {
	ID         string                  `json:"id"`
	JobID      string                  `json:"jobId"`
	Agent      domain.AgentIdentity    `json:"agent"`
	Result     domain.AgentRunResultV1 `json:"result"`
	StartedAt  time.Time               `json:"startedAt"`
	FinishedAt time.Time               `json:"finishedAt"`
}

// V1JobCompletion is the complete generic state admitted in one transaction.
type V1JobCompletion struct {
	Job       AgentJobRecordV1  `json:"job"`
	Run       V1AgentRunRecord  `json:"run"`
	Artifacts []domain.Artifact `json:"artifacts"`
}

// V1JobInspection is the durable job-centered review surface.
type V1JobInspection struct {
	Job          AgentJobRecordV1  `json:"job"`
	TriggerEvent domain.Event      `json:"triggerEvent"`
	Run          *V1AgentRunRecord `json:"run,omitempty"`
	Artifacts    []domain.Artifact `json:"artifacts"`
}

func ValidateV1AgentRunRecord(value V1AgentRunRecord) error {
	if value.ID != platform.DerivedID("run_", value.JobID) ||
		value.JobID == "" ||
		value.Agent.Validate() != nil ||
		value.Result.Validate() != nil ||
		value.StartedAt.IsZero() ||
		value.FinishedAt.IsZero() ||
		value.FinishedAt.Before(value.StartedAt) {
		return errors.New("V1 agent run is invalid")
	}
	return nil
}

func ValidateV1JobCompletion(value V1JobCompletion) error {
	if value.Job.Status != domain.JobRunning ||
		ValidateAgentJobRecordV1(value.Job) != nil ||
		ValidateV1AgentRunRecord(value.Run) != nil ||
		value.Run.JobID != value.Job.ID ||
		value.Run.Agent != value.Job.Agent ||
		value.Job.StartedAt == nil ||
		!value.Run.StartedAt.Equal(*value.Job.StartedAt) ||
		value.Run.Result.Outcome != domain.AgentRunOutcomeSucceeded ||
		len(value.Artifacts) != len(value.Run.Result.ArtifactIDs) {
		return errors.New("V1 job completion is invalid")
	}
	artifactIDs := make([]string, len(value.Artifacts))
	for index, artifact := range value.Artifacts {
		if artifact.Validate() != nil ||
			artifact.RunID != value.Run.ID ||
			artifact.TriggerEventID != value.Job.EventID ||
			artifact.JobFingerprint != value.Job.Fingerprint ||
			artifact.Inputs.AnalysisRunID != value.Job.Payload.Inputs.AnalysisRunID ||
			!slices.Equal(artifact.Inputs.ClaimIDs, value.Job.Payload.Inputs.ClaimIDs) {
			return errors.New("V1 job completion artifact is invalid")
		}
		artifactIDs[index] = artifact.ID
	}
	if !slices.Equal(artifactIDs, value.Run.Result.ArtifactIDs) {
		return errors.New("V1 job completion artifact order is invalid")
	}
	return nil
}

func ValidateV1JobFailure(job AgentJobRecordV1, run V1AgentRunRecord) error {
	if job.Status != domain.JobRunning ||
		ValidateAgentJobRecordV1(job) != nil ||
		ValidateV1AgentRunRecord(run) != nil ||
		run.JobID != job.ID ||
		run.Agent != job.Agent ||
		job.StartedAt == nil ||
		!run.StartedAt.Equal(*job.StartedAt) ||
		run.Result.Outcome != domain.AgentRunOutcomeFailed {
		return errors.New("V1 job failure is invalid")
	}
	return nil
}
