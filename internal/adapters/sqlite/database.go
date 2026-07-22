package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(ctx context.Context, path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)
	if err := configure(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := migrate(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := validateMigratedEvents(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	return database, nil
}

func configure(ctx context.Context, database *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func migrate(ctx context.Context, database *sql.DB) error {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		statement, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := database.ExecContext(ctx, string(statement)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func validateMigratedEvents(ctx context.Context, database *sql.DB) error {
	var eventID, eventType string
	err := database.QueryRowContext(ctx, `
		SELECT events.id, events.type
		  FROM events
		  LEFT JOIN event_subject_types ON event_subject_types.event_id = events.id
		 WHERE event_subject_types.event_id IS NULL
		 ORDER BY events.id
		 LIMIT 1
	`).Scan(&eventID, &eventType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate event subject types: %w", err)
	}
	return fmt.Errorf("event %s has unsupported type %s", eventID, eventType)
}
