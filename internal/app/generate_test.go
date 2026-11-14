package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/providers"
	"eduforge/internal/research"
	"eduforge/internal/store"
)

// Readers and decks are written from the pages as printed (page images go
// with each prompt), follow the user's wishes, and a deck topic becomes
// several slides. Related research is never a comparison with the library.
func TestOutputsAreWrittenFromThePagesAndTheUsersWishes(t *testing.T) {
	ctx := context.Background()
	a, owner, course, lesson := fixture(t)
	p, _ := domain.Decode[domain.Lesson](lesson.Data)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	preview, _, err := a.Store.Blob(buf.Bytes(), ".png")
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	figure2, _, err := a.Store.Blob(buf.Bytes(), ".png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = set[domain.Knowledge](ctx, a, owner, p.KnowledgeID, 0, func(k *domain.Knowledge) error {
		k.Units[0].Preview = preview
		k.Images = []domain.Image{{Path: preview, Citation: k.Units[0].ID, Caption: "Figure 1: one."}, {Path: figure2, Citation: k.Units[0].ID, Caption: "Figure 2: two."}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, k, _ := get[domain.Knowledge](ctx, a, owner, p.KnowledgeID, "knowledge")
	cite := k.Units[0].ID

	useFakeModel(t, a, owner, nil)
	prompts, images := []string{}, [][]string{}
	a.Providers.Transports["codex"] = func(ctx context.Context, _ domain.Provider, prompt, _ string) (string, providers.Usage, error) {
		prompts = append(prompts, prompt)
		images = append(images, providers.ImagesFrom(ctx))
		switch {
		case strings.Contains(prompt, "Plan the content slides"):
			// The plan: six slides in the lesson's order.
			items := []string{}
			for _, title := range []string{"Intro", "Design", "Merging", "Logs", "Eval", "Limits"} {
				items = append(items, `{"title":"`+title+`","part":"§ `+title+`","pages":["`+cite+`","unknown:p9"],"figures":[],"equation":`+map[bool]string{true: "true", false: "false"}[title == "Merging"]+`,"content":"what `+title+` says"}`)
			}
			return `{"slides":[` + strings.Join(items, ",") + `]}`, providers.Usage{}, nil
		case strings.Contains(prompt, "← write now"):
			n := strings.Count(prompt, "← write now")
			slides := []string{}
			for i := 0; i < n; i++ {
				figure, equation := "[]", ""
				if i == 0 {
					figure = `["Figure 2"]`
				}
				if i == 1 {
					figure, equation = `["Fig. 9"]`, `e(f) = \\prod_i c_i`
				}
				slides = append(slides, `{"title":"Written","body":"- a $w_{uv}$ b","notes":"spoken","equation":"`+equation+`","figures":`+figure+`,"citations":["`+cite+`"]}`)
			}
			return `{"slides":[` + strings.Join(slides, ",") + `,{"title":"","body":"","citations":[]}]}`, providers.Usage{}, nil
		}
		return `{"title":"Chapter","body":"Paragraph.","citations":["` + cite + `"]}`, providers.Usage{}, nil
	}
	run := func(kind, depth string) domain.Artifact {
		t.Helper()
		settings := domain.OutputSettings{Depth: depth, Level: "beginner", Minutes: 10, Instructions: map[string]string{"A": "Explain every equation twice.", "B": "Put the equations on the slides."}}
		req, err := a.CreateRequest(ctx, owner, course.ID, domain.Request{LessonIDs: []string{lesson.ID}, Selected: []string{kind}, Languages: map[string]string{kind: "zh-TW"}, UILanguage: "zh-TW", Unlimited: true, Settings: settings})
		if err != nil {
			t.Fatal(err)
		}
		tasks, err := a.StartRequest(ctx, owner, req.ID, "zh-TW", true)
		if err != nil {
			t.Fatal(err)
		}
		prompts, images = nil, nil
		if err = a.process(ctx, owner, tasks[0].ID); err != nil {
			t.Fatal(err)
		}
		rows, _ := a.Store.List(ctx, owner, store.Filter{Kind: "artifact", Lesson: lesson.ID})
		for _, r := range rows {
			if art, _ := domain.Decode[domain.Artifact](r.Data); art.TaskID == tasks[0].ID {
				return art
			}
		}
		t.Fatal("no output")
		return domain.Artifact{}
	}

	reader := run("A", "deep")
	if len(prompts) != len(research.HandoutParts) {
		t.Fatalf("one prompt per handout part: %d", len(prompts))
	}
	for i, prompt := range prompts {
		if !strings.Contains(prompt, "Explain every equation twice.") || strings.Contains(prompt, "Put the equations on the slides.") {
			t.Fatal("each output follows its own wishes")
		}
		if !strings.Contains(prompt, "Chinese characters") || !strings.Contains(prompt, "new to the subject") || !strings.Contains(prompt, "Part of the handout") {
			t.Fatalf("a deep handout part is long and written at the students' level:\n%s", prompt)
		}
		if !strings.Contains(prompt, "Images attached to this message are the original pages "+cite) || len(images[i]) != 1 || !filepath.IsAbs(images[i][0]) {
			t.Fatalf("the cited page goes with the prompt as an image: %v", images[i])
		}
	}
	for i, part := range research.HandoutParts {
		if reader.Sections[i].Part != part.Key || reader.Sections[i].Title != research.PartTitle(part.Key, "zh-TW") {
			t.Fatalf("part %d should be %s: %+v", i, part.Key, reader.Sections[i])
		}
	}
	if !strings.Contains(prompts[5], "NO answers") {
		t.Fatalf("the practice problems are written without answers:\n%s", prompts[5])
	}

	deck := run("B", "")
	if len(prompts) != 3 {
		t.Fatalf("a deck is planned once, then written four slides at a time: %d calls", len(prompts))
	}
	plan := prompts[0]
	for _, want := range []string{"Follow the order of the material", "Plan exactly 6 content slides", "Put the equations on the slides.", "Figure 1 (page 1): Figure 1: one.", "[" + cite + "]"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("the plan prompt lacks %q:\n%s", want, plan)
		}
	}
	if !strings.Contains(prompts[1], "Write slides 1 to 4") || !strings.Contains(prompts[2], "Write slides 5 to 6") || !strings.Contains(prompts[1], "[§ Merging] Merging  ← write now") || !strings.Contains(prompts[2], "6. [§ Limits] Limits") {
		t.Fatalf("batches follow the plan in order:\n%s", prompts[1])
	}
	if !strings.Contains(prompts[1], "Equations are LaTeX") || !strings.Contains(prompts[1], "what Merging says It presents a key equation.") || len(images[1]) != 1 {
		t.Fatalf("a batch carries its planned content, LaTeX rules and page images: %v\n%s", images[1], prompts[1])
	}
	if len(deck.Sections) != 2+6 {
		t.Fatalf("cover, the six planned slides and the closing: %d", len(deck.Sections))
	}
	if f := deck.Sections[1].Figures; len(f) != 1 || f[0] != figure2 {
		t.Fatalf("a slide shows the figure the model chose for it: %v", f)
	}
	if f := deck.Sections[2].Figures; len(f) != 1 || f[0] != preview {
		t.Fatalf("an unknown label falls back to the cited page's next figure: %v", f)
	}
	formula := deck.Sections[2].Layout[1]
	if formula.Kind != "formula" || formula.Tex != `e(f) = \prod_i c_i` {
		t.Fatalf("a slide's key equation is a formula box above its points: %#v", deck.Sections[2].Layout)
	}
	if !strings.Contains(deck.Sections[1].Body, "wᵤᵥ") {
		t.Fatalf("inline TeX in points reads as text: %q", deck.Sections[1].Body)
	}
	for _, sec := range deck.Sections {
		if sec.Slides != nil {
			t.Fatal("slides are flattened into the deck")
		}
	}
}

// A later lesson treats what earlier lessons of the course taught as
// background, in its handout and in its slides; the first lesson has none.
func TestEarlierLessonsAreBackgroundInHandoutsAndSlides(t *testing.T) {
	ctx := context.Background()
	a, owner, course, first := fixture(t)
	p, _ := domain.Decode[domain.Lesson](first.Data)
	if _, err := set[domain.Knowledge](ctx, a, owner, p.KnowledgeID, 0, func(k *domain.Knowledge) error {
		k.Terms = []domain.Term{{Original: "eigenvector", Citations: []string{k.Units[0].ID}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	second := newLesson(t, a, owner, course.ID, "Second lesson")
	second = lessonWithPages(t, a, owner, second, 2)
	sp, _ := domain.Decode[domain.Lesson](second.Data)
	if _, err := set[domain.Knowledge](ctx, a, owner, sp.KnowledgeID, 0, func(k *domain.Knowledge) error {
		k.Confirmed = true
		k.Claims = []domain.Claim{{Section: 2, Text: "A concept", Citations: []string{k.Units[0].ID}, Verified: true, Kind: "ai_summary"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cite := lessonUnit(t, a, owner, second)
	prompts := useFakeModel(t, a, owner, func(string) string {
		return `{"title":"T","body":"B","citations":["` + cite + `"]}`
	})
	for _, target := range []struct {
		lesson domain.Record
		want   bool
	}{{second, true}, {first, false}} {
		*prompts = nil
		req, err := a.CreateRequest(ctx, owner, course.ID, domain.Request{LessonIDs: []string{target.lesson.ID}, Selected: []string{"A"}, Languages: map[string]string{"A": "en"}, UILanguage: "en", Unlimited: true, Settings: domain.OutputSettings{Audience: "first-year students"}})
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
		if len(*prompts) == 0 {
			t.Fatal("no prompts")
		}
		for _, prompt := range *prompts {
			has := strings.Contains(prompt, "eigenvector") && strings.Contains(prompt, "do not explain them again")
			if has != target.want {
				t.Fatalf("lesson %q: background present = %v, want %v\n%s", target.lesson.ID, has, target.want, prompt)
			}
			if !strings.Contains(prompt, "The class: first-year students.") {
				t.Fatalf("the audience is stated in the teacher's words:\n%s", prompt)
			}
		}
	}
}

func lessonUnit(t *testing.T, a *App, owner string, lesson domain.Record) string {
	t.Helper()
	_, k := knowledgeOf(t, a, owner, mustGet(t, a, owner, lesson.ID))
	return k.Units[0].ID
}
