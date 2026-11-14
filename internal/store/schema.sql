PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    is_local INTEGER NOT NULL DEFAULT 0 CHECK (is_local IN (0, 1)),
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS one_local_user ON users(is_local) WHERE is_local = 1;
CREATE TABLE IF NOT EXISTS records (
    id TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL CHECK(kind IN ('course','lesson','source','knowledge','profile','profile_version','feedback','request','preview','task','step','artifact','provider','attempt','template','event','preference','chapter')),
    parent_id TEXT,
    course_id TEXT,
    lesson_id TEXT,
    version INTEGER NOT NULL CHECK(version > 0),
    data TEXT NOT NULL CHECK(json_valid(data)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(id, owner_user_id),
    FOREIGN KEY(parent_id,owner_user_id) REFERENCES records(id,owner_user_id) ON DELETE CASCADE,
    FOREIGN KEY(course_id,owner_user_id) REFERENCES records(id,owner_user_id) ON DELETE CASCADE,
    FOREIGN KEY(lesson_id,owner_user_id) REFERENCES records(id,owner_user_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS records_scope ON records(owner_user_id,kind,course_id,lesson_id,created_at);
CREATE INDEX IF NOT EXISTS records_parent ON records(parent_id);
CREATE UNIQUE INDEX IF NOT EXISTS step_once ON records(parent_id,json_extract(data,'$.key')) WHERE kind='step';
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT OR IGNORE INTO migrations VALUES(1,strftime('%Y-%m-%dT%H:%M:%fZ','now'));
