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

func TestSemanticEventSurvivesRestartAndPublishesOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "noema.db")
	now := time.Date(2026, time.July, 28, 18, 0, 0, 0, time.UTC)
	document, reference := semanticConcurrencyEvidence(t)

	database, err := sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	store := sqlitestore.NewStore(database)
	factResult, err := (application.FactAnalyzer{
		Source: semanticConcurrencyStaticSource{document: document},
		Extractor: semanticConcurrencyFactExtractor{draft: domain.FactDraft{
			Kind: "command",
			Value: domain.FactValue{Command: &domain.SelectedText{
				Text: "go test ./...", OriginalUTF8Bytes: len("go test ./..."),
				EmittedUTF8Bytes: len("go test ./..."), ContentHash: document.Entries[0].Content[0].Text.ContentHash,
			}},
			Outcome: domain.FactOutcomeSuccess, ParseRule: "integration-fixture-v1",
			Evidence: []domain.EvidenceRef{reference},
		}},
		Store: store,
		NewID: func() (string, error) { return "fact-analysis-publication", nil },
		Now:   func() time.Time { return now },
	}).Run(ctx, document.Revision.CanonicalID)
	if err != nil {
		t.Fatalf("seed fact analysis: %v", err)
	}
	generator := &emptySemanticGenerator{}
	workflow := semanticConcurrencyWorkflow(
		semanticConcurrencyStaticSource{document: document},
		store,
		generator,
		"semantic-analysis-publication",
		now,
	)
	request := application.SemanticWorkflowRequest{
		FactAnalysisID: factResult.Analysis.Run.ID,
		Route:          semanticConcurrencyRoute(t),
	}
	first, err := workflow.Run(ctx, request)
	if err != nil || first.Reused || len(first.Record.Events) != 1 {
		t.Fatalf("first semantic run = %#v, %v", first, err)
	}
	eventID := first.Record.Events[0].ID
	if err := database.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	database, err = sqlitestore.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer database.Close()
	store = sqlitestore.NewStore(database)
	pending, err := store.ListEvents(ctx, domain.OutboxStatusPending)
	if err != nil || len(pending) != 1 ||
		pending[0].Event.ID != eventID ||
		pending[0].Outbox.AttemptCount != 0 {
		t.Fatalf("pending after restart = %#v, %v", pending, err)
	}

	publisher := &recordingEventPublisher{}
	publication := application.EventPublication{
		Store: store, Publisher: publisher, Now: func() time.Time { return now.Add(time.Minute) },
	}
	delivered, err := publication.PublishOne(ctx)
	if err != nil || delivered.Status != application.PublicationDelivered ||
		delivered.EventID != eventID || len(publisher.events) != 1 {
		t.Fatalf("publication = %#v, calls=%d, err=%v", delivered, len(publisher.events), err)
	}
	stored, found, err := store.LoadEvent(ctx, eventID)
	if err != nil || !found ||
		stored.Outbox.Status != domain.OutboxStatusDelivered ||
		stored.Outbox.AttemptCount != 1 {
		t.Fatalf("delivered event = %#v, found=%v, err=%v", stored, found, err)
	}

	unexpectedGenerator := &emptySemanticGenerator{err: errors.New("must not generate")}
	reused, err := semanticConcurrencyWorkflow(
		semanticConcurrencyStaticSource{document: document},
		store,
		unexpectedGenerator,
		"semantic-analysis-duplicate",
		now,
	).Run(ctx, request)
	if err != nil || !reused.Reused ||
		reused.Record.Analysis.Run.ID != first.Record.Analysis.Run.ID ||
		unexpectedGenerator.calls != 0 {
		t.Fatalf("reused semantic run = %#v, calls=%d, err=%v", reused, unexpectedGenerator.calls, err)
	}
	noWork, err := publication.PublishOne(ctx)
	if err != nil || noWork.Status != application.PublicationNoWork ||
		len(publisher.events) != 1 {
		t.Fatalf("repeat publication = %#v, calls=%d, err=%v", noWork, len(publisher.events), err)
	}
	assertSemanticConcurrencyCount(t, ctx, database, "events", 1)
	assertSemanticConcurrencyCount(t, ctx, database, "event_outbox", 1)
}

type emptySemanticGenerator struct {
	calls int
	err   error
}

func (generator *emptySemanticGenerator) Generate(
	context.Context,
	application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	generator.calls++
	if generator.err != nil {
		return application.SemanticGenerationResult{}, generator.err
	}
	return application.SemanticGenerationResult{
		Candidates: []domain.ClaimCandidate{},
		Model: domain.ModelExecutionMetadata{
			ResolvedProvider: "cerebras",
			ResolvedModel:    "openai/gpt-oss-120b",
			RequestID:        "semantic-publication-request",
		},
	}, nil
}

type recordingEventPublisher struct {
	events []domain.DomainEvent
}

func (publisher *recordingEventPublisher) Publish(
	_ context.Context,
	event domain.DomainEvent,
) (string, error) {
	publisher.events = append(publisher.events, event)
	return "integration-ack", nil
}
