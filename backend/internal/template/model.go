package template

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"eduforge/backend/internal/artifact"
)

type State string

const (
	Requested            State = "requested"
	TemplateGenerated    State = "template_generated"
	AwaitingConfirmation State = "awaiting_confirmation"
	Confirmed            State = "confirmed"
	Generating           State = "generating"
	Ready                State = "ready"
)

var ErrInvalid = errors.New("invalid template")

type Spec struct {
	Palette string           `json:"palette"`
	Font    string           `json:"font"`
	Layout  string           `json:"layout"`
	Tone    string           `json:"tone"`
	Density string           `json:"density"`
	Example []artifact.Block `json:"example"`
}

type Request struct {
	Type             artifact.Type
	SlideCount       int
	ID               string
	State            State
	Language         artifact.Language
	Topic            string
	Audience         string
	Kind             string
	Version          int
	ConfirmedVersion int
	Spec             *Spec
}

func (s Spec) Validate() error { return s.ValidateFor(artifact.DocumentType) }

func (s Spec) ValidateFor(kind artifact.Type) error {
	if !slices.Contains([]string{"forest", "ocean", "warm"}, s.Palette) ||
		!slices.Contains([]string{"sans", "serif"}, s.Font) ||
		!slices.Contains([]string{"friendly", "formal"}, s.Tone) ||
		!slices.Contains([]string{"compact", "balanced", "detailed"}, s.Density) ||
		len(s.Example) < 1 || len(s.Example) > 3 {
		return ErrInvalid
	}
	if kind == artifact.SlidesType {
		if !slices.Contains([]string{"title_body", "two_columns", "image_text"}, s.Layout) || len(s.Example) != 1 {
			return ErrInvalid
		}
		page := s.Example[0]
		if page.Kind != "slide" || page.Layout != s.Layout || len(page.Children) > 3 ||
			utf8.RuneCountInString(page.Notes) > 300 || page.Validate() != nil {
			return ErrInvalid
		}
		for _, block := range artifact.FlattenBlocks(s.Example) {
			if block.ID != "" || block.ImageURL != "" || utf8.RuneCountInString(block.Text) > 300 {
				return ErrInvalid
			}
		}
		return nil
	}
	if kind != artifact.DocumentType || !slices.Contains([]string{"sections", "worksheet"}, s.Layout) {
		return ErrInvalid
	}
	for _, block := range s.Example {
		if block.ID != "" || block.Kind != "heading" && block.Kind != "paragraph" ||
			strings.TrimSpace(block.Text) == "" || utf8.RuneCountInString(block.Text) > 300 || block.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}
