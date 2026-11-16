package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"eduforge/internal/domain"
	"eduforge/internal/providers"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

// A lesson's knowledge is written by a model when one is connected: it reads
// every page of the lesson's sources in batches (text and page images), and
// the points it writes for each section cite the pages they come from (see
// research.BatchPrompt). The result records its coverage. Without a model the
// rule-based source excerpts stay.

// uiLanguage is the user's interface language (zh-TW by default).
func (a *App) uiLanguage(ctx context.Context, owner string) string {
	items, err := a.Store.List(ctx, owner, store.Filter{Kind: "preference"})
	if err != nil {
		return "zh-TW"
	}
	for _, r := range items {
		var p struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			Imported bool   `json:"imported_candidate"`
		}
		_ = json.Unmarshal(r.Data, &p)
		if p.Key == "ui_language" && !p.Imported && domain.Language(p.Value) {
			return p.Value
		}
	}
	return "zh-TW"
}

// priorKnowledge returns the knowledge of the lessons taught before a lesson:
// what its students already know.
func (a *App) priorKnowledge(ctx context.Context, owner, lessonID string) []domain.Knowledge {
	prior, err := a.PriorLessons(ctx, owner, lessonID)
	if err != nil {
		return nil
	}
	out := []domain.Knowledge{}
	for _, r := range prior {
		l, _ := domain.Decode[domain.Lesson](r.Data)
		if l.KnowledgeID == "" {
			continue
		}
		if _, k, err := get[domain.Knowledge](ctx, a, owner, l.KnowledgeID, "knowledge"); err == nil {
			out = append(out, k)
		}
	}
	return out
}

// readControl lets a long read report progress and be stopped.
type readControl struct {
	// Batches is called once the number of model calls is known.
	Batches func(total int)
	// Done is called after each batch, read or missed.
	Done func(done, total int)
	// Continue returns false when the read should stop (cancelled, paused).
	Continue func() bool
}

// writeKnowledge has a model read k's pages and write its points, terms and
// coverage. only limits the read to those page ids and adds to what k already
// holds (resuming after missed batches); nil reads every page and replaces
// k's claims and terms. A batch that fails is recorded as missed and the read
// goes on; if no batch succeeds, the error is returned.
func (a *App) writeKnowledge(ctx context.Context, owner, lessonID string, k domain.Knowledge, only map[string]bool, ctl readControl) (domain.Knowledge, error) {
	if len(k.Units) == 0 {
		return k, errors.New("knowledge_required")
	}
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return k, err
	}
	lang := a.uiLanguage(ctx, owner)
	courseName, audience := "", ""
	if r, err := a.Store.Get(ctx, owner, lessonID, "lesson"); err == nil {
		if _, course, e := get[domain.Course](ctx, a, owner, r.Course, "course"); e == nil {
			courseName = course.Name
		}
		if s, e := a.EffectiveSettings(ctx, owner, lessonID); e == nil {
			audience = s.Audience
		}
	}
	background := research.BackgroundConcepts(a.priorKnowledge(ctx, owner, lessonID))

	pages := []domain.Unit{}
	for _, u := range k.Units {
		if only == nil || only[u.ID] {
			pages = append(pages, u)
		}
	}
	batches := research.PlanBatches(pages)
	if ctl.Batches != nil {
		ctl.Batches(len(batches))
	}
	base := k
	previouslyRead := map[string]bool{}
	if only == nil {
		base.Claims, base.Terms = nil, nil
	} else if k.Coverage != nil {
		missed := map[string]bool{}
		for _, id := range k.Coverage.Missed {
			missed[id] = true
		}
		for _, u := range k.Units {
			previouslyRead[u.ID] = !missed[u.ID]
		}
	}
	read := map[string]bool{}
	parts := []research.Partial{}
	var lastErr error
	for i, batch := range batches {
		if ctl.Continue != nil && !ctl.Continue() {
			return k, context.Canceled
		}
		images := []string{}
		for _, u := range batch.Images() {
			if path, err := a.Store.Path(u.Preview); err == nil {
				images = append(images, path)
			}
		}
		prompt := research.BatchPrompt(research.BatchRequest{Lang: lang, Title: k.Title, Batch: batch, Index: i + 1, Total: len(batches), Background: background, Course: courseName, Audience: audience})
		var part research.Partial
		_, _, err := a.Providers.Complete(providers.WithImages(ctx, images), owner, refs, prompt, func(output string) error {
			p, err := research.ParseBatch(output, batch)
			if err == nil {
				part = p
			}
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return k, ctx.Err()
			}
			lastErr = err
			if ctl.Done != nil {
				ctl.Done(i+1, len(batches))
			}
			// No provider is usable: the remaining batches would fail the same way.
			if errors.Is(err, domain.ErrPaused) {
				break
			}
			continue
		}
		parts = append(parts, part)
		for _, id := range batch.IDs() {
			read[id] = true
		}
		if ctl.Done != nil {
			ctl.Done(i+1, len(batches))
		}
	}
	if len(parts) == 0 {
		if lastErr == nil {
			lastErr = errors.New("nothing_to_read")
		}
		return k, lastErr
	}
	out := research.Assemble(base, parts)
	for id, ok := range previouslyRead {
		if ok {
			read[id] = true
		}
	}
	out.Coverage = research.CountCoverage(out.Units, read, len(batches))
	if only != nil && k.Coverage != nil {
		out.Coverage.Batches += k.Coverage.Batches
	}
	out.Issues = slices.DeleteFunc(slices.Clone(out.Issues), func(i domain.Issue) bool { return i.Code == "coverage_incomplete" })
	if !out.Coverage.Complete() {
		out.Issues = append(out.Issues, domain.Issue{Code: "coverage_incomplete", Detail: fmt.Sprintf("%d/%d", out.Coverage.Read, out.Coverage.Pages)})
	}
	out.Language = lang
	// A full read replaces everything, so it records today's background. A
	// read of missed pages adds to knowledge written earlier and keeps that
	// knowledge's record unless it has none.
	out.Background = k.Background
	if only == nil || out.Background == nil {
		out.Background = a.backgroundOf(ctx, owner, lessonID)
	}
	return out, nil
}

// QueueKnowledge starts a background read that writes a new version of a
// lesson's knowledge from its parsed sources. With missedOnly it reads only
// the pages the last read missed and adds to the knowledge. The new version
// needs reviewing again.
func (a *App) QueueKnowledge(ctx context.Context, owner, lessonID string, expected int, missedOnly bool) (domain.Record, error) {
	lesson, p, err := get[domain.Lesson](ctx, a, owner, lessonID, "lesson")
	if err != nil {
		return domain.Record{}, err
	}
	old, oldK, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	if err != nil {
		return domain.Record{}, err
	}
	if expected != old.Version {
		return domain.Record{}, domain.ErrConflict
	}
	if missedOnly && (oldK.Coverage == nil || len(oldK.Coverage.Missed) == 0) {
		return domain.Record{}, errors.New("nothing_missed")
	}
	active, err := a.Store.List(ctx, owner, store.Filter{Kind: "task", Lesson: lessonID})
	if err != nil {
		return domain.Record{}, err
	}
	for _, r := range active {
		t, _ := domain.Decode[domain.Task](r.Data)
		if (t.Type == "knowledge" || t.Type == "parse") && (t.Status == "queued" || t.Status == "running" || t.Status == "paused") {
			return domain.Record{}, errors.New("knowledge_task_active")
		}
	}
	return a.create(ctx, owner, "task", lessonID, lesson.Course, lessonID, domain.Task{Type: "knowledge", Status: "queued", Stage: "knowledge", MissedOnly: missedOnly, Total: 1})
}

// knowledgeTask runs a queued knowledge read.
func (a *App) knowledgeTask(ctx context.Context, owner string, task domain.Record) error {
	t, _ := domain.Decode[domain.Task](task.Data)
	lesson, p, err := get[domain.Lesson](ctx, a, owner, task.Lesson, "lesson")
	if err != nil {
		return err
	}
	old, oldK, err := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	if err != nil {
		return err
	}
	var only map[string]bool
	if t.MissedOnly && oldK.Coverage != nil {
		only = map[string]bool{}
		for _, id := range oldK.Coverage.Missed {
			only[id] = true
		}
	}
	k, err := a.writeKnowledge(ctx, owner, task.Lesson, oldK, only, a.progressOf(ctx, owner, task.ID))
	if err != nil {
		if errors.Is(err, context.Canceled) && !a.taskCanContinue(ctx, owner, task.ID) {
			return nil
		}
		return err
	}
	k.Confirmed = false
	k.Edits = oldK.Edits + 1
	r, err := a.Store.Create(ctx, domain.Record{Owner: owner, Kind: "knowledge", Parent: task.Lesson, Course: lesson.Course, Lesson: task.Lesson, Version: old.Version + 1, Data: domain.Encode(k)})
	if err != nil {
		return err
	}
	if _, err = set[domain.Lesson](ctx, a, owner, task.Lesson, 0, func(p *domain.Lesson) error {
		p.KnowledgeID = r.ID
		return nil
	}); err != nil {
		return err
	}
	a.Store.Event(ctx, owner, lesson.Course, task.Lesson, "knowledge_written_by_ai", map[string]any{"previous": old.ID, "current": r.ID, "claims": len(k.Claims)})
	_, err = set[domain.Task](ctx, a, owner, task.ID, 0, func(t *domain.Task) error {
		t.Status = "completed"
		t.ResultID = r.ID
		t.Completed = t.Total
		return nil
	})
	return err
}

// progressOf reports a read's progress on its task and lets it be stopped.
func (a *App) progressOf(ctx context.Context, owner, taskID string) readControl {
	return a.progressOffset(ctx, owner, taskID, 0)
}

// progressOffset is progressOf for a task that has already done offset steps
// (the parse task parses sources first).
func (a *App) progressOffset(ctx context.Context, owner, taskID string, offset int) readControl {
	return readControl{
		Batches: func(total int) {
			_, _ = set[domain.Task](ctx, a, owner, taskID, 0, func(t *domain.Task) error {
				t.Total = offset + total
				t.Completed = offset
				return nil
			})
		},
		Done: func(done, _ int) {
			_, _ = set[domain.Task](ctx, a, owner, taskID, 0, func(t *domain.Task) error {
				t.Completed = offset + done
				return nil
			})
		},
		Continue: func() bool { return a.taskCanContinue(ctx, owner, taskID) },
	}
}
