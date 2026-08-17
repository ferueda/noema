CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL UNIQUE,
    schema_version INTEGER NOT NULL CHECK (schema_version >= 1),
    type TEXT NOT NULL CHECK (length(type) > 0),
    subject_type TEXT NOT NULL CHECK (length(subject_type) > 0),
    subject_id TEXT NOT NULL CHECK (length(subject_id) > 0),
    payload_json TEXT NOT NULL,
    references_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS event_outbox (
    event_id TEXT PRIMARY KEY REFERENCES events(id),
    status TEXT NOT NULL CHECK (status IN ('pending', 'delivered')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_failure_category TEXT NOT NULL DEFAULT '',
    acknowledgement_id TEXT,
    delivered_at TEXT,
    CHECK (
        (
            status = 'pending'
            AND acknowledgement_id IS NULL
            AND delivered_at IS NULL
            AND (
                (attempt_count = 0 AND last_failure_category = '')
                OR
                (attempt_count > 0 AND length(last_failure_category) > 0)
            )
        )
        OR
        (
            status = 'delivered'
            AND attempt_count > 0
            AND last_failure_category = ''
            AND delivered_at IS NOT NULL
        )
    )
);

CREATE INDEX IF NOT EXISTS event_outbox_pending
    ON event_outbox(status, event_id);
