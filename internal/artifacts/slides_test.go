package artifacts

import (
	"archive/zip"
	"bytes"
	"image"
	imagecolor "image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func TestSlidePointsAreShortAndAtMostFive(t *testing.T) {
	body := "**背景**\n網路監控可以找出故障裝置，但診斷故障原因仍然耗時且仰賴人工處理，需要大量專家時間與跨部門溝通才能完成。\n第二段。\n第三段說明。\n第四段說明。\n第五段說明。\n第六段說明。"
	points := SlidePoints(body, "zh-TW")
	if len(points) == 0 || len(points) > 5 {
		t.Fatalf("points = %q", points)
	}
	for _, p := range points {
		if len([]rune(p)) > zhPointRunes || strings.Contains(p, "**") {
			t.Fatalf("point too long or with markers: %q", p)
		}
	}
	if got := SlidePoints("- one\n- two\nexplanation paragraph that should not appear", "en"); len(got) != 2 || got[0] != "one" {
		t.Fatalf("existing bullets should be used as-is: %q", got)
	}
}

func TestDeckIsFigureFirstWithEditableBoxes(t *testing.T) {
	k := domain.Knowledge{Title: "user title", Units: []domain.Unit{{ID: "s:p1", SourceName: "AIDA lesson.pdf"}}, Images: []domain.Image{
		{Path: "f1", Citation: "s:p1", Caption: "Figure 1: workflow"}, {Path: "f2", Citation: "s:p1", Caption: "Figure 2: results"},
	}}
	deck := BuildDeck([]domain.Section{
		{Title: "Method", Body: "- Figure 2: results\n- more on results\n- Figure 1: the workflow", Notes: "explain", Citations: []string{"s:p1"}},
		{Title: "Limits", Body: "No figure here.", Citations: []string{"s:p9"}},
	}, k, "user title", "en", nil, DefaultGeometry())
	if len(deck) != 4 {
		t.Fatalf("expected cover, method (both figures), limits, closing: %#v", deck)
	}
	if cover := deck[0]; slideKind(cover) != "display" || cover.Layout[1].Text != "AIDA lesson" || !strings.Contains(cover.Notes, "AIDA lesson") {
		t.Fatalf("cover should show the lesson name with an opening script: %#v", cover)
	}
	method := deck[1].Layout
	if len(method) != 5 || method[1].Path != "f1" || method[3].Path != "f2" || method[3].ID == method[1].ID || deck[1].Notes != "explain" {
		t.Fatalf("two figures are two rows on one slide: %#v", method)
	}
	if method[2].Text != "• Figure 1: the workflow" || method[4].Text != "• Figure 2: results\n• more on results" {
		t.Fatalf("each figure sits beside the points that name it: %q / %q", method[2].Text, method[4].Text)
	}
	if method[2].X < method[1].X+method[1].W || method[2].Y != method[1].Y || method[4].Y != method[3].Y {
		t.Fatalf("points are beside their figure: %#v", method)
	}
	if limits := deck[2]; len(limits.Layout) != 2 || limits.Layout[1].Kind != "text" {
		t.Fatalf("slide without figure should enlarge the points: %#v", limits.Layout)
	}
	if closing := deck[3]; slideKind(closing) != "display" || closing.Layout[0].Text != "Q & A" {
		t.Fatalf("deck should end with a closing slide: %#v", closing)
	}
}

func TestDeckLayoutFollowsFigureShape(t *testing.T) {
	k := domain.Knowledge{Images: []domain.Image{{Path: "wide", Citation: "s:p1"}, {Path: "tall", Citation: "s:p2"}}}
	aspects := map[string]float64{"wide": 3, "tall": 0.6}
	deck := BuildDeck([]domain.Section{
		{Title: "Wide", Body: "- a\n- b\n- c\n- d", Citations: []string{"s:p1"}, Figures: []string{"wide"}},
		{Title: "Tall", Body: "- a", Citations: []string{"s:p2"}, Figures: []string{"tall"}},
	}, k, "t", "en", func(p string) float64 { return aspects[p] }, DefaultGeometry())
	wide, tall := deck[1].Layout, deck[2].Layout
	if wide[1].W < 0.8 || wide[2].Y < wide[1].Y+wide[1].H || strings.Count(wide[2].Text, "•") != 4 {
		t.Fatalf("wide figure should span the slide with every point below: %#v", wide)
	}
	if tall[1].W > 0.45 || tall[2].X < tall[1].X+tall[1].W {
		t.Fatalf("tall figure should take a narrow column: %#v", tall)
	}
}

func TestPPTXRendersLayoutBoxesAsShapesAndPictures(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	img.Set(1, 1, imagecolor.Black)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	path, _, err := s.Blob(buf.Bytes(), ".png")
	if err != nil {
		t.Fatal(err)
	}
	a := domain.Artifact{Type: "B", Language: "en", Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{"B": {}}}, Sections: []domain.Section{{
		Title: "T", Notes: "spoken notes",
		Layout: []domain.Element{
			{ID: "title", Kind: "text", Role: "title", X: .05, Y: .05, W: .9, H: .1, Text: "Slide title", Size: 28, Bold: true},
			{ID: "figure", Kind: "image", X: .05, Y: .2, W: .6, H: .7, Path: path},
			{ID: "points", Kind: "text", X: .7, Y: .2, W: .25, H: .7, Text: "• **key** point"},
		},
	}, {
		Title: "Cover", Notes: "**Opening**\nhello",
		Layout: []domain.Element{{ID: "title", Kind: "text", Role: "display", X: .1, Y: .3, W: .8, H: .3, Text: "Lesson", Size: 36}},
	}}}
	b, err := PPTX(s, a, domain.Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		_ = r.Close()
		files[f.Name] = string(data)
	}
	slide := files["ppt/slides/slide1.xml"]
	if !strings.Contains(slide, "Slide title") || !strings.Contains(slide, ">key point<") || !strings.Contains(slide, `<a:buChar char="•"/>`) || !strings.Contains(slide, "<p:pic>") {
		t.Fatalf("slide is missing editable boxes: %s", slide)
	}
	if _, ok := files["ppt/media/p2l-slide1-1.png"]; !ok || !strings.Contains(files["ppt/slides/_rels/slide1.xml.rels"], "p2l-slide1-1.png") {
		t.Fatal("image box media not packaged")
	}
	notes := files["ppt/notesSlides/notesSlide1.xml"]
	if !strings.Contains(notes, "spoken notes") || !strings.Contains(notes, `type="sldImg"`) {
		t.Fatal("notes page should show the slide image above the script")
	}
	if !strings.Contains(files["ppt/notesSlides/notesSlide2.xml"], `b="1"><a:solidFill><a:srgbClr val="222222"/></a:solidFill><a:latin typeface="Noto Sans CJK TC"/><a:ea typeface="Noto Sans CJK TC"/></a:rPr><a:t xml:space="preserve">Opening<`) {
		t.Fatal("notes subheadings should be bold without markers")
	}
	if !strings.Contains(files["ppt/slides/_rels/slide1.xml.rels"], "slideLayout1.xml") || !strings.Contains(files["ppt/slides/_rels/slide2.xml.rels"], "slideLayout2.xml") {
		t.Fatal("content and cover slides should use their own layouts")
	}
	if !strings.Contains(files["ppt/slideLayouts/slideLayout1.xml"], "Decoration") || strings.Contains(slide, "Decoration") {
		t.Fatal("theme decorations belong on the layout, not on the editable slide")
	}
	html, _ := HTML(s, a, domain.Knowledge{})
	if !strings.Contains(string(html), "spoken notes") || !strings.Contains(string(html), `class="slide"`) {
		t.Fatal("slide HTML should show each slide with its notes")
	}
}

func TestReaderHasCaptionsAndReferencesButNoGlossaryOrSourceIndex(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	k := domain.Knowledge{Title: "user title", Units: []domain.Unit{{ID: "s:p1", SourceName: "AIDA lesson.pdf", Text: "raw page text"}},
		Terms: []domain.Term{{Original: "Knowledge Graph", Abbreviation: "KG"}}}
	a := domain.Artifact{Type: "A", Language: "zh-TW", Check: domain.Check{Status: "draft"}, Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{}},
		Sections: []domain.Section{{Title: "方法", Body: "內容", Citations: []string{"s:p1"}, Figures: []string{}}}}
	b, err := HTML(s, a, k)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, bad := range []string{"關鍵術語", "Knowledge Graph", "來源索引", "raw page text", "草稿", "s:p1"} {
		if strings.Contains(html, bad) {
			t.Fatalf("reader still contains %q", bad)
		}
	}
	if !strings.Contains(html, `<span class="num">I.</span>方法`) || !strings.Contains(html, "講義") {
		t.Fatal("reader sections should be numbered under a titled header")
	}
	if !strings.Contains(html, "<h2>引用</h2><ol><li>AIDA lesson</li>") {
		t.Fatal("references section with the lesson name missing")
	}
	doc := string(readerDocument(s, a, k))
	if strings.Contains(doc, "Knowledge Graph") || strings.Contains(doc, "s:p1") || !strings.Contains(doc, "[1] AIDA lesson") || !strings.Contains(doc, "I.  方法") {
		t.Fatal("PDF document should list references only")
	}
}

func TestRomanAndSectionTitle(t *testing.T) {
	for n, want := range map[int]string{1: "I", 4: "IV", 9: "IX", 14: "XIV"} {
		if got := Roman(n); got != want {
			t.Errorf("Roman(%d) = %q", n, got)
		}
	}
	for in, want := range map[string]string{"一、研究動機": "研究動機", "2. Method": "Method", "III. Results": "Results", "1.2 Setup": "Setup", "GPT-4 results": "GPT-4 results", "3D scenes": "3D scenes"} {
		if got := SectionTitle(in); got != want {
			t.Errorf("SectionTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
