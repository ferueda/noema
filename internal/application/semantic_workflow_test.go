package application

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
)

type semanticWorkflowEvidenceSource struct {
	document domain.EvidenceDocument
	err      error
	reads    int
}

func (source *semanticWorkflowEvidenceSource) Read(context.Context, string) (domain.EvidenceDocument, error) {
	source.reads++
	return source.document, source.err
}

type semanticWorkflowFactStore struct {
	analysis domain.FactAnalysis
	err      error
	loads    int
}

func (*semanticWorkflowFactStore) FindCompletedFactAnalysis(context.Context, string) (domain.FactAnalysis, bool, error) {
	return domain.FactAnalysis{}, false, nil
}

func (*semanticWorkflowFactStore) CommitFactAnalysis(context.Context, domain.FactAnalysis) (bool, error) {
	return false, errors.New("not implemented")
}

func (*semanticWorkflowFactStore) RecordFailedAnalysis(context.Context, domain.AnalysisRun) error {
	return errors.New("not implemented")
}

func (store *semanticWorkflowFactStore) LoadFactAnalysis(context.Context, string) (domain.FactAnalysis, error) {
	store.loads++
	return store.analysis, store.err
}

type semanticWorkflowStore struct {
	attempt       *semanticWorkflowAttempt
	beginErr      error
	recordErr     error
	respectCancel bool
	begins        int
	recorded      []SemanticAnalysisRecord
}

func (store *semanticWorkflowStore) BeginSemanticAttempt(ctx context.Context) (SemanticAnalysisAttempt, error) {
	store.begins++
	if store.respectCancel && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if store.beginErr != nil {
		return nil, store.beginErr
	}
	if store.attempt == nil {
		store.attempt = &semanticWorkflowAttempt{}
	}
	return store.attempt, nil
}

func (*semanticWorkflowStore) LoadSemanticAnalysis(context.Context, string) (SemanticAnalysisRecord, error) {
	return SemanticAnalysisRecord{}, errors.New("not implemented")
}

func (*semanticWorkflowStore) AnalysisStage(context.Context, string) (string, error) {
	return "", errors.New("not implemented")
}

func (store *semanticWorkflowStore) RecordSemanticFailure(
	ctx context.Context,
	record SemanticAnalysisRecord,
) error {
	if store.respectCancel && ctx.Err() != nil {
		return ctx.Err()
	}
	if store.recordErr != nil {
		return store.recordErr
	}
	store.recorded = append(store.recorded, record)
	return nil
}

type semanticWorkflowAttempt struct {
	existing      SemanticAnalysisRecord
	found         bool
	expectedKey   string
	findErr       error
	commitErr     error
	failureErr    error
	respectCancel bool
	findKeys      []string
	committed     []SemanticAnalysisRecord
	failedRuns    []domain.AnalysisRun
	failedDetails []SemanticAnalysisDetails
	rollbacks     int
}

func (attempt *semanticWorkflowAttempt) FindCompleted(
	_ context.Context,
	processingKey string,
) (SemanticAnalysisRecord, bool, error) {
	attempt.findKeys = append(attempt.findKeys, processingKey)
	if attempt.findErr != nil {
		return SemanticAnalysisRecord{}, false, attempt.findErr
	}
	if attempt.expectedKey != "" && processingKey != attempt.expectedKey {
		return SemanticAnalysisRecord{}, false, errors.New("unexpected processing key")
	}
	return attempt.existing, attempt.found, nil
}

func (attempt *semanticWorkflowAttempt) Commit(ctx context.Context, record SemanticAnalysisRecord) error {
	if attempt.respectCancel && ctx.Err() != nil {
		return ctx.Err()
	}
	if attempt.commitErr != nil {
		return attempt.commitErr
	}
	attempt.committed = append(attempt.committed, record)
	return nil
}

func (attempt *semanticWorkflowAttempt) RecordFailure(
	ctx context.Context,
	run domain.AnalysisRun,
	details SemanticAnalysisDetails,
) error {
	if attempt.respectCancel && ctx.Err() != nil {
		return ctx.Err()
	}
	if attempt.failureErr != nil {
		return attempt.failureErr
	}
	attempt.failedRuns = append(attempt.failedRuns, run)
	attempt.failedDetails = append(attempt.failedDetails, details)
	return nil
}

func (attempt *semanticWorkflowAttempt) Rollback(context.Context) error {
	attempt.rollbacks++
	return nil
}

func TestSemanticWorkflowRejectsInvalidRouteBeforeLoadingFacts(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	facts := &semanticWorkflowFactStore{analysis: analysis}
	source := &semanticWorkflowEvidenceSource{document: document}
	store := &semanticWorkflowStore{}
	generator := &recordingSemanticGenerator{}
	workflow := semanticTestWorkflow(source, facts, store, generator, nil)
	route := semanticTestRoute()
	route.Requested.Alias = "unknown"

	_, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          route,
	})
	if !errors.Is(err, ErrSemanticInputInvalid) {
		t.Fatalf("error = %v, want ErrSemanticInputInvalid", err)
	}
	if facts.loads != 0 || source.reads != 0 || store.begins != 0 || len(generator.requests) != 0 {
		t.Fatalf("invalid route crossed the load boundary: facts=%d source=%d begins=%d generation=%d",
			facts.loads, source.reads, store.begins, len(generator.requests))
	}
}

func TestSemanticWorkflowRecordsDigestMismatchWithOnlyEarlyDetails(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	document.Revision.DocumentDigest.Digest = strings.Repeat("e", 64)
	store := &semanticWorkflowStore{}
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		store,
		&recordingSemanticGenerator{},
		func() (string, error) { return "semantic-digest-mismatch", nil },
	)

	_, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	assertAnalysisFailure(t, err, "semantic-digest-mismatch", "source-revision-unavailable")
	if len(store.recorded) != 1 {
		t.Fatalf("recorded failures = %d, want 1", len(store.recorded))
	}
	record := store.recorded[0]
	if record.Details.Schema.Digest == "" || !reflect.DeepEqual(record.Details.Route, semanticTestRoute()) {
		t.Fatalf("early schema/route details = %#v", record.Details)
	}
	assertNoSemanticProgressAfterRoute(t, record.Details)
}

func TestSemanticWorkflowPreflightBlockPreservesEstablishedDetails(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 24)
	analysis, document := semanticAnalysisFixture(t, "Inspect "+secret)
	store := &semanticWorkflowStore{}
	generator := &recordingSemanticGenerator{}
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		store,
		generator,
		func() (string, error) { return "semantic-preflight", nil },
	)

	_, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	assertAnalysisFailure(t, err, "semantic-preflight", "semantic-preflight-blocked")
	if len(store.recorded) != 1 || len(generator.requests) != 0 {
		t.Fatalf("recorded/generation = %d/%d, want 1/0", len(store.recorded), len(generator.requests))
	}
	details := store.recorded[0].Details
	if details.InputFactIDs == nil || !reflect.DeepEqual(*details.InputFactIDs, []string{"fact-command"}) ||
		details.Privacy == nil || len(details.Privacy.BlockedCategories) == 0 {
		t.Fatalf("preflight progress = %#v", details)
	}
	if details.Selection != nil || details.InputDigest != nil || details.AttemptedProcessingKey != nil ||
		details.Model != nil || details.ClaimIDs != nil {
		t.Fatalf("preflight invented later details: %#v", details)
	}
}

func TestSemanticWorkflowReusesExactCompletedAnalysisWithoutGenerationOrIdentity(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	generator := &recordingSemanticGenerator{}
	analyzer := semanticTestAnalyzer(generator)
	prepared, err := analyzer.prepare(SemanticAnalysisRequest{
		FactAnalysis: analysis,
		Document:     document,
		Route:        semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("prepare expected identity: %v", err)
	}
	existing := SemanticAnalysisRecord{Analysis: domain.SemanticAnalysis{Run: domain.AnalysisRun{
		ID: "existing-semantic", ProcessingKey: *prepared.ProcessingKey,
		Stage: domain.AnalysisStageClaims, Status: domain.AnalysisCompleted,
	}}}
	attempt := &semanticWorkflowAttempt{existing: existing, found: true, expectedKey: *prepared.ProcessingKey}
	store := &semanticWorkflowStore{attempt: attempt}
	idCalls := 0
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		store,
		generator,
		func() (string, error) {
			idCalls++
			return "unexpected-new-id", nil
		},
	)

	result, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("reuse semantic analysis: %v", err)
	}
	if !result.Reused || result.Record.Analysis.Run.ID != "existing-semantic" ||
		len(generator.requests) != 0 || idCalls != 0 || len(attempt.committed) != 0 || len(attempt.failedRuns) != 0 {
		t.Fatalf("reuse result/calls = %#v, generation=%d ids=%d commits=%d failures=%d",
			result, len(generator.requests), idCalls, len(attempt.committed), len(attempt.failedRuns))
	}
}

func TestSemanticWorkflowCommitsOrderedClaimsEventsAndDetails(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "Record the result.")
	generator := &recordingSemanticGenerator{generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
		first := request.Input.Entries[0].Segments[0].EvidenceID
		second := request.Input.Entries[1].Segments[0].EvidenceID
		return SemanticGenerationResult{
			Candidates: []domain.ClaimCandidate{
				{Type: domain.ClaimTypeProblem, Statement: "The behavior required inspection.", Status: domain.ClaimStatusObserved,
					Confidence: 0.9, SupportingEvidenceIDs: []string{first}},
				{Type: domain.ClaimTypeLesson, Statement: "A bounded record makes the result reusable.", Status: domain.ClaimStatusInferred,
					Confidence: 0.8, SupportingEvidenceIDs: []string{second}, SupportingFactIDs: []string{"fact-command"}},
			},
			Model: semanticModelMetadata(),
		}, nil
	}}
	attempt := &semanticWorkflowAttempt{}
	store := &semanticWorkflowStore{attempt: attempt}
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		store,
		generator,
		func() (string, error) { return "semantic-success", nil },
	)

	result, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("run semantic workflow: %v", err)
	}
	if result.Reused || len(attempt.committed) != 1 || !reflect.DeepEqual(result.Record, attempt.committed[0]) {
		t.Fatalf("committed result = %#v / %#v", result, attempt.committed)
	}
	record := result.Record
	if len(record.Analysis.Claims) != 2 || record.Analysis.Claims[0].Type != domain.ClaimTypeProblem ||
		record.Analysis.Claims[1].Type != domain.ClaimTypeLesson ||
		!reflect.DeepEqual(record.Analysis.Run.ClaimIDs, []string{record.Analysis.Claims[0].ID, record.Analysis.Claims[1].ID}) {
		t.Fatalf("ordered claims = %#v", record.Analysis)
	}
	if record.Details.InputFactIDs == nil || !reflect.DeepEqual(*record.Details.InputFactIDs, []string{"fact-command"}) ||
		record.Details.ClaimIDs == nil || !reflect.DeepEqual(*record.Details.ClaimIDs, record.Analysis.Run.ClaimIDs) ||
		record.Details.InputDigest == nil || record.Details.Selection == nil || record.Details.Privacy == nil ||
		record.Details.Model == nil || record.Details.AttemptedProcessingKey != nil {
		t.Fatalf("completed semantic details = %#v", record.Details)
	}
	if len(record.Events) != 3 || record.Events[0].Type != "claim.admitted" ||
		record.Events[0].SubjectType != "claim" || record.Events[0].SubjectID != record.Analysis.Claims[0].ID ||
		record.Events[1].SubjectID != record.Analysis.Claims[1].ID || record.Events[2].Type != "analysis.completed" ||
		record.Events[2].SubjectType != "analysis" || record.Events[2].SubjectID != record.Analysis.Run.ID {
		t.Fatalf("ordered semantic events = %#v", record.Events)
	}
}

func TestSemanticWorkflowPreservesKnownEmptyClaimIDs(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	generator := &recordingSemanticGenerator{generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
		return SemanticGenerationResult{Candidates: []domain.ClaimCandidate{}, Model: semanticModelMetadata()}, nil
	}}
	attempt := &semanticWorkflowAttempt{}
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		&semanticWorkflowStore{attempt: attempt},
		generator,
		func() (string, error) { return "semantic-empty", nil },
	)

	result, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("run empty semantic workflow: %v", err)
	}
	if result.Record.Details.ClaimIDs == nil || len(*result.Record.Details.ClaimIDs) != 0 ||
		result.Record.Analysis.Run.ClaimIDs == nil || len(result.Record.Analysis.Run.ClaimIDs) != 0 ||
		len(result.Record.Analysis.Claims) != 0 || len(result.Record.Events) != 1 {
		t.Fatalf("known-empty claims = %#v", result.Record)
	}
}

func TestSemanticWorkflowGenerationFailureKeepsAttemptedKeyWithoutModel(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	generator := &recordingSemanticGenerator{generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
		return SemanticGenerationResult{}, errors.New("upstream unavailable")
	}}
	attempt := &semanticWorkflowAttempt{}
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		&semanticWorkflowStore{attempt: attempt},
		generator,
		func() (string, error) { return "semantic-generation-failure", nil },
	)

	_, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	assertAnalysisFailure(t, err, "semantic-generation-failure", "semantic-generation-failed")
	if len(attempt.failedRuns) != 1 || len(attempt.failedDetails) != 1 {
		t.Fatalf("failed attempts = %#v / %#v", attempt.failedRuns, attempt.failedDetails)
	}
	details := attempt.failedDetails[0]
	if details.AttemptedProcessingKey == nil || *details.AttemptedProcessingKey == "" || details.Model != nil || details.ClaimIDs != nil {
		t.Fatalf("generation failure details = %#v", details)
	}
}

func TestSemanticWorkflowPostflightFailureKeepsModelWithoutClaimIDs(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	generator := &recordingSemanticGenerator{generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
		return SemanticGenerationResult{
			Candidates: []domain.ClaimCandidate{{
				Type: domain.ClaimTypeLesson, Statement: "Read /Users/example/private/notes.txt",
				Status: domain.ClaimStatusInferred, Confidence: 0.8,
				SupportingEvidenceIDs: []string{request.Input.Entries[0].Segments[0].EvidenceID},
			}},
			Model: semanticModelMetadata(),
		}, nil
	}}
	attempt := &semanticWorkflowAttempt{}
	workflow := semanticTestWorkflow(
		&semanticWorkflowEvidenceSource{document: document},
		&semanticWorkflowFactStore{analysis: analysis},
		&semanticWorkflowStore{attempt: attempt},
		generator,
		func() (string, error) { return "semantic-postflight", nil },
	)

	_, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
		FactAnalysisID: analysis.Run.ID,
		Route:          semanticTestRoute(),
	})
	assertAnalysisFailure(t, err, "semantic-postflight", "semantic-postflight-blocked")
	if len(attempt.failedDetails) != 1 || attempt.failedDetails[0].Model == nil ||
		attempt.failedDetails[0].AttemptedProcessingKey == nil || attempt.failedDetails[0].ClaimIDs != nil {
		t.Fatalf("postflight failure details = %#v", attempt.failedDetails)
	}
}

func TestSemanticWorkflowDoesNotExposeAnalysisIDWhenDurabilityFails(t *testing.T) {
	t.Run("failure record", func(t *testing.T) {
		analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
		attempt := &semanticWorkflowAttempt{failureErr: errors.New("disk unavailable")}
		workflow := semanticTestWorkflow(
			&semanticWorkflowEvidenceSource{document: document},
			&semanticWorkflowFactStore{analysis: analysis},
			&semanticWorkflowStore{attempt: attempt},
			&recordingSemanticGenerator{generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
				return SemanticGenerationResult{}, errors.New("upstream unavailable")
			}},
			func() (string, error) { return "not-durable-failure", nil },
		)

		_, err := workflow.Run(context.Background(), SemanticWorkflowRequest{
			FactAnalysisID: analysis.Run.ID,
			Route:          semanticTestRoute(),
		})
		assertNoInspectableAnalysisID(t, err, "not-durable-failure")
	})

	t.Run("canceled commit", func(t *testing.T) {
		analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
		attempt := &semanticWorkflowAttempt{respectCancel: true}
		ctx, cancel := context.WithCancel(context.Background())
		generator := &recordingSemanticGenerator{generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
			cancel()
			return SemanticGenerationResult{Candidates: []domain.ClaimCandidate{}, Model: semanticModelMetadata()}, nil
		}}
		workflow := semanticTestWorkflow(
			&semanticWorkflowEvidenceSource{document: document},
			&semanticWorkflowFactStore{analysis: analysis},
			&semanticWorkflowStore{attempt: attempt},
			generator,
			func() (string, error) { return "not-durable-success", nil },
		)

		_, err := workflow.Run(ctx, SemanticWorkflowRequest{
			FactAnalysisID: analysis.Run.ID,
			Route:          semanticTestRoute(),
		})
		assertNoInspectableAnalysisID(t, err, "not-durable-success")
		if len(attempt.committed) != 0 {
			t.Fatalf("committed records = %d, want 0", len(attempt.committed))
		}
	})
}

func TestBuildSemanticEventsHasStableIdentities(t *testing.T) {
	analysis := domain.SemanticAnalysis{
		Run: domain.AnalysisRun{
			ID: "semantic-stable", ClaimIDs: []string{"claim-stable"},
			FinishedAt: semanticTestAnalyzer(nil).now(),
		},
		Claims: []domain.Claim{{
			ID: "claim-stable", CreatedAt: semanticTestAnalyzer(nil).now(),
			SupportingEvidence: []domain.EvidenceRef{{ID: "evidence-stable"}},
		}},
	}

	first, err := buildSemanticEvents(analysis)
	if err != nil {
		t.Fatalf("build first events: %v", err)
	}
	second, err := buildSemanticEvents(analysis)
	if err != nil {
		t.Fatalf("build second events: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("event counts = %d/%d, want 2/2", len(first), len(second))
	}
	for index := range first {
		if first[index].ID == "" || first[index].Fingerprint == "" ||
			first[index].ID != second[index].ID || first[index].Fingerprint != second[index].Fingerprint {
			t.Fatalf("event %d identity changed: %#v / %#v", index, first[index], second[index])
		}
	}
}

func semanticTestWorkflow(
	source SessionEvidenceSource,
	facts FactAnalysisStore,
	store SemanticAnalysisStore,
	generator SemanticGenerator,
	newID IDGenerator,
) SemanticWorkflow {
	analyzer := semanticTestAnalyzer(generator)
	if newID != nil {
		analyzer.NewID = newID
	}
	return SemanticWorkflow{Source: source, Facts: facts, Store: store, Analyzer: analyzer}
}

func assertAnalysisFailure(t *testing.T, err error, analysisID, category string) {
	t.Helper()
	var failure AnalysisError
	if !errors.As(err, &failure) || failure.AnalysisID != analysisID || failure.Category != category {
		t.Fatalf("error = %v, want analysis %q category %q", err, analysisID, category)
	}
}

func assertNoSemanticProgressAfterRoute(t *testing.T, details SemanticAnalysisDetails) {
	t.Helper()
	if details.InputFactIDs != nil || details.ClaimIDs != nil || details.InputDigest != nil ||
		details.Selection != nil || details.Privacy != nil || details.Model != nil || details.AttemptedProcessingKey != nil {
		t.Fatalf("early failure invented semantic progress: %#v", details)
	}
}

func assertNoInspectableAnalysisID(t *testing.T, err error, analysisID string) {
	t.Helper()
	var failure AnalysisError
	if err == nil || errors.As(err, &failure) || strings.Contains(err.Error(), analysisID) {
		t.Fatalf("error = %v, want persistence failure without inspectable analysis id", err)
	}
}
