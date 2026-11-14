package artifacts

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// Slides are figure-first: each content slide shows the section's main
// figure with two to four short points, further figures get their own
// slides, and the full explanation lives in the speaker notes. Every slide is
// a list of positioned text and image boxes the editor can move and resize.

const (
	maxSlidePoints   = 5
	figuresPerTopic  = 3
	zhPointRunes     = 64
	enPointRunes     = 160
	slideTitleSize   = 28
	slideBodySize    = 18
	slideCaptionSize = 12
	// formulaPoints is the type size slide formulas are set at.
	formulaPoints = 24
	formulaEx     = 0.442 * formulaPoints * emuPerPoint // EMU per ex
)

var sentenceEnd = regexp.MustCompile(`([。！？]|[.!?](\s|$))`)

// SlidePoints turns prose into at most five short bullet points.
func SlidePoints(body, lang string) []string {
	limit := enPointRunes
	if lang == "zh-TW" {
		limit = zhPointRunes
	}
	text := PlainText(body)
	candidates := []string{}
	bullets := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "• ") {
			bullets = append(bullets, strings.TrimPrefix(line, "• "))
			continue
		}
		candidates = append(candidates, line)
	}
	if len(bullets) == 0 {
		// No bullets in the text: use the leading sentences of each paragraph.
		for _, p := range candidates {
			if utf8.RuneCountInString(p) <= limit/2 && !sentenceEnd.MatchString(p) {
				continue // short subheading
			}
			loc := sentenceEnd.FindStringIndex(p)
			if loc != nil {
				p = p[:loc[1]]
			}
			bullets = append(bullets, strings.TrimSpace(p))
		}
	}
	points := []string{}
	for _, b := range bullets {
		b = strings.TrimRight(strings.TrimSpace(b), "。.")
		if b == "" {
			continue
		}
		points = append(points, Clip(b, limit))
		if len(points) == maxSlidePoints {
			break
		}
	}
	return points
}

func Clip(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	return string(r[:n-1]) + "…"
}

func textBox(id, role string, x, y, w, h float64, text string, size float64, bold bool) domain.Element {
	return domain.Element{ID: id, Kind: "text", Role: role, X: x, Y: y, W: w, H: h, Text: text, Size: size, Bold: bold}
}

func centered(el domain.Element) domain.Element { el.Align = "center"; return el }

func imageBox(id string, x, y, w, h float64, path string) domain.Element {
	return domain.Element{ID: id, Kind: "image", Role: "figure", X: x, Y: y, W: w, H: h, Path: path}
}

func bulletText(points []string) string {
	lines := make([]string, len(points))
	for i, p := range points {
		lines[i] = "• " + p
	}
	return strings.Join(lines, "\n")
}

type deckWords struct{ kicker, closing, thanks, opening, ending, figure string }

func deckLabels(lang string) deckWords {
	if lang == "zh-TW" {
		return deckWords{"論文導讀", "Q & A", "謝謝聆聽", "大家好，今天要和大家分享的論文是〈%s〉。接下來會依序說明研究動機、方法、實驗結果與限制。", "以上就是這篇論文的重點整理。謝謝大家，歡迎提問與討論。", "請看這張圖。"}
	}
	return deckWords{"Paper reading", "Q & A", "Thank you", "Hello everyone. Today I will present “%s”, covering its motivation, method, results and limitations.", "That concludes the main points of this paper. Thank you, and I am happy to take questions.", "Let us look at this figure."}
}

// Slide geometry (fractions of the slide).
func scaled(size, scale float64) float64 { return float64(int(size*scale*2+0.5)) / 2 }

// BuildDeck turns generated sections into laid-out slides: a cover, one
// figure-first slide per topic (further figures on their own slides) and a
// closing slide, placed in the geometry's title and content areas. aspect
// reports an image's width/height ratio (0 if unknown).
func BuildDeck(sections []domain.Section, k domain.Knowledge, title, lang string, aspect func(string) float64, geo Geometry) []domain.Section {
	if names := PaperNames(k); len(names) > 0 {
		title = names[0]
	}
	if aspect == nil {
		aspect = func(string) float64 { return 0 }
	}
	sz := func(v float64) float64 { return scaled(v, geo.Scale) }
	words := deckLabels(lang)
	figures := SectionFigures(domain.Artifact{Sections: sections}, k)
	cite := []string{}
	if len(k.Units) > 0 {
		cite = []string{k.Units[0].ID}
	}
	names := PaperNames(k)
	subtitle := ""
	if len(names) > 1 {
		subtitle = strings.Join(names[1:], "\n")
	}
	ct, cs := geo.CoverTitle, geo.CoverSub
	var cover []domain.Element
	if geo.Template {
		// The template's title layout: title in its title area, the kind of
		// document (and further papers) in its subtitle area.
		display := textBox("title", "display", ct[0], ct[1], ct[2], ct[3], title, sz(36), true)
		display.Align = geo.CoverAlign
		sub := textBox("subtitle", "subtitle", cs[0], cs[1], cs[2], cs[3], strings.TrimSpace(words.kicker+"\n"+subtitle), sz(18), false)
		sub.Align = geo.CoverAlign
		cover = []domain.Element{display, sub}
	} else {
		cover = []domain.Element{
			textBox("kicker", "kicker", ct[0], ct[1]-0.07, ct[2], 0.06, words.kicker, sz(16), true),
			textBox("title", "display", ct[0], ct[1], ct[2], ct[3], title, sz(36), true),
		}
		if subtitle != "" {
			cover = append(cover, textBox("subtitle", "subtitle", cs[0], cs[1], cs[2], cs[3], subtitle, sz(16), false))
		}
	}
	deck := []domain.Section{{Title: title, Body: subtitle, Notes: fmt.Sprintf(words.opening, title), Citations: cite, Figures: []string{}, Layout: cover}}
	titleBox := func(text string) domain.Element {
		g := geo.Title
		el := textBox("title", "title", g[0], g[1], g[2], g[3], text, sz(slideTitleSize), true)
		el.Align = geo.TitleAlign
		return el
	}
	// top is the share of the body area a slide's formula takes; the other
	// boxes fill the rest.
	top := 0.0
	below := func(y, h float64) (float64, float64) { return top + y*(1-top), h * (1 - top) }
	body := func(id, role string, x, y, w, h float64, text string, size float64) domain.Element {
		y, h = below(y, h)
		bx, by, bw, bh := in(geo.Body, x, y, w, h)
		return textBox(id, role, bx, by, bw, bh, text, sz(size), false)
	}
	image := func(x, y, w, h float64, path string) domain.Element {
		y, h = below(y, h)
		bx, by, bw, bh := in(geo.Body, x, y, w, h)
		return imageBox("figure", bx, by, bw, bh, path)
	}
	for i, sec := range sections {
		heading := PlainText(sec.Title)
		points := SlidePoints(sec.Body, lang)
		notes := strings.TrimSpace(sec.Notes)
		if notes == "" {
			notes = strings.TrimSpace(sec.Body)
		}
		picks := figures[i]
		if len(picks) > figuresPerTopic {
			picks = picks[:figuresPerTopic]
		}
		slide := domain.Section{Title: heading, Body: bulletText(points), Notes: notes, Citations: sec.Citations, Figures: []string{}}
		layout := []domain.Element{titleBox(heading)}
		top = 0
		if tex := strings.TrimSpace(sec.Equation); tex != "" {
			// The slide's key formula across the top of the body area.
			top = 0.2
			fx, fy, fw, fh := in(geo.Body, 0, 0, 1, 0.17)
			layout = append(layout, domain.Element{ID: "formula", Kind: "formula", Role: "formula", X: fx, Y: fy, W: fw, H: fh, Tex: tex})
		}
		switch {
		case len(picks) >= 2:
			// Two figures: two rows, each figure beside the points about it.
			groups := pointsByFigure(points, picks[:2])
			for r, fig := range picks[:2] {
				y := float64(r) * 0.52
				id := "points"
				if r == 1 {
					id = "points2"
				}
				img := image(0, y, 0.56, 0.46, fig.Path)
				if r == 1 {
					img.ID = "figure2"
				}
				layout = append(layout, img, body(id, "body", 0.6, y, 0.4, 0.46, bulletText(groups[r]), slideBodySize))
			}
			slide.Body = bulletText(append(append([]string{}, groups[0]...), groups[1]...))
			slide.Figures = []string{picks[0].Path, picks[1].Path}
			picks = picks[1:] // the second figure is on the slide already
		case len(picks) == 0:
			layout = append(layout, body("points", "body", 0.05, 0.06, 0.9, 0.88, bulletText(points), slideBodySize+6))
		case aspect(picks[0].Path) >= 1.9:
			// Wide figure: figure across the area, points underneath; more
			// points leave the figure less room rather than being dropped.
			if len(points) > 3 {
				layout = append(layout, image(0, 0, 1, 0.55, picks[0].Path), body("points", "body", 0, 0.58, 1, 0.42, bulletText(points), slideBodySize-1))
			} else {
				layout = append(layout, image(0, 0, 1, 0.69, picks[0].Path), body("points", "body", 0, 0.72, 1, 0.28, bulletText(points), slideBodySize))
			}
		case aspect(picks[0].Path) > 0 && aspect(picks[0].Path) < 0.9:
			// Tall figure: narrower figure column, wider text.
			layout = append(layout, image(0, 0, 0.45, 1, picks[0].Path), body("points", "body", 0.51, 0.045, 0.49, 0.91, bulletText(points), slideBodySize+2))
		default:
			layout = append(layout, image(0, 0, 0.625, 1, picks[0].Path), body("points", "body", 0.665, 0.045, 0.335, 0.91, bulletText(points), slideBodySize+2))
		}
		if len(picks) > 0 && len(slide.Figures) == 0 {
			slide.Figures = []string{picks[0].Path}
		}
		slide.Layout = layout
		deck = append(deck, slide)
		if len(picks) < 2 {
			continue
		}
		for _, fig := range picks[1:] {
			caption := figureCaption(fig)
			fl := []domain.Element{titleBox(heading), image(0.03, 0, 0.94, 0.86, fig.Path)}
			if caption != "" {
				fl = append(fl, centered(body("caption", "caption", 0.05, 0.89, 0.9, 0.11, Clip(caption, 220), slideCaptionSize)))
			}
			deck = append(deck, domain.Section{
				Title: heading, Body: Clip(caption, 220), Notes: strings.TrimSpace(words.figure + "\n" + caption),
				Citations: []string{fig.Citation}, Figures: []string{fig.Path}, Layout: fl,
			})
		}
	}
	closing := []domain.Element{
		centered(textBox("title", "display", 0.10, 0.30, 0.80, 0.20, words.closing, sz(54), true)),
		centered(textBox("subtitle", "subtitle", 0.10, 0.52, 0.80, 0.09, words.thanks, sz(20), false)),
	}
	if geo.Template {
		closing = []domain.Element{
			textBox("title", "display", ct[0], ct[1], ct[2], ct[3], words.closing, sz(48), true),
			textBox("subtitle", "subtitle", cs[0], cs[1], cs[2], cs[3], words.thanks, sz(20), false),
		}
		closing[0].Align, closing[1].Align = firstNonEmpty(geo.CoverAlign, "center"), firstNonEmpty(geo.CoverAlign, "center")
	}
	deck = append(deck, domain.Section{Title: words.closing, Body: words.thanks, Notes: words.ending, Citations: cite, Figures: []string{}, Layout: closing})
	return deck
}

// pointsByFigure splits a slide's points between its two figures: a point
// goes with the figure it names ("Figure 3", "圖 3"), a point naming neither
// with the one before it. When a figure gets no points, they are split in
// half.
func pointsByFigure(points []string, figs []domain.Image) [2][]string {
	var groups [2][]string
	keys := [2]string{captionKey(figs[0]), captionKey(figs[1])}
	row := 0
	for _, p := range points {
		for _, key := range mentionKeys(p) {
			for r := range keys {
				if keys[r] != "" && key == keys[r] {
					row = r
				}
			}
		}
		groups[row] = append(groups[row], p)
	}
	if len(groups[0]) == 0 || len(groups[1]) == 0 {
		half := (len(points) + 1) / 2
		groups = [2][]string{points[:half], points[half:]}
	}
	return groups
}

// SlideImages lists the image paths a slide layout references.
func SlideImages(sec domain.Section) []string {
	paths := []string{}
	for _, el := range sec.Layout {
		if el.Kind == "image" && el.Path != "" {
			paths = append(paths, el.Path)
		}
	}
	return paths
}

type mediaWriter interface {
	Create(name string) (io.Writer, error)
}

const emuPerPoint = 12700

// textLook is how a text box gets its colour and font: explicit values for
// the default look, inherited from the template (placeholders, scheme colours)
// for an imported one.
type textLook struct {
	fill   string // run fill XML; empty inherits
	font   string // typeface; empty inherits
	bullet string // bullet colour XML
	ph     string // placeholder element; empty for a plain text box
}

func solidFill(hex string) string {
	return `<a:solidFill><a:srgbClr val="` + hex + `"/></a:solidFill>`
}

func schemeFill(name string, alpha int) string {
	if alpha > 0 && alpha < 100 {
		return fmt.Sprintf(`<a:solidFill><a:schemeClr val="%s"><a:alpha val="%d000"/></a:schemeClr></a:solidFill>`, name, alpha)
	}
	return `<a:solidFill><a:schemeClr val="` + name + `"/></a:solidFill>`
}

func bulletColor(fill string) string {
	return strings.NewReplacer("<a:solidFill>", "<a:buClr>", "</a:solidFill>", "</a:buClr>").Replace(fill)
}

// textShape is an editable PowerPoint text box. Lines starting with "• " or
// "- " become bulleted paragraphs; the text shrinks to fit its box.
func textShape(id int, name string, px, py, w, h int, text string, size float64, bold bool, align, anchor string, look textLook) string {
	sz := int(size * 100)
	weight := ""
	if bold {
		weight = ` b="1"`
	}
	algn := ""
	if a := map[string]string{"center": "ctr", "right": "r"}[align]; a != "" {
		algn = ` algn="` + a + `"`
	} else if look.ph == "" || align == "left" {
		algn = ` algn="l"`
	}
	font := ""
	if look.font != "" {
		font = `<a:latin typeface="` + x(look.font) + `"/><a:ea typeface="` + x(look.font) + `"/>`
	}
	indent := int(size * 1.1 * emuPerPoint)
	var paragraphs strings.Builder
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		var ppr string
		if rest, ok := cutBullet(trimmed); ok {
			trimmed = rest
			before := int(size * 45)
			if i == 0 {
				before = 0
			}
			ppr = fmt.Sprintf(`<a:pPr marL="%d" indent="-%d"%s><a:lnSpc><a:spcPct val="112000"/></a:lnSpc><a:spcBef><a:spcPts val="%d"/></a:spcBef>%s<a:buFont typeface="Arial"/><a:buChar char="•"/></a:pPr>`, indent, indent, algn, before, bulletColor(look.bullet))
		} else {
			ppr = fmt.Sprintf(`<a:pPr marL="0" indent="0"%s><a:lnSpc><a:spcPct val="108000"/></a:lnSpc><a:buNone/></a:pPr>`, algn)
		}
		fmt.Fprintf(&paragraphs, `<a:p>%s<a:r><a:rPr lang="zh-TW" sz="%d"%s>%s%s</a:rPr><a:t xml:space="preserve">%s</a:t></a:r><a:endParaRPr lang="zh-TW" sz="%d"/></a:p>`, ppr, sz, weight, look.fill, font, x(trimmed), sz)
	}
	anchorAttr := ""
	if anchor != "" {
		anchorAttr = ` anchor="` + anchor + `"`
	}
	if look.ph != "" {
		return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr>%s</p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm></p:spPr><p:txBody><a:bodyPr%s><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, id, x(name), look.ph, px, py, w, h, anchorAttr, paragraphs.String())
	}
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/><a:ln><a:noFill/></a:ln></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="91440" rIns="91440" tIns="45720" bIns="45720"%s><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, id, x(name), px, py, w, h, anchorAttr, paragraphs.String())
}

// look is how a box of the given role is written for theme t.
func (t theme) look(role, kind string) textLook {
	if !t.template() {
		return textLook{fill: solidFill(t.textColor(role, kind)), font: t.font, bullet: solidFill(t.accent)}
	}
	l := textLook{fill: schemeFill("tx1", 0), bullet: schemeFill("accent1", 0)}
	switch {
	case kind == "content" && role == "title":
		l.fill, l.ph = "", `<p:ph type="title"/>`
	case kind == "display" && (role == "display" || role == "title"):
		l.fill, l.ph = "", `<p:ph type="`+t.coverPlaceholder+`"/>`
	case kind == "display" && role == "subtitle" && t.subtitleIdx != "":
		l.fill, l.ph = "", `<p:ph type="subTitle" idx="`+t.subtitleIdx+`"/>`
	case role == "caption" || role == "subtitle":
		l.fill = schemeFill("tx1", 70)
	case role == "kicker":
		l.fill = schemeFill("accent1", 0)
	}
	return l
}

func cutBullet(line string) (string, bool) {
	for _, prefix := range []string{"• ", "- ", "・"} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return line, false
}

// rectShape is a filled decoration rectangle.
func rectShape(id int, px, py, w, h int, fill string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Decoration %d"/><p:cNvSpPr/><p:nvPr userDrawn="1"/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr></p:sp>`, id, id, px, py, max(w, 1), max(h, 1), fill)
}

// decorShapes draws a theme's fixed decorations for a slide kind.
func decorShapes(t theme, kind string, width, height int) string {
	var b strings.Builder
	for i, d := range t.decorations(kind) {
		b.WriteString(rectShape(100+i, int(d.X*float64(width)), int(d.Y*float64(height)), int(d.W*float64(width)), int(d.H*float64(height)), d.Color))
	}
	return b.String()
}

// layoutShapes renders a slide's boxes as PowerPoint shapes. Images keep their
// aspect ratio and are centred inside their box.
func layoutShapes(archive mediaWriter, s *store.Store, sec domain.Section, n, width, height int, t theme, style domain.Style) (string, [][3]string, error) {
	var shapes strings.Builder
	kind := slideKind(sec)
	extra := [][3]string{}
	id, media := 10, 0
	addImage := func(path, name string, px, py, pw, ph int) error {
		file, err := s.Path(path)
		if err != nil {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(file))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			return nil
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		media++
		mediaName := fmt.Sprintf("p2l-slide%d-%d%s", n, media, ext)
		w, err := archive.Create("ppt/media/" + mediaName)
		if err != nil {
			return err
		}
		if _, err = w.Write(b); err != nil {
			return err
		}
		rid := fmt.Sprintf("rId%d", 2+media)
		extra = append(extra, [3]string{rid, "image", "../media/" + mediaName})
		fw, fh := fit(b, pw, ph)
		shapes.WriteString(picture(id, name, rid, px+(pw-fw)/2, py+(ph-fh)/2, fw, fh))
		id++
		return nil
	}
	for _, el := range sec.Layout {
		px, py := int(el.X*float64(width)), int(el.Y*float64(height))
		pw, ph := int(el.W*float64(width)), int(el.H*float64(height))
		if pw <= 0 || ph <= 0 {
			continue
		}
		switch el.Kind {
		case "image":
			if el.Path != "" {
				if err := addImage(el.Path, "Image box", px, py, pw, ph); err != nil {
					return "", nil, err
				}
			}
		case "formula":
			if b, r, ok := formulaPNG(el.Tex, true, t.fg, 40); ok {
				media++
				mediaName := fmt.Sprintf("p2l-slide%d-%d.png", n, media)
				w, err := archive.Create("ppt/media/" + mediaName)
				if err != nil {
					return "", nil, err
				}
				if _, err = w.Write(b); err != nil {
					return "", nil, err
				}
				rid := fmt.Sprintf("rId%d", 2+media)
				extra = append(extra, [3]string{rid, "image", "../media/" + mediaName})
				// Formulas are set at one type size (shrunk only to fit the box),
				// so a short formula is not blown up to the box's height.
				fw, fh := int(r.Width*formulaEx), int(r.Height*formulaEx)
				if fw > pw || fh > ph {
					fw, fh = fit(b, pw, ph)
				}
				shapes.WriteString(picture(id, "Formula", rid, px+(pw-fw)/2, py+(ph-fh)/2, fw, fh))
				id++
			} else {
				shapes.WriteString(textShape(id, "Formula", px, py, pw, ph, TexText(el.Tex), slideBodySize+2, false, "center", "ctr", t.look("body", kind)))
				id++
			}
		case "text":
			size := el.Size
			if size <= 0 {
				size = slideBodySize
			}
			look := t.look(el.Role, kind)
			name, anchor, align := "Text box", "t", el.Align
			switch el.Role {
			case "title":
				name, anchor = "Title", "ctr"
			case "display":
				name = "Title"
			case "caption":
				if align == "" {
					align = "center"
				}
			}
			if look.ph != "" {
				anchor = "" // the template's placeholder decides
				if align == "" {
					align = "left"
				}
			}
			shapes.WriteString(textShape(id, name, px, py, pw, ph, PlainText(el.Text), size, el.Bold, align, anchor, look))
			id++
		}
	}
	if t.template() {
		if kind == "content" && t.numIdx != "" {
			fmt.Fprintf(&shapes, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Slide Number"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldNum" sz="quarter" idx="%s"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:fld id="{4C2B3C1D-2B6E-4F7A-9C55-5A0D2B1E7A10}" type="slidenum"><a:t>%d</a:t></a:fld></a:p></p:txBody></p:sp>`, id, x(t.numIdx), n)
		}
		return shapes.String(), extra, nil
	}
	if kind == "content" {
		muted := textLook{fill: solidFill(t.muted), font: t.font, bullet: solidFill(t.accent)}
		shapes.WriteString(textShape(id, "Page number", int(0.88*float64(width)), int(0.93*float64(height)), int(0.08*float64(width)), int(0.06*float64(height)), strconvString(n), 11, false, "right", "ctr", muted))
		id++
		if fixed := strings.Join(stringsStyle(style, "fixed_elements"), " · "); fixed != "" {
			shapes.WriteString(textShape(id, "Footer", int(0.06*float64(width)), int(0.93*float64(height)), int(0.7*float64(width)), int(0.06*float64(height)), fixed, 10, false, "", "ctr", muted))
			id++
		}
	}
	return shapes.String(), extra, nil
}
