package application

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ferueda/noema/internal/domain"
)

func TestSemanticConformanceUsesProductionContractAndFixedPublicInput(t *testing.T) {
	route := semanticTestRoute()
	wantSchema := mustSemanticClaimOutputSchema(t)
	generator := &recordingSemanticGenerator{
		generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
			if request.Instructions != semanticInstructions ||
				request.PromptVersion != SemanticPromptVersion ||
				request.Route != route.Requested ||
				!reflect.DeepEqual(request.Schema, wantSchema) {
				t.Fatalf("generation request contract = %#v", request)
			}
			if request.Input.SchemaVersion != SemanticInputSchemaVersion ||
				request.Input.Disposition != semanticInputDisposition ||
				request.Input.Selection.Mode != "complete" ||
				request.Input.Selection.Coverage != domain.CoverageCompleteRetainedSnapshot ||
				len(request.Input.Entries) != 0 || len(request.Input.Facts) != 0 {
				t.Fatalf("conformance input = %#v", request.Input)
			}
			encoded, err := json.Marshal(request.Input)
			if err != nil {
				t.Fatalf("encode conformance input: %v", err)
			}
			if strings.Contains(string(encoded), "Users/") ||
				strings.Contains(string(encoded), "canonicalId") ||
				strings.Contains(string(encoded), "evidenceId") {
				t.Fatalf("conformance input contains source data: %s", encoded)
			}
			return SemanticGenerationResult{
				Candidates: []domain.ClaimCandidate{},
				Model:      semanticModelMetadata(),
			}, nil
		},
	}

	result, err := (SemanticConformance{Generator: generator}).Run(context.Background(), route)
	if err != nil {
		t.Fatalf("run conformance: %v", err)
	}
	if len(generator.requests) != 1 || result.CandidateCount != 0 ||
		!reflect.DeepEqual(result.Schema, wantSchema.Identity) ||
		result.Model.RequestedRoute != route.Requested ||
		result.Model.PromptVersion != SemanticPromptVersion {
		t.Fatalf("conformance result = %#v; calls = %d", result, len(generator.requests))
	}
}

func TestSemanticConformanceRejectsCandidatesAndMismatchedMetadataSafely(t *testing.T) {
	protected := "private candidate " + "ghp_" + strings.Repeat("x", 24)
	for _, test := range []struct {
		name       string
		generation SemanticGenerationResult
	}{
		{
			name: "nonempty candidate",
			generation: SemanticGenerationResult{
				Candidates: []domain.ClaimCandidate{{Statement: protected}},
				Model:      semanticModelMetadata(),
			},
		},
		{
			name: "provider mismatch",
			generation: SemanticGenerationResult{
				Candidates: []domain.ClaimCandidate{},
				Model: func() domain.ModelExecutionMetadata {
					metadata := semanticModelMetadata()
					metadata.ResolvedProvider = "other"
					return metadata
				}(),
			},
		},
		{
			name: "model mismatch",
			generation: SemanticGenerationResult{
				Candidates: []domain.ClaimCandidate{},
				Model: func() domain.ModelExecutionMetadata {
					metadata := semanticModelMetadata()
					metadata.ResolvedModel = "openai/other"
					return metadata
				}(),
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			generator := &recordingSemanticGenerator{
				generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
					return test.generation, nil
				},
			}
			_, err := (SemanticConformance{Generator: generator}).Run(context.Background(), semanticTestRoute())
			var failure SemanticConformanceError
			if !errors.As(err, &failure) ||
				failure.Category != SemanticGenerationFailureResponse ||
				strings.Contains(err.Error(), protected) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSemanticConformanceSanitizesGeneratorFailure(t *testing.T) {
	protected := "provider detail " + "ghp_" + strings.Repeat("y", 24)
	generator := &recordingSemanticGenerator{
		generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
			return SemanticGenerationResult{}, categorizedConformanceTestError{
				category: SemanticGenerationFailurePermission,
				detail:   protected,
			}
		},
	}

	_, err := (SemanticConformance{Generator: generator}).Run(context.Background(), semanticTestRoute())
	var failure SemanticConformanceError
	if !errors.As(err, &failure) ||
		failure.Category != SemanticGenerationFailurePermission ||
		strings.Contains(err.Error(), protected) {
		t.Fatalf("error = %v", err)
	}
}

type categorizedConformanceTestError struct {
	category string
	detail   string
}

func (failure categorizedConformanceTestError) Error() string {
	return failure.detail
}

func (failure categorizedConformanceTestError) SemanticGenerationFailureCategory() string {
	return failure.category
}
