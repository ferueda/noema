package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

func TestEventPublicationReturnsNoWorkWithoutCallingPublisher(t *testing.T) {
	publisher := &publicationTestPublisher{}
	publication := EventPublication{
		Store: &publicationTestStore{}, Publisher: publisher,
	}

	result, err := publication.PublishOne(context.Background())
	if err != nil {
		t.Fatalf("publish one: %v", err)
	}
	if result.Status != PublicationNoWork || result.EventID != "" ||
		result.AcknowledgementID != "" || result.LastFailureCategory != "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if publisher.calls != 0 {
		t.Fatalf("publisher calls = %d, want 0", publisher.calls)
	}
}

func TestEventPublicationRecordsDeliveredAcknowledgement(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.FixedZone("test", 3600))
	attempt := publicationTestAttemptFor(t)
	publisher := &publicationTestPublisher{acknowledgementID: "transport-ack"}
	publication := EventPublication{
		Store:     &publicationTestStore{attempt: attempt, found: true},
		Publisher: publisher,
		Now:       func() time.Time { return now },
	}

	result, err := publication.PublishOne(context.Background())
	if err != nil {
		t.Fatalf("publish one: %v", err)
	}
	if result.Status != PublicationDelivered ||
		result.EventID != attempt.event.ID ||
		result.AcknowledgementID != "transport-ack" ||
		result.LastFailureCategory != "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if attempt.deliveredAck != "transport-ack" ||
		!attempt.deliveredAt.Equal(now.UTC()) ||
		attempt.failureCategory != "" ||
		attempt.rollbackCalls != 0 {
		t.Fatalf("unexpected terminal state: %#v", attempt)
	}
	if publisher.event.ID != attempt.event.ID {
		t.Fatalf("published event = %q, want %q", publisher.event.ID, attempt.event.ID)
	}
}

func TestEventPublicationAcceptsEmptyAcknowledgement(t *testing.T) {
	attempt := publicationTestAttemptFor(t)
	publication := EventPublication{
		Store:     &publicationTestStore{attempt: attempt, found: true},
		Publisher: &publicationTestPublisher{},
	}

	result, err := publication.PublishOne(context.Background())
	if err != nil {
		t.Fatalf("publish one: %v", err)
	}
	if result.Status != PublicationDelivered ||
		result.EventID != attempt.event.ID ||
		result.AcknowledgementID != "" ||
		attempt.deliveredAck != "" {
		t.Fatalf("unexpected result/state: %#v %#v", result, attempt)
	}
}

func TestEventPublicationRecordsSafeTransportFailure(t *testing.T) {
	attempt := publicationTestAttemptFor(t)
	publisher := &publicationTestPublisher{
		err: errors.New("private transport response: secret-token"),
	}
	publication := EventPublication{
		Store: &publicationTestStore{attempt: attempt, found: true}, Publisher: publisher,
	}

	result, err := publication.PublishOne(context.Background())
	if err != nil {
		t.Fatalf("publish one: %v", err)
	}
	if result.Status != PublicationFailed ||
		result.EventID != attempt.event.ID ||
		result.LastFailureCategory != domain.OutboxFailureTransport {
		t.Fatalf("unexpected result: %#v", result)
	}
	if strings.Contains(result.LastFailureCategory, "secret-token") ||
		attempt.failureCategory != domain.OutboxFailureTransport {
		t.Fatalf("unsafe failure state: %#v", attempt)
	}
}

func TestEventPublicationClassifiesDeadlineAndInvalidAcknowledgement(t *testing.T) {
	tests := []struct {
		name      string
		publisher publicationTestPublisher
		want      string
	}{
		{
			name: "deadline",
			publisher: publicationTestPublisher{
				err: errors.Join(errors.New("remote details"), context.DeadlineExceeded),
			},
			want: domain.OutboxFailureTimeout,
		},
		{
			name:      "invalid acknowledgement",
			publisher: publicationTestPublisher{acknowledgementID: " unsafe "},
			want:      domain.OutboxFailureInvalidAcknowledgement,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempt := publicationTestAttemptFor(t)
			publication := EventPublication{
				Store:     &publicationTestStore{attempt: attempt, found: true},
				Publisher: &test.publisher,
			}

			result, err := publication.PublishOne(context.Background())
			if err != nil {
				t.Fatalf("publish one: %v", err)
			}
			if result.Status != PublicationFailed ||
				result.LastFailureCategory != test.want ||
				attempt.failureCategory != test.want {
				t.Fatalf("unexpected result/state: %#v %#v", result, attempt)
			}
		})
	}
}

func TestEventPublicationFinalizesFailureAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempt := publicationTestAttemptFor(t)
	attempt.inspectTerminalContext = true
	publisher := &publicationTestPublisher{
		publish: func(context.Context, domain.DomainEvent) (string, error) {
			cancel()
			return "", context.Canceled
		},
	}
	publication := EventPublication{
		Store: &publicationTestStore{attempt: attempt, found: true}, Publisher: publisher,
	}

	result, err := publication.PublishOne(ctx)
	if err != nil {
		t.Fatalf("publish one: %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("caller context error = %v, want canceled", ctx.Err())
	}
	if result.Status != PublicationFailed ||
		attempt.failureCategory != domain.OutboxFailureTransport ||
		!attempt.terminalContextActive {
		t.Fatalf("unexpected result/state: %#v %#v", result, attempt)
	}
}

func TestEventPublicationRecordsDeliveryAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempt := publicationTestAttemptFor(t)
	attempt.inspectTerminalContext = true
	publisher := &publicationTestPublisher{
		publish: func(context.Context, domain.DomainEvent) (string, error) {
			cancel()
			return "accepted", nil
		},
	}
	publication := EventPublication{
		Store: &publicationTestStore{attempt: attempt, found: true}, Publisher: publisher,
	}

	result, err := publication.PublishOne(ctx)
	if err != nil {
		t.Fatalf("publish one: %v", err)
	}
	if result.Status != PublicationDelivered ||
		attempt.deliveredAck != "accepted" ||
		!attempt.terminalContextActive {
		t.Fatalf("unexpected result/state: %#v %#v", result, attempt)
	}
}

func TestEventPublicationRollsBackWhenTerminalWriteFails(t *testing.T) {
	attempt := publicationTestAttemptFor(t)
	attempt.markDeliveredErr = errors.New("write failed")
	publication := EventPublication{
		Store:     &publicationTestStore{attempt: attempt, found: true},
		Publisher: &publicationTestPublisher{},
	}

	_, err := publication.PublishOne(context.Background())
	if err == nil || !strings.Contains(err.Error(), "record event publication") {
		t.Fatalf("error = %v, want terminal write failure", err)
	}
	if attempt.rollbackCalls != 1 {
		t.Fatalf("rollback calls = %d, want 1", attempt.rollbackCalls)
	}
}

type publicationTestStore struct {
	attempt PublicationAttempt
	found   bool
	err     error
}

func (store *publicationTestStore) BeginPublication(
	context.Context,
) (PublicationAttempt, bool, error) {
	return store.attempt, store.found, store.err
}

type publicationTestPublisher struct {
	acknowledgementID string
	err               error
	event             domain.DomainEvent
	calls             int
	publish           func(context.Context, domain.DomainEvent) (string, error)
}

func (publisher *publicationTestPublisher) Publish(
	ctx context.Context,
	event domain.DomainEvent,
) (string, error) {
	publisher.calls++
	publisher.event = event
	if publisher.publish != nil {
		return publisher.publish(ctx, event)
	}
	return publisher.acknowledgementID, publisher.err
}

type publicationTestAttempt struct {
	event                  domain.DomainEvent
	outbox                 domain.OutboxRecord
	deliveredAck           string
	deliveredAt            time.Time
	failureCategory        string
	rollbackCalls          int
	markDeliveredErr       error
	recordFailureErr       error
	inspectTerminalContext bool
	terminalContextActive  bool
}

func publicationTestAttemptFor(t *testing.T) *publicationTestAttempt {
	t.Helper()
	event, err := domain.NewDomainEvent(
		"analysis.completed",
		domain.EventReferenceAnalysis,
		"analysis-1",
		map[string]any{
			"analysisId": "analysis-1",
		},
		[]domain.EventReference{{
			RecordType: domain.EventReferenceAnalysis,
			RecordID:   "analysis-1",
		}},
		time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	outbox, err := domain.NewOutboxRecord(event.ID)
	if err != nil {
		t.Fatalf("new outbox: %v", err)
	}
	return &publicationTestAttempt{event: event, outbox: outbox}
}

func (attempt *publicationTestAttempt) Event() domain.DomainEvent {
	return attempt.event
}

func (attempt *publicationTestAttempt) Outbox() domain.OutboxRecord {
	return attempt.outbox
}

func (attempt *publicationTestAttempt) MarkDelivered(
	ctx context.Context,
	acknowledgementID string,
	deliveredAt time.Time,
) error {
	attempt.inspectContext(ctx)
	attempt.deliveredAck = acknowledgementID
	attempt.deliveredAt = deliveredAt
	return attempt.markDeliveredErr
}

func (attempt *publicationTestAttempt) RecordFailure(
	ctx context.Context,
	category string,
) error {
	attempt.inspectContext(ctx)
	attempt.failureCategory = category
	return attempt.recordFailureErr
}

func (attempt *publicationTestAttempt) Rollback(context.Context) error {
	attempt.rollbackCalls++
	return nil
}

func (attempt *publicationTestAttempt) inspectContext(ctx context.Context) {
	if !attempt.inspectTerminalContext {
		return
	}
	_, hasDeadline := ctx.Deadline()
	attempt.terminalContextActive = ctx.Err() == nil && hasDeadline
}
