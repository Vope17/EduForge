package artifacts

import (
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"eduforge/internal/domain"
)

// Model output may use a small Markdown subset for structure: **bold**,
// "#" headings and "-" bullets. Exports render it instead of printing the
// markers literally.

var (
	mdHeading    = regexp.MustCompile(`^\s*#{1,6}\s+(.+?)\s*#*\s*$`)
	mdBoldLine   = regexp.MustCompile(`^\s*\*\*(.+?)\*\*\s*[:：]?\s*$`)
	mdBullet     = regexp.MustCompile(`^(\s*)[-*•]\s+(.*)$`)
	mdNumbered   = regexp.MustCompile(`^(\s*)(\d{1,2})[.)]\s+(.*)$`)
	mdBold       = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	mdItalic     = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*\n]*?)\*([^*\w]|$)`)
	mdInlineCode = regexp.MustCompile("`([^`\n]+)`")
)

type markupLine struct {
	heading bool   // the whole line is a subheading
	bullet  string // "" or the bullet/number prefix to show
	text    string // remaining text, still containing inline **bold**
}

func parseMarkupLine(line string) markupLine {
	if m := mdHeading.FindStringSubmatch(line); m != nil {
		return markupLine{heading: true, text: m[1]}
	}
	if m := mdBoldLine.FindStringSubmatch(line); m != nil {
		return markupLine{heading: true, text: m[1]}
	}
	if m := mdBullet.FindStringSubmatch(line); m != nil {
		return markupLine{bullet: m[1] + "• ", text: m[2]}
	}
	if m := mdNumbered.FindStringSubmatch(line); m != nil {
		return markupLine{bullet: m[1] + m[2] + ". ", text: m[3]}
	}
	return markupLine{text: line}
}

// inlineSegments splits text into plain and bold runs, dropping italic and
// code markers but keeping their text.
func inlineSegments(text string) []struct {
	text string
	bold bool
} {
	text = mdInlineCode.ReplaceAllString(text, "$1")
	text = mdItalic.ReplaceAllString(text, "$1$2$3")
	out := []struct {
		text string
		bold bool
	}{}
	last := 0
	for _, m := range mdBold.FindAllStringSubmatchIndex(text, -1) {
		if m[0] > last {
			out = append(out, struct {
				text string
				bold bool
			}{text[last:m[0]], false})
		}
		out = append(out, struct {
			text string
			bold bool
		}{text[m[2]:m[3]], true})
		last = m[1]
	}
	if last < len(text) {
		out = append(out, struct {
			text string
			bold bool
		}{text[last:], false})
	}
	return out
}

// PlainText removes the Markdown markers and writes formulas as readable
// text, for slides and speaker notes.
func PlainText(text string) string {
	lines := strings.Split(MathAsText(domain.StripCitationMarkers(text)), "\n")
	for i, line := range lines {
		l := parseMarkupLine(line)
		var b strings.Builder
		b.WriteString(l.bullet)
		for _, seg := range inlineSegments(l.text) {
			b.WriteString(seg.text)
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// bodyHTML renders a section body as escaped HTML, formulas as TeX source.
func bodyHTML(text string) string { return bodyHTMLWith(text, nil) }

// bodyHTMLWith renders a section body as escaped HTML with its formulas as
// inline SVG (from math; formulas missing from it stay TeX source). The
// container keeps white-space: pre-wrap, so line breaks are preserved.
func bodyHTMLWith(text string, math renderedSet) string {
	lines := strings.Split(domain.StripCitationMarkers(text), "\n")
	for i, line := range lines {
		if tex, ok := displayFormula(line); ok {
			lines[i] = `<span class="math-display">` + formulaHTML(mathPiece{TeX: tex, Display: true}, math) + `</span>`
			continue
		}
		l := parseMarkupLine(line)
		var b strings.Builder
		if l.heading {
			b.WriteString(`<strong class="sub">`)
		}
		b.WriteString(html.EscapeString(l.bullet))
		for _, piece := range splitMath(l.text) {
			if piece.isMath() {
				b.WriteString(formulaHTML(piece, math))
				continue
			}
			for _, seg := range inlineSegments(piece.Text) {
				if seg.bold && !l.heading {
					b.WriteString("<strong>" + html.EscapeString(seg.text) + "</strong>")
				} else {
					b.WriteString(html.EscapeString(seg.text))
				}
			}
		}
		if l.heading {
			b.WriteString("</strong>")
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// formulaHTML is a formula as inline SVG, sitting on the text baseline, or
// its TeX source when it was not rendered.
func formulaHTML(p mathPiece, math renderedSet) string {
	if r, ok := math.get(p); ok {
		return fmt.Sprintf(`<span class="math" style="vertical-align:-%.3fex">%s</span>`, r.Depth, r.SVG)
	}
	if p.Display {
		return `<code class="tex">$$` + html.EscapeString(p.TeX) + `$$</code>`
	}
	return `<code class="tex">$` + html.EscapeString(p.TeX) + `$</code>`
}

// SectionFigures returns the original figures for each section. A section
// with an explicit Figures list (set at generation or by the editor) shows
// exactly those; otherwise up to three not-yet-used figures from the pages it
// cites are assigned, so each figure appears once next to the text about it.
func SectionFigures(a domain.Artifact, k domain.Knowledge) [][]domain.Image {
	out := make([][]domain.Image, len(a.Sections))
	used := map[string]bool{}
	byPath := map[string]domain.Image{}
	for _, img := range k.Images {
		byPath[img.Path] = img
	}
	lookup := func(path string) domain.Image {
		if img, ok := byPath[path]; ok {
			return img
		}
		return domain.Image{Path: path}
	}
	for i, sec := range a.Sections {
		if sec.Figures != nil {
			for _, path := range sec.Figures {
				out[i] = append(out[i], lookup(path))
				used[path] = true
			}
			continue
		}
		if sec.Image != "" && !used[sec.Image] {
			out[i] = append(out[i], lookup(sec.Image))
			used[sec.Image] = true
		}
		cited := map[string]bool{}
		for _, c := range sec.Citations {
			cited[c] = true
		}
		for _, img := range k.Images {
			if len(out[i]) >= 3 {
				break
			}
			if cited[img.Citation] && !used[img.Path] {
				out[i] = append(out[i], img)
				used[img.Path] = true
			}
		}
	}
	return out
}

// LessonNames lists the distinct research documents behind the knowledge by
// their own titles (from the PDF, or read off its pages when the knowledge is
// AI-written), else their file names without the extension.
func LessonNames(k domain.Knowledge) []string {
	names := []string{}
	seen := map[string]bool{}
	for _, u := range k.Units {
		name := strings.TrimSpace(u.SourceTitle)
		if name == "" {
			name = strings.TrimSpace(strings.TrimSuffix(u.SourceName, filepath.Ext(u.SourceName)))
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 && k.Title != "" {
		names = append(names, k.Title)
	}
	return names
}

// DocumentTitle heads a reader: the lessons' own names, else the knowledge title.
func DocumentTitle(k domain.Knowledge) string {
	if names := LessonNames(k); len(names) > 0 {
		return strings.Join(names, " / ")
	}
	return k.Title
}

var (
	figureLabel   = regexp.MustCompile(`(?i)^\s*(fig(?:ure)?\.?|圖|table|表)\s*(\d+)`)
	figureMention = regexp.MustCompile(`(?i)(fig(?:ure)?s?\.?|圖|tables?|表)\s*(\d+)`)
)

// figureKey names a figure or table by kind and number ("fig 3", "table 2").
func figureKey(word, number string) string {
	word = strings.ToLower(word)
	if strings.HasPrefix(word, "tab") || word == "表" {
		return "table " + number
	}
	return "fig " + number
}

// captionKey is the key of a figure from its caption, "" without a number.
func captionKey(img domain.Image) string {
	if m := figureLabel.FindStringSubmatch(figureCaption(img)); m != nil {
		return figureKey(m[1], m[2])
	}
	return ""
}

// FigureLabels indexes images by their caption label ("fig 3", "table 2").
func FigureLabels(images []domain.Image) map[string]domain.Image {
	out := map[string]domain.Image{}
	for _, img := range images {
		if key := captionKey(img); key != "" {
			if _, ok := out[key]; !ok {
				out[key] = img
			}
		}
	}
	return out
}

// LabelKey is the key of a label such as "Figure 3", "Fig. 3" or "表 2".
func LabelKey(label string) string {
	if m := figureLabel.FindStringSubmatch(label); m != nil {
		return figureKey(m[1], m[2])
	}
	return ""
}

// mentionKeys lists the figures and tables a text mentions, in order.
func mentionKeys(text string) []string {
	keys := []string{}
	for _, m := range figureMention.FindAllStringSubmatch(text, -1) {
		keys = append(keys, figureKey(m[1], m[2]))
	}
	return keys
}

// MentionedFigures chooses each reader section's figures from its text: the
// figures and tables it mentions by number, in the order it mentions them.
// Each is shown once, in the section where it belongs most: one that cites
// the page it is on and mentions it most often (an overview that names many
// figures in passing does not take them all). A section left without
// figures shows up to two from the pages it cites that no section mentions.
func MentionedFigures(a domain.Artifact, k domain.Knowledge) [][]string {
	byKey := map[string]domain.Image{}
	for _, img := range k.Images {
		if key := captionKey(img); key != "" {
			if _, ok := byKey[key]; !ok {
				byKey[key] = img
			}
		}
	}
	home := map[string]int{} // figure path → section
	best := map[string]int{}
	for i, sec := range a.Sections {
		counts := map[string]int{}
		for _, key := range mentionKeys(sec.Body) {
			counts[key]++
		}
		for key, n := range counts {
			img, ok := byKey[key]
			if !ok {
				continue
			}
			// Citing the figure's page counts most, then how often the
			// section mentions it; on a tie the more focused section (fewer
			// figures mentioned) wins.
			score := 10*n - len(counts)
			if slices.Contains(sec.Citations, img.Citation) {
				score += 1000
			}
			if prev, seen := best[img.Path]; !seen || score > prev {
				best[img.Path], home[img.Path] = score, i
			}
		}
	}
	shown := map[string]bool{}
	out := make([][]string, len(a.Sections))
	for i, sec := range a.Sections {
		out[i] = []string{}
		for _, key := range mentionKeys(sec.Body) {
			if img, ok := byKey[key]; ok && home[img.Path] == i && !shown[img.Path] {
				out[i] = append(out[i], img.Path)
				shown[img.Path] = true
			}
		}
	}
	for i, sec := range a.Sections {
		if len(out[i]) > 0 {
			continue
		}
		for _, img := range k.Images {
			_, mentioned := best[img.Path]
			if len(out[i]) < 2 && slices.Contains(sec.Citations, img.Citation) && !shown[img.Path] && !mentioned {
				out[i] = append(out[i], img.Path)
				shown[img.Path] = true
			}
		}
	}
	return out
}

// PlaceFigures decides where a section's figures go in its body: right
// after the paragraph that first mentions them by number ("Figure 3",
// "圖 3", "Table 2"), else spread evenly between paragraphs, so text and
// figures alternate. It returns the figures to show after each line.
func PlaceFigures(body string, figures []domain.Image) [][]domain.Image {
	lines := strings.Split(body, "\n")
	out := make([][]domain.Image, len(lines))
	if len(figures) == 0 {
		return out
	}
	// A paragraph ends at a blank line, before a subheading, or at the end.
	ends := []int{}
	for i, line := range lines {
		next := i + 1
		if strings.TrimSpace(line) == "" || parseMarkupLine(line).heading {
			continue
		}
		if next == len(lines) || strings.TrimSpace(lines[next]) == "" || parseMarkupLine(lines[next]).heading {
			ends = append(ends, i)
		}
	}
	if len(ends) == 0 {
		ends = []int{len(lines) - 1}
	}
	endOf := func(line int) int {
		for _, e := range ends {
			if e >= line {
				return e
			}
		}
		return ends[len(ends)-1]
	}
	first := map[string]int{}
	for i, line := range lines {
		for _, key := range mentionKeys(line) {
			if _, ok := first[key]; !ok {
				first[key] = i
			}
		}
	}
	unplaced := []domain.Image{}
	for _, fig := range figures {
		if line, ok := first[captionKey(fig)]; ok && captionKey(fig) != "" {
			at := endOf(line)
			out[at] = append(out[at], fig)
			continue
		}
		unplaced = append(unplaced, fig)
	}
	for j, fig := range unplaced {
		at := ends[min(len(ends)-1, (j+1)*len(ends)/(len(unplaced)+1))]
		if len(unplaced) >= len(ends) {
			at = ends[min(len(ends)-1, j)]
		}
		out[at] = append(out[at], fig)
	}
	return out
}

// figureCaption is the lesson's own caption ("Figure 5: …"); figures without
// one are shown uncaptioned rather than with a source label.
func figureCaption(img domain.Image) string {
	return img.Caption
}

func referencesTitle(lang string) string {
	if lang == "zh-TW" {
		return "引用"
	}
	return "References"
}

// Roman numbers reader sections: 1 → "I", 4 → "IV".
func Roman(n int) string {
	if n <= 0 || n >= 4000 {
		return fmt.Sprint(n)
	}
	var b strings.Builder
	for _, p := range []struct {
		v int
		s string
	}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"}, {50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}} {
		for n >= p.v {
			b.WriteString(p.s)
			n -= p.v
		}
	}
	return b.String()
}

var leadingNumber = regexp.MustCompile(`^\s*(第?[一二三四五六七八九十]+[、.．章節]\s*|[IVXLC]+[.、．]\s*|\d+(\.\d+)*[.、．)]?\s+|\(\d+\)\s*)`)

// SectionTitle is a heading without numbering the model may have written;
// readers number their sections themselves.
func SectionTitle(title string) string {
	return strings.TrimSpace(leadingNumber.ReplaceAllString(PlainText(title), ""))
}
