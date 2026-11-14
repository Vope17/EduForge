package style

import (
	"eduforge/internal/domain"
	"errors"
)

var allowed = map[string]bool{"tone": true, "density": true, "visual_ratio": true, "formula_explanation": true, "background": true, "foreground": true, "accent": true, "font": true, "ratio": true, "layout": true, "figure_style": true, "notes_tone": true, "detail": true, "fixed_elements": true, "decor": true, "title_color": true}

func Validate(s domain.Style) error {
	for key := range s {
		if !allowed[key] {
			return errors.New("invalid_style_field: " + key)
		}
	}
	return nil
}
func Defaults(kind string) domain.Style {
	out := domain.Style{"tone": "academic", "density": "balanced", "accent": "087F73", "background": "F5F4EE", "foreground": "19332F"}
	switch kind {
	case "A":
		out["formula_explanation"] = "symbols_and_intuition"
		out["visual_ratio"] = "balanced"
	case "B":
		// The default slide look is the built-in "Scholar" template.
		out["background"] = "FFFFFF"
		out["foreground"] = "1F2D3A"
		out["accent"] = "0F7B6C"
		out["decor"] = "bar"
		out["font"] = "Noto Sans CJK TC"
		out["ratio"] = "16:9"
		out["notes_tone"] = "spoken"
	}
	return out
}

// Resolve merges field by field; explicit nil, false and empty values are overrides.
func Resolve(kind string, common, specific, project, current domain.Style) (domain.Style, error) {
	out := Defaults(kind)
	for _, s := range []domain.Style{common, specific, project, current} {
		if err := Validate(s); err != nil {
			return nil, err
		}
		for key, value := range s {
			out[key] = value
		}
	}
	return out, nil
}
func Snapshot(selected []string, version domain.ProfileVersion, project, current map[string]domain.Style) (domain.StyleSnapshot, error) {
	out := domain.StyleSnapshot{Schema: 1, ProfileID: version.ProfileID, Resolved: map[string]domain.Style{}}
	for _, kind := range selected {
		resolved, err := Resolve(kind, version.Common, version.Outputs[kind], project[kind], current[kind])
		if err != nil {
			return out, err
		}
		out.Resolved[kind] = resolved
	}
	return out, nil
}
