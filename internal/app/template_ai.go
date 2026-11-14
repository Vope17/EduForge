package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"eduforge/internal/artifacts"
	"eduforge/internal/domain"
)

// AI editing of a template's design: the user selects parts of the spec
// (colour slots, fonts, layout boxes, Word styles, margins, header/footer)
// and gives an instruction; the model returns new values for those parts
// only. Proposals are validated against the spec rules and returned for
// review; nothing is saved here.

// SpecChange is one proposed change to a template spec.
type SpecChange struct {
	Key    string          `json:"key"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// specKeys lists the editable parts of a spec of the given kind.
func specKeys(spec artifacts.TemplateSpec) []string {
	if spec.Kind == "docx" {
		keys := []string{}
		for _, role := range artifacts.WordSpecRoles {
			keys = append(keys, "styles."+role)
		}
		if spec.Margins != nil {
			keys = append(keys, "margins")
		}
		if spec.Header != nil && spec.Header.Editable {
			keys = append(keys, "header")
		}
		if spec.Footer != nil && spec.Footer.Editable {
			keys = append(keys, "footer")
		}
		return keys
	}
	keys := []string{}
	for _, slot := range artifacts.ThemeSlots {
		keys = append(keys, "colors."+slot)
	}
	keys = append(keys, "heading_font", "heading_font_ea", "body_font", "body_font_ea", "title_box", "body_box", "cover_title_box", "cover_subtitle_box")
	return keys
}

// specValue reads one part of a spec as JSON (header/footer as their text).
func specValue(spec artifacts.TemplateSpec, key string) json.RawMessage {
	var v any
	switch {
	case strings.HasPrefix(key, "colors."):
		v = spec.Colors[strings.TrimPrefix(key, "colors.")]
	case strings.HasPrefix(key, "styles."):
		v = spec.Styles[strings.TrimPrefix(key, "styles.")]
	case key == "header":
		v = spec.Header.Text
	case key == "footer":
		v = spec.Footer.Text
	default:
		m := map[string]json.RawMessage{}
		_ = json.Unmarshal(domain.Encode(spec), &m)
		return m[key]
	}
	return domain.Encode(v)
}

// setSpecValue writes one part; objects (styles, margins) are merged so the
// model may send only the fields it changes.
func setSpecValue(spec *artifacts.TemplateSpec, key string, value json.RawMessage) error {
	switch {
	case strings.HasPrefix(key, "colors."):
		var c string
		if err := json.Unmarshal(value, &c); err != nil {
			return err
		}
		if spec.Colors == nil {
			spec.Colors = map[string]string{}
		}
		spec.Colors[strings.TrimPrefix(key, "colors.")] = c
	case strings.HasPrefix(key, "styles."):
		role := strings.TrimPrefix(key, "styles.")
		st := spec.Styles[role]
		if err := json.Unmarshal(value, &st); err != nil {
			return err
		}
		spec.Styles[role] = st
	case key == "margins":
		m := *spec.Margins
		if err := json.Unmarshal(value, &m); err != nil {
			return err
		}
		spec.Margins = &m
	case key == "header" || key == "footer":
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return err
		}
		hf := spec.Header
		if key == "footer" {
			hf = spec.Footer
		}
		copied := *hf
		copied.Text = text
		if key == "header" {
			spec.Header = &copied
		} else {
			spec.Footer = &copied
		}
	default:
		m := map[string]json.RawMessage{}
		if err := json.Unmarshal(domain.Encode(spec), &m); err != nil {
			return err
		}
		m[key] = value
		var next artifacts.TemplateSpec
		if err := json.Unmarshal(domain.Encode(m), &next); err != nil {
			return err
		}
		*spec = next
	}
	return nil
}

func cloneSpec(spec artifacts.TemplateSpec) artifacts.TemplateSpec {
	var out artifacts.TemplateSpec
	_ = json.Unmarshal(domain.Encode(spec), &out)
	return out
}

func templateEditPrompt(spec artifacts.TemplateSpec, name, instruction string, keys []string) string {
	var b strings.Builder
	kind := "PowerPoint slide template"
	if spec.Kind == "docx" {
		kind = "Word document template for reading guides"
	}
	fmt.Fprintf(&b, "You are EduForge's template design assistant. The user is editing the %s %q and selected some of its design settings. Apply the user's instruction by proposing new values for the selected settings only.\n", kind, name)
	b.WriteString("Rules:\n- Change only the selected keys; leave a key out to keep it.\n- Colours are 6-digit hex without #.\n")
	if spec.Kind == "docx" {
		b.WriteString("- A style is {\"font\": Latin font, \"font_ea\": CJK font, \"size\": points, \"color\": hex or \"\" for automatic, \"bold\": true/false, \"before\": points, \"after\": points}; send only the fields you change.\n- margins are millimetres {\"top\",\"bottom\",\"left\",\"right\"}. header and footer are plain text (\\n separates lines).\n")
	} else {
		b.WriteString("- Fonts are font family names; *_ea fonts are for Chinese text (e.g. Noto Sans CJK TC, 微軟正黑體).\n- Boxes are [x, y, width, height] as fractions of the slide (0–1); keep them on the slide (x+width ≤ 1, y+height ≤ 1, width and height ≥ 0.05) and keep titles above content.\n- dk1 is the main text colour, lt1 the background, accent1 the main highlight; keep enough contrast between text and background.\n")
	}
	fmt.Fprintf(&b, "\nInstruction from the user:\n%s\n", strings.TrimSpace(instruction))
	b.WriteString("\nCurrent design (for context):\n")
	for _, key := range specKeys(spec) {
		mark := ""
		for _, k := range keys {
			if k == key {
				mark = "  ← selected"
			}
		}
		fmt.Fprintf(&b, "%s = %s%s\n", key, specValue(spec, key), mark)
	}
	b.WriteString("\nReply with JSON only, no prose and no code fence:\n{\"changes\":{\"<selected key>\":<new value>, ...}}")
	return b.String()
}

// SuggestTemplateEdits asks a model to change the selected parts of a
// template spec (the editor's working copy) and returns validated changes.
func (a *App) SuggestTemplateEdits(ctx context.Context, owner, id, instruction string, spec artifacts.TemplateSpec, selection []string) ([]SpecChange, error) {
	_, t, err := get[domain.Template](ctx, a, owner, id, "template")
	if err != nil {
		return nil, err
	}
	instruction = strings.TrimSpace(instruction)
	if instruction == "" || utf8.RuneCountInString(instruction) > 2000 {
		return nil, errors.New("edit_instruction_required")
	}
	if spec.Kind != t.Kind && !(spec.Kind == "pptx" && t.Kind == "") {
		return nil, errors.New("invalid_template_spec")
	}
	check := cloneSpec(spec)
	if err := check.Normalize(); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, k := range specKeys(spec) {
		allowed[k] = true
	}
	if len(selection) == 0 || len(selection) > 40 {
		return nil, errors.New("edit_selection_required")
	}
	for _, k := range selection {
		if !allowed[k] {
			return nil, errors.New("invalid_edit_block")
		}
	}
	sort.Strings(selection)
	refs, err := a.ProviderRefs(ctx, owner)
	if err != nil {
		return nil, err
	}
	var changes []SpecChange
	_, _, err = a.Providers.Complete(ctx, owner, refs, templateEditPrompt(spec, t.Name, instruction, selection), func(output string) error {
		parsed, err := parseSpecChanges(output, spec, selection)
		if err == nil {
			changes = parsed
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// parseSpecChanges keeps the model's changes to selected keys that leave the
// spec valid, each checked on its own.
func parseSpecChanges(output string, spec artifacts.TemplateSpec, selection []string) ([]SpecChange, error) {
	start, end := strings.Index(output, "{"), strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return nil, errors.New("invalid_model_json")
	}
	var reply struct {
		Changes map[string]json.RawMessage `json:"changes"`
	}
	if err := json.Unmarshal([]byte(output[start:end+1]), &reply); err != nil || len(reply.Changes) == 0 {
		return nil, errors.New("invalid_model_json")
	}
	out := []SpecChange{}
	for _, key := range selection {
		value, ok := reply.Changes[key]
		if !ok {
			continue
		}
		next := cloneSpec(spec)
		if err := setSpecValue(&next, key, value); err != nil {
			continue
		}
		if err := next.Normalize(); err != nil {
			continue
		}
		before, after := specValue(spec, key), specValue(next, key)
		var b, a any
		_ = json.Unmarshal(before, &b)
		_ = json.Unmarshal(after, &a)
		if reflect.DeepEqual(a, b) {
			continue
		}
		out = append(out, SpecChange{Key: key, Before: before, After: after})
	}
	if len(out) == 0 {
		return nil, errors.New("no_valid_changes")
	}
	return out, nil
}
