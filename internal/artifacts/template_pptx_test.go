package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// writeTemplate builds a small PowerPoint template: a dark master with a
// centred title style, a title layout, a title-only layout with a slide
// number, and one sample slide with a picture that must not survive.
func writeTemplate(t *testing.T, dir string) string {
	t.Helper()
	ns := `xmlns:a="` + nsA + `" xmlns:r="` + nsR + `" xmlns:p="` + nsP + `"`
	sp := func(typ, idx string, x, y, w, h int) string {
		attrs := ""
		if typ != "" {
			attrs += ` type="` + typ + `"`
		}
		if idx != "" {
			attrs += ` idx="` + idx + `"`
		}
		geom := ""
		if w > 0 {
			geom = `<a:xfrm><a:off x="` + itoa(x) + `" y="` + itoa(y) + `"/><a:ext cx="` + itoa(w) + `" cy="` + itoa(h) + `"/></a:xfrm>`
		}
		return `<p:sp><p:nvSpPr><p:cNvPr id="2" name="ph"/><p:cNvSpPr/><p:nvPr><p:ph` + attrs + `/></p:nvPr></p:nvSpPr><p:spPr>` + geom + `</p:spPr></p:sp>`
	}
	parts := map[string]string{
		"[Content_Types].xml":                          `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="jpeg" ContentType="image/jpeg"/><Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.template.main+xml"/><Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/><Override ContentType="application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml" PartName="/ppt/notesSlides/notesSlide7.xml"/></Types>`,
		"_rels/.rels":                                  rels([3]string{"rId1", "officeDocument", "ppt/presentation.xml"}, [3]string{"rId2", "metadata/thumbnail", "docProps/thumbnail.jpeg"}),
		"docProps/thumbnail.jpeg":                      "thumb",
		"ppt/presentation.xml":                         `<p:presentation ` + ns + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst><p:sldId id="256" r:id="rId9"/></p:sldIdLst><p:sldSz cx="9144000" cy="5143500"/><p:notesSz cx="6858000" cy="9144000"/><p:extLst><p:ext uri="{521415D9-36F7-43E2-AB2F-B90AF26B5E84}"><p14:sectionLst xmlns:p14="x"><p14:section name="s"><p14:sldIdLst><p14:sldId id="256"/></p14:sldIdLst></p14:section></p14:sectionLst></p:ext></p:extLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels":              rels([3]string{"rId1", "slideMaster", "slideMasters/slideMaster1.xml"}, [3]string{"rId9", "slide", "slides/slide1.xml"}, [3]string{"rId3", "notesMaster", "notesMasters/notesMaster1.xml"}),
		"ppt/slideMasters/slideMaster1.xml":            `<p:sldMaster ` + ns + `><p:cSld><p:spTree>` + group + sp("title", "", 457200, 205740, 8229600, 857250) + sp("body", "1", 457200, 1200150, 8229600, 3394472) + `</p:spTree></p:cSld><p:clrMap bg1="dk1" tx1="lt1" bg2="dk2" tx2="lt2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/><p:sldLayoutId id="2147483650" r:id="rId2"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle><a:lvl1pPr algn="ctr"><a:defRPr sz="4400"><a:solidFill><a:srgbClr val="FFFF00"/></a:solidFill></a:defRPr></a:lvl1pPr></p:titleStyle><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": rels([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, [3]string{"rId2", "slideLayout", "../slideLayouts/slideLayout2.xml"}, [3]string{"rId3", "theme", "../theme/theme1.xml"}),
		"ppt/slideLayouts/slideLayout1.xml":            `<p:sldLayout ` + ns + ` type="title"><p:cSld name="Title Slide"><p:spTree>` + group + sp("ctrTitle", "", 685800, 1597819, 7772400, 1102519) + sp("subTitle", "1", 1371600, 2914650, 6400800, 1314450) + `</p:spTree></p:cSld></p:sldLayout>`,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": rels([3]string{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"}),
		"ppt/slideLayouts/slideLayout2.xml":            `<p:sldLayout ` + ns + ` type="titleOnly"><p:cSld name="Title Only"><p:spTree>` + group + sp("title", "", 0, 0, 0, 0) + sp("sldNum", "12", 6553200, 4767263, 2133600, 273844) + `</p:spTree></p:cSld></p:sldLayout>`,
		"ppt/slideLayouts/_rels/slideLayout2.xml.rels": rels([3]string{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"}),
		"ppt/theme/theme1.xml":                         `<a:theme xmlns:a="` + nsA + `"><a:themeElements><a:clrScheme name="c"><a:dk1><a:srgbClr val="0A1A3A"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="111111"/></a:dk2><a:lt2><a:srgbClr val="EEEEEE"/></a:lt2><a:accent1><a:srgbClr val="FA4616"/></a:accent1></a:clrScheme><a:fontScheme name="f"><a:majorFont><a:latin typeface="Georgia"/></a:majorFont><a:minorFont><a:latin typeface="Verdana"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`,
		"ppt/notesMasters/notesMaster1.xml":            `<p:notesMaster ` + ns + `><p:cSld><p:spTree>` + group + `</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/></p:notesMaster>`,
		"ppt/notesMasters/_rels/notesMaster1.xml.rels": rels([3]string{"rId1", "theme", "../theme/theme1.xml"}),
		"ppt/slides/slide1.xml":                        `<p:sld ` + ns + `><p:cSld><p:spTree>` + group + `<p:sp><p:txBody><a:p><a:r><a:t>SAMPLE SLIDE TEXT</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels":             rels([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout2.xml"}, [3]string{"rId2", "image", "../media/sample.jpeg"}),
		"ppt/media/sample.jpeg":                        "not really a jpeg",
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range parts {
		w, _ := z.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = z.Close()
	file := filepath.Join(dir, "theme.potx")
	if err := os.WriteFile(file, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func itoa(n int) string { return strconvString(n) }

func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
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
	return files
}

func TestInspectTemplateReadsLayoutsGeometryAndColours(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tpl, err := InspectTemplate(context.Background(), s, writeTemplate(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if tpl.CoverLayout != "ppt/slideLayouts/slideLayout1.xml" || tpl.ContentLayout != "ppt/slideLayouts/slideLayout2.xml" {
		t.Fatalf("layouts = %q, %q", tpl.CoverLayout, tpl.ContentLayout)
	}
	// The title-only layout inherits the master's title position.
	if tpl.TitleBox[0] != 0.05 || tpl.TitleBox[1] != 0.04 || tpl.TitleAlign != "center" || tpl.CoverAlign != "center" {
		t.Fatalf("title geometry = %v align %q/%q", tpl.TitleBox, tpl.TitleAlign, tpl.CoverAlign)
	}
	if tpl.CoverPlaceholder != "ctrTitle" || tpl.CoverSubtitleIdx != "1" || tpl.ContentNumberIdx != "12" {
		t.Fatalf("placeholders = %q %q %q", tpl.CoverPlaceholder, tpl.CoverSubtitleIdx, tpl.ContentNumberIdx)
	}
	// The colour map swaps dark and light: dark background, light text, yellow titles.
	if tpl.Background != "0A1A3A" || tpl.Foreground != "FFFFFF" || tpl.TitleColor != "FFFF00" || tpl.Accent != "FA4616" || tpl.Font != "Verdana" || tpl.Ratio != "16:9" {
		t.Fatalf("colours = %+v", tpl)
	}
}

func TestDeckIsBuiltOnTheImportedTemplate(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	raw, _ := os.ReadFile(writeTemplate(t, dir))
	rel, _, err := s.Blob(raw, ".potx")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := s.Path(rel)
	tpl, err := InspectTemplate(context.Background(), s, path)
	if err != nil {
		t.Fatal(err)
	}
	tpl.File = rel
	style := TemplateStyle(tpl)
	deck := BuildDeck([]domain.Section{{Title: "Method", Body: "- point one", Notes: "say this"}}, domain.Knowledge{Title: "Paper"}, "Paper", "en", nil, GeometryOf(style))
	if g := GeometryOf(style); deck[1].Layout[0].X != g.Title[0] || deck[1].Layout[0].Size != 21 || deck[1].Layout[0].Align != "center" {
		t.Fatalf("content title should sit in the template's title area, scaled to its slide: %#v", deck[1].Layout[0])
	}
	a := domain.Artifact{Type: "B", Language: "en", Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{"B": style}}, Sections: deck}
	b, err := PPTX(s, a, domain.Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	for name, body := range files {
		if strings.Contains(body, "SAMPLE SLIDE TEXT") {
			t.Fatalf("template sample slide kept in %s", name)
		}
	}
	if _, ok := files["ppt/media/sample.jpeg"]; ok {
		t.Fatal("media used only by sample slides should be dropped")
	}
	if strings.Contains(files["_rels/.rels"], "thumbnail") || strings.Contains(files["[Content_Types].xml"], "notesSlide7") {
		t.Fatal("relationships and content types must not name dropped parts")
	}
	if strings.Contains(files["[Content_Types].xml"], "template.main") || !strings.Contains(files["[Content_Types].xml"], "presentation.main") || !strings.Contains(files["[Content_Types].xml"], `Extension="png"`) {
		t.Fatal("content types should describe a presentation with picture types")
	}
	pres := files["ppt/presentation.xml"]
	if strings.Count(pres, "<p:sldId ") != len(deck) || strings.Contains(pres, "sectionLst") {
		t.Fatalf("presentation should list only the generated slides: %s", pres)
	}
	if !strings.Contains(files["ppt/slides/_rels/slide1.xml.rels"], "slideLayout1.xml") || !strings.Contains(files["ppt/slides/_rels/slide2.xml.rels"], "slideLayout2.xml") {
		t.Fatal("cover and content slides should use the template's title and title-only layouts")
	}
	content := files["ppt/slides/slide2.xml"]
	if !strings.Contains(content, `<p:ph type="title"/>`) || !strings.Contains(content, `<p:ph type="sldNum" sz="quarter" idx="12"/>`) || !strings.Contains(content, ">point one<") {
		t.Fatalf("content slide should use the template's title and slide number placeholders: %s", content)
	}
	if strings.Contains(content, `<a:srgbClr val="1F2D3A"/>`) {
		t.Fatal("text colours should come from the template's scheme")
	}
	if !strings.Contains(files["ppt/slides/slide1.xml"], `<p:ph type="ctrTitle"/>`) || !strings.Contains(files["ppt/slides/slide1.xml"], `<p:ph type="subTitle" idx="1"/>`) {
		t.Fatal("cover should fill the title layout's placeholders")
	}
	if !strings.Contains(files["ppt/notesSlides/notesSlide2.xml"], "say this") || !strings.Contains(files["ppt/notesSlides/_rels/notesSlide2.xml.rels"], "notesMaster1.xml") {
		t.Fatal("notes should use the template's notes master")
	}
}

func TestRemapDeckMovesBoxesIntoTheNewTemplateAreas(t *testing.T) {
	from := DefaultGeometry()
	to := DefaultGeometry()
	to.Title, to.Body, to.Scale, to.TitleAlign, to.Template = [4]float64{0.1, 0.8, 0.8, 0.1}, [4]float64{0.1, 0.1, 0.8, 0.6}, 0.75, "center", true
	deck := BuildDeck([]domain.Section{{Title: "Method", Body: "- a"}}, domain.Knowledge{Title: "P"}, "P", "en", nil, from)
	RemapDeck(deck, from, to)
	title, points := deck[1].Layout[0], deck[1].Layout[1]
	if title.Y != 0.8 || title.Align != "center" || title.Size != 21 {
		t.Fatalf("title should move to the new title area: %#v", title)
	}
	if points.Y < 0.1 || points.Y+points.H > 0.71 {
		t.Fatalf("content should move into the new content area: %#v", points)
	}
}
