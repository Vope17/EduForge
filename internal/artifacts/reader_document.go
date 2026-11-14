package artifacts

import (
	"encoding/base64"
	"fmt"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/mathtex"
	"eduforge/internal/store"
)

// Writer's native format preserves page flow, text styles and figures during PDF export.
func readerDocument(s *store.Store, a domain.Artifact, k domain.Knowledge) []byte {
	accent := color(a.Style.Resolved[a.Type], "accent", "087F73")
	var doc strings.Builder
	fmt.Fprintf(&doc, `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
 xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0"
 xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"
 xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"
 xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0"
 xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0"
 xmlns:xlink="http://www.w3.org/1999/xlink"
 office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.text">
 <office:font-face-decls><style:font-face style:name="Noto" svg:font-family="Noto Sans CJK TC"/></office:font-face-decls>
 <office:styles>
  <style:default-style style:family="paragraph">
   <style:paragraph-properties fo:line-height="165%%" fo:margin-bottom="0.12cm"/>
   <style:text-properties style:font-name="Noto" style:font-name-asian="Noto" fo:font-size="11pt" style:font-size-asian="11pt" fo:color="#19332F"/>
  </style:default-style>
  <style:style style:name="Kicker" style:family="paragraph">
   <style:paragraph-properties fo:keep-with-next="always" fo:margin-bottom="0.1cm"/>
   <style:text-properties fo:font-size="10pt" style:font-size-asian="10pt" fo:font-weight="bold" style:font-weight-asian="bold" fo:letter-spacing="0.05cm" fo:color="#%[1]s"/>
  </style:style>
  <style:style style:name="Title" style:family="paragraph">
   <style:paragraph-properties fo:text-align="start" fo:keep-with-next="always" fo:line-height="130%%" fo:margin-bottom="0.5cm" fo:padding-bottom="0.3cm" fo:border-bottom="0.06cm solid #%[1]s"/>
   <style:text-properties fo:font-size="21pt" style:font-size-asian="21pt" fo:font-weight="bold" style:font-weight-asian="bold"/>
  </style:style>
  <style:style style:name="Section" style:family="paragraph">
   <style:paragraph-properties fo:text-align="start" fo:keep-with-next="always" fo:margin-top="1.1cm" fo:margin-bottom="0.4cm" fo:padding-bottom="0.15cm" fo:border-bottom="0.03cm solid #%[1]s"/>
   <style:text-properties fo:font-size="16pt" style:font-size-asian="16pt" fo:font-weight="bold" style:font-weight-asian="bold" fo:color="#%[1]s"/>
  </style:style>
  <style:style style:name="Chapter" style:family="paragraph" style:parent-style-name="Section">
   <style:paragraph-properties fo:break-before="page" fo:margin-top="0cm"/>
  </style:style>
  <style:style style:name="Gap" style:family="paragraph">
   <style:paragraph-properties fo:line-height="100%%" fo:margin-bottom="0.18cm"/>
   <style:text-properties fo:font-size="4pt" style:font-size-asian="4pt"/>
  </style:style>
  <style:style style:name="List" style:family="paragraph">
   <style:paragraph-properties fo:margin-left="0.6cm" fo:text-indent="-0.4cm" fo:margin-bottom="0.1cm"/>
  </style:style>
  <style:style style:name="Formula" style:family="paragraph">
   <style:paragraph-properties fo:text-align="center" fo:margin-top="0.25cm" fo:margin-bottom="0.3cm"/>
  </style:style>
  <style:style style:name="Math" style:family="graphic">
   <style:graphic-properties style:vertical-pos="from-top" style:vertical-rel="baseline" fo:padding="0cm" fo:border="none" style:wrap="none"/>
  </style:style>
  <style:style style:name="Figure" style:family="paragraph">
   <style:paragraph-properties fo:text-align="center" fo:keep-with-next="always" fo:margin-top="0.45cm" fo:margin-bottom="0.15cm"/>
  </style:style>
  <style:style style:name="Caption" style:family="paragraph">
   <style:paragraph-properties fo:text-align="start" fo:margin-left="0.8cm" fo:margin-right="0.8cm" fo:margin-bottom="0.6cm" fo:line-height="140%%"/>
   <style:text-properties fo:font-size="9pt" style:font-size-asian="9pt" fo:color="#526E67"/>
  </style:style>
  <style:style style:name="Strong" style:family="text">
   <style:text-properties fo:font-weight="bold" style:font-weight-asian="bold"/>
  </style:style>
  <style:style style:name="Term" style:family="paragraph">
   <style:paragraph-properties fo:text-align="start" fo:keep-with-next="always" fo:margin-top="0.5cm" fo:margin-bottom="0.15cm"/>
   <style:text-properties fo:font-size="12.5pt" style:font-size-asian="12.5pt" fo:font-weight="bold" style:font-weight-asian="bold"/>
  </style:style>
 </office:styles>
 <office:automatic-styles><style:page-layout style:name="Page">
  <style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin="1.8cm"/>
 </style:page-layout></office:automatic-styles>
 <office:master-styles><style:master-page style:name="Standard" style:page-layout-name="Page"/></office:master-styles>
 <office:body><office:text>`, accent)
	write := func(style, text string) {
		for _, line := range strings.Split(text, "\n") {
			fmt.Fprintf(&doc, `<text:p text:style-name="%s">%s</text:p>`, style, x(line))
		}
	}
	bodies := []string{}
	for _, sec := range a.Sections {
		bodies = append(bodies, sec.Body)
	}
	math := renderFormulas(bodies...)
	// formula is a rendered formula as an SVG picture on the text baseline
	// (sizes in ex of 11 pt text), or its TeX source.
	formula := func(p mathPiece) string {
		r, ok := math.get(p)
		if !ok {
			if p.Display {
				return x("$$" + p.TeX + "$$")
			}
			return x("$" + p.TeX + "$")
		}
		const ex = 0.442 * 11 * 0.03528 // cm
		return fmt.Sprintf(`<draw:frame draw:style-name="Math" text:anchor-type="as-char" svg:y="-%.3fcm" svg:width="%.3fcm" svg:height="%.3fcm"><draw:image draw:mime-type="image/svg+xml"><office:binary-data>%s</office:binary-data></draw:image></draw:frame>`,
			(r.Height-r.Depth)*ex, r.Width*ex, r.Height*ex, base64.StdEncoding.EncodeToString([]byte(mathtex.Colored(mathtex.Sized(r, 0.442*11), "19332F"))))
	}
	// writeLine renders one body line of the Markdown subset: subheadings in
	// the bold Term style, bullets indented, inline **bold** as Strong spans,
	// formulas as pictures, markers removed; a blank line separates
	// paragraphs.
	writeLine := func(line string) {
		if strings.TrimSpace(line) == "" {
			doc.WriteString(`<text:p text:style-name="Gap"/>`)
			return
		}
		if tex, ok := displayFormula(line); ok {
			fmt.Fprintf(&doc, `<text:p text:style-name="Formula">%s</text:p>`, formula(mathPiece{TeX: tex, Display: true}))
			return
		}
		l := parseMarkupLine(line)
		style := "Standard"
		if l.heading {
			style = "Term"
		} else if l.bullet != "" {
			style = "List"
		}
		var runs strings.Builder
		runs.WriteString(x(l.bullet))
		for _, piece := range splitMath(l.text) {
			if piece.isMath() {
				runs.WriteString(formula(piece))
				continue
			}
			for _, seg := range inlineSegments(piece.Text) {
				if seg.bold && !l.heading {
					runs.WriteString(`<text:span text:style-name="Strong">` + x(seg.text) + `</text:span>`)
				} else {
					runs.WriteString(x(seg.text))
				}
			}
		}
		fmt.Fprintf(&doc, `<text:p text:style-name="%s">%s</text:p>`, style, runs.String())
	}
	write("Kicker", documentKicker(a))
	write("Title", DocumentTitle(k))
	writeFigure := func(figure domain.Image) {
		uri := inlineImage(s, figure.Path)
		header, content, ok := strings.Cut(uri, ";base64,")
		if !ok {
			return
		}
		bytes, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return
		}
		w, h := fit(bytes, 15500, 9000)
		fmt.Fprintf(&doc, `<text:p text:style-name="Figure"><draw:frame text:anchor-type="as-char" svg:width="%.2fcm" svg:height="%.2fcm"><draw:image draw:mime-type="%s"><office:binary-data>%s</office:binary-data></draw:image></draw:frame></text:p>`,
			float64(w)/1000, float64(h)/1000, strings.TrimPrefix(header, "data:"), content)
		if caption := figureCaption(figure); caption != "" {
			write("Caption", caption)
		}
	}
	figures := SectionFigures(a, k)
	for i, section := range a.Sections {
		heading := PlainText(section.Title)
		if a.Type != "B" {
			heading = Roman(i+1) + ".  " + SectionTitle(section.Title)
		}
		// Longer readers start each chapter on a new page.
		style := "Section"
		if i > 0 && a.Type == "A" && a.Settings.Depth != "brief" {
			style = "Chapter"
		}
		write(style, heading)
		body := domain.StripCitationMarkers(section.Body)
		placed := PlaceFigures(body, figures[i])
		for j, line := range strings.Split(body, "\n") {
			writeLine(line)
			for _, figure := range placed[j] {
				writeFigure(figure)
			}
		}
	}
	if names := LessonNames(k); len(names) > 0 {
		write("Section", referencesTitle(a.Language))
		for i, name := range names {
			write("Standard", fmt.Sprintf("[%d] %s", i+1, name))
		}
	}
	doc.WriteString("</office:text></office:body></office:document>")
	return []byte(doc.String())
}
