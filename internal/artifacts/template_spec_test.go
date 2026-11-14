package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// wellFormed checks every XML part of a package parses.
func wellFormed(t *testing.T, b []byte) map[string]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if z.File[0].Name != "[Content_Types].xml" {
		t.Fatal("[Content_Types].xml must come first")
	}
	parts := map[string]string{}
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		parts[f.Name] = string(data)
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			d := xml.NewDecoder(bytes.NewReader(data))
			for {
				if _, err := d.Token(); err == io.EOF {
					break
				} else if err != nil {
					t.Fatalf("%s is not well-formed: %v", f.Name, err)
				}
			}
		}
	}
	return parts
}

func writeTemp(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSlideTemplateSpecRoundTrip(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	file := "../../tests/fixtures/template.potx"
	tpl, err := InspectTemplate(context.Background(), s, file)
	if err != nil {
		t.Fatal(err)
	}
	tpl.Kind = "pptx"
	spec, err := ReadTemplateSpec(file, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Colors["accent1"] == "" || len(spec.TitleBox) != 4 {
		t.Fatalf("spec should carry theme colours and boxes: %+v", spec)
	}
	untouched := spec.Colors["dk2"]
	spec.Colors["accent1"] = "#123abc"
	spec.HeadingFont, spec.HeadingFontEA = "Georgia", "Noto Serif CJK TC"
	spec.TitleBox = []float64{0.1, 0.05, 0.7, 0.15}
	spec.CoverTitleBox = []float64{0.1, 0.4, 0.8, 0.2}
	out, err := WriteTemplateSpec(file, tpl, spec)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, out)
	next := writeTemp(t, "edited.potx", out)
	tpl2, err := InspectTemplate(context.Background(), s, next)
	if err != nil {
		t.Fatal(err)
	}
	tpl2.Kind = "pptx"
	got, err := ReadTemplateSpec(next, tpl2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Colors["accent1"] != "123ABC" || got.Colors["dk2"] != untouched {
		t.Fatalf("only the edited colour should change: %v", got.Colors)
	}
	if got.HeadingFont != "Georgia" || got.HeadingFontEA != "Noto Serif CJK TC" {
		t.Fatalf("heading fonts not written: %q %q", got.HeadingFont, got.HeadingFontEA)
	}
	for i, want := range []float64{0.1, 0.05, 0.7, 0.15} {
		if math.Abs(tpl2.TitleBox[i]-want) > 0.001 {
			t.Fatalf("title placeholder not moved: %v", tpl2.TitleBox)
		}
	}
	if math.Abs(tpl2.CoverTitleBox[1]-0.4) > 0.001 {
		t.Fatalf("cover title not moved: %v", tpl2.CoverTitleBox)
	}
	bad := spec
	bad.TitleBox = []float64{0.5, 0.5, 0.8, 0.2} // runs off the slide
	if _, err = WriteTemplateSpec(file, tpl, bad); err == nil {
		t.Fatal("a box outside the slide was accepted")
	}
}

func TestWordTemplateSpecRoundTrip(t *testing.T) {
	file := "../../tests/fixtures/reader-template.dotx"
	tpl := domain.Template{Kind: "docx"}
	spec, err := ReadTemplateSpec(file, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Margins == nil || spec.Header == nil || !strings.Contains(spec.Header.Text, "EduForge fixture header") || !spec.Header.Editable {
		t.Fatalf("margins and header should be read: %+v %+v", spec.Margins, spec.Header)
	}
	if spec.Footer == nil || spec.Footer.Text != "" || !spec.Footer.Editable {
		t.Fatalf("a missing footer is empty and can be created: %+v", spec.Footer)
	}
	normal := spec.Styles["normal"]
	h1 := spec.Styles["h1"]
	h1.Size, h1.Color, h1.Bold, h1.FontEA, h1.Before = 18, "#aa0000", !h1.Bold, "標楷體", 24
	spec.Styles["h1"] = h1
	caption := spec.Styles["caption"]
	caption.Size = 8.5
	spec.Styles["caption"] = caption
	spec.Margins.Left = 30
	spec.Header.Text = "實驗室名稱 & Lab"
	spec.Footer.Text = "第一行\n第二行"
	out, err := WriteTemplateSpec(file, tpl, spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := wellFormed(t, out)
	next := writeTemp(t, "edited.dotx", out)
	got, err := ReadTemplateSpec(next, tpl)
	if err != nil {
		t.Fatal(err)
	}
	if g := got.Styles["h1"]; g.Size != 18 || g.Color != "AA0000" || g.Bold != h1.Bold || g.FontEA != "標楷體" || g.Before != 24 {
		t.Fatalf("heading 1 not written: %+v", g)
	}
	if got.Styles["caption"].Size != 8.5 || got.Styles["normal"] != normal {
		t.Fatalf("caption should change and normal stay: %+v %+v", got.Styles["caption"], got.Styles["normal"])
	}
	if math.Abs(got.Margins.Left-30) > 0.1 || math.Abs(got.Margins.Top-spec.Margins.Top) > 0.1 {
		t.Fatalf("margins: %+v", got.Margins)
	}
	if got.Header.Text != "實驗室名稱 & Lab" || got.Footer.Text != "第一行\n第二行" {
		t.Fatalf("header/footer: %q %q", got.Header.Text, got.Footer.Text)
	}
	// Elements stay in schema order inside the heading's run properties.
	def := ""
	for _, d := range wordStylePattern.FindAllString(parts["word/styles.xml"], -1) {
		if strings.Contains(d, `w:val="heading 1"`) {
			def = d
		}
	}
	rPr := child(def, "rPr")
	if i, j := strings.Index(rPr, "<w:rFonts"), strings.Index(rPr, "<w:sz "); i < 0 || j < 0 || i > j || strings.Index(rPr, "<w:color") > j {
		t.Fatalf("run properties out of order: %s", rPr)
	}
	if !strings.Contains(parts["[Content_Types].xml"], "p2l-footer.xml") || !strings.Contains(parts["word/_rels/document.xml.rels"], "p2l-footer.xml") {
		t.Fatal("a created footer needs a content type and relationship")
	}
	// The reader is still built on the edited template.
	doc, err := buildWordDocument(next, []wordBlock{{Kind: "h1", Text: "I. 測試"}, {Kind: "para", Text: "內文"}})
	if err != nil {
		t.Fatal(err)
	}
	built := wellFormed(t, doc)
	if !strings.Contains(built["word/document.xml"], "footerReference") {
		t.Fatal("the reader should keep the new footer")
	}
}
