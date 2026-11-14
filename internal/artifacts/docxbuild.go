package artifacts

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"strings"
)

// A minimal Word document writer for the forms and worksheets EduForge makes
// itself: paragraphs and tables with the Noto fonts, nothing else. (Handouts
// go through the Writer pipeline in reader_document.go, which has figures,
// formulas and templates; these documents are tables and short text.)

const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

// wRun is one run of text with its look.
type wRun struct {
	Text  string
	Bold  bool
	Size  int // half-points; 0 for the default
	Color string
}

func (r wRun) node() *xnode {
	rpr := elem("w:rPr")
	rpr.kids = append(rpr.kids, elem("w:rFonts").attr("w:ascii", "Noto Sans CJK TC").attr("w:hAnsi", "Noto Sans CJK TC").attr("w:eastAsia", "Noto Sans CJK TC"))
	if r.Bold {
		rpr.kids = append(rpr.kids, elem("w:b"))
	}
	if r.Color != "" {
		rpr.kids = append(rpr.kids, elem("w:color").attr("w:val", r.Color))
	}
	if r.Size > 0 {
		rpr.kids = append(rpr.kids, elem("w:sz").attr("w:val", wnum(r.Size)))
	}
	parts := strings.Split(r.Text, "\n")
	run := elem("w:r", rpr)
	for i, part := range parts {
		if i > 0 {
			run.kids = append(run.kids, elem("w:br"))
		}
		run.kids = append(run.kids, elem("w:t", textNode(part)).attr("xml:space", "preserve"))
	}
	return run
}

func wnum(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// wPara builds a paragraph: align is "", "center" or "right"; after is the
// space below in twips.
func wPara(align string, after int, runs ...wRun) *xnode {
	ppr := elem("w:pPr", elem("w:spacing").attr("w:after", wnum(after)).attr("w:line", "300").attr("w:lineRule", "auto"))
	if align != "" {
		ppr.kids = append(ppr.kids, elem("w:jc").attr("w:val", align))
	}
	p := elem("w:p", ppr)
	for _, r := range runs {
		p.kids = append(p.kids, r.node())
	}
	return p
}

// wCell builds a table cell holding paragraphs (one per line of text).
func wCell(width, span int, fill string, bold bool, text string) *xnode {
	tcpr := elem("w:tcPr", elem("w:tcW").attr("w:w", wnum(width)).attr("w:type", "dxa"))
	if span > 1 {
		tcpr.kids = append(tcpr.kids, elem("w:gridSpan").attr("w:val", wnum(span)))
	}
	if fill != "" {
		tcpr.kids = append(tcpr.kids, elem("w:shd").attr("w:val", "clear").attr("w:color", "auto").attr("w:fill", fill))
	}
	tc := elem("w:tc", tcpr)
	for _, line := range strings.Split(text, "\n") {
		tc.kids = append(tc.kids, wPara("", 40, wRun{Text: line, Bold: bold, Size: 21}))
	}
	return tc
}

// wRow builds a row from cells.
func wRow(cells ...*xnode) *xnode { return elem("w:tr", cells...) }

// wTable builds a bordered table with the given column widths (twips).
func wTable(widths []int, rows ...*xnode) *xnode {
	border := func(name string) *xnode {
		return elem(name).attr("w:val", "single").attr("w:sz", "4").attr("w:space", "0").attr("w:color", "808080")
	}
	tblpr := elem("w:tblPr",
		elem("w:tblW").attr("w:w", "0").attr("w:type", "auto"),
		elem("w:tblBorders", border("w:top"), border("w:left"), border("w:bottom"), border("w:right"), border("w:insideH"), border("w:insideV")),
		elem("w:tblLayout").attr("w:type", "fixed"),
		elem("w:tblCellMar", elem("w:top").attr("w:w", "40").attr("w:type", "dxa"), elem("w:left").attr("w:w", "90").attr("w:type", "dxa"), elem("w:bottom").attr("w:w", "40").attr("w:type", "dxa"), elem("w:right").attr("w:w", "90").attr("w:type", "dxa")),
	)
	grid := elem("w:tblGrid")
	for _, w := range widths {
		grid.kids = append(grid.kids, elem("w:gridCol").attr("w:w", wnum(w)))
	}
	t := elem("w:tbl", tblpr, grid)
	t.kids = append(t.kids, rows...)
	return t
}

// docxBytes packs body elements into a Word document (A4, 2 cm margins).
func docxBytes(body ...*xnode) []byte {
	doc := elem("w:document", elem("w:body"))
	doc.attrs = append(doc.attrs, attrOf("xmlns:w", "http://schemas.openxmlformats.org/wordprocessingml/2006/main"))
	b := doc.kids[0]
	b.kids = append(b.kids, body...)
	b.kids = append(b.kids, elem("w:sectPr",
		elem("w:pgSz").attr("w:w", "11906").attr("w:h", "16838"),
		elem("w:pgMar").attr("w:top", "1134").attr("w:right", "1134").attr("w:bottom", "1134").attr("w:left", "1134").attr("w:header", "567").attr("w:footer", "567").attr("w:gutter", "0"),
	))
	return packDocx((&xnode{kids: []*xnode{{raw: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`}, doc}}).bytes())
}

func attrOf(name, value string) (a xml.Attr) {
	a.Value = value
	if prefix, local, ok := strings.Cut(name, ":"); ok {
		a.Name.Space, a.Name.Local = prefix, local
	} else {
		a.Name.Local = name
	}
	return a
}

// packDocx wraps document.xml in the package parts a Word file needs.
func packDocx(document []byte) []byte {
	files := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"word/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles ` + wNS + `><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Noto Sans CJK TC" w:hAnsi="Noto Sans CJK TC" w:eastAsia="Noto Sans CJK TC" w:cs="Noto Sans CJK TC"/><w:sz w:val="21"/><w:szCs w:val="21"/><w:lang w:val="en-US" w:eastAsia="zh-TW"/></w:rPr></w:rPrDefault></w:docDefaults></w:styles>`},
		{"word/document.xml", string(document)},
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		fw, _ := w.Create(f.name)
		_, _ = fw.Write([]byte(f.body))
	}
	_ = w.Close()
	return buf.Bytes()
}
