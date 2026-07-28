package application

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

type subscriptionMatcherStore struct {
	record SemanticAnalysisRecord
	jobs   map[string]AgentJobRecordV1
}

func (store *subscriptionMatcherStore) LoadSemanticAnalysis(
	context.Context,
	string,
) (SemanticAnalysisRecord, error) {
	return store.record, nil
}

func (store *subscriptionMatcherStore) CreateOrReuseV1Job(
	_ context.Context,
	job AgentJobRecordV1,
) (bool, error) {
	if store.jobs == nil {
		store.jobs = map[string]AgentJobRecordV1{}
	}
	if _, exists := store.jobs[job.Fingerprint]; exists {
		return false, nil
	}
	store.jobs[job.Fingerprint] = job
	return true, nil
}

func TestSubscriptionMatcherCreatesAndReusesContentScoutJob(t *testing.T) {
	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	store := &subscriptionMatcherStore{record: contentScoutCompletionForTest(t, now, []string{"claim-one", "claim-two"})}
	matcher := SubscriptionMatcher{Store: store, Now: func() time.Time { return now.Add(time.Hour) }}
	configuration := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(strings.Repeat("a", 64)),
		`{"schemaVersion":1,"approvedPublicTerms":[]}`,
	)

	first, err := matcher.MatchContentScout(context.Background(), "analysis-one", configuration)
	if err != nil {
		t.Fatalf("match Content Scout: %v", err)
	}
	second, err := matcher.MatchContentScout(context.Background(), "analysis-one", configuration)
	if err != nil {
		t.Fatalf("reuse Content Scout: %v", err)
	}
	if len(first.Jobs) != 1 || !first.Jobs[0].Created ||
		len(second.Jobs) != 1 || second.Jobs[0].Created ||
		first.Jobs[0].ID != second.Jobs[0].ID ||
		first.EventID != store.record.Events[0].ID ||
		len(store.jobs) != 1 {
		t.Fatalf("unexpected match results: %#v / %#v / %#v", first, second, store.jobs)
	}
	var job AgentJobRecordV1
	for _, stored := range store.jobs {
		job = stored
	}
	if !slices.Equal(job.Payload.Inputs.ClaimIDs, []string{"claim-one", "claim-two"}) ||
		job.Payload.Inputs.AnalysisRunID != "analysis-one" ||
		job.EventID != first.EventID {
		t.Fatalf("stored job = %#v", job)
	}

	changed := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(strings.Repeat("b", 64)),
		`{"schemaVersion":1,"approvedPublicTerms":[]}`,
	)
	third, err := matcher.MatchContentScout(context.Background(), "analysis-one", changed)
	if err != nil {
		t.Fatalf("match changed Content Scout configuration: %v", err)
	}
	if len(third.Jobs) != 1 || !third.Jobs[0].Created ||
		third.Jobs[0].ID == first.Jobs[0].ID || len(store.jobs) != 2 {
		t.Fatalf("changed configuration result = %#v", third)
	}
}

func TestSubscriptionMatcherAcceptsZeroClaimCompletion(t *testing.T) {
	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	store := &subscriptionMatcherStore{record: contentScoutCompletionForTest(t, now, []string{})}
	matcher := SubscriptionMatcher{Store: store, Now: func() time.Time { return now }}
	result, err := matcher.MatchContentScout(
		context.Background(),
		"analysis-one",
		loadContentScoutConfigurationForTest(
			t,
			contentScoutAgentJSON(strings.Repeat("a", 64)),
			`{"schemaVersion":1,"approvedPublicTerms":[]}`,
		),
	)
	if err != nil {
		t.Fatalf("match zero-claim completion: %v", err)
	}
	if len(result.Jobs) != 1 || !result.Jobs[0].Created {
		t.Fatalf("zero-claim result = %#v", result)
	}
}

func TestSubscriptionMatcherRejectsInvalidCompletionBeforeWriting(t *testing.T) {
	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	configuration := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(strings.Repeat("a", 64)),
		`{"schemaVersion":1,"approvedPublicTerms":[]}`,
	)
	tests := map[string]func(*SemanticAnalysisRecord){
		"extra completion event": func(record *SemanticAnalysisRecord) {
			record.Events = append(record.Events, record.Events[0])
		},
		"changed ordered claims": func(record *SemanticAnalysisRecord) {
			record.Events[0].Payload["claimIds"] = []string{"claim-two", "claim-one"}
			refreshEventIdentity(t, &record.Events[0])
		},
		"unknown payload field": func(record *SemanticAnalysisRecord) {
			record.Events[0].Payload["extra"] = true
			refreshEventIdentity(t, &record.Events[0])
		},
		"changed event identity": func(record *SemanticAnalysisRecord) {
			record.Events[0].ID = "different-event"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			record := contentScoutCompletionForTest(t, now, []string{"claim-one", "claim-two"})
			mutate(&record)
			store := &subscriptionMatcherStore{record: record}
			matcher := SubscriptionMatcher{Store: store, Now: func() time.Time { return now }}
			if _, err := matcher.MatchContentScout(
				context.Background(), "analysis-one", configuration,
			); err == nil {
				t.Fatal("invalid completion was accepted")
			}
			if len(store.jobs) != 0 {
				t.Fatalf("jobs were written: %#v", store.jobs)
			}
		})
	}
}

func TestValidateAgentJobRecordV1RejectsChangedIdentity(t *testing.T) {
	now := time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
	configuration := loadContentScoutConfigurationForTest(
		t,
		contentScoutAgentJSON(strings.Repeat("a", 64)),
		`{"schemaVersion":1,"approvedPublicTerms":[]}`,
	)
	payload := domain.AgentJobPayloadV1{
		SchemaVersion: domain.AgentJobPayloadSchemaVersion,
		Inputs: domain.KnowledgeInputRefsV1{
			AnalysisRunID: "analysis-one", ClaimIDs: []string{"claim-one"},
		},
		Configuration: configuration.Identity,
	}
	fingerprint, err := domain.AgentJobFingerprint("event-one", configuration.Agent, payload)
	if err != nil {
		t.Fatalf("fingerprint job: %v", err)
	}
	job := AgentJobRecordV1{
		ID: platform.DerivedID("job_", fingerprint), Fingerprint: fingerprint,
		EventID: "event-one", Agent: configuration.Agent, Status: domain.JobPending,
		Payload: payload, CreatedAt: now,
	}
	if err := ValidateAgentJobRecordV1(job); err != nil {
		t.Fatalf("valid job rejected: %v", err)
	}
	job.ID = "different-job"
	if err := ValidateAgentJobRecordV1(job); err == nil {
		t.Fatal("changed job identity was accepted")
	}
}

func contentScoutCompletionForTest(
	t *testing.T,
	now time.Time,
	claimIDs []string,
) SemanticAnalysisRecord {
	t.Helper()
	claims := make([]domain.Claim, len(claimIDs))
	for index, claimID := range claimIDs {
		claims[index] = domain.Claim{ID: claimID, AnalysisRunID: "analysis-one"}
	}
	event := domain.Event{
		Type:        ContentScoutEventType,
		SubjectType: "analysis",
		SubjectID:   "analysis-one",
		Payload: map[string]any{
			"schemaVersion": 1,
			"analysisId":    "analysis-one",
			"claimIds":      append([]string{}, claimIDs...),
		},
		Evidence:  []domain.EvidenceRef{},
		CreatedAt: now,
	}
	refreshEventIdentity(t, &event)
	detailsClaims := append([]string{}, claimIDs...)
	return SemanticAnalysisRecord{
		Analysis: domain.SemanticAnalysis{
			Run: domain.AnalysisRun{
				ID:         "analysis-one",
				Stage:      domain.AnalysisStageClaims,
				Status:     domain.AnalysisCompleted,
				ClaimIDs:   append([]string{}, claimIDs...),
				FinishedAt: now,
			},
			Claims: claims,
		},
		Details: SemanticAnalysisDetails{ClaimIDs: &detailsClaims},
		Events:  []domain.Event{event},
	}
}

func refreshEventIdentity(t *testing.T, event *domain.Event) {
	t.Helper()
	fingerprint, err := EventFingerprint(
		event.Type, event.SubjectType, event.SubjectID, event.Payload,
	)
	if err != nil {
		t.Fatalf("fingerprint event: %v", err)
	}
	event.Fingerprint = fingerprint
	event.ID = platform.DerivedID("evt_", fingerprint)
}
