package render

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net"
	"strings"
	"testing"

	"eduforge/backend/internal/artifact"
	"eduforge/backend/internal/template"
)

func TestOfficeExportsPreserveContentAndEmbedAssets(t *testing.T) {
	for _, language := range []artifact.Language{artifact.TraditionalChinese, artifact.English} {
		for _, format := range []string{"docx", "pptx"} {
			t.Run(string(language)+format, func(t *testing.T) {
				block := artifact.Block{ID: "title", Kind: "heading", Text: "水循環 & Water <cycle>"}
				text := artifact.Block{ID: "body", Kind: "paragraph", Text: "蒸發\nCondensation"}
				image := artifact.Block{ID: "image", Kind: "image", Text: "水循環示意圖", ImageURL: "https://example.com/image.png"}
				content := artifact.Content{Title: "水循環課程", Blocks: []artifact.Block{block, text, image}}
				kind := artifact.DocumentType
				if format == "pptx" {
					kind = artifact.SlidesType
					content.Blocks = []artifact.Block{{ID: "page", Kind: "slide", Layout: "image_text", Notes: "請學生說明蒸發。", Children: content.Blocks}}
				}
				in := Input{Artifact: artifact.Artifact{Type: kind, Language: language, Content: content, Revision: 3}, Spec: template.Spec{Font: "sans", Palette: "ocean"}, Font: bytes.Repeat([]byte{1}, 128), Images: map[string]Image{image.ImageURL: {Data: []byte("image-data"), Width: 100, Height: 50}}}
				before := in.Artifact.Content.Blocks[0].ID
				data, err := Office(t.Context(), in, format)
				if err != nil {
					t.Fatal(err)
				}
				archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				files := map[string][]byte{}
				for _, entry := range archive.File {
					stream, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(stream)
					if err != nil {
						t.Fatal(err)
					}
					if err := stream.Close(); err != nil {
						t.Fatal(err)
					}
					if _, ok := files[entry.Name]; ok {
						t.Fatal("duplicate package entry")
					}
					files[entry.Name] = data
					if strings.HasSuffix(entry.Name, ".xml") || strings.HasSuffix(entry.Name, ".rels") {
						decoder := xml.NewDecoder(bytes.NewReader(data))
						for {
							_, err := decoder.Token()
							if err == io.EOF {
								break
							}
							if err != nil {
								t.Fatalf("%s: %v", entry.Name, err)
							}
						}
					}
				}
				main := "word/document.xml"
				font := "word/fonts/font.odttf"
				img := "word/media/image1.png"
				if format == "pptx" {
					main = "ppt/slides/slide1.xml"
					font = "ppt/fonts/font.ttf"
					img = "ppt/media/image1_2.png"
					if !bytes.Contains(files["ppt/notesSlides/notesSlide1.xml"], []byte("請學生說明蒸發。")) {
						t.Fatal("lost notes")
					}
				}
				if !bytes.Contains(files[main], []byte("水循環 &amp; Water &lt;cycle&gt;")) {
					t.Fatal("lost or unescaped content")
				}
				if len(files[font]) != len(in.Font) || !bytes.Equal(files[img], []byte("image-data")) {
					t.Fatal("missing embedded assets")
				}
				if in.Artifact.Content.Blocks[0].ID != before || in.Artifact.Revision != 3 {
					t.Fatal("export mutated revision")
				}
				delete(in.Images, image.ImageURL)
				if _, err := Office(t.Context(), in, format); err == nil {
					t.Fatal("missing image silently accepted")
				}
			})
		}
	}
}

func TestImageFetchRejectsPrivateDestinations(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "::1", "fc00::1", "::ffff:127.0.0.1"} {
		if publicIP(net.ParseIP(address)) {
			t.Fatalf("allowed %s", address)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("rejected public address")
	}
	if _, err := FetchImage(context.Background(), "http://example.com/image"); err == nil {
		t.Fatal("allowed HTTP")
	}
	if _, err := DecodeImage([]byte("<svg/>")); err == nil {
		t.Fatal("accepted executable image format")
	}
}

func TestDenseSlideTextFailsInsteadOfClipping(t *testing.T) {
	if fittedSize(strings.Repeat("字", 800), 5303520, 400000, 2400, 1000) != 0 {
		t.Fatal("dense text accepted in a small box")
	}
	if fittedSize("水循環 Water cycle", 5303520, 4663440, 2400, 1000) == 0 {
		t.Fatal("normal text rejected")
	}
}
