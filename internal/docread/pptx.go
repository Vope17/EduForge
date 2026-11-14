package docread

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const maxExpanded = 256 << 20

// archive is a size-limited view of an Office zip.
type archive struct {
	zip   *zip.ReadCloser
	files map[string]*zip.File
}

func openArchive(file string) (*archive, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, errors.New("invalid_office_archive")
	}
	a := &archive{zip: r, files: map[string]*zip.File{}}
	var size uint64
	for _, f := range r.File {
		size += f.UncompressedSize64
		if size > maxExpanded || len(r.File) > 10000 || f.Flags&1 != 0 {
			r.Close()
			return nil, errors.New("archive_limits_exceeded")
		}
		a.files[f.Name] = f
	}
	return a, nil
}

func (a *archive) close() { _ = a.zip.Close() }

func (a *archive) read(name string) ([]byte, error) {
	f, ok := a.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, maxExpanded+1))
}

type relation struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Mode   string `xml:"TargetMode,attr"`
}

func relations(b []byte) (map[string]relation, error) {
	var doc struct {
		R []relation `xml:"Relationship"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := map[string]relation{}
	for _, r := range doc.R {
		if r.Mode != "External" {
			out[r.ID] = r
		}
	}
	return out, nil
}

// slidePart is one slide as stored in the file.
type slidePart struct {
	name   string
	hidden bool
}

// slideParts lists the slides in presentation order.
func slideParts(a *archive) ([]slidePart, error) {
	b, err := a.read("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	relData, err := a.read("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return nil, err
	}
	rels, err := relations(relData)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	var out []slidePart
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		el, ok := token.(xml.StartElement)
		if !ok || el.Name.Local != "sldId" {
			continue
		}
		for _, attr := range el.Attr {
			if attr.Name.Local == "id" && strings.Contains(attr.Name.Space, "relationships") {
				rel, ok := rels[attr.Value]
				if !ok {
					return nil, errors.New("slide_relationship_missing")
				}
				out = append(out, slidePart{name: path.Clean("ppt/" + rel.Target)})
			}
		}
	}
	for i := range out {
		b, err := a.read(out[i].name)
		if err != nil {
			return nil, err
		}
		out[i].hidden = isHidden(b)
	}
	return out, nil
}

// isHidden reports whether a slide's root element has show="0".
func isHidden(slide []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(slide))
	for {
		token, err := dec.Token()
		if err != nil {
			return false
		}
		if el, ok := token.(xml.StartElement); ok {
			for _, attr := range el.Attr {
				if attr.Name.Local == "show" && (attr.Value == "0" || attr.Value == "false") {
					return true
				}
			}
			return false
		}
	}
}

// shapeText returns the text of a slide or notes part, one line per
// paragraph. Slide numbers, footers and dates are left out; for notes only
// the notes body is read.
func shapeText(b []byte, notes bool) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	type shape struct {
		ph    string
		lines []string
	}
	var stack []*shape
	var para strings.Builder
	var out []string
	inPara := false
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch el := token.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "sp", "graphicFrame":
				stack = append(stack, &shape{})
			case "ph":
				if len(stack) > 0 {
					for _, attr := range el.Attr {
						if attr.Name.Local == "type" {
							stack[len(stack)-1].ph = attr.Value
						}
					}
					if stack[len(stack)-1].ph == "" {
						stack[len(stack)-1].ph = "body"
					}
				}
			case "p":
				inPara = true
				para.Reset()
			case "br":
				if inPara {
					para.WriteByte('\n')
				}
			case "t":
				var text string
				if err := dec.DecodeElement(&text, &el); err != nil {
					return "", err
				}
				if inPara {
					para.WriteString(text)
				}
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "p":
				if inPara && len(stack) > 0 {
					line := strings.TrimSpace(para.String())
					if line != "" {
						top := stack[len(stack)-1]
						top.lines = append(top.lines, line)
					}
				}
				inPara = false
			case "sp", "graphicFrame":
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				keep := true
				switch top.ph {
				case "sldNum", "ftr", "dt", "sldImg":
					keep = false
				}
				if notes && top.ph != "body" {
					keep = false
				}
				if keep {
					out = append(out, top.lines...)
				}
			}
		}
	}
	return strings.Join(out, "\n"), nil
}

// notesFor reads a slide's speaker notes, if it has any.
func notesFor(a *archive, slide string) string {
	data, err := a.read(path.Join(path.Dir(slide), "_rels", path.Base(slide)+".rels"))
	if err != nil {
		return ""
	}
	rels, err := relations(data)
	if err != nil {
		return ""
	}
	for _, rel := range rels {
		if !strings.HasSuffix(rel.Type, "/notesSlide") {
			continue
		}
		b, err := a.read(path.Clean(path.Join(path.Dir(slide), rel.Target)))
		if err != nil {
			return ""
		}
		text, _ := shapeText(b, true)
		return strings.TrimSpace(text)
	}
	return ""
}

// officeTitle reads dc:title from docProps/core.xml.
func officeTitle(a *archive) string {
	b, err := a.read("docProps/core.xml")
	if err != nil {
		return ""
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		token, err := dec.Token()
		if err != nil {
			return ""
		}
		if el, ok := token.(xml.StartElement); ok && el.Name.Local == "title" {
			var title string
			if dec.DecodeElement(&title, &el) == nil {
				return strings.TrimSpace(title)
			}
			return ""
		}
	}
}

// readPPTX reads a presentation: each visible slide's text and notes come
// from the file, its image from a LibreOffice rendering. Hidden slides are
// skipped (they are not shown to the class, and the rendering omits them).
func readPPTX(ctx context.Context, file string, o Options) (Document, error) {
	a, err := openArchive(file)
	if err != nil {
		return Document{}, err
	}
	defer a.close()
	parts, err := slideParts(a)
	if err != nil {
		return Document{}, err
	}
	doc := Document{Title: officeTitle(a), Format: "pptx"}
	var visible []slidePart
	hidden := 0
	for _, p := range parts {
		if p.hidden {
			hidden++
			continue
		}
		visible = append(visible, p)
	}
	if len(visible) > o.MaxPages {
		return Document{}, tooManyPages(o.MaxPages)
	}
	if hidden > 0 {
		doc.Issues = append(doc.Issues, Issue{Code: IssueHiddenSlides, Detail: fmt.Sprint(hidden)})
	}
	if len(visible) == 0 {
		return doc, nil
	}
	text := make([]string, len(visible))
	notes := make([]string, len(visible))
	for i, p := range visible {
		b, err := a.read(p.name)
		if err != nil {
			return Document{}, err
		}
		if text[i], err = shapeText(b, false); err != nil {
			return Document{}, err
		}
		notes[i] = notesFor(a, p.name)
	}
	pdf, cleanup, err := toPDF(ctx, file)
	if err != nil {
		return Document{}, err
	}
	defer cleanup()
	count, _, err := pdfInfo(ctx, pdf)
	if err != nil {
		return Document{}, err
	}
	images, err := renderPages(ctx, pdf, o.ImageDir, o.ImageEdge)
	if err != nil {
		return Document{}, err
	}
	if count != len(visible) {
		doc.Issues = append(doc.Issues, Issue{Code: IssueSlideMismatch, Detail: fmt.Sprintf("%d slides, %d rendered pages", len(visible), count)})
	}
	pages, issues := assemble(KindSlide, text, images, o)
	for i := range pages {
		pages[i].Notes = notes[i]
	}
	doc.Pages, doc.Issues = pages, append(doc.Issues, issues...)
	return doc, nil
}

// readDOCX reads a Word document through its PDF rendering, so a page's text
// and image always describe the same page.
func readDOCX(ctx context.Context, file string, o Options) (Document, error) {
	title := ""
	if a, err := openArchive(file); err == nil {
		title = officeTitle(a)
		a.close()
	} else {
		return Document{}, err
	}
	pdf, cleanup, err := toPDF(ctx, file)
	if err != nil {
		return Document{}, err
	}
	defer cleanup()
	doc, err := readPDF(ctx, pdf, o)
	if err != nil {
		return Document{}, err
	}
	doc.Format = "docx"
	if title != "" {
		doc.Title = title
	}
	return doc, nil
}
