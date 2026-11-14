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
	if len(prompts) != 7 {
		t.Fatalf("one prompt per knowledge section: %d", len(prompts))
	}
	for i, prompt := range prompts {
		if !strings.Contains(prompt, "Explain every equation twice.") || strings.Contains(prompt, "Put the equations on the slides.") {
			t.Fatal("each output follows its own wishes")
		}
		if !strings.Contains(prompt, "1600 to 2800 Chinese characters") || !strings.Contains(prompt, "new to the field") {
			t.Fatalf("a deep reader asks for long chapters at the reader's level:\n%s", prompt)
		}
		if !strings.Contains(prompt, "Images attached to this message are the original pages "+cite) || len(images[i]) != 1 || !filepath.IsAbs(images[i][0]) {
			t.Fatalf("the cited page goes with the prompt as an image: %v", images[i])
		}
	}
	if !strings.Contains(prompts[2], "Do not skip components") {
		t.Fatal("the method follows the lesson's order (core concepts come second, method third)")
	}
	for _, sec := range reader.Sections {
		if strings.Contains(sec.Title, "相關研究比較") || strings.Contains(sec.Title, "Related research comparison") {
			t.Fatal("no comparison with other studies in the library")
		}
	}

	deck := run("B", "")
	if len(prompts) != 3 {
		t.Fatalf("a deck is planned once, then written four slides at a time: %d calls", len(prompts))
	}
	plan := prompts[0]
	for _, want := range []string{"Follow the lesson's own structure and order", "Plan exactly 14 content slides", "Put the equations on the slides.", "Figure 1 (page 1): Figure 1: one.", "[" + cite + "]"} {
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
