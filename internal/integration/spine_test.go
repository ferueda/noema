package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	sqlitestore "github.com/ferueda/noema/internal/adapters/sqlite"
	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
	"github.com/ferueda/noema/internal/platform"
)

func TestScanAndWorkerUseSQLiteAsTheirOnlyHandoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "noema.db")
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{result: application.SourceResult{
		SourceKind: "sessions",
		Coverage:   "complete",
		Chunks: []domain.EvidenceChunk{{
			Fingerprint: "sessions-entry-v1",
			Evidence: domain.EvidenceRef{
				SourceKind:     "sessions",
				SourceIdentity: "session-example",
				DocumentDigest: "document-digest-v1",
				EntryOrdinal:   4,
				ContentHash:    "content-hash-v1",
				Excerpt:        "A bounded generic example about separating producer and worker roles.",
			},
			CapturedAt: now,
		}},
	}}
	distiller := &fakeDistiller{}

	producerDatabase, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open producer database: %v", err)
	}
	producerStore := sqlitestore.NewStore(producerDatabase)
	scanner := application.Scanner{
		Store:     producerStore,
		Source:    source,
		Distiller: distiller,
		NewID:     platform.NewID,
		Now:       func() time.Time { return now },
	}
	request := application.ScanRequest{
		After:                 now.Add(-time.Hour),
		Before:                now,
		ContentScope:          "private",
		DistillationConfigKey: "distillation-test-v1",
		ContentScoutConfigKey: "content-scout-test-v1",
	}
	firstScan, err := scanner.Run(ctx, request)
	if err != nil {
		t.Fatalf("run producer: %v", err)
	}
	if firstScan.Reused {
		t.Fatal("first scan unexpectedly reused prior state")
	}
	if firstScan.Scan.JobID == "" {
		t.Fatal("first scan did not create a job")
	}
	if distiller.calls != 1 {
		t.Fatalf("distiller calls = %d, want 1", distiller.calls)
	}

	jobs, err := producerStore.ListJobs(ctx)
	if err != nil {
		t.Fatalf("list producer jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != domain.JobPending {
		t.Fatalf("producer jobs = %#v, want one pending job", jobs)
	}
	if err := producerDatabase.Close(); err != nil {
		t.Fatalf("close producer database: %v", err)
	}

	// Opening a fresh database connection proves the worker receives no
	// in-memory state from the producer process.
	consumerDatabase, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open consumer database: %v", err)
	}
	defer consumerDatabase.Close()
	consumerStore := sqlitestore.NewStore(consumerDatabase)
	worker := application.Worker{
		Store: consumerStore,
		Registry: application.NewStaticAgentRegistry(
			fakeContentScout{},
		),
		Now: func() time.Time { return now.Add(time.Minute) },
	}
	workerResult, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run consumer: %v", err)
	}
	if !workerResult.FoundJob || workerResult.JobID != firstScan.Scan.JobID {
		t.Fatalf("worker result = %#v, want job %s", workerResult, firstScan.Scan.JobID)
	}
	if len(workerResult.IdeaIDs) != 1 {
		t.Fatalf("worker ideas = %d, want 1", len(workerResult.IdeaIDs))
	}

	jobs, err = consumerStore.ListJobs(ctx)
	if err != nil {
		t.Fatalf("list completed jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != domain.JobSucceeded {
		t.Fatalf("completed jobs = %#v, want one succeeded job", jobs)
	}
	ideas, err := consumerStore.ListIdeas(ctx)
	if err != nil {
		t.Fatalf("list ideas: %v", err)
	}
	if len(ideas) != 1 {
		t.Fatalf("ideas = %d, want 1", len(ideas))
	}
	if ideas[0].ID != workerResult.IdeaIDs[0] {
		t.Fatalf("persisted idea = %s, worker reported %s", ideas[0].ID, workerResult.IdeaIDs[0])
	}
	if got := ideas[0].Evidence[0].SourceIdentity; got != "session-example" {
		t.Fatalf("idea evidence source = %q, want session-example", got)
	}

	secondScan, err := scannerWithStore(consumerStore, source, distiller, now).Run(ctx, request)
	if err != nil {
		t.Fatalf("repeat producer: %v", err)
	}
	if !secondScan.Reused {
		t.Fatal("repeat scan did not reuse the completed scan")
	}
	if distiller.calls != 1 {
		t.Fatalf("distiller calls after unchanged scan = %d, want 1", distiller.calls)
	}
	secondWorkerResult, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run consumer with empty queue: %v", err)
	}
	if secondWorkerResult.FoundJob {
		t.Fatalf("second worker unexpectedly found job %#v", secondWorkerResult)
	}

	changedRequest := request
	changedRequest.ContentScoutConfigKey = "content-scout-test-v2"
	changedScan, err := scannerWithStore(consumerStore, source, distiller, now).Run(ctx, changedRequest)
	if err != nil {
		t.Fatalf("run changed Content Scout configuration: %v", err)
	}
	if changedScan.Reused {
		t.Fatal("changed Content Scout configuration unexpectedly reused prior scan")
	}
	changedWorkerResult, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("run changed Content Scout job: %v", err)
	}
	if !changedWorkerResult.FoundJob {
		t.Fatal("changed Content Scout configuration did not create a job")
	}
	if distiller.calls != 1 {
		t.Fatalf("distiller calls after agent re-evaluation = %d, want 1", distiller.calls)
	}
	var observationCreatedEvents int
	if err := consumerDatabase.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM events WHERE type = 'observation.created'",
	).Scan(&observationCreatedEvents); err != nil {
		t.Fatalf("count observation.created events: %v", err)
	}
	if observationCreatedEvents != 1 {
		t.Fatalf(
			"observation.created events after agent re-evaluation = %d, want 1",
			observationCreatedEvents,
		)
	}
	ideas, err = consumerStore.ListIdeas(ctx)
	if err != nil {
		t.Fatalf("list re-evaluated ideas: %v", err)
	}
	if len(ideas) != 2 {
		t.Fatalf("ideas after re-evaluation = %d, want 2", len(ideas))
	}
	if ideas[0].ID == ideas[1].ID {
		t.Fatalf("re-evaluated ideas share id %s", ideas[0].ID)
	}
}

type fakeSource struct {
	result application.SourceResult
}

func (source *fakeSource) Read(
	context.Context,
	application.ScanRequest,
) (application.SourceResult, error) {
	return source.result, nil
}

type fakeDistiller struct {
	calls int
}

func (distiller *fakeDistiller) Distill(
	_ context.Context,
	chunks []domain.EvidenceChunk,
) ([]application.ObservationDraft, error) {
	distiller.calls++
	return []application.ObservationDraft{{
		Kind:        "insight",
		Summary:     "Separate event producers from agent consumers with a durable handoff.",
		Confidence:  0.9,
		EvidenceIDs: []string{chunks[0].ID},
	}}, nil
}

type fakeContentScout struct{}

func (fakeContentScout) Name() string {
	return "content-scout"
}

func (fakeContentScout) Version() string {
	return "v0"
}

func (fakeContentScout) Run(
	_ context.Context,
	request application.AgentRequest,
) ([]application.ContentIdeaDraft, error) {
	evidenceID := request.Observations[0].Evidence[0].ID
	return []application.ContentIdeaDraft{{
		Rank:            1,
		Concept:         "One binary can still have a real queue boundary",
		CoreLesson:      "Separate producer and consumer execution roles before separating deployments.",
		AudienceBenefit: "Helps developers test event-driven designs without premature infrastructure.",
		Hook:            "A CLI can be two processes without becoming two services.",
		Resonance:       "Many agent systems become distributed before their boundaries are proven.",
		Confidence:      0.9,
		ShortPost: domain.FormatAngle{
			Suitable: true,
			Angle:    "Show the producer and worker commands.",
		},
		Thread: domain.FormatAngle{
			Suitable: true,
			Angle:    "Walk through the SQLite handoff.",
		},
		Article: domain.FormatAngle{
			Suitable: true,
			Angle:    "Explain modular monoliths for event-driven agents.",
		},
		EvidenceIDs: []string{evidenceID},
	}}, nil
}

func scannerWithStore(
	store application.Store,
	source application.Source,
	distiller application.Distiller,
	now time.Time,
) application.Scanner {
	return application.Scanner{
		Store:     store,
		Source:    source,
		Distiller: distiller,
		NewID:     platform.NewID,
		Now:       func() time.Time { return now },
	}
}
