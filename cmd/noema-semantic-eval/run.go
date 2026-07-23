package main

import (
	"context"
	"errors"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

type recordingGenerator struct {
	next      application.SemanticGenerator
	called    bool
	succeeded bool
	request   application.SemanticGenerationRequest
	result    application.SemanticGenerationResult
}

func (generator *recordingGenerator) Generate(
	ctx context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	if generator.called || generator.next == nil {
		return application.SemanticGenerationResult{}, errors.New("evaluation generator call is invalid")
	}
	generator.called = true
	generator.request = request
	result, err := generator.next.Generate(ctx, request)
	if err == nil {
		generator.succeeded = true
		generator.result = result
	}
	return result, err
}

func preflightCorpus(corpus evaluationCorpus, route domain.ValidatedModelRoute) error {
	analyzer := application.SemanticAnalyzer{Privacy: application.PrivacyPolicy{}}
	for _, fixture := range corpus.Cases {
		if err := analyzer.Preflight(application.SemanticAnalysisRequest{
			FactAnalysis: fixture.FactAnalysis, Document: fixture.Document, Route: route,
		}); err != nil {
			return errors.New("evaluation corpus failed production semantic preflight")
		}
	}
	return nil
}

func executeEvaluation(
	ctx context.Context,
	corpus evaluationCorpus,
	route domain.ValidatedModelRoute,
	generator application.SemanticGenerator,
	now func() time.Time,
) evaluationReport {
	if now == nil {
		now = time.Now
	}
	report := newEvaluationReport(corpus, route, now())
	for caseIndex, fixture := range corpus.Cases {
		capture := &recordingGenerator{next: generator}
		analysisID := "evaluation-semantic-" + fixture.Definition.ID
		analyzer := application.SemanticAnalyzer{
			Generator: capture, Privacy: application.PrivacyPolicy{},
			NewID: func() (string, error) { return analysisID, nil },
			Now:   func() time.Time { return now().UTC() },
		}
		result, err := analyzer.Run(ctx, application.SemanticAnalysisRequest{
			FactAnalysis: fixture.FactAnalysis, Document: fixture.Document, Route: route,
		})
		caseResult := evaluationCaseResult{
			ID: fixture.Definition.ID, Intent: fixture.Definition.Intent,
			Status: "completed", Claims: []domain.Claim{}, Evidence: []reviewEvidence{},
		}
		if capture.called && report.Contract.ClaimSchema.Name == "" {
			report.Contract.ClaimSchema = capture.request.Schema.Identity
		}
		if err == nil {
			caseResult.CandidateCount = len(capture.result.Candidates)
			caseResult.Claims = append([]domain.Claim{}, result.Analysis.Claims...)
			caseResult.AdmittedCount = len(caseResult.Claims)
			caseResult.Evidence = citedEvidence(fixture, caseResult.Claims)
			if result.Analysis.Run.Model != nil {
				model := *result.Analysis.Run.Model
				caseResult.Model = &model
			}
			caseResult.MachineExpectations = expectationResults(
				fixture.Definition.MachineExpectations, caseResult.Claims, true,
			)
		} else {
			caseResult.Status = "failed"
			caseResult.MachineExpectations = expectationResults(
				fixture.Definition.MachineExpectations, nil, false,
			)
			category := ""
			switch {
			case capture.called && !capture.succeeded:
				caseResult.FailureStage = "generation"
				category = application.SemanticGenerationFailureCategory(err)
			case capture.succeeded:
				caseResult.FailureStage = "admission"
				category = application.SemanticAdmissionFailureCategory(err)
				caseResult.CandidateCount = len(capture.result.Candidates)
				model := capture.result.Model
				model.RequestedRoute = route.Requested
				model.PromptVersion = application.SemanticPromptVersion
				caseResult.Model = &model
			default:
				caseResult.FailureStage = "local"
				category = "semantic-input-invalid"
			}
			caseResult.FailureCategory = category
			report.Cases = append(report.Cases, caseResult)
			if stopsEvaluation(category) {
				report.StopCategory = category
				for _, remaining := range corpus.Cases[caseIndex+1:] {
					report.Cases = append(report.Cases, unattemptedCaseResult(remaining))
				}
				break
			}
			continue
		}
		report.Cases = append(report.Cases, caseResult)
	}
	finalizeReport(&report, now())
	return report
}

func unattemptedCaseResult(fixture evaluationCase) evaluationCaseResult {
	return evaluationCaseResult{
		ID: fixture.Definition.ID, Intent: fixture.Definition.Intent,
		Status: "not-attempted", Claims: []domain.Claim{}, Evidence: []reviewEvidence{},
		MachineExpectations: expectationResults(
			fixture.Definition.MachineExpectations, nil, false,
		),
	}
}

func stopsEvaluation(category string) bool {
	switch category {
	case application.SemanticGenerationFailureAuthentication,
		application.SemanticGenerationFailurePermission,
		application.SemanticGenerationFailureRateLimited,
		application.SemanticGenerationFailureRequest,
		application.SemanticGenerationFailureSchema,
		application.SemanticGenerationFailureUpstream,
		application.SemanticGenerationFailureTransport,
		application.SemanticGenerationFailureGeneric:
		return true
	default:
		return false
	}
}
