package artifacts

import (
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func TestMarkdownSubsetIsRenderedNotPrinted(t *testing.T) {
	body := "**研究動機**\n網路 **監控** 與 <script>\n- 第一點\n## 方法"
	got := bodyHTML(body)
	for _, want := range []string{`<strong class="sub">研究動機</strong>`, "網路 <strong>監控</strong> 與 &lt;script&gt;", "• 第一點", `<strong class="sub">方法</strong>`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "**") || strings.Contains(PlainText(body), "**") || strings.Contains(PlainText(body), "##") {
		t.Fatal("markdown markers leaked into output")
	}
	if PlainText("a * b * c = 2") != "a * b * c = 2" {
		t.Fatal("arithmetic asterisks were treated as emphasis")
	}
}

func TestFiguresAppearOnceNextToCitingSections(t *testing.T) {
	k := domain.Knowledge{Images: []domain.Image{{Path: "f1", Citation: "s:p1"}, {Path: "f2", Citation: "s:p2"}, {Path: "f3", Citation: "s:p2"}}}
	a := domain.Artifact{Sections: []domain.Section{
		{Citations: []string{"s:p2"}},
		{Citations: []string{"s:p1", "s:p2"}},
		{Image: "f3", Citations: []string{"s:p9"}},
	}}
	got := SectionFigures(a, k)
	paths := func(imgs []domain.Image) string {
		out := []string{}
		for _, i := range imgs {
			out = append(out, i.Path)
		}
		return strings.Join(out, ",")
	}
	if paths(got[0]) != "f2,f3" || paths(got[1]) != "f1" || paths(got[2]) != "" {
		t.Fatalf("unexpected assignment: %q %q %q", paths(got[0]), paths(got[1]), paths(got[2]))
	}
}

func TestPaperNamesPreferDocumentTitle(t *testing.T) {
	k := domain.Knowledge{Title: "test_title", Units: []domain.Unit{
		{ID: "a:p1", SourceName: "AIDA DeviceFailures.pdf", SourceTitle: "AIDA: Device Failures"},
		{ID: "a:p2", SourceName: "AIDA DeviceFailures.pdf", SourceTitle: "AIDA: Device Failures"},
		{ID: "b:p1", SourceName: "second paper.pdf"},
	}}
	if got := DocumentTitle(k); got != "AIDA: Device Failures / second paper" {
		t.Fatalf("DocumentTitle = %q", got)
	}
	if got := DocumentTitle(domain.Knowledge{Title: "test_title"}); got != "test_title" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestFiguresFollowTheParagraphAboutThem(t *testing.T) {
	body := "**Overview**\nAIDA has three stages.\n\nThe pipeline is shown in Figure 9.\n- step one\n- step two\n\nResults come last."
	fig9 := domain.Image{Path: "a.png", Caption: "Figure 9: System architecture."}
	other := domain.Image{Path: "b.png", Caption: "Table 2: Results."}
	placed := PlaceFigures(body, []domain.Image{other, fig9})
	at := map[string]int{}
	for i, figs := range placed {
		for _, f := range figs {
			at[f.Path] = i
		}
	}
	if at["a.png"] != 5 {
		t.Fatalf("a figure the text mentions follows that paragraph (after its list): %v", at)
	}
	if _, ok := at["b.png"]; !ok || at["b.png"] == 0 {
		t.Fatalf("other figures are spread between paragraphs, never after a subheading: %v", at)
	}
	table := PlaceFigures("Intro.\n\nAs Table 2 shows, it wins.\n\nEnd.", []domain.Image{other})
	if len(table[2]) != 1 {
		t.Fatalf("tables are placed under their mention too: %v", table)
	}
	if got := PlaceFigures("one line", []domain.Image{other, fig9}); len(got[0]) != 2 {
		t.Fatal("a one-paragraph body keeps every figure")
	}
}

func TestReaderSectionsShowTheFiguresTheirTextMentions(t *testing.T) {
	k := domain.Knowledge{Images: []domain.Image{
		{Path: "f1.png", Citation: "p1", Caption: "Figure 1: Workflow."},
		{Path: "f10.png", Citation: "p1", Caption: "Figure 10: Merging."},
		{Path: "f20.png", Citation: "p9", Caption: "Figure 20: Errors."},
		{Path: "t2.png", Citation: "p9", Caption: "Table 2: Results."},
		{Path: "x.png", Citation: "p5", Caption: "Figure 7: Unmentioned."},
	}}
	a := domain.Artifact{Sections: []domain.Section{
		{Body: "An overview: Figure 1, Figure 10 and Figure 20.", Citations: []string{"p2"}},
		{Body: "Figure 10 shows merging; compare Figure 1.", Citations: []string{"p1"}},
		{Body: "Errors (圖 20) and Table 2; Figure 20 again.", Citations: []string{"p3"}},
		{Body: "No figure mentioned.", Citations: []string{"p5", "p1"}},
	}}
	got := MentionedFigures(a, k)
	want := [][]string{{}, {"f10.png", "f1.png"}, {"f20.png", "t2.png"}, {"x.png"}}
	// Both cite the page and mention Figure 1 once: the focused section wins.
	tie := MentionedFigures(domain.Artifact{Sections: []domain.Section{
		{Body: "Figure 1, Figure 10, Figure 20.", Citations: []string{"p1"}},
		{Body: "Figure 1 in detail.", Citations: []string{"p1"}},
	}}, k)
	if strings.Join(tie[1], ",") != "f1.png" {
		t.Fatalf("on a tie the focused section shows the figure: %v", tie)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("section %d shows %v, want %v (each figure in the section that cites its page or mentions it most, in mention order; unmentioned sections use cited pages)", i, got[i], want[i])
		}
	}
}

func TestASectionMentioningUncaptionedFiguresStillShowsOne(t *testing.T) {
	k := domain.Knowledge{Images: []domain.Image{{Path: "plain.png", Citation: "p1"}}}
	a := domain.Artifact{Sections: []domain.Section{{Body: "Figure 1 shows the regions.", Citations: []string{"p1"}}}}
	if got := MentionedFigures(a, k); len(got[0]) != 1 || got[0][0] != "plain.png" {
		t.Fatalf("a figure without a numbered caption comes from the cited page: %v", got)
	}
}
