package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestContentScoutDispatcherCompletesZeroClaimsWithoutRemoteExecution(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	fixture.record = contentScoutZeroClaimRecord(t)
	fixture.reader.record = fixture.record
	fixture.job = contentScoutJobForRecord(t, fixture.configuration, fixture.record)
	store := newContentScoutDispatcherStore(t, fixture)

	result, err := (ContentScoutDispatcherV1{
		Store: store,
		Now:   dispatcherTestClock(fixture.job.CreatedAt),
	}).RunOnce(context.Background())
	if err != nil {
		t.Fatalf("dispatch zero-claim job: %v", err)
	}
	if !result.Claimed ||
		result.Outcome != domain.AgentRunOutcomeSucceeded ||
		result.Disposition != domain.AgentExecutionDispositionSkipped ||
		store.completed == nil || store.failed != nil ||
		store.completed.Run.Result.Receipt != nil ||
		len(store.completed.Artifacts) != 0 {
		t.Fatalf("zero-claim dispatch = %#v / %#v", result, store)
	}
}

func TestContentScoutDispatcherLeavesJobPendingWhenPreflightFails(t *testing.T) {
	fixture := newContentScoutFixture(t, nil)
	store := newContentScoutDispatcherStore(t, fixture)
	preflightError := errors.New("executor preflight failed")
	executor := &contentScoutDispatcherExecutor{}

	_, err := (ContentScoutDispatcherV1{
		Store: store, Executor: executor, AllowRemote: true,
		Preflight: func(
			context.Context,
			AgentJobRecordV1,
			domain.AgentExecutionIdentity,
		) error {
			return preflightError
		},
		Now: dispatcherTestClock(fixture.job.CreatedAt),
	}).RunOnce(context.Background())
	if !errors.Is(err, preflightError) {
		t.Fatalf("preflight failure = %v", err)
	}
	if store.job.Status != domain.JobPending ||
		store.completed != nil || store.failed != nil ||
		executor.calls != 0 {
		t.Fatalf("preflight changed durable work: %#v / %#v", store, executor)
	}
}

func TestContentScoutDispatcherPersistsPreparationFailureWithoutInvocation(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	delete(fixture.reader.facts, fixture.factOneID)
	store := newContentScoutDispatcherStore(t, fixture)
	executor := &contentScoutDispatcherExecutor{}

	result, err := (ContentScoutDispatcherV1{
		Store: store, Executor: executor, AllowRemote: true,
		Preflight: func(
			context.Context,
			AgentJobRecordV1,
			domain.AgentExecutionIdentity,
		) error {
			return nil
		},
		Now: dispatcherTestClock(fixture.job.CreatedAt),
	}).RunOnce(context.Background())
	if err != nil {
		t.Fatalf("dispatch preparation failure: %v", err)
	}
	if result.Outcome != domain.AgentRunOutcomeFailed ||
		result.Disposition != domain.AgentExecutionDispositionNone ||
		result.FailureCategory != domain.AgentFailureCategoryInputInvalid ||
		store.failed == nil ||
		store.failed.Result.Execution == nil ||
		store.failed.Result.Receipt != nil ||
		executor.calls != 0 {
		t.Fatalf("preparation failure = %#v / %#v / %#v", result, store, executor)
	}
}

func TestContentScoutDispatcherCompletesWithValidZeroBudgetFactText(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	replaceContentScoutFixtureFact(
		t, &fixture, fixture.factOneID,
		func(fact *domain.Fact) {
			fact.Value = domain.FactValue{Error: &domain.SelectedText{
				Text: "", EmittedUTF8Bytes: 0,
				OriginalUTF8Bytes: 24, Truncated: true,
				ContentHash: domain.Digest{
					Scheme: "sha256-utf8-v1",
					Digest: strings.Repeat("b", 64),
				},
			}}
		},
	)
	store := newContentScoutDispatcherStore(t, fixture)
	executor := &contentScoutResultExecutor{
		execute: func(request domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
			var input domain.ContentScoutInputV1
			if err := json.Unmarshal(request.Input.CanonicalJSON, &input); err != nil {
				return domain.AgentExecutionResponseV1{}, err
			}
			if input.Facts[0].Value.Error != nil ||
				input.Omissions.OmittedTextFactCount == 0 {
				return domain.AgentExecutionResponseV1{}, errors.New("omitted fact text was disclosed")
			}
			return domain.AgentExecutionResponseV1{
				ContractVersion: domain.AgentExecutionContractVersion,
				CandidateJSON:   json.RawMessage(`{"ideas":[]}`),
				Receipt:         contentScoutDispatcherReceipt(request),
			}, nil
		},
	}

	result, err := (ContentScoutDispatcherV1{
		Store: store, Executor: executor, AllowRemote: true,
		Preflight: func(
			context.Context,
			AgentJobRecordV1,
			domain.AgentExecutionIdentity,
		) error {
			return nil
		},
		Now: dispatcherTestClock(fixture.job.CreatedAt),
	}).RunOnce(context.Background())
	if err != nil {
		t.Fatalf("dispatch zero-budget fact text: %v", err)
	}
	if result.Outcome != domain.AgentRunOutcomeSucceeded ||
		result.Disposition != domain.AgentExecutionDispositionInvoked ||
		len(result.ArtifactIDs) != 0 ||
		store.completed == nil ||
		store.failed != nil ||
		executor.calls != 1 {
		t.Fatalf("zero-budget dispatch = %#v / %#v / %#v", result, store, executor)
	}
}

func TestContentScoutDispatcherPersistsTerminalFailureAfterCallerCancellation(
	t *testing.T,
) {
	fixture := newContentScoutFixture(t, nil)
	store := newContentScoutDispatcherStore(t, fixture)
	ctx, cancel := context.WithCancel(context.Background())
	executor := &contentScoutResultExecutor{
		execute: func(
			domain.AgentExecutionRequestV1,
		) (domain.AgentExecutionResponseV1, error) {
			cancel()
			return domain.AgentExecutionResponseV1{},
				errors.New("execution canceled")
		},
	}

	result, err := (ContentScoutDispatcherV1{
		Store: store, Executor: executor, AllowRemote: true,
		Preflight: func(
			context.Context,
			AgentJobRecordV1,
			domain.AgentExecutionIdentity,
		) error {
			return nil
		},
		Now: dispatcherTestClock(fixture.job.CreatedAt),
	}).RunOnce(ctx)
	if err != nil {
		t.Fatalf("dispatch canceled execution: %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) ||
		result.Outcome != domain.AgentRunOutcomeFailed ||
		result.Disposition != domain.AgentExecutionDispositionInvoked ||
		result.FailureCategory != domain.AgentFailureCategoryExecutorFailed ||
		len(result.ArtifactIDs) != 0 ||
		store.failed == nil ||
		store.completed != nil ||
		len(store.failed.Result.ArtifactIDs) != 0 {
		t.Fatalf("canceled execution result = %#v / %#v", result, store)
	}
}

func TestContentScoutDispatcherRejectsMismatchedExecutionReceipt(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*domain.AgentExecutionReceiptV1)
	}{
		{
			name: "executor kind",
			mutate: func(receipt *domain.AgentExecutionReceiptV1) {
				receipt.ExecutorKind = "different-executor"
			},
		},
		{
			name: "executor version",
			mutate: func(receipt *domain.AgentExecutionReceiptV1) {
				receipt.ExecutorVersion = "different-version"
			},
		},
		{
			name: "requested route",
			mutate: func(receipt *domain.AgentExecutionReceiptV1) {
				receipt.RequestedRoute.ServiceTier = "different-tier"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newContentScoutFixture(t, nil)
			store := newContentScoutDispatcherStore(t, fixture)
			executor := &contentScoutReceiptMismatchExecutor{
				mutate: test.mutate,
			}

			result, err := (ContentScoutDispatcherV1{
				Store: store, Executor: executor, AllowRemote: true,
				Preflight: func(
					context.Context,
					AgentJobRecordV1,
					domain.AgentExecutionIdentity,
				) error {
					return nil
				},
				Now: dispatcherTestClock(fixture.job.CreatedAt),
			}).RunOnce(context.Background())
			if err != nil {
				t.Fatalf("dispatch mismatched receipt: %v", err)
			}
			if !result.Claimed ||
				result.Outcome != domain.AgentRunOutcomeFailed ||
				result.Disposition != domain.AgentExecutionDispositionInvoked ||
				result.FailureCategory != domain.AgentFailureCategoryExecutorMismatch ||
				len(result.ArtifactIDs) != 0 ||
				executor.calls != 1 ||
				store.completed != nil ||
				store.failed == nil ||
				store.failed.Result.Failure == nil ||
				store.failed.Result.Failure.Stage != domain.AgentFailureStageResponseDecode ||
				store.failed.Result.Failure.Category != domain.AgentFailureCategoryExecutorMismatch ||
				store.failed.Result.Receipt != nil ||
				len(store.failed.Result.ArtifactIDs) != 0 {
				t.Fatalf(
					"mismatched receipt result = %#v / %#v / %#v",
					result, store, executor,
				)
			}
		})
	}
}

func TestContentScoutDispatcherPersistsSafeInvokedFailures(t *testing.T) {
	tests := []struct {
		name         string
		execute      func(domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error)
		wantStage    string
		wantCategory string
		wantReceipt  bool
	}{
		{
			name: "executor failure without receipt",
			execute: func(domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
				return domain.AgentExecutionResponseV1{}, errors.New("private executor failure")
			},
			wantStage:    domain.AgentFailureStageExecution,
			wantCategory: domain.AgentFailureCategoryExecutorFailed,
		},
		{
			name: "executor failure with receipt",
			execute: func(request domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
				receipt := contentScoutDispatcherReceipt(request)
				receipt.FailureCategory = domain.AgentFailureCategoryExecutorProtocol
				return domain.AgentExecutionResponseV1{Receipt: receipt},
					errors.New("private executor failure")
			},
			wantStage:    domain.AgentFailureStageExecution,
			wantCategory: domain.AgentFailureCategoryExecutorProtocol,
			wantReceipt:  true,
		},
		{
			name: "executor failure with mismatched partial receipt",
			execute: func(domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
				return domain.AgentExecutionResponseV1{
					Receipt: domain.AgentExecutionReceiptV1{
						ExecutorKind: "different-executor",
					},
				}, errors.New("private executor failure")
			},
			wantStage:    domain.AgentFailureStageExecution,
			wantCategory: domain.AgentFailureCategoryExecutorMismatch,
		},
		{
			name: "invalid response",
			execute: func(request domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
				return domain.AgentExecutionResponseV1{
					CandidateJSON: json.RawMessage(`{}`),
					Receipt:       contentScoutDispatcherReceipt(request),
				}, nil
			},
			wantStage:    domain.AgentFailureStageResponseDecode,
			wantCategory: domain.AgentFailureCategoryResponseInvalid,
			wantReceipt:  true,
		},
		{
			name: "invalid response with mismatched partial receipt",
			execute: func(request domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
				route := request.Configuration.Route
				route.ServiceTier = "different-tier"
				return domain.AgentExecutionResponseV1{
					CandidateJSON: json.RawMessage(`{}`),
					Receipt: domain.AgentExecutionReceiptV1{
						RequestedRoute: &route,
					},
				}, nil
			},
			wantStage:    domain.AgentFailureStageResponseDecode,
			wantCategory: domain.AgentFailureCategoryExecutorMismatch,
		},
		{
			name: "candidate admission failure",
			execute: func(request domain.AgentExecutionRequestV1) (domain.AgentExecutionResponseV1, error) {
				return domain.AgentExecutionResponseV1{
					ContractVersion: domain.AgentExecutionContractVersion,
					CandidateJSON:   json.RawMessage(`{"ideas":[{"concept":"incomplete"}]}`),
					Receipt:         contentScoutDispatcherReceipt(request),
				}, nil
			},
			wantStage:    domain.AgentFailureStageAdmission,
			wantCategory: domain.AgentFailureCategoryCandidateInvalid,
			wantReceipt:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newContentScoutFixture(t, nil)
			store := newContentScoutDispatcherStore(t, fixture)
			executor := &contentScoutResultExecutor{execute: test.execute}

			result, err := (ContentScoutDispatcherV1{
				Store: store, Executor: executor, AllowRemote: true,
				Preflight: func(
					context.Context,
					AgentJobRecordV1,
					domain.AgentExecutionIdentity,
				) error {
					return nil
				},
				Now: dispatcherTestClock(fixture.job.CreatedAt),
			}).RunOnce(context.Background())
			if err != nil {
				t.Fatalf("dispatch invoked failure: %v", err)
			}
			if !result.Claimed ||
				result.Outcome != domain.AgentRunOutcomeFailed ||
				result.Disposition != domain.AgentExecutionDispositionInvoked ||
				result.FailureCategory != test.wantCategory ||
				len(result.ArtifactIDs) != 0 ||
				executor.calls != 1 ||
				store.completed != nil ||
				store.failed == nil ||
				store.failed.Result.Failure == nil ||
				store.failed.Result.Failure.Stage != test.wantStage ||
				store.failed.Result.Failure.Category != test.wantCategory ||
				(store.failed.Result.Receipt != nil) != test.wantReceipt ||
				len(store.failed.Result.Privacy.CompletedStages) != 2 ||
				len(store.failed.Result.ArtifactIDs) != 0 {
				t.Fatalf(
					"invoked failure result = %#v / %#v / %#v",
					result, store, executor,
				)
			}
		})
	}
}

type contentScoutDispatcherStore struct {
	reader    *contentScoutKnowledgeReader
	job       AgentJobRecordV1
	completed *V1JobCompletion
	failed    *V1AgentRunRecord
}

func newContentScoutDispatcherStore(
	t *testing.T,
	fixture contentScoutFixture,
) *contentScoutDispatcherStore {
	t.Helper()
	return &contentScoutDispatcherStore{
		reader: fixture.reader,
		job:    fixture.job,
	}
}

func (store *contentScoutDispatcherStore) LoadSemanticAnalysis(
	ctx context.Context,
	id string,
) (SemanticAnalysisRecord, error) {
	return store.reader.LoadSemanticAnalysis(ctx, id)
}

func (store *contentScoutDispatcherStore) LoadFactsByID(
	ctx context.Context,
	ids []string,
) ([]domain.Fact, error) {
	return store.reader.LoadFactsByID(ctx, ids)
}

func (store *contentScoutDispatcherStore) LoadFactAnalysis(
	ctx context.Context,
	id string,
) (domain.FactAnalysis, error) {
	return store.reader.LoadFactAnalysis(ctx, id)
}

func (store *contentScoutDispatcherStore) InspectOldestPendingV1Job(
	context.Context,
) (PendingV1JobRecord, bool, error) {
	if store.job.Status != domain.JobPending {
		return PendingV1JobRecord{}, false, nil
	}
	payload, err := json.Marshal(store.job.Payload)
	if err != nil {
		return PendingV1JobRecord{}, false, err
	}
	return PendingV1JobRecord{
		PendingV1JobIdentity: PendingV1JobIdentity{
			ID: store.job.ID, Fingerprint: store.job.Fingerprint,
			EventID: store.job.EventID, AgentName: store.job.Agent.Name,
			AgentVersion:         store.job.Agent.Version,
			PayloadSchemaVersion: store.job.Payload.SchemaVersion,
			ConfigurationDigest:  store.job.Payload.Configuration.Digest,
			PayloadJSON:          payload,
		},
		CreatedAt: store.job.CreatedAt,
	}, true, nil
}

func (store *contentScoutDispatcherStore) ClaimPendingV1Job(
	_ context.Context,
	expected PendingV1JobIdentity,
	startedAt time.Time,
) (bool, error) {
	pending, found, err := store.InspectOldestPendingV1Job(context.Background())
	if err != nil || !found {
		return false, err
	}
	if !reflect.DeepEqual(pending.PendingV1JobIdentity, expected) {
		return false, nil
	}
	store.job.Status = domain.JobRunning
	store.job.StartedAt = &startedAt
	return true, nil
}

func (store *contentScoutDispatcherStore) LoadV1Job(
	_ context.Context,
	id string,
) (AgentJobRecordV1, bool, error) {
	if id != store.job.ID {
		return AgentJobRecordV1{}, false, nil
	}
	return store.job, true, nil
}

func (store *contentScoutDispatcherStore) CompleteV1Job(
	ctx context.Context,
	completion V1JobCompletion,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if ValidateV1JobCompletion(completion) != nil {
		return false, errors.New("invalid completion")
	}
	store.completed = &completion
	store.job.Status = domain.JobSucceeded
	return true, nil
}

func (store *contentScoutDispatcherStore) FailV1Job(
	ctx context.Context,
	job AgentJobRecordV1,
	run V1AgentRunRecord,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if ValidateV1JobFailure(job, run) != nil {
		return false, errors.New("invalid failure")
	}
	store.failed = &run
	store.job.Status = domain.JobFailed
	return true, nil
}

type contentScoutDispatcherExecutor struct {
	calls int
}

func (executor *contentScoutDispatcherExecutor) Execute(
	context.Context,
	domain.AgentExecutionRequestV1,
	domain.StructuredOutputSchema,
) (domain.AgentExecutionResponseV1, error) {
	executor.calls++
	return domain.AgentExecutionResponseV1{}, errors.New("unexpected execution")
}

type contentScoutReceiptMismatchExecutor struct {
	calls  int
	mutate func(*domain.AgentExecutionReceiptV1)
}

func (executor *contentScoutReceiptMismatchExecutor) Execute(
	_ context.Context,
	request domain.AgentExecutionRequestV1,
	_ domain.StructuredOutputSchema,
) (domain.AgentExecutionResponseV1, error) {
	executor.calls++
	route := request.Configuration.Route
	receipt := domain.AgentExecutionReceiptV1{
		ExecutorKind:        request.Execution.ExecutorKind,
		ExecutorVersion:     request.Execution.ExecutorVersion,
		SessionID:           "executor-session",
		TurnID:              "executor-turn",
		CompletedModelSteps: 1,
		RequestedRoute:      &route,
	}
	executor.mutate(&receipt)
	return domain.AgentExecutionResponseV1{
		ContractVersion: domain.AgentExecutionContractVersion,
		CandidateJSON:   json.RawMessage(`{"ideas":[]}`),
		Receipt:         receipt,
	}, nil
}

type contentScoutResultExecutor struct {
	calls   int
	execute func(
		domain.AgentExecutionRequestV1,
	) (domain.AgentExecutionResponseV1, error)
}

func (executor *contentScoutResultExecutor) Execute(
	_ context.Context,
	request domain.AgentExecutionRequestV1,
	_ domain.StructuredOutputSchema,
) (domain.AgentExecutionResponseV1, error) {
	executor.calls++
	return executor.execute(request)
}

func contentScoutDispatcherReceipt(
	request domain.AgentExecutionRequestV1,
) domain.AgentExecutionReceiptV1 {
	route := request.Configuration.Route
	return domain.AgentExecutionReceiptV1{
		ExecutorKind:        request.Execution.ExecutorKind,
		ExecutorVersion:     request.Execution.ExecutorVersion,
		SessionID:           "executor-session",
		TurnID:              "executor-turn",
		CompletedModelSteps: 1,
		RequestedRoute:      &route,
	}
}

func dispatcherTestClock(createdAt time.Time) func() time.Time {
	current := createdAt
	return func() time.Time {
		current = current.Add(time.Minute)
		return current
	}
}

var _ ContentScoutDispatchStore = (*contentScoutDispatcherStore)(nil)
var _ AgentExecutor = (*contentScoutDispatcherExecutor)(nil)
var _ AgentExecutor = (*contentScoutReceiptMismatchExecutor)(nil)
var _ AgentExecutor = (*contentScoutResultExecutor)(nil)
