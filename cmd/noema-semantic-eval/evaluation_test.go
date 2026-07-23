package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/adapters/aigateway"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

type fakeGenerator struct {
	calls    int
	generate func(int, application.SemanticGenerationRequest) (application.SemanticGenerationResult, error)
}

func (generator *fakeGenerator) Generate(
	_ context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	call := generator.calls
	generator.calls++
	if generator.generate != nil {
		return generator.generate(call, request)
	}
	return fakeGeneration(call, request, nil), nil
}

type categorizedGenerationError string

func (failure categorizedGenerationError) Error() string {
	return "synthetic generation failure"
}

func (failure categorizedGenerationError) SemanticGenerationFailureCategory() string {
	return string(failure)
}

func TestReviewedCorporaPassProductionPreflight(t *testing.T) {
	for _, path := range []string{testCorpusPath(), testCorpusV2Path()} {
		corpus := testCorpusAtPath(t, path)
		if len(corpus.Cases) != corpus.Profile.CaseCount {
			t.Fatalf("%s case count = %d, want %d", path, len(corpus.Cases), corpus.Profile.CaseCount)
		}
		route := testRoute(t)
		if err := preflightCorpus(corpus, route.Validated()); err != nil {
			t.Fatalf("preflight corpus %s: %v", path, err)
		}
		for _, fixture := range corpus.Cases {
			if len(fixture.Document.Entries) == 0 ||
				fixture.FactAnalysis.Run.Revision == nil ||
				fixture.FactAnalysis.Run.Revision.Identity() != fixture.Document.Revision.Identity() {
				t.Fatalf("invalid fixture lineage for %s", fixture.Definition.ID)
			}
		}
	}
}

func TestCorpusV2PreservesV1AndAddsReviewedCases(t *testing.T) {
	var v1, v2 corpusFile
	for _, input := range []struct {
		path   string
		target *corpusFile
	}{
		{path: testCorpusPath(), target: &v1},
		{path: testCorpusV2Path(), target: &v2},
	} {
		content, err := os.ReadFile(input.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := decodeStrictJSON(content, input.target); err != nil {
			t.Fatalf("decode %s: %v", input.path, err)
		}
	}
	if len(v1.Cases) != 12 || len(v2.Cases) != 20 ||
		!reflect.DeepEqual(v1.Cases, v2.Cases[:len(v1.Cases)]) {
		t.Fatal("V2 does not preserve the 12 V1 cases exactly")
	}
	expected := map[string]bool{
		"mixed-verification-scope":         true,
		"confirmed-root-cause":             true,
		"unrelated-concurrent-problems":    true,
		"reverted-solution":                true,
		"later-failure-after-success":      true,
		"decision-without-implementation":  true,
		"implementation-without-rationale": true,
		"prompt-injection-evidence":        true,
	}
	for _, definition := range v2.Cases[len(v1.Cases):] {
		if !expected[definition.ID] || len(definition.HumanCriteria) != 1 {
			t.Fatalf("unexpected V2 case contract: %#v", definition)
		}
		delete(expected, definition.ID)
	}
	if len(expected) != 0 {
		t.Fatalf("missing V2 cases: %#v", expected)
	}
}

func TestRunRejectsWrongCorpusDigestBeforeGeneratorConstruction(t *testing.T) {
	content, err := os.ReadFile(testCorpusPath())
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, '\n')
	path := filepath.Join(t.TempDir(), "changed-corpus.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_GATEWAY_API_KEY", "test-key")
	calls := 0
	dependencies := commandDependencies{
		newGenerator: func(aigateway.Route, string) (application.SemanticGenerator, error) {
			calls++
			return &fakeGenerator{}, nil
		},
		now: time.Now,
	}
	var stdout, stderr bytes.Buffer
	temp := t.TempDir()
	err = run(context.Background(), []string{
		"run", "--corpus", path, "--allow-remote",
		"--route-config", testRoutePath(),
		"--output", filepath.Join(temp, "report.json"),
		"--review-output", filepath.Join(temp, "review.json"),
	}, &stdout, &stderr, dependencies)
	if err == nil || calls != 0 {
		t.Fatalf("error = %v, generator constructions = %d", err, calls)
	}
}

func TestEvaluationReportsAdmittedAndEmptyBatchesWithExactAggregates(t *testing.T) {
	corpus := testCorpus(t)
	caseCount := len(corpus.Cases)
	route := testRoute(t).Validated()
	generator := &fakeGenerator{generate: func(
		call int,
		request application.SemanticGenerationRequest,
	) (application.SemanticGenerationResult, error) {
		var candidates []domain.ClaimCandidate
		if call == 1 {
			candidates = []domain.ClaimCandidate{{
				Type:      domain.ClaimTypeProblem,
				Statement: "The compile check exposed an unavailable parser package.",
				Status:    domain.ClaimStatusObserved, Confidence: 0.95,
				SupportingEvidenceIDs: []string{request.Input.Entries[1].Segments[0].EvidenceID},
				Attribution:           domain.ClaimAttributionUnknown,
			}}
		}
		return fakeGeneration(call, request, candidates), nil
	}}
	clock := testClock()
	report := executeEvaluation(context.Background(), corpus, route, generator, clock)
	if !report.Complete || len(report.Cases) != caseCount || generator.calls != caseCount {
		t.Fatalf("complete/cases/calls = %v/%d/%d", report.Complete, len(report.Cases), generator.calls)
	}
	if report.Cases[1].CandidateCount != 1 || report.Cases[1].AdmittedCount != 1 ||
		len(report.Cases[1].Evidence) != 1 || report.Aggregates.ValidBatchCount != caseCount ||
		report.Aggregates.EmptyBatchCount != caseCount-1 {
		t.Fatalf("unexpected batch results: %#v / %#v", report.Cases[1], report.Aggregates)
	}
	if report.Aggregates.TotalCostUSD == nil || *report.Aggregates.TotalCostUSD != "0.0012" ||
		report.Aggregates.AverageCostUSD == nil || *report.Aggregates.AverageCostUSD != "0.0001" ||
		report.Aggregates.TokenMetadataCount != caseCount ||
		report.Aggregates.LatencyMetadataCount != caseCount {
		t.Fatalf("unexpected aggregates: %#v", report.Aggregates)
	}
	for _, result := range report.Cases {
		if len(result.MachineExpectations) != len(corpus.Cases[resultIndex(corpus, result.ID)].Definition.MachineExpectations) {
			t.Fatalf("machine expectation result mismatch for %s", result.ID)
		}
	}
	review, err := buildReviewTemplate(report, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.ClaimReviews) != 1 || len(review.CaseCriteria) != caseCount {
		t.Fatalf("review template sizes = %d/%d", len(review.ClaimReviews), len(review.CaseCriteria))
	}
}

func TestEvaluationUsesExactStopAndContinueCategories(t *testing.T) {
	caseCount := len(testCorpus(t).Cases)
	for _, test := range []struct {
		name         string
		category     string
		wantCalls    int
		wantComplete bool
	}{
		{
			name: "timeout continues", category: application.SemanticGenerationFailureTimeout,
			wantCalls: caseCount, wantComplete: true,
		},
		{
			name: "authentication stops", category: application.SemanticGenerationFailureAuthentication,
			wantCalls: 1, wantComplete: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			generator := &fakeGenerator{generate: func(
				call int,
				request application.SemanticGenerationRequest,
			) (application.SemanticGenerationResult, error) {
				if call == 0 {
					return application.SemanticGenerationResult{}, categorizedGenerationError(test.category)
				}
				return fakeGeneration(call, request, nil), nil
			}}
			report := executeEvaluation(
				context.Background(), testCorpus(t), testRoute(t).Validated(), generator, testClock(),
			)
			if generator.calls != test.wantCalls || report.Complete != test.wantComplete ||
				report.Cases[0].FailureCategory != test.category {
				t.Fatalf("calls/complete/category = %d/%v/%q", generator.calls, report.Complete, report.Cases[0].FailureCategory)
			}
			if test.category == application.SemanticGenerationFailureAuthentication {
				if len(report.Cases) != caseCount ||
					report.Cases[1].Status != "not-attempted" ||
					report.Aggregates.ExpectationCount != 8 ||
					report.Aggregates.EvaluatedExpectations != 0 ||
					report.Aggregates.ExpectationCoverage != (countRatio{Numerator: 0, Denominator: 8}) {
					t.Fatalf("incomplete expectation coverage = %#v / %#v", report.Cases, report.Aggregates)
				}
			}
		})
	}
}

func TestEvaluationStopCategoriesAreExact(t *testing.T) {
	for _, category := range []string{
		application.SemanticGenerationFailureAuthentication,
		application.SemanticGenerationFailurePermission,
		application.SemanticGenerationFailureRateLimited,
		application.SemanticGenerationFailureRequest,
		application.SemanticGenerationFailureSchema,
		application.SemanticGenerationFailureUpstream,
		application.SemanticGenerationFailureTransport,
		application.SemanticGenerationFailureGeneric,
	} {
		if !stopsEvaluation(category) {
			t.Fatalf("category %q should stop the corpus", category)
		}
	}
	for _, category := range []string{
		application.SemanticGenerationFailureContext,
		application.SemanticGenerationFailureContent,
		application.SemanticGenerationFailureTimeout,
		application.SemanticGenerationFailureResponse,
		"semantic-postflight-blocked",
		"claim-evidence-required",
		"claim-outcome-unsupported",
		"semantic-admission-invalid",
	} {
		if stopsEvaluation(category) {
			t.Fatalf("category %q should remain case-local", category)
		}
	}
}

func TestEvaluationContinuesAfterAdmissionFailureAndScoresPartialReviews(t *testing.T) {
	generator := &fakeGenerator{generate: func(
		call int,
		request application.SemanticGenerationRequest,
	) (application.SemanticGenerationResult, error) {
		if call == 0 {
			return fakeGeneration(call, request, []domain.ClaimCandidate{{
				Type: domain.ClaimTypeLesson, Statement: "The user should inspect timing data.",
				Status: domain.ClaimStatusInferred, Confidence: 0.7,
				SupportingEvidenceIDs: []string{request.Input.Entries[0].Segments[0].EvidenceID},
			}}), nil
		}
		var candidates []domain.ClaimCandidate
		if call == 1 {
			candidates = []domain.ClaimCandidate{{
				Type:      domain.ClaimTypeProblem,
				Statement: "The compile check exposed an unavailable parser package.",
				Status:    domain.ClaimStatusObserved, Confidence: 0.9,
				SupportingEvidenceIDs: []string{request.Input.Entries[1].Segments[0].EvidenceID},
			}}
		}
		return fakeGeneration(call, request, candidates), nil
	}}
	corpus := testCorpus(t)
	caseCount := len(corpus.Cases)
	report := executeEvaluation(
		context.Background(), corpus, testRoute(t).Validated(), generator, testClock(),
	)
	if !report.Complete || generator.calls != caseCount ||
		report.Cases[0].FailureCategory != "claim-free-text-attribution-invalid" {
		t.Fatalf("admission continuation failed: %#v", report)
	}
	reviews, err := buildReviewTemplate(report, corpus)
	if err != nil {
		t.Fatal(err)
	}
	reviews.ClaimReviews[0].EvidenceSupport = "supported"
	reviews.ClaimReviews[0].Usefulness = "useful"
	reviews.CaseCriteria = reviews.CaseCriteria[:1]
	reviews.CaseCriteria[0].Verdict = "pass"
	score, err := scoreReviews(report, reviews)
	if err != nil {
		t.Fatalf("score partial review: %v", err)
	}
	if score.Claims.Reviewed != 1 || score.Claims.UsefulCaseCount != 1 ||
		score.CaseCriteria.Reviewed != 1 ||
		score.CaseCriteria.Verdicts["unreviewed"] != caseCount-1 {
		t.Fatalf("unexpected score: %#v", score)
	}
	reviews.ReportDigest = "stale"
	if _, err := scoreReviews(report, reviews); err == nil {
		t.Fatal("stale review was accepted")
	}
}

func fakeGeneration(
	call int,
	request application.SemanticGenerationRequest,
	candidates []domain.ClaimCandidate,
) application.SemanticGenerationResult {
	input, output := 10+call, 2
	total := input + output
	latency := int64(call + 1)
	cost := "0.0001"
	return application.SemanticGenerationResult{
		Candidates: append([]domain.ClaimCandidate{}, candidates...),
		Model: domain.ModelExecutionMetadata{
			RequestedRoute: request.Route, ResolvedProvider: request.Route.Provider,
			ResolvedModel: request.Route.Model, PromptVersion: request.PromptVersion,
			RequestID: "request-" + string(rune('a'+call)), InputTokens: &input,
			OutputTokens: &output, TotalTokens: &total,
			LatencyMilliseconds: &latency, CostUSD: &cost,
		},
	}
}

func testCorpus(t *testing.T) evaluationCorpus {
	t.Helper()
	return testCorpusAtPath(t, testCorpusPath())
}

func testCorpusAtPath(t *testing.T, path string) evaluationCorpus {
	t.Helper()
	corpus, err := loadEvaluationCorpus(path)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	return corpus
}

func testRoute(t *testing.T) aigateway.Route {
	t.Helper()
	route, err := aigateway.LoadRoute(testRoutePath())
	if err != nil {
		t.Fatalf("load route: %v", err)
	}
	return route
}

func testCorpusPath() string {
	return filepath.Join("..", "..", "dev", "evaluations", "semantic-claims", "corpus-v1.json")
}

func testCorpusV2Path() string {
	return filepath.Join("..", "..", "dev", "evaluations", "semantic-claims", "corpus-v2.json")
}

func testRoutePath() string {
	return filepath.Join("..", "..", "config", "semantic-route.example.json")
}

func testClock() func() time.Time {
	current := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		value := current
		current = current.Add(time.Millisecond)
		return value
	}
}

func resultIndex(corpus evaluationCorpus, id string) int {
	for index := range corpus.Cases {
		if corpus.Cases[index].Definition.ID == id {
			return index
		}
	}
	return -1
}

var _ error = categorizedGenerationError("")
