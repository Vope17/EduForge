package store

import (
	"context"
	"path/filepath"
	"testing"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/migrations"
)

func TestSlidesMigrationPreservesExistingDocuments(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "legacy.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := db.db.ExecContext(ctx, "CREATE TABLE schema_migrations(name TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_documents.sql", "002_model_registration.sql"} {
		schema, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.db.ExecContext(ctx, string(schema)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.db.ExecContext(ctx, "INSERT INTO schema_migrations VALUES (?)", name); err != nil {
			t.Fatal(err)
		}
	}
	content := `{"title":"原始教案","blocks":[{"id":"original-block","kind":"paragraph","text":"舊內容"}]}`
	spec := `{"palette":"forest","font":"sans","layout":"sections","tone":"friendly","density":"balanced","example":[{"kind":"heading","text":"範例"}]}`
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO document_requests(user_id,id,state,language,topic,audience,kind,template_version,confirmed_version)
			VALUES ('teacher','legacy','ready','zh-TW','水循環','五年級','lesson_plan',1,1)`, nil},
		{`INSERT INTO template_versions VALUES ('teacher','legacy',1,?)`, []any{spec}},
		{`INSERT INTO artifacts VALUES ('teacher','legacy',1)`, nil},
		{`INSERT INTO artifact_revisions(user_id,artifact_id,revision,content_json,origin_revision,undo_revision,redo_json)
			VALUES ('teacher','legacy',1,?,1,0,'[]')`, []any{content}},
		{`INSERT INTO revision_blocks VALUES ('teacher','legacy',1,'original-block')`, nil},
	}
	for _, statement := range statements {
		if _, err := db.db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migration was not repeatable: %v", err)
	}
	var stored, storedSpec string
	if err := db.db.QueryRowContext(ctx, "SELECT content_json FROM artifact_revisions WHERE artifact_id='legacy'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, "SELECT spec_json FROM template_versions WHERE artifact_id='legacy'").Scan(&storedSpec); err != nil {
		t.Fatal(err)
	}
	if stored != content || storedSpec != spec {
		t.Fatal("migration rewrote immutable source JSON")
	}
	request, err := db.Request(ctx, "teacher", "legacy")
	if err != nil || request.Type != artifact.DocumentType || request.ConfirmedVersion != 1 || request.Version != 1 {
		t.Fatalf("legacy request changed: %+v, %v", request, err)
	}
	document, err := db.Document(ctx, "teacher", "legacy", 1)
	if err != nil || document.Type != artifact.DocumentType || document.Content.Blocks[0].ID != "original-block" {
		t.Fatalf("legacy revision changed: %+v, %v", document, err)
	}
	rows, err := db.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration broke foreign keys")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
