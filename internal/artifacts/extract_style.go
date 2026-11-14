package artifacts

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"eduforge/internal/domain"
)

// Many decks (especially generated ones) draw everything on the slides: one
// blank layout, the Office theme left as it is, and every title, colour and
// font set on the slide's own text boxes. Their design is only visible by
// measuring the slides: where the title sits on most pages, which colours
// the text and highlights use, which fonts the runs name. That measurement
// becomes the template's theme and title/content areas.

var (
	runPattern   = regexp.MustCompile(`(?s)<a:r>(.*?)</a:r>`)
	rPrPattern   = regexp.MustCompile(`(?s)<a:rPr\b[^>]*/>|<a:rPr\b[^>]*>.*?</a:rPr>`)
	szAttr       = regexp.MustCompile(`\bsz="(\d+)"`)
	latinFace    = regexp.MustCompile(`<a:latin\b[^>]*\btypeface="([^"]*)"`)
	eaFace       = regexp.MustCompile(`<a:ea\b[^>]*\btypeface="([^"]*)"`)
	defRPrSz     = regexp.MustCompile(`<a:defRPr\b[^>]*\bsz="(\d+)"`)
	pageCounter  = regexp.MustCompile(`^\s*\d{1,3}\s*(?:(?:/|／|of)\s*\d{1,3})?\s*$`)
	cjkFontNames = regexp.MustCompile(`(?i)cjk|han\b|hei|ming|song|kai|gothic|jhenghei|yahei|pingfang|meiryo|mincho|batang|gulim|malgun|[^\x00-\x7f]`)
)

// textRuns reads the size (points), colour and fonts of a shape's text,
// each the value carrying the most characters.
func (s *deckShape) readRuns(p *templatePackage, colors map[string]string) {
	size, color, font, ea := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, m := range runPattern.FindAllStringSubmatch(s.xml, -1) {
		n := 0
		for _, t := range drawTextRun.FindAllStringSubmatch(m[1], -1) {
			n += len([]rune(strings.TrimSpace(unescapeXML(t[1]))))
		}
		if n == 0 {
			continue
		}
		s.chars += n
		rpr := rPrPattern.FindString(m[1])
		if v := szAttr.FindStringSubmatch(rpr); v != nil {
			size[v[1]] += n
		}
		if c := solidSRGB.FindStringSubmatch(rpr); c != nil {
			color[strings.ToUpper(c[1])] += n
		} else if c := solidScheme.FindStringSubmatch(rpr); c != nil {
			if hex := p.schemeColor(c[1], colors); hex != "" {
				color[hex] += n
			}
		}
		if f := latinFace.FindStringSubmatch(rpr); f != nil && f[1] != "" && !strings.HasPrefix(f[1], "+") {
			font[unescapeXML(f[1])] += n
		}
		if f := eaFace.FindStringSubmatch(rpr); f != nil && f[1] != "" && !strings.HasPrefix(f[1], "+") {
			ea[unescapeXML(f[1])] += n
		}
	}
	if v := top(size); v != "" {
		hundredths, _ := strconv.Atoi(v)
		s.size = float64(hundredths) / 100
	} else if v := defRPrSz.FindStringSubmatch(s.xml); v != nil {
		hundredths, _ := strconv.Atoi(v[1])
		s.size = float64(hundredths) / 100
	}
	s.textColor, s.font, s.fontEA = top(color), top(font), top(ea)
}

// top is the key with the largest count (ties broken by key, for stable output).
func top(counts map[string]int) string {
	best, bestN := "", 0
	for k, n := range counts {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	return v[len(v)/2]
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func union(a, b []float64) []float64 {
	if a == nil {
		return append([]float64(nil), b...)
	}
	x0, y0 := math.Min(a[0], b[0]), math.Min(a[1], b[1])
	x1, y1 := math.Max(a[0]+a[2], b[0]+b[2]), math.Max(a[1]+a[3], b[1]+b[3])
	return []float64{x0, y0, x1 - x0, y1 - y0}
}

// clampBox keeps a box on the slide with a usable size.
func clampBox(b []float64) []float64 {
	if b == nil {
		return nil
	}
	out := make([]float64, 4)
	out[0] = math.Min(math.Max(b[0], 0), 0.95)
	out[1] = math.Min(math.Max(b[1], 0), 0.95)
	out[2] = math.Min(math.Max(b[2], 0.05), 1-out[0])
	out[3] = math.Min(math.Max(b[3], 0.05), 1-out[1])
	for i := range out {
		out[i] = round3(out[i])
	}
	return out
}

// hsv returns a colour's hue (degrees), saturation and value (0–1).
func hsv(hex string) (float64, float64, float64) {
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || len(hex) != 6 {
		return 0, 0, 0
	}
	r, g, b := float64(n>>16&255)/255, float64(n>>8&255)/255, float64(n&255)/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d := mx - mn
	h := 0.0
	switch {
	case d == 0:
	case mx == r:
		h = math.Mod((g-b)/d, 6)
	case mx == g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	s := 0.0
	if mx > 0 {
		s = d / mx
	}
	return h, s, mx
}

func hueGap(a, b float64) float64 {
	d := math.Abs(a - b)
	return math.Min(d, 360-d)
}

func looksCJKFont(name string) bool { return cjkFontNames.MatchString(name) }

// isTitlePlaceholder reports a slide title held in a placeholder.
func (s deckShape) isTitlePlaceholder() bool {
	return (s.placeholder == "title" || s.placeholder == "ctrTitle") && s.text != ""
}

// inferStyle measures a deck's design from its slides. It returns nil for
// decks whose titles sit in placeholders: their layouts already carry the
// design. slides[0] is the cover.
func inferStyle(slides [][]deckShape, theme map[string]string) *domain.DeckStyle {
	if len(slides) < 3 {
		return nil
	}
	content := slides[1:]
	placed := 0
	for _, shapes := range content {
		for _, s := range shapes {
			if s.isTitlePlaceholder() {
				placed++
				break
			}
		}
	}
	if placed*2 > len(content) {
		return nil
	}
	st := &domain.DeckStyle{Colors: map[string]string{}}

	// Titles: the largest text near the top of each content slide.
	type titled struct {
		title deckShape
		rest  []deckShape
	}
	titles := []titled{}
	for _, shapes := range content {
		best := -1
		for i, s := range shapes {
			if s.kind != "text" || len(s.box) != 4 || s.box[1] > 0.3 || s.size < 16 {
				continue
			}
			if best < 0 || s.size > shapes[best].size || (s.size == shapes[best].size && s.box[1] < shapes[best].box[1]) {
				best = i
			}
		}
		if best >= 0 {
			rest := []deckShape{}
			for i, s := range shapes {
				if i != best {
					rest = append(rest, s)
				}
			}
			titles = append(titles, titled{shapes[best], rest})
		}
	}
	if len(titles)*2 >= len(content) && len(titles) >= 2 {
		xs, ys, ws, hs := []float64{}, []float64{}, []float64{}, []float64{}
		for _, t := range titles {
			xs, ys, ws, hs = append(xs, t.title.box[0]), append(ys, t.title.box[1]), append(ws, t.title.box[2]), append(hs, t.title.box[3])
		}
		box := []float64{median(xs), median(ys), median(ws), median(hs)}
		agree := 0
		for _, t := range titles {
			if math.Abs(t.title.box[0]-box[0]) < 0.03 && math.Abs(t.title.box[1]-box[1]) < 0.05 {
				agree++
			}
		}
		if agree*2 >= len(titles) {
			st.TitleBox = clampBox(box)
			// A line under the title on most slides (a lede) belongs to the header.
			bottom := box[1] + box[3]
			ledes := []float64{}
			for _, t := range titles {
				for _, s := range t.rest {
					if s.kind == "text" && len(s.box) == 4 && s.box[1] >= bottom-0.02 && s.box[1] <= bottom+0.04 && s.size < t.title.size {
						ledes = append(ledes, s.box[1]+s.box[3])
						break
					}
				}
			}
			if len(ledes)*2 >= len(titles) {
				bottom = math.Max(bottom, median(ledes))
			}
			// Footers: text low on the slide on most slides.
			foot := []float64{}
			for _, t := range titles {
				low := 1.0
				for _, s := range t.rest {
					if s.kind == "text" && len(s.box) == 4 && s.box[1] >= 0.82 {
						low = math.Min(low, s.box[1])
					}
				}
				if low < 1 {
					foot = append(foot, low)
				}
			}
			end := 0.92
			if len(foot)*2 >= len(titles) {
				end = median(foot) - 0.02
			}
			top := bottom + 0.025
			if end-top >= 0.3 {
				st.BodyBox = clampBox([]float64{box[0], top, box[2], end - top})
			}
		}
	}

	// Cover: the largest text, the lines right under it that are still large,
	// and the smaller lines below as the subtitle.
	cover := []deckShape{}
	for _, s := range slides[0] {
		if s.kind == "text" && len(s.box) == 4 && s.size > 0 {
			cover = append(cover, s)
		}
	}
	sort.SliceStable(cover, func(i, j int) bool { return cover[i].box[1] < cover[j].box[1] })
	main := -1
	for i, s := range cover {
		if s.size >= 18 && (main < 0 || s.size > cover[main].size) {
			main = i
		}
	}
	if main >= 0 {
		title := append([]float64(nil), cover[main].box...)
		next := main + 1
		for ; next < len(cover); next++ {
			s := cover[next]
			bottom := title[1] + title[3]
			if s.box[1] < bottom-0.02 || s.box[1] > bottom+0.05 || s.size < cover[main].size*0.5 || math.Abs(s.box[0]-title[0]) > 0.05 {
				break
			}
			title = union(title, s.box)
		}
		st.CoverTitleBox = clampBox(title)
		var sub []float64
		for _, s := range cover[next:] {
			if s.box[1] < title[1]+title[3]-0.02 || s.box[1]+s.box[3] > 0.86 || math.Abs(s.box[0]-title[0]) > 0.08 {
				continue
			}
			sub = union(sub, s.box)
		}
		st.CoverSubtitleBox = clampBox(sub)
	}

	// Colours: the title colour is the text colour; the most used saturated
	// colours (text and fills) are the accents.
	titleColor := map[string]int{}
	for _, t := range titles {
		if t.title.textColor != "" {
			titleColor[t.title.textColor] += t.title.chars
		}
	}
	accents, greys := map[string]int{}, map[string]int{}
	textColor := top(titleColor)
	for _, shapes := range slides {
		for _, s := range shapes {
			if s.textColor != "" {
				if _, sat, val := hsv(s.textColor); sat >= 0.4 && val >= 0.3 {
					accents[s.textColor] += s.chars
				} else if s.textColor != textColor && sat < 0.25 && val < 0.75 && val > 0.2 && s.size >= 10 {
					// Secondary text: greys at reading size, not footnotes.
					greys[s.textColor] += s.chars
				}
			}
			if s.color != "" && s.kind != "text" {
				if _, sat, val := hsv(s.color); sat >= 0.4 && val >= 0.3 {
					accents[s.color] += 20
				}
			}
		}
	}
	set := func(slot, hex string) {
		if hex != "" && !strings.EqualFold(theme[slot], hex) {
			st.Colors[slot] = strings.ToUpper(hex)
		}
	}
	if _, _, val := hsv(textColor); textColor != "" && val < 0.45 {
		set("dk1", textColor)
	}
	set("dk2", top(greys))
	type counted struct {
		hex string
		n   int
	}
	ranked := []counted{}
	for hex, n := range accents {
		ranked = append(ranked, counted{hex, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].n != ranked[j].n {
			return ranked[i].n > ranked[j].n
		}
		return ranked[i].hex < ranked[j].hex
	})
	picked := []string{}
	for _, c := range ranked {
		h, _, _ := hsv(c.hex)
		distinct := true
		for _, p := range picked {
			ph, _, _ := hsv(p)
			if hueGap(h, ph) < 25 {
				distinct = false
			}
		}
		if distinct {
			picked = append(picked, c.hex)
		}
		if len(picked) == 3 {
			break
		}
	}
	for i, hex := range picked {
		set("accent"+strconv.Itoa(i+1), hex)
	}

	// Fonts: what the title runs and the other runs name.
	heading, body, headingEA, bodyEA := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, t := range titles {
		if t.title.font != "" {
			heading[t.title.font] += t.title.chars
		}
		if t.title.fontEA != "" && looksCJKFont(t.title.fontEA) {
			headingEA[t.title.fontEA] += t.title.chars
		}
		for _, s := range t.rest {
			if s.font != "" {
				body[s.font] += s.chars
			}
			if s.fontEA != "" && looksCJKFont(s.fontEA) {
				bodyEA[s.fontEA] += s.chars
			}
		}
	}
	font := func(counts map[string]int) string {
		if f := top(counts); validFont(f) {
			return f
		}
		return ""
	}
	st.HeadingFont, st.BodyFont = font(heading), font(body)
	st.HeadingFontEA, st.BodyFontEA = font(headingEA), font(bodyEA)
	if st.HeadingFont == "" {
		st.HeadingFont = st.BodyFont
	}
	if st.TitleBox == nil && st.CoverTitleBox == nil && len(st.Colors) == 0 && st.HeadingFont == "" {
		return nil
	}
	return st
}
