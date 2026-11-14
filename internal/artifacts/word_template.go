package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
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

// Imported Word templates (.docx/.dotx) for readers. The template's styles,
// theme, headers, footers and page setup are kept; its body is replaced by
// the reader, written with the template's own Title, Heading 1/2, Caption and
// list styles (added when the template lacks them).

const nsW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

type wordPackage struct {
	parts map[string][]byte
	main  string // main document part, e.g. word/document.xml
}

func openWordTemplate(file string) (*wordPackage, error) {
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
	p := &wordPackage{parts: map[string][]byte{}}
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
	}
	for _, r := range parseRels(p.parts["_rels/.rels"]) {
		if strings.HasSuffix(r.Type, "/officeDocument") {
			p.main = resolvePart("", r.Target)
		}
	}
	if _, ok := p.parts[p.main]; !ok || !strings.Contains(string(p.parts[p.main]), "<w:body") {
		return nil, errors.New("not_a_word_document")
	}
	return p, nil
}

func (p *wordPackage) related(kind string) string {
	for _, r := range parseRels(p.parts[relsPath(p.main)]) {
		if strings.HasSuffix(r.Type, "/"+kind) {
			return resolvePart(p.main, r.Target)
		}
	}
	return ""
}

var (
	wordStylePattern = regexp.MustCompile(`(?s)<w:style\b[^>]*>.*?</w:style>`)
	styleNamePattern = regexp.MustCompile(`<w:name w:val="([^"]+)"`)
	colorPattern     = regexp.MustCompile(`<w:color w:val="([0-9A-Fa-f]{6})"`)
)

// Word's built-in style names, the same in every language's Word.
var wordRoles = map[string][]string{
	"normal": {"normal"}, "title": {"title"}, "subtitle": {"subtitle"},
	"h1": {"heading 1"}, "h2": {"heading 2"}, "caption": {"caption"}, "list": {"list bullet", "list paragraph"},
}

type wordStyle struct{ id, xml string }

// wordStyles finds the template's paragraph style for each role.
func (p *wordPackage) wordStyles() map[string]wordStyle {
	byName := map[string]wordStyle{}
	for _, def := range wordStylePattern.FindAllString(string(p.parts["word/styles.xml"]), -1) {
		head := def[:strings.Index(def, ">")+1]
		a := attrs(head)
		if a["type"] != "paragraph" {
			continue
		}
		if m := styleNamePattern.FindStringSubmatch(def); m != nil {
			byName[strings.ToLower(m[1])] = wordStyle{a["styleId"], def}
		}
	}
	out := map[string]wordStyle{}
	for role, names := range wordRoles {
		for _, name := range names {
			if st, ok := byName[name]; ok {
				out[role] = st
				break
			}
		}
	}
	return out
}

// InspectWordTemplate reads an imported Word template's look and renders a
// sample page showing how a reader will appear in it.
func InspectWordTemplate(ctx context.Context, s *store.Store, file string) (domain.Template, error) {
	t := domain.Template{Kind: "docx", Fixed: []string{}, Issues: []domain.Issue{}, Confirmed: true}
	p, err := openWordTemplate(file)
	if err != nil {
		return t, err
	}
	styles := p.wordStyles()
	theme := string(p.parts[p.related("theme")])
	font := func(kind string) string {
		m := regexp.MustCompile(`(?s)<a:` + kind + `>.*?<a:latin typeface="([^"]*)"`).FindStringSubmatch(theme)
		if m == nil {
			return ""
		}
		return m[1]
	}
	t.Font, t.HeadingFont = font("minorFont"), font("majorFont")
	if m := regexp.MustCompile(`(?s)<w:docDefaults>.*?<w:rFonts\b[^>]*w:ascii="([^"]+)"`).FindStringSubmatch(string(p.parts["word/styles.xml"])); m != nil {
		t.Font = m[1]
	}
	if m := regexp.MustCompile(`<w:rFonts\b[^>]*w:ascii="([^"]+)"`).FindStringSubmatch(styles["h1"].xml); m != nil {
		t.HeadingFont = m[1]
	}
	colorOf := func(role string) string {
		if m := colorPattern.FindStringSubmatch(styles[role].xml); m != nil {
			return strings.ToUpper(m[1])
		}
		return ""
	}
	t.TitleColor = firstNonEmpty(colorOf("h1"), colorOf("title"))
	t.Accent = firstNonEmpty(colorOf("title"), t.TitleColor)
	t.Foreground = firstNonEmpty(colorOf("normal"), "000000")
	t.Background = "FFFFFF"
	if err := renderWordPreview(ctx, s, file, &t); err != nil {
		t.Issues = append(t.Issues, domain.Issue{Code: "preview_failed", Detail: err.Error()})
	}
	return t, nil
}

func renderWordPreview(ctx context.Context, s *store.Store, file string, t *domain.Template) error {
	blocks := []wordBlock{
		{Kind: "subtitle", Text: "論文導讀 · Lesson reading guide"},
		{Kind: "title", Text: "論文題目 Lesson title"},
		{Kind: "h1", Text: "I.  研究動機與問題"},
		{Kind: "para", Text: "這裡是讀本內文，會套用模板的內文樣式、字型與段落設定。The reader's text uses the template's body style."},
		{Kind: "bullet", Text: "重點一：模板的標題與內文樣式"},
		{Kind: "bullet", Text: "重點二：保留頁首、頁尾與頁面設定"},
		{Kind: "h2", Text: "小標題 Subheading"},
		{Kind: "para", Text: "論文原圖會置中插入，下方加上原圖說。"},
		{Kind: "h1", Text: "II.  方法"},
		{Kind: "para", Text: "……"},
	}
	b, err := buildWordDocument(file, blocks)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "eduforge-word-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	doc := filepath.Join(tmp, "sample.docx")
	if err = os.WriteFile(doc, b, 0600); err != nil {
		return err
	}
	pdf, err := documents.ConvertPDF(ctx, doc, tmp)
	if err != nil {
		return err
	}
	if _, err = documents.Command(ctx, "pdftoppm", "-f", "1", "-l", "1", "-singlefile", "-scale-to", "900", "-png", pdf, filepath.Join(tmp, "page")); err != nil {
		return err
	}
	png, err := os.ReadFile(filepath.Join(tmp, "page.png"))
	if err != nil {
		return err
	}
	t.Preview, _, err = s.Blob(png, ".png")
	return err
}

// wordBlock is one paragraph of a generated Word document.
type wordBlock struct {
	Kind  string // title, subtitle, h1, h2, para, bullet, figure, caption, formula (Text is TeX)
	Text  string // may contain **bold** spans for para and bullet
	Image []byte
	Ext   string
}

// readerBlocks lays a reader out as Word paragraphs.
func readerBlocks(s *store.Store, a domain.Artifact, k domain.Knowledge) []wordBlock {
	blocks := []wordBlock{{Kind: "subtitle", Text: documentKicker(a)}, {Kind: "title", Text: DocumentTitle(k)}}
	figures := SectionFigures(a, k)
	for i, sec := range a.Sections {
		heading := PlainText(sec.Title)
		if a.Type != "B" {
			heading = Roman(i+1) + ".  " + SectionTitle(sec.Title)
		}
		blocks = append(blocks, wordBlock{Kind: "h1", Text: heading})
		// Each figure goes right under the paragraph about it (PlaceFigures).
		body := domain.StripCitationMarkers(sec.Body)
		placed := PlaceFigures(body, figures[i])
		for j, line := range strings.Split(body, "\n") {
			l := parseMarkupLine(line)
			tex, display := displayFormula(line)
			switch {
			case display:
				blocks = append(blocks, wordBlock{Kind: "formula", Text: tex})
			case strings.TrimSpace(l.text) == "":
			case l.heading:
				blocks = append(blocks, wordBlock{Kind: "h2", Text: l.text})
			case l.bullet != "":
				blocks = append(blocks, wordBlock{Kind: "bullet", Text: l.text})
			default:
				blocks = append(blocks, wordBlock{Kind: "para", Text: l.text})
			}
			for _, fig := range placed[j] {
				blocks = append(blocks, figureBlocks(s, fig)...)
			}
		}
	}
	if names := LessonNames(k); len(names) > 0 {
		blocks = append(blocks, wordBlock{Kind: "h1", Text: referencesTitle(a.Language)})
		for i, name := range names {
			blocks = append(blocks, wordBlock{Kind: "para", Text: fmt.Sprintf("[%d] %s", i+1, name)})
		}
	}
	return blocks
}

// figureBlocks are a figure and its caption as Word paragraphs (none when the
// image cannot be read or is not PNG/JPEG).
func figureBlocks(s *store.Store, fig domain.Image) []wordBlock {
	file, err := s.Path(fig.Path)
	if err != nil {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(file))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	out := []wordBlock{{Kind: "figure", Image: b, Ext: ext}}
	if caption := figureCaption(fig); caption != "" {
		out = append(out, wordBlock{Kind: "caption", Text: caption})
	}
	return out
}

// WordDocument writes a reader as a Word document on its imported template.
func WordDocument(s *store.Store, a domain.Artifact, k domain.Knowledge) ([]byte, error) {
	file, err := s.Path(str(a.Style.Resolved[a.Type], "word_template"))
	if err != nil {
		return nil, errors.New("template_missing")
	}
	return buildWordDocument(file, readerBlocks(s, a, k))
}

// HasWordTemplate reports whether an artifact is written on a Word template.
func HasWordTemplate(a domain.Artifact) bool {
	return str(a.Style.Resolved[a.Type], "word_template") != ""
}

var (
	sectPrPattern   = regexp.MustCompile(`(?s)<w:sectPr\b.*?</w:sectPr>`)
	twipAttr        = func(tag, name string) int { v, _ := strconv.Atoi(attrs(tag)[name]); return v }
	pgSzPattern     = regexp.MustCompile(`<w:pgSz\b[^>]*>`)
	pgMarPattern    = regexp.MustCompile(`<w:pgMar\b[^>]*>`)
	abstractIDs     = regexp.MustCompile(`w:abstractNumId="(\d+)"`)
	numIDs          = regexp.MustCompile(`<w:num\b[^>]*w:numId="(\d+)"`)
	firstNumPattern = regexp.MustCompile(`<w:num\b[^>]*w:numId=`)
)

func buildWordDocument(file string, blocks []wordBlock) ([]byte, error) {
	p, err := openWordTemplate(file)
	if err != nil {
		return nil, err
	}
	doc := string(p.parts[p.main])
	bodyStart := strings.Index(doc, "<w:body>")
	bodyEnd := strings.LastIndex(doc, "</w:body>")
	if bodyStart < 0 || bodyEnd < bodyStart {
		return nil, errors.New("not_a_word_document")
	}
	// The body's own section properties (last child of the body) keep the
	// template's page size, margins, headers and footers.
	sect := ""
	if all := sectPrPattern.FindAllStringIndex(doc[bodyStart:bodyEnd], -1); len(all) > 0 {
		last := all[len(all)-1]
		sect = doc[bodyStart+last[0] : bodyStart+last[1]]
	}
	root := doc[:bodyStart]
	for prefix, uri := range map[string]string{"r": nsR, "wp": "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"} {
		if !strings.Contains(root, "xmlns:"+prefix+"=") {
			root = strings.Replace(root, "<w:document ", `<w:document xmlns:`+prefix+`="`+uri+`" `, 1)
		}
	}
	textWidth, pageHeight := 5943600, 9144000 // EMU: 6.5in text width, 10in page
	if sz := pgSzPattern.FindString(sect); sz != "" {
		mar := pgMarPattern.FindString(sect)
		// attrs keys drop the w: prefix.
		if w := twipAttr(sz, "w") - twipAttr(mar, "left") - twipAttr(mar, "right"); w > 1440 {
			textWidth = w * 635
		}
		if h := twipAttr(sz, "h"); h > 1440 {
			pageHeight = h * 635
		}
	}

	styles := p.wordStyles()
	stylesXML := string(p.parts["word/styles.xml"])
	normal := styles["normal"].id
	fallback := map[string]string{
		"title":    `<w:pPr><w:spacing w:after="240"/></w:pPr><w:rPr><w:b/><w:sz w:val="44"/><w:szCs w:val="44"/></w:rPr>`,
		"subtitle": `<w:pPr><w:spacing w:after="60"/></w:pPr><w:rPr><w:color w:val="666666"/><w:sz w:val="22"/></w:rPr>`,
		"h1":       `<w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/><w:szCs w:val="30"/></w:rPr>`,
		"h2":       `<w:pPr><w:keepNext/><w:spacing w:before="200" w:after="60"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/></w:rPr>`,
		"caption":  `<w:pPr><w:spacing w:after="200"/></w:pPr><w:rPr><w:i/><w:color w:val="595959"/><w:sz w:val="18"/></w:rPr>`,
		"list":     `<w:pPr><w:ind w:left="720"/></w:pPr>`,
	}
	var added strings.Builder
	for _, role := range []string{"title", "subtitle", "h1", "h2", "caption", "list"} {
		if styles[role].id != "" {
			continue
		}
		id := "P2L" + strings.ToUpper(role[:1]) + role[1:]
		based := ""
		if normal != "" {
			based = `<w:basedOn w:val="` + normal + `"/><w:next w:val="` + normal + `"/>`
		}
		fmt.Fprintf(&added, `<w:style w:type="paragraph" w:customStyle="1" w:styleId="%s"><w:name w:val="EduForge %s"/>%s<w:qFormat/>%s</w:style>`, id, role, based, fallback[role])
		styles[role] = wordStyle{id: id}
	}
	if added.Len() > 0 {
		if !strings.Contains(stylesXML, "</w:styles>") {
			return nil, errors.New("not_a_word_document")
		}
		stylesXML = strings.Replace(stylesXML, "</w:styles>", added.String()+"</w:styles>", 1)
	}
	p.parts["word/styles.xml"] = []byte(stylesXML)

	// Bullets: the template's List Bullet style when it carries numbering,
	// otherwise a bullet list definition of our own.
	bulletNum := ""
	if !strings.Contains(styles["list"].xml, "<w:numPr>") {
		bulletNum, err = p.addBulletNumbering()
		if err != nil {
			return nil, err
		}
	}

	docRels := relsPath(p.main)
	relsXML := string(p.parts[docRels])
	if relsXML == "" {
		relsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
	}
	var body strings.Builder
	pStyle := func(role string) string {
		if id := styles[role].id; id != "" {
			return `<w:pStyle w:val="` + x(id) + `"/>`
		}
		return ""
	}
	figures := 0
	addMedia := func(b []byte, ext string) string {
		figures++
		media := fmt.Sprintf("media/p2l-figure%d%s", figures, ext)
		rid := fmt.Sprintf("rIdP2LFigure%d", figures)
		p.parts[path.Join(path.Dir(p.main), media)] = b
		relsXML = strings.Replace(relsXML, "</Relationships>", fmt.Sprintf(`<Relationship Id="%s" Type="%s/image" Target="%s"/></Relationships>`, rid, nsR, media), 1)
		return rid
	}
	drawing := func(rid string, w, h int, name string) string {
		return fmt.Sprintf(`<w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="%d" cy="%d"/><wp:docPr id="%d" name="%s %d"/><wp:cNvGraphicFramePr><a:graphicFrameLocks xmlns:a="%s" noChangeAspect="1"/></wp:cNvGraphicFramePr><a:graphic xmlns:a="%s"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="%d" name="%s%d"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="%s"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing>`,
			w, h, 4000+figures, name, figures, nsA, nsA, 4000+figures, strings.ToLower(name), figures, rid, w, h)
	}
	// formula is a formula as a picture run sized for 11 pt text and lowered
	// to sit on the baseline, or its TeX source when it cannot be rendered.
	formula := func(tex string, display bool) string {
		b, r, ok := formulaPNG(tex, display, "000000", 20)
		if !ok {
			src := "$" + tex + "$"
			if display {
				src = "$" + src + "$"
			}
			return `<w:r><w:t xml:space="preserve">` + x(src) + `</w:t></w:r>`
		}
		const ex = 0.442 * 11 * 12700 // EMU
		w, h := int(r.Width*ex), int(r.Height*ex)
		if w > textWidth {
			w, h = textWidth, h*textWidth/max(1, w)
		}
		lower := int(r.Depth * 0.442 * 11 * 2) // half-points
		return fmt.Sprintf(`<w:r><w:rPr><w:position w:val="-%d"/></w:rPr>%s</w:r>`, lower, drawing(addMedia(b, ".png"), w, h, "Formula"))
	}
	runs := func(text string) string {
		var b strings.Builder
		for _, piece := range splitMath(text) {
			if piece.isMath() {
				b.WriteString(formula(piece.TeX, false))
				continue
			}
			for _, seg := range inlineSegments(piece.Text) {
				bold := ""
				if seg.bold {
					bold = `<w:rPr><w:b/><w:bCs/></w:rPr>`
				}
				fmt.Fprintf(&b, `<w:r>%s<w:t xml:space="preserve">%s</w:t></w:r>`, bold, x(seg.text))
			}
		}
		return b.String()
	}
	for _, bl := range blocks {
		switch bl.Kind {
		case "title", "subtitle", "h1", "h2", "caption":
			// Readers number their sections (I., II.); a template's own
			// heading numbering is switched off so numbers do not double.
			off := ""
			if bl.Kind == "h1" || bl.Kind == "h2" {
				off = `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="0"/></w:numPr>`
			}
			fmt.Fprintf(&body, `<w:p><w:pPr>%s%s</w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, pStyle(bl.Kind), off, x(PlainText(bl.Text)))
		case "bullet":
			num := ""
			if bulletNum != "" {
				num = `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="` + bulletNum + `"/></w:numPr>`
			}
			fmt.Fprintf(&body, `<w:p><w:pPr>%s%s</w:pPr>%s</w:p>`, pStyle("list"), num, runs(bl.Text))
		case "para":
			fmt.Fprintf(&body, `<w:p>%s</w:p>`, runs(bl.Text))
		case "formula":
			fmt.Fprintf(&body, `<w:p><w:pPr><w:spacing w:before="120" w:after="120" w:line="240" w:lineRule="auto"/><w:ind w:left="0" w:firstLine="0"/><w:jc w:val="center"/></w:pPr>%s</w:p>`, formula(bl.Text, true))
		case "figure":
			// Auto line spacing: templates with exact line heights would clip the picture.
			rid := addMedia(bl.Image, bl.Ext)
			w, h := figureSize(bl.Image, textWidth, pageHeight*45/100)
			fmt.Fprintf(&body, `<w:p><w:pPr><w:keepNext/><w:spacing w:before="120" w:after="60" w:line="240" w:lineRule="auto"/><w:ind w:left="0" w:firstLine="0"/><w:jc w:val="center"/></w:pPr><w:r>%s</w:r></w:p>`, drawing(rid, w, h, "Figure"))
		}
	}
	body.WriteString(sect)
	p.parts[p.main] = []byte(root + "<w:body>" + body.String() + "</w:body></w:document>")
	p.parts[docRels] = []byte(relsXML)

	types := string(p.parts["[Content_Types].xml"])
	types = strings.NewReplacer("wordprocessingml.template.main+xml", "wordprocessingml.document.main+xml", "ms-word.template.macroEnabledTemplate.main+xml", "wordprocessingml.document.main+xml").Replace(types)
	for ext, typ := range map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg"} {
		if !regexp.MustCompile(`(?i)Extension="` + ext + `"`).MatchString(types) {
			types = strings.Replace(types, "<Override", fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/><Override`, ext, typ), 1)
		}
	}
	if _, ok := p.parts["word/numbering.xml"]; ok && !strings.Contains(types, `PartName="/word/numbering.xml"`) {
		types = strings.Replace(types, "</Types>", `<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/></Types>`, 1)
	}
	p.parts["[Content_Types].xml"] = []byte(types)

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	names := make([]string, 0, len(p.parts))
	for name := range p.parts {
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
		if _, err = w.Write(p.parts[name]); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// addBulletNumbering adds a bullet list definition and returns its numId.
func (p *wordPackage) addBulletNumbering() (string, error) {
	const name = "word/numbering.xml"
	numbering := string(p.parts[name])
	if numbering == "" {
		numbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="` + nsW + `"></w:numbering>`
		docRels := relsPath(p.main)
		rels := string(p.parts[docRels])
		if rels == "" {
			rels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
		}
		p.parts[docRels] = []byte(strings.Replace(rels, "</Relationships>", `<Relationship Id="rIdP2LNumbering" Type="`+nsR+`/numbering" Target="`+relTarget(p.main, name)+`"/></Relationships>`, 1))
	}
	maxOf := func(re *regexp.Regexp) int {
		n := 0
		for _, m := range re.FindAllStringSubmatch(numbering, -1) {
			if v, _ := strconv.Atoi(m[1]); v > n {
				n = v
			}
		}
		return n
	}
	abstract, num := maxOf(abstractIDs)+1, maxOf(numIDs)+1
	def := fmt.Sprintf(`<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="hybridMultilevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="360" w:hanging="360"/></w:pPr><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial" w:hint="default"/></w:rPr></w:lvl></w:abstractNum>`, abstract)
	if loc := firstNumPattern.FindStringIndex(numbering); loc != nil {
		numbering = numbering[:loc[0]] + def + numbering[loc[0]:]
	} else if i := strings.Index(numbering, "<w:numIdMacAtCleanup"); i >= 0 {
		numbering = numbering[:i] + def + numbering[i:]
	} else {
		numbering = strings.Replace(numbering, "</w:numbering>", def+"</w:numbering>", 1)
	}
	inst := fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="%d"/></w:num>`, num, abstract)
	if i := strings.Index(numbering, "<w:numIdMacAtCleanup"); i >= 0 {
		numbering = numbering[:i] + inst + numbering[i:]
	} else {
		numbering = strings.Replace(numbering, "</w:numbering>", inst+"</w:numbering>", 1)
	}
	p.parts[name] = []byte(numbering)
	return strconv.Itoa(num), nil
}

// figureSize fills the text width with an image, within a maximum height (EMU).
func figureSize(b []byte, maxW, maxH int) (int, int) {
	config, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || config.Width == 0 || config.Height == 0 {
		return maxW, maxW * 3 / 4
	}
	w := maxW
	h := w * config.Height / config.Width
	if h > maxH {
		w, h = w*maxH/h, maxH
	}
	return w, h
}
