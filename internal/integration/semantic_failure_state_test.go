package integration_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestSemanticWorkflowPersistsKnownEmptyFactIDsAfterGenerationFailure(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "noema.db")
	now := time.Date(2026, time.July, 21, 19, 0, 0, 0, time.UTC)
	document, _ := semanticConcurrencyEvidence(t)

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	store := sqlitestore.NewStore(database)
	factResult, err := (application.FactAnalyzer{
		Source:    semanticConcurrencyStaticSource{document: document},
		Extractor: semanticKnownEmptyFactExtractor{},
		Store:     store,
		NewID:     func() (string, error) { return "fact-analysis-empty", nil },
		Now:       func() time.Time { return now },
	}).Run(ctx, document.Revision.CanonicalID)
	if err != nil {
		database.Close()
		t.Fatalf("seed empty fact analysis: %v", err)
	}
	if factResult.Analysis.Run.FactIDs == nil || len(factResult.Analysis.Run.FactIDs) != 0 {
		database.Close()
		t.Fatalf("seeded fact ids = %#v, want known-empty", factResult.Analysis.Run.FactIDs)
	}

	generator := &semanticKnownEmptyFailingGenerator{}
	workflow := semanticConcurrencyWorkflow(
		semanticConcurrencyStaticSource{document: document},
		store,
		generator,
		"semantic-analysis-empty-failure",
		now,
	)
	_, err = workflow.Run(ctx, application.SemanticWorkflowRequest{
		FactAnalysisID: factResult.Analysis.Run.ID,
		Route:          semanticConcurrencyRoute(t),
	})
	var failure application.AnalysisError
	if !errors.As(err, &failure) || failure.AnalysisID != "semantic-analysis-empty-failure" ||
		failure.Category != "semantic-generation-failed" {
		database.Close()
		t.Fatalf("semantic workflow error = %v, want durable generation failure", err)
	}
	if generator.calls != 1 {
		database.Close()
		t.Fatalf("generator calls = %d, want 1", generator.calls)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close database before reload: %v", err)
	}

	reopened, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopened.Close()
	loaded, err := sqlitestore.NewStore(reopened).LoadSemanticAnalysis(ctx, failure.AnalysisID)
	if err != nil {
		t.Fatalf("reload failed semantic analysis: %v", err)
	}
	if loaded.Details.InputFactIDs == nil || *loaded.Details.InputFactIDs == nil ||
		len(*loaded.Details.InputFactIDs) != 0 {
		t.Fatalf("reloaded detail input fact ids = %#v, want known-empty", loaded.Details.InputFactIDs)
	}
	if loaded.Analysis.Run.InputFactIDs == nil || len(loaded.Analysis.Run.InputFactIDs) != 0 {
		t.Fatalf("reloaded run input fact ids = %#v, want known-empty", loaded.Analysis.Run.InputFactIDs)
	}
	if loaded.Details.ClaimIDs != nil {
		t.Fatalf("reloaded claim ids = %#v, want unavailable", loaded.Details.ClaimIDs)
	}
	if loaded.Details.AttemptedProcessingKey == nil {
		t.Fatal("reloaded failure did not preserve the completed preparation identity")
	}
}

type semanticKnownEmptyFactExtractor struct{}

func (semanticKnownEmptyFactExtractor) Name() string       { return "integration-empty" }
func (semanticKnownEmptyFactExtractor) Version() string    { return "1" }
func (semanticKnownEmptyFactExtractor) SchemaVersion() int { return 1 }

func (semanticKnownEmptyFactExtractor) Extract(
	domain.EvidenceDocument,
) ([]domain.FactDraft, domain.AnalysisOmissions, error) {
	return []domain.FactDraft{}, domain.AnalysisOmissions{}, nil
}

type semanticKnownEmptyFailingGenerator struct {
	calls int
}

func (generator *semanticKnownEmptyFailingGenerator) Generate(
	context.Context,
	application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	generator.calls++
	return application.SemanticGenerationResult{}, errors.New("synthetic generation failure")
}
