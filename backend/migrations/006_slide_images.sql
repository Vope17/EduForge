CREATE TABLE slide_images (
    user_id TEXT NOT NULL,
    artifact_id TEXT NOT NULL,
    id TEXT NOT NULL,
    filename TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, artifact_id, id),
    FOREIGN KEY (user_id, artifact_id) REFERENCES artifacts(user_id, id)
);
