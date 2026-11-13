-- ChatGPT now signs in through Codex CLI, which keeps its own credentials.
-- The OAuth client registration is no longer needed; only the chosen model is kept.
DROP TABLE model_registrations;
CREATE TABLE model_settings (
    user_id TEXT PRIMARY KEY,
    chatgpt_model TEXT NOT NULL DEFAULT ''
);
