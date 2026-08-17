package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

func TestEventStoreInspectsAtomicSemanticEventsAndOutbox(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC), "events", true)
	seedSemanticStoreFacts(t, ctx, store, record)
	commitSemanticStoreRecord(t, ctx, store, record)

	records, err := store.ListEvents(ctx, domain.OutboxStatusPending)
	if err != nil {
		t.Fatalf("list pending events: %v", err)
	}
	if len(records) != len(record.Events) {
		t.Fatalf("pending event count = %d, want %d", len(records), len(record.Events))
	}
	for _, stored := range records {
		if stored.Outbox.EventID != stored.Event.ID ||
			stored.Outbox.Status != domain.OutboxStatusPending ||
			stored.Outbox.AttemptCount != 0 {
			t.Fatalf("stored event/outbox = %#v", stored)
		}
		loaded, found, err := store.LoadEvent(ctx, stored.Event.ID)
		if err != nil || !found {
			t.Fatalf("load event %s = %#v, %v, %v", stored.Event.ID, loaded, found, err)
		}
		if loaded.Event.Fingerprint != stored.Event.Fingerprint ||
			loaded.Outbox != stored.Outbox {
			t.Fatalf("loaded event = %#v, want %#v", loaded, stored)
		}
	}
	delivered, err := store.ListEvents(ctx, domain.OutboxStatusDelivered)
	if err != nil || len(delivered) != 0 {
		t.Fatalf("delivered events = %#v, %v; want empty", delivered, err)
	}
	if _, found, err := store.LoadEvent(ctx, "evt_missing"); err != nil || found {
		t.Fatalf("load missing event = %v, %v; want not found", found, err)
	}
}

func TestPublicationAttemptRecordsFailureThenDelivery(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC), "publication", false)
	commitSemanticStoreRecord(t, ctx, store, record)

	first, found, err := store.BeginPublication(ctx)
	if err != nil || !found {
		t.Fatalf("begin first publication = %v, %v", found, err)
	}
	eventID := first.Event().ID
	if first.Outbox().AttemptCount != 0 {
		t.Fatalf("initial outbox = %#v", first.Outbox())
	}
	if err := first.RecordFailure(ctx, domain.OutboxFailureTimeout); err != nil {
		t.Fatalf("record publication failure: %v", err)
	}
	failed, found, err := store.LoadEvent(ctx, eventID)
	if err != nil || !found {
		t.Fatalf("load failed publication = %#v, %v, %v", failed, found, err)
	}
	if failed.Outbox.Status != domain.OutboxStatusPending ||
		failed.Outbox.AttemptCount != 1 ||
		failed.Outbox.LastFailureCategory != domain.OutboxFailureTimeout {
		t.Fatalf("failed outbox = %#v", failed.Outbox)
	}

	retry, found, err := store.BeginPublication(ctx)
	if err != nil || !found || retry.Event().ID != eventID {
		t.Fatalf("begin retry = %v, %v, event %q", found, err, retry.Event().ID)
	}
	deliveredAt := time.Date(2026, 7, 28, 13, 1, 0, 0, time.UTC)
	if err := retry.MarkDelivered(ctx, "", deliveredAt); err != nil {
		t.Fatalf("mark publication delivered: %v", err)
	}
	delivered, found, err := store.LoadEvent(ctx, eventID)
	if err != nil || !found {
		t.Fatalf("load delivered publication = %#v, %v, %v", delivered, found, err)
	}
	if delivered.Outbox.Status != domain.OutboxStatusDelivered ||
		delivered.Outbox.AttemptCount != 2 ||
		delivered.Outbox.LastFailureCategory != "" ||
		delivered.Outbox.AcknowledgementID != "" ||
		delivered.Outbox.DeliveredAt == nil ||
		!delivered.Outbox.DeliveredAt.Equal(deliveredAt) {
		t.Fatalf("delivered outbox = %#v", delivered.Outbox)
	}
	if _, found, err := store.BeginPublication(ctx); err != nil || found {
		t.Fatalf("publication after delivery = %v, %v; want no work", found, err)
	}
}

func TestPublicationAttemptRollbackPreservesPendingState(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 28, 14, 0, 0, 0, time.UTC), "rollback-publication", false)
	commitSemanticStoreRecord(t, ctx, store, record)

	attempt, found, err := store.BeginPublication(ctx)
	if err != nil || !found {
		t.Fatalf("begin publication = %v, %v", found, err)
	}
	eventID := attempt.Event().ID
	if err := attempt.Rollback(ctx); err != nil {
		t.Fatalf("rollback publication: %v", err)
	}
	loaded, found, err := store.LoadEvent(ctx, eventID)
	if err != nil || !found {
		t.Fatalf("load rolled back publication = %#v, %v, %v", loaded, found, err)
	}
	if loaded.Outbox.Status != domain.OutboxStatusPending ||
		loaded.Outbox.AttemptCount != 0 {
		t.Fatalf("rolled back outbox = %#v", loaded.Outbox)
	}
}

func TestPublicationAttemptsSerializeAcrossSQLiteHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "noema.db")
	firstDatabase, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open first database: %v", err)
	}
	defer firstDatabase.Close()
	firstStore := NewStore(firstDatabase)
	record := semanticStoreRecord(time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC), "concurrent-publication", false)
	commitSemanticStoreRecord(t, ctx, firstStore, record)

	secondDatabase, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open second database: %v", err)
	}
	defer secondDatabase.Close()
	secondStore := NewStore(secondDatabase)

	first, found, err := firstStore.BeginPublication(ctx)
	if err != nil || !found {
		t.Fatalf("begin first publication = %v, %v", found, err)
	}
	result := make(chan publicationBeginResult, 1)
	go func() {
		attempt, found, beginErr := secondStore.BeginPublication(ctx)
		result <- publicationBeginResult{attempt: attempt, found: found, err: beginErr}
	}()
	select {
	case second := <-result:
		if second.attempt != nil {
			_ = second.attempt.Rollback(ctx)
		}
		t.Fatalf("second publication did not wait: %#v", second)
	case <-time.After(100 * time.Millisecond):
	}
	if err := first.MarkDelivered(ctx, "ack-first", time.Date(2026, 7, 28, 15, 1, 0, 0, time.UTC)); err != nil {
		t.Fatalf("complete first publication: %v", err)
	}
	select {
	case second := <-result:
		if second.err != nil || second.found || second.attempt != nil {
			t.Fatalf("second publication after delivery = %#v, want no work", second)
		}
	case <-ctx.Done():
		t.Fatal("second publication did not finish after first committed")
	}
}

func TestEventInspectionRejectsTamperedEventContent(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "noema.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	store := NewStore(database)
	record := semanticStoreRecord(time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC), "tampered-event", false)
	commitSemanticStoreRecord(t, ctx, store, record)
	eventID := record.Events[0].ID
	if _, err := database.ExecContext(ctx,
		"UPDATE events SET payload_json = ? WHERE id = ?",
		`{"analysisId":"changed","claimIds":[]}`,
		eventID,
	); err != nil {
		t.Fatalf("tamper event: %v", err)
	}
	if _, _, err := store.LoadEvent(ctx, eventID); err == nil {
		t.Fatal("tampered event was accepted")
	}
}

type publicationBeginResult struct {
	attempt application.PublicationAttempt
	found   bool
	err     error
}

func commitSemanticStoreRecord(
	t *testing.T,
	ctx context.Context,
	store *Store,
	record application.SemanticAnalysisRecord,
) {
	t.Helper()
	attempt, err := store.BeginSemanticAttempt(ctx)
	if err != nil {
		t.Fatalf("begin semantic attempt: %v", err)
	}
	defer attempt.Rollback(ctx)
	if err := attempt.Commit(ctx, record); err != nil {
		t.Fatalf("commit semantic record: %v", err)
	}
}
