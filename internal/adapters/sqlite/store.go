package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ferueda/noema/internal/domain"
)

type Store struct {
	database *sql.DB
}

func NewStore(database *sql.DB) *Store {
	return &Store{database: database}
}

type eventWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func insertEvent(ctx context.Context, transaction eventWriter, event domain.Event) error {
	if event.SubjectType == "" || event.SubjectID == "" {
		return errors.New("event subject type and identity are required")
	}
	payload, err := encodeJSON(event.Payload)
	if err != nil {
		return err
	}
	evidence, err := encodeJSON(event.Evidence)
	if err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO events (
			id, fingerprint, type, subject_id, payload_json, evidence_json,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO NOTHING
	`, event.ID, event.Fingerprint, event.Type, event.SubjectID, payload, evidence,
		formatTime(event.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check inserted event: %w", err)
	}
	if inserted == 0 {
		var existingID string
		if err := transaction.QueryRowContext(ctx,
			"SELECT id FROM events WHERE fingerprint = ?", event.Fingerprint,
		).Scan(&existingID); err != nil {
			return fmt.Errorf("read existing event: %w", err)
		}
		if existingID != event.ID {
			return errors.New("event fingerprint identity mismatch")
		}
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO event_subject_types (event_id, subject_type)
		VALUES (?, ?)
		ON CONFLICT(event_id) DO UPDATE SET subject_type = excluded.subject_type
		WHERE event_subject_types.subject_type = excluded.subject_type
	`, event.ID, event.SubjectType); err != nil {
		return fmt.Errorf("insert event subject type: %w", err)
	}
	var subjectType string
	if err := transaction.QueryRowContext(ctx,
		"SELECT subject_type FROM event_subject_types WHERE event_id = ?", event.ID,
	).Scan(&subjectType); err != nil {
		return fmt.Errorf("read event subject type: %w", err)
	}
	if subjectType != event.SubjectType {
		return errors.New("event subject type mismatch")
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func encodeJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode json: %w", err)
	}
	return string(encoded), nil
}

func decodeJSON(encoded string, destination any) error {
	if err := json.Unmarshal([]byte(encoded), destination); err != nil {
		return fmt.Errorf("decode json: %w", err)
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time: %w", err)
	}
	return parsed, nil
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
