package app

import (
	"context"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func planReply(cite string) string {
	return `{"goal":"g","analysis":"a","experience":"e","objectives":[{"code":"O1","domain":"cognitive","text":"one"}],
	 "activities":[{"phase":"prepare","title":"w","description":"d","minutes":10,"objectives":["O1"]},{"phase":"develop","title":"t","description":"d","minutes":30,"objectives":["O1"]},{"phase":"wrap","title":"r","description":"d","minutes":10,"objectives":["O1"]}],
	 "references":["ref"],"citations":["` + cite + `"]}`
}

func sheetReply(cite string) string {
	return `{"title":"WS","questions":[{"type":"short","prompt":"Why?","answer":"Because.","explanation":"x","objective":"O1","points":2,"citations":["` + cite + `"]}]}`
}

func runOutput(t *testing.T, a *App, owner, course, lesson, kind string, settings domain.OutputSettings) domain.Artifact {
	t.Helper()
	ctx := context.Background()
	req, err := a.CreateRequest(ctx, owner, course, domain.Request{LessonIDs: []string{lesson}, Selected: []string{kind}, Languages: map[string]string{kind: "en"}, UILanguage: "en", Unlimited: true, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := a.StartRequest(ctx, owner, req.ID, "en", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.process(ctx, owner, tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	rows, _ := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Lesson: lesson})
	for _, r := range rows {
		if art, _ := domain.Decode[domain.Artifact](r.Data); art.TaskID == tasks[0].ID {
			return art
		}
	}
	t.Fatal("no output")
	return domain.Artifact{}
}

func TestLessonPlanIsWrittenFromTheKnowledgeAndChecked(t *testing.T) {
	a, owner, course, lesson := fixture(t)
	_, k := knowledgeOf(t, a, owner, lesson)
	cite := k.Units[0].ID
	prompts := useFakeModel(t, a, owner, func(string) string { return planReply(cite) })
	art := runOutput(t, a, owner, course.ID, lesson.ID, "C", domain.OutputSettings{Audience: "first-year students", Minutes: 50})
	if art.Type != "C" || art.Plan == nil || art.Mode != "model" {
		t.Fatalf("artifact = %+v", art)
	}
	if art.Plan.Info.Minutes != 50 || art.Plan.Info.Unit != "Synthetic nearest-centroid study" || !strings.Contains(art.Plan.Info.Source, "Acceptance fixture") || art.Plan.Info.Audience != "first-year students" {
		t.Fatalf("the basic data come from the lesson, not the model: %+v", art.Plan.Info)
	}
	if len(*prompts) != 1 || !strings.Contains((*prompts)[0], "MUST add up to exactly 50") {
		t.Fatalf("one model call asks for the class time:\n%v", *prompts)
	}
	if art.Check.Status != "checked" {
		t.Fatalf("a plan whose minutes add up and whose objectives are served: %+v", art.Check.Issues)
	}
}

func TestPlanWithWrongTotalTimeIsReportedNotFixed(t *testing.T) {
	a, owner, course, lesson := fixture(t)
	_, k := knowledgeOf(t, a, owner, lesson)
	useFakeModel(t, a, owner, func(string) string { return planReply(k.Units[0].ID) })
	art := runOutput(t, a, owner, course.ID, lesson.ID, "C", domain.OutputSettings{Minutes: 60})
	found := false
	for _, is := range art.Check.Issues {
		found = found || (is.Code == "plan_time_mismatch" && is.Detail == "50/60")
	}
	if !found || art.Plan.Activities[1].Minutes != 30 {
		t.Fatalf("a 50-minute plan for a 60-minute class is flagged and left as written: %+v", art.Check.Issues)
	}
}

func TestWorksheetIsWrittenWithTheQuestionMixAndEdited(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	_, k := knowledgeOf(t, a, owner, lesson)
	prompts := useFakeModel(t, a, owner, func(string) string { return sheetReply(k.Units[0].ID) })
	art := runOutput(t, a, owner, course.ID, lesson.ID, "D", domain.OutputSettings{Questions: map[string]int{"short": 1}})
	if art.Worksheet == nil || len(art.Worksheet.Questions) != 1 || art.Settings.Questions["short"] != 1 {
		t.Fatalf("worksheet = %+v", art.Worksheet)
	}
	if !strings.Contains((*prompts)[0], "- short: 1") {
		t.Fatalf("the prompt asks for the mix:\n%s", (*prompts)[0])
	}
	if art.Check.Status != "checked" {
		t.Fatalf("issues: %+v", art.Check.Issues)
	}
	// Editing saves a new version with the edited questions and checks it again.
	rows, _ := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Lesson: lesson.ID})
	rec := rows[len(rows)-1]
	edited := art
	w := *art.Worksheet
	w.Questions = append([]domain.Question{}, w.Questions...)
	w.Questions[0].Answer = ""
	edited.Worksheet = &w
	next, err := a.EditArtifact(ctx, owner, rec.ID, rec.Version, edited)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := domain.Decode[domain.Artifact](next.Data)
	if saved.Worksheet.Questions[0].Answer != "" || saved.Previous != rec.ID || saved.Check.Status != "draft" {
		t.Fatalf("an edit that blanks an answer is saved as a new draft version: %+v", saved.Check)
	}
	if _, err = a.EditArtifact(ctx, owner, rec.ID, rec.Version, domain.Artifact{Type: "D"}); err == nil {
		t.Fatal("a worksheet edit without a worksheet is rejected")
	}
}

func TestOutputsWithoutAModelAreExtractiveDrafts(t *testing.T) {
	a, owner, course, lesson := fixture(t)
	for _, kind := range []string{"C", "D"} {
		art := runOutput(t, a, owner, course.ID, lesson.ID, kind, domain.OutputSettings{Minutes: 50})
		if art.Mode != "source-extract" || art.Check.Status != "draft" {
			t.Fatalf("%s: %+v", kind, art)
		}
	}
}

func TestSyllabusIsPlannedFromTheCourseLessonsInOrder(t *testing.T) {
	ctx := context.Background()
	a, owner, course, first := fixture(t)
	second := newLesson(t, a, owner, course.ID, "Second lesson")
	if _, err := a.QueueSyllabus(ctx, owner, course.ID, 4, "en"); err == nil || err.Error() != "parse_before_generation" {
		t.Fatalf("every lesson needs knowledge first: %v", err)
	}
	lessonWithPages(t, a, owner, second, 2)
	if _, err := a.QueueSyllabus(ctx, owner, course.ID, 0, "en"); err == nil {
		t.Fatal("weeks must be 1 to 40")
	}
	prompts := useFakeModel(t, a, owner, func(string) string {
		return `{"description":"d","objectives":["o"],"schedule":[{"week":1,"topic":"Start","chapters":"c","method":"lecture","assessment":""},{"week":2,"topic":"More","chapters":"c","method":"lab","assessment":"quiz"}],"grading":[{"item":"Exam","percent":100}],"books":["b"]}`
	})
	task, err := a.QueueSyllabus(ctx, owner, course.ID, 2, "en")
	if err != nil {
		t.Fatal(err)
	}
	if err = a.process(ctx, owner, task.ID); err != nil {
		t.Fatal(err)
	}
	p := (*prompts)[0]
	if !strings.Contains(p, "over 2 weeks") || strings.Index(p, "Synthetic nearest-centroid study") > strings.Index(p, "Second lesson") {
		t.Fatalf("the prompt lists the lessons in teaching order:\n%s", p)
	}
	rows, _ := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Course: course.ID})
	var found domain.Artifact
	for _, r := range rows {
		if v, _ := domain.Decode[domain.Artifact](r.Data); v.Type == "S" {
			found = v
		}
	}
	if found.Syllabus == nil || len(found.Syllabus.Schedule) != 2 || found.Check.Status != "checked" || found.Syllabus.Name != "Acceptance fixture" {
		t.Fatalf("syllabus = %+v / %+v", found.Syllabus, found.Check)
	}
	_ = first
}
