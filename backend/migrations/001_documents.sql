CREATE TABLE document_requests (
    user_id TEXT NOT NULL,
    id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('requested','template_generated','awaiting_confirmation','confirmed','generating','ready')),
    language TEXT NOT NULL CHECK (language IN ('zh-TW','en')),
    topic TEXT NOT NULL,
    audience TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('lesson_plan','worksheet','handout')),
    template_version INTEGER NOT NULL DEFAULT 0,
    confirmed_version INTEGER NOT NULL DEFAULT 0,
    preview_busy INTEGER NOT NULL DEFAULT 0 CHECK (preview_busy IN (0,1)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, id)
);

CREATE TABLE template_versions (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    spec_json TEXT NOT NULL CHECK (json_valid(spec_json)),
    PRIMARY KEY (user_id, artifact_id, version),
    FOREIGN KEY (user_id, artifact_id) REFERENCES document_requests(user_id, id)
);

CREATE TABLE artifacts (
    user_id TEXT NOT NULL,
    id TEXT NOT NULL,
    current_revision INTEGER NOT NULL,
    PRIMARY KEY (user_id, id),
    FOREIGN KEY (user_id, id) REFERENCES document_requests(user_id, id)
);

CREATE TABLE artifact_revisions (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    content_json TEXT NOT NULL CHECK (json_valid(content_json)),
    origin_revision INTEGER NOT NULL,
    undo_revision INTEGER NOT NULL,
    redo_json TEXT NOT NULL CHECK (json_valid(redo_json)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, artifact_id, revision),
    FOREIGN KEY (user_id, artifact_id) REFERENCES artifacts(user_id, id)
);

CREATE TABLE revision_blocks (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    block_id TEXT NOT NULL,
    PRIMARY KEY (user_id, artifact_id, revision, block_id),
    FOREIGN KEY (user_id, artifact_id, revision) REFERENCES artifact_revisions(user_id, artifact_id, revision)
);

CREATE TABLE generation_usage (
    user_id TEXT NOT NULL,
    day TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    PRIMARY KEY (user_id, day)
);
