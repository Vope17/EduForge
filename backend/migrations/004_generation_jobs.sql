CREATE TABLE generation_jobs (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    template_version INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('queued','running','completed','failed','cancelled')),
    progress INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, id),
    FOREIGN KEY (user_id, artifact_id) REFERENCES document_requests(user_id, id)
);
CREATE UNIQUE INDEX one_active_generation
ON generation_jobs(user_id, artifact_id) WHERE state IN ('queued','running');
CREATE INDEX artifact_generation_history ON generation_jobs(user_id, artifact_id, sequence DESC);
