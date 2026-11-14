// Package artifacts exports portable HTML/PDF and editable OOXML presentations.
package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"eduforge/internal/documents"
	"eduforge/internal/domain"
	"eduforge/internal/store"
)

func color(style domain.Style, key, fallback string) string {
	v, _ := style[key].(string)
	if regexp.MustCompile(`^[0-9A-Fa-f]{6}$`).MatchString(v) {
		return v
	}
	return fallback
}
func label(u domain.Unit, language string) string {
	name := u.SourceName
	if name == "" {
		name = u.SourceID[:min(8, len(u.SourceID))]
	}
	kind := u.Kind
	if language == "zh-TW" {
		kind = map[string]string{"page": "頁", "slide": "投影片"}[u.Kind]
	}
	return fmt.Sprintf("%s · %s %d", name, kind, u.Locator)
}
func inlineImage(s *store.Store, path string) string {
	file, err := s.Path(path)
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	typ := mime.TypeByExtension(filepath.Ext(file))
	if typ != "image/png" && typ != "image/jpeg" {
		return ""
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b)
}
func HTML(s *store.Store, a domain.Artifact, k domain.Knowledge) ([]byte, error) {
	if a.Type == "B" {
		return deckHTML(s, a, k), nil
	}
	style := a.Style.Resolved[a.Type]
	accent := color(style, "accent", "087F73")
	var out strings.Builder
	fmt.Fprintf(&out, `<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>@page{size:A4;margin:18mm}*{box-sizing:border-box}body{max-width:860px;margin:40px auto;padding:0 24px;color:#19332f;font:17px/1.9 'Noto Sans CJK TC',sans-serif}header{border-bottom:3px solid #%s;padding-bottom:20px;margin-bottom:8px}.kicker{margin:0;color:#%s;font-weight:700;letter-spacing:.08em;font-size:14px}h1{font-size:30px;line-height:1.35;margin:8px 0 0}h2{font-size:24px;margin:64px 0 20px;padding-bottom:8px;border-bottom:1px solid #d9e2dc;color:#%s}h2 .num{display:inline-block;min-width:2.2em;font-family:'Noto Serif CJK TC',serif}section{break-inside:auto}.body{white-space:pre-wrap;overflow-wrap:anywhere}.body strong.sub{display:inline-block;margin-top:18px;font-size:19px;color:#%s}figure{margin:36px auto;break-inside:avoid;text-align:center;max-width:780px}img{max-width:100%%;max-height:480px;object-fit:contain}figcaption{font-size:14px;line-height:1.6;color:#526e67;text-align:left;margin:10px 24px 0}.math svg{display:inline-block;overflow:visible}.math-display{display:block;text-align:center;margin:14px 0;overflow-x:auto}.math-display .math svg{max-width:100%%}code.tex{font-size:.9em;color:#7a4b00;background:#fff6e0;padding:0 3px;border-radius:3px}table{border-collapse:collapse;width:100%%}td,th{border-bottom:1px solid #d9e2dc;padding:8px;text-align:left;vertical-align:top}a{color:#%s}pre{white-space:pre-wrap}svg{max-width:100%%}.references li{margin:4px 0}@media print{body{margin:0;padding:0}a{text-decoration:none}}</style></head><body><header><p class="kicker">%s</p><h1>%s</h1></header>`, a.Language, html.EscapeString(DocumentTitle(k)), accent, accent, accent, accent, accent, html.EscapeString(documentKicker(a)), html.EscapeString(DocumentTitle(k)))
	figures := SectionFigures(a, k)
	bodies := []string{}
	for _, sec := range a.Sections {
		bodies = append(bodies, sec.Body)
	}
	math := renderFormulas(bodies...)
	for si, sec := range a.Sections {
		heading := html.EscapeString(PlainText(sec.Title))
		if a.Type != "B" {
			heading = fmt.Sprintf(`<span class="num">%s.</span>%s`, Roman(si+1), html.EscapeString(SectionTitle(sec.Title)))
		}
		fmt.Fprintf(&out, `<section><h2>%s</h2>`, heading)
		// Text and figures alternate: each figure follows the paragraph about it.
		body := domain.StripCitationMarkers(sec.Body)
		placed := PlaceFigures(body, figures[si])
		lines := strings.Split(body, "\n")
		start := 0
		for j := range lines {
			if len(placed[j]) == 0 && j < len(lines)-1 {
				continue
			}
			if chunk := strings.Trim(strings.Join(lines[start:j+1], "\n"), "\n"); chunk != "" {
				fmt.Fprintf(&out, `<div class="body">%s</div>`, bodyHTMLWith(chunk, math))
			}
			start = j + 1
			for _, img := range placed[j] {
				data := inlineImage(s, img.Path)
				if data == "" {
					continue
				}
				caption := figureCaption(img)
				if caption == "" {
					fmt.Fprintf(&out, `<figure><img src="%s" alt=""></figure>`, data)
					continue
				}
				fmt.Fprintf(&out, `<figure><img src="%s" alt="%s"><figcaption>%s</figcaption></figure>`, data, html.EscapeString(caption), html.EscapeString(caption))
			}
		}
		out.WriteString("</section>")
	}
	if names := PaperNames(k); len(names) > 0 {
		fmt.Fprintf(&out, `<section class="references"><h2>%s</h2><ol>`, referencesTitle(a.Language))
		for _, name := range names {
			fmt.Fprintf(&out, "<li>%s</li>", html.EscapeString(name))
		}
		out.WriteString("</ol></section>")
	}
	out.WriteString("</body></html>")
	return []byte(out.String()), nil
}

// documentKicker labels the kind of document above its title.
func documentKicker(a domain.Artifact) string {
	zh := a.Language == "zh-TW"
	switch a.Type {
	case "A":
		if zh {
			return "論文導讀"
		}
		return "Paper reading guide"
	case "B":
		if zh {
			return "簡報講稿"
		}
		return "Slides and speaker notes"
	}
	if zh {
		return "實驗復現規劃書"
	}
	return "Experiment reproduction plan"
}

func PDF(ctx context.Context, s *store.Store, a domain.Artifact, k domain.Knowledge) ([]byte, error) {
	return exportPDF(ctx, s, a, k, "")
}

// DOCX is a reader as an editable Word document: on its Word template when it
// has one, otherwise converted from the default reader layout.
func DOCX(ctx context.Context, s *store.Store, a domain.Artifact, k domain.Knowledge) ([]byte, error) {
	if a.Type == "B" {
		return nil, fmt.Errorf("reader_required")
	}
	if HasWordTemplate(a) {
		return WordDocument(s, a, k)
	}
	tmp, err := os.MkdirTemp("", "eduforge-export-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	file := filepath.Join(tmp, "reader.fodt")
	if err = os.WriteFile(file, readerDocument(s, a, k), 0600); err != nil {
		return nil, err
	}
	out, err := documents.ConvertTo(ctx, file, tmp, "docx")
	if err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

// NotesPDF prints a presentation's notes pages: each slide with its script.
func NotesPDF(ctx context.Context, s *store.Store, a domain.Artifact, k domain.Knowledge) ([]byte, error) {
	if a.Type != "B" {
		return nil, fmt.Errorf("presentation_required")
	}
	return exportPDF(ctx, s, a, k, notesPDFFilter)
}

const notesPDFFilter = `impress_pdf_Export:{"ExportNotesPages":{"type":"boolean","value":"true"},"ExportOnlyNotesPages":{"type":"boolean","value":"true"}}`

func exportPDF(ctx context.Context, s *store.Store, a domain.Artifact, k domain.Knowledge, filter string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "eduforge-export-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	// Slides convert from the deck itself so the PDF matches what was edited;
	// readers on a Word template convert from that Word document.
	file := filepath.Join(tmp, "reader.fodt")
	if a.Type != "B" && HasWordTemplate(a) {
		doc, err := WordDocument(s, a, k)
		if err != nil {
			return nil, err
		}
		file = filepath.Join(tmp, "reader.docx")
		if err = os.WriteFile(file, doc, 0600); err != nil {
			return nil, err
		}
	} else if a.Type == "B" {
		deck, err := PPTX(s, a, k)
		if err != nil {
			return nil, err
		}
		file = filepath.Join(tmp, "slides.pptx")
		if err = os.WriteFile(file, deck, 0600); err != nil {
			return nil, err
		}
	} else if err = os.WriteFile(file, readerDocument(s, a, k), 0600); err != nil {
		return nil, err
	}
	pdf, err := documents.ConvertPDFWith(ctx, file, tmp, filter)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(pdf)
}

const nsA = "http://schemas.openxmlformats.org/drawingml/2006/main"
const nsP = "http://schemas.openxmlformats.org/presentationml/2006/main"
const nsR = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
const xmlHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`

func x(text string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' && r != '\r' {
			return -1
		}
		return r
	}, text)))
	return b.String()
}
func rels(items ...[3]string) string {
	var b strings.Builder
	b.WriteString(xmlHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, item := range items {
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s/%s" Target="%s"/>`, item[0], nsR, item[1], x(item[2]))
	}
	b.WriteString("</Relationships>")
	return b.String()
}

const group = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`

func shape(id int, name string, px, py, w, h int, text string, size int, fg, bg string, bold bool, font string) string {
	weight := ""
	if bold {
		weight = ` b="1"`
	}
	fill := `<a:noFill/>`
	if bg != "" {
		fill = `<a:solidFill><a:srgbClr val="` + bg + `"/></a:solidFill>`
	}
	var paragraphs strings.Builder
	for _, line := range strings.Split(text, "\n") {
		fmt.Fprintf(&paragraphs, `<a:p><a:pPr><a:lnSpc><a:spcPct val="112000"/></a:lnSpc></a:pPr><a:r><a:rPr lang="zh-TW" sz="%d"%s><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="%s"/><a:ea typeface="%s"/></a:rPr><a:t xml:space="preserve">%s</a:t></a:r><a:endParaRPr sz="%d"/></a:p>`, size, weight, fg, x(font), x(font), x(line), size)
	}
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom>%s<a:ln><a:noFill/></a:ln></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="90000" rIns="90000" tIns="60000" bIns="60000"/><a:lstStyle/>%s</p:txBody></p:sp>`, id, x(name), px, py, w, h, fill, paragraphs.String())
}

// DefaultLayout gives a slide without boxes (from before box layouts) a
// title, its first figure and its text.
func DefaultLayout(sec domain.Section, figures []domain.Image, geo Geometry) []domain.Element {
	g := geo.Title
	title := textBox("title", "title", g[0], g[1], g[2], g[3], PlainText(sec.Title), scaled(slideTitleSize, geo.Scale), true)
	title.Align = geo.TitleAlign
	layout := []domain.Element{title}
	image := sec.Image
	if image == "" && len(figures) > 0 {
		image = figures[0].Path
	}
	if image != "" {
		ix, iy, iw, ih := in(geo.Body, 0, 0, 0.625, 1)
		px, py, pw, ph := in(geo.Body, 0.665, 0.045, 0.335, 0.91)
		return append(layout,
			imageBox("figure", ix, iy, iw, ih, image),
			textBox("points", "body", px, py, pw, ph, PlainText(sec.Body), scaled(slideBodySize+2, geo.Scale), false))
	}
	px, py, pw, ph := in(geo.Body, 0.05, 0.06, 0.9, 0.88)
	return append(layout, textBox("points", "body", px, py, pw, ph, PlainText(sec.Body), scaled(slideBodySize+4, geo.Scale), false))
}

// Notes pages are portrait: the slide on top, the script underneath.
const notesWidth, notesHeight = 6858000, 9144000

func notesGeometry(width, height int) (img, body [4]int) {
	w := 5943600
	h := w * height / width
	img = [4]int{(notesWidth - w) / 2, 548640, w, h}
	top := img[1] + h + 365760
	body = [4]int{(notesWidth - w) / 2, top, w, notesHeight - top - 548640}
	return img, body
}

// notesParagraphs formats a script: "**subheading**" lines bold, "- " lines
// bulleted, blank lines as paragraph breaks.
func notesParagraphs(notes, font string) string {
	var b strings.Builder
	gap := false
	for _, line := range strings.Split(strings.TrimSpace(domain.StripCitationMarkers(notes)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			gap = true
			continue
		}
		before := 0
		if gap && b.Len() > 0 {
			before = 900
		}
		gap = false
		bold, text, ppr := "", line, ""
		if inner, ok := strings.CutPrefix(line, "**"); ok && strings.HasSuffix(inner, "**") && len(inner) > 2 {
			bold, text = ` b="1"`, strings.TrimSuffix(inner, "**")
			before = max(before, 900)
		}
		text = PlainText(text)
		if rest, ok := cutBullet(line); ok {
			text = PlainText(rest)
			ppr = `<a:buFont typeface="Arial"/><a:buChar char="•"/>`
			fmt.Fprintf(&b, `<a:p><a:pPr marL="228600" indent="-228600"><a:lnSpc><a:spcPct val="125000"/></a:lnSpc><a:spcBef><a:spcPts val="%d"/></a:spcBef>%s</a:pPr>`, max(before, 300), ppr)
		} else {
			fmt.Fprintf(&b, `<a:p><a:pPr marL="0" indent="0"><a:lnSpc><a:spcPct val="125000"/></a:lnSpc><a:spcBef><a:spcPts val="%d"/></a:spcBef><a:buNone/></a:pPr>`, before)
		}
		fmt.Fprintf(&b, `<a:r><a:rPr lang="zh-TW" sz="1400"%s><a:solidFill><a:srgbClr val="222222"/></a:solidFill><a:latin typeface="%s"/><a:ea typeface="%s"/></a:rPr><a:t xml:space="preserve">%s</a:t></a:r></a:p>`, bold, x(font), x(font), x(text))
	}
	if b.Len() == 0 {
		return `<a:p><a:endParaRPr lang="zh-TW" sz="1400"/></a:p>`
	}
	return b.String()
}

func notesPlaceholders(img, body [4]int, paragraphs string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image"/><p:cNvSpPr><a:spLocks noGrp="1" noRot="1" noChangeAspect="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/><a:ln w="9525"><a:solidFill><a:srgbClr val="BFBFBF"/></a:solidFill></a:ln></p:spPr></p:sp>`, img[0], img[1], img[2], img[3]) +
		fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Speaker notes"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="0" rIns="0" tIns="0" bIns="0" anchor="t"><a:normAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, body[0], body[1], body[2], body[3], paragraphs)
}

func PPTX(s *store.Store, a domain.Artifact, k domain.Knowledge) ([]byte, error) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{}
	style := a.Style.Resolved["B"]
	t := themeOf(style)
	if t.template() {
		return templatePPTX(s, a, k, t, style)
	}
	width, height := 12192000, 6858000
	if style["ratio"] == "4:3" {
		width = 9144000
	}
	content := xmlHead + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="jpg" ContentType="image/jpeg"/><Default Extension="jpeg" ContentType="image/jpeg"/>`
	for _, p := range [][2]string{{"/ppt/presentation.xml", "presentationml.presentation.main"}, {"/ppt/slideMasters/slideMaster1.xml", "presentationml.slideMaster"}, {"/ppt/slideLayouts/slideLayout1.xml", "presentationml.slideLayout"}, {"/ppt/slideLayouts/slideLayout2.xml", "presentationml.slideLayout"}, {"/ppt/notesMasters/notesMaster1.xml", "presentationml.notesMaster"}, {"/ppt/theme/theme1.xml", "theme"}} {
		content += fmt.Sprintf(`<Override PartName="%s" ContentType="application/vnd.openxmlformats-officedocument.%s+xml"/>`, p[0], p[1])
	}
	presRels := [][3]string{{"rId1", "slideMaster", "slideMasters/slideMaster1.xml"}, {"rId2", "notesMaster", "notesMasters/notesMaster1.xml"}}
	var slideIDs strings.Builder
	slideFigures := SectionFigures(a, k)
	img, body := notesGeometry(width, height)
	for i, sec := range a.Sections {
		n := i + 1
		if len(sec.Layout) == 0 {
			sec.Layout = DefaultLayout(sec, slideFigures[i], GeometryOf(style))
		}
		content += fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/><Override PartName="/ppt/notesSlides/notesSlide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"/>`, n, n)
		presRels = append(presRels, [3]string{fmt.Sprintf("rId%d", n+2), "slide", fmt.Sprintf("slides/slide%d.xml", n)})
		fmt.Fprintf(&slideIDs, `<p:sldId id="%d" r:id="rId%d"/>`, 255+n, n+2)
		shapes, extra, err := layoutShapes(archive, s, sec, n, width, height, t, style)
		if err != nil {
			return nil, err
		}
		layout := "slideLayout1"
		if slideKind(sec) == "display" {
			layout = "slideLayout2"
		}
		slideRels := [][3]string{{"rId1", "slideLayout", "../slideLayouts/" + layout + ".xml"}, {"rId2", "notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", n)}}
		files[fmt.Sprintf("ppt/slides/slide%d.xml", n)] = xmlHead + fmt.Sprintf(`<p:sld xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:cSld><p:spTree>%s%s</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`, nsA, nsR, nsP, group, shapes)
		files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n)] = rels(append(slideRels, extra...)...)
		files[fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n)] = xmlHead + fmt.Sprintf(`<p:notes xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:cSld><p:spTree>%s%s</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`, nsA, nsR, nsP, group, notesPlaceholders(img, body, notesParagraphs(sec.Notes, t.font)))
		files[fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", n)] = rels([3]string{"rId1", "notesMaster", "../notesMasters/notesMaster1.xml"}, [3]string{"rId2", "slide", fmt.Sprintf("../slides/slide%d.xml", n)})
	}
	files["[Content_Types].xml"] = content + "</Types>"
	files["_rels/.rels"] = rels([3]string{"rId1", "officeDocument", "ppt/presentation.xml"})
	files["ppt/presentation.xml"] = xmlHead + fmt.Sprintf(`<p:presentation xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:notesMasterIdLst><p:notesMasterId r:id="rId2"/></p:notesMasterIdLst><p:sldIdLst>%s</p:sldIdLst><p:sldSz cx="%d" cy="%d"/><p:notesSz cx="%d" cy="%d"/></p:presentation>`, nsA, nsR, nsP, slideIDs.String(), width, height, notesWidth, notesHeight)
	files["ppt/_rels/presentation.xml.rels"] = rels(presRels...)
	clr := `<p:clrMap accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" bg1="lt1" bg2="lt2" folHlink="folHlink" hlink="hlink" tx1="dk1" tx2="dk2"/>`
	background := fmt.Sprintf(`<p:bg><p:bgPr><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`, t.bg)
	files["ppt/slideMasters/slideMaster1.xml"] = xmlHead + fmt.Sprintf(`<p:sldMaster xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:cSld name="EduForge">%s<p:spTree>%s</p:spTree></p:cSld>%s<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/><p:sldLayoutId id="2147483650" r:id="rId3"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`, nsA, nsR, nsP, background, group, clr)
	files["ppt/slideMasters/_rels/slideMaster1.xml.rels"] = rels([3]string{"rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"}, [3]string{"rId2", "theme", "../theme/theme1.xml"}, [3]string{"rId3", "slideLayout", "../slideLayouts/slideLayout2.xml"})
	for i, l := range []struct{ name, kind string }{{"Title and content", "content"}, {"Title slide", "display"}} {
		files[fmt.Sprintf("ppt/slideLayouts/slideLayout%d.xml", i+1)] = xmlHead + fmt.Sprintf(`<p:sldLayout xmlns:a="%s" xmlns:r="%s" xmlns:p="%s" preserve="1"><p:cSld name="%s">%s<p:spTree>%s%s</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`, nsA, nsR, nsP, l.name, background, group, decorShapes(t, l.kind, width, height))
		files[fmt.Sprintf("ppt/slideLayouts/_rels/slideLayout%d.xml.rels", i+1)] = rels([3]string{"rId1", "slideMaster", "../slideMasters/slideMaster1.xml"})
	}
	files["ppt/notesMasters/notesMaster1.xml"] = xmlHead + fmt.Sprintf(`<p:notesMaster xmlns:a="%s" xmlns:r="%s" xmlns:p="%s"><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>%s%s</p:spTree></p:cSld>%s<p:notesStyle><a:lvl1pPr marL="0" indent="0"><a:defRPr sz="1400"><a:solidFill><a:srgbClr val="222222"/></a:solidFill><a:latin typeface="%s"/><a:ea typeface="%s"/></a:defRPr></a:lvl1pPr></p:notesStyle></p:notesMaster>`, nsA, nsR, nsP, group, notesPlaceholders(img, body, `<a:p><a:endParaRPr lang="zh-TW" sz="1400"/></a:p>`), clr, x(t.font), x(t.font))
	files["ppt/notesMasters/_rels/notesMaster1.xml.rels"] = rels([3]string{"rId1", "theme", "../theme/theme1.xml"})
	files["ppt/theme/theme1.xml"] = xmlHead + fmt.Sprintf(`<a:theme xmlns:a="%s" name="EduForge"><a:themeElements><a:clrScheme name="EduForge"><a:dk1><a:srgbClr val="%s"/></a:dk1><a:lt1><a:srgbClr val="%s"/></a:lt1><a:dk2><a:srgbClr val="%s"/></a:dk2><a:lt2><a:srgbClr val="%s"/></a:lt2><a:accent1><a:srgbClr val="%s"/></a:accent1><a:accent2><a:srgbClr val="D5A54E"/></a:accent2><a:accent3><a:srgbClr val="6A8F85"/></a:accent3><a:accent4><a:srgbClr val="56758A"/></a:accent4><a:accent5><a:srgbClr val="B37A68"/></a:accent5><a:accent6><a:srgbClr val="889C6F"/></a:accent6><a:hlink><a:srgbClr val="%s"/></a:hlink><a:folHlink><a:srgbClr val="56758A"/></a:folHlink></a:clrScheme><a:fontScheme name="EduForge"><a:majorFont><a:latin typeface="%s"/><a:ea typeface="%s"/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="%s"/><a:ea typeface="%s"/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="EduForge"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="solid"/></a:ln><a:ln w="25400"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="38100"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`, nsA, t.fg, t.bg, t.fg, t.line, t.accent, t.accent, x(t.font), x(t.font), x(t.font), x(t.font))
	for name, text := range files {
		w, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = ioWriteString(w, text); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func ioWriteString(w interface{ Write([]byte) (int, error) }, text string) (int, error) {
	return w.Write([]byte(text))
}
func strconvString(n int) string { return fmt.Sprintf("%d", n) }
