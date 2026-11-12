CREATE TABLE patch_proposals (
    user_id TEXT NOT NULL,
    id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    base_revision INTEGER NOT NULL,
    anchors_json TEXT NOT NULL CHECK (json_valid(anchors_json)),
    edits_json TEXT NOT NULL CHECK (json_valid(edits_json)),
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','accepted','rejected')),
    accepted_revision INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, id),
    FOREIGN KEY (user_id, artifact_id, base_revision) REFERENCES artifact_revisions(user_id, artifact_id, revision)
);
