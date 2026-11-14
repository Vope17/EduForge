package artifacts

import (
	"context"
	"strings"
	"testing"

	"eduforge/internal/store"
)

func TestDeckAnalysisFindsDesignAndBuildsATemplate(t *testing.T) {
	file := "../../tests/fixtures/sample-deck.pptx"
	a, err := AnalyzeDeck(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Slides) != 4 || len(a.Layouts) != 2 {
		t.Fatalf("want 4 slides on 2 layouts, got %d / %d", len(a.Slides), len(a.Layouts))
	}
	byName := map[string]int{}
	for _, d := range a.Decorations {
		byName[d.Name] = len(d.Slides)
		if !d.Default {
			t.Fatalf("%s should be kept by default", d.Name)
		}
	}
	for name, slides := range map[string]int{"Cover band": 1, "Top bar": 3, "Footer": 3, "Logo": 3} {
		if byName[name] != slides {
			t.Fatalf("decoration %q on %d slides, want %d (found %v)", name, byName[name], slides, byName)
		}
	}
	if _, ok := byName["Content"]; ok {
		t.Fatal("slide-specific text is content, not design")
	}
	s := SuggestDeck(a, "Deck")
	if s.CoverLayout != "ppt/slideLayouts/slideLayout1.xml" || s.ContentLayout != "ppt/slideLayouts/slideLayout2.xml" || s.LayoutTypes[s.ContentLayout] != "content" {
		t.Fatalf("suggestion: %+v", s)
	}
	bad := s
	bad.Decorations = []string{"d99"}
	if err := CheckSuggestion(a, &bad); err == nil {
		t.Fatal("an unknown decoration should be rejected")
	}

	if a.Style != nil || s.ApplyStyle {
		t.Fatal("a deck with title placeholders keeps its layouts' design")
	}
	out, cover, err := BuildExtractedTemplate(file, s)
	if err != nil {
		t.Fatal(err)
	}
	if cover != s.CoverLayout {
		t.Fatalf("separate cover layout should be kept, got %s", cover)
	}
	parts := wellFormed(t, out)
	for name := range parts {
		if strings.HasPrefix(name, "ppt/slides/") {
			t.Fatalf("slides should be dropped, found %s", name)
		}
	}
	cover, content := parts["ppt/slideLayouts/slideLayout1.xml"], parts["ppt/slideLayouts/slideLayout2.xml"]
	if !strings.Contains(cover, `name="Cover band"`) || strings.Contains(cover, `name="Top bar"`) {
		t.Fatal("the cover band belongs to the cover layout only")
	}
	for _, want := range []string{`name="Top bar"`, `name="Footer"`, `name="Logo"`, "Vision Lab"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content layout lacks %s", want)
		}
	}
	if strings.Contains(content, "hlinkClick") || !strings.Contains(parts["ppt/slideLayouts/_rels/slideLayout2.xml.rels"], "../media/logo.png") || parts["ppt/media/logo.png"] == "" {
		t.Fatal("the logo keeps its picture through a layout relationship, without the link")
	}
	if i, j := strings.Index(content, `name="Top bar"`), strings.Index(content, "<p:ph"); i > j {
		t.Fatal("decorations go behind the placeholders")
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	path := writeTemp(t, "extracted.pptx", out)
	tpl, err := InspectTemplateLayouts(context.Background(), st, path, s.CoverLayout, s.ContentLayout)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.CoverLayout != s.CoverLayout || tpl.ContentLayout != s.ContentLayout {
		t.Fatalf("chosen layouts should be used: %s %s", tpl.CoverLayout, tpl.ContentLayout)
	}
}

func TestFlatDeckIsMeasuredFromItsSlides(t *testing.T) {
	file := "../../tests/fixtures/flat-deck.pptx"
	a, err := AnalyzeDeck(file)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, d := range a.Decorations {
		names[d.Name] = d.Default
	}
	if !names["Side bar"] || len(names) != 2 {
		t.Fatalf("only the side bar (kept) and the shared card (offered) should be candidates: %v", names)
	}
	if names["Card"] {
		t.Fatal("a card on two of five slides is content: offer it, unticked")
	}
	skipped := strings.Join(a.Skipped, "\n")
	for _, want := range []string{"MOTIVATION / DESIGN / EVALUATION", "1 / 6", "Source: figure 2"} {
		if !strings.Contains(skipped, want) {
			t.Fatalf("changing text %q should be listed as skipped:\n%s", want, skipped)
		}
	}
	st := a.Style
	if st == nil {
		t.Fatal("a flat deck should get a measured style")
	}
	near := func(got, want []float64) bool {
		if len(got) != 4 {
			return false
		}
		for i := range want {
			if got[i] < want[i]-0.01 || got[i] > want[i]+0.01 {
				return false
			}
		}
		return true
	}
	if !near(st.TitleBox, []float64{0.045, 0.083, 0.904, 0.069}) {
		t.Fatalf("title box %v", st.TitleBox)
	}
	// Below the lede, above the source line.
	if !near(st.BodyBox, []float64{0.045, 0.213, 0.904, 0.684}) {
		t.Fatalf("body box %v", st.BodyBox)
	}
	// The short name and the lesson title under it; then authors and venue.
	if !near(st.CoverTitleBox, []float64{0.052, 0.153, 0.907, 0.254}) || !near(st.CoverSubtitleBox, []float64{0.05, 0.409, 0.909, 0.117}) {
		t.Fatalf("cover boxes %v %v", st.CoverTitleBox, st.CoverSubtitleBox)
	}
	if st.Colors["accent1"] != "2563EB" || st.Colors["dk1"] != "111827" || st.Colors["dk2"] != "667085" {
		t.Fatalf("colours %v", st.Colors)
	}
	if st.HeadingFont != "Georgia" || st.BodyFont != "Arial" || st.BodyFontEA != "Noto Sans CJK TC" {
		t.Fatalf("fonts %+v", st)
	}

	s := SuggestDeck(a, "Flat")
	if s.CoverLayout != s.ContentLayout || !s.ApplyStyle {
		t.Fatalf("one layout serves both, and the style applies: %+v", s)
	}
	out, cover, err := BuildExtractedTemplate(file, s)
	if err != nil {
		t.Fatal(err)
	}
	if cover == s.ContentLayout || cover != "ppt/slideLayouts/slideLayout2.xml" {
		t.Fatalf("the cover gets its own copy of the layout, got %s", cover)
	}
	parts := wellFormed(t, out)
	if !strings.Contains(parts["ppt/slideLayouts/slideLayout2.xml"], `name="EduForge Cover"`) ||
		!strings.Contains(parts["ppt/slideMasters/slideMaster1.xml"], `r:id="rIdP2LCoverLayout"`) ||
		!strings.Contains(parts["ppt/slideMasters/_rels/slideMaster1.xml.rels"], `Target="../slideLayouts/slideLayout2.xml"`) ||
		!strings.Contains(parts["[Content_Types].xml"], `/ppt/slideLayouts/slideLayout2.xml`) ||
		parts["ppt/slideLayouts/_rels/slideLayout2.xml.rels"] == "" {
		t.Fatal("the copied cover layout is registered with the master and the package")
	}
	content := parts["ppt/slideLayouts/slideLayout1.xml"]
	if !strings.Contains(content, `name="Side bar"`) || strings.Contains(content, "MOTIVATION") || strings.Contains(content, "Card") {
		t.Fatal("only the side bar moves into the content layout")
	}
	if strings.Contains(parts["ppt/slideLayouts/slideLayout2.xml"], `name="Side bar"`) {
		t.Fatal("the cover never had the side bar")
	}
	st2, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	path := writeTemp(t, "flat.pptx", out)
	tpl, err := InspectTemplateLayouts(context.Background(), st2, path, cover, s.ContentLayout)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.CoverLayout != cover || tpl.ContentLayout != s.ContentLayout {
		t.Fatalf("layouts %s %s", tpl.CoverLayout, tpl.ContentLayout)
	}
}
