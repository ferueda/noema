CREATE TABLE IF NOT EXISTS scans (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    knowledge_fingerprint TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    after_time TEXT NOT NULL,
    before_time TEXT NOT NULL,
    content_scope TEXT NOT NULL,
    coverage TEXT NOT NULL,
    status TEXT NOT NULL,
    skipped_count INTEGER NOT NULL,
    observation_ids_json TEXT NOT NULL,
    job_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    finished_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS scans_completed_knowledge
    ON scans(knowledge_fingerprint, status, finished_at);

CREATE TABLE IF NOT EXISTS evidence_chunks (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    distillation_key TEXT NOT NULL,
    evidence_json TEXT NOT NULL,
    captured_at TEXT NOT NULL,
    processing_skipped INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS observations (
    id TEXT PRIMARY KEY,
    scan_id TEXT NOT NULL REFERENCES scans(id),
    fingerprint TEXT NOT NULL UNIQUE,
    distillation_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    summary TEXT NOT NULL,
    confidence REAL NOT NULL,
    evidence_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    evidence_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    event_id TEXT NOT NULL REFERENCES events(id),
    agent_name TEXT NOT NULL,
    agent_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    payload_json TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS jobs_pending_created
    ON jobs(status, created_at, id);

CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL UNIQUE REFERENCES jobs(id),
    agent_name TEXT NOT NULL,
    agent_version TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed')),
    evidence_json TEXT NOT NULL,
    output_json TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS content_ideas (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    run_id TEXT NOT NULL REFERENCES agent_runs(id),
    rank INTEGER NOT NULL,
    concept TEXT NOT NULL,
    core_lesson TEXT NOT NULL,
    audience_benefit TEXT NOT NULL,
    hook TEXT NOT NULL,
    resonance TEXT NOT NULL,
    confidence REAL NOT NULL,
    formats_json TEXT NOT NULL,
    evidence_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
