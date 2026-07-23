package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
	noemaevidence "github.com/ferueda/noema/internal/evidence"
	"github.com/ferueda/noema/internal/platform"
)

type recordingSemanticGenerator struct {
	requests []SemanticGenerationRequest
	generate func(SemanticGenerationRequest) (SemanticGenerationResult, error)
}

func (generator *recordingSemanticGenerator) Generate(
	_ context.Context,
	request SemanticGenerationRequest,
) (SemanticGenerationResult, error) {
	generator.requests = append(generator.requests, request)
	if generator.generate == nil {
		return SemanticGenerationResult{}, nil
	}
	return generator.generate(request)
}

func TestSemanticAnalyzerFiltersBoundedInputAndBuildsCompletedAnalysis(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t,
		"Investigate /Users/example/dev/project/main.go",
		"The implementation now has a focused boundary.",
	)
	analysisBefore := semanticTestJSON(t, analysis)
	documentBefore := semanticTestJSON(t, document)
	generator := &recordingSemanticGenerator{
		generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
			return SemanticGenerationResult{
				Candidates: []domain.ClaimCandidate{{
					Type: domain.ClaimTypeLesson, Statement: "A focused boundary keeps evidence handling inspectable.",
					Status: domain.ClaimStatusInferred, Confidence: 0.82,
					SupportingEvidenceIDs: []string{request.Input.Entries[0].Segments[0].EvidenceID},
					SupportingFactIDs:     []string{request.Input.Facts[0].ID},
					Attribution:           domain.ClaimAttributionUnknown,
				}},
				Model: semanticModelMetadata(),
			}, nil
		},
	}

	result, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("run semantic analysis: %v", err)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(generator.requests))
	}
	request := generator.requests[0]
	encodedInput := semanticTestJSON(t, request.Input)
	filteredText := request.Input.Entries[0].Segments[0].Text.Text
	if strings.Contains(encodedInput, "/Users/example") ||
		!strings.Contains(filteredText, privacyLocalPathPlaceholder) {
		t.Fatalf("generator input was not privacy filtered: %s", encodedInput)
	}
	if len([]byte(encodedInput)) > maxSemanticInputBytes || len(request.Input.Entries) != 2 ||
		request.Input.Selection.Coverage != domain.CoverageCompleteRetainedSnapshot {
		t.Fatalf("generator input is not the expected bounded complete selection: %#v", request.Input.Selection)
	}
	if request.PromptVersion != SemanticPromptVersion || request.Schema.Identity.Version != SemanticClaimSchemaVersion ||
		request.Schema.Identity.Name != SemanticClaimSchemaName ||
		request.Schema.Identity.Disposition != domain.StructuredOutputDispositionStrict ||
		len(request.Schema.CanonicalJSON) == 0 ||
		!strings.Contains(request.Instructions, "technical artifact or observed behavior") ||
		!strings.Contains(request.Instructions, "A required check never started") ||
		!strings.Contains(request.Instructions, "Actor and origin must be null") ||
		!strings.Contains(request.Instructions, "Outcome must be failure for a failed-attempt claim") ||
		!strings.Contains(request.Instructions, "otherwise omit that claim entirely") ||
		request.Route != semanticTestRoute().Requested {
		t.Fatalf("generation request metadata = %#v", request)
	}
	if got := semanticTestJSON(t, analysis); got != analysisBefore {
		t.Fatal("fact analysis was mutated while filtering model input")
	}
	if got := semanticTestJSON(t, document); got != documentBefore {
		t.Fatal("source document was mutated while filtering model input")
	}

	run := result.Analysis.Run
	if run.ID != "semantic-analysis" || run.Stage != domain.AnalysisStageClaims ||
		run.Status != domain.AnalysisCompleted || run.ProcessingKey == "" ||
		!reflect.DeepEqual(run.InputFactIDs, []string{"fact-command"}) || len(run.FactIDs) != 0 ||
		len(run.ClaimIDs) != 1 || run.Model == nil {
		t.Fatalf("semantic run = %#v", run)
	}
	if run.Model.RequestedRoute != semanticTestRoute().Requested || run.Model.PromptVersion != SemanticPromptVersion ||
		run.Model.ResolvedProvider != "cerebras" || run.Model.RequestID != "request-1" ||
		run.Model.InputTokens == nil || *run.Model.InputTokens != 17 {
		t.Fatalf("model execution metadata = %#v", run.Model)
	}
	if len(result.Analysis.Claims) != 1 || result.Analysis.Claims[0].ID != run.ClaimIDs[0] ||
		result.Analysis.Claims[0].AnalysisRunID != run.ID ||
		!reflect.DeepEqual(result.Analysis.Claims[0].SupportingFactIDs, run.InputFactIDs) {
		t.Fatalf("claims/run lineage = %#v / %#v", result.Analysis.Claims, run)
	}
	if result.InputDigest == "" || result.Privacy.PolicyVersion != PrivacyPolicyVersion ||
		len(result.Privacy.Redactions) == 0 {
		t.Fatalf("input identity/privacy = %q / %#v", result.InputDigest, result.Privacy)
	}
}

func TestSemanticAnalyzerRejectsInvalidGeneratorMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*domain.ModelExecutionMetadata)
	}{
		{name: "resolved provider", mutate: func(metadata *domain.ModelExecutionMetadata) {
			metadata.ResolvedProvider = "other"
		}},
		{name: "resolved model", mutate: func(metadata *domain.ModelExecutionMetadata) {
			metadata.ResolvedModel = "openai/other"
		}},
		{name: "malformed cost", mutate: func(metadata *domain.ModelExecutionMetadata) {
			value := "1e-4"
			metadata.CostUSD = &value
		}},
		{name: "negative tokens", mutate: func(metadata *domain.ModelExecutionMetadata) {
			value := -1
			metadata.OutputTokens = &value
		}},
		{name: "inconsistent tokens", mutate: func(metadata *domain.ModelExecutionMetadata) {
			value := 23
			metadata.TotalTokens = &value
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticAnalysisFixture(t, "Inspect the behavior.")
			generator := &recordingSemanticGenerator{generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
				metadata := semanticModelMetadata()
				test.mutate(&metadata)
				return SemanticGenerationResult{Candidates: []domain.ClaimCandidate{}, Model: metadata}, nil
			}}
			_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
				FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
			})
			if err == nil || !strings.Contains(err.Error(), "generation metadata") {
				t.Fatalf("error = %v, want generation metadata failure", err)
			}
		})
	}
}

func TestSemanticAnalyzerPreparesCompleteIdentityBeforeGeneration(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t,
		"Investigate /Users/example/dev/project/main.go",
		"Record the bounded result.",
	)
	generator := &recordingSemanticGenerator{}
	analyzer := semanticTestAnalyzer(generator)

	prepared, err := analyzer.prepare(SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("prepare semantic analysis: %v", err)
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls during preparation = %d, want 0", len(generator.requests))
	}
	if prepared.InputFactIDs == nil || !reflect.DeepEqual(*prepared.InputFactIDs, []string{"fact-command"}) ||
		prepared.InputDigest == nil || *prepared.InputDigest == "" || prepared.Selection == nil ||
		prepared.Privacy == nil || prepared.ProcessingKey == nil || *prepared.ProcessingKey == "" {
		t.Fatalf("prepared identity is incomplete: %#v", prepared)
	}
	if prepared.Schema.Name != SemanticClaimSchemaName ||
		prepared.Schema.Version != SemanticClaimSchemaVersion ||
		prepared.Schema.Disposition != domain.StructuredOutputDispositionStrict ||
		prepared.Schema.Digest == "" || prepared.Route.ConfigDigest == "" {
		t.Fatalf("prepared schema/route identity = %#v / %#v", prepared.Schema, prepared.Route)
	}
	if strings.Contains(semanticTestJSON(t, prepared.GenerationRequest.Input), "/Users/example") ||
		prepared.GenerationRequest.Schema.Identity != prepared.Schema ||
		prepared.GenerationRequest.Route != prepared.Route.Requested {
		t.Fatalf("prepared generation request = %#v", prepared.GenerationRequest)
	}

	changedRequest := SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	}
	changedConfig := json.RawMessage(`{"profile":"semantic-v1","revision":2}`)
	changedRequest.Route.SanitizedConfig = changedConfig
	changedRequest.Route.ConfigDigest, err = platform.Fingerprint(changedConfig)
	if err != nil {
		t.Fatalf("fingerprint route configuration: %v", err)
	}
	changed, err := analyzer.prepare(changedRequest)
	if err != nil {
		t.Fatalf("prepare changed route configuration: %v", err)
	}
	if *changed.InputDigest != *prepared.InputDigest || *changed.ProcessingKey == *prepared.ProcessingKey {
		t.Fatalf("configuration changed the wrong identities: %#v / %#v", prepared, changed)
	}
}

func TestSemanticAnalyzerPreparePreservesProgressOnPrivacyFailure(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 24)
	analysis, document := semanticAnalysisFixture(t, "Inspect "+secret)
	generator := &recordingSemanticGenerator{}

	prepared, err := semanticTestAnalyzer(generator).prepare(SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	var violation PrivacyViolation
	if !errors.As(err, &violation) {
		t.Fatalf("error = %v, want PrivacyViolation", err)
	}
	if prepared.Schema.Digest == "" || prepared.Route.ConfigDigest == "" || prepared.InputFactIDs == nil ||
		prepared.Privacy == nil || len(prepared.Privacy.BlockedCategories) == 0 {
		t.Fatal("established preparation fields were lost")
	}
	if prepared.Selection != nil || prepared.InputDigest != nil || prepared.ProcessingKey != nil {
		t.Fatal("unavailable preparation fields were invented")
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls = %d, want 0", len(generator.requests))
	}
}

func TestSemanticAnalyzerPreparePreservesKnownEmptyFactSelection(t *testing.T) {
	document := semanticDocument()
	analysis := semanticAnalysisForDocument(t, document, nil)

	prepared, err := semanticTestAnalyzer(nil).prepare(SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("prepare empty semantic analysis: %v", err)
	}
	if prepared.InputFactIDs == nil || len(*prepared.InputFactIDs) != 0 ||
		prepared.Selection == nil || prepared.InputDigest == nil || prepared.ProcessingKey == nil {
		t.Fatal("known-empty fact selection was not preserved")
	}
}

func TestSemanticAnalyzerRejectsMutatedPreparationBeforeGeneration(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	generator := &recordingSemanticGenerator{}
	analyzer := semanticTestAnalyzer(generator)
	prepared, err := analyzer.prepare(SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("prepare semantic analysis: %v", err)
	}
	prepared.GenerationRequest.Input.Entries[0].Kind = "mutated"

	_, err = analyzer.generatePrepared(context.Background(), prepared)
	if !errors.Is(err, ErrSemanticInputInvalid) {
		t.Fatalf("error = %v, want ErrSemanticInputInvalid", err)
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls = %d, want 0", len(generator.requests))
	}
}

func TestSemanticAnalyzerRejectsPreparationMutatedDuringGeneration(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	generator := &recordingSemanticGenerator{generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
		support := request.Input.Entries[0].Segments[0].EvidenceID
		request.Input.Entries[0].Kind = "mutated-by-generator"
		return SemanticGenerationResult{
			Candidates: []domain.ClaimCandidate{{
				Type: domain.ClaimTypeLesson, Statement: "A second validation boundary detects aliased mutations.",
				Status: domain.ClaimStatusInferred, Confidence: 0.8,
				SupportingEvidenceIDs: []string{support},
			}},
			Model: semanticModelMetadata(),
		}, nil
	}}
	analyzer := semanticTestAnalyzer(generator)
	prepared, err := analyzer.prepare(SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("prepare semantic analysis: %v", err)
	}
	generation, err := analyzer.generatePrepared(context.Background(), prepared)
	if err != nil {
		t.Fatalf("generate semantic analysis: %v", err)
	}
	if len(generation.Candidates) != 1 || len(generator.requests) != 1 {
		t.Fatalf("generation result/calls = %#v / %d, want one candidate from one call", generation, len(generator.requests))
	}

	result, err := analyzer.admitPrepared(prepared, generation, "semantic-mutated", analyzer.now())
	if !errors.Is(err, ErrSemanticInputInvalid) {
		t.Fatalf("error = %v, want ErrSemanticInputInvalid", err)
	}
	if result.Analysis.Run.ID != "" || len(result.Analysis.Claims) != 0 {
		t.Fatalf("mutated preparation produced admitted claims: %#v", result)
	}
}

func TestSemanticAnalyzerPreflightBlockerPreventsGeneration(t *testing.T) {
	secret := "ghp_" + strings.Repeat("a", 24)
	analysis, document := semanticAnalysisFixture(t, "token="+secret, "safe follow-up")
	generator := &recordingSemanticGenerator{}

	_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	var violation PrivacyViolation
	if !errors.As(err, &violation) {
		t.Fatalf("error = %v, want PrivacyViolation", err)
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls = %d, want 0", len(generator.requests))
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("privacy error exposed blocked value: %v", err)
	}
}

func TestSemanticAnalyzerPreflightCoversOpenEntryMetadata(t *testing.T) {
	secret := "ghp_" + strings.Repeat("m", 24)
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
	document.Entries[0].Kind = secret
	analysis.Facts[0].Evidence[0].EntryKind = secret
	generator := &recordingSemanticGenerator{}

	_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	var violation PrivacyViolation
	if !errors.As(err, &violation) || !reflect.DeepEqual(violation.Categories, []string{privacyProviderToken}) {
		t.Fatalf("error = %v, want provider-token PrivacyViolation", err)
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls = %d, want 0", len(generator.requests))
	}
}

func TestSemanticAnalyzerRejectsCompleteInputExpandedPastTextLimitByPrivacy(t *testing.T) {
	text := strings.Repeat("/tmp/x ", 1000)
	analysis, document := semanticAnalysisFixture(t, text)
	generator := &recordingSemanticGenerator{}

	_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if !errors.Is(err, ErrSemanticInputTooLarge) {
		t.Fatalf("error = %v, want ErrSemanticInputTooLarge", err)
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls = %d, want 0", len(generator.requests))
	}
}

func TestSemanticAnalyzerBoundsExplicitInputAfterPrivacyExpansion(t *testing.T) {
	text := strings.Repeat("/tmp/x ", 1000)
	analysis, document := semanticAnalysisFixture(t, text)
	generator := &recordingSemanticGenerator{
		generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
			return SemanticGenerationResult{Candidates: []domain.ClaimCandidate{}, Model: semanticModelMetadata()}, nil
		},
	}
	first, last := 0, 0

	_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document,
		Bounds: EntryBounds{First: &first, Last: &last}, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("run semantic analysis: %v", err)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(generator.requests))
	}
	input := generator.requests[0].Input
	if input.Selection.Coverage != semanticCoveragePartial || input.Selection.TruncatedTextSegments != 1 ||
		input.Selection.TruncatedFactTexts != 1 || len([]byte(input.Entries[0].Segments[0].Text.Text)) > maxSemanticTextValueBytes ||
		len([]byte(input.Facts[0].Value.Command.Text)) > maxSemanticTextValueBytes {
		t.Fatalf("bounded input selection = %#v", input.Selection)
	}
}

func TestSemanticAnalyzerRejectsProtectedCandidateFieldsAsOneBatch(t *testing.T) {
	for _, field := range []string{"statement", "subject", "scope", "actor", "origin"} {
		t.Run(field, func(t *testing.T) {
			analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "Record the result.")
			generator := &recordingSemanticGenerator{
				generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
					support := request.Input.Entries[0].Segments[0].EvidenceID
					valid := domain.ClaimCandidate{
						Type: domain.ClaimTypeLesson, Statement: "The first candidate is safe.",
						Status: domain.ClaimStatusInferred, Confidence: 0.7,
						SupportingEvidenceIDs: []string{support},
					}
					protected := domain.ClaimCandidate{
						Type: domain.ClaimTypeLesson, Statement: "The second candidate is unsafe.",
						Status: domain.ClaimStatusInferred, Confidence: 0.7,
						SupportingEvidenceIDs: []string{support},
					}
					switch field {
					case "statement":
						protected.Statement = "Read /Users/example/private/notes.txt"
					case "subject":
						protected.Subject = "/Users/example/private/subject"
					case "scope":
						protected.Scope = "/Users/example/private/scope"
					case "actor":
						protected.Actor = "/Users/example/private/actor"
					case "origin":
						protected.Origin = "/Users/example/private/origin"
					}
					return SemanticGenerationResult{
						Candidates: []domain.ClaimCandidate{valid, protected}, Model: semanticModelMetadata(),
					}, nil
				},
			}

			result, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
				FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
			})
			var violation PrivacyViolation
			if !errors.As(err, &violation) {
				t.Fatalf("error = %v, want PrivacyViolation", err)
			}
			if result.Analysis.Run.ID != "" || len(result.Analysis.Claims) != 0 || len(generator.requests) != 1 {
				t.Fatalf("partial semantic result/calls = %#v / %d", result, len(generator.requests))
			}
		})
	}
}

func TestSemanticAnalyzerCompletesWithNoCandidates(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "No reusable lesson found.")
	generator := &recordingSemanticGenerator{
		generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
			return SemanticGenerationResult{Candidates: []domain.ClaimCandidate{}, Model: semanticModelMetadata()}, nil
		},
	}

	result, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err != nil {
		t.Fatalf("run semantic analysis: %v", err)
	}
	if result.Analysis.Run.Status != domain.AnalysisCompleted || result.Analysis.Run.Model == nil ||
		len(result.Analysis.Run.ClaimIDs) != 0 || len(result.Analysis.Claims) != 0 {
		t.Fatalf("empty semantic analysis = %#v", result.Analysis)
	}
}

func TestSemanticAnalyzerRejectsWholeInvalidCandidateBatch(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "Record the result.")
	generator := &recordingSemanticGenerator{
		generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
			support := request.Input.Entries[0].Segments[0].EvidenceID
			return SemanticGenerationResult{
				Candidates: []domain.ClaimCandidate{
					{Type: domain.ClaimTypeLesson, Statement: "A supported claim.", Status: domain.ClaimStatusInferred,
						Confidence: 0.8, SupportingEvidenceIDs: []string{support}},
					{Type: domain.ClaimType("unsupported"), Statement: "An invalid claim.", Status: domain.ClaimStatusInferred,
						Confidence: 0.8, SupportingEvidenceIDs: []string{support}},
				},
				Model: semanticModelMetadata(),
			}, nil
		},
	}

	result, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if !errors.Is(err, ErrClaimCandidateInvalid) {
		t.Fatalf("error = %v, want ErrClaimCandidateInvalid", err)
	}
	if result.Analysis.Run.ID != "" || len(result.Analysis.Claims) != 0 || len(generator.requests) != 1 {
		t.Fatalf("partial semantic result/calls = %#v / %d", result, len(generator.requests))
	}
}

func TestSemanticAnalyzerSanitizesGeneratorFailure(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "Record the result.")
	protected := "upstream failed with token " + "ghp_" + strings.Repeat("b", 24)
	generator := &recordingSemanticGenerator{
		generate: func(SemanticGenerationRequest) (SemanticGenerationResult, error) {
			return SemanticGenerationResult{}, errors.New(protected)
		},
	}

	_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: semanticTestRoute(),
	})
	if err == nil || err.Error() != "semantic generation failed" || strings.Contains(err.Error(), protected) {
		t.Fatalf("error = %v, want bounded sanitized failure", err)
	}
	if len(generator.requests) != 1 {
		t.Fatalf("generation calls = %d, want 1", len(generator.requests))
	}
}

func TestSemanticAnalyzerRejectsRoutePrivacyMismatchBeforeGeneration(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "Record the result.")
	generator := &recordingSemanticGenerator{}
	route := semanticTestRoute()
	route.Requested.PrivacyPolicyVersion = "different-policy"

	_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
		FactAnalysis: analysis, Document: document, Route: route,
	})
	if !errors.Is(err, ErrSemanticInputInvalid) {
		t.Fatalf("error = %v, want ErrSemanticInputInvalid", err)
	}
	if len(generator.requests) != 0 {
		t.Fatalf("generation calls = %d, want 0", len(generator.requests))
	}
}

func TestSemanticAnalyzerRejectsProtectedAndInvalidRoutesBeforeGeneration(t *testing.T) {
	for _, test := range []struct {
		name       string
		change     func(*domain.ValidatedModelRoute)
		privacyErr bool
	}{
		{name: "protected model", change: func(route *domain.ValidatedModelRoute) {
			route.Requested.Model = "openai/ghp_" + strings.Repeat("r", 24)
		}, privacyErr: true},
		{name: "oversized provider", change: func(route *domain.ValidatedModelRoute) {
			route.Requested.Provider = strings.Repeat("p", 65)
		}},
		{name: "unknown alias", change: func(route *domain.ValidatedModelRoute) { route.Requested.Alias = "other" }},
		{name: "unknown gateway", change: func(route *domain.ValidatedModelRoute) { route.Requested.Gateway = "other" }},
		{name: "unknown route version", change: func(route *domain.ValidatedModelRoute) { route.Requested.RouteVersion = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
			generator := &recordingSemanticGenerator{}
			route := semanticTestRoute()
			test.change(&route)

			_, err := semanticTestAnalyzer(generator).Run(context.Background(), SemanticAnalysisRequest{
				FactAnalysis: analysis, Document: document, Route: route,
			})
			if test.privacyErr {
				var violation PrivacyViolation
				if !errors.As(err, &violation) {
					t.Fatalf("error = %v, want PrivacyViolation", err)
				}
			} else if !errors.Is(err, ErrSemanticInputInvalid) {
				t.Fatalf("error = %v, want ErrSemanticInputInvalid", err)
			}
			if len(generator.requests) != 0 {
				t.Fatalf("generation calls = %d, want 0", len(generator.requests))
			}
		})
	}
}

func TestSemanticAnalyzerRejectsInvalidRouteConfigurationIdentityBeforeGeneration(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*domain.ValidatedModelRoute)
	}{
		{name: "missing configuration", change: func(route *domain.ValidatedModelRoute) {
			route.SanitizedConfig = nil
		}},
		{name: "non-object configuration", change: func(route *domain.ValidatedModelRoute) {
			route.SanitizedConfig = json.RawMessage(`[]`)
			route.ConfigDigest, _ = platform.Fingerprint(route.SanitizedConfig)
		}},
		{name: "digest mismatch", change: func(route *domain.ValidatedModelRoute) {
			route.ConfigDigest = strings.Repeat("f", 64)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.")
			generator := &recordingSemanticGenerator{}
			route := semanticTestRoute()
			test.change(&route)

			prepared, err := semanticTestAnalyzer(generator).prepare(SemanticAnalysisRequest{
				FactAnalysis: analysis, Document: document, Route: route,
			})
			if !errors.Is(err, ErrSemanticInputInvalid) {
				t.Fatalf("error = %v, want ErrSemanticInputInvalid", err)
			}
			if prepared.Schema.Digest == "" || prepared.InputFactIDs != nil || len(generator.requests) != 0 {
				t.Fatal("route failure did not preserve the expected preparation boundary")
			}
		})
	}
}

func TestValidateSemanticGenerationRequestSizeCapsCompleteEnvelope(t *testing.T) {
	request := SemanticGenerationRequest{
		Instructions: semanticInstructions, PromptVersion: SemanticPromptVersion,
		Schema: mustSemanticClaimOutputSchema(t), Route: semanticTestRoute().Requested,
		Input: SemanticModelInput{SchemaVersion: SemanticInputSchemaVersion},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal generation request: %v", err)
	}
	if err := validateSemanticGenerationRequestSize(request, len(encoded)); err != nil {
		t.Fatalf("validate exact request budget: %v", err)
	}
	if err := validateSemanticGenerationRequestSize(request, len(encoded)-1); !errors.Is(err, ErrSemanticInputTooLarge) {
		t.Fatalf("error = %v, want ErrSemanticInputTooLarge", err)
	}
}

func TestSemanticAnalyzerIdentitiesTrackInputAndRoute(t *testing.T) {
	analysis, document := semanticAnalysisFixture(t, "Inspect the current behavior.", "Record the result.")
	generator := &recordingSemanticGenerator{
		generate: func(request SemanticGenerationRequest) (SemanticGenerationResult, error) {
			metadata := semanticModelMetadata()
			metadata.ResolvedProvider = request.Route.Provider
			metadata.ResolvedModel = request.Route.Model
			return SemanticGenerationResult{Candidates: []domain.ClaimCandidate{}, Model: metadata}, nil
		},
	}
	analyzer := semanticTestAnalyzer(generator)
	base := SemanticAnalysisRequest{FactAnalysis: analysis, Document: document, Route: semanticTestRoute()}

	first, err := analyzer.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := analyzer.Run(context.Background(), base)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first.InputDigest != second.InputDigest ||
		first.Analysis.Run.ProcessingKey != second.Analysis.Run.ProcessingKey {
		t.Fatalf("same input identities changed: %#v / %#v", first, second)
	}

	firstOrdinal, lastOrdinal := 0, 0
	ranged := base
	ranged.Bounds = EntryBounds{First: &firstOrdinal, Last: &lastOrdinal}
	rangeResult, err := analyzer.Run(context.Background(), ranged)
	if err != nil {
		t.Fatalf("range run: %v", err)
	}
	if rangeResult.InputDigest == first.InputDigest ||
		rangeResult.Analysis.Run.ProcessingKey == first.Analysis.Run.ProcessingKey {
		t.Fatalf("range did not change input/processing identities: %#v / %#v", first, rangeResult)
	}

	routed := base
	routed.Route = semanticTestRoute()
	routed.Route.Requested.Model = "openai/comparison-model"
	routeResult, err := analyzer.Run(context.Background(), routed)
	if err != nil {
		t.Fatalf("route run: %v", err)
	}
	if routeResult.InputDigest != first.InputDigest ||
		routeResult.Analysis.Run.ProcessingKey == first.Analysis.Run.ProcessingKey {
		t.Fatalf("route changed the wrong identities: %#v / %#v", first, routeResult)
	}
	if len(generator.requests) != 4 {
		t.Fatalf("generation calls = %d, want 4", len(generator.requests))
	}
	for _, request := range generator.requests {
		if request.PromptVersion != SemanticPromptVersion {
			t.Fatalf("prompt version = %q, want %q", request.PromptVersion, SemanticPromptVersion)
		}
	}
}

func semanticTestAnalyzer(generator SemanticGenerator) SemanticAnalyzer {
	return SemanticAnalyzer{
		Generator: generator,
		Privacy:   PrivacyPolicy{},
		NewID:     func() (string, error) { return "semantic-analysis", nil },
		Now:       func() time.Time { return time.Date(2026, 7, 21, 16, 0, 0, 0, time.UTC) },
	}
}

func semanticTestRoute() domain.ValidatedModelRoute {
	configuration := json.RawMessage(`{"profile":"semantic-v1"}`)
	digest, err := platform.Fingerprint(configuration)
	if err != nil {
		panic(err)
	}
	return domain.ValidatedModelRoute{
		Requested: domain.RequestedModelRoute{
			Alias: semanticRouteAlias, Gateway: semanticRouteGateway, Model: "openai/gpt-oss-120b",
			Provider: "cerebras", RouteVersion: semanticRouteVersion, PrivacyPolicyVersion: PrivacyPolicyVersion,
		},
		SanitizedConfig: configuration,
		ConfigDigest:    digest,
	}
}

func mustSemanticClaimOutputSchema(t *testing.T) domain.StructuredOutputSchema {
	t.Helper()
	schema, err := semanticClaimOutputSchema()
	if err != nil {
		t.Fatalf("build semantic output schema: %v", err)
	}
	return schema
}

func semanticModelMetadata() domain.ModelExecutionMetadata {
	inputTokens, outputTokens, totalTokens := 17, 5, 22
	latency := int64(31)
	return domain.ModelExecutionMetadata{
		ResolvedProvider: "cerebras", ResolvedModel: "openai/gpt-oss-120b", RequestID: "request-1",
		InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens,
		LatencyMilliseconds: &latency,
	}
}

func semanticAnalysisFixture(t *testing.T, texts ...string) (domain.FactAnalysis, domain.EvidenceDocument) {
	t.Helper()
	entries := make([]domain.EvidenceEntry, len(texts))
	totalTextBytes := 0
	for index, text := range texts {
		hash := sha256.Sum256([]byte(text))
		actor := "model"
		if index == 0 {
			actor = "human"
		}
		entries[index] = domain.EvidenceEntry{
			Ordinal: index, Kind: "message", Actor: actor,
			Content: []domain.EvidenceSegment{{
				Ordinal: 0, Kind: "text", Origin: actor, OriginConfidence: "high",
				Text: &domain.SelectedText{
					Text: text, OriginalUTF8Bytes: len([]byte(text)), EmittedUTF8Bytes: len([]byte(text)),
					ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
				},
			}},
		}
		totalTextBytes += len([]byte(text))
	}
	firstOrdinal, lastOrdinal := 0, len(entries)-1
	selection := domain.EvidenceSelection{
		Mode: "full",
		Entries: domain.EntrySelection{
			Selected: len(entries), Total: len(entries), FirstOrdinal: &firstOrdinal, LastOrdinal: &lastOrdinal,
		},
		Segments: domain.CountSelection{Selected: len(entries), Total: len(entries)},
		SegmentText: domain.ByteSelection{
			EmittedUTF8Bytes: totalTextBytes, OriginalUTF8Bytes: totalTextBytes,
		},
		Coverage: domain.CoverageCompleteRetainedSnapshot,
	}
	document := domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind: domain.EvidenceSourceSessions, CanonicalID: "synthetic@local:semantic",
			NativeSourceKind: "synthetic", SourceInstanceID: "local", NativeID: "semantic",
			DocumentDigest: domain.Digest{
				Scheme: "sha256-sessions-document-jcs-v1", Digest: strings.Repeat("d", 64),
			},
		},
		Selection: selection,
		Entries:   entries,
	}

	segmentOrdinal := 0
	ref, err := noemaevidence.SessionsReference(document, 0, &segmentOrdinal)
	if err != nil {
		t.Fatalf("build fact evidence: %v", err)
	}
	fact := domain.Fact{
		ID: "fact-command", AnalysisRunID: "fact-analysis", Kind: "command",
		SchemaVersion: 1, Outcome: domain.FactOutcomeNotApplicable,
		ExtractorName: "fixture", ExtractorVersion: "1", ParseRule: "fixture-v1",
		Value: domain.FactValue{Command: &domain.SelectedText{
			Text: texts[0], OriginalUTF8Bytes: len([]byte(texts[0])), EmittedUTF8Bytes: len([]byte(texts[0])),
			ContentHash: entries[0].Content[0].Text.ContentHash,
		}},
		Evidence: []domain.EvidenceRef{ref}, CreatedAt: time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC),
	}
	revision := document.Revision
	runSelection := selection
	run := domain.AnalysisRun{
		ID: "fact-analysis", Stage: domain.AnalysisStageFacts, RequestedSourceIdentity: revision.CanonicalID,
		Revision: &revision, Selection: &runSelection, ExtractorName: "fixture", ExtractorVersion: "1",
		SchemaVersion: 1, FactIDs: []string{fact.ID}, Status: domain.AnalysisCompleted,
	}
	return domain.FactAnalysis{Run: run, Facts: []domain.Fact{fact}}, document
}

func semanticTestJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test value: %v", err)
	}
	return string(encoded)
}
