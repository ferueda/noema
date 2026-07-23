package application

import (
	"context"

	"github.com/ferueda/noema/internal/domain"
)

// SemanticConformanceResult reports only bounded protocol metadata. The fixed
// input contains no evidence, so a successful check always has zero candidates.
type SemanticConformanceResult struct {
	Schema         domain.StructuredOutputSchemaIdentity
	Model          domain.ModelExecutionMetadata
	CandidateCount int
}

// SemanticConformanceError exposes one allowlisted operational category and
// never retains a provider message or candidate text.
type SemanticConformanceError struct {
	Category string
}

func (failure SemanticConformanceError) Error() string {
	return failure.Category
}

// SemanticConformance checks the production semantic contract using fixed
// public input. It does not admit claims or persist analysis state.
type SemanticConformance struct {
	Generator SemanticGenerator
}

func (check SemanticConformance) Run(
	ctx context.Context,
	route domain.ValidatedModelRoute,
) (SemanticConformanceResult, error) {
	if check.Generator == nil {
		return SemanticConformanceResult{}, semanticConformanceError(SemanticGenerationFailureRequest)
	}
	if err := validateValidatedSemanticRoute(route, PrivacyPolicy{}); err != nil {
		return SemanticConformanceResult{}, semanticConformanceError(SemanticGenerationFailureRequest)
	}
	schema, err := semanticClaimOutputSchema()
	if err != nil {
		return SemanticConformanceResult{}, semanticConformanceError(SemanticGenerationFailureRequest)
	}
	request := SemanticGenerationRequest{
		Instructions:  semanticInstructions,
		PromptVersion: SemanticPromptVersion,
		Schema:        schema,
		Route:         route.Requested,
		Input:         semanticConformanceInput(),
	}
	if err := validateSemanticEncodedSize(request.Input, defaultSemanticInputLimits); err != nil {
		return SemanticConformanceResult{}, semanticConformanceError(SemanticGenerationFailureRequest)
	}
	if err := validateSemanticGenerationRequestSize(request, maxSemanticGenerationRequestBytes); err != nil {
		return SemanticConformanceResult{}, semanticConformanceError(SemanticGenerationFailureRequest)
	}

	generation, err := check.Generator.Generate(ctx, request)
	if err != nil {
		return SemanticConformanceResult{}, semanticConformanceError(semanticGenerationFailureCategory(err))
	}
	generation.Model.RequestedRoute = route.Requested
	generation.Model.PromptVersion = SemanticPromptVersion
	if err := validateSemanticModelExecution(generation.Model, route.Requested); err != nil ||
		len(generation.Candidates) != 0 {
		return SemanticConformanceResult{}, semanticConformanceError(SemanticGenerationFailureResponse)
	}
	return SemanticConformanceResult{
		Schema: schema.Identity, Model: generation.Model, CandidateCount: 0,
	}, nil
}

func semanticConformanceInput() SemanticModelInput {
	return SemanticModelInput{
		SchemaVersion: SemanticInputSchemaVersion,
		Disposition:   semanticInputDisposition,
		Selection: SemanticSelection{
			Mode:     "complete",
			Coverage: domain.CoverageCompleteRetainedSnapshot,
		},
		Entries:   []SemanticEntryInput{},
		Facts:     []SemanticFactInput{},
		Omissions: SemanticInputOmissions{FactAnalysis: domain.AnalysisOmissions{}},
	}
}

func semanticConformanceError(category string) error {
	switch category {
	case SemanticGenerationFailureAuthentication,
		SemanticGenerationFailurePermission,
		SemanticGenerationFailureRateLimited,
		SemanticGenerationFailureRequest,
		SemanticGenerationFailureSchema,
		SemanticGenerationFailureContext,
		SemanticGenerationFailureContent,
		SemanticGenerationFailureUpstream,
		SemanticGenerationFailureTimeout,
		SemanticGenerationFailureTransport,
		SemanticGenerationFailureResponse,
		SemanticGenerationFailureGeneric:
		return SemanticConformanceError{Category: category}
	default:
		return SemanticConformanceError{Category: SemanticGenerationFailureGeneric}
	}
}
