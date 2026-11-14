package documents

import (
	"fmt"
	"strings"
	"testing"

	"eduforge/internal/domain"
)

func TestFigureRegionStopsAtBodyTextAndHeader(t *testing.T) {
	page := layoutPage{width: 612, height: 792, blocks: []layoutBlock{
		{x0: 54, y0: 40, x1: 558, y1: 50, lines: 1, lineHeight: 8, text: "SIGCOMM '26, Denver"},
		{x0: 54, y0: 80, x1: 296, y1: 200, lines: 12, lineHeight: 10, text: "Body paragraph text"},
		{x0: 80, y0: 230, x1: 200, y1: 270, lines: 4, lineHeight: 6, text: "label inside the figure"},
		{x0: 54, y0: 320, x1: 296, y1: 345, lines: 2, lineHeight: 9, text: "Figure 4: The impact of redundancy."},
		{x0: 318, y0: 260, x1: 560, y1: 300, lines: 4, lineHeight: 9, text: "Figure 6: Quality advantage"},
	}}
	regions := figureRegions(page)
	if len(regions) != 2 {
		t.Fatalf("regions = %#v", regions)
	}
	left := regions[0]
	if left.top < 200 || left.bottom > 320 || left.right > 306 {
		t.Fatalf("left-column figure should sit between body text and caption: %#v", left)
	}
	if regions[1].top < 50 || regions[1].left < 306 {
		t.Fatalf("right-column figure should start below the running header: %#v", regions[1])
	}
}

func TestFormulaReviewNeedsMathNotAnyEqualsSign(t *testing.T) {
	if needsFormulaReview("Error code ------> DIAG TEST, value=3 in the log") {
		t.Fatal("plain '=' flagged as a formula")
	}
	if !needsFormulaReview("L = sum of losses   (3)") || !needsFormulaReview("∑ x_i") || !needsFormulaReview("(3)\n(Wrc )𝑀 𝑆 = min 𝑎¯ (𝐿𝑈𝑀 )") {
		t.Fatal("equation not flagged")
	}
	if needsFormulaReview("We merge 𝑇 3 and 𝑇 2 despite low similarity, as shown in (2) of the list.") {
		t.Fatal("inline variables flagged as a display equation")
	}
}

func TestOutdatedParseResultsAreParsedAgain(t *testing.T) {
	if !NeedsParse(domain.Source{}) {
		t.Fatal("unparsed source skipped")
	}
	if !NeedsParse(domain.Source{Parsed: &domain.Parsed{}}) {
		t.Fatal("result from the old parser was reused")
	}
	if NeedsParse(domain.Source{Parsed: &domain.Parsed{ParserVersion: ParserVersion}}) {
		t.Fatal("current result parsed again")
	}
}

func layoutXML(lines ...string) []byte {
	return []byte(`<page width="612" height="792"><flow><block xMin="0" yMin="10" xMax="600" yMax="40">` + strings.Join(lines, "") + `</block></flow></page>`)
}

func word(x0, x1 float64, text string) string {
	return fmt.Sprintf(`<word xMin="%.3f" yMin="1" xMax="%.3f" yMax="2">%s</word>`, x0, x1, text)
}

func line(words ...string) string {
	return `<line xMin="0" yMin="1" xMax="600" yMax="2">` + strings.Join(words, "") + `</line>`
}

func TestSideBySideCaptionsReadPerColumn(t *testing.T) {
	// Figure 6 and 7 captions of the AIDA lesson, as pdftotext -bbox-layout groups them.
	b := layoutXML(
		line(word(317.955, 344.4, "Figure"), word(347.7, 354.8, "6:"), word(358.1, 389.2, "Quality"), word(392.5, 434.5, "advantage"),
			word(440.643, 466.1, "Figure"), word(467.6, 474.4, "7:"), word(475.9, 521.1, "Superiority"), word(522.6, 530.9, "of"), word(532.4, 557.6, "multi-")),
		line(word(317.955, 326.2, "of"), word(327.8, 367.1, "reasoning"), word(368.6, 394.5, "chains"), word(396.1, 416.0, "from"), word(417.6, 436.1, "$ne-"),
			word(440.643, 456.9, "step"), word(458.8, 498.1, "reasoning"), word(499.9, 512.0, "for"), word(513.8, 532.8, "RCA"), word(534.7, 557.6, "infer-")),
		line(word(440.643, 461.1, "ence.")),
		line(word(317.955, 341.5, "tuned"), word(343.8, 371.4, "model.")),
	)
	got := RepairLigatures(parseLayout(b)[0].blocks[0].text)
	want := "Figure 6: Quality advantage of reasoning chains from fine-tuned model. Figure 7: Superiority of multi-step reasoning for RCA inference."
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestCaptionMentioningAnotherFigureStaysWhole(t *testing.T) {
	b := layoutXML(
		line(word(50, 80, "Figure"), word(82, 90, "3:"), word(92, 120, "Same"), word(122, 135, "as"), word(140, 170, "Figure"), word(172, 180, "2"), word(182, 200, "with")),
		line(word(50, 80, "noisy"), word(82, 120, "inputs"), word(125, 160, "removed.")),
	)
	if got := parseLayout(b)[0].blocks[0].text; got != "Figure 3: Same as Figure 2 with noisy inputs removed." {
		t.Fatalf("got %q", got)
	}
}
