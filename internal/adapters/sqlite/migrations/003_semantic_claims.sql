CREATE TABLE IF NOT EXISTS semantic_analysis_details (
    analysis_run_id TEXT PRIMARY KEY REFERENCES analysis_runs(id),
    schema_name TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    schema_disposition TEXT NOT NULL,
    schema_digest TEXT NOT NULL,
    route_config_json TEXT NOT NULL,
    route_config_digest TEXT NOT NULL,
    input_fact_ids_json TEXT,
    claim_ids_json TEXT,
    input_digest TEXT,
    semantic_selection_json TEXT,
    privacy_report_json TEXT,
    model_json TEXT,
    attempted_processing_key TEXT
);

CREATE TABLE IF NOT EXISTS claims (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    analysis_run_id TEXT NOT NULL REFERENCES analysis_runs(id),
    ordinal INTEGER NOT NULL,
    type TEXT NOT NULL,
    statement TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('observed', 'inferred', 'uncertain')),
    confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    supporting_evidence_json TEXT NOT NULL,
    contradicting_evidence_json TEXT NOT NULL,
    supporting_fact_ids_json TEXT NOT NULL,
    outcome TEXT NOT NULL,
    actor TEXT NOT NULL,
    origin TEXT NOT NULL,
    subject TEXT NOT NULL,
    scope TEXT NOT NULL,
    attribution TEXT NOT NULL,
    extractor_name TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    prompt_version TEXT NOT NULL,
    requested_route_json TEXT NOT NULL,
    resolved_provider TEXT NOT NULL,
    resolved_model TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (analysis_run_id, ordinal)
);

CREATE TABLE IF NOT EXISTS event_subject_types (
    event_id TEXT PRIMARY KEY REFERENCES events(id),
    subject_type TEXT NOT NULL CHECK (length(subject_type) > 0)
);

INSERT OR IGNORE INTO event_subject_types (event_id, subject_type)
SELECT id,
       CASE type
           WHEN 'observation.created' THEN 'observation'
           WHEN 'scan.completed' THEN 'scan'
       END
  FROM events
 WHERE type IN ('observation.created', 'scan.completed');
