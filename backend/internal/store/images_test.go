package store

import (
	"bytes"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"eduforge/backend/internal/artifact"
)

func TestUploadedImagesAreScopedAndPreservedAcrossRevisions(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "images.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	deck := artifact.NewID()
	_, err = db.db.ExecContext(ctx, `INSERT INTO document_requests(user_id,id,artifact_type,state,language,topic,audience,kind,slide_count) VALUES ('default',?,'slides','ready','en','Water','Teachers','presentation',1)`, deck)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.ExecContext(ctx, `INSERT INTO artifacts(user_id,id,current_revision) VALUES ('default',?,1)`, deck)
	if err != nil {
		t.Fatal(err)
	}
	original := artifact.Content{Title: "Water", Blocks: []artifact.Block{{ID: artifact.NewID(), Kind: "slide", Layout: "image_text", Children: []artifact.Block{
		{ID: artifact.NewID(), Kind: "heading", Text: "Water"}, {ID: artifact.NewID(), Kind: "image", Text: "Water cycle"},
	}}}}
	if err := db.transaction(ctx, func(tx *sql.Tx) error {
		return insertRevision(ctx, tx, "default", deck, 1, revision{content: original, origin: 1})
	}); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	url, err := db.SaveSlideImage(ctx, "default", deck, data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	owner, imageID, ok := artifact.LocalSlideImage(url)
	if !ok || owner != deck {
		t.Fatal("invalid image URL")
	}
	path, err := db.SlideImagePath(ctx, "default", deck, imageID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(stored)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SlideImagePath(ctx, "other-user", deck, imageID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user image read")
	}
	original.Blocks[0].Children[1].ImageURL = url
	saved, err := db.ChangeDocument(ctx, "default", deck, Change{BaseRevision: 1, Action: "save", Content: &original})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Content.Blocks[0].Children[1].ID != original.Blocks[0].Children[1].ID {
		t.Fatal("changed block ID")
	}
	old, err := db.Document(ctx, "default", deck, 1)
	if err != nil || old.Content.Blocks[0].Children[1].ImageURL != "" {
		t.Fatal("old revision changed")
	}
	original.Blocks[0].Children[1].ImageURL = "/api/v1/slides/" + artifact.NewID() + "/images/" + imageID
	if _, err := db.ChangeDocument(ctx, "default", deck, Change{BaseRevision: 2, Action: "save", Content: &original}); !errors.Is(err, artifact.ErrInvalid) {
		t.Fatal("cross-deck image accepted")
	}
	original.Blocks[0].Children[1].ImageURL = "/api/v1/slides/" + deck + "/images/" + artifact.NewID()
	if _, err := db.ChangeDocument(ctx, "default", deck, Change{BaseRevision: 2, Action: "save", Content: &original}); !errors.Is(err, artifact.ErrInvalid) {
		t.Fatal("unknown image accepted")
	}
	restored, err := db.ChangeDocument(ctx, "default", deck, Change{BaseRevision: 2, Action: "undo"})
	if err != nil || restored.Content.Blocks[0].Children[1].ImageURL != "" {
		t.Fatal("undo failed")
	}
	restored, err = db.ChangeDocument(ctx, "default", deck, Change{BaseRevision: 3, Action: "redo"})
	if err != nil || restored.Content.Blocks[0].Children[1].ImageURL != url {
		t.Fatal("redo lost image")
	}
	for _, invalid := range [][]byte{[]byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"), data.Bytes()[:30], make([]byte, (8<<20)+1)} {
		if _, err := db.SaveSlideImage(ctx, "default", deck, invalid); !errors.Is(err, artifact.ErrInvalid) {
			t.Fatal("invalid image accepted")
		}
	}
}
