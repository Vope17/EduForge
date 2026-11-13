package render

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/template"
)

func TestPDFExportIntegration(t *testing.T) {
	if os.Getenv("EDUFORGE_EXPORT_INTEGRATION") != "1" {
		t.Skip("run in backend-tools with EDUFORGE_EXPORT_INTEGRATION=1")
	}
	font, err := os.ReadFile("/app/fonts/NotoSansTC.ttf")
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []artifact.Type{artifact.DocumentType, artifact.SlidesType} {
		blocks := []artifact.Block{{Kind: "heading", Text: "水循環"}, {Kind: "paragraph", Text: "蒸發與凝結 Water cycle"}}
		format := "docx"
		if kind == artifact.SlidesType {
			format = "pptx"
			blocks = []artifact.Block{{Kind: "slide", Layout: "title_body", Children: blocks, Notes: "請學生說明水循環。"}}
		}
		input := Input{Artifact: artifact.Artifact{Type: kind, Language: artifact.TraditionalChinese, Content: artifact.Content{Title: "水循環", Blocks: blocks}}, Spec: template.Spec{Font: "sans"}, Font: font}
		office, err := Office(t.Context(), input, format)
		if err != nil {
			t.Fatal(err)
		}
		data, err := PDF(t.Context(), office, format)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Fatal("invalid PDF")
		}
		file := filepath.Join(t.TempDir(), "output.pdf")
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		text, err := exec.CommandContext(t.Context(), "pdftotext", file, "-").Output()
		if err != nil || !strings.Contains(string(text), "水循環") {
			t.Fatalf("missing CJK text: %v", err)
		}
		fonts, err := exec.CommandContext(t.Context(), "pdffonts", file).Output()
		if err != nil || !strings.Contains(string(fonts), "Noto") || !strings.Contains(string(fonts), "yes") {
			t.Fatalf("missing embedded CJK font: %v", err)
		}
	}
}

func TestPDFRejectsInvalidSourceAndCancellation(t *testing.T) {
	if _, err := PDF(t.Context(), nil, "html"); err == nil {
		t.Fatal("accepted unsupported source")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := PDF(ctx, []byte("invalid"), "docx"); err == nil {
		t.Fatal("ignored cancellation")
	}
}
