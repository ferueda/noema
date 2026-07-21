package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/ferueda/noema/internal/domain"
)

func (store *Store) FindCompletedFactAnalysis(
	ctx context.Context,
	processingKey string,
) (domain.FactAnalysis, bool, error) {
	var id string
	err := store.database.QueryRowContext(ctx, `
		SELECT id FROM analysis_runs
		 WHERE processing_key = ? AND status = 'completed'
	`, processingKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.FactAnalysis{}, false, nil
	}
	if err != nil {
		return domain.FactAnalysis{}, false, fmt.Errorf("find completed fact analysis: %w", err)
	}
	analysis, err := store.LoadFactAnalysis(ctx, id)
	if err != nil {
		return domain.FactAnalysis{}, false, err
	}
	return analysis, true, nil
}

func (store *Store) CommitFactAnalysis(ctx context.Context, analysis domain.FactAnalysis) (bool, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin fact analysis transaction: %w", err)
	}
	defer transaction.Rollback()
	inserted, err := insertAnalysisRun(ctx, transaction, analysis.Run, true)
	if err != nil || !inserted {
		return inserted, err
	}
	for ordinal, fact := range analysis.Facts {
		if err := insertFact(ctx, transaction, fact, ordinal); err != nil {
			return false, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit fact analysis transaction: %w", err)
	}
	return true, nil
}

func (store *Store) RecordFailedAnalysis(ctx context.Context, run domain.AnalysisRun) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failed analysis transaction: %w", err)
	}
	defer transaction.Rollback()
	if _, err := insertAnalysisRun(ctx, transaction, run, false); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit failed analysis transaction: %w", err)
	}
	return nil
}

func (store *Store) LoadFactAnalysis(ctx context.Context, id string) (domain.FactAnalysis, error) {
	run, err := readAnalysisRun(store.database.QueryRowContext(ctx, `
		SELECT id, processing_key, stage, requested_source_identity, revision_json,
		       selection_json, extractor_name, extractor_version, schema_version,
		       fact_ids_json, omissions_json, status, error, started_at, finished_at
		  FROM analysis_runs WHERE id = ?
	`, id))
	if err != nil {
		return domain.FactAnalysis{}, fmt.Errorf("read fact analysis %s: %w", id, err)
	}
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, fingerprint, analysis_run_id, kind, schema_version, value_json,
		       outcome, extractor_name, extractor_version, parse_rule, evidence_json,
		       created_at
		  FROM facts WHERE analysis_run_id = ? ORDER BY ordinal
	`, id)
	if err != nil {
		return domain.FactAnalysis{}, fmt.Errorf("query facts: %w", err)
	}
	defer rows.Close()
	facts := make([]domain.Fact, 0, len(run.FactIDs))
	for rows.Next() {
		fact, err := readFact(rows)
		if err != nil {
			return domain.FactAnalysis{}, fmt.Errorf("read fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return domain.FactAnalysis{}, fmt.Errorf("iterate facts: %w", err)
	}
	if len(facts) != len(run.FactIDs) {
		return domain.FactAnalysis{}, errors.New("stored fact count does not match analysis")
	}
	for index := range facts {
		if facts[index].ID != run.FactIDs[index] {
			return domain.FactAnalysis{}, errors.New("stored fact order does not match analysis")
		}
	}
	return domain.FactAnalysis{Run: run, Facts: facts}, nil
}

func insertAnalysisRun(
	ctx context.Context,
	transaction *sql.Tx,
	run domain.AnalysisRun,
	ignoreProcessingConflict bool,
) (bool, error) {
	revision, err := encodeOptionalJSON(run.Revision)
	if err != nil {
		return false, err
	}
	selection, err := encodeOptionalJSON(run.Selection)
	if err != nil {
		return false, err
	}
	factIDs, err := encodeJSON(run.FactIDs)
	if err != nil {
		return false, err
	}
	omissions, err := encodeJSON(run.Omissions)
	if err != nil {
		return false, err
	}
	var processingKey any
	if run.ProcessingKey != "" {
		processingKey = run.ProcessingKey
	}
	query := `
		INSERT INTO analysis_runs (
			id, processing_key, stage, requested_source_identity, revision_json,
			selection_json, extractor_name, extractor_version, schema_version,
			fact_ids_json, omissions_json, status, error, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	if ignoreProcessingConflict {
		query += " ON CONFLICT(processing_key) DO NOTHING"
	}
	result, err := transaction.ExecContext(ctx, query,
		run.ID, processingKey, run.Stage, run.RequestedSourceIdentity, revision, selection,
		run.ExtractorName, run.ExtractorVersion, run.SchemaVersion, factIDs, omissions,
		run.Status, run.Error, formatTime(run.StartedAt), formatTime(run.FinishedAt),
	)
	if err != nil {
		return false, fmt.Errorf("insert analysis run: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check inserted analysis run: %w", err)
	}
	return inserted == 1, nil
}

func insertFact(ctx context.Context, transaction *sql.Tx, fact domain.Fact, ordinal int) error {
	value, err := encodeJSON(fact.Value)
	if err != nil {
		return err
	}
	evidence, err := encodeJSON(fact.Evidence)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO facts (
			id, fingerprint, analysis_run_id, ordinal, kind, schema_version,
			value_json, outcome, extractor_name, extractor_version, parse_rule,
			evidence_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, fact.ID, fact.Fingerprint, fact.AnalysisRunID, ordinal, fact.Kind, fact.SchemaVersion,
		value, fact.Outcome, fact.ExtractorName, fact.ExtractorVersion, fact.ParseRule,
		evidence, formatTime(fact.CreatedAt)); err != nil {
		return fmt.Errorf("insert fact: %w", err)
	}
	return nil
}

func readAnalysisRun(row rowScanner) (domain.AnalysisRun, error) {
	var run domain.AnalysisRun
	var processingKey, revision, selection sql.NullString
	var factIDs, omissions, startedAt, finishedAt string
	if err := row.Scan(
		&run.ID, &processingKey, &run.Stage, &run.RequestedSourceIdentity, &revision,
		&selection, &run.ExtractorName, &run.ExtractorVersion, &run.SchemaVersion,
		&factIDs, &omissions, &run.Status, &run.Error, &startedAt, &finishedAt,
	); err != nil {
		return domain.AnalysisRun{}, err
	}
	if processingKey.Valid {
		run.ProcessingKey = processingKey.String
	}
	if revision.Valid {
		run.Revision = new(domain.EvidenceRevision)
		if err := decodeJSON(revision.String, run.Revision); err != nil {
			return domain.AnalysisRun{}, err
		}
	}
	if selection.Valid {
		run.Selection = new(domain.EvidenceSelection)
		if err := decodeJSON(selection.String, run.Selection); err != nil {
			return domain.AnalysisRun{}, err
		}
	}
	if err := decodeJSON(factIDs, &run.FactIDs); err != nil {
		return domain.AnalysisRun{}, err
	}
	if err := decodeJSON(omissions, &run.Omissions); err != nil {
		return domain.AnalysisRun{}, err
	}
	var err error
	run.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	run.FinishedAt, err = parseTime(finishedAt)
	if err != nil {
		return domain.AnalysisRun{}, err
	}
	return run, nil
}

func readFact(row rowScanner) (domain.Fact, error) {
	var fact domain.Fact
	var value, evidence, createdAt string
	if err := row.Scan(
		&fact.ID, &fact.Fingerprint, &fact.AnalysisRunID, &fact.Kind, &fact.SchemaVersion,
		&value, &fact.Outcome, &fact.ExtractorName, &fact.ExtractorVersion,
		&fact.ParseRule, &evidence, &createdAt,
	); err != nil {
		return domain.Fact{}, err
	}
	if err := decodeJSON(value, &fact.Value); err != nil {
		return domain.Fact{}, err
	}
	if err := decodeJSON(evidence, &fact.Evidence); err != nil {
		return domain.Fact{}, err
	}
	var err error
	fact.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Fact{}, err
	}
	return fact, nil
}

func encodeOptionalJSON(value any) (any, error) {
	if value == nil || (reflect.ValueOf(value).Kind() == reflect.Pointer && reflect.ValueOf(value).IsNil()) {
		return nil, nil
	}
	encoded, err := encodeJSON(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
