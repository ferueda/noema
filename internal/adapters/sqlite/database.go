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

var errIncompatibleV1Schema = errors.New(
	"database schema is incompatible with V1; create a new database",
)

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
	if err := rejectPreV1Schema(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := migrate(ctx, database); err != nil {
		database.Close()
		return nil, err
	}
	if err := validateCurrentSchema(ctx, database); err != nil {
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

func rejectPreV1Schema(ctx context.Context, database *sql.DB) error {
	for _, retiredTable := range []string{"scans", "evidence_chunks", "observations", "content_ideas"} {
		found, err := schemaTableExists(ctx, database, retiredTable)
		if err != nil {
			return err
		}
		if found {
			return errIncompatibleV1Schema
		}
	}
	for _, requirement := range currentSchemaRequirements() {
		found, err := schemaTableExists(ctx, database, requirement.table)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		count, err := schemaColumnCount(ctx, database, requirement.table, requirement.column)
		if err != nil {
			return err
		}
		if count != 1 {
			return errIncompatibleV1Schema
		}
	}
	return nil
}

func validateCurrentSchema(ctx context.Context, database *sql.DB) error {
	for _, requirement := range currentSchemaRequirements() {
		count, err := schemaColumnCount(ctx, database, requirement.table, requirement.column)
		if err != nil {
			return err
		}
		if count != 1 {
			return errIncompatibleV1Schema
		}
	}
	for _, retiredTable := range []string{"scans", "evidence_chunks", "observations", "content_ideas"} {
		found, err := schemaTableExists(ctx, database, retiredTable)
		if err != nil {
			return err
		}
		if found {
			return errIncompatibleV1Schema
		}
	}
	return nil
}

func currentSchemaRequirements() []struct {
	table  string
	column string
} {
	return []struct {
		table  string
		column string
	}{
		{table: "events", column: "subject_type"},
		{table: "jobs", column: "payload_schema_version"},
		{table: "jobs", column: "configuration_digest"},
	}
}

func schemaTableExists(ctx context.Context, database *sql.DB, table string) (bool, error) {
	var count int
	if err := database.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		table,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("validate V1 database schema: %w", err)
	}
	return count == 1, nil
}

func schemaColumnCount(
	ctx context.Context,
	database *sql.DB,
	table, column string,
) (int, error) {
	var count int
	query := fmt.Sprintf(
		"SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?",
		table,
	)
	if err := database.QueryRowContext(ctx, query, column).Scan(&count); err != nil {
		return 0, fmt.Errorf("validate V1 database schema: %w", err)
	}
	return count, nil
}
