package artifacts

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func TestPPTXContainsEditableTextNotesAndValidXML(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := domain.Artifact{Type: "B", Language: "zh-TW", Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{"B": {"accent": "087F73"}}}, Check: domain.Check{Status: "draft"}, Sections: []domain.Section{{Title: "方法 & Method", Body: "可編輯的研究文字", Notes: "這是逐頁講稿。", Citations: []string{"source:s1"}}}}
	b, err := PPTX(s, a, domain.Knowledge{})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	foundText, foundNotes := false, false
	for _, f := range z.File {
		r, _ := f.Open()
		data, _ := io.ReadAll(r)
		_ = r.Close()
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") {
			decoder := xml.NewDecoder(bytes.NewReader(data))
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("invalid XML %s: %v", f.Name, err)
				}
			}
		}
		if f.Name == "ppt/slides/slide1.xml" {
			foundText = bytes.Contains(data, []byte("可編輯的研究文字")) && bytes.Contains(data, []byte("<p:sp>"))
		}
		if f.Name == "ppt/notesSlides/notesSlide1.xml" {
			foundNotes = bytes.Contains(data, []byte("這是逐頁講稿。"))
		}
	}
	if !foundText || !foundNotes {
		t.Fatal("editable text or speaker notes missing")
	}
}
func TestReaderEscapesSourceContentAndKeepsOriginalNumbers(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := domain.Artifact{Type: "A", Language: "en", Style: domain.StyleSnapshot{Resolved: map[string]domain.Style{}}, Sections: []domain.Section{{Title: "Source", Body: "<script>alert(1)</script> Accuracy 0.91", Citations: []string{"s:p1"}}}}
	k := domain.Knowledge{Title: "Research", Units: []domain.Unit{{ID: "s:p1", SourceID: "s", Locator: 1, Kind: "page", Text: "Accuracy 0.91"}}}
	b, err := HTML(s, a, k)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("<script>")) || !bytes.Contains(b, []byte("0.91")) {
		t.Fatal("reader escaped or modified source incorrectly")
	}
	if bytes.Contains(b, []byte("s:p1")) || !bytes.Contains(b, []byte("<li>Research</li>")) {
		t.Fatal("references should list the lesson name, not internal source IDs")
	}
}
