// Package documents parses local research and design files without expanding the literature corpus.
package documents

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"eduforge/internal/docread"
	"eduforge/internal/domain"
	"eduforge/internal/store"
)

const maxExpanded = 256 << 20

func Command(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var errout bytes.Buffer
	cmd.Stderr = &errout
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s_failed: %w", name, err)
	}
	return out, nil
}
func ConvertPDF(ctx context.Context, input, outdir string) (string, error) {
	return ConvertPDFWith(ctx, input, outdir, "")
}

// ConvertPDFWith converts with a LibreOffice PDF filter specification such as
// `impress_pdf_Export:{...}`; empty uses the default PDF export.
func ConvertPDFWith(ctx context.Context, input, outdir, filter string) (string, error) {
	target := "pdf"
	if filter != "" {
		target = "pdf:" + filter
	}
	return convert(ctx, input, outdir, target, ".pdf")
}

// ConvertTo converts a document with LibreOffice to another format, e.g. "docx".
func ConvertTo(ctx context.Context, input, outdir, format string) (string, error) {
	return convert(ctx, input, outdir, format, "."+format)
}

func convert(ctx context.Context, input, outdir, target, ext string) (string, error) {
	if err := os.MkdirAll(outdir, 0700); err != nil {
		return "", err
	}
	profile, err := os.MkdirTemp("", "eduforge-office-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(profile)
	_, err = Command(ctx, "libreoffice", "-env:UserInstallation=file://"+profile, "--headless", "--convert-to", target, "--outdir", outdir, input)
	if err != nil {
		return "", err
	}
	file := filepath.Join(outdir, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))+ext)
	if _, err = os.Stat(file); err != nil {
		return "", errors.New("pdf_export_missing")
	}
	return file, nil
}

// ParserVersion is bumped whenever parsing output changes meaningfully.
// 2: figures cropped from captions; formula review limited to real equations.
// 3: page text in reading order instead of -layout column interleaving.
// 4: ligatures restored where fonts extract them as punctuation.
// 7: pages come from docread (PDF, PPTX, DOCX), with a mode per page.
const ParserVersion = 7

// pageImageEdge is the longest edge, in pixels, of the page images models read.
const pageImageEdge = 1400

// NeedsParse reports whether a source has no parse result from the current parser.
func NeedsParse(src domain.Source) bool {
	return src.Parsed == nil || src.Parsed.ParserVersion < ParserVersion
}

// Parse reads a source file into page units with page images. A page with
// little text (a scan, a picture-only slide) is kept and marked "vision" so a
// model reads its image; it is not rejected.
func Parse(ctx context.Context, s *store.Store, r domain.Record) (domain.Parsed, error) {
	src, err := domain.Decode[domain.Source](r.Data)
	if err != nil {
		return domain.Parsed{}, err
	}
	if src.Role != "research" {
		return domain.Parsed{}, errors.New("design_is_not_research")
	}
	file, err := s.Path(src.Path)
	if err != nil {
		return domain.Parsed{}, err
	}
	tmp, err := os.MkdirTemp("", "eduforge-parse-")
	if err != nil {
		return domain.Parsed{}, err
	}
	defer os.RemoveAll(tmp)
	doc, err := docread.Read(ctx, file, docread.Options{ImageDir: filepath.Join(tmp, "pages"), ImageEdge: pageImageEdge})
	if err != nil {
		return domain.Parsed{}, err
	}
	p := domain.Parsed{ParserVersion: ParserVersion, Title: doc.Title, Units: []domain.Unit{}, Images: []domain.Image{}, Issues: []domain.Issue{}}
	label := map[string]string{docread.KindPage: "p", docread.KindSlide: "s"}
	for _, page := range doc.Pages {
		cite := fmt.Sprintf("%s:%s%d", r.ID, label[page.Kind], page.Index)
		text := page.Text
		if page.Kind == docread.KindPage {
			text = JoinHyphenatedLines(RepairLigatures(text))
		}
		if page.Notes != "" {
			text = strings.TrimSpace(text + "\n\nNotes:\n" + page.Notes)
		}
		u := domain.Unit{ID: cite, SourceID: r.ID, Locator: page.Index, Kind: page.Kind, Text: text, Mode: page.Mode}
		if page.Image != "" {
			if b, e := os.ReadFile(page.Image); e == nil {
				u.Preview, _, e = s.Blob(b, ".png")
				if e != nil {
					u.Preview = ""
				}
			}
		}
		if u.Preview == "" {
			p.Issues = append(p.Issues, domain.Issue{Code: "preview_failed", Citation: cite})
		}
		if page.Mode == docread.ModeText && needsFormulaReview(text) {
			p.Issues = append(p.Issues, domain.Issue{Code: "formula_review", Citation: cite})
		}
		p.Units = append(p.Units, u)
	}
	for _, issue := range doc.Issues {
		switch issue.Code {
		case docread.IssueNoImage:
			// Reported above as preview_failed for the page.
		default:
			p.Issues = append(p.Issues, domain.Issue{Code: issue.Code, Detail: issue.Detail})
		}
	}
	switch doc.Format {
	case "pdf":
		figures, err := extractFigures(ctx, s, file, r.ID, tmp)
		if err == nil && len(figures) == 0 {
			figures, err = embeddedImages(ctx, s, file, r.ID, tmp)
		}
		p.Images = append(p.Images, figures...)
		if err != nil {
			p.Issues = append(p.Issues, domain.Issue{Code: "figure_review"})
		}
	case "pptx":
		images, issues, err := pptxImages(s, file, r.ID)
		if err != nil {
			return p, err
		}
		p.Images = append(p.Images, images...)
		p.Issues = append(p.Issues, issues...)
	}
	return p, nil
}

type Archive struct {
	reader *zip.ReadCloser
	files  map[string]*zip.File
}

func OpenArchive(file string) (*Archive, error) {
	r, err := zip.OpenReader(file)
	if err != nil {
		return nil, errors.New("invalid_office_archive")
	}
	a := &Archive{reader: r, files: map[string]*zip.File{}}
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
func (a *Archive) Close() { _ = a.reader.Close() }
func (a *Archive) Read(name string) ([]byte, error) {
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
func slideNames(a *Archive) ([]string, error) {
	b, err := a.Read("ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	relsData, err := a.Read("ppt/_rels/presentation.xml.rels")
	if err != nil {
		return nil, err
	}
	rels, err := relations(relsData)
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	out := []string{}
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if el, ok := token.(xml.StartElement); ok && el.Name.Local == "sldId" {
			for _, attr := range el.Attr {
				if attr.Name.Local == "id" && strings.Contains(attr.Name.Space, "relationships") {
					rel, ok := rels[attr.Value]
					if !ok {
						return nil, errors.New("slide_relationship_missing")
					}
					out = append(out, path.Clean("ppt/"+rel.Target))
				}
			}
		}
	}
	if len(out) > 300 {
		return nil, errors.New("document_exceeds_300_slides")
	}
	return out, nil
}

// pptxImages collects the pictures embedded in a presentation's slides, with
// the slide each came from. Slides are numbered as docread numbers them
// (visible slides only), so a figure cites the same unit as its slide's text.
func pptxImages(s *store.Store, file, id string) ([]domain.Image, []domain.Issue, error) {
	a, err := OpenArchive(file)
	if err != nil {
		return nil, nil, err
	}
	defer a.Close()
	names, err := slideNames(a)
	if err != nil {
		return nil, nil, err
	}
	var images []domain.Image
	var issues []domain.Issue
	n := 0
	for _, name := range names {
		if b, e := a.Read(name); e == nil && hiddenSlide(b) {
			continue
		}
		n++
		cite := fmt.Sprintf("%s:s%d", id, n)
		data, err := a.Read(path.Join(path.Dir(name), "_rels", path.Base(name)+".rels"))
		if err != nil {
			continue
		}
		rels, err := relations(data)
		if err != nil {
			return nil, nil, err
		}
		keys := []string{}
		for key := range rels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			r := rels[key]
			if strings.HasSuffix(r.Type, "/chart") {
				issues = append(issues, domain.Issue{Code: "chart_formula_review", Citation: cite})
			}
			if !strings.HasSuffix(r.Type, "/image") {
				continue
			}
			target := path.Clean(path.Join(path.Dir(name), r.Target))
			ext := strings.ToLower(path.Ext(target))
			if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
				issues = append(issues, domain.Issue{Code: "figure_review", Citation: cite})
				continue
			}
			img, err := a.Read(target)
			if err != nil {
				continue
			}
			rel, _, err := s.Blob(img, ext)
			if err != nil {
				return nil, nil, err
			}
			images = append(images, domain.Image{Path: rel, Citation: cite, Original: true})
		}
	}
	return images, issues, nil
}

// hiddenSlide reports whether a slide's root element has show="0".
func hiddenSlide(slide []byte) bool {
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
