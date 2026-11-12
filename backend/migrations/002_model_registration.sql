CREATE TABLE model_registrations (
    user_id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL,
    client_id TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT ''
);
