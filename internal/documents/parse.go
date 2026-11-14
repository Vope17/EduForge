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
	"strconv"
	"strings"
	"time"

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
const ParserVersion = 6

// NeedsParse reports whether a source has no parse result from the current parser.
func NeedsParse(src domain.Source) bool {
	return src.Parsed == nil || src.Parsed.ParserVersion < ParserVersion
}

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
	var p domain.Parsed
	switch strings.ToLower(filepath.Ext(file)) {
	case ".pdf":
		p, err = parsePDF(ctx, s, file, r.ID)
	case ".pptx":
		p, err = parsePPTX(s, file, r.ID)
	default:
		err = errors.New("unsupported_research_file")
	}
	if err != nil {
		return p, err
	}
	readable := false
	for _, u := range p.Units {
		if len(strings.TrimSpace(u.Text)) >= 30 {
			readable = true
		}
	}
	if !readable {
		return p, errors.New("ocr_required")
	}
	p.ParserVersion = ParserVersion
	return p, nil
}

func parsePDF(ctx context.Context, s *store.Store, file, id string) (domain.Parsed, error) {
	p := domain.Parsed{Units: []domain.Unit{}, Images: []domain.Image{}, Issues: []domain.Issue{}}
	// Reading order keeps two-column prose intact; -layout interleaves columns.
	out, err := Command(ctx, "pdftotext", "-enc", "UTF-8", file, "-")
	if err != nil {
		return p, err
	}
	if len(out) > 32<<20 {
		return p, errors.New("document_text_too_large")
	}
	pages := strings.Split(string(out), "\f")
	if len(pages) > 0 && strings.TrimSpace(pages[len(pages)-1]) == "" {
		pages = pages[:len(pages)-1]
	}
	if len(pages) > 300 {
		return p, errors.New("document_exceeds_300_pages")
	}
	if info, err := Command(ctx, "pdfinfo", "-enc", "UTF-8", file); err == nil {
		p.Title = pdfTitle(string(info))
	}
	tmp, err := os.MkdirTemp("", "eduforge-pdf-")
	if err != nil {
		return p, err
	}
	defer os.RemoveAll(tmp)
	for i, text := range pages {
		select {
		case <-ctx.Done():
			return p, ctx.Err()
		default:
		}
		loc := i + 1
		cite := fmt.Sprintf("%s:p%d", id, loc)
		text = JoinHyphenatedLines(RepairLigatures(text))
		u := domain.Unit{ID: cite, SourceID: id, Locator: loc, Kind: "page", Text: strings.TrimSpace(text)}
		base := filepath.Join(tmp, fmt.Sprintf("page-%d", loc))
		_, err = Command(ctx, "pdftoppm", "-f", strconv.Itoa(loc), "-l", strconv.Itoa(loc), "-scale-to", "1000", "-singlefile", "-png", file, base)
		if err == nil {
			b, e := os.ReadFile(base + ".png")
			if e == nil {
				u.Preview, _, err = s.Blob(b, ".png")
			}
		}
		if err != nil {
			p.Issues = append(p.Issues, domain.Issue{Code: "preview_failed", Citation: cite})
		}
		if len(strings.TrimSpace(text)) < 30 {
			p.Issues = append(p.Issues, domain.Issue{Code: "ocr_required", Citation: cite})
		}
		if needsFormulaReview(text) {
			p.Issues = append(p.Issues, domain.Issue{Code: "formula_review", Citation: cite})
		}
		p.Units = append(p.Units, u)
	}
	figures, err := extractFigures(ctx, s, file, id, tmp)
	if err == nil && len(figures) == 0 {
		figures, err = embeddedImages(ctx, s, file, id, tmp)
	}
	p.Images = append(p.Images, figures...)
	if err != nil {
		p.Issues = append(p.Issues, domain.Issue{Code: "figure_review"})
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
func textFromXML(b []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	var out strings.Builder
	for {
		t, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch el := t.(type) {
		case xml.StartElement:
			if el.Name.Local == "t" {
				var text string
				if err := dec.DecodeElement(&text, &el); err != nil {
					return "", err
				}
				out.WriteString(text)
			}
		case xml.EndElement:
			if el.Name.Local == "p" {
				out.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(out.String()), nil
}
func parsePPTX(s *store.Store, file, id string) (domain.Parsed, error) {
	p := domain.Parsed{Units: []domain.Unit{}, Images: []domain.Image{}, Issues: []domain.Issue{}}
	a, err := OpenArchive(file)
	if err != nil {
		return p, err
	}
	defer a.Close()
	names, err := slideNames(a)
	if err != nil {
		return p, err
	}
	for i, name := range names {
		b, err := a.Read(name)
		if err != nil {
			return p, err
		}
		text, err := textFromXML(b)
		if err != nil {
			return p, err
		}
		cite := fmt.Sprintf("%s:s%d", id, i+1)
		p.Units = append(p.Units, domain.Unit{ID: cite, SourceID: id, Locator: i + 1, Kind: "slide", Text: text})
		if text == "" {
			p.Issues = append(p.Issues, domain.Issue{Code: "unreadable_slide", Citation: cite})
		}
		relfile := path.Join(path.Dir(name), "_rels", path.Base(name)+".rels")
		data, err := a.Read(relfile)
		if err != nil {
			continue
		}
		rels, err := relations(data)
		if err != nil {
			return p, err
		}
		keys := []string{}
		for key := range rels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			r := rels[key]
			if strings.HasSuffix(r.Type, "/chart") {
				p.Issues = append(p.Issues, domain.Issue{Code: "chart_formula_review", Citation: cite})
			}
			if !strings.HasSuffix(r.Type, "/image") {
				continue
			}
			target := path.Clean(path.Join(path.Dir(name), r.Target))
			ext := strings.ToLower(path.Ext(target))
			if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
				p.Issues = append(p.Issues, domain.Issue{Code: "figure_review", Citation: cite})
				continue
			}
			img, err := a.Read(target)
			if err != nil {
				continue
			}
			rel, _, err := s.Blob(img, ext)
			if err != nil {
				return p, err
			}
			p.Images = append(p.Images, domain.Image{Path: rel, Citation: cite, Original: true})
		}
	}
	return p, nil
}
