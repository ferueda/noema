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

func (store *Store) FindCompletedScan(
	ctx context.Context,
	fingerprint string,
) (domain.Scan, bool, error) {
	row := store.database.QueryRowContext(ctx, `
		SELECT id, fingerprint, knowledge_fingerprint, source_kind, after_time,
		       before_time, content_scope, coverage, status, skipped_count,
		       observation_ids_json, job_id, created_at, finished_at
		  FROM scans
		 WHERE fingerprint = ? AND status = 'completed'
	`, fingerprint)
	scan, err := readScan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Scan{}, false, nil
	}
	if err != nil {
		return domain.Scan{}, false, fmt.Errorf("read completed scan: %w", err)
	}
	return scan, true, nil
}

func (store *Store) FindCompletedKnowledge(
	ctx context.Context,
	fingerprint string,
) ([]domain.Observation, bool, error) {
	var encodedIDs string
	err := store.database.QueryRowContext(ctx, `
		SELECT observation_ids_json
		  FROM scans
		 WHERE knowledge_fingerprint = ? AND status = 'completed'
		 ORDER BY finished_at DESC, id
		 LIMIT 1
	`, fingerprint).Scan(&encodedIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find completed knowledge: %w", err)
	}
	var observationIDs []string
	if err := decodeJSON(encodedIDs, &observationIDs); err != nil {
		return nil, false, fmt.Errorf("decode completed knowledge: %w", err)
	}
	observations, err := store.LoadObservations(ctx, observationIDs)
	if err != nil {
		return nil, false, fmt.Errorf("load completed knowledge: %w", err)
	}
	return observations, true, nil
}

func (store *Store) CommitScan(ctx context.Context, commit domain.ScanCommit) (bool, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin scan transaction: %w", err)
	}
	defer transaction.Rollback()

	observationIDs, err := encodeJSON(commit.Scan.ObservationIDs)
	if err != nil {
		return false, err
	}
	result, err := transaction.ExecContext(ctx, `
		INSERT INTO scans (
			id, fingerprint, knowledge_fingerprint, source_kind, after_time,
			before_time, content_scope, coverage, status, skipped_count,
			observation_ids_json, job_id, created_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO NOTHING
	`,
		commit.Scan.ID,
		commit.Scan.Fingerprint,
		commit.Scan.KnowledgeFingerprint,
		commit.Scan.SourceKind,
		formatTime(commit.Scan.After),
		formatTime(commit.Scan.Before),
		commit.Scan.ContentScope,
		commit.Scan.Coverage,
		commit.Scan.Status,
		commit.Scan.SkippedCount,
		observationIDs,
		commit.Scan.JobID,
		formatTime(commit.Scan.CreatedAt),
		formatTime(commit.Scan.FinishedAt),
	)
	if err != nil {
		return false, fmt.Errorf("insert scan: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check inserted scan: %w", err)
	}
	if inserted == 0 {
		return false, nil
	}

	for _, chunk := range commit.Chunks {
		evidence, encodeErr := encodeJSON(chunk.Evidence)
		if encodeErr != nil {
			return false, encodeErr
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO evidence_chunks (
				id, fingerprint, distillation_key, evidence_json, captured_at,
				processing_skipped
			) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(fingerprint) DO UPDATE SET
				distillation_key = excluded.distillation_key,
				evidence_json = excluded.evidence_json,
				captured_at = excluded.captured_at,
				processing_skipped = excluded.processing_skipped
		`,
			chunk.ID,
			chunk.Fingerprint,
			chunk.DistillationKey,
			evidence,
			formatTime(chunk.CapturedAt),
			boolInteger(chunk.ProcessingSkipped),
		); err != nil {
			return false, fmt.Errorf("insert evidence chunk: %w", err)
		}
	}

	for _, observation := range commit.Observations {
		evidence, encodeErr := encodeJSON(observation.Evidence)
		if encodeErr != nil {
			return false, encodeErr
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO observations (
				id, scan_id, fingerprint, distillation_key, kind, summary,
				confidence, evidence_json, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(fingerprint) DO NOTHING
		`,
			observation.ID,
			commit.Scan.ID,
			observation.Fingerprint,
			observation.DistillationKey,
			observation.Kind,
			observation.Summary,
			observation.Confidence,
			evidence,
			formatTime(observation.CreatedAt),
		); err != nil {
			return false, fmt.Errorf("insert observation: %w", err)
		}
	}

	for _, event := range commit.Events {
		payload, encodeErr := encodeJSON(event.Payload)
		if encodeErr != nil {
			return false, encodeErr
		}
		evidence, encodeErr := encodeJSON(event.Evidence)
		if encodeErr != nil {
			return false, encodeErr
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO events (
				id, fingerprint, type, subject_id, payload_json, evidence_json,
				created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(fingerprint) DO NOTHING
		`,
			event.ID,
			event.Fingerprint,
			event.Type,
			event.SubjectID,
			payload,
			evidence,
			formatTime(event.CreatedAt),
		); err != nil {
			return false, fmt.Errorf("insert event: %w", err)
		}
	}

	if commit.Job != nil {
		payload, encodeErr := encodeJSON(commit.Job.Payload)
		if encodeErr != nil {
			return false, encodeErr
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO jobs (
				id, fingerprint, event_id, agent_name, agent_version, status,
				payload_json, error, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)
			ON CONFLICT(fingerprint) DO NOTHING
		`,
			commit.Job.ID,
			commit.Job.Fingerprint,
			commit.Job.EventID,
			commit.Job.AgentName,
			commit.Job.AgentVersion,
			commit.Job.Status,
			payload,
			formatTime(commit.Job.CreatedAt),
		); err != nil {
			return false, fmt.Errorf("insert job: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, fmt.Errorf("commit scan transaction: %w", err)
	}
	return true, nil
}

func (store *Store) ClaimPendingJob(ctx context.Context) (domain.Job, bool, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer transaction.Rollback()

	row := transaction.QueryRowContext(ctx, `
		SELECT id, fingerprint, event_id, agent_name, agent_version, status,
		       payload_json, error, created_at, started_at, finished_at
		  FROM jobs
		 WHERE status = 'pending'
		 ORDER BY created_at, id
		 LIMIT 1
	`)
	job, err := readJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Job{}, false, nil
	}
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("read pending job: %w", err)
	}
	startedAt := time.Now().UTC()
	result, err := transaction.ExecContext(ctx, `
		UPDATE jobs
		   SET status = 'running', started_at = ?
		 WHERE id = ? AND status = 'pending'
	`, formatTime(startedAt), job.ID)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("claim pending job: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("check claimed job: %w", err)
	}
	if updated != 1 {
		return domain.Job{}, false, fmt.Errorf("pending job %s was claimed concurrently", job.ID)
	}
	if err := transaction.Commit(); err != nil {
		return domain.Job{}, false, fmt.Errorf("commit claimed job: %w", err)
	}
	job.Status = domain.JobRunning
	job.StartedAt = &startedAt
	return job, true, nil
}

func (store *Store) LoadObservations(
	ctx context.Context,
	ids []string,
) ([]domain.Observation, error) {
	observations := make([]domain.Observation, 0, len(ids))
	for _, id := range ids {
		row := store.database.QueryRowContext(ctx, `
			SELECT id, fingerprint, distillation_key, kind, summary, confidence,
			       evidence_json, created_at
			  FROM observations
			 WHERE id = ?
		`, id)
		observation, err := readObservation(row)
		if err != nil {
			return nil, fmt.Errorf("read observation %s: %w", id, err)
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func (store *Store) CompleteJob(ctx context.Context, completion domain.JobCompletion) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin completion transaction: %w", err)
	}
	defer transaction.Rollback()

	if err := insertRun(ctx, transaction, completion.Run); err != nil {
		return err
	}
	for _, idea := range completion.Ideas {
		if err := insertIdea(ctx, transaction, idea); err != nil {
			return err
		}
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE jobs
		   SET status = 'succeeded', finished_at = ?, error = ''
		 WHERE id = ? AND status = 'running'
	`, formatTime(completion.Run.FinishedAt), completion.JobID)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check completed job: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("job %s is not running", completion.JobID)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit completion transaction: %w", err)
	}
	return nil
}

func (store *Store) FailJob(ctx context.Context, run domain.AgentRun) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin failure transaction: %w", err)
	}
	defer transaction.Rollback()

	if err := insertRun(ctx, transaction, run); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `
		UPDATE jobs
		   SET status = 'failed', finished_at = ?, error = ?
		 WHERE id = ? AND status = 'running'
	`, formatTime(run.FinishedAt), run.Error, run.JobID)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check failed job: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("job %s is not running", run.JobID)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit failure transaction: %w", err)
	}
	return nil
}

func (store *Store) ListJobs(ctx context.Context) ([]domain.Job, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, fingerprint, event_id, agent_name, agent_version, status,
		       payload_json, error, created_at, started_at, finished_at
		  FROM jobs
		 ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]domain.Job, 0)
	for rows.Next() {
		job, err := readJob(rows)
		if err != nil {
			return nil, fmt.Errorf("read job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return jobs, nil
}

func (store *Store) ListIdeas(ctx context.Context) ([]domain.ContentIdea, error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT id, fingerprint, run_id, rank, concept, core_lesson,
		       audience_benefit, hook, resonance, confidence, formats_json,
		       evidence_json, created_at
		  FROM content_ideas
		 ORDER BY created_at DESC, rank, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list ideas: %w", err)
	}
	defer rows.Close()

	ideas := make([]domain.ContentIdea, 0)
	for rows.Next() {
		idea, err := readIdea(rows)
		if err != nil {
			return nil, fmt.Errorf("read idea: %w", err)
		}
		ideas = append(ideas, idea)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ideas: %w", err)
	}
	return ideas, nil
}

type rowScanner interface {
	Scan(...any) error
}

func readScan(row rowScanner) (domain.Scan, error) {
	var scan domain.Scan
	var after, before, createdAt, finishedAt string
	var observationIDs string
	err := row.Scan(
		&scan.ID,
		&scan.Fingerprint,
		&scan.KnowledgeFingerprint,
		&scan.SourceKind,
		&after,
		&before,
		&scan.ContentScope,
		&scan.Coverage,
		&scan.Status,
		&scan.SkippedCount,
		&observationIDs,
		&scan.JobID,
		&createdAt,
		&finishedAt,
	)
	if err != nil {
		return domain.Scan{}, err
	}
	if err := decodeJSON(observationIDs, &scan.ObservationIDs); err != nil {
		return domain.Scan{}, err
	}
	scan.After, err = parseTime(after)
	if err != nil {
		return domain.Scan{}, err
	}
	scan.Before, err = parseTime(before)
	if err != nil {
		return domain.Scan{}, err
	}
	scan.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Scan{}, err
	}
	scan.FinishedAt, err = parseTime(finishedAt)
	if err != nil {
		return domain.Scan{}, err
	}
	return scan, nil
}

func readJob(row rowScanner) (domain.Job, error) {
	var job domain.Job
	var payload string
	var createdAt string
	var startedAt, finishedAt sql.NullString
	err := row.Scan(
		&job.ID,
		&job.Fingerprint,
		&job.EventID,
		&job.AgentName,
		&job.AgentVersion,
		&job.Status,
		&payload,
		&job.Error,
		&createdAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		return domain.Job{}, err
	}
	if err := decodeJSON(payload, &job.Payload); err != nil {
		return domain.Job{}, err
	}
	job.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Job{}, err
	}
	job.StartedAt, err = parseNullableTime(startedAt)
	if err != nil {
		return domain.Job{}, err
	}
	job.FinishedAt, err = parseNullableTime(finishedAt)
	if err != nil {
		return domain.Job{}, err
	}
	return job, nil
}

func readObservation(row rowScanner) (domain.Observation, error) {
	var observation domain.Observation
	var evidence string
	var createdAt string
	err := row.Scan(
		&observation.ID,
		&observation.Fingerprint,
		&observation.DistillationKey,
		&observation.Kind,
		&observation.Summary,
		&observation.Confidence,
		&evidence,
		&createdAt,
	)
	if err != nil {
		return domain.Observation{}, err
	}
	if err := decodeJSON(evidence, &observation.Evidence); err != nil {
		return domain.Observation{}, err
	}
	observation.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.Observation{}, err
	}
	return observation, nil
}

func readIdea(row rowScanner) (domain.ContentIdea, error) {
	var idea domain.ContentIdea
	var formats, evidence string
	var createdAt string
	err := row.Scan(
		&idea.ID,
		&idea.Fingerprint,
		&idea.RunID,
		&idea.Rank,
		&idea.Concept,
		&idea.CoreLesson,
		&idea.AudienceBenefit,
		&idea.Hook,
		&idea.Resonance,
		&idea.Confidence,
		&formats,
		&evidence,
		&createdAt,
	)
	if err != nil {
		return domain.ContentIdea{}, err
	}
	var decodedFormats struct {
		ShortPost domain.FormatAngle `json:"shortPost"`
		Thread    domain.FormatAngle `json:"thread"`
		Article   domain.FormatAngle `json:"article"`
	}
	if err := decodeJSON(formats, &decodedFormats); err != nil {
		return domain.ContentIdea{}, err
	}
	idea.ShortPost = decodedFormats.ShortPost
	idea.Thread = decodedFormats.Thread
	idea.Article = decodedFormats.Article
	if err := decodeJSON(evidence, &idea.Evidence); err != nil {
		return domain.ContentIdea{}, err
	}
	idea.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.ContentIdea{}, err
	}
	return idea, nil
}

func insertRun(ctx context.Context, transaction *sql.Tx, run domain.AgentRun) error {
	evidence, err := encodeJSON(run.Evidence)
	if err != nil {
		return err
	}
	output, err := encodeJSON(run.Output)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO agent_runs (
			id, job_id, agent_name, agent_version, status, evidence_json,
			output_json, error, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		run.ID,
		run.JobID,
		run.AgentName,
		run.AgentVersion,
		run.Status,
		evidence,
		output,
		run.Error,
		formatTime(run.StartedAt),
		formatTime(run.FinishedAt),
	); err != nil {
		return fmt.Errorf("insert agent run: %w", err)
	}
	return nil
}

func insertIdea(ctx context.Context, transaction *sql.Tx, idea domain.ContentIdea) error {
	formats, err := encodeJSON(struct {
		ShortPost domain.FormatAngle `json:"shortPost"`
		Thread    domain.FormatAngle `json:"thread"`
		Article   domain.FormatAngle `json:"article"`
	}{
		ShortPost: idea.ShortPost,
		Thread:    idea.Thread,
		Article:   idea.Article,
	})
	if err != nil {
		return err
	}
	evidence, err := encodeJSON(idea.Evidence)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
		INSERT INTO content_ideas (
			id, fingerprint, run_id, rank, concept, core_lesson,
			audience_benefit, hook, resonance, confidence, formats_json,
			evidence_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		idea.ID,
		idea.Fingerprint,
		idea.RunID,
		idea.Rank,
		idea.Concept,
		idea.CoreLesson,
		idea.AudienceBenefit,
		idea.Hook,
		idea.Resonance,
		idea.Confidence,
		formats,
		evidence,
		formatTime(idea.CreatedAt),
	); err != nil {
		return fmt.Errorf("insert content idea: %w", err)
	}
	return nil
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

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}
