package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"eduforge/internal/domain"
)

var firstPage = regexp.MustCompile(`(?s)Pages:\n.*?\n\[([^\]]+)\] `)
var batchNo = regexp.MustCompile(`batch (\d+) of (\d+)`)

// lessonWithPages stores knowledge with n source pages (pages in vision are
// listed) as the lesson's current knowledge.
func lessonWithPages(t *testing.T, a *App, owner string, lesson domain.Record, n int, vision ...int) domain.Record {
	t.Helper()
	units := []domain.Unit{}
	for i := 1; i <= n; i++ {
		u := domain.Unit{ID: fmt.Sprintf("src:p%d", i), SourceID: "src", SourceName: "lecture.pdf", Locator: i, Kind: "page", Text: fmt.Sprintf("Page %d explains concept %d with the value %d0.", i, i, i), Mode: "text"}
		for _, v := range vision {
			if v == i {
				u.Text, u.Mode = "", "vision"
			}
		}
		units = append(units, u)
	}
	k := domain.Knowledge{Title: "Lecture", Units: units, Claims: []domain.Claim{{Section: 2, Text: "rule-based excerpt", Citations: []string{"src:p1"}, Kind: "source_excerpt"}}, Terms: []domain.Term{}, Images: []domain.Image{}, Issues: []domain.Issue{}}
	r, err := a.create(context.Background(), owner, "knowledge", lesson.ID, lesson.Course, lesson.ID, k)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := set[domain.Lesson](context.Background(), a, owner, lesson.ID, 0, func(l *domain.Lesson) error {
		l.KnowledgeID = r.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func knowledgeOf(t *testing.T, a *App, owner string, lesson domain.Record) (domain.Record, domain.Knowledge) {
	t.Helper()
	l, _ := domain.Decode[domain.Lesson](lesson.Data)
	r, k, err := get[domain.Knowledge](context.Background(), a, owner, l.KnowledgeID, "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	return r, k
}

// batchModel answers each batch prompt with one point citing its first page.
// failBatch (1-based, 0 for none) answers with garbage instead.
func batchModel(t *testing.T, a *App, owner string, failBatch int) *[]string {
	return useFakeModel(t, a, owner, func(prompt string) string {
		m := batchNo.FindStringSubmatch(prompt)
		p := firstPage.FindStringSubmatch(prompt)
		if m == nil || p == nil {
			t.Errorf("not a batch prompt:\n%s", prompt)
			return "{}"
		}
		if fmt.Sprint(failBatch) == m[1] {
			return "I cannot do that"
		}
		return `{"sections":[{"section":2,"points":[{"text":"Concept from ` + p[1] + `","citations":["` + p[1] + `"]}]}],"terms":[{"original":"term-` + m[1] + `","citations":["` + p[1] + `"]}]}`
	})
}

func TestKnowledgeIsReadInBatchesAndMissedPagesCanBeResumed(t *testing.T) {
	ctx := context.Background()
	a, owner, _, lesson := fixture(t)
	lesson = lessonWithPages(t, a, owner, lesson, 9, 3)
	_, k := knowledgeOf(t, a, owner, lesson)

	prompts := batchModel(t, a, owner, 2)
	progress := []string{}
	got, err := a.writeKnowledge(ctx, owner, lesson.ID, k, nil, readControl{
		Batches: func(total int) { progress = append(progress, fmt.Sprintf("batches=%d", total)) },
		Done:    func(done, total int) { progress = append(progress, fmt.Sprintf("%d/%d", done, total)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(progress, " ") != "batches=3 1/3 2/3 3/3" {
		t.Fatalf("progress = %v", progress)
	}
	if got.Coverage == nil || got.Coverage.Pages != 9 || got.Coverage.Read != 5 || got.Coverage.Vision != 1 || strings.Join(got.Coverage.Missed, ",") != "src:p5,src:p6,src:p7,src:p8" {
		t.Fatalf("coverage = %+v", got.Coverage)
	}
	for _, c := range got.Claims {
		if c.Kind == "source_excerpt" {
			t.Fatal("a full read replaces the rule-based excerpts")
		}
	}
	found := false
	for _, is := range got.Issues {
		found = found || (is.Code == "coverage_incomplete" && is.Detail == "5/9")
	}
	if !found {
		t.Fatalf("incomplete coverage must be an issue, not silent: %+v", got.Issues)
	}
	if !got.Claims[0].Vision && got.Claims[0].Citations[0] == "src:p3" {
		t.Fatal("a point from a vision page is marked")
	}
	if len(*prompts) < 3 || !strings.Contains((*prompts)[0], "batch 1 of 3") {
		t.Fatalf("one call per batch (the failed one may be retried): %d", len(*prompts))
	}

	// Resume: read only the missed pages and add to what is there.
	batchModel(t, a, owner, 0)
	only := map[string]bool{}
	for _, id := range got.Coverage.Missed {
		only[id] = true
	}
	resumed, err := a.writeKnowledge(ctx, owner, lesson.ID, got, only, readControl{})
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.Coverage.Complete() || resumed.Coverage.Read != 9 || resumed.Coverage.Vision != 1 {
		t.Fatalf("resumed coverage = %+v", resumed.Coverage)
	}
	if len(resumed.Claims) <= len(got.Claims) {
		t.Fatal("resuming keeps the points already written and adds new ones")
	}
	for _, is := range resumed.Issues {
		if is.Code == "coverage_incomplete" {
			t.Fatal("complete coverage clears the issue")
		}
	}
}

func TestKnowledgeReadFailsWhenNoBatchSucceeds(t *testing.T) {
	a, owner, _, lesson := fixture(t)
	lesson = lessonWithPages(t, a, owner, lesson, 3)
	_, k := knowledgeOf(t, a, owner, lesson)
	batchModel(t, a, owner, 1) // the only batch fails
	if _, err := a.writeKnowledge(context.Background(), owner, lesson.ID, k, nil, readControl{}); err == nil {
		t.Fatal("a read where nothing was read must not look like success")
	}
}

func TestEarlierLessonsAreBackgroundForLaterOnes(t *testing.T) {
	ctx := context.Background()
	a, owner, course, first := fixture(t)
	_, fk := knowledgeOf(t, a, owner, first)
	fk.Terms = []domain.Term{{Original: "eigenvector", Citations: []string{fk.Units[0].ID}}}
	l1, _ := domain.Decode[domain.Lesson](first.Data)
	if _, err := a.Store.Create(ctx, domain.Record{Owner: owner, Kind: "knowledge", Parent: first.ID, Course: course.ID, Lesson: first.ID, Version: 2, Data: domain.Encode(fk)}); err != nil {
		t.Fatal(err)
	}
	_ = l1
	// The fixture's knowledge record is the lesson's current one; point the
	// lesson at the version with terms.
	rows, _ := a.Store.List(ctx, owner, storeFilter("knowledge", first.ID))
	latest := rows[len(rows)-1]
	if _, err := set[domain.Lesson](ctx, a, owner, first.ID, 0, func(l *domain.Lesson) error { l.KnowledgeID = latest.ID; return nil }); err != nil {
		t.Fatal(err)
	}
	second := newLesson(t, a, owner, course.ID, "Second lesson")
	second = lessonWithPages(t, a, owner, second, 2)
	_, k := knowledgeOf(t, a, owner, second)
	prompts := batchModel(t, a, owner, 0)
	if _, err := a.writeKnowledge(ctx, owner, second.ID, k, nil, readControl{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains((*prompts)[0], "- eigenvector") || !strings.Contains((*prompts)[0], "Do not explain them again") {
		t.Fatalf("the earlier lesson's concepts are background:\n%s", (*prompts)[0])
	}
	// The first lesson has nothing before it.
	prompts = batchModel(t, a, owner, 0)
	if _, err := a.writeKnowledge(ctx, owner, first.ID, fk, nil, readControl{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains((*prompts)[0], "Do not explain them again") {
		t.Fatal("the first lesson has no background")
	}
}

func TestKnowledgeTaskWritesANewRevisionAndRefusesWhileBusy(t *testing.T) {
	ctx := context.Background()
	a, owner, _, lesson := fixture(t)
	lesson = lessonWithPages(t, a, owner, lesson, 6)
	rec, _ := knowledgeOf(t, a, owner, lesson)
	batchModel(t, a, owner, 0)

	if _, err := a.QueueKnowledge(ctx, owner, lesson.ID, rec.Version+1, false); err == nil {
		t.Fatal("a stale version must be a conflict")
	}
	if _, err := a.QueueKnowledge(ctx, owner, lesson.ID, rec.Version, true); err == nil || err.Error() != "nothing_missed" {
		t.Fatalf("resuming without missed pages: %v", err)
	}
	task, err := a.QueueKnowledge(ctx, owner, lesson.ID, rec.Version, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.QueueKnowledge(ctx, owner, lesson.ID, rec.Version, false); err == nil || err.Error() != "knowledge_task_active" {
		t.Fatalf("a second read while one is queued: %v", err)
	}
	if err = a.process(ctx, owner, task.ID); err != nil {
		t.Fatal(err)
	}
	done, _ := domain.Decode[domain.Task](mustGet(t, a, owner, task.ID).Data)
	if done.Status != "completed" || done.Completed != done.Total || done.Total != 2 {
		t.Fatalf("task = %+v", done)
	}
	newRec, k := knowledgeOf(t, a, owner, mustGet(t, a, owner, lesson.ID))
	if newRec.Version != rec.Version+1 || k.Confirmed || !k.Coverage.Complete() || k.Author != "ai" {
		t.Fatalf("a new unconfirmed revision with full coverage: v%d %+v", newRec.Version, k.Coverage)
	}
}
