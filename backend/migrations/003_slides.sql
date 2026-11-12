-- Preserve existing document requests, templates and immutable revisions.
CREATE TABLE document_requests_next (
    user_id TEXT NOT NULL,
    id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('requested','template_generated','awaiting_confirmation','confirmed','generating','ready')),
    language TEXT NOT NULL CHECK (language IN ('zh-TW','en')),
    artifact_type TEXT NOT NULL DEFAULT 'document' CHECK (artifact_type IN ('document','slides')),
    slide_count INTEGER NOT NULL DEFAULT 0 CHECK (slide_count >= 0 AND slide_count <= 12),
    topic TEXT NOT NULL,
    audience TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('lesson_plan','worksheet','handout','presentation')),
    template_version INTEGER NOT NULL DEFAULT 0,
    confirmed_version INTEGER NOT NULL DEFAULT 0,
    preview_busy INTEGER NOT NULL DEFAULT 0 CHECK (preview_busy IN (0,1)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, id)
);

INSERT INTO document_requests_next
    (user_id, id, state, language, topic, audience, kind, template_version, confirmed_version, preview_busy, created_at)
SELECT user_id, id, state, language, topic, audience, kind, template_version, confirmed_version, preview_busy, created_at
FROM document_requests;

CREATE TABLE template_versions_next (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    spec_json TEXT NOT NULL CHECK (json_valid(spec_json)),
    PRIMARY KEY (user_id, artifact_id, version),
    FOREIGN KEY (user_id, artifact_id) REFERENCES document_requests_next(user_id, id)
);

INSERT INTO template_versions_next SELECT * FROM template_versions;

CREATE TABLE artifacts_next (
    user_id TEXT NOT NULL,
    id TEXT NOT NULL,
    current_revision INTEGER NOT NULL,
    PRIMARY KEY (user_id, id),
    FOREIGN KEY (user_id, id) REFERENCES document_requests_next(user_id, id)
);

INSERT INTO artifacts_next SELECT * FROM artifacts;

CREATE TABLE artifact_revisions_next (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    content_json TEXT NOT NULL CHECK (json_valid(content_json)),
    origin_revision INTEGER NOT NULL,
    undo_revision INTEGER NOT NULL,
    redo_json TEXT NOT NULL CHECK (json_valid(redo_json)),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, artifact_id, revision),
    FOREIGN KEY (user_id, artifact_id) REFERENCES artifacts_next(user_id, id)
);

INSERT INTO artifact_revisions_next SELECT * FROM artifact_revisions;

CREATE TABLE revision_blocks_next (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    block_id TEXT NOT NULL,
    PRIMARY KEY (user_id, artifact_id, revision, block_id),
    FOREIGN KEY (user_id, artifact_id, revision) REFERENCES artifact_revisions_next(user_id, artifact_id, revision)
);

INSERT INTO revision_blocks_next SELECT * FROM revision_blocks;

DROP TABLE revision_blocks;

DROP TABLE artifact_revisions;

DROP TABLE artifacts;

DROP TABLE template_versions;

DROP TABLE document_requests;

ALTER TABLE document_requests_next RENAME TO document_requests;

ALTER TABLE template_versions_next RENAME TO template_versions;

ALTER TABLE artifacts_next RENAME TO artifacts;

ALTER TABLE artifact_revisions_next RENAME TO artifact_revisions;

ALTER TABLE revision_blocks_next RENAME TO revision_blocks;

CREATE INDEX request_artifact_type ON document_requests(user_id, artifact_type, created_at);
