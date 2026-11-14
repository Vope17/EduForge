// Package docread turns teaching material (PDF, PPTX, DOCX) into pages: for
// every page or slide its text, a rendered image and where it came from.
//
// It depends only on the standard library and the command line tools
// pdftotext, pdftoppm, pdfinfo and LibreOffice, so it can be lifted out of
// EduForge and reused. It reads and renders; it never interprets. What the
// pages mean (knowledge, evidence, coverage) is decided by the caller.
//
// Every page carries a Mode. A page with enough text is ModeText. A page with
// little or no text (a scan, a slide that is one picture) is ModeVision: the
// reader must look at the page image instead of trusting the text.
package docread

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Page modes.
const (
	ModeText   = "text"
	ModeVision = "vision"
)

// Page kinds.
const (
	KindPage  = "page"
	KindSlide = "slide"
)

// Issue codes reported on a document or a page.
const (
	IssueNoImage         = "page_image_missing"
	IssueSlideMismatch   = "slide_image_mismatch"
	IssueHiddenSlides    = "hidden_slides_skipped"
	IssueTextCountOffset = "text_page_count_mismatch"
)

// Errors returned by Read.
var (
	ErrUnsupported = errors.New("unsupported_document_type")
	ErrNoPages     = errors.New("document_has_no_pages")
	ErrNoImages    = errors.New("page_images_unavailable")
)

// Options configure a read. ImageDir is required and receives one PNG per page.
type Options struct {
	ImageDir  string
	MaxPages  int // default 300
	ImageEdge int // longest edge of a page image in pixels, default 1600
	MinText   int // fewer text characters than this make a page ModeVision, default 30
}

// Page is one page of a PDF or DOCX, or one slide of a PPTX.
type Page struct {
	Index int    `json:"index"` // 1-based
	Kind  string `json:"kind"`  // KindPage or KindSlide
	Text  string `json:"text"`
	Notes string `json:"notes,omitempty"` // speaker notes (slides)
	Image string `json:"image,omitempty"` // PNG path inside Options.ImageDir
	Mode  string `json:"mode"`
}

// Issue is something a reader should know about the document or one page.
type Issue struct {
	Code   string `json:"code"`
	Page   int    `json:"page,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Document is the result of a read.
type Document struct {
	Title  string  `json:"title,omitempty"`
	Format string  `json:"format"` // pdf, pptx or docx
	Pages  []Page  `json:"pages"`
	Issues []Issue `json:"issues,omitempty"`
}

// VisionPages counts the pages a reader must look at instead of the text.
func (d Document) VisionPages() int {
	n := 0
	for _, p := range d.Pages {
		if p.Mode == ModeVision {
			n++
		}
	}
	return n
}

func (o Options) withDefaults() (Options, error) {
	if o.ImageDir == "" {
		return o, errors.New("image_dir_required")
	}
	if o.MaxPages <= 0 {
		o.MaxPages = 300
	}
	if o.ImageEdge <= 0 {
		o.ImageEdge = 1600
	}
	if o.MinText <= 0 {
		o.MinText = 30
	}
	return o, os.MkdirAll(o.ImageDir, 0o700)
}

// Read reads the file at path. The format comes from the extension.
func Read(ctx context.Context, path string, opts Options) (Document, error) {
	opts, err := opts.withDefaults()
	if err != nil {
		return Document{}, err
	}
	var doc Document
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		doc, err = readPDF(ctx, path, opts)
	case ".pptx":
		doc, err = readPPTX(ctx, path, opts)
	case ".docx":
		doc, err = readDOCX(ctx, path, opts)
	default:
		return Document{}, ErrUnsupported
	}
	if err != nil {
		return Document{}, err
	}
	if len(doc.Pages) == 0 {
		return Document{}, ErrNoPages
	}
	withImage := 0
	for _, p := range doc.Pages {
		if p.Image != "" {
			withImage++
		}
	}
	if withImage == 0 {
		return Document{}, ErrNoImages
	}
	return doc, nil
}

func tooManyPages(max int) error { return fmt.Errorf("document_exceeds_%d_pages", max) }

// classify sets a page's mode from the amount of readable text it has.
func classify(p *Page, minText int) {
	if len([]rune(strings.TrimSpace(p.Text))) < minText {
		p.Mode = ModeVision
	} else {
		p.Mode = ModeText
	}
}
