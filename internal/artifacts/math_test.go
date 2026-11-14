package artifacts

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/mathtex"
	"eduforge/internal/store"
)

// fakeMath renders every formula as a small SVG naming its TeX; "\bad" fails.
type fakeMath struct{ calls int }

func (f *fakeMath) Render(_ context.Context, formulas []mathtex.Formula) ([]mathtex.Rendered, error) {
	f.calls++
	out := make([]mathtex.Rendered, len(formulas))
	for i, m := range formulas {
		if strings.Contains(m.TeX, `\bad`) {
			out[i].Error = "Undefined control sequence"
			continue
		}
		out[i] = mathtex.Rendered{SVG: `<svg data-tex="` + m.TeX + `" fill="currentColor"></svg>`, Width: 10, Height: 2, Depth: 0.5}
	}
	return out, nil
}

func (f *fakeMath) PNG(context.Context, mathtex.Rendered, string, float64) ([]byte, error) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 40, 8)))
	return b.Bytes(), nil
}

func useFakeMath(t *testing.T) *fakeMath {
	t.Helper()
	f := &fakeMath{}
	old := Math
	Math = f
	t.Cleanup(func() { Math = old })
	return f
}

func TestFormulasAreFoundInText(t *testing.T) {
	got := splitMath(`權重 $w_{uv}$ 與 \(\alpha\)，費用 $5 與 $10，字面 \$3`)
	want := []mathPiece{{Text: "權重 "}, {TeX: "w_{uv}"}, {Text: " 與 "}, {TeX: `\alpha`}, {Text: "，費用 $5 與 $10，字面 $3"}}
	if len(got) != len(want) {
		t.Fatalf("pieces = %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("piece %d = %#v, want %#v", i, got[i], want[i])
		}
	}
	if tex, ok := displayFormula(`  $$ e(f) = \prod_i c_i $$ `); !ok || tex != `e(f) = \prod_i c_i` {
		t.Fatalf("display formula = %q %v", tex, ok)
	}
	if _, ok := displayFormula("see $$x$$ here"); ok {
		t.Fatal("a formula inside a sentence is inline")
	}
}

func TestFormulasReadAsTextOnSlides(t *testing.T) {
	for tex, want := range map[string]string{
		`S^{(t+1)}(u,v) = (1-w_{uv}) S_{\mathrm{cont}}`: "S⁽ᵗ⁺¹⁾(u,v) = (1-wᵤᵥ) S_(cont)",
		`\alpha \cdot \frac{a}{b+c} \le \sqrt{x}`:       "α · a/(b+c) ≤ √(x)",
		`e(f)=\mathrm{Rel}(R)\prod_{i} \mathrm{Conf}_i`: "e(f)=Rel(R)∏ᵢ Confᵢ",
	} {
		if got := TexText(tex); got != want {
			t.Errorf("TexText(%q) = %q, want %q", tex, got, want)
		}
	}
	if got := PlainText("- 權重 $w_{uv}$ 由 **相似度** 決定"); got != "• 權重 wᵤᵥ 由 相似度 決定" {
		t.Fatalf("PlainText = %q", got)
	}
}

func TestReaderExportsRenderFormulas(t *testing.T) {
	math := useFakeMath(t)
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := domain.Artifact{Type: "A", Language: "zh-TW", Sections: []domain.Section{{Title: "方法", Body: "更新式為 $S_{t+1}$，其中 $\\bad$ 不可解析。\n$$e(f) = \\prod_i c_i$$\n- 價格 $5"}}}
	k := domain.Knowledge{Title: "t"}
	page, err := HTML(s, a, k)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{`<span class="math" style="vertical-align:-0.500ex"><svg data-tex="S_{t+1}"`, `<span class="math-display"><span class="math"`, `data-tex="e(f) = \prod_i c_i"`, `<code class="tex">$\bad$</code>`, "• 價格 $5"} {
		if !strings.Contains(html, want) {
			t.Fatalf("HTML lacks %q:\n%s", want, html)
		}
	}
	if math.calls != 1 {
		t.Fatalf("a document's formulas render in one batch: %d calls", math.calls)
	}
	doc := string(readerDocument(s, a, k))
	if strings.Count(doc, `draw:mime-type="image/svg+xml"`) != 2 || !strings.Contains(doc, `<text:p text:style-name="Formula"><draw:frame draw:style-name="Math"`) || !strings.Contains(doc, `svg:y="-0.257cm"`) {
		t.Fatalf("PDF/Word reader draws formulas as SVG on the baseline:\n%s", doc)
	}
	if !strings.Contains(doc, "$\\bad$") {
		t.Fatal("a formula that cannot be rendered shows its TeX")
	}
}

func TestSlideFormulaIsAPictureAboveThePoints(t *testing.T) {
	useFakeMath(t)
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deck := BuildDeck([]domain.Section{{Title: "Ranking", Body: "- score $e(f)$\n- more", Citations: []string{"s:p1"}, Equation: `e(f) = \prod_i c_i`}}, domain.Knowledge{}, "Paper", "en", nil, DefaultGeometry())
	slide := deck[1]
	formula, points := slide.Layout[1], slide.Layout[2]
	if formula.Kind != "formula" || formula.Tex != `e(f) = \prod_i c_i` || points.Y < formula.Y+formula.H {
		t.Fatalf("the formula sits above the points: %#v", slide.Layout)
	}
	if !strings.Contains(points.Text, "score e(f)") {
		t.Fatalf("inline TeX in points reads as text: %q", points.Text)
	}
	a := domain.Artifact{Type: "B", Language: "en", Sections: deck, Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{"B": DefaultStyle()}}}
	b, err := PPTX(s, a, domain.Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	if !strings.Contains(files["ppt/slides/slide2.xml"], `name="Formula"`) || !strings.Contains(files["ppt/slides/_rels/slide2.xml.rels"], "p2l-slide2-1.png") {
		t.Fatal("the PPTX shows the formula as a picture")
	}
	page, _ := HTML(s, a, domain.Knowledge{})
	if !strings.Contains(string(page), `class="box formula"`) {
		t.Fatal("the slide preview shows the formula")
	}
}
