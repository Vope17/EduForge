package docread

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func need(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

func opts(t *testing.T) Options { return Options{ImageDir: t.TempDir()} }

// writeDocx writes a minimal Word file with one paragraph per entry and a
// page break after each.
func writeDocx(t *testing.T, name, title string, pages []string) string {
	t.Helper()
	var body strings.Builder
	for _, p := range pages {
		body.WriteString(`<w:p><w:r><w:t>` + p + `</w:t></w:r></w:p><w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
	}
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `</w:body></w:document>`,
		"docProps/core.xml":   `<?xml version="1.0" encoding="UTF-8"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>` + title + `</dc:title></cp:coreProperties>`,
	}
	return writeZip(t, name, files)
}

func writeZip(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for n, c := range files {
		fw, _ := w.Create(n)
		_, _ = fw.Write([]byte(c))
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestDocxPagesTextImagesAndTitle(t *testing.T) {
	need(t, "libreoffice", "pdftotext", "pdftoppm", "pdfinfo")
	long := "Gradient descent updates parameters against the gradient of the loss. 梯度下降法沿著損失函數的負梯度方向更新參數。"
	file := writeDocx(t, "lecture.docx", "Optimisation notes", []string{"Page one. " + long, "Page two. " + long, "Page three. " + long})
	doc, err := Read(context.Background(), file, opts(t))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Format != "docx" || doc.Title != "Optimisation notes" {
		t.Fatalf("format/title = %q/%q", doc.Format, doc.Title)
	}
	if len(doc.Pages) < 3 {
		t.Fatalf("pages = %d, want at least 3", len(doc.Pages))
	}
	for i, want := range []string{"Page one", "Page two", "Page three"} {
		p := doc.Pages[i]
		if !strings.Contains(p.Text, want) || p.Mode != ModeText || p.Kind != KindPage || p.Index != i+1 {
			t.Fatalf("page %d = %+v", i+1, p)
		}
		if _, err := os.Stat(p.Image); err != nil {
			t.Fatalf("page %d has no image: %v", i+1, err)
		}
	}
	if !strings.Contains(strings.ReplaceAll(doc.Pages[0].Text, "\n", ""), "梯度下降法") {
		t.Fatalf("Chinese text lost: %q", doc.Pages[0].Text)
	}
}

func TestPDFFromConvertedDocument(t *testing.T) {
	need(t, "libreoffice", "pdftotext", "pdftoppm", "pdfinfo")
	docx := writeDocx(t, "a.docx", "", []string{"First page has enough readable words to count as text.", "Second page also has enough readable words to count."})
	pdf, cleanup, err := toPDF(context.Background(), docx)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	doc, err := Read(context.Background(), pdf, opts(t))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Format != "pdf" || len(doc.Pages) < 2 || doc.VisionPages() != 0 {
		t.Fatalf("doc = %+v", doc)
	}
	if !strings.Contains(doc.Pages[1].Text, "Second page") {
		t.Fatalf("page 2 text = %q", doc.Pages[1].Text)
	}
}

// scannedPDF is a one-page PDF that holds only a picture, like a scan.
func scannedPDF(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 260))
	for y := 0; y < 260; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 180, 255})
		}
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	var pdf bytes.Buffer
	offsets := []int{}
	obj := func(s string) { offsets = append(offsets, pdf.Len()); pdf.WriteString(s) }
	pdf.WriteString("%PDF-1.4\n")
	obj("1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n")
	obj("2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n")
	obj("3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 260]/Resources<</XObject<</Im0 5 0 R>>>>/Contents 4 0 R>>endobj\n")
	content := "q 200 0 0 260 0 0 cm /Im0 Do Q"
	obj(fmt.Sprintf("4 0 obj<</Length %d>>stream\n%s\nendstream endobj\n", len(content), content))
	offsets = append(offsets, pdf.Len())
	fmt.Fprintf(&pdf, "5 0 obj<</Type/XObject/Subtype/Image/Width 200/Height 260/ColorSpace/DeviceRGB/BitsPerComponent 8/Filter/DCTDecode/Length %d>>stream\n", jpg.Len())
	pdf.Write(jpg.Bytes())
	pdf.WriteString("\nendstream endobj\n")
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 6\n0000000000 65535 f \n")
	for _, o := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&pdf, "trailer<</Size 6/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", xref)
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, pdf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScannedPageIsReadByVisionNotRejected(t *testing.T) {
	need(t, "pdftotext", "pdftoppm", "pdfinfo")
	doc, err := Read(context.Background(), scannedPDF(t), opts(t))
	if err != nil {
		t.Fatalf("a scan must not be rejected: %v", err)
	}
	if len(doc.Pages) != 1 || doc.Pages[0].Mode != ModeVision || doc.VisionPages() != 1 {
		t.Fatalf("scan page = %+v", doc.Pages)
	}
	if _, err := os.Stat(doc.Pages[0].Image); err != nil {
		t.Fatal("the scan has no image to read")
	}
}

func TestPPTXSlidesNotesAndHiddenSlides(t *testing.T) {
	need(t, "libreoffice", "pdftoppm", "pdfinfo")
	src, err := os.ReadFile("../../tests/fixtures/sample-deck.pptx")
	if err != nil {
		t.Skip("sample deck fixture missing")
	}
	// Hide slide 3 and give slide 2 speaker notes.
	zr, err := zip.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(r)
		r.Close()
		files[f.Name] = b.String()
	}
	files["ppt/slides/slide3.xml"] = strings.Replace(files["ppt/slides/slide3.xml"], "<p:sld ", `<p:sld show="0" `, 1)
	if !strings.Contains(files["ppt/slides/slide3.xml"], `show="0"`) {
		t.Fatal("fixture slide 3 could not be hidden")
	}
	files["ppt/notesSlides/notesSlide2.xml"] = `<?xml version="1.0" encoding="UTF-8"?><p:notes xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2" name="img"/><p:cNvSpPr/><p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr></p:sp><p:sp><p:nvSpPr><p:cNvPr id="3" name="n"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>Remind students of the baseline.</a:t></a:r></a:p></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="4" name="num"/><p:cNvSpPr/><p:nvPr><p:ph type="sldNum"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:fld><a:t>2</a:t></a:fld></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`
	files["ppt/slides/_rels/slide2.xml.rels"] = strings.Replace(files["ppt/slides/_rels/slide2.xml.rels"], "</Relationships>", `<Relationship Id="rIdNotes" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide2.xml"/></Relationships>`, 1)
	file := writeZip(t, "deck.pptx", files)
	doc, err := Read(context.Background(), file, opts(t))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Format != "pptx" || len(doc.Pages) != 3 {
		t.Fatalf("a hidden slide should be skipped: %d pages", len(doc.Pages))
	}
	hidden := false
	for _, is := range doc.Issues {
		hidden = hidden || is.Code == IssueHiddenSlides
	}
	if !hidden {
		t.Fatalf("hidden slides not reported: %+v", doc.Issues)
	}
	if !strings.Contains(doc.Pages[1].Text, "Motivation") || doc.Pages[1].Notes != "Remind students of the baseline." {
		t.Fatalf("slide 2 = %+v", doc.Pages[1])
	}
	if !strings.Contains(doc.Pages[2].Text, "Results") || doc.Pages[2].Kind != KindSlide {
		t.Fatalf("the third page should be the old fourth slide (Results): %+v", doc.Pages[2])
	}
	for _, p := range doc.Pages {
		if strings.Contains(p.Text, "A two-stage network") {
			t.Fatal("the hidden slide's text was read")
		}
	}
	for i, p := range doc.Pages {
		if p.Image == "" {
			t.Fatalf("slide %d has no image (issues %+v)", i+1, doc.Issues)
		}
	}
	for _, is := range doc.Issues {
		if is.Code == IssueSlideMismatch {
			t.Fatalf("rendered pages and slides disagree: %+v", is)
		}
	}
}

func TestUnsupportedAndPageLimit(t *testing.T) {
	if _, err := Read(context.Background(), "notes.txt", opts(t)); err != ErrUnsupported {
		t.Fatalf("err = %v", err)
	}
	if _, err := Read(context.Background(), "a.pdf", Options{}); err == nil {
		t.Fatal("ImageDir is required")
	}
	need(t, "libreoffice", "pdfinfo")
	file := writeDocx(t, "long.docx", "", []string{"one", "two", "three"})
	o := opts(t)
	o.MaxPages = 2
	if _, err := Read(context.Background(), file, o); err == nil || !strings.Contains(err.Error(), "exceeds_2_pages") {
		t.Fatalf("page limit not enforced: %v", err)
	}
}

func TestOfficeFileThatIsNotAZipIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pptx")
	_ = os.WriteFile(path, []byte("not a zip"), 0o600)
	if _, err := Read(context.Background(), path, opts(t)); err == nil || err.Error() != "invalid_office_archive" {
		t.Fatalf("err = %v", err)
	}
}
