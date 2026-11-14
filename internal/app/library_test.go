package app

import (
	"context"
	"errors"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func count(t *testing.T, a *App, owner string, f store.Filter) int {
	t.Helper()
	items, err := a.Store.List(context.Background(), owner, f)
	if err != nil {
		t.Fatal(err)
	}
	return len(items)
}

func TestDeletePaperRemovesItsRecordsAndReading(t *testing.T) {
	ctx := context.Background()
	a, owner, project, paper := fixture(t)
	other, err := a.CreatePaper(ctx, owner, project.ID, domain.Paper{Title: "Kept study"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Upload(ctx, owner, paper.ID, "paper.pdf", "research", []byte("%PDF-1.4 test")); err != nil {
		t.Fatal(err)
	}
	if _, err = a.create(ctx, owner, "artifact", paper.ID, project.ID, paper.ID, domain.Artifact{Type: "A", Language: "en"}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SavePlanChat(ctx, owner, paper.ID, domain.PlanChat{Messages: []domain.ChatMessage{{Role: "user", Text: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{paper.ID, other.ID} {
		if _, err = a.MarkRead(ctx, owner, id, true); err != nil {
			t.Fatal(err)
		}
	}
	task, err := a.create(ctx, owner, "task", paper.ID, project.ID, paper.ID, domain.Task{Type: "A", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.DeletePaper(ctx, owner, paper.ID); err == nil || err.Error() != "cancel_active_tasks_before_delete" {
		t.Fatalf("a running task should block deletion, got %v", err)
	}
	if _, err = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error { t.Status = "completed"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err = a.DeletePaper(ctx, owner, paper.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Get(ctx, owner, paper.ID, "paper"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("paper should be gone")
	}
	for _, kind := range []string{"source", "knowledge", "artifact", "plan_chat", "task"} {
		if n := count(t, a, owner, store.Filter{Kind: kind, Paper: paper.ID}); n != 0 {
			t.Fatalf("%d %s records survived", n, kind)
		}
	}
	readings, _ := a.Store.List(ctx, owner, store.Filter{Kind: "reading"})
	if len(readings) != 1 {
		t.Fatalf("only the other study's reading entry should remain, found %d", len(readings))
	}
	if r, _ := domain.Decode[domain.Reading](readings[0].Data); r.PaperID != other.ID {
		t.Fatal("the wrong reading entry was removed")
	}
	if _, err = a.Store.Get(ctx, owner, other.ID, "paper"); err != nil {
		t.Fatal("another study was affected")
	}
}

func TestDeleteArtifactVersionOrAllVersions(t *testing.T) {
	ctx := context.Background()
	a, owner, project, paper := fixture(t)
	add := func(lang string) domain.Record {
		r, err := a.create(ctx, owner, "artifact", paper.ID, project.ID, paper.ID, domain.Artifact{Type: "A", Language: lang})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	v1, v2, v3, english := add("zh-TW"), add("zh-TW"), add("zh-TW"), add("en")
	if n, err := a.DeleteArtifact(ctx, owner, v2.ID, false); err != nil || n != 1 {
		t.Fatalf("single version delete: %d %v", n, err)
	}
	if _, err := a.Store.Get(ctx, owner, v1.ID, "artifact"); err != nil {
		t.Fatal("other versions should remain")
	}
	if n, err := a.DeleteArtifact(ctx, owner, v3.ID, true); err != nil || n != 2 {
		t.Fatalf("all versions delete removed %d: %v", n, err)
	}
	if n := count(t, a, owner, store.Filter{Kind: "artifact", Paper: paper.ID}); n != 1 {
		t.Fatalf("only the English output should remain, %d left", n)
	}
	if _, err := a.Store.Get(ctx, owner, english.ID, "artifact"); err != nil {
		t.Fatal("an output in another language was removed")
	}
}

func TestFoldersFileStudiesAndSurviveExport(t *testing.T) {
	ctx := context.Background()
	a, owner, project, paper := fixture(t)
	folder, err := a.CreateFolder(ctx, owner, project.ID, "  Vision  ")
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := domain.Decode[domain.Folder](folder.Data); f.Name != "Vision" {
		t.Fatalf("folder name not trimmed: %q", f.Name)
	}
	if _, err = a.CreateFolder(ctx, owner, project.ID, " "); err == nil {
		t.Fatal("empty folder name accepted")
	}
	if _, err = a.MovePaper(ctx, owner, paper.ID, folder.ID); err != nil {
		t.Fatal(err)
	}
	elsewhere, err := a.CreateProject(ctx, owner, domain.Project{Name: "Elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := a.CreateFolder(ctx, owner, elsewhere.ID, "Foreign")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.MovePaper(ctx, owner, paper.ID, foreign.ID); err == nil {
		t.Fatal("a study moved into another project's folder")
	}
	// Saving the study's other fields keeps its folder.
	current, _ := a.Store.Get(ctx, owner, paper.ID, "paper")
	p, _ := domain.Decode[domain.Paper](current.Data)
	p.Repo = "https://example.org/repo"
	p.Folder = ""
	if current, err = a.SavePaper(ctx, owner, paper.ID, current.Version, p); err != nil {
		t.Fatal(err)
	}
	if p, _ = domain.Decode[domain.Paper](current.Data); p.Folder != folder.ID {
		t.Fatal("saving study fields dropped its folder")
	}
	if _, err = a.RenameFolder(ctx, owner, folder.ID, "Vision models"); err != nil {
		t.Fatal(err)
	}

	archive, err := a.Export(ctx, owner, project.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := a.Import(ctx, owner, archive, false)
	if err != nil {
		t.Fatal(err)
	}
	folders, _ := a.Store.List(ctx, owner, store.Filter{Kind: "folder", Project: imported.ID})
	papers, _ := a.Store.List(ctx, owner, store.Filter{Kind: "paper", Project: imported.ID})
	if len(folders) != 1 || len(papers) != 1 {
		t.Fatalf("import should bring 1 folder and 1 study, got %d and %d", len(folders), len(papers))
	}
	if ip, _ := domain.Decode[domain.Paper](papers[0].Data); ip.Folder != folders[0].ID {
		t.Fatal("imported study should point at the imported folder")
	}

	if err = a.DeleteFolder(ctx, owner, folder.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = a.Store.Get(ctx, owner, paper.ID, "paper")
	if p, _ = domain.Decode[domain.Paper](current.Data); p.Folder != "" {
		t.Fatal("deleting a folder should unfile its studies")
	}
}

func TestDesignReferencesAreRejected(t *testing.T) {
	ctx := context.Background()
	a, owner, _, paper := fixture(t)
	if _, err := a.Upload(ctx, owner, paper.ID, "look.png", "design", []byte("png")); err == nil {
		t.Fatal("design reference upload accepted")
	}
	if _, err := a.Upload(ctx, owner, paper.ID, "deck.potx", "research", []byte("PK..")); err == nil {
		t.Fatal("a template file was accepted as a research source")
	}
	if _, err := a.Upload(ctx, owner, paper.ID, "paper.pdf", "", []byte("%PDF-1.4")); err != nil {
		t.Fatalf("a source without a role is research: %v", err)
	}
}

func TestOfflineProviderIsAlwaysLast(t *testing.T) {
	ctx := context.Background()
	a, owner, _, _ := fixture(t)
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	// Put the offline mode first in the saved order.
	offline := refs[len(refs)-1].ID
	if refs[len(refs)-1].Provider.Kind != "source-extract" {
		t.Fatal("offline mode should be listed last by default")
	}
	if _, err = a.create(ctx, owner, "preference", "", "", "", map[string]any{"key": "provider_order", "ids": append([]string{offline}, ids[:len(ids)-1]...)}); err != nil {
		t.Fatal(err)
	}
	if refs, err = a.ProviderRefs(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if refs[len(refs)-1].ID != offline {
		t.Fatal("offline mode should stay after every model provider")
	}
}
