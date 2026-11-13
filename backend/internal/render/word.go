package render

import (
	"context"
	"encoding/hex"
	"strings"
)

func word(ctx context.Context, in Input) ([]byte, error) {
	p := &packageBuilder{}
	p.xml("_rels/.rels", "", relationships(rel("rId1", "officeDocument", "word/document.xml")))
	family := esc(in.family())
	paragraph := func(text, style string) string {
		runs := ""
		for i, line := range strings.Split(text, "\n") {
			if i > 0 {
				runs += `<w:r><w:br/></w:r>`
			}
			runs += `<w:r><w:t xml:space="preserve">` + esc(line) + `</w:t></w:r>`
		}
		return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>` + runs + `</w:p>`
	}
	body := paragraph(in.Artifact.Content.Title, "Title")
	rels := rel("styles", "styles", "styles.xml") + rel("fonts", "fontTable", "fontTable.xml")
	imageIndex := 0
	for _, b := range in.Artifact.Content.Blocks {
		switch b.Kind {
		case "heading":
			body += paragraph(b.Text, "Heading1")
		case "paragraph":
			body += paragraph(b.Text, "Normal")
		case "list":
			if b.Text != "" {
				body += paragraph(b.Text, "Normal")
			}
			for _, item := range b.Items {
				body += paragraph("• "+item, "Normal")
			}
		case "table":
			if b.Text != "" {
				body += paragraph(b.Text, "Normal")
			}
			body += `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders>`
			for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
				body += `<w:` + side + ` w:val="single" w:sz="4" w:color="CCCCCC"/>`
			}
			body += `</w:tblBorders></w:tblPr><w:tblGrid>`
			for range b.Rows[0] {
				body += `<w:gridCol w:w="` + num(9000/len(b.Rows[0])) + `"/>`
			}
			body += `</w:tblGrid>`
			for _, row := range b.Rows {
				body += `<w:tr>`
				for _, cell := range row {
					body += `<w:tc><w:tcPr><w:tcW w:w="` + num(9000/len(row)) + `" w:type="dxa"/></w:tcPr>` + paragraph(cell, "Normal") + `</w:tc>`
				}
				body += `</w:tr>`
			}
			body += `</w:tbl>`
		case "image":
			imageIndex++
			img := in.Images[b.ImageURL]
			name := "image" + num(imageIndex) + ".png"
			id := "image" + num(imageIndex)
			p.add("word/media/"+name, "image/png", img.Data)
			rels += rel(id, "image", "media/"+name)
			width, height := fit(img, 5486400, 5486400)
			body += `<w:p><w:r><w:drawing><wp:inline xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"><wp:extent cx="` + num(width) + `" cy="` + num(height) + `"/><wp:docPr id="` + num(imageIndex) + `" name="` + id + `" descr="` + esc(b.Text) + `"/><a:graphic xmlns:a="` + aNS + `"><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:nvPicPr><pic:cNvPr id="0" name="` + name + `"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="` + id + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="` + num(width) + `" cy="` + num(height) + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
			if b.Text != "" {
				body += paragraph(b.Text, "Normal")
			}
		}
	}
	body += `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`
	p.xml("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", `<w:document xmlns:w="`+wNS+`" xmlns:r="`+rNS+`"><w:body>`+body+`</w:body></w:document>`)
	p.xml("word/_rels/document.xml.rels", "", relationships(rels))
	p.xml("word/styles.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml", `<w:styles xmlns:w="`+wNS+`"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="`+family+`" w:hAnsi="`+family+`" w:eastAsia="`+family+`"/><w:sz w:val="24"/><w:lang w:val="`+string(in.Artifact.Language)+`" w:eastAsia="`+string(in.Artifact.Language)+`"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/><w:color w:val="`+in.color()+`"/></w:rPr></w:style></w:styles>`)
	// OOXML font obfuscation uses the reversed GUID bytes for the first 32 bytes.
	const fontKey = "00112233-4455-6677-8899-AABBCCDDEEFF"
	key, _ := hex.DecodeString(strings.ReplaceAll(fontKey, "-", ""))
	font := append([]byte(nil), in.Font...)
	for i := 0; i < 32; i++ {
		font[i] ^= key[15-i%16]
	}
	p.add("word/fonts/font.odttf", "application/vnd.openxmlformats-officedocument.obfuscatedFont", font)
	p.xml("word/fontTable.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml", `<w:fonts xmlns:w="`+wNS+`" xmlns:r="`+rNS+`"><w:font w:name="`+family+`"><w:embedRegular r:id="font" w:fontKey="{`+fontKey+`}"/></w:font></w:fonts>`)
	p.xml("word/_rels/fontTable.xml.rels", "", relationships(rel("font", "font", "fonts/font.odttf")))
	return p.finish(ctx)
}
func fit(img Image, w, h int) (int, int) {
	if img.Width <= 0 || img.Height <= 0 {
		return w, h
	}
	if float64(w)/float64(img.Width) < float64(h)/float64(img.Height) {
		return w, int(float64(img.Height) * float64(w) / float64(img.Width))
	}
	return int(float64(img.Width) * float64(h) / float64(img.Height)), h
}
