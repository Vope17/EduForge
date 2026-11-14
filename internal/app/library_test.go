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

func TestDeleteLessonRemovesItsRecords(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	other, err := a.CreateLesson(ctx, owner, course.ID, domain.Lesson{Title: "Kept study"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Upload(ctx, owner, lesson.ID, "lesson.pdf", "research", []byte("%PDF-1.4 test")); err != nil {
		t.Fatal(err)
	}
	if _, err = a.create(ctx, owner, "artifact", lesson.ID, course.ID, lesson.ID, domain.Artifact{Type: "A", Language: "en"}); err != nil {
		t.Fatal(err)
	}
	task, err := a.create(ctx, owner, "task", lesson.ID, course.ID, lesson.ID, domain.Task{Type: "A", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.DeleteLesson(ctx, owner, lesson.ID); err == nil || err.Error() != "cancel_active_tasks_before_delete" {
		t.Fatalf("a running task should block deletion, got %v", err)
	}
	if _, err = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error { t.Status = "completed"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err = a.DeleteLesson(ctx, owner, lesson.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Get(ctx, owner, lesson.ID, "lesson"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("lesson should be gone")
	}
	for _, kind := range []string{"source", "knowledge", "artifact", "task"} {
		if n := count(t, a, owner, store.Filter{Kind: kind, Lesson: lesson.ID}); n != 0 {
			t.Fatalf("%d %s records survived", n, kind)
		}
	}
	if _, err = a.Store.Get(ctx, owner, other.ID, "lesson"); err != nil {
		t.Fatal("another study was affected")
	}
}

func TestDeleteArtifactVersionOrAllVersions(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	add := func(lang string) domain.Record {
		r, err := a.create(ctx, owner, "artifact", lesson.ID, course.ID, lesson.ID, domain.Artifact{Type: "A", Language: lang})
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
	if n := count(t, a, owner, store.Filter{Kind: "artifact", Lesson: lesson.ID}); n != 1 {
		t.Fatalf("only the English output should remain, %d left", n)
	}
	if _, err := a.Store.Get(ctx, owner, english.ID, "artifact"); err != nil {
		t.Fatal("an output in another language was removed")
	}
}

func TestChaptersFileStudiesAndSurviveExport(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	chapter, err := a.CreateChapter(ctx, owner, course.ID, "  Vision  ")
	if err != nil {
		t.Fatal(err)
	}
	if f, _ := domain.Decode[domain.Chapter](chapter.Data); f.Name != "Vision" {
		t.Fatalf("chapter name not trimmed: %q", f.Name)
	}
	if _, err = a.CreateChapter(ctx, owner, course.ID, " "); err == nil {
		t.Fatal("empty chapter name accepted")
	}
	if _, err = a.MoveLesson(ctx, owner, lesson.ID, chapter.ID); err != nil {
		t.Fatal(err)
	}
	elsewhere, err := a.CreateCourse(ctx, owner, domain.Course{Name: "Elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := a.CreateChapter(ctx, owner, elsewhere.ID, "Foreign")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.MoveLesson(ctx, owner, lesson.ID, foreign.ID); err == nil {
		t.Fatal("a study moved into another course's chapter")
	}
	// Saving the study's other fields keeps its chapter.
	current, _ := a.Store.Get(ctx, owner, lesson.ID, "lesson")
	p, _ := domain.Decode[domain.Lesson](current.Data)
	p.Title = "Renamed lesson"
	p.Chapter = ""
	if current, err = a.SaveLesson(ctx, owner, lesson.ID, current.Version, p); err != nil {
		t.Fatal(err)
	}
	if p, _ = domain.Decode[domain.Lesson](current.Data); p.Chapter != chapter.ID {
		t.Fatal("saving study fields dropped its chapter")
	}
	if _, err = a.SaveChapter(ctx, owner, chapter.ID, "Vision models", domain.OutputSettings{}); err != nil {
		t.Fatal(err)
	}

	archive, err := a.Export(ctx, owner, course.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := a.Import(ctx, owner, archive, false)
	if err != nil {
		t.Fatal(err)
	}
	chapters, _ := a.Store.List(ctx, owner, store.Filter{Kind: "chapter", Course: imported.ID})
	lessons, _ := a.Store.List(ctx, owner, store.Filter{Kind: "lesson", Course: imported.ID})
	if len(chapters) != 1 || len(lessons) != 1 {
		t.Fatalf("import should bring 1 chapter and 1 study, got %d and %d", len(chapters), len(lessons))
	}
	if ip, _ := domain.Decode[domain.Lesson](lessons[0].Data); ip.Chapter != chapters[0].ID {
		t.Fatal("imported study should point at the imported chapter")
	}

	if err = a.DeleteChapter(ctx, owner, chapter.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = a.Store.Get(ctx, owner, lesson.ID, "lesson")
	if p, _ = domain.Decode[domain.Lesson](current.Data); p.Chapter != "" {
		t.Fatal("deleting a chapter should unfile its studies")
	}
}

func TestDesignReferencesAreRejected(t *testing.T) {
	ctx := context.Background()
	a, owner, _, lesson := fixture(t)
	if _, err := a.Upload(ctx, owner, lesson.ID, "look.png", "design", []byte("png")); err == nil {
		t.Fatal("design reference upload accepted")
	}
	if _, err := a.Upload(ctx, owner, lesson.ID, "deck.potx", "research", []byte("PK..")); err == nil {
		t.Fatal("a template file was accepted as a research source")
	}
	if _, err := a.Upload(ctx, owner, lesson.ID, "lesson.pdf", "", []byte("%PDF-1.4")); err != nil {
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
