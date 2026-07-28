package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
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
	delete(fixture.reader.facts, "fact-one")
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
	_ context.Context,
	completion V1JobCompletion,
) (bool, error) {
	if ValidateV1JobCompletion(completion) != nil {
		return false, errors.New("invalid completion")
	}
	store.completed = &completion
	store.job.Status = domain.JobSucceeded
	return true, nil
}

func (store *contentScoutDispatcherStore) FailV1Job(
	_ context.Context,
	job AgentJobRecordV1,
	run V1AgentRunRecord,
) (bool, error) {
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

func dispatcherTestClock(createdAt time.Time) func() time.Time {
	current := createdAt
	return func() time.Time {
		current = current.Add(time.Minute)
		return current
	}
}

var _ ContentScoutDispatchStore = (*contentScoutDispatcherStore)(nil)
var _ AgentExecutor = (*contentScoutDispatcherExecutor)(nil)
