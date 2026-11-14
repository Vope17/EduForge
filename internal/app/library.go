package app

import (
	"context"
	"errors"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// DeleteLesson removes a study and everything recorded under it: sources,
// knowledge, tasks, outputs, plans and chats go with it through the lesson
// foreign key; the reading-history entry is removed explicitly.
func (a *App) DeleteLesson(ctx context.Context, owner, id string) error {
	if _, err := a.Store.Get(ctx, owner, id, "lesson"); err != nil {
		return err
	}
	tasks, err := a.Store.List(ctx, owner, store.Filter{Kind: "task", Lesson: id})
	if err != nil {
		return err
	}
	for _, r := range tasks {
		t, _ := domain.Decode[domain.Task](r.Data)
		if t.Status == "running" || t.Status == "queued" || t.Status == "paused" {
			return errors.New("cancel_active_tasks_before_delete")
		}
	}
	return a.Store.Delete(ctx, owner, id)
}

// outputKey identifies one output across its versions, matching how the
// interface groups them: type, language and the lessons it combines.
func outputKey(art domain.Artifact) string {
	return art.Type + "|" + art.Language + "|" + strings.Join(art.GroupedLessons, ",")
}

// DeleteArtifact removes one output version, or with all every version of
// that output (same lesson, type, language and grouping).
func (a *App) DeleteArtifact(ctx context.Context, owner, id string, all bool) (int, error) {
	r, art, err := get[domain.Artifact](ctx, a, owner, id, "artifact")
	if err != nil {
		return 0, err
	}
	if !all {
		return 1, a.Store.Delete(ctx, owner, id)
	}
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Course: r.Course, Lesson: r.Lesson})
	if err != nil {
		return 0, err
	}
	key, deleted := outputKey(art), 0
	for _, item := range items {
		if item.Lesson != r.Lesson {
			continue
		}
		v, err := domain.Decode[domain.Artifact](item.Data)
		if err != nil || outputKey(v) != key {
			continue
		}
		if err = a.Store.Delete(ctx, owner, item.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func chapterName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return "", errors.New("chapter_name_required")
	}
	return name, nil
}

// CreateChapter adds a library chapter to a course.
func (a *App) CreateChapter(ctx context.Context, owner, course, name string) (domain.Record, error) {
	if _, err := a.Store.Get(ctx, owner, course, "course"); err != nil {
		return domain.Record{}, err
	}
	name, err := chapterName(name)
	if err != nil {
		return domain.Record{}, err
	}
	return a.create(ctx, owner, "chapter", course, course, "", domain.Chapter{Name: name})
}

// RenameChapter changes a chapter's name.
func (a *App) RenameChapter(ctx context.Context, owner, id, name string) (domain.Record, error) {
	if _, err := a.Store.Get(ctx, owner, id, "chapter"); err != nil {
		return domain.Record{}, err
	}
	name, err := chapterName(name)
	if err != nil {
		return domain.Record{}, err
	}
	return set[domain.Chapter](ctx, a, owner, id, 0, func(f *domain.Chapter) error {
		f.Name = name
		return nil
	})
}

// DeleteChapter removes a chapter; its studies become unfiled.
func (a *App) DeleteChapter(ctx context.Context, owner, id string) error {
	r, err := a.Store.Get(ctx, owner, id, "chapter")
	if err != nil {
		return err
	}
	lessons, err := a.Store.List(ctx, owner, store.Filter{Kind: "lesson", Course: r.Course})
	if err != nil {
		return err
	}
	for _, item := range lessons {
		p, _ := domain.Decode[domain.Lesson](item.Data)
		if p.Chapter != id {
			continue
		}
		if _, err = a.MoveLesson(ctx, owner, item.ID, ""); err != nil {
			return err
		}
	}
	return a.Store.Delete(ctx, owner, id)
}

// MoveLesson files a study in a chapter of its course, or unfiles it.
func (a *App) MoveLesson(ctx context.Context, owner, lessonID, chapterID string) (domain.Record, error) {
	lesson, err := a.Store.Get(ctx, owner, lessonID, "lesson")
	if err != nil {
		return domain.Record{}, err
	}
	if chapterID != "" {
		chapter, err := a.Store.Get(ctx, owner, chapterID, "chapter")
		if err != nil {
			return domain.Record{}, err
		}
		if chapter.Course != lesson.Course {
			return domain.Record{}, errors.New("chapter_course_mismatch")
		}
	}
	return set[domain.Lesson](ctx, a, owner, lessonID, 0, func(p *domain.Lesson) error {
		p.Chapter = chapterID
		return nil
	})
}
