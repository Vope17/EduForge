package app

import (
	"context"
	"testing"

	"eduforge/internal/domain"
)

func newLesson(t *testing.T, a *App, owner, course, title string) domain.Record {
	t.Helper()
	r, err := a.CreateLesson(context.Background(), owner, course, domain.Lesson{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func titles(t *testing.T, records []domain.Record) []string {
	t.Helper()
	out := []string{}
	for _, r := range records {
		l, _ := domain.Decode[domain.Lesson](r.Data)
		out = append(out, l.Title)
	}
	return out
}

func TestTeachingOrderFollowsChaptersThenLessons(t *testing.T) {
	ctx := context.Background()
	a, owner, course, first := fixture(t)
	one, _ := a.CreateChapter(ctx, owner, course.ID, "One")
	two, _ := a.CreateChapter(ctx, owner, course.ID, "Two")
	b := newLesson(t, a, owner, course.ID, "B")
	c := newLesson(t, a, owner, course.ID, "C")
	d := newLesson(t, a, owner, course.ID, "D")
	// Arrange: Two first, then One; C and D in One (D first), B in Two; the
	// fixture's lesson stays unfiled and therefore comes first.
	err := a.ReorderCourse(ctx, owner, course.ID, CourseOrder{
		Chapters: []string{two.ID, one.ID},
		Groups: []LessonGroup{
			{Chapter: "", Lessons: []string{first.ID}},
			{Chapter: two.ID, Lessons: []string{b.ID}},
			{Chapter: one.ID, Lessons: []string{d.ID, c.ID}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := a.OrderedLessons(ctx, owner, course.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := titles(t, ordered)
	want := []string{"Synthetic nearest-centroid study", "B", "D", "C"}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	prior, err := a.PriorLessons(ctx, owner, c.ID)
	if err != nil || len(prior) != 3 {
		t.Fatalf("prior lessons of the last one = %d, %v", len(prior), err)
	}
	prior, _ = a.PriorLessons(ctx, owner, first.ID)
	if len(prior) != 0 {
		t.Fatalf("the first lesson has no prior lessons, got %d", len(prior))
	}
}

func TestReorderRejectsIncompleteOrForeignIDs(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	chapter, _ := a.CreateChapter(ctx, owner, course.ID, "One")
	other, _ := a.CreateCourse(ctx, owner, domain.Course{Name: "Other"})
	foreign := newLesson(t, a, owner, other.ID, "Foreign")
	cases := map[string]CourseOrder{
		"missing chapter":  {Chapters: nil, Groups: []LessonGroup{{Lessons: []string{lesson.ID}}}},
		"missing lesson":   {Chapters: []string{chapter.ID}},
		"foreign lesson":   {Chapters: []string{chapter.ID}, Groups: []LessonGroup{{Lessons: []string{lesson.ID, foreign.ID}}}},
		"duplicate lesson": {Chapters: []string{chapter.ID}, Groups: []LessonGroup{{Lessons: []string{lesson.ID}}, {Chapter: chapter.ID, Lessons: []string{lesson.ID}}}},
		"unknown chapter":  {Chapters: []string{chapter.ID}, Groups: []LessonGroup{{Chapter: "nope", Lessons: []string{lesson.ID}}}},
	}
	for name, order := range cases {
		if err := a.ReorderCourse(ctx, owner, course.ID, order); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSettingsInheritCourseChapterLesson(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	courseRecord, _ := a.Store.Get(ctx, owner, course.ID, "course")
	if _, err := a.SaveCourse(ctx, owner, course.ID, courseRecord.Version, "Course", "", domain.OutputSettings{Audience: "undergraduates", Minutes: 90, Level: "advanced"}); err != nil {
		t.Fatal(err)
	}
	got, err := a.EffectiveSettings(ctx, owner, lesson.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Audience != "undergraduates" || got.Minutes != 90 || got.Level != "advanced" || got.Depth != "standard" {
		t.Fatalf("course defaults not inherited: %+v", got)
	}
	chapter, _ := a.CreateChapter(ctx, owner, course.ID, "Lab")
	if _, err = a.SaveChapter(ctx, owner, chapter.ID, "Lab", domain.OutputSettings{Minutes: 30, Instructions: map[string]string{"A": "short"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.MoveLesson(ctx, owner, lesson.ID, chapter.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = a.EffectiveSettings(ctx, owner, lesson.ID)
	if got.Minutes != 30 || got.Audience != "undergraduates" || got.Instructions["A"] != "short" {
		t.Fatalf("chapter override not applied: %+v", got)
	}
	current, _ := a.Store.Get(ctx, owner, lesson.ID, "lesson")
	l, _ := domain.Decode[domain.Lesson](current.Data)
	l.Settings = domain.OutputSettings{Audience: "graduate students", Instructions: map[string]string{"A": "long"}}
	if _, err = a.SaveLesson(ctx, owner, lesson.ID, current.Version, l); err != nil {
		t.Fatal(err)
	}
	got, _ = a.EffectiveSettings(ctx, owner, lesson.ID)
	if got.Audience != "graduate students" || got.Minutes != 30 || got.Instructions["A"] != "long" {
		t.Fatalf("lesson override not applied on top: %+v", got)
	}
	// Unset fields in an override never blank an inherited value.
	l.Settings = domain.OutputSettings{}
	current, _ = a.Store.Get(ctx, owner, lesson.ID, "lesson")
	if _, err = a.SaveLesson(ctx, owner, lesson.ID, current.Version, l); err != nil {
		t.Fatal(err)
	}
	got, _ = a.EffectiveSettings(ctx, owner, lesson.ID)
	if got.Audience != "undergraduates" || got.Minutes != 30 {
		t.Fatalf("clearing an override should inherit again: %+v", got)
	}
	if _, err = a.SaveChapter(ctx, owner, chapter.ID, "Lab", domain.OutputSettings{Minutes: 999}); err == nil {
		t.Fatal("invalid override accepted")
	}
}

func TestNewCourseDefaultsToFiftyMinutes(t *testing.T) {
	a, owner, course, _ := fixture(t)
	_, c, _ := get[domain.Course](context.Background(), a, owner, course.ID, "course")
	if c.Settings.Minutes != 50 || c.Settings.Depth != "standard" {
		t.Fatalf("defaults = %+v", c.Settings)
	}
}
