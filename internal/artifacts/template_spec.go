package artifacts

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"eduforge/internal/domain"
)

// A template's editable design ("spec"). It is read from the template file
// and written back into it, so downloads built on the template change too.
// Only fields that differ from the file are written, leaving everything else
// (theme links, other styles, pictures) as the author made it.

// TemplateSpec is the editable design of an imported PowerPoint or Word template.
type TemplateSpec struct {
	Kind string `json:"kind"` // pptx or docx

	// PowerPoint: theme colour slots, theme fonts (Latin and East Asian) and
	// where titles and content go, as fractions x, y, w, h of the slide.
	Colors           map[string]string `json:"colors,omitempty"`
	HeadingFont      string            `json:"heading_font,omitempty"`
	HeadingFontEA    string            `json:"heading_font_ea,omitempty"`
	BodyFont         string            `json:"body_font,omitempty"`
	BodyFontEA       string            `json:"body_font_ea,omitempty"`
	TitleBox         []float64         `json:"title_box,omitempty"`
	BodyBox          []float64         `json:"body_box,omitempty"`
	CoverTitleBox    []float64         `json:"cover_title_box,omitempty"`
	CoverSubtitleBox []float64         `json:"cover_subtitle_box,omitempty"`

	// Word: paragraph styles by role, page margins and header/footer text.
	Styles  map[string]WordStyleSpec `json:"styles,omitempty"`
	Margins *Margins                 `json:"margins,omitempty"`
	Header  *HeaderFooter            `json:"header,omitempty"`
	Footer  *HeaderFooter            `json:"footer,omitempty"`
}

// WordStyleSpec is the look of one Word paragraph style.
type WordStyleSpec struct {
	Font   string  `json:"font"`    // Latin font
	FontEA string  `json:"font_ea"` // East Asian font
	Size   float64 `json:"size"`    // points
	Color  string  `json:"color"`   // hex; empty is automatic
	Bold   bool    `json:"bold"`
	Before float64 `json:"before"` // space before, points
	After  float64 `json:"after"`  // space after, points
}

// Margins are page margins in millimetres.
type Margins struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

// HeaderFooter is the text of the default page header or footer.
type HeaderFooter struct {
	Text string `json:"text"`
	// Editable is false when it holds pictures, fields or tables, which only
	// Word itself should change.
	Editable bool `json:"editable"`
}

// ThemeSlots are a PowerPoint theme's colour slots.
var ThemeSlots = []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2", "accent3", "accent4", "accent5", "accent6"}

// WordSpecRoles are the Word paragraph styles a template spec edits.
var WordSpecRoles = []string{"title", "subtitle", "h1", "h2", "normal", "caption"}

var hexColor = regexp.MustCompile(`^[0-9A-F]{6}$`)

func validFont(name string) bool {
	return utf8.RuneCountInString(name) <= 64 && !strings.ContainsAny(name, "<>&\"\n\r\t")
}

func validBox(b []float64) bool {
	if b == nil {
		return true
	}
	if len(b) != 4 {
		return false
	}
	for _, v := range b {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b[0] >= 0 && b[1] >= 0 && b[2] >= 0.03 && b[3] >= 0.03 && b[0]+b[2] <= 1.001 && b[1]+b[3] <= 1.001
}

// Normalize checks a spec and canonicalises colours to upper-case hex.
func (s *TemplateSpec) Normalize() error {
	bad := errors.New("invalid_template_spec")
	for slot, c := range s.Colors {
		c = strings.ToUpper(strings.TrimPrefix(c, "#"))
		if !hexColor.MatchString(c) || !contains(ThemeSlots, slot) {
			return bad
		}
		s.Colors[slot] = c
	}
	for _, f := range []string{s.HeadingFont, s.HeadingFontEA, s.BodyFont, s.BodyFontEA} {
		if !validFont(f) {
			return bad
		}
	}
	for _, b := range [][]float64{s.TitleBox, s.BodyBox, s.CoverTitleBox, s.CoverSubtitleBox} {
		if !validBox(b) {
			return bad
		}
	}
	for role, st := range s.Styles {
		st.Color = strings.ToUpper(strings.TrimPrefix(st.Color, "#"))
		if !contains(WordSpecRoles, role) || !validFont(st.Font) || !validFont(st.FontEA) || (st.Color != "" && !hexColor.MatchString(st.Color)) ||
			st.Size < 0 || st.Size > 200 || st.Before < 0 || st.Before > 500 || st.After < 0 || st.After > 500 {
			return bad
		}
		s.Styles[role] = st
	}
	if m := s.Margins; m != nil {
		for _, v := range []float64{m.Top, m.Bottom, m.Left, m.Right} {
			if math.IsNaN(v) || v < 0 || v > 120 {
				return bad
			}
		}
	}
	for _, hf := range []*HeaderFooter{s.Header, s.Footer} {
		if hf != nil && utf8.RuneCountInString(hf.Text) > 1000 {
			return bad
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ReadTemplateSpec reads the editable design of a template file. t supplies
// what the file alone does not record (the content box EduForge uses).
func ReadTemplateSpec(file string, t domain.Template) (TemplateSpec, error) {
	if t.Kind == "docx" {
		return readWordSpec(file)
	}
	return readSlidesSpec(file, t)
}

// WriteTemplateSpec returns the template file with the spec applied.
func WriteTemplateSpec(file string, t domain.Template, spec TemplateSpec) ([]byte, error) {
	if err := spec.Normalize(); err != nil {
		return nil, err
	}
	if t.Kind == "docx" {
		return writeWordSpec(file, spec)
	}
	return writeSlidesSpec(file, t, spec)
}

// ---- PowerPoint ----

func themeSlot(theme, slot string) string {
	m := regexp.MustCompile(`(?s)<a:` + slot + `>(.*?)</a:` + slot + `>`).FindStringSubmatch(theme)
	if m == nil {
		return ""
	}
	if c := regexp.MustCompile(`(?:val|lastClr)="([0-9A-Fa-f]{6})"`).FindAllStringSubmatch(m[1], -1); c != nil {
		return strings.ToUpper(c[len(c)-1][1])
	}
	return ""
}

func themeFont(theme, kind, script string) string {
	block := regexp.MustCompile(`(?s)<a:` + kind + `>.*?</a:` + kind + `>`).FindString(theme)
	if m := regexp.MustCompile(`<a:` + script + ` typeface="([^"]*)"`).FindStringSubmatch(block); m != nil {
		return m[1]
	}
	return ""
}

func readSlidesSpec(file string, t domain.Template) (TemplateSpec, error) {
	p, err := openTemplate(file)
	if err != nil {
		return TemplateSpec{}, err
	}
	theme := string(p.parts[p.theme])
	spec := TemplateSpec{Kind: "pptx", Colors: map[string]string{}}
	for _, slot := range ThemeSlots {
		if c := themeSlot(theme, slot); c != "" {
			spec.Colors[slot] = c
		}
	}
	spec.HeadingFont, spec.HeadingFontEA = themeFont(theme, "majorFont", "latin"), themeFont(theme, "majorFont", "ea")
	spec.BodyFont, spec.BodyFontEA = themeFont(theme, "minorFont", "latin"), themeFont(theme, "minorFont", "ea")
	spec.TitleBox, spec.BodyBox, spec.CoverTitleBox, spec.CoverSubtitleBox = t.TitleBox, t.BodyBox, t.CoverTitleBox, t.CoverSubtitleBox
	return spec, nil
}

func setThemeFont(theme, kind, script, font string) string {
	block := regexp.MustCompile(`(?s)<a:` + kind + `>.*?</a:` + kind + `>`)
	return block.ReplaceAllStringFunc(theme, func(b string) string {
		tag := regexp.MustCompile(`<a:` + script + ` typeface="[^"]*"`)
		if tag.MatchString(b) {
			return tag.ReplaceAllLiteralString(b, `<a:`+script+` typeface="`+x(font)+`"`)
		}
		// latin is required first; ea follows it.
		if script == "ea" {
			return regexp.MustCompile(`(<a:latin\b[^>]*/>)`).ReplaceAllString(b, `${1}<a:ea typeface="`+x(font)+`"/>`)
		}
		return b
	})
}

var spXfrmBlock = regexp.MustCompile(`(?s)<a:xfrm\b[^>]*>.*?</a:xfrm>`)
var spPrEmpty = regexp.MustCompile(`<p:spPr\b[^>]*/>`)
var spPrOpen = regexp.MustCompile(`<p:spPr\b[^>]*>`)

// setPlaceholderBox moves the first placeholder of the given types in a
// layout to box (fractions of the slide).
func setPlaceholderBox(layout string, types []string, box []float64, w, h int64) string {
	done := false
	return spPattern.ReplaceAllStringFunc(layout, func(sp string) string {
		ph := phPattern.FindString(sp)
		if done || ph == "" {
			return sp
		}
		typ := attrs(ph)["type"]
		if typ == "" {
			typ = "body"
		}
		if !contains(types, typ) {
			return sp
		}
		done = true
		xfrm := fmt.Sprintf(`<a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>`,
			int64(box[0]*float64(w)), int64(box[1]*float64(h)), int64(box[2]*float64(w)), int64(box[3]*float64(h)))
		switch {
		case spXfrmBlock.MatchString(sp):
			return spXfrmBlock.ReplaceAllLiteralString(sp, xfrm)
		case spPrEmpty.MatchString(sp):
			return spPrEmpty.ReplaceAllLiteralString(sp, "<p:spPr>"+xfrm+"</p:spPr>")
		case spPrOpen.MatchString(sp):
			loc := spPrOpen.FindStringIndex(sp)
			return sp[:loc[1]] + xfrm + sp[loc[1]:]
		}
		return sp
	})
}

func sameBox(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-4 {
			return false
		}
	}
	return true
}

func writeSlidesSpec(file string, t domain.Template, spec TemplateSpec) ([]byte, error) {
	old, err := readSlidesSpec(file, t)
	if err != nil {
		return nil, err
	}
	p, err := openTemplate(file)
	if err != nil {
		return nil, err
	}
	theme := string(p.parts[p.theme])
	for slot, c := range spec.Colors {
		if old.Colors[slot] == c {
			continue
		}
		theme = regexp.MustCompile(`(?s)(<a:`+slot+`>).*?(</a:`+slot+`>)`).ReplaceAllString(theme, `${1}<a:srgbClr val="`+c+`"/>${2}`)
	}
	for _, f := range []struct{ kind, script, now, was string }{
		{"majorFont", "latin", spec.HeadingFont, old.HeadingFont}, {"majorFont", "ea", spec.HeadingFontEA, old.HeadingFontEA},
		{"minorFont", "latin", spec.BodyFont, old.BodyFont}, {"minorFont", "ea", spec.BodyFontEA, old.BodyFontEA},
	} {
		if f.now != f.was {
			theme = setThemeFont(theme, f.kind, f.script, f.now)
		}
	}
	p.parts[p.theme] = []byte(theme)
	move := func(layout string, types []string, box, was []float64) {
		if box == nil || sameBox(box, was) || p.parts[layout] == nil {
			return
		}
		p.parts[layout] = []byte(setPlaceholderBox(string(p.parts[layout]), types, box, p.width, p.height))
	}
	move(t.ContentLayout, []string{"title"}, spec.TitleBox, old.TitleBox)
	move(t.CoverLayout, []string{"ctrTitle", "title"}, spec.CoverTitleBox, old.CoverTitleBox)
	move(t.CoverLayout, []string{"subTitle"}, spec.CoverSubtitleBox, old.CoverSubtitleBox)
	return zipParts(p.parts, p.order)
}

// zipParts writes an OOXML package, [Content_Types].xml first.
func zipParts(parts map[string][]byte, order []string) ([]byte, error) {
	names := []string{}
	seen := map[string]bool{"[Content_Types].xml": true}
	for _, n := range order {
		if _, ok := parts[n]; ok && !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}
	rest := []string{}
	for n := range parts {
		if !seen[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for _, name := range append(append([]string{"[Content_Types].xml"}, names...), rest...) {
		w, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(parts[name]); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// ---- WordprocessingML element editing ----

// Schema orders of the children Word cares about (it rejects files whose
// elements are out of order).
var (
	styleOrder  = []string{"name", "aliases", "basedOn", "next", "link", "autoRedefine", "uiPriority", "semiHidden", "unhideWhenUsed", "qFormat", "locked", "personal", "personalCompose", "personalReply", "rsid", "pPr", "rPr", "tblPr", "trPr", "tcPr", "tblStylePr"}
	pPrOrder    = []string{"pStyle", "keepNext", "keepLines", "pageBreakBefore", "framePr", "widowControl", "numPr", "suppressLineNumbers", "pBdr", "shd", "tabs", "suppressAutoHyphens", "kinsoku", "wordWrap", "overflowPunct", "topLinePunct", "autoSpaceDE", "autoSpaceDN", "bidi", "adjustRightInd", "snapToGrid", "spacing", "ind", "contextualSpacing", "mirrorIndents", "suppressOverlap", "jc", "textDirection", "textAlignment", "textboxTightWrap", "outlineLvl", "divId", "cnfStyle", "rPr", "sectPr", "pPrChange"}
	rPrOrder    = []string{"rStyle", "rFonts", "b", "bCs", "i", "iCs", "caps", "smallCaps", "strike", "dstrike", "outline", "shadow", "emboss", "imprint", "noProof", "snapToGrid", "vanish", "webHidden", "color", "spacing", "w", "kern", "position", "sz", "szCs", "highlight", "u", "effect", "bdr", "shd", "fitText", "vertAlign", "rtl", "cs", "em", "lang", "eastAsianLayout", "specVanish", "oMath"}
	sectPrOrder = []string{"headerReference", "footerReference", "footnotePr", "endnotePr", "type", "pgSz", "pgMar", "paperSrc", "pgBorders", "lnNumType", "pgNumType", "cols", "formProt", "vAlign", "noEndnote", "titlePg", "textDirection", "bidi", "rtlGutter", "docGrid", "printerSettings", "sectPrChange"}
)

// element splits "<w:x a>inner</w:x>" (or "<w:x a/>") into its open tag,
// inner XML and close tag.
func element(el string) (open, inner, close string) {
	end := strings.IndexByte(el, '>')
	if end < 0 {
		return el, "", ""
	}
	if strings.HasSuffix(el[:end+1], "/>") {
		name := strings.Fields(strings.TrimSuffix(el[1:end], "/"))[0]
		return strings.TrimSuffix(strings.TrimSpace(el[:end]), "/") + ">", "", "</" + name + ">"
	}
	c := strings.LastIndex(el, "</")
	return el[:end+1], el[end+1 : c], el[c:]
}

// childElements splits inner XML into its top-level elements.
func childElements(inner string) []string {
	out := []string{}
	depth, start := 0, 0
	for i := 0; i < len(inner); {
		if inner[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(inner[i:], '>')
		if end < 0 {
			break
		}
		end += i
		tag := inner[i : end+1]
		switch {
		case strings.HasPrefix(tag, "<?"), strings.HasPrefix(tag, "<!"):
		case strings.HasPrefix(tag, "</"):
			depth--
			if depth == 0 {
				out = append(out, inner[start:end+1])
			}
		case strings.HasSuffix(tag, "/>"):
			if depth == 0 {
				out = append(out, tag)
			}
		default:
			if depth == 0 {
				start = i
			}
			depth++
		}
		i = end + 1
	}
	return out
}

func localName(el string) string {
	n := strings.TrimLeft(el, "<")
	if i := strings.IndexAny(n, " />\t\n"); i >= 0 {
		n = n[:i]
	}
	if i := strings.IndexByte(n, ':'); i >= 0 {
		n = n[i+1:]
	}
	return n
}

// child returns the first top-level child of el with the given local name.
func child(el, name string) string {
	_, inner, _ := element(el)
	for _, c := range childElements(inner) {
		if localName(c) == name {
			return c
		}
	}
	return ""
}

// setChild replaces el's child of that name (all of them) with value, or
// removes it when value is empty, inserting in schema order.
func setChild(el, name, value string, order []string) string {
	open, inner, close := element(el)
	kids := childElements(inner)
	rank := func(n string) int {
		for i, o := range order {
			if o == n {
				return i
			}
		}
		return -1
	}
	out := []string{}
	for _, c := range kids {
		if localName(c) != name {
			out = append(out, c)
		}
	}
	if value != "" {
		at := len(out)
		mine := rank(name)
		for i, c := range out {
			if r := rank(localName(c)); r > mine {
				at = i
				break
			}
		}
		out = append(out[:at], append([]string{value}, out[at:]...)...)
	}
	return open + strings.Join(out, "") + close
}

// setAttrs sets (or with "" removes) attributes on a single tag.
func setAttrs(tag string, values map[string]string) string {
	selfClosing := strings.HasSuffix(tag, "/>")
	body := strings.TrimSuffix(strings.TrimSuffix(tag, "/>"), ">")
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		re := regexp.MustCompile(`\s` + regexp.QuoteMeta(k) + `="[^"]*"`)
		body = re.ReplaceAllString(body, "")
		if v := values[k]; v != "" {
			body += ` ` + k + `="` + x(v) + `"`
		}
	}
	if selfClosing {
		return body + "/>"
	}
	return body + ">"
}

func attrNamed(tag, name string) (string, bool) {
	m := regexp.MustCompile(`\s` + regexp.QuoteMeta(name) + `="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ---- Word ----

var wordStyleNames = map[string][2]string{ // role → styleId, built-in name
	"title": {"Title", "Title"}, "subtitle": {"Subtitle", "Subtitle"}, "h1": {"Heading1", "heading 1"},
	"h2": {"Heading2", "heading 2"}, "normal": {"Normal", "Normal"}, "caption": {"Caption", "caption"},
}

// Defaults for a role whose style the template lacks (as buildWordDocument adds).
var wordRoleDefaults = map[string]WordStyleSpec{
	"title": {Size: 22, Bold: true, After: 12}, "subtitle": {Size: 11, Color: "666666", After: 3},
	"h1": {Size: 15, Bold: true, Before: 18, After: 6}, "h2": {Size: 12, Bold: true, Before: 10, After: 3},
	"normal": {Size: 11}, "caption": {Size: 9, Color: "595959", After: 10},
}

func onOff(el string) (bool, bool) {
	if el == "" {
		return false, false
	}
	v, ok := attrNamed(el, "w:val")
	return !ok || (v != "0" && v != "false" && v != "off"), true
}

func halfPoints(el string) (float64, bool) {
	if v, ok := attrNamed(el, "w:val"); ok {
		n, err := strconv.ParseFloat(v, 64)
		return n / 2, err == nil
	}
	return 0, false
}

func readWordSpec(file string) (TemplateSpec, error) {
	p, err := openWordTemplate(file)
	if err != nil {
		return TemplateSpec{}, err
	}
	spec := TemplateSpec{Kind: "docx", Styles: map[string]WordStyleSpec{}}
	stylesXML := string(p.parts["word/styles.xml"])
	theme := string(p.parts[p.related("theme")])
	byID := map[string]string{}
	for _, def := range wordStylePattern.FindAllString(stylesXML, -1) {
		if id, ok := attrNamed(def[:strings.IndexByte(def, '>')+1], "w:styleId"); ok {
			byID[id] = def
		}
	}
	defaults := regexp.MustCompile(`(?s)<w:docDefaults>.*?</w:docDefaults>`).FindString(stylesXML)
	defRPr := child(child(defaults, "rPrDefault"), "rPr")
	defPPr := child(child(defaults, "pPrDefault"), "pPr")
	roles := p.wordStyles()
	for _, role := range WordSpecRoles {
		def, found := roles[role].xml, roles[role].id != ""
		st := wordRoleDefaults[role]
		var font, fontEA, size, color, bold, before, after bool
		if !found {
			// An added style takes EduForge's defaults; only fonts come from
			// the document defaults.
			def = ""
			size, color, bold, before, after = true, true, true, true, true
		}
		// Walk the basedOn chain, nearest first, then document defaults.
		chain := []string{}
		for cur, n := def, 0; cur != "" && n < 8; n++ {
			chain = append(chain, cur)
			based := child(cur, "basedOn")
			id, _ := attrNamed(based, "w:val")
			cur = byID[id]
		}
		pick := func(rPr, pPr string) {
			if f := child(rPr, "rFonts"); f != "" {
				if !font {
					if v, ok := attrNamed(f, "w:ascii"); ok {
						st.Font, font = v, true
					} else if v, ok := attrNamed(f, "w:asciiTheme"); ok {
						st.Font, font = themeFont(theme, map[bool]string{true: "majorFont", false: "minorFont"}[strings.HasPrefix(v, "major")], "latin"), true
					}
				}
				if !fontEA {
					if v, ok := attrNamed(f, "w:eastAsia"); ok {
						st.FontEA, fontEA = v, true
					} else if v, ok := attrNamed(f, "w:eastAsiaTheme"); ok {
						st.FontEA, fontEA = themeFont(theme, map[bool]string{true: "majorFont", false: "minorFont"}[strings.HasPrefix(v, "major")], "ea"), true
					}
				}
			}
			if !size {
				if v, ok := halfPoints(child(rPr, "sz")); ok {
					st.Size, size = v, true
				}
			}
			if !color {
				if c := child(rPr, "color"); c != "" {
					v, _ := attrNamed(c, "w:val")
					if hexColor.MatchString(strings.ToUpper(v)) {
						st.Color = strings.ToUpper(v)
					} else {
						st.Color = ""
					}
					color = true
				}
			}
			if !bold {
				if v, ok := onOff(child(rPr, "b")); ok {
					st.Bold, bold = v, true
				}
			}
			sp := child(pPr, "spacing")
			if !before {
				if v, ok := attrNamed(sp, "w:before"); ok {
					n, _ := strconv.ParseFloat(v, 64)
					st.Before, before = n/20, true
				}
			}
			if !after {
				if v, ok := attrNamed(sp, "w:after"); ok {
					n, _ := strconv.ParseFloat(v, 64)
					st.After, after = n/20, true
				}
			}
		}
		for _, cur := range chain {
			pick(child(cur, "rPr"), child(cur, "pPr"))
		}
		pick(defRPr, defPPr)
		if !font {
			st.Font = themeFont(theme, "minorFont", "latin")
		}
		if !fontEA {
			st.FontEA = themeFont(theme, "minorFont", "ea")
		}
		if !size {
			st.Size = 10 // Word's default when nothing sets it
		}
		spec.Styles[role] = st
	}
	sect := lastSectPr(string(p.parts[p.main]))
	if mar := pgMarPattern.FindString(sect); mar != "" {
		mm := func(name string) float64 { return math.Round(float64(twipAttr(mar, name))*25.4/1440*10) / 10 }
		spec.Margins = &Margins{Top: mm("top"), Bottom: mm("bottom"), Left: mm("left"), Right: mm("right")}
	}
	spec.Header = p.headerFooter(sect, "header")
	spec.Footer = p.headerFooter(sect, "footer")
	return spec, nil
}

func lastSectPr(doc string) string {
	all := sectPrPattern.FindAllString(doc, -1)
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

var refPattern = func(kind string) *regexp.Regexp {
	return regexp.MustCompile(`<w:` + kind + `Reference\b[^>]*w:type="default"[^>]*>`)
}
var textRun = regexp.MustCompile(`(?s)<w:t\b[^>]*>(.*?)</w:t>`)
var paragraphPattern = regexp.MustCompile(`(?s)<w:p\b[^>]*>.*?</w:p>|<w:p\b[^>]*/>`)
var complexHeader = regexp.MustCompile(`<w:drawing\b|<w:pict\b|<w:fldSimple\b|<w:instrText\b|<w:sdt\b|<mc:AlternateContent\b|<w:tbl\b|<w:object\b`)

// headerPart returns the part holding the default header or footer.
func (p *wordPackage) headerPart(sect, kind string) string {
	ref := refPattern(kind).FindString(sect)
	id, ok := attrNamed(ref, "r:id")
	if !ok {
		return ""
	}
	for _, r := range parseRels(p.parts[relsPath(p.main)]) {
		if r.ID == id {
			return resolvePart(p.main, r.Target)
		}
	}
	return ""
}

func (p *wordPackage) headerFooter(sect, kind string) *HeaderFooter {
	part := p.headerPart(sect, kind)
	xml := string(p.parts[part])
	if xml == "" {
		return &HeaderFooter{Editable: true}
	}
	lines := []string{}
	for _, para := range paragraphPattern.FindAllString(xml, -1) {
		var line strings.Builder
		for _, m := range textRun.FindAllStringSubmatch(para, -1) {
			line.WriteString(unescapeXML(m[1]))
		}
		lines = append(lines, line.String())
	}
	return &HeaderFooter{Text: strings.TrimRight(strings.Join(lines, "\n"), "\n"), Editable: !complexHeader.MatchString(xml)}
}

func unescapeXML(s string) string {
	return strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&").Replace(s)
}

// styleDef returns a role's style definition, creating one with Word's
// built-in name when the template lacks it.
func styleDef(roles map[string]wordStyle, byID map[string]bool, role, normal string) (string, bool) {
	if st := roles[role]; st.id != "" {
		return st.xml, false
	}
	names := wordStyleNames[role]
	id := names[0]
	if byID[id] {
		id = "P2L" + id
	}
	based := ""
	if normal != "" && role != "normal" {
		based = `<w:basedOn w:val="` + normal + `"/><w:next w:val="` + normal + `"/>`
	}
	pPr := ""
	switch role {
	case "h1":
		pPr = `<w:pPr><w:keepNext/><w:outlineLvl w:val="0"/></w:pPr>`
	case "h2":
		pPr = `<w:pPr><w:keepNext/><w:outlineLvl w:val="1"/></w:pPr>`
	}
	return fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="%s"><w:name w:val="%s"/>%s<w:qFormat/>%s</w:style>`, id, names[1], based, pPr), true
}

func applyWordStyle(def string, now, was WordStyleSpec) string {
	rPr := child(def, "rPr")
	if rPr == "" {
		rPr = "<w:rPr></w:rPr>"
	}
	pPr := child(def, "pPr")
	if pPr == "" {
		pPr = "<w:pPr></w:pPr>"
	}
	if now.Font != was.Font || now.FontEA != was.FontEA {
		f := child(rPr, "rFonts")
		if f == "" {
			f = "<w:rFonts/>"
		}
		set := map[string]string{}
		if now.Font != was.Font {
			set["w:ascii"], set["w:hAnsi"], set["w:asciiTheme"], set["w:hAnsiTheme"] = now.Font, now.Font, "", ""
		}
		if now.FontEA != was.FontEA {
			set["w:eastAsia"], set["w:eastAsiaTheme"] = now.FontEA, ""
		}
		rPr = setChild(rPr, "rFonts", setAttrs(f, set), rPrOrder)
	}
	if now.Size != was.Size && now.Size > 0 {
		hp := strconv.Itoa(int(math.Round(now.Size * 2)))
		rPr = setChild(rPr, "sz", `<w:sz w:val="`+hp+`"/>`, rPrOrder)
		rPr = setChild(rPr, "szCs", `<w:szCs w:val="`+hp+`"/>`, rPrOrder)
	}
	if now.Color != was.Color {
		c := now.Color
		if c == "" {
			c = "auto"
		}
		rPr = setChild(rPr, "color", `<w:color w:val="`+c+`"/>`, rPrOrder)
	}
	if now.Bold != was.Bold {
		b, bcs := `<w:b/>`, `<w:bCs/>`
		if !now.Bold {
			b, bcs = `<w:b w:val="0"/>`, `<w:bCs w:val="0"/>`
		}
		rPr = setChild(setChild(rPr, "b", b, rPrOrder), "bCs", bcs, rPrOrder)
	}
	if now.Before != was.Before || now.After != was.After {
		sp := child(pPr, "spacing")
		if sp == "" {
			sp = "<w:spacing/>"
		}
		set := map[string]string{}
		if now.Before != was.Before {
			set["w:before"], set["w:beforeLines"], set["w:beforeAutospacing"] = strconv.Itoa(int(math.Round(now.Before*20))), "", ""
		}
		if now.After != was.After {
			set["w:after"], set["w:afterLines"], set["w:afterAutospacing"] = strconv.Itoa(int(math.Round(now.After*20))), "", ""
		}
		pPr = setChild(pPr, "spacing", setAttrs(sp, set), pPrOrder)
	}
	if _, inner, _ := element(pPr); strings.TrimSpace(inner) != "" {
		def = setChild(def, "pPr", pPr, styleOrder)
	}
	if _, inner, _ := element(rPr); strings.TrimSpace(inner) != "" {
		def = setChild(def, "rPr", rPr, styleOrder)
	}
	return def
}

func writeWordSpec(file string, spec TemplateSpec) ([]byte, error) {
	old, err := readWordSpec(file)
	if err != nil {
		return nil, err
	}
	p, err := openWordTemplate(file)
	if err != nil {
		return nil, err
	}
	stylesXML := string(p.parts["word/styles.xml"])
	if !strings.Contains(stylesXML, "</w:styles>") {
		return nil, errors.New("not_a_word_document")
	}
	byID := map[string]bool{}
	for _, def := range wordStylePattern.FindAllString(stylesXML, -1) {
		if id, ok := attrNamed(def[:strings.IndexByte(def, '>')+1], "w:styleId"); ok {
			byID[id] = true
		}
	}
	roles := p.wordStyles()
	for _, role := range WordSpecRoles {
		now, ok := spec.Styles[role]
		if !ok || now == old.Styles[role] {
			continue
		}
		def, created := styleDef(roles, byID, role, roles["normal"].id)
		next := applyWordStyle(def, now, old.Styles[role])
		if created {
			stylesXML = strings.Replace(stylesXML, "</w:styles>", next+"</w:styles>", 1)
		} else {
			stylesXML = strings.Replace(stylesXML, def, next, 1)
		}
	}
	p.parts["word/styles.xml"] = []byte(stylesXML)

	sect := lastSectPr(string(p.parts[p.main]))
	newSect := sect
	if m := spec.Margins; m != nil && sect != "" && (old.Margins == nil || *m != *old.Margins) {
		twips := func(mm float64) string { return strconv.Itoa(int(math.Round(mm * 1440 / 25.4))) }
		mar := pgMarPattern.FindString(newSect)
		if mar == "" {
			mar = `<w:pgMar w:header="720" w:footer="720" w:gutter="0"/>`
		}
		mar = setAttrs(mar, map[string]string{"w:top": twips(m.Top), "w:bottom": twips(m.Bottom), "w:left": twips(m.Left), "w:right": twips(m.Right)})
		newSect = setChild(newSect, "pgMar", mar, sectPrOrder)
	}
	for _, hf := range []struct {
		kind     string
		now, was *HeaderFooter
	}{{"header", spec.Header, old.Header}, {"footer", spec.Footer, old.Footer}} {
		if hf.now == nil || hf.was == nil || hf.now.Text == hf.was.Text || !hf.was.Editable || sect == "" {
			continue
		}
		newSect = p.writeHeaderFooter(newSect, hf.kind, hf.now.Text)
	}
	if newSect != sect {
		doc := string(p.parts[p.main]) // a new header may have added a namespace
		i := strings.LastIndex(doc, sect)
		doc = doc[:i] + newSect + doc[i+len(sect):]
		p.parts[p.main] = []byte(doc)
	}
	order := make([]string, 0, len(p.parts))
	for n := range p.parts {
		order = append(order, n)
	}
	sort.Strings(order)
	return zipParts(p.parts, order)
}

// writeHeaderFooter replaces the default header/footer text (keeping its
// first paragraph's and run's formatting), creating the part when missing.
func (p *wordPackage) writeHeaderFooter(sect, kind, text string) string {
	part := p.headerPart(sect, kind)
	root := map[string]string{"header": "hdr", "footer": "ftr"}[kind]
	pPr, rPr := "", ""
	open := `<w:` + root + ` xmlns:w="` + nsW + `" xmlns:r="` + nsR + `">`
	if xml := string(p.parts[part]); xml != "" {
		if i := strings.Index(xml, "<w:"+root); i >= 0 {
			head := xml[:i]
			end := strings.IndexByte(xml[i:], '>') + i
			open = head + xml[i:end+1]
		}
		if first := paragraphPattern.FindString(xml); first != "" {
			pPr = child(first, "pPr")
			rPr = child(child(first, "r"), "rPr")
		}
	}
	var body strings.Builder
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(&body, `<w:p>%s<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r></w:p>`, pPr, rPr, x(line))
	}
	xml := open + body.String() + `</w:` + root + `>`
	if part != "" {
		p.parts[part] = []byte(xml)
		return sect
	}
	// A new part: relationship, content type and section reference.
	dir := strings.TrimSuffix(p.main, "/document.xml")
	name := "p2l-" + kind + ".xml"
	p.parts[dir+"/"+name] = []byte(xml)
	rid := "rIdP2L" + strings.ToUpper(kind[:1]) + kind[1:]
	rels := string(p.parts[relsPath(p.main)])
	rels = strings.Replace(rels, "</Relationships>", fmt.Sprintf(`<Relationship Id="%s" Type="%s/%s" Target="%s"/></Relationships>`, rid, nsR, kind, name), 1)
	p.parts[relsPath(p.main)] = []byte(rels)
	types := string(p.parts["[Content_Types].xml"])
	types = strings.Replace(types, "</Types>", fmt.Sprintf(`<Override PartName="/%s/%s" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.%s+xml"/></Types>`, dir, name, kind), 1)
	p.parts["[Content_Types].xml"] = []byte(types)
	ref := fmt.Sprintf(`<w:%sReference w:type="default" r:id="%s"/>`, kind, rid)
	if root := string(p.parts[p.main]); !strings.Contains(root[:strings.Index(root, "<w:body")], "xmlns:r=") {
		p.parts[p.main] = []byte(strings.Replace(string(p.parts[p.main]), "<w:document ", `<w:document xmlns:r="`+nsR+`" `, 1))
	}
	// Header references come first, then footer references.
	open2, inner, close := element(sect)
	kids := childElements(inner)
	at := 0
	for i, c := range kids {
		n := localName(c)
		if n == "headerReference" || (kind == "footer" && n == "footerReference") {
			at = i + 1
		}
	}
	kids = append(kids[:at], append([]string{ref}, kids[at:]...)...)
	return open2 + strings.Join(kids, "") + close
}
