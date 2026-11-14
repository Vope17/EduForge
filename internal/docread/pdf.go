package docread

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var pagesLine = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)
var titleLine = regexp.MustCompile(`(?m)^Title:\s+(.+)$`)

// pdfInfo returns the page count and the title in the PDF's metadata.
func pdfInfo(ctx context.Context, file string) (pages int, title string, err error) {
	out, err := run(ctx, 30*time.Second, "pdfinfo", "-enc", "UTF-8", file)
	if err != nil {
		return 0, "", err
	}
	if m := pagesLine.FindSubmatch(out); m != nil {
		pages, _ = strconv.Atoi(string(m[1]))
	}
	if m := titleLine.FindSubmatch(out); m != nil {
		title = strings.TrimSpace(string(m[1]))
	}
	return pages, title, nil
}

// pdfText returns each page's text in reading order (so two-column prose is
// not interleaved), indexed from page 1.
func pdfText(ctx context.Context, file string, pages int) ([]string, error) {
	out, err := run(ctx, 90*time.Second, "pdftotext", "-enc", "UTF-8", file, "-")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(out), "\f")
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	text := make([]string, pages)
	for i := range text {
		if i < len(parts) {
			text[i] = tidy(parts[i])
		}
	}
	return text, nil
}

// tidy trims a page's text and collapses runs of blank lines.
func tidy(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

var imageName = regexp.MustCompile(`^page-(\d+)\.png$`)

// renderPages draws every page of a PDF as a PNG in dir, in one pdftoppm run,
// and returns page number → path. Pages that failed to render are absent.
func renderPages(ctx context.Context, file, dir string, edge int) (map[int]string, error) {
	tmp, err := os.MkdirTemp(dir, ".render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if _, err = run(ctx, 240*time.Second, "pdftoppm", "-png", "-scale-to", strconv.Itoa(edge), file, filepath.Join(tmp, "page")); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return nil, err
	}
	out := map[int]string{}
	for _, e := range entries {
		m := imageName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		final := filepath.Join(dir, fmt.Sprintf("page-%04d.png", n))
		if err = os.Rename(filepath.Join(tmp, e.Name()), final); err != nil {
			return nil, err
		}
		out[n] = final
	}
	return out, nil
}

// readPDF reads a PDF: text per page and an image per page.
func readPDF(ctx context.Context, file string, o Options) (Document, error) {
	pages, title, err := pdfInfo(ctx, file)
	if err != nil {
		return Document{}, err
	}
	if pages > o.MaxPages {
		return Document{}, tooManyPages(o.MaxPages)
	}
	doc := Document{Title: title, Format: "pdf"}
	if pages == 0 {
		return doc, nil
	}
	text, err := pdfText(ctx, file, pages)
	if err != nil {
		return Document{}, err
	}
	images, err := renderPages(ctx, file, o.ImageDir, o.ImageEdge)
	if err != nil {
		return Document{}, err
	}
	doc.Pages, doc.Issues = assemble(KindPage, text, images, o)
	return doc, nil
}

// assemble builds pages from per-page text and images.
func assemble(kind string, text []string, images map[int]string, o Options) ([]Page, []Issue) {
	pages := make([]Page, len(text))
	var issues []Issue
	for i, t := range text {
		p := Page{Index: i + 1, Kind: kind, Text: t, Image: images[i+1]}
		classify(&p, o.MinText)
		if p.Image == "" {
			issues = append(issues, Issue{Code: IssueNoImage, Page: p.Index})
		}
		pages[i] = p
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Page < issues[j].Page })
	return pages, issues
}
