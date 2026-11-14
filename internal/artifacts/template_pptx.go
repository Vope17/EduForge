package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"eduforge/internal/documents"
	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// Imported PowerPoint templates. A template is the user's own .pptx/.potx:
// its masters, layouts and theme are kept, its sample slides are dropped and
// the generated slides are placed on two of its layouts (a title layout for
// the cover and closing slides, a title-only layout for content). Titles use
// the layout's title placeholder so they take the template's font, colour and
// alignment.

const maxTemplateBytes = 64 << 20

type ooxmlRel struct{ ID, Type, Target string }

var (
	relPattern     = regexp.MustCompile(`<Relationship\b[^>]*>`)
	attrPattern    = regexp.MustCompile(`(\w+)="([^"]*)"`)
	spPattern      = regexp.MustCompile(`(?s)<p:sp>.*?</p:sp>`)
	phPattern      = regexp.MustCompile(`<p:ph\b[^>]*>`)
	xfrmPattern    = regexp.MustCompile(`(?s)<a:xfrm\b[^>]*>\s*<a:off x="(-?\d+)" y="(-?\d+)"\s*/>\s*<a:ext cx="(\d+)" cy="(\d+)"\s*/>`)
	sldSzPattern   = regexp.MustCompile(`<p:sldSz\b[^>]*>`)
	layoutTypeAttr = regexp.MustCompile(`<p:sldLayout\b[^>]*\btype="(\w+)"`)
	clrMapPattern  = regexp.MustCompile(`<p:clrMap\b[^>]*>`)
	sldIDListBlock = regexp.MustCompile(`(?s)<p:sldIdLst>.*?</p:sldIdLst>|<p:sldIdLst\s*/>`)
	custShowBlock  = regexp.MustCompile(`(?s)<p:custShowLst>.*?</p:custShowLst>`)
	extBlock       = regexp.MustCompile(`(?s)<p:ext\b[^>]*>.*?</p:ext>`)
)

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrPattern.FindAllStringSubmatch(tag, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func parseRels(b []byte) []ooxmlRel {
	out := []ooxmlRel{}
	for _, tag := range relPattern.FindAllString(string(b), -1) {
		a := attrs(tag)
		out = append(out, ooxmlRel{ID: a["Id"], Type: a["Type"], Target: a["Target"]})
	}
	return out
}

func relsPath(part string) string { return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels") }

// resolvePart turns a relationship target into a package part name.
func resolvePart(part, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Clean(path.Join(path.Dir(part), target))
}

// relTarget makes a part name relative to the part that references it.
func relTarget(from, to string) string {
	rel, err := filepath.Rel(path.Dir(from), to)
	if err != nil {
		return "/" + to
	}
	return filepath.ToSlash(rel)
}

type placeholder struct {
	Type  string
	Idx   string
	Box   []int64 // x, y, w, h in EMU; nil when inherited
	Align string  // first paragraph alignment it sets, if any
}

var alignPattern = regexp.MustCompile(`<a:(?:lvl1pPr|pPr)\b[^>]*\balgn="(\w+)"`)

// boxAlign maps DrawingML alignment to an Element's Align.
func boxAlign(algn string) string {
	switch algn {
	case "ctr":
		return "center"
	case "r":
		return "right"
	}
	return ""
}

func placeholders(xml string) []placeholder {
	out := []placeholder{}
	for _, sp := range spPattern.FindAllString(xml, -1) {
		tag := phPattern.FindString(sp)
		if tag == "" {
			continue
		}
		a := attrs(tag)
		p := placeholder{Type: a["type"], Idx: a["idx"]}
		if p.Type == "" {
			p.Type = "body"
		}
		if m := alignPattern.FindStringSubmatch(sp); m != nil {
			p.Align = m[1]
		}
		if m := xfrmPattern.FindStringSubmatch(sp); m != nil {
			for _, v := range m[1:] {
				n, _ := strconv.ParseInt(v, 10, 64)
				p.Box = append(p.Box, n)
			}
		}
		out = append(out, p)
	}
	return out
}

func findPlaceholder(list []placeholder, types ...string) *placeholder {
	for _, t := range types {
		for i := range list {
			if list[i].Type == t {
				return &list[i]
			}
		}
	}
	return nil
}

// templatePackage is an opened template with its parts in memory.
type templatePackage struct {
	parts         map[string][]byte
	order         []string
	width, height int64
	master        string
	layouts       []string // in master order
	notesMaster   string
	theme         string
}

func openTemplate(file string) (*templatePackage, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxTemplateBytes {
		return nil, errors.New("template_too_large")
	}
	z, err := zip.OpenReader(file)
	if err != nil {
		return nil, errors.New("invalid_office_file")
	}
	defer z.Close()
	p := &templatePackage{parts: map[string][]byte{}}
	var total int64
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > 4*maxTemplateBytes {
			return nil, errors.New("template_too_large")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, 4*maxTemplateBytes))
		r.Close()
		if err != nil {
			return nil, err
		}
		p.parts[f.Name] = b
		p.order = append(p.order, f.Name)
	}
	pres, ok := p.parts["ppt/presentation.xml"]
	if !ok {
		return nil, errors.New("not_a_presentation")
	}
	sz := attrs(sldSzPattern.FindString(string(pres)))
	p.width, _ = strconv.ParseInt(sz["cx"], 10, 64)
	p.height, _ = strconv.ParseInt(sz["cy"], 10, 64)
	if p.width <= 0 || p.height <= 0 {
		return nil, errors.New("not_a_presentation")
	}
	for _, r := range parseRels(p.parts[relsPath("ppt/presentation.xml")]) {
		switch {
		case strings.HasSuffix(r.Type, "/slideMaster") && p.master == "":
			p.master = resolvePart("ppt/presentation.xml", r.Target)
		case strings.HasSuffix(r.Type, "/notesMaster"):
			p.notesMaster = resolvePart("ppt/presentation.xml", r.Target)
		}
	}
	if _, ok := p.parts[p.master]; !ok {
		return nil, errors.New("not_a_presentation")
	}
	masterRels := map[string]string{}
	for _, r := range parseRels(p.parts[relsPath(p.master)]) {
		masterRels[r.ID] = resolvePart(p.master, r.Target)
		if strings.HasSuffix(r.Type, "/theme") {
			p.theme = resolvePart(p.master, r.Target)
		}
	}
	for _, m := range regexp.MustCompile(`<p:sldLayoutId\b[^>]*r:id="([^"]+)"`).FindAllStringSubmatch(string(p.parts[p.master]), -1) {
		if l := masterRels[m[1]]; l != "" {
			if _, ok := p.parts[l]; ok {
				p.layouts = append(p.layouts, l)
			}
		}
	}
	if len(p.layouts) == 0 {
		return nil, errors.New("template_has_no_layouts")
	}
	return p, nil
}

// themeColors reads the colour scheme and fonts of the master's theme.
func (p *templatePackage) themeColors() (map[string]string, string, string) {
	theme := string(p.parts[p.theme])
	colors := map[string]string{}
	for _, slot := range []string{"dk1", "lt1", "dk2", "lt2", "accent1", "accent2"} {
		m := regexp.MustCompile(`(?s)<a:` + slot + `>(.*?)</a:` + slot + `>`).FindStringSubmatch(theme)
		if m == nil {
			continue
		}
		if c := regexp.MustCompile(`(?:val|lastClr)="([0-9A-Fa-f]{6})"`).FindAllStringSubmatch(m[1], -1); c != nil {
			colors[slot] = strings.ToUpper(c[len(c)-1][1])
		}
	}
	font := func(kind string) string {
		m := regexp.MustCompile(`(?s)<a:` + kind + `>.*?<a:latin typeface="([^"]*)"`).FindStringSubmatch(theme)
		if m == nil {
			return ""
		}
		return m[1]
	}
	return colors, font("majorFont"), font("minorFont")
}

// schemeColor maps a scheme colour (tx1, bg1, accent1, …) through the
// master's colour map.
func (p *templatePackage) schemeColor(name string, colors map[string]string) string {
	mapping := attrs(clrMapPattern.FindString(string(p.parts[p.master])))
	if slot, ok := mapping[name]; ok {
		name = slot
	}
	return colors[name]
}

func (p *templatePackage) styleColor(style string, colors map[string]string) string {
	m := regexp.MustCompile(`(?s)<p:` + style + `>.*?<a:lvl1pPr\b.*?<a:defRPr\b[^>]*>(.*?)</a:defRPr>`).FindStringSubmatch(string(p.parts[p.master]))
	if m == nil {
		return ""
	}
	if c := regexp.MustCompile(`<a:srgbClr val="([0-9A-Fa-f]{6})"`).FindStringSubmatch(m[1]); c != nil {
		return strings.ToUpper(c[1])
	}
	if c := regexp.MustCompile(`<a:schemeClr val="(\w+)"`).FindStringSubmatch(m[1]); c != nil {
		return p.schemeColor(c[1], colors)
	}
	return ""
}

// InspectTemplate reads an imported PowerPoint template: which layouts to use,
// where titles and content go, its colours and fonts, and renders the two
// layouts' backgrounds for the editor.
func InspectTemplate(ctx context.Context, s *store.Store, file string) (domain.Template, error) {
	return InspectTemplateLayouts(ctx, s, file, "", "")
}

// InspectTemplateLayouts is InspectTemplate with preferred cover and content
// layouts (part names), used when they were chosen rather than guessed.
func InspectTemplateLayouts(ctx context.Context, s *store.Store, file, preferCover, preferContent string) (domain.Template, error) {
	t := domain.Template{Fixed: []string{}, Issues: []domain.Issue{}, Confirmed: true}
	p, err := openTemplate(file)
	if err != nil {
		return t, err
	}
	t.Width, t.Height = p.width, p.height
	t.Ratio = "16:9"
	if float64(p.width)/float64(p.height) < 1.5 {
		t.Ratio = "4:3"
	}
	colors, major, minor := p.themeColors()
	t.Background = firstNonEmpty(p.schemeColor("bg1", colors), "FFFFFF")
	t.Foreground = firstNonEmpty(p.styleColor("bodyStyle", colors), p.schemeColor("tx1", colors), "1F2D3A")
	t.Accent = firstNonEmpty(colors["accent1"], t.Foreground)
	t.TitleColor = firstNonEmpty(p.styleColor("titleStyle", colors), p.schemeColor("tx1", colors), t.Foreground)
	t.Font, t.HeadingFont = minor, major
	master := placeholders(string(p.parts[p.master]))

	type layoutInfo struct {
		part, kind string
		phs        []placeholder
	}
	infos := []layoutInfo{}
	for _, l := range p.layouts {
		x := string(p.parts[l])
		kind := ""
		if m := layoutTypeAttr.FindStringSubmatch(x); m != nil {
			kind = m[1]
		}
		infos = append(infos, layoutInfo{l, kind, placeholders(x)})
	}
	content := func(l layoutInfo) []placeholder {
		out := []placeholder{}
		for _, ph := range l.phs {
			if ph.Type != "dt" && ph.Type != "ftr" && ph.Type != "sldNum" {
				out = append(out, ph)
			}
		}
		return out
	}
	pick := func(match func(layoutInfo) bool) *layoutInfo {
		for i := range infos {
			if match(infos[i]) {
				return &infos[i]
			}
		}
		return nil
	}
	cover := pick(func(l layoutInfo) bool { return l.kind == "title" })
	if cover == nil {
		cover = pick(func(l layoutInfo) bool { return findPlaceholder(l.phs, "ctrTitle") != nil })
	}
	if cover == nil {
		cover = &infos[0]
	}
	body := pick(func(l layoutInfo) bool { return l.kind == "obj" || l.kind == "tx" })
	slide := pick(func(l layoutInfo) bool { return l.kind == "titleOnly" })
	if slide == nil {
		slide = pick(func(l layoutInfo) bool { c := content(l); return len(c) == 1 && c[0].Type == "title" })
	}
	if slide == nil {
		slide = body
	}
	if slide == nil {
		slide = pick(func(l layoutInfo) bool { return findPlaceholder(l.phs, "title") != nil })
	}
	if slide == nil {
		slide = cover
	}
	if body == nil {
		body = slide
	}
	if l := pick(func(l layoutInfo) bool { return l.part == preferCover }); l != nil {
		cover = l
	}
	if l := pick(func(l layoutInfo) bool { return l.part == preferContent }); l != nil {
		slide = l
		// A chosen content layout with a body placeholder also places content.
		if findPlaceholder(l.phs, "body", "obj") != nil {
			body = l
		}
	}
	t.CoverLayout, t.ContentLayout = cover.part, slide.part

	frac := func(box []int64) []float64 {
		return []float64{float64(box[0]) / float64(p.width), float64(box[1]) / float64(p.height), float64(box[2]) / float64(p.width), float64(box[3]) / float64(p.height)}
	}
	boxOf := func(own []placeholder, types ...string) []float64 {
		if ph := findPlaceholder(own, types...); ph != nil && len(ph.Box) == 4 {
			return frac(ph.Box)
		}
		if ph := findPlaceholder(master, types...); ph != nil && len(ph.Box) == 4 {
			return frac(ph.Box)
		}
		return nil
	}
	t.TitleBox = boxOf(slide.phs, "title", "ctrTitle")
	if t.TitleBox == nil {
		t.TitleBox = []float64{0.06, 0.035, 0.88, 0.12}
	}
	t.BodyBox = boxOf(body.phs, "body", "obj")
	if t.BodyBox == nil || overlaps(t.BodyBox, t.TitleBox) {
		top := t.TitleBox[1] + t.TitleBox[3] + 0.04
		t.BodyBox = []float64{t.TitleBox[0], top, t.TitleBox[2], max(0.9-top, 0.3)}
	}
	t.CoverTitleBox = boxOf(cover.phs, "ctrTitle", "title")
	if t.CoverTitleBox == nil {
		t.CoverTitleBox = []float64{0.08, 0.31, 0.84, 0.32}
	}
	t.CoverSubtitleBox = boxOf(cover.phs, "subTitle")
	if ph := findPlaceholder(cover.phs, "subTitle"); ph != nil {
		t.CoverSubtitleIdx = firstNonEmpty(ph.Idx, "1")
	}
	if t.CoverSubtitleBox == nil {
		top := t.CoverTitleBox[1] + t.CoverTitleBox[3] + 0.02
		t.CoverSubtitleBox = []float64{t.CoverTitleBox[0], min(top, 0.85), t.CoverTitleBox[2], 0.1}
	}
	if ph := findPlaceholder(slide.phs, "sldNum"); ph != nil {
		t.ContentNumberIdx = firstNonEmpty(ph.Idx, "12")
	}
	masterAlign := ""
	if m := regexp.MustCompile(`(?s)<p:titleStyle>.*?<a:lvl1pPr\b[^>]*\balgn="(\w+)"`).FindStringSubmatch(string(p.parts[p.master])); m != nil {
		masterAlign = m[1]
	}
	alignOf := func(own []placeholder, types ...string) string {
		if ph := findPlaceholder(own, types...); ph != nil && ph.Align != "" {
			return boxAlign(ph.Align)
		}
		if ph := findPlaceholder(master, types...); ph != nil && ph.Align != "" {
			return boxAlign(ph.Align)
		}
		return boxAlign(masterAlign)
	}
	t.TitleAlign = alignOf(slide.phs, "title")
	t.CoverAlign = alignOf(cover.phs, "ctrTitle", "title")
	t.CoverPlaceholder = "title"
	if findPlaceholder(cover.phs, "ctrTitle") != nil {
		t.CoverPlaceholder = "ctrTitle"
	}
	if err := renderTemplateBackgrounds(ctx, s, file, &t); err != nil {
		t.Issues = append(t.Issues, domain.Issue{Code: "preview_failed", Detail: err.Error()})
	}
	return t, nil
}

func overlaps(a, b []float64) bool {
	return a[1] < b[1]+b[3]-0.01 && b[1] < a[1]+a[3]-0.01 && a[0] < b[0]+b[2] && b[0] < a[0]+a[2]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// renderTemplateBackgrounds renders the cover and content layouts without any
// slide content, giving the editor the template's look.
func renderTemplateBackgrounds(ctx context.Context, s *store.Store, file string, t *domain.Template) error {
	slides := []templateSlide{
		{layout: t.CoverLayout, shapes: ""},
		{layout: t.ContentLayout, shapes: ""},
	}
	b, err := buildFromTemplate(file, slides, nil)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "eduforge-template-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	deck := filepath.Join(tmp, "layouts.pptx")
	if err = os.WriteFile(deck, b, 0600); err != nil {
		return err
	}
	pdf, err := documents.ConvertPDF(ctx, deck, tmp)
	if err != nil {
		return err
	}
	if _, err = documents.Command(ctx, "pdftoppm", "-scale-to", "1280", "-png", pdf, filepath.Join(tmp, "page")); err != nil {
		return err
	}
	pages, _ := filepath.Glob(filepath.Join(tmp, "page-*.png"))
	sort.Strings(pages)
	if len(pages) < 2 {
		return errors.New("template_render_failed")
	}
	for i, target := range []*string{&t.CoverImage, &t.ContentImage} {
		png, err := os.ReadFile(pages[i])
		if err != nil {
			return err
		}
		if *target, _, err = s.Blob(png, ".png"); err != nil {
			return err
		}
	}
	t.Preview = t.ContentImage
	return nil
}

// templateSlide is one generated slide placed on a template layout.
type templateSlide struct {
	layout string // layout part name
	shapes string
	rels   [][3]string
	media  map[string][]byte // part name → bytes
	notes  string            // notes body paragraphs; empty for none
}

// buildFromTemplate writes a presentation from the template's masters,
// layouts and theme with the given slides in place of its own.
func buildFromTemplate(file string, slides []templateSlide, notes *notesSetup) ([]byte, error) {
	p, err := openTemplate(file)
	if err != nil {
		return nil, err
	}
	drop := func(name string) bool {
		for _, prefix := range []string{"ppt/slides/", "ppt/notesSlides/", "ppt/comments/", "docProps/thumbnail"} {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}
	out := map[string][]byte{}
	for name, b := range p.parts {
		if !drop(name) {
			out[name] = b
		}
	}
	notesMaster := p.notesMaster
	if _, ok := p.parts[notesMaster]; !ok {
		notesMaster = ""
	}
	presRels := []ooxmlRel{}
	for _, r := range parseRels(p.parts[relsPath("ppt/presentation.xml")]) {
		if strings.HasSuffix(r.Type, "/slide") {
			continue
		}
		presRels = append(presRels, r)
	}
	var ids strings.Builder
	var overrides strings.Builder
	for _, sl := range slides {
		if _, ok := p.parts[sl.layout]; !ok {
			return nil, errors.New("template_layout_missing")
		}
	}
	for i, sl := range slides {
		n := i + 1
		name := fmt.Sprintf("ppt/slides/slide%d.xml", n)
		rid := fmt.Sprintf("rIdP2LSlide%d", n)
		presRels = append(presRels, ooxmlRel{ID: rid, Type: nsR + "/slide", Target: relTarget("ppt/presentation.xml", name)})
		fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="%s"/>`, 255+n, rid)
		slideRels := [][3]string{{"rId1", "slideLayout", relTarget(name, sl.layout)}}
		slideRels = append(slideRels, sl.rels...)
		if notes != nil && notesMaster != "" {
			notesName := fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n)
			slideRels = append(slideRels, [3]string{"rId2", "notesSlide", relTarget(name, notesName)})
			out[notesName] = []byte(xmlHead + fmt.Sprintf(`<p:notes xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:cSld><p:spTree>%s%s</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`, nsA, nsR, nsP, group, notesPlaceholders(notes.img, notes.body, sl.notes)))
			out[relsPath(notesName)] = []byte(rels([3]string{"rId1", "notesMaster", relTarget(notesName, notesMaster)}, [3]string{"rId2", "slide", relTarget(notesName, name)}))
			fmt.Fprintf(&overrides, `<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"/>`, notesName)
		}
		out[name] = []byte(xmlHead + fmt.Sprintf(`<p:sld xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:cSld><p:spTree>%s%s</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`, nsA, nsR, nsP, group, sl.shapes))
		out[relsPath(name)] = []byte(rels(slideRels...))
		for media, b := range sl.media {
			out[media] = b
		}
		fmt.Fprintf(&overrides, `<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, name)
	}
	pres := string(p.parts["ppt/presentation.xml"])
	pres = sldIDListBlock.ReplaceAllString(pres, "")
	pres = custShowBlock.ReplaceAllString(pres, "")
	// Sections list the template's slide IDs; they no longer exist.
	pres = extBlock.ReplaceAllStringFunc(pres, func(ext string) string {
		if strings.Contains(ext, "sectionLst") {
			return ""
		}
		return ext
	})
	pres = strings.Replace(pres, "<p:sldSz", "<p:sldIdLst>"+ids.String()+"</p:sldIdLst><p:sldSz", 1)
	out["ppt/presentation.xml"] = []byte(pres)
	var relsXML strings.Builder
	relsXML.WriteString(xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range presRels {
		fmt.Fprintf(&relsXML, `<Relationship Id="%s" Type="%s" Target="%s"/>`, x(r.ID), x(r.Type), x(r.Target))
	}
	relsXML.WriteString("</Relationships>")
	out[relsPath("ppt/presentation.xml")] = []byte(relsXML.String())

	types := string(p.parts["[Content_Types].xml"])
	types = strings.ReplaceAll(types, "presentationml.template.main+xml", "presentationml.presentation.main+xml")
	types = strings.ReplaceAll(types, "presentationml.slideshow.main+xml", "presentationml.presentation.main+xml")
	for ext, typ := range map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg"} {
		if !regexp.MustCompile(`(?i)Extension="` + ext + `"`).MatchString(types) {
			types = strings.Replace(types, "<Override", fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/><Override`, ext, typ), 1)
		}
	}
	types = strings.Replace(types, "</Types>", overrides.String()+"</Types>", 1)
	out["[Content_Types].xml"] = []byte(types)

	// Drop media only the template's sample slides used.
	referenced := map[string]bool{}
	for name, b := range out {
		if strings.HasSuffix(name, ".rels") {
			owner := strings.Replace(strings.TrimSuffix(name, ".rels"), "/_rels/", "/", 1)
			if name == "_rels/.rels" {
				owner = ""
			}
			for _, r := range parseRels(b) {
				referenced[resolvePart(owner, r.Target)] = true
			}
		}
	}
	for name := range out {
		if strings.HasPrefix(name, "ppt/media/") && !referenced[name] {
			delete(out, name)
		}
	}
	// Relationships and content types must not name parts that were dropped.
	for name, b := range out {
		if !strings.HasSuffix(name, ".rels") {
			continue
		}
		owner := strings.Replace(strings.TrimSuffix(name, ".rels"), "/_rels/", "/", 1)
		if name == "_rels/.rels" {
			owner = ""
		}
		out[name] = []byte(relPattern.ReplaceAllStringFunc(string(b), func(tag string) string {
			a := attrs(tag)
			if a["TargetMode"] == "External" {
				return tag
			}
			if _, ok := out[resolvePart(owner, a["Target"])]; !ok {
				return ""
			}
			return tag
		}))
	}
	out["[Content_Types].xml"] = []byte(regexp.MustCompile(`<Override\b[^>]*/>`).ReplaceAllStringFunc(string(out["[Content_Types].xml"]), func(tag string) string {
		if _, ok := out[strings.TrimPrefix(attrs(tag)["PartName"], "/")]; !ok {
			return ""
		}
		return tag
	}))

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	names := make([]string, 0, len(out))
	for name := range out {
		if name != "[Content_Types].xml" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range append([]string{"[Content_Types].xml"}, names...) {
		w, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write(out[name]); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

type notesSetup struct{ img, body [4]int }

// mediaMap collects a slide's media files for buildFromTemplate.
type mediaMap map[string]*bytes.Buffer

func (m mediaMap) Create(name string) (io.Writer, error) {
	b := &bytes.Buffer{}
	m[name] = b
	return b, nil
}

// templatePPTX writes the deck on an imported template's layouts.
func templatePPTX(s *store.Store, a domain.Artifact, k domain.Knowledge, t theme, style domain.Style) ([]byte, error) {
	file, err := s.Path(t.file)
	if err != nil {
		return nil, errors.New("template_missing")
	}
	if t.width <= 0 || t.height <= 0 || t.coverLayout == "" || t.contentLayout == "" {
		return nil, errors.New("template_missing")
	}
	geo := GeometryOf(style)
	figures := SectionFigures(a, k)
	img, body := notesGeometry(int(t.width), int(t.height))
	slides := []templateSlide{}
	for i, sec := range a.Sections {
		if len(sec.Layout) == 0 {
			sec.Layout = DefaultLayout(sec, figures[i], geo)
		}
		media := mediaMap{}
		shapes, extra, err := layoutShapes(media, s, sec, i+1, int(t.width), int(t.height), t, style)
		if err != nil {
			return nil, err
		}
		layout := t.contentLayout
		if slideKind(sec) == "display" {
			layout = t.coverLayout
		}
		files := map[string][]byte{}
		for name, b := range media {
			files[name] = b.Bytes()
		}
		slides = append(slides, templateSlide{layout: layout, shapes: shapes, rels: extra, media: files, notes: notesParagraphs(sec.Notes, t.font)})
	}
	return buildFromTemplate(file, slides, &notesSetup{img, body})
}
