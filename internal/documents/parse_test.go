package documents

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func parseFixture(t *testing.T, name string, data []byte) (*store.Store, domain.Record) {
	t.Helper()
	for _, tool := range []string{"libreoffice", "pdftotext", "pdftoppm", "pdfinfo"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	rel, sum, err := s.Blob(data, filepath.Ext(name))
	if err != nil {
		t.Fatal(err)
	}
	src := domain.Source{Name: name, Role: "research", Path: rel, SHA256: sum, Size: int64(len(data))}
	return s, domain.Record{ID: "src1", Kind: "source", Data: domain.Encode(src)}
}

func TestParsePPTXMakesSlideUnitsWithImages(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/sample-deck.pptx")
	if err != nil {
		t.Skip("fixture missing")
	}
	s, r := parseFixture(t, "deck.pptx", data)
	p, err := Parse(context.Background(), s, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Units) != 4 || p.ParserVersion != ParserVersion {
		t.Fatalf("units = %d, version %d", len(p.Units), p.ParserVersion)
	}
	u := p.Units[1]
	if u.ID != "src1:s2" || u.Kind != "slide" || u.Locator != 2 || u.Mode != "text" || !strings.Contains(u.Text, "Motivation") {
		t.Fatalf("slide unit = %+v", u)
	}
	if _, err := s.Path(u.Preview); err != nil || u.Preview == "" {
		t.Fatalf("slide has no preview image: %q %v", u.Preview, err)
	}
}

func TestParseDocxMakesPageUnits(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Newton's second law relates force, mass and acceleration: F = m a.</w:t></w:r></w:p></w:body></w:document>`,
	} {
		f, _ := w.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = w.Close()
	s, r := parseFixture(t, "notes.docx", buf.Bytes())
	p, err := Parse(context.Background(), s, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Units) != 1 || p.Units[0].ID != "src1:p1" || p.Units[0].Kind != "page" || p.Units[0].Mode != "text" || p.Units[0].Preview == "" {
		t.Fatalf("units = %+v", p.Units)
	}
	if !strings.Contains(p.Units[0].Text, "second law") {
		t.Fatalf("text = %q", p.Units[0].Text)
	}
}

func TestParseNoLongerRejectsPagesWithoutText(t *testing.T) {
	// A one-page PDF with no text layer (rendered from an empty Word page).
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Hi</w:t></w:r></w:p></w:body></w:document>`,
	} {
		f, _ := w.Create(name)
		_, _ = f.Write([]byte(body))
	}
	_ = w.Close()
	s, r := parseFixture(t, "short.docx", buf.Bytes())
	p, err := Parse(context.Background(), s, r)
	if err != nil {
		t.Fatalf("a page with almost no text must be kept for vision reading: %v", err)
	}
	if len(p.Units) != 1 || p.Units[0].Mode != "vision" {
		t.Fatalf("units = %+v", p.Units)
	}
}
