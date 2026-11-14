// Package store owns SQLite, file storage, ownership and optimistic concurrency.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
	"eduforge/internal/domain"
)

//go:embed schema.sql
var Schema string

type Store struct {
	DB        *sql.DB
	Root      string
	mu        sync.Mutex
	LocalUser string
}
type Filter struct {
	Kind    string
	Project string
	Paper   string
	Parent  string
}

func Open(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(abs, "eduforge.sqlite3")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(Schema); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateRecordKinds(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateData(db); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db, Root: abs}
	err = db.QueryRow("SELECT id FROM users WHERE is_local=1").Scan(&s.LocalUser)
	if errors.Is(err, sql.ErrNoRows) {
		s.LocalUser = domain.ID()
		_, err = db.Exec("INSERT INTO users VALUES(?,?,1,?)", s.LocalUser, "Local researcher", domain.Now())
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func scan(row interface{ Scan(...any) error }) (domain.Record, error) {
	var r domain.Record
	var parent, project, paper sql.NullString
	var data string
	err := row.Scan(&r.ID, &r.Owner, &r.Kind, &parent, &project, &paper, &r.Version, &data, &r.Created, &r.Updated)
	r.Parent = parent.String
	r.Project = project.String
	r.Paper = paper.String
	r.Data = json.RawMessage(data)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return r, err
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *Store) Get(ctx context.Context, owner, id, kind string) (domain.Record, error) {
	r, err := scan(s.DB.QueryRowContext(ctx, "SELECT * FROM records WHERE id=? AND owner_user_id=?", id, owner))
	if err == nil && kind != "" && r.Kind != kind {
		return domain.Record{}, domain.ErrNotFound
	}
	return r, err
}
func (s *Store) List(ctx context.Context, owner string, f Filter) ([]domain.Record, error) {
	q := "SELECT * FROM records WHERE owner_user_id=?"
	args := []any{owner}
	for _, pair := range [][2]string{{"kind", f.Kind}, {"project_id", f.Project}, {"paper_id", f.Paper}, {"parent_id", f.Parent}} {
		if pair[1] != "" {
			q += " AND " + pair[0] + "=?"
			args = append(args, pair[1])
		}
	}
	q += " ORDER BY created_at,id"
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Record{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Create(ctx context.Context, r domain.Record) (domain.Record, error) {
	if r.ID == "" {
		r.ID = domain.ID()
	}
	if r.Owner == "" {
		return r, errors.New("owner_required")
	}
	if r.Version == 0 {
		r.Version = 1
	}
	if !json.Valid(r.Data) {
		return r, errors.New("invalid_json")
	}
	r.Created = domain.Now()
	r.Updated = r.Created
	for _, pair := range [][2]string{{r.Project, "project"}, {r.Paper, "paper"}} {
		if pair[0] != "" {
			if _, err := s.Get(ctx, r.Owner, pair[0], pair[1]); err != nil {
				return r, err
			}
		}
	}
	if r.Paper != "" {
		paper, err := s.Get(ctx, r.Owner, r.Paper, "paper")
		if err != nil {
			return r, err
		}
		if paper.Project != r.Project {
			return r, errors.New("paper_project_mismatch")
		}
	}
	_, err := s.DB.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?,?,?,?,?,?,?)", r.ID, r.Owner, r.Kind, nullable(r.Parent), nullable(r.Project), nullable(r.Paper), r.Version, string(r.Data), r.Created, r.Updated)
	return r, err
}

// Update serializes local mutations and uses a version predicate to detect stale browser edits.
func (s *Store) Update(ctx context.Context, owner, id string, expected int, fn func(*domain.Record) error) (domain.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Get(ctx, owner, id, "")
	if err != nil {
		return r, err
	}
	if expected != 0 && r.Version != expected {
		return r, domain.ErrConflict
	}
	old := r.Version
	if err = fn(&r); err != nil {
		return r, err
	}
	r.Version++
	r.Updated = domain.Now()
	res, err := s.DB.ExecContext(ctx, "UPDATE records SET data=?,version=?,updated_at=? WHERE id=? AND owner_user_id=? AND version=?", string(r.Data), r.Version, r.Updated, id, owner, old)
	if err != nil {
		return r, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return r, domain.ErrConflict
	}
	return r, nil
}
func (s *Store) Delete(ctx context.Context, owner, id string) error {
	res, err := s.DB.ExecContext(ctx, "DELETE FROM records WHERE id=? AND owner_user_id=?", id, owner)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
func (s *Store) Blob(data []byte, suffix string) (string, string, error) {
	digest := sha256.Sum256(data)
	sum := hex.EncodeToString(digest[:])
	if strings.ContainsAny(suffix, "/\\") {
		return "", "", errors.New("invalid_suffix")
	}
	rel := filepath.Join("blobs", sum+strings.ToLower(suffix))
	path := filepath.Join(s.Root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", "", err
	}
	if _, err := os.Stat(path); err == nil {
		return filepath.ToSlash(rel), sum, nil
	}
	if err := Atomic(path, data, 0600); err != nil {
		return "", "", err
	}
	return filepath.ToSlash(rel), sum, nil
}
func Atomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".p2l-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}
func (s *Store) Path(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", errors.New("absolute_path_not_allowed")
	}
	root, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return "", err
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	r, err := filepath.Rel(root, path)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("path_outside_data")
	}
	return path, nil
}
func (s *Store) Event(ctx context.Context, owner, project, paper, action string, detail any) {
	_, _ = s.Create(ctx, domain.Record{Owner: owner, Kind: "event", Project: project, Paper: paper, Parent: project, Data: domain.Encode(map[string]any{"action": action, "detail": detail})})
}
func (s *Store) Users(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (s *Store) AddUser(ctx context.Context, name string) (string, error) {
	id := domain.ID()
	_, err := s.DB.ExecContext(ctx, "INSERT INTO users VALUES(?,?,0,?)", id, name, domain.Now())
	return id, err
}
func ValidateID(id string) error {
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 16 {
		return fmt.Errorf("invalid_id")
	}
	return nil
}

var recordsTable = regexp.MustCompile(`(?s)CREATE TABLE IF NOT EXISTS records \((.*?)\n\);`)
var recordKinds = regexp.MustCompile(`CHECK\(kind IN \([^)]*\)\)`)

// migrateRecordKinds rebuilds the records table when an older database's
// CHECK constraint does not yet allow every record kind the schema lists
// (SQLite cannot alter a CHECK constraint in place). It follows SQLite's
// documented procedure: create the new table, copy, drop, rename.
func migrateRecordKinds(db *sql.DB) error {
	var current string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='records'").Scan(&current); err != nil {
		return err
	}
	m := recordsTable.FindStringSubmatch(Schema)
	if m == nil {
		return errors.New("schema_records_missing")
	}
	// Up to date when the stored table already has the schema's kind list.
	if kinds := recordKinds.FindString(m[1]); kinds != "" && strings.Contains(current, kinds) {
		return nil
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		"CREATE TABLE records_new (" + m[1] + "\n)",
		"INSERT INTO records_new SELECT * FROM records",
		"DROP TABLE records",
		"ALTER TABLE records_new RENAME TO records",
		"INSERT OR IGNORE INTO migrations VALUES(2,strftime('%Y-%m-%dT%H:%M:%fZ','now'))",
	} {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate records: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// The indexes went with the old table; the schema recreates them.
	if _, err = conn.ExecContext(ctx, Schema); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("migrate records: foreign key check failed")
	}
	return rows.Err()
}

// migrateData applies one-time data changes, each recorded in migrations.
//
//	3: presentation design references were removed (2026-11-13): drop those
//	   sources; rename the source-excerpt provider to the offline test mode.
func migrateData(db *sql.DB) error {
	steps := []struct {
		version int
		stmts   []string
	}{
		{3, []string{
			"DELETE FROM records WHERE kind='source' AND json_extract(data,'$.role')='design'",
			"UPDATE records SET data=json_set(data,'$.name','" + OfflineProviderName + "') WHERE kind='provider' AND json_extract(data,'$.kind')='source-extract'",
		}},
	}
	for _, step := range steps {
		var done int
		if err := db.QueryRow("SELECT count(*) FROM migrations WHERE version=?", step.version).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		for _, stmt := range append(step.stmts, fmt.Sprintf("INSERT INTO migrations VALUES(%d,strftime('%%Y-%%m-%%dT%%H:%%M:%%fZ','now'))", step.version)) {
			if _, err = tx.Exec(stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d: %w", step.version, err)
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// OfflineProviderName names the provider that drafts from source excerpts
// without calling a model; the interface shows a translated label.
const OfflineProviderName = "Offline test mode (no AI)"
