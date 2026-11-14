package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// writeWordTemplate builds a small .dotx: Dutch style IDs (as Word writes
// them in a Dutch template), a header, a sample body and no numbering part.
func writeWordTemplate(t *testing.T, dir string) string {
	t.Helper()
	w := `xmlns:w="` + nsW + `" xmlns:r="` + nsR + `"`
	style := func(id, name, extra string) string {
		return `<w:style w:type="paragraph" w:styleId="` + id + `"><w:name w:val="` + name + `"/>` + extra + `</w:style>`
	}
	parts := map[string]string{
		"[Content_Types].xml":          `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/></Types>`,
		"_rels/.rels":                  rels([3]string{"rId1", "officeDocument", "word/document.xml"}),
		"word/document.xml":            `<w:document ` + w + `><w:body><w:p><w:r><w:t>SAMPLE TEMPLATE TEXT</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId7"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`,
		"word/_rels/document.xml.rels": rels([3]string{"rId1", "styles", "styles.xml"}, [3]string{"rId7", "header", "header1.xml"}),
		"word/styles.xml": `<w:styles ` + w + `><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Georgia" w:hAnsi="Georgia"/></w:rPr></w:rPrDefault></w:docDefaults>` +
			style("Standaard", "Normal", "") + style("Kop1", "heading 1", `<w:rPr><w:color w:val="C00000"/></w:rPr>`) + style("Titel", "Title", "") + `</w:styles>`,
		"word/header1.xml": `<w:hdr ` + w + `><w:p><w:r><w:t>Lab header</w:t></w:r></w:p></w:hdr>`,
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range parts {
		f, _ := z.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = z.Close()
	file := filepath.Join(dir, "report.dotx")
	if err := os.WriteFile(file, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestReaderIsWrittenOnTheWordTemplate(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := writeWordTemplate(t, t.TempDir())
	tpl, err := InspectWordTemplate(context.Background(), s, file)
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Kind != "docx" || tpl.Font != "Georgia" || tpl.TitleColor != "C00000" {
		t.Fatalf("template look = %+v", tpl)
	}
	raw, _ := os.ReadFile(file)
	rel, _, _ := s.Blob(raw, ".dotx")
	tpl.File = rel
	img := image.NewRGBA(image.Rect(0, 0, 300, 150))
	var pic bytes.Buffer
	_ = png.Encode(&pic, img)
	figure, _, _ := s.Blob(pic.Bytes(), ".png")
	a := domain.Artifact{Type: "A", Language: "zh-TW", Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{"A": TemplateStyle(tpl)}},
		Sections: []domain.Section{{Title: "一、研究動機", Body: "**背景**\n監控找得到故障，**原因**仍需人工（Figure 1）。\n\n後續段落。\n- 第一點\n- 第二點", Figures: []string{figure}}}}
	k := domain.Knowledge{Title: "user title", Units: []domain.Unit{{ID: "s:p1", SourceTitle: "AIDA"}}, Images: []domain.Image{{Path: figure, Caption: "Figure 1: Workflow"}}}
	if !HasWordTemplate(a) {
		t.Fatal("artifact should carry the Word template")
	}
	b, err := DOCX(context.Background(), s, a, k)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	doc := files["word/document.xml"]
	for _, want := range []string{`<w:pStyle w:val="Titel"/>`, ">AIDA<", `<w:pStyle w:val="Kop1"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="0"/></w:numPr>`, "I.  研究動機", `<w:b/><w:bCs/></w:rPr><w:t xml:space="preserve">原因`, `<w:numId w:val="1"/>`, `r:embed="rIdP2LFigure1"`, "Figure 1: Workflow", `<w:headerReference w:type="default" r:id="rId7"/>`, "[1] AIDA"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document lacks %q:\n%s", want, doc)
		}
	}
	if fig, next := strings.Index(doc, `r:embed="rIdP2LFigure1"`), strings.Index(doc, "後續段落"); fig > next || fig < strings.Index(doc, "仍需人工") {
		t.Fatal("the figure sits right under the paragraph that mentions it")
	}
	if strings.Contains(doc, "SAMPLE TEMPLATE TEXT") {
		t.Fatal("the template's own body text should be replaced")
	}
	// Missing styles (heading 2, caption, list, subtitle) are added; numbering is created.
	if !strings.Contains(files["word/styles.xml"], `w:styleId="P2LH2"`) || !strings.Contains(files["word/styles.xml"], `w:styleId="P2LCaption"`) {
		t.Fatal("missing styles should be added")
	}
	if !strings.Contains(files["word/numbering.xml"], `<w:lvlText w:val="•"/>`) || !strings.Contains(files["word/_rels/document.xml.rels"], "numbering.xml") || !strings.Contains(files["[Content_Types].xml"], "/word/numbering.xml") {
		t.Fatal("bullet numbering should be added with its relationship and content type")
	}
	if _, ok := files["word/media/p2l-figure1.png"]; !ok || !strings.Contains(files["word/_rels/document.xml.rels"], "media/p2l-figure1.png") {
		t.Fatal("figure should be embedded")
	}
	if strings.Contains(files["[Content_Types].xml"], "template.main") || !strings.Contains(files["[Content_Types].xml"], `Extension="png"`) {
		t.Fatal("a .dotx template should become a document with picture types")
	}
}
