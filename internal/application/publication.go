package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

const (
	PublicationNoWork    = "no-work"
	PublicationDelivered = "delivered"
	PublicationFailed    = "failed"

	publicationTerminalTimeout = 5 * time.Second
)

// EventPublicationStore owns one atomic attempt to publish the oldest pending
// outbox record. Beginning an attempt must not publish or change the record.
type EventPublicationStore interface {
	BeginPublication(context.Context) (PublicationAttempt, bool, error)
}

// PublicationAttempt keeps the selected event and outbox record stable while
// one transport call is made.
type PublicationAttempt interface {
	Event() domain.DomainEvent
	Outbox() domain.OutboxRecord
	MarkDelivered(context.Context, string, time.Time) error
	RecordFailure(context.Context, string) error
	Rollback(context.Context) error
}

// EventPublisher hands one complete domain event to a transport. The optional
// acknowledgement ID must be bounded and safe to retain.
type EventPublisher interface {
	Publish(context.Context, domain.DomainEvent) (string, error)
}

type PublicationResult struct {
	Status              string `json:"status"`
	EventID             string `json:"eventId,omitempty"`
	AcknowledgementID   string `json:"acknowledgementId,omitempty"`
	LastFailureCategory string `json:"lastFailureCategory,omitempty"`
}

// EventPublication performs at most one explicit publication attempt. It does
// not retry, discover consumers, or wait for downstream work.
type EventPublication struct {
	Store     EventPublicationStore
	Publisher EventPublisher
	Now       func() time.Time
}

func (publication EventPublication) PublishOne(
	ctx context.Context,
) (PublicationResult, error) {
	if publication.Store == nil || publication.Publisher == nil {
		return PublicationResult{}, errors.New("event publication is not configured")
	}
	now := publication.Now
	if now == nil {
		now = time.Now
	}

	attempt, found, err := publication.Store.BeginPublication(ctx)
	if err != nil {
		return PublicationResult{}, fmt.Errorf("begin event publication: %w", err)
	}
	if !found {
		return PublicationResult{Status: PublicationNoWork}, nil
	}

	terminal := false
	defer func() {
		if terminal {
			return
		}
		terminalContext, cancel := publicationTerminalContext(ctx)
		defer cancel()
		_ = attempt.Rollback(terminalContext)
	}()

	event := attempt.Event()
	outbox := attempt.Outbox()
	if err := validatePublicationSelection(event, outbox); err != nil {
		return PublicationResult{}, err
	}

	acknowledgementID, publishErr := publication.Publisher.Publish(ctx, event)
	if publishErr != nil {
		category := publicationFailureCategory(publishErr)
		if err := recordPublicationFailure(ctx, attempt, category); err != nil {
			return PublicationResult{}, err
		}
		terminal = true
		return PublicationResult{
			Status: PublicationFailed, EventID: event.ID, LastFailureCategory: category,
		}, nil
	}
	deliveredAt := now().UTC()
	if deliveredAt.IsZero() {
		return PublicationResult{}, errors.New("event publication time is unavailable")
	}
	if _, err := outbox.WithAcknowledgement(acknowledgementID, deliveredAt); err != nil {
		category := domain.OutboxFailureInvalidAcknowledgement
		if err := recordPublicationFailure(ctx, attempt, category); err != nil {
			return PublicationResult{}, err
		}
		terminal = true
		return PublicationResult{
			Status: PublicationFailed, EventID: event.ID, LastFailureCategory: category,
		}, nil
	}

	terminalContext, cancel := publicationTerminalContext(ctx)
	defer cancel()
	if err := attempt.MarkDelivered(terminalContext, acknowledgementID, deliveredAt); err != nil {
		return PublicationResult{}, fmt.Errorf("record event publication: %w", err)
	}
	terminal = true
	return PublicationResult{
		Status: PublicationDelivered, EventID: event.ID, AcknowledgementID: acknowledgementID,
	}, nil
}

func validatePublicationSelection(
	event domain.DomainEvent,
	outbox domain.OutboxRecord,
) error {
	if err := event.Validate(); err != nil {
		return errors.New("publication event is invalid")
	}
	if err := outbox.Validate(); err != nil ||
		outbox.EventID != event.ID ||
		outbox.Status != domain.OutboxStatusPending {
		return errors.New("publication outbox record is invalid")
	}
	return nil
}

func recordPublicationFailure(
	ctx context.Context,
	attempt PublicationAttempt,
	category string,
) error {
	terminalContext, cancel := publicationTerminalContext(ctx)
	defer cancel()
	if err := attempt.RecordFailure(terminalContext, category); err != nil {
		return fmt.Errorf("record event publication failure: %w", err)
	}
	return nil
}

func publicationTerminalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), publicationTerminalTimeout)
}

func publicationFailureCategory(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.OutboxFailureTimeout
	}
	return domain.OutboxFailureTransport
}
