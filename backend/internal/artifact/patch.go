package artifact

import (
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

var ErrSelection = errors.New("invalid or out-of-scope selection patch")

type Anchor struct {
	ArtifactID string `json:"artifact_id"`
	BlockID    string `json:"block_id"`
	Field      string `json:"field,omitempty"`
	Start      *int   `json:"start,omitempty"`
	End        *int   `json:"end,omitempty"`
}

type Segment struct {
	BlockID string `json:"block_id"`
	Field   string `json:"field"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
}

type Edit struct {
	BlockID     string `json:"block_id"`
	Field       string `json:"field"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Replacement string `json:"replacement"`
}

func findBlock(blocks []Block, id string) *Block {
	for i := range blocks {
		if blocks[i].ID == id {
			return &blocks[i]
		}
		if child := findBlock(blocks[i].Children, id); child != nil {
			return child
		}
	}
	return nil
}

func textField(block *Block, field string) *string {
	if field == "text" && block.Kind != "slide" {
		return &block.Text
	}
	if field == "notes" && block.Kind == "slide" {
		return &block.Notes
	}
	if block.Kind == "list" && strings.HasPrefix(field, "items.") {
		value := strings.TrimPrefix(field, "items.")
		index, err := strconv.Atoi(value)
		if err == nil && index >= 0 && index < len(block.Items) && strconv.Itoa(index) == value {
			return &block.Items[index]
		}
	}
	return nil
}

func utf16Range(value string, start, end int) (prefix, selected, suffix string, err error) {
	units := utf16.Encode([]rune(value))
	if start < 0 || end < start || end > len(units) {
		return "", "", "", ErrSelection
	}
	for _, offset := range []int{start, end} {
		if offset > 0 && offset < len(units) && utf16.IsSurrogate(rune(units[offset])) &&
			units[offset] >= 0xdc00 && units[offset-1] >= 0xd800 && units[offset-1] <= 0xdbff {
			return "", "", "", ErrSelection
		}
	}
	return string(utf16.Decode(units[:start])), string(utf16.Decode(units[start:end])), string(utf16.Decode(units[end:])), nil
}

func ResolveSelection(id string, content Content, anchors []Anchor) ([]Segment, error) {
	if len(anchors) == 0 || len(anchors) > 50 {
		return nil, ErrSelection
	}
	segments := make([]Segment, 0)
	for _, anchor := range anchors {
		if anchor.ArtifactID != id {
			return nil, ErrSelection
		}
		block := findBlock(content.Blocks, anchor.BlockID)
		if block == nil {
			return nil, ErrSelection
		}
		fields := []string{anchor.Field}
		if anchor.Field == "" {
			fields = []string{"text"}
			if block.Kind == "slide" {
				fields = []string{"notes"}
			} else if block.Kind == "list" && anchor.Start == nil && anchor.End == nil {
				for i := range block.Items {
					fields = append(fields, "items."+strconv.Itoa(i))
				}
			}
		}
		for _, field := range fields {
			value := textField(block, field)
			if value == nil || (anchor.Start == nil) != (anchor.End == nil) {
				return nil, ErrSelection
			}
			start, end := 0, len(utf16.Encode([]rune(*value)))
			if anchor.Start != nil {
				start, end = *anchor.Start, *anchor.End
				if start >= end {
					return nil, ErrSelection
				}
			}
			_, text, _, err := utf16Range(*value, start, end)
			if err != nil {
				return nil, err
			}
			segments = append(segments, Segment{BlockID: block.ID, Field: field, Start: start, End: end, Text: text})
		}
	}
	sort.Slice(segments, func(i, j int) bool {
		a, b := segments[i], segments[j]
		if a.BlockID != b.BlockID {
			return a.BlockID < b.BlockID
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Start < b.Start
	})
	merged := make([]Segment, 0)
	for _, segment := range segments {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if last.BlockID == segment.BlockID && last.Field == segment.Field && segment.Start <= last.End {
				if segment.End > last.End {
					last.End = segment.End
				}
				continue
			}
		}
		merged = append(merged, segment)
	}
	for i := range merged {
		value := textField(findBlock(content.Blocks, merged[i].BlockID), merged[i].Field)
		_, text, _, err := utf16Range(*value, merged[i].Start, merged[i].End)
		if err != nil {
			return nil, err
		}
		merged[i].Text = text
	}
	if len(merged) > 100 {
		return nil, ErrSelection
	}
	return merged, nil
}

func (c Content) Clone() Content {
	result := c
	result.Blocks = slices.Clone(c.Blocks)
	for i := range result.Blocks {
		block := &result.Blocks[i]
		block.Items = slices.Clone(block.Items)
		block.Rows = slices.Clone(block.Rows)
		for j := range block.Rows {
			block.Rows[j] = slices.Clone(block.Rows[j])
		}
		block.Children = (Content{Blocks: block.Children}).Clone().Blocks
	}
	return result
}

func ApplyEdits(id string, kind Type, content Content, anchors []Anchor, edits []Edit) (Content, error) {
	segments, err := ResolveSelection(id, content, anchors)
	if err != nil || len(edits) == 0 || len(edits) > 100 {
		return Content{}, ErrSelection
	}
	seen := make(map[int]bool)
	for _, edit := range edits {
		match := -1
		for i, segment := range segments {
			if segment.BlockID == edit.BlockID && segment.Field == edit.Field && segment.Start == edit.Start && segment.End == edit.End {
				match = i
				break
			}
		}
		if match == -1 || seen[match] {
			return Content{}, ErrSelection
		}
		seen[match] = true
	}
	ordered := append([]Edit(nil), edits...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.BlockID != b.BlockID {
			return a.BlockID < b.BlockID
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Start > b.Start
	})
	result := content.Clone()
	for _, edit := range ordered {
		field := textField(findBlock(result.Blocks, edit.BlockID), edit.Field)
		prefix, _, suffix, err := utf16Range(*field, edit.Start, edit.End)
		if err != nil {
			return Content{}, err
		}
		*field = prefix + edit.Replacement + suffix
	}
	if err := result.ValidateFor(kind); err != nil {
		return Content{}, err
	}
	return result, nil
}
