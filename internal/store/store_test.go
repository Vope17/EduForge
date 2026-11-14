package store

import (
	"context"
	"database/sql"
	"errors"
	"eduforge/internal/domain"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnershipConcurrencyAndPersistence(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "data")
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	alice := s.LocalUser
	bob, err := s.AddUser(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Create(ctx, domain.Record{Owner: alice, Kind: "project", Data: domain.Encode(domain.Project{Name: "Research"})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, bob, p.ID, "project"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-user read: %v", err)
	}
	_, err = s.Create(ctx, domain.Record{Owner: bob, Kind: "paper", Parent: p.ID, Project: p.ID, Data: domain.Encode(domain.Paper{Title: "Other"})})
	if err == nil {
		t.Fatal("cross-user project link accepted")
	}
	_, err = s.Update(ctx, bob, p.ID, 0, func(r *domain.Record) error { return nil })
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-user update accepted")
	}
	_, err = s.Update(ctx, alice, p.ID, p.Version, func(r *domain.Record) error {
		r.Data = domain.Encode(domain.Project{Name: "New"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(ctx, alice, p.ID, p.Version, func(r *domain.Record) error { return nil })
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatal("stale write accepted")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.LocalUser != alice {
		t.Fatal("local identity changed after restart")
	}
	r, err := s.Get(ctx, alice, p.ID, "project")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := domain.Decode[domain.Project](r.Data)
	if data.Name != "New" {
		t.Fatal("data lost after restart")
	}
}
func TestBlobTraversalAndAtomicTaskCommit(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	name, _, err := s.Blob([]byte("evidence"), ".log")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Path(name); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Path("../../etc/passwd"); err == nil {
		t.Fatal("traversal accepted")
	}
	p, err := s.Create(ctx, domain.Record{Owner: s.LocalUser, Kind: "project", Data: domain.Encode(domain.Project{Name: "P"})})
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.Create(ctx, domain.Record{Owner: s.LocalUser, Kind: "request", Parent: p.ID, Project: p.ID, Data: domain.Encode(domain.Request{Status: "preparing"})})
	if err != nil {
		t.Fatal(err)
	}
	tasks := []domain.Record{{Project: p.ID, Data: domain.Encode(domain.Task{Type: "A", Status: "queued"})}, {Project: domain.ID(), Data: domain.Encode(domain.Task{Type: "B", Status: "queued"})}}
	if _, err = s.CommitTasks(ctx, s.LocalUser, req.ID, req.Version, domain.Encode(domain.Request{Status: "running"}), tasks); err == nil {
		t.Fatal("bad batch accepted")
	}
	list, err := s.List(ctx, s.LocalUser, Filter{Kind: "task"})
	if err != nil || len(list) != 0 {
		t.Fatal("partial tasks survived rollback")
	}
	r, err := s.Get(ctx, s.LocalUser, req.ID, "request")
	if err != nil {
		t.Fatal(err)
	}
	v, _ := domain.Decode[domain.Request](r.Data)
	if v.Status != "preparing" {
		t.Fatal("request changed after rollback")
	}
}

func TestOlderDatabasesAreMigratedToNewRecordKinds(t *testing.T) {
	dir := t.TempDir()
	// A database created by an earlier schema, before plan_chat and folder existed.
	old := strings.Replace(Schema, ",'plan_chat','folder'", "", 1)
	if old == Schema {
		t.Fatal("test fixture no longer matches the schema's kind list")
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "eduforge.sqlite3")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{old,
		`INSERT INTO users VALUES('u','Local',1,'now')`,
		`INSERT INTO records VALUES('p','u','project',NULL,NULL,NULL,1,'{"name":"kept"}','now','now')`,
		`INSERT INTO records VALUES('a','u','paper','p','p',NULL,1,'{}','now','now')`,
	} {
		if _, err = db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO records VALUES('c','u','plan_chat','a','p','a',1,'{}','now','now')`); err == nil {
		t.Fatal("the old schema should reject the new kind")
	}
	db.Close()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.DB.Exec(`INSERT INTO records VALUES('c','u','plan_chat','a','p','a',1,'{}','now','now')`); err != nil {
		t.Fatalf("migrated schema should accept plan_chat: %v", err)
	}
	if _, err = s.DB.Exec(`INSERT INTO records VALUES('f','u','folder','p','p',NULL,1,'{"name":"x"}','now','now')`); err != nil {
		t.Fatalf("migrated schema should accept folder: %v", err)
	}
	var name string
	if err = s.DB.QueryRow(`SELECT json_extract(data,'$.name') FROM records WHERE id='p'`).Scan(&name); err != nil || name != "kept" {
		t.Fatalf("existing records should survive: %q %v", name, err)
	}
	if _, err = s.DB.Exec(`DELETE FROM records WHERE id='p'`); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = s.DB.QueryRow(`SELECT count(*) FROM records`).Scan(&left)
	if left != 0 {
		t.Fatal("foreign keys with cascading deletes should still hold after the rebuild")
	}
	var indexes int
	_ = s.DB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND tbl_name='records' AND name IN ('records_scope','records_parent','step_once')`).Scan(&indexes)
	if indexes != 3 {
		t.Fatalf("indexes should be recreated, found %d", indexes)
	}
}

func TestDesignReferencesAndOfflineProviderAreMigrated(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	u := s.LocalUser
	for _, stmt := range []string{
		`DELETE FROM migrations WHERE version=3`,
		`INSERT INTO records VALUES('p','` + u + `','project',NULL,NULL,NULL,1,'{"name":"x"}','now','now')`,
		`INSERT INTO records VALUES('a','` + u + `','paper','p','p',NULL,1,'{}','now','now')`,
		`INSERT INTO records VALUES('r','` + u + `','source','a','p','a',1,'{"role":"research"}','now','now')`,
		`INSERT INTO records VALUES('d','` + u + `','source','a','p','a',1,'{"role":"design"}','now','now')`,
		`INSERT INTO records VALUES('o','` + u + `','provider',NULL,NULL,NULL,1,'{"name":"Source excerpts · drafts only","kind":"source-extract"}','now','now')`,
	} {
		if _, err = s.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if s, err = Open(dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var sources int
	_ = s.DB.QueryRow(`SELECT count(*) FROM records WHERE kind='source'`).Scan(&sources)
	if sources != 1 {
		t.Fatalf("design references should be removed and research sources kept, %d left", sources)
	}
	var name string
	_ = s.DB.QueryRow(`SELECT json_extract(data,'$.name') FROM records WHERE id='o'`).Scan(&name)
	if name != OfflineProviderName {
		t.Fatalf("offline provider not renamed: %q", name)
	}
}
