package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func TestExtractTemplateFromADeck(t *testing.T) {
	ctx := context.Background()
	a, owner, _, _ := fixture(t)
	b, err := os.ReadFile("../../tests/fixtures/sample-deck.pptx")
	if err != nil {
		t.Fatal(err)
	}
	// Without a model the rules decide.
	r, err := a.ExtractTemplate(ctx, owner, "Lab talk.pptx", "zh-TW", b)
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := domain.Decode[domain.Template](r.Data)
	if !draft.Draft || draft.File != "" || draft.Extraction == nil || draft.Extraction.UsedAI || draft.Name != "Lab talk" {
		t.Fatalf("a draft without a file is kept for review: %+v", draft)
	}
	if len(draft.Extraction.Suggestion.Decorations) != 4 {
		t.Fatalf("all default decorations are suggested: %v", draft.Extraction.Suggestion.Decorations)
	}

	// With a model: its choices, validated, replace the rules.
	prompts := useFakeModel(t, a, owner, func(string) string {
		return `{"name":"青綠實驗室","description":"白底、青綠封面色帶與珊瑚色頂欄。","cover_layout":"ppt/slideLayouts/slideLayout1.xml","content_layout":"ppt/slideLayouts/slideLayout2.xml","layout_types":{"ppt/slideLayouts/slideLayout2.xml":"content"},"decorations":["d1","d2"]}`
	})
	r, err = a.ExtractTemplate(ctx, owner, "Lab talk.pptx", "zh-TW", b)
	if err != nil {
		t.Fatal(err)
	}
	draft, _ = domain.Decode[domain.Template](r.Data)
	if !draft.Extraction.UsedAI || draft.Name != "青綠實驗室" || len(draft.Extraction.Suggestion.Decorations) != 2 {
		t.Fatalf("the model's suggestion should be used: %+v", draft.Extraction.Suggestion)
	}
	if p := (*prompts)[0]; !strings.Contains(p, "Top bar") || !strings.Contains(p, "Traditional Chinese") || !strings.Contains(p, "slideLayout2.xml") {
		t.Fatal("the prompt should describe the deck's layouts and shapes")
	}

	s := draft.Extraction.Suggestion
	s.Decorations = []string{"d1"}
	s.Name = "最終模板"
	done, err := a.ConfirmExtraction(ctx, owner, r.ID, r.Version, s)
	if err != nil {
		t.Fatal(err)
	}
	tpl, _ := domain.Decode[domain.Template](done.Data)
	if tpl.Draft || tpl.Extraction != nil || tpl.File == "" || tpl.Name != "最終模板" || tpl.ContentLayout != s.ContentLayout || tpl.LayoutTypes[s.ContentLayout] != "content" {
		t.Fatalf("confirming makes a usable template: %+v", tpl)
	}
	if _, err = a.ConfirmExtraction(ctx, owner, r.ID, done.Version, s); err == nil {
		t.Fatal("a confirmed template is no longer a draft")
	}
	if spec, err := a.TemplateSpec(ctx, owner, r.ID); err != nil || spec.Colors["accent1"] != "0E7C7B" {
		t.Fatalf("the extracted template is editable with the deck's theme: %v %v", err, spec.Colors)
	}
}

func TestExtractingAFlatDeckAppliesItsMeasuredStyle(t *testing.T) {
	ctx := context.Background()
	a, owner, _, _ := fixture(t)
	b, err := os.ReadFile("../../tests/fixtures/flat-deck.pptx")
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.ExtractTemplate(ctx, owner, "Flat.pptx", "zh-TW", b)
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := domain.Decode[domain.Template](r.Data)
	s := draft.Extraction.Suggestion
	if !s.ApplyStyle || draft.Extraction.Analysis.Style == nil {
		t.Fatalf("the measured style is suggested: %+v", s)
	}
	done, err := a.ConfirmExtraction(ctx, owner, r.ID, r.Version, s)
	if err != nil {
		t.Fatal(err)
	}
	tpl, _ := domain.Decode[domain.Template](done.Data)
	if tpl.CoverLayout == tpl.ContentLayout || tpl.LayoutTypes[tpl.CoverLayout] != "cover" || tpl.LayoutTypes[tpl.ContentLayout] != "content" {
		t.Fatalf("the cover gets its own layout: %s %s %v", tpl.CoverLayout, tpl.ContentLayout, tpl.LayoutTypes)
	}
	if tpl.Accent != "2563EB" || tpl.HeadingFont != "Georgia" || tpl.Font != "Arial" {
		t.Fatalf("theme carries the measured colours and fonts: %s %s %s", tpl.Accent, tpl.HeadingFont, tpl.Font)
	}
	near := func(got []float64, want ...float64) bool {
		for i := range want {
			if len(got) != 4 || got[i] < want[i]-0.01 || got[i] > want[i]+0.01 {
				return false
			}
		}
		return true
	}
	if !near(tpl.TitleBox, 0.045, 0.083, 0.904, 0.069) || !near(tpl.BodyBox, 0.045, 0.213, 0.904, 0.684) || !near(tpl.CoverTitleBox, 0.052, 0.153, 0.907, 0.254) || !near(tpl.CoverSubtitleBox, 0.05, 0.409) {
		t.Fatalf("boxes: title %v body %v cover %v %v", tpl.TitleBox, tpl.BodyBox, tpl.CoverTitleBox, tpl.CoverSubtitleBox)
	}

	// Without the style the deck's own theme stays.
	r, _ = a.ExtractTemplate(ctx, owner, "Flat.pptx", "zh-TW", b)
	draft, _ = domain.Decode[domain.Template](r.Data)
	s = draft.Extraction.Suggestion
	s.ApplyStyle = false
	done, err = a.ConfirmExtraction(ctx, owner, r.ID, r.Version, s)
	if err != nil {
		t.Fatal(err)
	}
	tpl, _ = domain.Decode[domain.Template](done.Data)
	if tpl.Accent != "4472C4" {
		t.Fatalf("theme untouched without the style: %s", tpl.Accent)
	}
}
