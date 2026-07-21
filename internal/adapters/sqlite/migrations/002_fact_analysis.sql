CREATE TABLE IF NOT EXISTS analysis_runs (
    id TEXT PRIMARY KEY,
    processing_key TEXT UNIQUE,
    stage TEXT NOT NULL,
    requested_source_identity TEXT NOT NULL,
    revision_json TEXT,
    selection_json TEXT,
    extractor_name TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    fact_ids_json TEXT NOT NULL,
    omissions_json TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('completed', 'failed')),
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    CHECK (status != 'completed' OR processing_key IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS analysis_runs_requested_source
    ON analysis_runs(requested_source_identity, finished_at, id);

CREATE TABLE IF NOT EXISTS facts (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    analysis_run_id TEXT NOT NULL REFERENCES analysis_runs(id),
    ordinal INTEGER NOT NULL,
    kind TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    value_json TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('success', 'failure', 'unknown', 'not-applicable')),
    extractor_name TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    parse_rule TEXT NOT NULL,
    evidence_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (analysis_run_id, ordinal)
);
