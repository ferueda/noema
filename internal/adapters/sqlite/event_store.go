package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ferueda/noema/internal/domain"
)

// EventRecord is the inspectable join between an immutable domain event and
// its mutable publication state.
type EventRecord struct {
	Event  domain.DomainEvent  `json:"event"`
	Outbox domain.OutboxRecord `json:"outbox"`
}

type eventWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func insertEventAndPendingOutbox(
	ctx context.Context,
	writer eventWriter,
	event domain.DomainEvent,
) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate domain event: %w", err)
	}
	if err := validateEventRecordsExist(ctx, writer, event); err != nil {
		return err
	}
	payload, err := encodeJSON(event.Payload)
	if err != nil {
		return fmt.Errorf("encode domain event payload: %w", err)
	}
	references, err := encodeJSON(event.References)
	if err != nil {
		return fmt.Errorf("encode domain event references: %w", err)
	}
	if _, err := writer.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, schema_version, type, subject_type, subject_id,
			payload_json, references_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		event.ID,
		event.Fingerprint,
		event.SchemaVersion,
		event.Type,
		event.SubjectType,
		event.SubjectID,
		payload,
		references,
		formatTime(event.CreatedAt),
	); err != nil {
		return fmt.Errorf("insert domain event: %w", err)
	}
	outbox, err := domain.NewOutboxRecord(event.ID)
	if err != nil {
		return fmt.Errorf("build event outbox record: %w", err)
	}
	if _, err := writer.ExecContext(ctx, `
		INSERT INTO event_outbox (
			event_id, status, attempt_count, last_failure_category,
			acknowledgement_id, delivered_at
		) VALUES (?, ?, ?, ?, NULL, NULL)
	`,
		outbox.EventID,
		outbox.Status,
		outbox.AttemptCount,
		outbox.LastFailureCategory,
	); err != nil {
		return fmt.Errorf("insert event outbox record: %w", err)
	}
	return nil
}

func validateEventRecordsExist(
	ctx context.Context,
	reader eventWriter,
	event domain.DomainEvent,
) error {
	if err := validateEventRecordExists(ctx, reader, event.SubjectType, event.SubjectID); err != nil {
		return fmt.Errorf("validate domain event subject: %w", err)
	}
	for _, reference := range event.References {
		if err := validateEventRecordExists(ctx, reader, reference.RecordType, reference.RecordID); err != nil {
			return fmt.Errorf("validate domain event reference: %w", err)
		}
	}
	return nil
}

func validateEventRecordExists(
	ctx context.Context,
	reader eventWriter,
	recordType string,
	recordID string,
) error {
	var query string
	switch recordType {
	case domain.EventReferenceAnalysis:
		query = `
			SELECT COUNT(*) FROM analysis_runs
			 WHERE id = ? AND status = 'completed'
		`
	case domain.EventReferenceClaim:
		query = "SELECT COUNT(*) FROM claims WHERE id = ?"
	case domain.EventReferenceFact:
		query = "SELECT COUNT(*) FROM facts WHERE id = ?"
	case domain.EventReferenceSummary, domain.EventReferenceEpisode:
		return errors.New("referenced record type is not stored in V1")
	default:
		return errors.New("referenced record type is unsupported")
	}
	var count int
	if err := reader.QueryRowContext(ctx, query, recordID).Scan(&count); err != nil {
		return fmt.Errorf("query referenced record: %w", err)
	}
	if count != 1 {
		return errors.New("referenced record is unavailable")
	}
	return nil
}

// ListEvents returns events with their publication state in canonical event
// order. Empty status includes both pending and delivered records.
func (store *Store) ListEvents(
	ctx context.Context,
	status string,
) ([]EventRecord, error) {
	if status != "" && status != domain.OutboxStatusPending && status != domain.OutboxStatusDelivered {
		return nil, errors.New("event publication status is invalid")
	}
	query := `
		SELECT events.id, events.fingerprint, events.schema_version, events.type,
		       events.subject_type, events.subject_id, events.payload_json,
		       events.references_json, events.created_at,
		       event_outbox.event_id, event_outbox.status,
		       event_outbox.attempt_count, event_outbox.last_failure_category,
		       event_outbox.acknowledgement_id, event_outbox.delivered_at
		  FROM events
		  JOIN event_outbox ON event_outbox.event_id = events.id
	`
	arguments := []any{}
	if status != "" {
		query += " WHERE event_outbox.status = ?"
		arguments = append(arguments, status)
	}
	query += " ORDER BY events.created_at, events.id"
	rows, err := store.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list domain events: %w", err)
	}
	defer rows.Close()

	records := make([]EventRecord, 0)
	for rows.Next() {
		record, err := readEventRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("read domain event: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domain events: %w", err)
	}
	return records, nil
}

// LoadEvent returns one event and its publication state.
func (store *Store) LoadEvent(
	ctx context.Context,
	eventID string,
) (EventRecord, bool, error) {
	if strings.TrimSpace(eventID) == "" {
		return EventRecord{}, false, errors.New("domain event ID is required")
	}
	record, err := readEventRecord(store.database.QueryRowContext(ctx, `
		SELECT events.id, events.fingerprint, events.schema_version, events.type,
		       events.subject_type, events.subject_id, events.payload_json,
		       events.references_json, events.created_at,
		       event_outbox.event_id, event_outbox.status,
		       event_outbox.attempt_count, event_outbox.last_failure_category,
		       event_outbox.acknowledgement_id, event_outbox.delivered_at
		  FROM events
		  JOIN event_outbox ON event_outbox.event_id = events.id
		 WHERE events.id = ?
	`, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return EventRecord{}, false, nil
	}
	if err != nil {
		return EventRecord{}, false, fmt.Errorf("load domain event: %w", err)
	}
	return record, true, nil
}

func readEventRecord(row rowScanner) (EventRecord, error) {
	var record EventRecord
	var payload, references, createdAt string
	var outboxEventID string
	var acknowledgementID, deliveredAt sql.NullString
	if err := row.Scan(
		&record.Event.ID,
		&record.Event.Fingerprint,
		&record.Event.SchemaVersion,
		&record.Event.Type,
		&record.Event.SubjectType,
		&record.Event.SubjectID,
		&payload,
		&references,
		&createdAt,
		&outboxEventID,
		&record.Outbox.Status,
		&record.Outbox.AttemptCount,
		&record.Outbox.LastFailureCategory,
		&acknowledgementID,
		&deliveredAt,
	); err != nil {
		return EventRecord{}, err
	}
	if err := decodeJSON(payload, &record.Event.Payload); err != nil {
		return EventRecord{}, fmt.Errorf("decode domain event payload: %w", err)
	}
	if err := decodeStrictJSON(references, &record.Event.References); err != nil {
		return EventRecord{}, fmt.Errorf("decode domain event references: %w", err)
	}
	var err error
	record.Event.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return EventRecord{}, fmt.Errorf("decode domain event creation time: %w", err)
	}
	record.Outbox.EventID = outboxEventID
	if acknowledgementID.Valid {
		record.Outbox.AcknowledgementID = acknowledgementID.String
	}
	if deliveredAt.Valid {
		value, err := parseTime(deliveredAt.String)
		if err != nil {
			return EventRecord{}, fmt.Errorf("decode outbox delivery time: %w", err)
		}
		record.Outbox.DeliveredAt = &value
	}
	if record.Outbox.EventID != record.Event.ID {
		return EventRecord{}, errors.New("event and outbox identity do not match")
	}
	if err := record.Event.Validate(); err != nil {
		return EventRecord{}, fmt.Errorf("validate stored domain event: %w", err)
	}
	if err := record.Outbox.Validate(); err != nil {
		return EventRecord{}, fmt.Errorf("validate stored outbox record: %w", err)
	}
	return record, nil
}

func decodeStrictJSON(encoded string, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("stored JSON contains trailing data")
	}
	return nil
}
