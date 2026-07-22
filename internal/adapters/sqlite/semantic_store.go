package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ferueda/noema/internal/application"
	"github.com/ferueda/noema/internal/domain"
)

type semanticAttempt struct {
	connection *sql.Conn
	closed     bool
}

func (store *Store) AnalysisStage(ctx context.Context, id string) (string, error) {
	var stage string
	if err := store.database.QueryRowContext(ctx,
		"SELECT stage FROM analysis_runs WHERE id = ?", id,
	).Scan(&stage); err != nil {
		return "", fmt.Errorf("read analysis stage %s: %w", id, err)
	}
	return stage, nil
}

func (store *Store) LoadSemanticAnalysis(
	ctx context.Context,
	id string,
) (application.SemanticAnalysisRecord, error) {
	record, err := loadSemanticAnalysis(ctx, store.database, id)
	if err != nil {
		return application.SemanticAnalysisRecord{}, fmt.Errorf("read semantic analysis %s: %w", id, err)
	}
	return record, nil
}

func (store *Store) RecordSemanticFailure(
	ctx context.Context,
	record application.SemanticAnalysisRecord,
) error {
	if err := validateFailedSemanticRecord(record); err != nil {
		return err
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin semantic failure transaction: %w", err)
	}
	defer transaction.Rollback()
	if _, err := insertAnalysisRun(ctx, transaction, record.Analysis.Run, false); err != nil {
		return err
	}
	if err := insertSemanticDetails(ctx, transaction, record.Analysis.Run.ID, record.Details); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit semantic failure transaction: %w", err)
	}
	return nil
}

func (store *Store) BeginSemanticAttempt(ctx context.Context) (application.SemanticAnalysisAttempt, error) {
	connection, err := store.database.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire semantic attempt connection: %w", err)
	}
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		connection.Close()
		return nil, fmt.Errorf("begin immediate semantic attempt: %w", err)
	}
	return &semanticAttempt{connection: connection}, nil
}

func (attempt *semanticAttempt) FindCompleted(
	ctx context.Context,
	processingKey string,
) (application.SemanticAnalysisRecord, bool, error) {
	if err := attempt.requireOpen(); err != nil {
		return application.SemanticAnalysisRecord{}, false, err
	}
	var id string
	err := attempt.connection.QueryRowContext(ctx, `
		SELECT id FROM analysis_runs
		 WHERE processing_key = ? AND status = 'completed' AND stage = 'claims'
	`, processingKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return application.SemanticAnalysisRecord{}, false, nil
	}
	if err != nil {
		return application.SemanticAnalysisRecord{}, false, fmt.Errorf("find completed semantic analysis: %w", err)
	}
	record, err := loadSemanticAnalysis(ctx, attempt.connection, id)
	if err != nil {
		return application.SemanticAnalysisRecord{}, false, err
	}
	return record, true, nil
}

func (attempt *semanticAttempt) Commit(
	ctx context.Context,
	record application.SemanticAnalysisRecord,
) error {
	if err := attempt.requireOpen(); err != nil {
		return err
	}
	if err := validateCompletedSemanticRecord(record); err != nil {
		return err
	}
	if _, err := insertAnalysisRun(ctx, attempt.connection, record.Analysis.Run, false); err != nil {
		return err
	}
	if err := insertSemanticDetails(ctx, attempt.connection, record.Analysis.Run.ID, record.Details); err != nil {
		return err
	}
	for ordinal, claim := range record.Analysis.Claims {
		if err := insertClaim(ctx, attempt.connection, claim, ordinal); err != nil {
			return err
		}
	}
	for _, event := range record.Events {
		if err := insertEvent(ctx, attempt.connection, event); err != nil {
			return err
		}
	}
	return attempt.commit(ctx)
}

func (attempt *semanticAttempt) RecordFailure(
	ctx context.Context,
	run domain.AnalysisRun,
	details application.SemanticAnalysisDetails,
) error {
	if err := attempt.requireOpen(); err != nil {
		return err
	}
	record := application.SemanticAnalysisRecord{
		Analysis: domain.SemanticAnalysis{Run: run, Claims: []domain.Claim{}},
		Details:  details,
		Events:   []domain.Event{},
	}
	if err := validateFailedSemanticRecord(record); err != nil {
		return err
	}
	if _, err := insertAnalysisRun(ctx, attempt.connection, run, false); err != nil {
		return err
	}
	if err := insertSemanticDetails(ctx, attempt.connection, run.ID, details); err != nil {
		return err
	}
	return attempt.commit(ctx)
}

func (attempt *semanticAttempt) Rollback(ctx context.Context) error {
	if attempt.closed {
		return nil
	}
	attempt.closed = true
	_, rollbackErr := attempt.connection.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
	closeErr := attempt.connection.Close()
	if rollbackErr != nil {
		return fmt.Errorf("rollback semantic attempt: %w", rollbackErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close semantic attempt connection: %w", closeErr)
	}
	return nil
}

func (attempt *semanticAttempt) requireOpen() error {
	if attempt.closed || attempt.connection == nil {
		return errors.New("semantic attempt is closed")
	}
	return nil
}

func (attempt *semanticAttempt) commit(ctx context.Context) error {
	if _, err := attempt.connection.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit semantic attempt: %w", err)
	}
	attempt.closed = true
	_ = attempt.connection.Close()
	return nil
}
