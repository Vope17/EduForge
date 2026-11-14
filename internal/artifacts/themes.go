package artifacts

import (
	"fmt"
	"strconv"

	"eduforge/internal/domain"
)

// Slides use either the default look (drawn here: colours plus a title
// decoration) or an imported PowerPoint template, whose own masters and
// layouts provide the look.

// DefaultStyle is the slide look without an imported template.
func DefaultStyle() domain.Style {
	return domain.Style{"background": "FFFFFF", "foreground": "1F2D3A", "accent": "0F7B6C", "decor": "bar", "font": "Noto Sans CJK TC", "ratio": "16:9"}
}

// WordStyleKeys are the style keys a Word template sets on a reader.
var WordStyleKeys = []string{"word_template", "doc_font", "doc_heading_font", "doc_heading_color", "doc_text_color"}

// TemplateStyle is the style an imported template applies: a Word template
// to readers, a PowerPoint template to slides.
func TemplateStyle(t domain.Template) domain.Style {
	if t.Kind == "docx" {
		return domain.Style{"word_template": t.File, "doc_font": t.Font, "doc_heading_font": t.HeadingFont, "doc_heading_color": t.TitleColor, "doc_text_color": t.Foreground}
	}
	if t.File == "" {
		style := DefaultStyle()
		for key, value := range map[string]string{"background": t.Background, "foreground": t.Foreground, "accent": t.Accent, "font": t.Font, "ratio": t.Ratio, "decor": t.Decor, "title_color": t.TitleColor} {
			if value != "" {
				style[key] = value
			}
		}
		return style
	}
	return domain.Style{
		"template_file": t.File, "decor": "none", "ratio": t.Ratio,
		"background": t.Background, "foreground": t.Foreground, "accent": t.Accent, "title_color": t.TitleColor,
		"font": t.Font, "heading_font": t.HeadingFont,
		"slide_width": t.Width, "slide_height": t.Height,
		"cover_layout": t.CoverLayout, "content_layout": t.ContentLayout,
		"cover_placeholder": t.CoverPlaceholder, "cover_subtitle_idx": t.CoverSubtitleIdx, "content_number_idx": t.ContentNumberIdx,
		"title_box": t.TitleBox, "body_box": t.BodyBox, "cover_title_box": t.CoverTitleBox, "cover_subtitle_box": t.CoverSubtitleBox,
		"title_align": t.TitleAlign, "cover_align": t.CoverAlign,
		"cover_image": t.CoverImage, "content_image": t.ContentImage,
	}
}

func str(style domain.Style, key string) string { v, _ := style[key].(string); return v }

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func box(style domain.Style, key string) ([4]float64, bool) {
	var out [4]float64
	switch v := style[key].(type) {
	case []float64:
		if len(v) == 4 {
			copy(out[:], v)
			return out, true
		}
	case []any:
		if len(v) == 4 {
			for i := range v {
				out[i] = number(v[i])
			}
			return out, true
		}
	}
	return out, false
}

// theme is a resolved presentation style.
type theme struct {
	bg, fg, accent, title, muted, line string
	font, decor                        string
	// Imported template.
	file, coverLayout, contentLayout      string
	coverPlaceholder, subtitleIdx, numIdx string
	width, height                         int64
}

func (t theme) template() bool { return t.file != "" }

func themeOf(style domain.Style) theme {
	t := theme{bg: color(style, "background", "FFFFFF"), fg: color(style, "foreground", "1F2D3A"), accent: color(style, "accent", "0F7B6C")}
	t.muted = mix(t.fg, t.bg, 0.45)
	t.line = mix(t.fg, t.bg, 0.85)
	t.font = str(style, "font")
	if t.font == "" {
		t.font = "Noto Sans CJK TC"
	}
	t.decor = str(style, "decor")
	switch t.decor {
	case "bar", "line", "rule", "band", "none":
	default:
		t.decor = "bar"
	}
	t.title = color(style, "title_color", t.fg)
	if t.decor == "band" {
		t.title = color(style, "title_color", "FFFFFF")
	}
	if t.file = str(style, "template_file"); t.file != "" {
		t.decor = "none"
		t.coverLayout, t.contentLayout = str(style, "cover_layout"), str(style, "content_layout")
		t.coverPlaceholder, t.subtitleIdx, t.numIdx = firstNonEmpty(str(style, "cover_placeholder"), "title"), str(style, "cover_subtitle_idx"), str(style, "content_number_idx")
		t.width, t.height = int64(number(style["slide_width"])), int64(number(style["slide_height"]))
	}
	return t
}

// mix blends hex colour a toward b by f (0 = a, 1 = b).
func mix(a, b string, f float64) string {
	ch := func(s string, i int) float64 { v, _ := strconv.ParseUint(s[i:i+2], 16, 8); return float64(v) }
	out := ""
	for i := 0; i < 6; i += 2 {
		out += fmt.Sprintf("%02X", int(ch(a, i)*(1-f)+ch(b, i)*f+0.5))
	}
	return out
}

// decorRect is a fixed decoration in slide fractions.
type decorRect struct {
	X, Y, W, H float64
	Color      string
}

// slideKind tells cover/closing slides (a "display" text box) from content.
// Decks made before display boxes existed placed the cover title mid-slide.
func slideKind(sec domain.Section) string {
	for _, el := range sec.Layout {
		if el.Role == "display" || (el.Role == "title" && el.Y >= 0.25) {
			return "display"
		}
	}
	return "content"
}

// decorations returns the default look's fixed shapes for a slide kind.
func (t theme) decorations(kind string) []decorRect {
	if t.decor == "none" {
		return nil
	}
	if kind == "display" {
		if t.decor == "band" {
			return []decorRect{{0, 0.80, 1, 0.20, t.accent}}
		}
		return []decorRect{{0, 0, 0.025, 1, t.accent}}
	}
	switch t.decor {
	case "band":
		return []decorRect{{0, 0, 1, 0.19, t.accent}}
	case "line":
		return []decorRect{{0.06, 0.175, 0.07, 0.008, t.accent}}
	case "rule":
		return []decorRect{{0.06, 0.18, 0.88, 0.003, t.accent}, {0.06, 0.93, 0.88, 0.0015, t.line}}
	default: // bar
		return []decorRect{{0.04, 0.065, 0.007, 0.09, t.accent}, {0.06, 0.195, 0.88, 0.002, t.line}}
	}
}

// textColor is the colour of a text box by its role.
func (t theme) textColor(role, kind string) string {
	switch role {
	case "title":
		if kind == "content" || t.template() {
			return t.title
		}
		return t.fg
	case "display":
		if t.template() {
			return t.title
		}
		return t.fg
	case "caption", "subtitle":
		return t.muted
	case "kicker":
		return t.accent
	}
	return t.fg
}

// Geometry is where a deck's titles and content go, in slide fractions, and
// how font sizes scale with the slide's physical size.
type Geometry struct {
	Title, Body, CoverTitle, CoverSub [4]float64
	TitleAlign, CoverAlign            string
	Scale                             float64
	Template                          bool
}

// DefaultGeometry is the default look's layout on a 16:9 slide.
func DefaultGeometry() Geometry {
	return Geometry{
		Title: [4]float64{0.06, 0.035, 0.88, 0.12}, Body: [4]float64{0.06, 0.23, 0.88, 0.68},
		CoverTitle: [4]float64{0.08, 0.31, 0.84, 0.32}, CoverSub: [4]float64{0.08, 0.66, 0.84, 0.12},
		Scale: 1,
	}
}

// GeometryOf reads a style's geometry; styles without a template use the default.
func GeometryOf(style domain.Style) Geometry {
	g := DefaultGeometry()
	if style["ratio"] == "4:3" {
		g.Scale = 0.75
	}
	if str(style, "template_file") == "" {
		return g
	}
	g.Template = true
	if v, ok := box(style, "title_box"); ok {
		g.Title = v
	}
	if v, ok := box(style, "body_box"); ok {
		g.Body = v
	}
	if v, ok := box(style, "cover_title_box"); ok {
		g.CoverTitle = v
	}
	if v, ok := box(style, "cover_subtitle_box"); ok {
		g.CoverSub = v
	}
	g.TitleAlign, g.CoverAlign = str(style, "title_align"), str(style, "cover_align")
	if w := number(style["slide_width"]); w > 0 {
		g.Scale = w / 12192000
	}
	return g
}

// in places a box given relative to region r (all fractions).
func in(r [4]float64, x, y, w, h float64) (float64, float64, float64, float64) {
	return r[0] + x*r[2], r[1] + y*r[3], w * r[2], h * r[3]
}

// RemapDeck moves a deck's boxes from one geometry to another, keeping each
// box's place relative to the title, content and cover areas.
func RemapDeck(sections []domain.Section, from, to Geometry) {
	ratio := to.Scale / from.Scale
	mapBox := func(el *domain.Element, a, b [4]float64) {
		fx := func(v, a0, aw, b0, bw float64) float64 {
			if aw == 0 {
				return b0
			}
			return b0 + (v-a0)/aw*bw
		}
		clamp := func(v float64) float64 { return min(max(v, 0), 1) }
		x0, y0 := clamp(fx(el.X, a[0], a[2], b[0], b[2])), clamp(fx(el.Y, a[1], a[3], b[1], b[3]))
		x1, y1 := clamp(fx(el.X+el.W, a[0], a[2], b[0], b[2])), clamp(fx(el.Y+el.H, a[1], a[3], b[1], b[3]))
		el.X, el.Y, el.W, el.H = x0, y0, max(x1-x0, 0.02), max(y1-y0, 0.02)
	}
	for i := range sections {
		kind := slideKind(sections[i])
		for j := range sections[i].Layout {
			el := &sections[i].Layout[j]
			if el.Size > 0 {
				el.Size = float64(int(el.Size*ratio*2+0.5)) / 2
			}
			switch {
			case kind == "content" && el.Role == "title":
				mapBox(el, from.Title, to.Title)
				el.Align = to.TitleAlign
			case kind == "display" && (el.Role == "display" || el.Role == "title"):
				mapBox(el, from.CoverTitle, to.CoverTitle)
				if to.Template && to.CoverAlign != "" {
					el.Align = to.CoverAlign
				}
			case kind == "display" && el.Role == "subtitle":
				mapBox(el, from.CoverSub, to.CoverSub)
			case kind == "display":
				mapBox(el, from.CoverTitle, to.CoverTitle)
			default:
				mapBox(el, from.Body, to.Body)
			}
		}
	}
}
