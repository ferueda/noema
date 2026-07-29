package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Store struct {
	database *sql.DB
}

func NewStore(database *sql.DB) *Store {
	return &Store{database: database}
}

type rowScanner interface {
	Scan(...any) error
}

func encodeJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func decodeJSON(encoded string, destination any) error {
	if !json.Valid([]byte(encoded)) {
		return errors.New("stored JSON is invalid")
	}
	return json.Unmarshal([]byte(encoded), destination)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
