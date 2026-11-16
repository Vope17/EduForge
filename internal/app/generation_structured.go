package app

import (
	"context"
	"errors"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/research"
)

// A lesson plan (C) and a worksheet (D) are written in one model call from
// the lesson's knowledge; a syllabus (S) is written for a whole course from
// its lessons. Without a model, a draft is built from the knowledge and
// marked as an extractive draft.

// lessonSource names where a lesson's material sits: course / chapter / lesson.
func (a *App) lessonSource(ctx context.Context, owner string, lesson domain.Record) string {
	parts := []string{}
	if _, c, err := get[domain.Course](ctx, a, owner, lesson.Course, "course"); err == nil {
		parts = append(parts, c.Name)
	}
	l, _ := domain.Decode[domain.Lesson](lesson.Data)
	if l.Chapter != "" {
		if _, ch, err := get[domain.Chapter](ctx, a, owner, l.Chapter, "chapter"); err == nil {
			parts = append(parts, ch.Name)
		}
	}
	return strings.Join(append(parts, l.Title), " / ")
}

func (a *App) writeStructured(ctx context.Context, owner, id string, task domain.Record, t domain.Task, k domain.Knowledge, kind string) error {
	lesson, err := a.Store.Get(ctx, owner, task.Lesson, "lesson")
	if err != nil {
		return err
	}
	l, _ := domain.Decode[domain.Lesson](lesson.Data)
	_, request, err := get[domain.Request](ctx, a, owner, t.RequestID, "request")
	if err != nil {
		return err
	}
	art := domain.Artifact{Type: kind, Language: t.Language, KnowledgeID: t.KnowledgeID, RequestID: t.RequestID, TaskID: id, Style: t.Style, Settings: t.Settings, Sections: []domain.Section{}, Mode: "model", Background: t.Background}
	info := domain.PlanInfo{Unit: l.Title, Audience: t.Settings.Audience, Minutes: t.Settings.Minutes, Source: a.lessonSource(ctx, owner, lesson)}
	var provider string
	switch kind {
	case "C":
		var plan domain.LessonPlan
		prompt := research.PlanPrompt(research.PlanRequest{Knowledge: k, Info: info, Lang: t.Language, Settings: t.Settings, Known: t.ReadTerms})
		_, provider, err = a.Providers.Complete(ctx, owner, request.Providers, prompt, func(output string) error {
			p, err := research.ParsePlan(output, k, info)
			if err == nil {
				plan = p
			}
			return err
		})
		if err == nil {
			art.Plan = &plan
		}
		if err != nil && err.Error() == "model_connection_required" {
			p := research.FallbackPlan(k, info, t.Language)
			art.Plan, art.Mode, err = &p, "source-extract", nil
		}
	case "D":
		mix := t.Settings.Questions
		if len(mix) == 0 {
			mix = research.DefaultQuestions(k)
		}
		art.Settings.Questions = mix
		var sheet domain.Worksheet
		prompt := research.WorksheetPrompt(k, t.Language, t.Settings, mix, t.ReadTerms)
		_, provider, err = a.Providers.Complete(ctx, owner, request.Providers, prompt, func(output string) error {
			w, err := research.ParseWorksheet(output, k)
			if err == nil {
				sheet = w
			}
			return err
		})
		if err == nil {
			if strings.TrimSpace(sheet.Title) == "" {
				sheet.Title = l.Title
			}
			art.Worksheet = &sheet
		}
		if err != nil && err.Error() == "model_connection_required" {
			w := research.FallbackWorksheet(k, l.Title)
			art.Worksheet, art.Mode, err = &w, "source-extract", nil
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.ErrPaused
	}
	_ = provider
	if !a.taskCanContinue(ctx, owner, id) {
		return nil
	}
	art.Check = research.Validate(art, k)
	return a.finishArtifact(ctx, owner, task, art, false)
}

// QueueSyllabus starts writing a course's syllabus over the given number of
// weeks, from its lessons in teaching order.
func (a *App) QueueSyllabus(ctx context.Context, owner, course string, weeks int, language string) (domain.Record, error) {
	if _, err := a.Store.Get(ctx, owner, course, "course"); err != nil {
		return domain.Record{}, err
	}
	if weeks < 1 || weeks > 40 {
		return domain.Record{}, errors.New("invalid_weeks")
	}
	if !domain.Language(language) {
		language = a.uiLanguage(ctx, owner)
	}
	lessons, err := a.OrderedLessons(ctx, owner, course)
	if err != nil {
		return domain.Record{}, err
	}
	if len(lessons) == 0 {
		return domain.Record{}, errors.New("course_has_no_lessons")
	}
	for _, r := range lessons {
		if l, _ := domain.Decode[domain.Lesson](r.Data); l.KnowledgeID == "" {
			return domain.Record{}, errors.New("parse_before_generation")
		}
	}
	return a.create(ctx, owner, "task", course, course, "", domain.Task{Type: "syllabus", Status: "queued", Stage: "generate", Language: language, Weeks: weeks, Total: 1})
}

func (a *App) syllabusTask(ctx context.Context, owner string, task domain.Record) error {
	t, _ := domain.Decode[domain.Task](task.Data)
	_, course, err := get[domain.Course](ctx, a, owner, task.Course, "course")
	if err != nil {
		return err
	}
	ordered, err := a.OrderedLessons(ctx, owner, task.Course)
	if err != nil {
		return err
	}
	var lessons []research.SyllabusLesson
	for _, r := range ordered {
		l, _ := domain.Decode[domain.Lesson](r.Data)
		item := research.SyllabusLesson{Title: l.Title}
		if l.Chapter != "" {
			if _, ch, e := get[domain.Chapter](ctx, a, owner, l.Chapter, "chapter"); e == nil {
				item.Chapter = ch.Name
			}
		}
		if _, k, e := get[domain.Knowledge](ctx, a, owner, l.KnowledgeID, "knowledge"); e == nil {
			for _, c := range k.Claims {
				text := research.Clip(strings.Join(strings.Fields(c.Text), " "), 120)
				switch {
				case c.Section == research.SecObjectives && len(item.Objective) < 6:
					item.Objective = append(item.Objective, text)
				case c.Section == research.SecConcepts && len(item.Concepts) < 6:
					item.Concepts = append(item.Concepts, text)
				}
			}
		}
		lessons = append(lessons, item)
	}
	settings := NormalizeSettings(course.Settings)
	audience := settings.Audience
	var syllabus domain.Syllabus
	mode := "model"
	prompt := research.SyllabusPrompt(course.Name, audience, t.Weeks, lessons, t.Language, settings.Instructions["S"])
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return err
	}
	_, _, err = a.Providers.Complete(ctx, owner, refs, prompt, func(output string) error {
		s, err := research.ParseSyllabus(output, course.Name, audience, t.Weeks)
		if err == nil {
			syllabus = s
		}
		return err
	})
	if err != nil && err.Error() == "model_connection_required" {
		syllabus, mode, err = research.FallbackSyllabus(course.Name, audience, t.Weeks, lessons), "source-extract", nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.ErrPaused
	}
	if !a.taskCanContinue(ctx, owner, task.ID) {
		return nil
	}
	art := domain.Artifact{Type: "S", Language: t.Language, TaskID: task.ID, Sections: []domain.Section{}, Syllabus: &syllabus, Mode: mode, Settings: settings}
	art.Check = research.Validate(art, domain.Knowledge{})
	return a.finishArtifact(ctx, owner, task, art, false)
}
