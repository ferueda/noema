package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

type publicationAttempt struct {
	connection *sql.Conn
	record     EventRecord
	closed     bool
}

// BeginPublication starts one serialized publication attempt and selects the
// oldest pending event without changing it. The immediate transaction remains
// open across the transport call made by the application service.
func (store *Store) BeginPublication(
	ctx context.Context,
) (application.PublicationAttempt, bool, error) {
	connection, err := store.database.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquire publication connection: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = connection.Close()
		return nil, false, fmt.Errorf("begin immediate publication: %w", err)
	}
	attempt := &publicationAttempt{connection: connection}
	record, err := readEventRecord(connection.QueryRowContext(ctx, `
		SELECT events.id, events.fingerprint, events.schema_version, events.type,
		       events.subject_type, events.subject_id, events.payload_json,
		       events.references_json, events.created_at,
		       event_outbox.event_id, event_outbox.status,
		       event_outbox.attempt_count, event_outbox.last_failure_category,
		       event_outbox.acknowledgement_id, event_outbox.delivered_at
		  FROM event_outbox
		  JOIN events ON events.id = event_outbox.event_id
		 WHERE event_outbox.status = 'pending'
		 ORDER BY events.created_at, events.id
		 LIMIT 1
	`))
	if errors.Is(err, sql.ErrNoRows) {
		if rollbackErr := attempt.Rollback(ctx); rollbackErr != nil {
			return nil, false, rollbackErr
		}
		return nil, false, nil
	}
	if err != nil {
		_ = attempt.Rollback(ctx)
		return nil, false, fmt.Errorf("inspect pending event publication: %w", err)
	}
	attempt.record = record
	return attempt, true, nil
}

func (attempt *publicationAttempt) Event() domain.DomainEvent {
	return attempt.record.Event
}

func (attempt *publicationAttempt) Outbox() domain.OutboxRecord {
	return attempt.record.Outbox
}

func (attempt *publicationAttempt) MarkDelivered(
	ctx context.Context,
	acknowledgementID string,
	deliveredAt time.Time,
) error {
	if err := attempt.requireOpen(); err != nil {
		return err
	}
	next, err := attempt.record.Outbox.WithAcknowledgement(acknowledgementID, deliveredAt)
	if err != nil {
		return fmt.Errorf("validate delivered outbox state: %w", err)
	}
	result, err := attempt.connection.ExecContext(ctx, `
		UPDATE event_outbox
		   SET status = ?,
		       attempt_count = ?,
		       last_failure_category = '',
		       acknowledgement_id = ?,
		       delivered_at = ?
		 WHERE event_id = ?
		   AND status = 'pending'
		   AND attempt_count = ?
	`,
		next.Status,
		next.AttemptCount,
		nullableAcknowledgement(next.AcknowledgementID),
		formatTime(*next.DeliveredAt),
		next.EventID,
		attempt.record.Outbox.AttemptCount,
	)
	if err != nil {
		return fmt.Errorf("mark event publication delivered: %w", err)
	}
	if err := requireOneUpdatedRow(result); err != nil {
		return err
	}
	if err := attempt.commit(ctx); err != nil {
		return err
	}
	attempt.record.Outbox = next
	return nil
}

func (attempt *publicationAttempt) RecordFailure(
	ctx context.Context,
	category string,
) error {
	if err := attempt.requireOpen(); err != nil {
		return err
	}
	next, err := attempt.record.Outbox.WithFailure(category)
	if err != nil {
		return fmt.Errorf("validate failed outbox state: %w", err)
	}
	result, err := attempt.connection.ExecContext(ctx, `
		UPDATE event_outbox
		   SET status = 'pending',
		       attempt_count = ?,
		       last_failure_category = ?,
		       acknowledgement_id = NULL,
		       delivered_at = NULL
		 WHERE event_id = ?
		   AND status = 'pending'
		   AND attempt_count = ?
	`,
		next.AttemptCount,
		next.LastFailureCategory,
		next.EventID,
		attempt.record.Outbox.AttemptCount,
	)
	if err != nil {
		return fmt.Errorf("record event publication failure: %w", err)
	}
	if err := requireOneUpdatedRow(result); err != nil {
		return err
	}
	if err := attempt.commit(ctx); err != nil {
		return err
	}
	attempt.record.Outbox = next
	return nil
}

func (attempt *publicationAttempt) Rollback(ctx context.Context) error {
	if attempt.closed {
		return nil
	}
	attempt.closed = true
	_, rollbackErr := attempt.connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	closeErr := attempt.connection.Close()
	if rollbackErr != nil {
		return fmt.Errorf("rollback publication attempt: %w", rollbackErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close publication connection: %w", closeErr)
	}
	return nil
}

func (attempt *publicationAttempt) requireOpen() error {
	if attempt.closed || attempt.connection == nil {
		return errors.New("publication attempt is closed")
	}
	return nil
}

func (attempt *publicationAttempt) commit(ctx context.Context) error {
	if _, err := attempt.connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit publication attempt: %w", err)
	}
	attempt.closed = true
	_ = attempt.connection.Close()
	return nil
}

func nullableAcknowledgement(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func requireOneUpdatedRow(result sql.Result) error {
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check publication state update: %w", err)
	}
	if updated != 1 {
		return errors.New("pending publication state changed unexpectedly")
	}
	return nil
}
