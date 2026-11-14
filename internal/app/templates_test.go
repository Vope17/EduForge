package app

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"eduforge/internal/domain"
)

func TestTemplateEditsAreVersionedAndRestorable(t *testing.T) {
	ctx := context.Background()
	a, owner, project, _ := fixture(t)
	b, err := os.ReadFile("../../tests/fixtures/template.potx")
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.ImportTemplate(ctx, owner, "Lab", "template.potx", b)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := domain.Decode[domain.Template](r.Data)
	spec, err := a.TemplateSpec(ctx, owner, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec.Colors["accent1"] = "00AA55"
	spec.BodyBox = []float64{0.1, 0.25, 0.8, 0.6}
	saved, err := a.SaveTemplateSpec(ctx, owner, r.ID, r.Version, spec)
	if err != nil {
		t.Fatal(err)
	}
	edited, _ := domain.Decode[domain.Template](saved.Data)
	if edited.File == original.File || edited.Name != "Lab" || len(edited.History) != 1 || edited.History[0].Version != r.Version {
		t.Fatalf("an edit should write a new file and keep the old version: %+v", edited.History)
	}
	if edited.BodyBox[1] != 0.25 {
		t.Fatal("the content area is kept from the spec")
	}
	if got, _ := a.TemplateSpec(ctx, owner, r.ID); got.Colors["accent1"] != "00AA55" {
		t.Fatal("the saved file should carry the edit")
	}
	if _, err = a.SaveTemplateSpec(ctx, owner, r.ID, r.Version, spec); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a stale save should conflict, got %v", err)
	}

	archive, err := a.Export(ctx, owner, project.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	z, _ := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	found := false
	for _, f := range z.File {
		found = found || f.Name == original.File
	}
	if !found {
		t.Fatal("exports should include earlier template files")
	}

	restored, err := a.RestoreTemplate(ctx, owner, r.ID, saved.Version, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := domain.Decode[domain.Template](restored.Data)
	if back.File != original.File || len(back.History) != 2 || back.History[0].Version != saved.Version {
		t.Fatalf("restoring should bring back the old file and keep the edit in history: %s %d", back.File, len(back.History))
	}
	if _, err = a.RestoreTemplate(ctx, owner, r.ID, restored.Version, 99); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("restoring an unknown version should fail")
	}
}
