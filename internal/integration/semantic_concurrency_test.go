package integration_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	noemaevidence "github.com/ferueda/noema/internal/evidence"
	"github.com/ferueda/noema/internal/platform"
)

func TestSemanticWorkflowSerializesGenerationAcrossIndependentSQLiteHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	databasePath := filepath.Join(t.TempDir(), "noema.db")
	now := time.Date(2026, time.July, 21, 18, 0, 0, 0, time.UTC)
	document, reference := semanticConcurrencyEvidence(t)

	firstDatabase, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open first database: %v", err)
	}
	defer firstDatabase.Close()
	firstStore := sqlitestore.NewStore(firstDatabase)

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
		Store: firstStore,
		NewID: func() (string, error) { return "fact-analysis-concurrent", nil },
		Now:   func() time.Time { return now },
	}).Run(ctx, document.Revision.CanonicalID)
	if err != nil {
		t.Fatalf("seed fact analysis: %v", err)
	}

	// Open a second database and store after seeding. The workflows therefore
	// coordinate through SQLite, not a shared connection or in-memory store.
	secondDatabase, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open second database: %v", err)
	}
	defer secondDatabase.Close()
	secondStore := sqlitestore.NewStore(secondDatabase)

	generator := newSemanticConcurrencyGenerator()
	defer generator.releaseGeneration()
	workflowSource := &semanticConcurrencySignalingSource{
		document: document,
		reads:    make(chan struct{}, 2),
	}
	request := application.SemanticWorkflowRequest{
		FactAnalysisID: factResult.Analysis.Run.ID,
		Route:          semanticConcurrencyRoute(t),
	}
	firstWorkflow := semanticConcurrencyWorkflow(
		workflowSource, firstStore, generator, "semantic-analysis-first", now,
	)
	secondWorkflow := semanticConcurrencyWorkflow(
		workflowSource, secondStore, generator, "semantic-analysis-second", now,
	)

	firstDone := make(chan semanticConcurrencyOutcome, 1)
	go func() {
		result, runErr := firstWorkflow.Run(ctx, request)
		firstDone <- semanticConcurrencyOutcome{result: result, err: runErr}
	}()
	waitForSemanticConcurrencySignal(t, ctx, generator.entered, "first generation")
	waitForSemanticConcurrencySignal(t, ctx, workflowSource.reads, "first source read")

	secondDone := make(chan semanticConcurrencyOutcome, 1)
	go func() {
		result, runErr := secondWorkflow.Run(ctx, request)
		secondDone <- semanticConcurrencyOutcome{result: result, err: runErr}
	}()
	waitForSemanticConcurrencySignal(t, ctx, workflowSource.reads, "second source read")
	waitForSemanticConcurrencyConnection(t, ctx, secondDatabase)

	// The first workflow holds BEGIN IMMEDIATE while generation is blocked. The
	// second independently opened handle must wait instead of generating again.
	select {
	case outcome := <-secondDone:
		t.Fatalf("second workflow finished before first committed: result=%#v err=%v", outcome.result, outcome.err)
	case <-generator.duplicate:
		t.Fatal("second workflow called the generator while the first held the write attempt")
	case <-time.After(100 * time.Millisecond):
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("generation calls while first attempt is blocked = %d, want 1", got)
	}

	generator.releaseGeneration()
	firstOutcome := waitForSemanticConcurrencyOutcome(t, ctx, firstDone, "first workflow")
	secondOutcome := waitForSemanticConcurrencyOutcome(t, ctx, secondDone, "second workflow")
	if firstOutcome.err != nil {
		t.Fatalf("first workflow: %v", firstOutcome.err)
	}
	if secondOutcome.err != nil {
		t.Fatalf("second workflow: %v", secondOutcome.err)
	}
	if firstOutcome.result.Reused {
		t.Fatal("first workflow unexpectedly reused a prior semantic analysis")
	}
	if !secondOutcome.result.Reused {
		t.Fatal("waiting workflow did not reuse the completed semantic analysis")
	}
	if firstOutcome.result.Record.Analysis.Run.ID != "semantic-analysis-first" ||
		secondOutcome.result.Record.Analysis.Run.ID != firstOutcome.result.Record.Analysis.Run.ID {
		t.Fatalf(
			"semantic analysis identities = %q / %q, want the first committed identity",
			firstOutcome.result.Record.Analysis.Run.ID,
			secondOutcome.result.Record.Analysis.Run.ID,
		)
	}
	if got := generator.calls.Load(); got != 1 {
		t.Fatalf("generation calls = %d, want 1", got)
	}

	assertSemanticConcurrencyCount(t, ctx, firstDatabase, "claims", 1)
	assertSemanticConcurrencyCount(t, ctx, firstDatabase, "events", 2)
	assertSemanticConcurrencyCount(t, ctx, firstDatabase, "event_subject_types", 2)
	assertSemanticConcurrencyCount(t, ctx, firstDatabase, "jobs", 0)
	assertSemanticConcurrencyCount(t, ctx, firstDatabase, "agent_runs", 0)
	assertSemanticConcurrencyCount(t, ctx, firstDatabase, "content_ideas", 0)
}

type semanticConcurrencyStaticSource struct {
	document domain.EvidenceDocument
}

func (source semanticConcurrencyStaticSource) Read(
	_ context.Context,
	canonicalID string,
) (domain.EvidenceDocument, error) {
	if canonicalID != source.document.Revision.CanonicalID {
		return domain.EvidenceDocument{}, errors.New("unexpected source identity")
	}
	return source.document, nil
}

type semanticConcurrencySignalingSource struct {
	document domain.EvidenceDocument
	reads    chan struct{}
}

func (source *semanticConcurrencySignalingSource) Read(
	ctx context.Context,
	canonicalID string,
) (domain.EvidenceDocument, error) {
	if canonicalID != source.document.Revision.CanonicalID {
		return domain.EvidenceDocument{}, errors.New("unexpected source identity")
	}
	select {
	case source.reads <- struct{}{}:
		return source.document, nil
	case <-ctx.Done():
		return domain.EvidenceDocument{}, ctx.Err()
	}
}

type semanticConcurrencyFactExtractor struct {
	draft domain.FactDraft
}

func (semanticConcurrencyFactExtractor) Name() string       { return "integration-fixture" }
func (semanticConcurrencyFactExtractor) Version() string    { return "1" }
func (semanticConcurrencyFactExtractor) SchemaVersion() int { return 1 }

func (extractor semanticConcurrencyFactExtractor) Extract(
	domain.EvidenceDocument,
) ([]domain.FactDraft, domain.AnalysisOmissions, error) {
	return []domain.FactDraft{extractor.draft}, domain.AnalysisOmissions{}, nil
}

type semanticConcurrencyGenerator struct {
	calls       atomic.Int32
	entered     chan struct{}
	duplicate   chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func newSemanticConcurrencyGenerator() *semanticConcurrencyGenerator {
	return &semanticConcurrencyGenerator{
		entered:   make(chan struct{}),
		duplicate: make(chan struct{}, 1),
		release:   make(chan struct{}),
	}
}

func (generator *semanticConcurrencyGenerator) Generate(
	ctx context.Context,
	request application.SemanticGenerationRequest,
) (application.SemanticGenerationResult, error) {
	call := generator.calls.Add(1)
	if call == 1 {
		close(generator.entered)
	} else {
		select {
		case generator.duplicate <- struct{}{}:
		default:
		}
	}
	select {
	case <-generator.release:
	case <-ctx.Done():
		return application.SemanticGenerationResult{}, ctx.Err()
	}
	return application.SemanticGenerationResult{
		Candidates: []domain.ClaimCandidate{{
			Type: domain.ClaimTypeLesson, Statement: "A durable lock keeps semantic generation single-flight.",
			Status: domain.ClaimStatusInferred, Confidence: 0.9,
			SupportingEvidenceIDs: []string{request.Input.Facts[0].EvidenceIDs[0]},
			SupportingFactIDs:     []string{request.Input.Facts[0].ID},
			Attribution:           domain.ClaimAttributionUnknown,
		}},
		Model: domain.ModelExecutionMetadata{
			ResolvedProvider: "cerebras", ResolvedModel: "openai/gpt-oss-120b",
			RequestID: "semantic-concurrency-request",
		},
	}, nil
}

func (generator *semanticConcurrencyGenerator) releaseGeneration() {
	generator.releaseOnce.Do(func() { close(generator.release) })
}

func semanticConcurrencyWorkflow(
	source application.SessionEvidenceSource,
	store *sqlitestore.Store,
	generator application.SemanticGenerator,
	analysisID string,
	now time.Time,
) application.SemanticWorkflow {
	return application.SemanticWorkflow{
		Source: source,
		Facts:  store,
		Store:  store,
		Analyzer: application.SemanticAnalyzer{
			Generator: generator,
			Privacy:   application.PrivacyPolicy{},
			NewID:     func() (string, error) { return analysisID, nil },
			Now:       func() time.Time { return now },
		},
	}
}

func semanticConcurrencyRoute(t *testing.T) domain.ValidatedModelRoute {
	t.Helper()
	configuration := json.RawMessage(`{"profile":"semantic-v1"}`)
	digest, err := platform.Fingerprint(configuration)
	if err != nil {
		t.Fatalf("fingerprint semantic route: %v", err)
	}
	return domain.ValidatedModelRoute{
		Requested: domain.RequestedModelRoute{
			Alias: "semantic-v1", Gateway: "vercel-ai-gateway", Model: "openai/gpt-oss-120b",
			Provider: "cerebras", RouteVersion: "route-v1",
			PrivacyPolicyVersion: application.PrivacyPolicyVersion,
		},
		SanitizedConfig: configuration,
		ConfigDigest:    digest,
	}
}

func semanticConcurrencyEvidence(t *testing.T) (domain.EvidenceDocument, domain.EvidenceRef) {
	t.Helper()
	text := "go test ./..."
	hash := sha256.Sum256([]byte(text))
	first, last, segment := 0, 0, 0
	document := domain.EvidenceDocument{
		Revision: domain.EvidenceRevision{
			SourceKind: domain.EvidenceSourceSessions, CanonicalID: "synthetic@local:concurrent",
			NativeSourceKind: "synthetic", SourceInstanceID: "local", NativeID: "concurrent",
			DocumentDigest: domain.Digest{
				Scheme: "sha256-sessions-document-jcs-v1", Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			},
		},
		Selection: domain.EvidenceSelection{
			Mode: "full",
			Entries: domain.EntrySelection{
				Selected: 1, Total: 1, FirstOrdinal: &first, LastOrdinal: &last,
			},
			Segments: domain.CountSelection{Selected: 1, Total: 1},
			SegmentText: domain.ByteSelection{
				EmittedUTF8Bytes: len(text), OriginalUTF8Bytes: len(text),
			},
			Coverage: domain.CoverageCompleteRetainedSnapshot,
		},
		Entries: []domain.EvidenceEntry{{
			Ordinal: 0, Kind: "tool-call", Actor: "model", ToolCallID: "call-1", ToolName: "exec_command",
			Content: []domain.EvidenceSegment{{
				Ordinal: 0, Kind: "text", Origin: "model", OriginConfidence: "high",
				Text: &domain.SelectedText{
					Text: text, OriginalUTF8Bytes: len(text), EmittedUTF8Bytes: len(text),
					ContentHash: domain.Digest{Scheme: "sha256-utf8-v1", Digest: hex.EncodeToString(hash[:])},
				},
			}},
		}},
	}
	reference, err := noemaevidence.SessionsReference(document, 0, &segment)
	if err != nil {
		t.Fatalf("build Sessions evidence reference: %v", err)
	}
	return document, reference
}

type semanticConcurrencyOutcome struct {
	result application.SemanticWorkflowResult
	err    error
}

func waitForSemanticConcurrencySignal(
	t *testing.T,
	ctx context.Context,
	signal <-chan struct{},
	name string,
) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v", name, ctx.Err())
	}
}

func waitForSemanticConcurrencyOutcome(
	t *testing.T,
	ctx context.Context,
	done <-chan semanticConcurrencyOutcome,
	name string,
) semanticConcurrencyOutcome {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v", name, ctx.Err())
		return semanticConcurrencyOutcome{}
	}
}

func waitForSemanticConcurrencyConnection(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if database.Stats().InUse == 1 {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("wait for second SQLite attempt: %v", ctx.Err())
		}
	}
}

func assertSemanticConcurrencyCount(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	table string,
	want int,
) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}
