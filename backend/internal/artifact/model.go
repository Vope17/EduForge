package artifact

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

type Type string

const (
	DocumentType Type = "document"
	SlidesType   Type = "slides"
	MaxSlides         = 12
)

func (t Type) Valid() bool { return t == DocumentType || t == SlidesType }

type Language string

const (
	TraditionalChinese Language = "zh-TW"
	English            Language = "en"
	MaxBlocks                   = 80
)

var ErrInvalid = errors.New("invalid document")

func (l Language) Valid() bool {
	return l == TraditionalChinese || l == English
}

type Block struct {
	ID       string     `json:"id,omitempty"`
	Kind     string     `json:"kind"`
	Text     string     `json:"text"`
	Items    []string   `json:"items,omitempty"`
	Rows     [][]string `json:"rows,omitempty"`
	ImageURL string     `json:"image_url,omitempty"`
	Children []Block    `json:"children,omitempty"`
	Notes    string     `json:"notes,omitempty"`
	Layout   string     `json:"layout,omitempty"`
}

type Content struct {
	Title  string  `json:"title"`
	Blocks []Block `json:"blocks"`
}

type Artifact struct {
	Type     Type
	ID       string
	Language Language
	Revision int
	Content  Content
	CanUndo  bool
	CanRedo  bool
}

func NewID() string {
	var data [16]byte
	// crypto/rand.Read fills the buffer or terminates the process.
	rand.Read(data[:])
	return hex.EncodeToString(data[:])
}

func (c Content) Validate() error { return c.ValidateFor(DocumentType) }

func (c Content) ValidateFor(kind Type) error {
	if strings.TrimSpace(c.Title) == "" || utf8.RuneCountInString(c.Title) > 160 || len(c.Blocks) == 0 || len(c.Blocks) > MaxBlocks {
		return ErrInvalid
	}
	if !kind.Valid() || kind == SlidesType && len(c.Blocks) > MaxSlides {
		return ErrInvalid
	}
	for i, block := range c.Blocks {
		if kind == SlidesType && block.Kind != "slide" || kind == DocumentType && block.Kind == "slide" {
			return ErrInvalid
		}
		if err := block.Validate(); err != nil {
			return fmt.Errorf("block %d: %w", i+1, err)
		}
	}
	return nil
}

func (b Block) Validate() error {
	if utf8.RuneCountInString(b.Text) > 2500 {
		return ErrInvalid
	}
	if b.Kind != "slide" && (len(b.Children) != 0 || b.Notes != "" || b.Layout != "") {
		return ErrInvalid
	}
	switch b.Kind {
	case "slide":
		return b.validateSlide()
	case "heading", "paragraph":
		if len(b.Items) != 0 || len(b.Rows) != 0 || b.ImageURL != "" {
			return ErrInvalid
		}
	case "list":
		if len(b.Items) == 0 || len(b.Items) > 20 || !validCells(b.Items) || len(b.Rows) != 0 || b.ImageURL != "" {
			return ErrInvalid
		}
	case "table":
		if len(b.Rows) == 0 || len(b.Rows) > 20 || len(b.Items) != 0 || b.ImageURL != "" {
			return ErrInvalid
		}
		width := len(b.Rows[0])
		if width == 0 || width > 8 {
			return ErrInvalid
		}
		for _, row := range b.Rows {
			if len(row) != width || !validCells(row) {
				return ErrInvalid
			}
		}
	case "image":
		parsed, err := url.Parse(b.ImageURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || len(b.ImageURL) > 2048 ||
			len(b.Items) != 0 || len(b.Rows) != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validCells(values []string) bool {
	for _, value := range values {
		if utf8.RuneCountInString(value) > 400 {
			return false
		}
	}
	return true
}

// FlattenBlocks returns pages and their elements in source order.
func FlattenBlocks(blocks []Block) []Block {
	result := make([]Block, 0)
	for _, block := range blocks {
		result = append(result, block)
		result = append(result, FlattenBlocks(block.Children)...)
	}
	return result
}

// AssignIDs preserves existing block identities and assigns IDs to new blocks.
func (c *Content) AssignIDs(current []Block) error {
	known := make(map[string]string)
	for _, block := range FlattenBlocks(current) {
		known[block.ID] = block.Kind
	}
	var assign func([]Block) error
	assign = func(blocks []Block) error {
		for i := range blocks {
			block := &blocks[i]
			if block.ID == "" {
				block.ID = NewID()
			} else {
				if known[block.ID] != block.Kind {
					return ErrInvalid
				}
				delete(known, block.ID)
			}
			if err := assign(block.Children); err != nil {
				return err
			}
		}
		return nil
	}
	return assign(c.Blocks)
}

func (b Block) validateSlide() error {
	if b.Text != "" || len(b.Items) != 0 || len(b.Rows) != 0 || b.ImageURL != "" ||
		len(b.Children) < 2 || len(b.Children) > 8 || utf8.RuneCountInString(b.Notes) > 2500 {
		return ErrInvalid
	}
	switch b.Layout {
	case "title_body", "two_columns", "image_text":
	default:
		return ErrInvalid
	}
	titles := 0
	for _, child := range b.Children {
		switch child.Kind {
		case "heading":
			titles++
			if utf8.RuneCountInString(child.Text) > 120 {
				return ErrInvalid
			}
		case "paragraph":
			if utf8.RuneCountInString(child.Text) > 800 {
				return ErrInvalid
			}
		case "list":
			if len(child.Items) > 8 {
				return ErrInvalid
			}
		case "image":
			// An empty URL is an editable image placeholder in a slide.
			if child.ImageURL == "" && child.Text != "" && len(child.Children) == 0 &&
				len(child.Items) == 0 && len(child.Rows) == 0 && child.Notes == "" && child.Layout == "" &&
				utf8.RuneCountInString(child.Text) <= 300 {
				continue
			}
		default:
			return ErrInvalid
		}
		if err := child.Validate(); err != nil {
			return err
		}
	}
	if titles != 1 {
		return ErrInvalid
	}
	return nil
}
