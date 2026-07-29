package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestContentScoutDispatchesRetainedAnalysisAcrossSQLiteConnections(
	t *testing.T,
) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "noema.db")
	now := time.Date(2026, time.July, 28, 20, 0, 0, 0, time.UTC)
	document, reference := semanticConcurrencyEvidence(t)

	producerDatabase, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open producer database: %v", err)
	}
	producerStore := sqlitestore.NewStore(producerDatabase)
	factResult, err := (application.FactAnalyzer{
		Source: semanticConcurrencyStaticSource{document: document},
		Extractor: semanticConcurrencyFactExtractor{draft: domain.FactDraft{
			Kind: "command",
			Value: domain.FactValue{Command: &domain.SelectedText{
				Text: "go test ./...", OriginalUTF8Bytes: len("go test ./..."),
				EmittedUTF8Bytes: len("go test ./..."),
				ContentHash:      document.Entries[0].Content[0].Text.ContentHash,
			}},
			Outcome:   domain.FactOutcomeSuccess,
			ParseRule: "content-scout-integration-v1",
			Evidence:  []domain.EvidenceRef{reference},
		}},
		Store: producerStore,
		NewID: func() (string, error) { return "fact-analysis-content-scout", nil },
		Now:   func() time.Time { return now },
	}).Run(ctx, document.Revision.CanonicalID)
	if err != nil {
		t.Fatalf("seed fact analysis: %v", err)
	}
	semantic, err := semanticConcurrencyWorkflow(
		semanticConcurrencyStaticSource{document: document},
		producerStore,
		contentScoutIntegrationSemanticGenerator{},
		"semantic-analysis-content-scout",
		now.Add(time.Minute),
	).Run(ctx, application.SemanticWorkflowRequest{
		FactAnalysisID: factResult.Analysis.Run.ID,
		Route:          semanticConcurrencyRoute(t),
	})
	if err != nil {
		t.Fatalf("seed semantic analysis: %v", err)
	}
	configuration := loadContentScoutIntegrationConfiguration(t)
	match, err := (application.SubscriptionMatcher{
		Store: producerStore,
		Now:   func() time.Time { return now.Add(2 * time.Minute) },
	}).MatchContentScout(
		ctx, semantic.Record.Analysis.Run.ID, configuration,
	)
	if err != nil || len(match.Jobs) != 1 || !match.Jobs[0].Created {
		t.Fatalf("match Content Scout job = %#v, %v", match, err)
	}
	jobID := match.Jobs[0].ID
	if err := producerDatabase.Close(); err != nil {
		t.Fatalf("close producer database: %v", err)
	}

	workerDatabase, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open independent worker database: %v", err)
	}
	defer workerDatabase.Close()
	workerStore := sqlitestore.NewStore(workerDatabase)
	executor := &contentScoutIntegrationExecutor{}
	tick := 2
	result, err := (application.ContentScoutDispatcherV1{
		Store: workerStore, Executor: executor, AllowRemote: true,
		Preflight: func(
			_ context.Context,
			job application.AgentJobRecordV1,
			execution domain.AgentExecutionIdentity,
		) error {
			executor.preflightCalls++
			if job.ID != jobID || execution.Validate() != nil {
				return application.ErrContentScoutExecutorUnavailable
			}
			return nil
		},
		Now: func() time.Time {
			tick++
			return now.Add(time.Duration(tick) * time.Minute)
		},
	}).RunOnce(ctx)
	if err != nil {
		t.Fatalf("dispatch Content Scout: %v", err)
	}
	if !result.Claimed || result.JobID != jobID ||
		result.Outcome != domain.AgentRunOutcomeSucceeded ||
		result.Disposition != domain.AgentExecutionDispositionInvoked ||
		len(result.ArtifactIDs) != 1 ||
		executor.preflightCalls != 1 || executor.executeCalls != 1 {
		t.Fatalf("dispatch result = %#v, executor = %#v", result, executor)
	}

	inspection, found, err := workerStore.InspectV1Job(ctx, jobID)
	if err != nil || !found || inspection.Run == nil ||
		len(inspection.Artifacts) != 1 {
		t.Fatalf("inspect dispatched job = %#v, %v, %v", inspection, found, err)
	}
	artifact := inspection.Artifacts[0]
	if artifact.ID != result.ArtifactIDs[0] ||
		artifact.Kind != domain.ArtifactKindContentIdea ||
		len(artifact.ClaimIDs) != 1 ||
		len(artifact.FactIDs) != 1 ||
		len(artifact.SupportingEvidence) != 1 ||
		artifact.ProposalStatus != domain.ArtifactProposalReviewRequired ||
		artifact.SafetyStatus != domain.ArtifactSafetyReviewRequired {
		t.Fatalf("dispatched artifact = %#v", artifact)
	}
	if strings.Contains(
		string(executor.request.Input.CanonicalJSON),
		document.Revision.CanonicalID,
	) || strings.Contains(
		string(executor.request.Input.CanonicalJSON),
		document.Revision.DocumentDigest.Digest,
	) {
		t.Fatal("executor input contains source identity")
	}
	for table, want := range map[string]int{
		"jobs": 1, "agent_runs": 1, "artifacts": 1,
	} {
		assertSemanticConcurrencyCount(t, ctx, workerDatabase, table, want)
	}
}

type contentScoutIntegrationSemanticGenerator struct{}

func (contentScoutIntegrationSemanticGenerator) Generate(
	_ context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	return application.SemanticGenerationResult{
		Candidates: []domain.ClaimCandidate{{
			Type:                  domain.ClaimTypeLesson,
			Statement:             "Testing one command before a change reduces uncertainty.",
			Status:                domain.ClaimStatusInferred,
			Confidence:            0.9,
			SupportingEvidenceIDs: []string{request.Input.Facts[0].EvidenceIDs[0]},
			SupportingFactIDs:     []string{request.Input.Facts[0].ID},
			Attribution:           domain.ClaimAttributionUnknown,
		}},
		Model: domain.ModelExecutionMetadata{
			ResolvedProvider: "cerebras",
			ResolvedModel:    "openai/gpt-oss-120b",
			RequestID:        "content-scout-semantic-request",
		},
	}, nil
}

type contentScoutIntegrationExecutor struct {
	preflightCalls int
	executeCalls   int
	request        domain.AgentExecutionRequestV1
}

func (executor *contentScoutIntegrationExecutor) Execute(
	_ context.Context,
	request domain.AgentExecutionRequestV1,
	schema domain.StructuredOutputSchema,
) (domain.AgentExecutionResponseV1, error) {
	executor.executeCalls++
	executor.request = request
	if request.Validate() != nil ||
		schema.Identity != request.RequiredOutputSchema {
		return domain.AgentExecutionResponseV1{}, application.ErrContentScoutExecutorUnavailable
	}
	var input domain.ContentScoutInputV1
	if err := json.Unmarshal(request.Input.CanonicalJSON, &input); err != nil ||
		input.Validate() != nil || len(input.Claims) != 1 {
		return domain.AgentExecutionResponseV1{}, application.ErrContentScoutExecutorUnavailable
	}
	candidates, err := json.Marshal(domain.ContentScoutCandidatesV1{
		Ideas: []domain.ContentIdeaCandidateV1{{
			Concept:         "Build small agent jobs.",
			CoreLesson:      "A focused agent can turn a useful lesson into content.",
			AudienceBenefit: "Developers get a clear example they can use.",
			Hook:            "Useful lessons can support several content formats.",
			Resonance:       "The idea connects daily coding work with useful content.",
			Confidence:      0.9,
			ShortPost: domain.ContentFormatAngleV1{
				Suitable: true, Angle: "Use a short example.",
			},
			Thread: domain.ContentFormatAngleV1{
				Suitable: true, Angle: "Explain the workflow in steps.",
			},
			Article: domain.ContentFormatAngleV1{
				Suitable: true, Angle: "Explain the full approach.",
			},
			ClaimIDs: []string{input.Claims[0].ID},
		}},
	})
	if err != nil {
		return domain.AgentExecutionResponseV1{}, err
	}
	latency := int64(25)
	return domain.AgentExecutionResponseV1{
		ContractVersion: domain.AgentExecutionContractVersion,
		CandidateJSON:   candidates,
		Receipt: domain.AgentExecutionReceiptV1{
			ExecutorKind:        application.ContentScoutExecutorKind,
			ExecutorVersion:     application.ContentScoutExecutorVersion,
			SessionID:           "eve-session-content-scout",
			TurnID:              "eve-turn-content-scout",
			CompletedModelSteps: 1,
			RequestedRoute:      &request.Configuration.Route,
			GatewayGenerationID: "generation-content-scout",
			Usage: &domain.AgentUsageV1{
				InputTokens: 100, OutputTokens: 60, TotalTokens: 160,
			},
			CostUSD:             "0.001",
			LatencyMilliseconds: &latency,
		},
	}, nil
}

func loadContentScoutIntegrationConfiguration(
	t *testing.T,
) application.ContentScoutConfiguration {
	t.Helper()
	agent, err := os.Open(filepath.Join("..", "..", "config", "content-scout-agent.example.json"))
	if err != nil {
		t.Fatalf("open Content Scout agent configuration: %v", err)
	}
	defer agent.Close()
	disclosure, err := os.Open(filepath.Join("..", "..", "config", "content-scout-disclosure.example.json"))
	if err != nil {
		t.Fatalf("open Content Scout disclosure configuration: %v", err)
	}
	defer disclosure.Close()
	configuration, err := application.LoadContentScoutConfiguration(agent, disclosure)
	if err != nil {
		t.Fatalf("load Content Scout configuration: %v", err)
	}
	return configuration
}

var _ application.AgentExecutor = (*contentScoutIntegrationExecutor)(nil)
