package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

var (
	ErrContentScoutRemoteAuthorityRequired = errors.New(
		"Content Scout remote authority is required",
	)
	ErrContentScoutExecutorUnavailable = errors.New(
		"Content Scout executor is unavailable",
	)
)

// ContentScoutDispatchStore is the durable boundary needed for one V1 job.
// Agent-specific knowledge interpretation remains in ContentScoutHandlerV1.
type ContentScoutDispatchStore interface {
	ContentScoutKnowledgeReader
	InspectOldestPendingV1Job(context.Context) (PendingV1JobRecord, bool, error)
	ClaimPendingV1Job(context.Context, PendingV1JobIdentity, time.Time) (bool, error)
	LoadV1Job(context.Context, string) (AgentJobRecordV1, bool, error)
	CompleteV1Job(context.Context, V1JobCompletion) (bool, error)
	FailV1Job(context.Context, AgentJobRecordV1, V1AgentRunRecord) (bool, error)
}

// ContentScoutPreflight validates the configured executor before a non-empty
// job is claimed. Failures leave the job pending.
type ContentScoutPreflight func(
	context.Context,
	AgentJobRecordV1,
	domain.AgentExecutionIdentity,
) error

type ContentScoutDispatchResult struct {
	NoWork          bool     `json:"noWork"`
	Claimed         bool     `json:"claimed"`
	JobID           string   `json:"jobId,omitempty"`
	Outcome         string   `json:"outcome,omitempty"`
	Disposition     string   `json:"disposition,omitempty"`
	ArtifactIDs     []string `json:"artifactIds,omitempty"`
	FailureCategory string   `json:"failureCategory,omitempty"`
}

// ContentScoutDispatcherV1 owns one-shot queue lifecycle around the
// Content Scout-specific preparation and admission boundary.
type ContentScoutDispatcherV1 struct {
	Store       ContentScoutDispatchStore
	Executor    AgentExecutor
	Preflight   ContentScoutPreflight
	AllowRemote bool
	Now         func() time.Time
}

func (dispatcher ContentScoutDispatcherV1) RunOnce(
	ctx context.Context,
) (ContentScoutDispatchResult, error) {
	if dispatcher.Store == nil || dispatcher.Now == nil {
		return ContentScoutDispatchResult{}, errors.New("Content Scout dispatcher is invalid")
	}
	pending, found, err := dispatcher.Store.InspectOldestPendingV1Job(ctx)
	if err != nil {
		return ContentScoutDispatchResult{}, err
	}
	if !found {
		return ContentScoutDispatchResult{NoWork: true}, nil
	}
	job, found, err := dispatcher.Store.LoadV1Job(ctx, pending.ID)
	if err != nil || !found || job.Status != domain.JobPending {
		if err != nil {
			return ContentScoutDispatchResult{}, err
		}
		return ContentScoutDispatchResult{}, errors.New("pending Content Scout job is unavailable")
	}
	configuration, err := contentScoutConfigurationFromJob(job)
	if err != nil {
		return ContentScoutDispatchResult{}, err
	}
	execution := contentScoutExecutionIdentity(configuration)
	hasClaims := len(job.Payload.Inputs.ClaimIDs) > 0
	if hasClaims {
		if !dispatcher.AllowRemote {
			return ContentScoutDispatchResult{}, ErrContentScoutRemoteAuthorityRequired
		}
		if dispatcher.Executor == nil || dispatcher.Preflight == nil {
			return ContentScoutDispatchResult{}, ErrContentScoutExecutorUnavailable
		}
		if err := dispatcher.Preflight(ctx, job, execution); err != nil {
			return ContentScoutDispatchResult{}, err
		}
	}

	startedAt := dispatcher.Now().UTC()
	claimed, err := dispatcher.Store.ClaimPendingV1Job(
		ctx, pending.PendingV1JobIdentity, startedAt,
	)
	if err != nil {
		return ContentScoutDispatchResult{}, err
	}
	if !claimed {
		return ContentScoutDispatchResult{JobID: pending.ID}, nil
	}
	job, found, err = dispatcher.Store.LoadV1Job(ctx, pending.ID)
	if err != nil || !found || job.Status != domain.JobRunning || job.StartedAt == nil {
		if err != nil {
			return ContentScoutDispatchResult{}, err
		}
		return ContentScoutDispatchResult{}, errors.New("claimed Content Scout job is unavailable")
	}

	handler := ContentScoutHandlerV1{Knowledge: dispatcher.Store}
	prepared, err := handler.Prepare(ctx, job)
	if err != nil {
		failure := contentScoutDispatchFailure(
			err,
			domain.AgentFailureStagePreparation,
			domain.AgentFailureCategoryInputInvalid,
			domain.AgentExecutionDispositionNone,
			&execution,
			nil,
		)
		return dispatcher.persistFailure(ctx, job, failure)
	}
	if prepared.SkipNoClaims {
		return dispatcher.persistSkipped(ctx, job)
	}

	outputSchema, err := ContentScoutOutputSchema()
	if err != nil {
		failure := contentScoutRunFailure{
			stage:       domain.AgentFailureStagePreparation,
			category:    domain.AgentFailureCategoryConfigurationInvalid,
			disposition: domain.AgentExecutionDispositionNone,
			execution:   &execution,
			privacy:     prepared.Privacy,
		}
		return dispatcher.persistFailure(ctx, job, failure)
	}
	request := domain.AgentExecutionRequestV1{
		ContractVersion: domain.AgentExecutionContractVersion,
		JobID:           job.ID, JobFingerprint: job.Fingerprint,
		TriggerEventID: job.EventID, Agent: job.Agent,
		Configuration: job.Payload.Configuration, Execution: execution,
		Input: domain.AgentExecutionInputV1{
			SchemaName:    ContentScoutInputSchemaName,
			SchemaVersion: domain.ContentScoutInputSchemaVersion,
			SchemaDigest:  ContentScoutInputSchemaDigest,
			CanonicalJSON: append(json.RawMessage{}, prepared.CanonicalJSON...),
		},
		RequiredOutputSchema: outputSchema.Identity,
		DeadlineAt: dispatcher.Now().UTC().Add(
			time.Duration(ContentScoutDeadlineMilliseconds) * time.Millisecond,
		),
		Authority: domain.AgentExecutionAuthorityV1{AllowRemote: true},
	}
	if request.Validate() != nil {
		failure := contentScoutRunFailure{
			stage:       domain.AgentFailureStagePreparation,
			category:    domain.AgentFailureCategoryInputInvalid,
			disposition: domain.AgentExecutionDispositionNone,
			execution:   &execution,
			privacy:     prepared.Privacy,
		}
		return dispatcher.persistFailure(ctx, job, failure)
	}

	response, executeErr := dispatcher.Executor.Execute(ctx, request, outputSchema)
	if executeErr != nil {
		category := domain.AgentFailureCategoryExecutorFailed
		var receipt *domain.AgentExecutionReceiptV1
		if response.Receipt.ValidateObserved() == nil {
			copy := response.Receipt
			receipt = &copy
			if copy.FailureCategory != "" {
				category = copy.FailureCategory
			}
		}
		failure := contentScoutRunFailure{
			stage:       domain.AgentFailureStageExecution,
			category:    category,
			disposition: domain.AgentExecutionDispositionInvoked,
			execution:   &execution,
			receipt:     receipt,
			privacy:     prepared.Privacy,
		}
		return dispatcher.persistFailure(ctx, job, failure)
	}
	if response.Validate() != nil {
		var receipt *domain.AgentExecutionReceiptV1
		if response.Receipt.ValidateObserved() == nil {
			copy := response.Receipt
			receipt = &copy
		}
		failure := contentScoutRunFailure{
			stage:       domain.AgentFailureStageResponseDecode,
			category:    domain.AgentFailureCategoryResponseInvalid,
			disposition: domain.AgentExecutionDispositionInvoked,
			execution:   &execution,
			receipt:     receipt,
			privacy:     prepared.Privacy,
		}
		return dispatcher.persistFailure(ctx, job, failure)
	}
	if !contentScoutReceiptMatchesRequest(response.Receipt, request) {
		receipt := response.Receipt
		failure := contentScoutRunFailure{
			stage:       domain.AgentFailureStageResponseDecode,
			category:    domain.AgentFailureCategoryExecutorMismatch,
			disposition: domain.AgentExecutionDispositionInvoked,
			execution:   &execution,
			receipt:     &receipt,
			privacy:     prepared.Privacy,
		}
		return dispatcher.persistFailure(ctx, job, failure)
	}

	finishedAt := dispatcher.Now().UTC()
	runID := platform.DerivedID("run_", job.ID)
	admission, err := prepared.AdmitCandidates(
		response.CandidateJSON, runID, finishedAt,
	)
	if err != nil {
		failure := contentScoutDispatchFailure(
			err,
			domain.AgentFailureStageAdmission,
			domain.AgentFailureCategoryCandidateInvalid,
			domain.AgentExecutionDispositionInvoked,
			&execution,
			&response.Receipt,
		)
		return dispatcher.persistFailure(ctx, job, failure)
	}
	artifactIDs := make([]string, len(admission.Artifacts))
	for index, artifact := range admission.Artifacts {
		artifactIDs[index] = artifact.ID
	}
	run := V1AgentRunRecord{
		ID: runID, JobID: job.ID, Agent: job.Agent,
		Result: domain.AgentRunResultV1{
			SchemaVersion: domain.AgentRunResultSchemaVersion,
			Outcome:       domain.AgentRunOutcomeSucceeded,
			Disposition:   domain.AgentExecutionDispositionInvoked,
			Execution:     &execution, Receipt: &response.Receipt,
			Privacy: admission.Privacy, ArtifactIDs: artifactIDs,
		},
		StartedAt: *job.StartedAt, FinishedAt: finishedAt,
	}
	if _, err := dispatcher.Store.CompleteV1Job(ctx, V1JobCompletion{
		Job: job, Run: run, Artifacts: admission.Artifacts,
	}); err != nil {
		return ContentScoutDispatchResult{}, err
	}
	return ContentScoutDispatchResult{
		Claimed: true, JobID: job.ID, Outcome: run.Result.Outcome,
		Disposition: run.Result.Disposition, ArtifactIDs: artifactIDs,
	}, nil
}

func contentScoutReceiptMatchesRequest(
	receipt domain.AgentExecutionReceiptV1,
	request domain.AgentExecutionRequestV1,
) bool {
	return receipt.ExecutorKind == request.Execution.ExecutorKind &&
		receipt.ExecutorVersion == request.Execution.ExecutorVersion &&
		receipt.RequestedRoute != nil &&
		*receipt.RequestedRoute == request.Configuration.Route
}

func contentScoutExecutionIdentity(
	configuration ContentScoutConfiguration,
) domain.AgentExecutionIdentity {
	return domain.AgentExecutionIdentity{
		ExecutorKind:          ContentScoutExecutorKind,
		ExecutorVersion:       ContentScoutExecutorVersion,
		AgentDefinitionDigest: configuration.agentFileDigest,
		ContractVersion:       domain.AgentExecutionContractVersion,
		RecoveryPolicyVersion: ContentScoutRecoveryPolicyVersion,
	}
}

type contentScoutRunFailure struct {
	stage       string
	category    string
	disposition string
	execution   *domain.AgentExecutionIdentity
	receipt     *domain.AgentExecutionReceiptV1
	privacy     domain.AgentPrivacyOutcomeV1
}

func contentScoutDispatchFailure(
	err error,
	stage string,
	fallbackCategory string,
	disposition string,
	execution *domain.AgentExecutionIdentity,
	receipt *domain.AgentExecutionReceiptV1,
) contentScoutRunFailure {
	failure := contentScoutRunFailure{
		stage: stage, category: fallbackCategory, disposition: disposition,
		execution: execution, receipt: receipt,
	}
	var applicationFailure ContentScoutApplicationFailure
	if errors.As(err, &applicationFailure) {
		failure.category = applicationFailure.Category
		failure.privacy = applicationFailure.Privacy
	}
	return failure
}

func (dispatcher ContentScoutDispatcherV1) persistSkipped(
	ctx context.Context,
	job AgentJobRecordV1,
) (ContentScoutDispatchResult, error) {
	finishedAt := dispatcher.Now().UTC()
	run := V1AgentRunRecord{
		ID: platform.DerivedID("run_", job.ID), JobID: job.ID, Agent: job.Agent,
		Result: domain.AgentRunResultV1{
			SchemaVersion: domain.AgentRunResultSchemaVersion,
			Outcome:       domain.AgentRunOutcomeSucceeded,
			Disposition:   domain.AgentExecutionDispositionSkipped,
			ArtifactIDs:   []string{},
		},
		StartedAt: *job.StartedAt, FinishedAt: finishedAt,
	}
	if _, err := dispatcher.Store.CompleteV1Job(ctx, V1JobCompletion{
		Job: job, Run: run, Artifacts: []domain.Artifact{},
	}); err != nil {
		return ContentScoutDispatchResult{}, err
	}
	return ContentScoutDispatchResult{
		Claimed: true, JobID: job.ID, Outcome: run.Result.Outcome,
		Disposition: run.Result.Disposition, ArtifactIDs: []string{},
	}, nil
}

func (dispatcher ContentScoutDispatcherV1) persistFailure(
	ctx context.Context,
	job AgentJobRecordV1,
	failure contentScoutRunFailure,
) (ContentScoutDispatchResult, error) {
	finishedAt := dispatcher.Now().UTC()
	run := V1AgentRunRecord{
		ID: platform.DerivedID("run_", job.ID), JobID: job.ID, Agent: job.Agent,
		Result: domain.AgentRunResultV1{
			SchemaVersion: domain.AgentRunResultSchemaVersion,
			Outcome:       domain.AgentRunOutcomeFailed,
			Disposition:   failure.disposition,
			Execution:     failure.execution,
			Receipt:       failure.receipt,
			Privacy:       failure.privacy,
			Failure: &domain.AgentFailureV1{
				Stage: failure.stage, Category: failure.category,
			},
			ArtifactIDs: []string{},
		},
		StartedAt: *job.StartedAt, FinishedAt: finishedAt,
	}
	if _, err := dispatcher.Store.FailV1Job(ctx, job, run); err != nil {
		return ContentScoutDispatchResult{}, err
	}
	return ContentScoutDispatchResult{
		Claimed: true, JobID: job.ID, Outcome: run.Result.Outcome,
		Disposition: run.Result.Disposition,
		ArtifactIDs: []string{}, FailureCategory: failure.category,
	}, nil
}
