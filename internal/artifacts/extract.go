package artifacts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"eduforge/internal/documents"
	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// Extracting a template from an ordinary presentation. The deck's masters,
// layouts and theme already carry most of its design; what a deck repeats
// on most of its slides (a bar, a footer, a logo) or puts only on its cover
// is design too. Those shapes are offered as decorations; the kept ones move
// into the chosen layouts and every slide is dropped. Text that sits in one
// place but changes from slide to slide (section labels, page numbers) is
// content. Decks that draw their titles on the slides also get their design
// measured from the slides (see extract_style.go).

const maxDeckSlides = 60

var (
	sldIDPattern   = regexp.MustCompile(`<p:sldId\b[^>]*r:id="([^"]+)"`)
	spTreePattern  = regexp.MustCompile(`(?s)<p:spTree>(.*)</p:spTree>`)
	drawTextRun    = regexp.MustCompile(`(?s)<a:t>(.*?)</a:t>`)
	spPrBlock      = regexp.MustCompile(`(?s)<p:spPr\b[^>]*>.*?</p:spPr>`)
	solidSRGB      = regexp.MustCompile(`(?s)<a:solidFill>\s*<a:srgbClr val="([0-9A-Fa-f]{6})"`)
	solidScheme    = regexp.MustCompile(`(?s)<a:solidFill>\s*<a:schemeClr val="(\w+)"`)
	cNvPrID        = regexp.MustCompile(`(<p:cNvPr\b[^>]*\bid=")\d+(")`)
	hlinkPattern   = regexp.MustCompile(`(?s)<a:hlink(?:Click|Hover)\b[^>]*/>|<a:hlink(?:Click|Hover)\b[^>]*>.*?</a:hlink(?:Click|Hover)>`)
	relAttrPattern = regexp.MustCompile(`\br:(embed|link|id|pict)="([^"]+)"`)
	layoutName     = regexp.MustCompile(`<p:cSld\b[^>]*\bname="([^"]*)"`)
	grpSpPrEnd     = regexp.MustCompile(`</p:grpSpPr>|<p:grpSpPr\s*/>`)
)

// deckShape is one top-level shape of a slide.
type deckShape struct {
	kind, name, color, text, xml string
	box                          []float64
	placeholder                  string // placeholder type, "" for none
	// Text runs: size in points, colour and fonts carrying the most characters.
	size                    float64
	textColor, font, fontEA string
	chars                   int
}

type deckCandidate struct {
	info  domain.DeckDecoration
	xml   string
	slide string // the slide part it was taken from
}

func (p *templatePackage) slideParts() []string {
	pres := string(p.parts["ppt/presentation.xml"])
	rels := map[string]string{}
	for _, r := range parseRels(p.parts[relsPath("ppt/presentation.xml")]) {
		if strings.HasSuffix(r.Type, "/slide") {
			rels[r.ID] = resolvePart("ppt/presentation.xml", r.Target)
		}
	}
	out := []string{}
	for _, m := range sldIDPattern.FindAllStringSubmatch(pres, -1) {
		if part := rels[m[1]]; part != "" && p.parts[part] != nil {
			out = append(out, part)
		}
		if len(out) == maxDeckSlides {
			break
		}
	}
	return out
}

func (p *templatePackage) slideLayout(slide string) string {
	for _, r := range parseRels(p.parts[relsPath(slide)]) {
		if strings.HasSuffix(r.Type, "/slideLayout") {
			return resolvePart(slide, r.Target)
		}
	}
	return ""
}

func (p *templatePackage) fullTheme() map[string]string {
	theme := string(p.parts[p.theme])
	colors := map[string]string{}
	for _, slot := range ThemeSlots {
		if c := themeSlot(theme, slot); c != "" {
			colors[slot] = c
		}
	}
	return colors
}

func (p *templatePackage) shapes(part string, colors map[string]string) []deckShape {
	m := spTreePattern.FindStringSubmatch(string(p.parts[part]))
	if m == nil {
		return nil
	}
	out := []deckShape{}
	for _, el := range childElements(m[1]) {
		kind := map[string]string{"sp": "shape", "pic": "picture", "cxnSp": "line", "grpSp": "group"}[localName(el)]
		if kind == "" {
			continue // group properties, graphic frames (tables, charts), …
		}
		s := deckShape{kind: kind, xml: el}
		if ph := phPattern.FindString(el); ph != "" && kind != "group" {
			s.placeholder = firstNonEmpty(attrs(ph)["type"], "body")
		}
		if n := regexp.MustCompile(`<p:cNvPr\b[^>]*\bname="([^"]*)"`).FindStringSubmatch(el); n != nil {
			s.name = unescapeXML(n[1])
		}
		if x := xfrmPattern.FindStringSubmatch(el); x != nil && p.width > 0 && p.height > 0 {
			v := make([]float64, 4)
			for i, raw := range x[1:] {
				n, _ := strconv.ParseFloat(raw, 64)
				if i%2 == 0 {
					v[i] = n / float64(p.width)
				} else {
					v[i] = n / float64(p.height)
				}
			}
			s.box = v
		}
		if sp := spPrBlock.FindString(el); sp != "" {
			if c := solidSRGB.FindStringSubmatch(sp); c != nil {
				s.color = strings.ToUpper(c[1])
			} else if c := solidScheme.FindStringSubmatch(sp); c != nil {
				s.color = p.schemeColor(c[1], colors)
			}
		}
		texts := []string{}
		for _, t := range drawTextRun.FindAllStringSubmatch(el, -1) {
			texts = append(texts, unescapeXML(t[1]))
		}
		s.text = strings.TrimSpace(strings.Join(texts, " "))
		if kind == "shape" && s.text != "" {
			s.kind = "text"
			s.readRuns(p, colors)
		}
		out = append(out, s)
	}
	return out
}

func roundBox(b []float64) string {
	if len(b) != 4 {
		return "?"
	}
	parts := make([]string, 4)
	for i, v := range b {
		parts[i] = strconv.FormatFloat(math.Round(v*100)/100, 'f', 2, 64)
	}
	return strings.Join(parts, ",")
}

// posKey places a shape on the slide, ignoring its size and text.
func posKey(s deckShape) string {
	return fmt.Sprintf("%s|%.2f,%.2f", s.kind, s.box[0], s.box[1])
}

// analyze reads a deck's slides, layouts, decoration candidates and, for
// decks that draw on their slides, the measured style.
func (p *templatePackage) analyze() (domain.DeckAnalysis, []deckCandidate) {
	a := domain.DeckAnalysis{Width: p.width, Height: p.height, Slides: []domain.DeckSlide{}, Layouts: []domain.DeckLayout{}, Decorations: []domain.DeckDecoration{}, Skipped: []string{}}
	colors := p.fullTheme()
	layouts := map[string]*domain.DeckLayout{}
	order := []string{}
	byKey := map[string]*deckCandidate{}
	keys := []string{}
	all := [][]deckShape{}
	// Text seen at each position, to tell fixed text from changing text.
	textsAt := map[string][]string{}
	slidesAt := map[string]map[int]bool{}
	for i, slide := range p.slideParts() {
		n := i + 1
		layout := p.slideLayout(slide)
		ds := domain.DeckSlide{N: n, Layout: layout, Texts: []string{}}
		shapes := p.shapes(slide, colors)
		all = append(all, shapes)
		for _, s := range shapes {
			if s.text != "" && len(ds.Texts) < 4 {
				ds.Texts = append(ds.Texts, clipText(s.text, 120))
			}
			if s.placeholder != "" || len(s.box) != 4 {
				continue
			}
			if s.kind == "text" {
				at := posKey(s)
				if !contains(textsAt[at], s.text) {
					textsAt[at] = append(textsAt[at], s.text)
				}
				if slidesAt[at] == nil {
					slidesAt[at] = map[int]bool{}
				}
				slidesAt[at][n] = true
			}
			key := s.kind + "|" + roundBox(s.box) + "|" + s.text
			c, ok := byKey[key]
			if !ok {
				c = &deckCandidate{xml: s.xml, slide: slide, info: domain.DeckDecoration{Kind: s.kind, Name: s.name, Box: s.box, Color: s.color, Text: clipText(s.text, 80)}}
				byKey[key] = c
				keys = append(keys, key)
			}
			if len(c.info.Slides) == 0 || c.info.Slides[len(c.info.Slides)-1] != n {
				c.info.Slides = append(c.info.Slides, n)
			}
		}
		a.Slides = append(a.Slides, ds)
		if layout == "" {
			continue
		}
		l, ok := layouts[layout]
		if !ok {
			x := string(p.parts[layout])
			l = &domain.DeckLayout{Part: layout, Placeholders: []string{}}
			if m := layoutName.FindStringSubmatch(x); m != nil {
				l.Name = unescapeXML(m[1])
			}
			if m := layoutTypeAttr.FindStringSubmatch(x); m != nil {
				l.Type = m[1]
			}
			for _, ph := range placeholders(x) {
				l.Placeholders = append(l.Placeholders, ph.Type)
			}
			layouts[layout] = l
			order = append(order, layout)
		}
		l.Slides = append(l.Slides, n)
	}
	for _, part := range order {
		a.Layouts = append(a.Layouts, *layouts[part])
	}
	layoutOf := map[int]string{}
	for _, s := range a.Slides {
		layoutOf[s.N] = s.Layout
	}
	changing := map[string]bool{}
	for _, at := range sortedKeys(textsAt) {
		texts := textsAt[at]
		counter := true
		for _, t := range texts {
			counter = counter && pageCounter.MatchString(t)
		}
		if len(slidesAt[at]) < 2 || (len(texts) < 2 && !counter) {
			continue
		}
		changing[at] = true
		// Only list what recurs on a good share of the slides (section
		// labels, page numbers, footers), not text that shares a spot by chance.
		if len(slidesAt[at]) < 3 || len(slidesAt[at])*3 < len(a.Slides) {
			continue
		}
		sample := texts
		if len(sample) > 4 {
			sample = append(append([]string{}, sample[:4]...), "…")
		}
		for i := range sample {
			sample[i] = clipText(sample[i], 40)
		}
		a.Skipped = append(a.Skipped, strings.Join(sample, " / "))
	}
	candidates := []deckCandidate{}
	for _, key := range keys {
		c := byKey[key]
		if c.info.Kind == "text" && (changing[posKey(deckShape{kind: "text", box: c.info.Box})] || pageCounter.MatchString(c.info.Text)) {
			continue
		}
		// Share of the content slides (on the same layouts) that carry it.
		on := map[string]bool{}
		count := 0
		for _, n := range c.info.Slides {
			on[layoutOf[n]] = true
			if n != 1 {
				count++
			}
		}
		pool := 0
		for _, s := range a.Slides {
			if s.N != 1 && on[s.Layout] {
				pool++
			}
		}
		share := 0.0
		if pool > 0 {
			share = float64(count) / float64(pool)
		}
		repeated := count >= 2 && share >= 0.34
		coverOnly := len(c.info.Slides) == 1 && c.info.Slides[0] == 1 && c.info.Kind != "text"
		if !repeated && !coverOnly {
			continue
		}
		c.info.ID = fmt.Sprintf("d%d", len(candidates)+1)
		c.info.Default = (coverOnly || share >= 0.5) && (c.info.Kind != "text" || len([]rune(c.info.Text)) <= 60)
		candidates = append(candidates, *c)
		a.Decorations = append(a.Decorations, c.info)
	}
	a.Style = inferStyle(all, colors)
	return a, candidates
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// AnalyzeDeck reads an uploaded presentation for extraction.
func AnalyzeDeck(file string) (domain.DeckAnalysis, error) {
	p, err := openTemplate(file)
	if err != nil {
		return domain.DeckAnalysis{}, err
	}
	a, _ := p.analyze()
	if len(a.Slides) == 0 {
		return a, errors.New("deck_has_no_slides")
	}
	return a, nil
}

var layoutRoles = map[string]string{
	"title": "cover", "secHead": "section", "obj": "content", "tx": "content", "titleOnly": "content",
	"twoObj": "two_column", "twoTxTwoObj": "two_column", "objTx": "figure", "picTx": "figure",
}

// SuggestDeck is the rule-based suggestion: the first slide's layout is the
// cover, the layout most other slides use carries content (it may be the
// cover's own layout: building then gives the cover a copy of its own), every
// default decoration is kept and a measured style is applied.
func SuggestDeck(a domain.DeckAnalysis, name string) domain.DeckSuggestion {
	s := domain.DeckSuggestion{Name: name, LayoutTypes: map[string]string{}, Decorations: []string{}, ApplyStyle: a.Style != nil}
	if len(a.Slides) > 0 {
		s.CoverLayout = a.Slides[0].Layout
	}
	best := 0
	for _, l := range a.Layouts {
		count := len(l.Slides)
		if l.Part == s.CoverLayout {
			count-- // not counting the cover itself
		}
		if count > best {
			best, s.ContentLayout = count, l.Part
		}
		s.LayoutTypes[l.Part] = firstNonEmpty(layoutRoles[l.Type], "other")
	}
	if s.ContentLayout == "" {
		s.ContentLayout = s.CoverLayout
	}
	if s.CoverLayout != "" {
		s.LayoutTypes[s.CoverLayout] = "cover"
	}
	if s.ContentLayout != s.CoverLayout {
		s.LayoutTypes[s.ContentLayout] = "content"
	} else if s.ContentLayout != "" {
		s.LayoutTypes[s.ContentLayout] = "content"
	}
	for _, d := range a.Decorations {
		if d.Default {
			s.Decorations = append(s.Decorations, d.ID)
		}
	}
	return s
}

// LayoutRoles are the page types a layout may be given.
var LayoutRoles = []string{"cover", "section", "content", "two_column", "figure", "closing", "other"}

// CheckSuggestion verifies a suggestion only names this deck's layouts and decorations.
func CheckSuggestion(a domain.DeckAnalysis, s *domain.DeckSuggestion) error {
	bad := errors.New("invalid_extraction")
	layouts := map[string]bool{}
	for _, l := range a.Layouts {
		layouts[l.Part] = true
	}
	if !layouts[s.CoverLayout] || !layouts[s.ContentLayout] {
		return bad
	}
	for part, role := range s.LayoutTypes {
		if !layouts[part] || !contains(LayoutRoles, role) {
			return bad
		}
	}
	ids := map[string]bool{}
	for _, d := range a.Decorations {
		ids[d.ID] = true
	}
	seen := map[string]bool{}
	kept := []string{}
	for _, id := range s.Decorations {
		if !ids[id] {
			return bad
		}
		if !seen[id] {
			kept = append(kept, id)
			seen[id] = true
		}
	}
	s.Decorations = kept
	s.Name = clipText(strings.TrimSpace(s.Name), 80)
	s.Description = clipText(strings.TrimSpace(s.Description), 600)
	return nil
}

var (
	sldLayoutIDTag = regexp.MustCompile(`<p:sldLayoutId\b[^>]*\bid="(\d+)"`)
	sldMasterIDTag = regexp.MustCompile(`<p:sldMasterId\b[^>]*\bid="(\d+)"`)
	layoutPartName = regexp.MustCompile(`^ppt/slideLayouts/slideLayout(\d+)\.xml$`)
	cSldOpen       = regexp.MustCompile(`<p:cSld\b[^>]*>`)
)

// cloneLayout copies a layout (with its relationships) as a new layout of
// the master, named name, and returns the new part.
func (p *templatePackage) cloneLayout(part, name string) (string, error) {
	next := 0
	for existing := range p.parts {
		if m := layoutPartName.FindStringSubmatch(existing); m != nil {
			n, _ := strconv.Atoi(m[1])
			next = max(next, n)
		}
	}
	clone := fmt.Sprintf("ppt/slideLayouts/slideLayout%d.xml", next+1)
	layout := string(p.parts[part])
	open := cSldOpen.FindString(layout)
	if open == "" {
		return "", errors.New("template_layout_missing")
	}
	p.parts[clone] = []byte(strings.Replace(layout, open, setAttrs(open, map[string]string{"name": name}), 1))
	p.order = append(p.order, clone)
	if r, ok := p.parts[relsPath(part)]; ok {
		p.parts[relsPath(clone)] = r
		p.order = append(p.order, relsPath(clone))
	}
	// Layout ids share one range with master ids.
	id := int64(2147483648)
	for _, pattern := range []struct {
		re   *regexp.Regexp
		part string
	}{{sldLayoutIDTag, p.master}, {sldMasterIDTag, "ppt/presentation.xml"}} {
		for _, m := range pattern.re.FindAllStringSubmatch(string(p.parts[pattern.part]), -1) {
			n, _ := strconv.ParseInt(m[1], 10, 64)
			id = max(id, n+1)
		}
	}
	master := string(p.parts[p.master])
	if !strings.Contains(master, "</p:sldLayoutIdLst>") {
		return "", errors.New("template_layout_missing")
	}
	rid := "rIdP2LCoverLayout"
	p.parts[p.master] = []byte(strings.Replace(master, "</p:sldLayoutIdLst>", fmt.Sprintf(`<p:sldLayoutId id="%d" r:id="%s"/></p:sldLayoutIdLst>`, id, rid), 1))
	masterRels := string(p.parts[relsPath(p.master)])
	p.parts[relsPath(p.master)] = []byte(strings.Replace(masterRels, "</Relationships>", fmt.Sprintf(`<Relationship Id="%s" Type="%s/slideLayout" Target="%s"/></Relationships>`, rid, nsR, x(relTarget(p.master, clone))), 1))
	types := string(p.parts["[Content_Types].xml"])
	p.parts["[Content_Types].xml"] = []byte(strings.Replace(types, "</Types>", `<Override PartName="/`+clone+`" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/></Types>`, 1))
	p.layouts = append(p.layouts, clone)
	return clone, nil
}

// BuildExtractedTemplate moves the kept decorations into the cover and
// content layouts and drops every slide, returning a template file and its
// cover layout (a copy of the content layout when the deck used one layout
// for both).
func BuildExtractedTemplate(file string, s domain.DeckSuggestion) ([]byte, string, error) {
	p, err := openTemplate(file)
	if err != nil {
		return nil, "", err
	}
	a, candidates := p.analyze()
	if err := CheckSuggestion(a, &s); err != nil {
		return nil, "", err
	}
	if s.CoverLayout == s.ContentLayout {
		if s.CoverLayout, err = p.cloneLayout(s.ContentLayout, "EduForge Cover"); err != nil {
			return nil, "", err
		}
	}
	keep := map[string]bool{}
	for _, id := range s.Decorations {
		keep[id] = true
	}
	nextID, nextRel := 3000, 0
	insert := map[string][]string{} // layout → shapes, in order
	for _, c := range candidates {
		if !keep[c.info.ID] {
			continue
		}
		onCover, elsewhere := false, false
		for _, n := range c.info.Slides {
			if n == 1 {
				onCover = true
			} else {
				elsewhere = true
			}
		}
		targets := []string{}
		if onCover {
			targets = append(targets, s.CoverLayout)
		}
		if elsewhere {
			targets = append(targets, s.ContentLayout)
		}
		slideRels := map[string]ooxmlRel{}
		for _, r := range parseRels(p.parts[relsPath(c.slide)]) {
			slideRels[r.ID] = r
		}
		for _, layout := range uniq(targets) {
			xml := hlinkPattern.ReplaceAllString(c.xml, "")
			xml = cNvPrID.ReplaceAllStringFunc(xml, func(m string) string {
				nextID++
				return cNvPrID.ReplaceAllString(m, "${1}"+strconv.Itoa(nextID)+"${2}")
			})
			ok := true
			added := []string{}
			xml = relAttrPattern.ReplaceAllStringFunc(xml, func(m string) string {
				sub := relAttrPattern.FindStringSubmatch(m)
				r, found := slideRels[sub[2]]
				if !found || !strings.HasSuffix(r.Type, "/image") || strings.Contains(r.Target, "://") {
					ok = false
					return m
				}
				nextRel++
				id := fmt.Sprintf("rIdP2LDeco%d", nextRel)
				target := relTarget(layout, resolvePart(c.slide, r.Target))
				added = append(added, fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, id, x(r.Type), x(target)))
				return `r:` + sub[1] + `="` + id + `"`
			})
			if !ok {
				continue // shapes tied to charts, media or links stay behind
			}
			insert[layout] = append(insert[layout], xml)
			if len(added) > 0 {
				relsName := relsPath(layout)
				relsXML := string(p.parts[relsName])
				if relsXML == "" {
					relsXML = xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
				}
				p.parts[relsName] = []byte(strings.Replace(relsXML, "</Relationships>", strings.Join(added, "")+"</Relationships>", 1))
			}
		}
	}
	for layout, shapes := range insert {
		x := string(p.parts[layout])
		loc := grpSpPrEnd.FindStringIndex(x)
		if loc == nil {
			continue
		}
		// Decorations go behind the layout's placeholders.
		p.parts[layout] = []byte(x[:loc[1]] + strings.Join(shapes, "") + x[loc[1]:])
	}
	staged, err := zipParts(p.parts, p.order)
	if err != nil {
		return nil, "", err
	}
	tmp, err := os.MkdirTemp("", "eduforge-extract-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "staged.pptx")
	if err = os.WriteFile(path, staged, 0600); err != nil {
		return nil, "", err
	}
	out, err := buildFromTemplate(path, nil, nil)
	return out, s.CoverLayout, err
}

// ApplyDeckStyle overlays a measured style on a template's spec.
func ApplyDeckStyle(spec *TemplateSpec, st *domain.DeckStyle) {
	if st == nil {
		return
	}
	for slot, c := range st.Colors {
		spec.Colors[slot] = c
	}
	for _, f := range []struct {
		to   *string
		from string
	}{{&spec.HeadingFont, st.HeadingFont}, {&spec.BodyFont, st.BodyFont}, {&spec.HeadingFontEA, st.HeadingFontEA}, {&spec.BodyFontEA, st.BodyFontEA}} {
		if f.from != "" {
			*f.to = f.from
		}
	}
	for _, b := range []struct {
		to   *[]float64
		from []float64
	}{{&spec.TitleBox, st.TitleBox}, {&spec.BodyBox, st.BodyBox}, {&spec.CoverTitleBox, st.CoverTitleBox}, {&spec.CoverSubtitleBox, st.CoverSubtitleBox}} {
		if b.from != nil {
			*b.to = append([]float64(nil), b.from...)
		}
	}
}

func uniq(list []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range list {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// RenderDeckThumbs renders up to max slides of a deck as small images.
func RenderDeckThumbs(ctx context.Context, s *store.Store, file string, max int) ([]string, error) {
	tmp, err := os.MkdirTemp("", "eduforge-deck-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	deck := filepath.Join(tmp, "deck"+filepath.Ext(file))
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(deck, b, 0600); err != nil {
		return nil, err
	}
	pdf, err := documents.ConvertPDF(ctx, deck, tmp)
	if err != nil {
		return nil, err
	}
	if _, err = documents.Command(ctx, "pdftoppm", "-l", strconv.Itoa(max), "-scale-to", "480", "-png", pdf, filepath.Join(tmp, "slide")); err != nil {
		return nil, err
	}
	pages, _ := filepath.Glob(filepath.Join(tmp, "slide-*.png"))
	sort.Slice(pages, func(i, j int) bool {
		num := func(p string) int {
			n, _ := strconv.Atoi(strings.TrimSuffix(filepath.Base(p)[strings.LastIndex(filepath.Base(p), "-")+1:], ".png"))
			return n
		}
		return num(pages[i]) < num(pages[j])
	})
	out := []string{}
	for _, page := range pages {
		png, err := os.ReadFile(page)
		if err != nil {
			return out, err
		}
		rel, _, err := s.Blob(png, ".png")
		if err != nil {
			return out, err
		}
		out = append(out, rel)
	}
	return out, nil
}
