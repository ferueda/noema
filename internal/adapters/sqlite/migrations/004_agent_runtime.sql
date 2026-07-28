CREATE TABLE IF NOT EXISTS agent_job_details (
    job_id TEXT PRIMARY KEY REFERENCES jobs(id),
    payload_schema_version INTEGER NOT NULL CHECK (payload_schema_version >= 1),
    configuration_digest TEXT NOT NULL CHECK (length(configuration_digest) > 0)
);

CREATE TABLE IF NOT EXISTS artifacts (
    id TEXT PRIMARY KEY CHECK (length(id) > 0),
    fingerprint TEXT NOT NULL UNIQUE CHECK (length(fingerprint) > 0),
    kind TEXT NOT NULL CHECK (length(kind) > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    payload_json TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES agent_runs(id),
    event_id TEXT NOT NULL REFERENCES events(id),
    job_fingerprint TEXT NOT NULL CHECK (length(job_fingerprint) > 0),
    inputs_json TEXT NOT NULL,
    claim_ids_json TEXT NOT NULL,
    fact_ids_json TEXT NOT NULL,
    supporting_evidence_json TEXT NOT NULL,
    contradicting_evidence_json TEXT NOT NULL,
    proposal_status TEXT NOT NULL CHECK (length(proposal_status) > 0),
    safety_status TEXT NOT NULL CHECK (length(safety_status) > 0),
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS artifacts_run
    ON artifacts(run_id, id);
