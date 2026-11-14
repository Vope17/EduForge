package artifacts

import (
	"fmt"
	"html"
	"strings"

	"eduforge/internal/domain"
	"eduforge/internal/store"
)

// deckHTML is the presentation as a handout: every slide drawn from its boxes
// as in the PPTX, followed by its speaker notes; one slide per printed page.
func deckHTML(s *store.Store, a domain.Artifact, k domain.Knowledge) []byte {
	style := a.Style.Resolved["B"]
	t := themeOf(style)
	ratio, points := "16 / 9", 960.0
	if style["ratio"] == "4:3" {
		ratio, points = "4 / 3", 720.0
	}
	if t.template() && t.width > 0 && t.height > 0 {
		ratio, points = fmt.Sprintf("%d / %d", t.width, t.height), float64(t.width)/emuPerPoint
	}
	// An imported template's rendered layouts stand in for its masters.
	backgrounds := map[string]string{}
	for kind, key := range map[string]string{"display": "cover_image", "content": "content_image"} {
		if data := inlineImage(s, str(style, key)); data != "" {
			backgrounds[kind] = fmt.Sprintf(`background-image:url('%s');background-size:100%% 100%%;`, data)
		}
	}
	cqw := func(pt float64) string { return fmt.Sprintf("%.3fcqw", pt/points*100) }
	notesLabel, slideLabel := "Speaker notes", "Slide"
	if a.Language == "zh-TW" {
		notesLabel, slideLabel = "講稿", "投影片"
	}
	title := DocumentTitle(k)
	var out strings.Builder
	fmt.Fprintf(&out, `<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>
@page{size:A4;margin:14mm}*{box-sizing:border-box}
body{margin:0;background:#eef0f2;color:#222;font:16px/1.8 '%s','Noto Sans CJK TC',sans-serif}
main{max-width:920px;margin:32px auto;padding:0 16px}
header{margin:0 0 8px}header p{margin:0;color:#%s;font-weight:700;letter-spacing:.08em;font-size:13px}header h1{margin:6px 0 0;font-size:26px;line-height:1.35}
.page{background:#fff;border-radius:12px;box-shadow:0 1px 3px rgba(0,0,0,.12);margin:24px 0;padding:22px}
.slide{position:relative;aspect-ratio:%s;container-type:inline-size;overflow:hidden;background:#%s;border:1px solid #d8dde2;border-radius:4px;font-family:'%s','Noto Sans CJK TC',sans-serif}
.decor{position:absolute}
.box{position:absolute;display:flex;flex-direction:column;justify-content:flex-start;padding:%s %s;overflow:hidden}
.box.mid{justify-content:center}.box img{width:100%%;height:100%%;object-fit:contain}
.box .l{white-space:pre-wrap;line-height:1.12;overflow-wrap:anywhere}
.box .b{padding-left:1.1em;text-indent:-1.1em;line-height:1.12;margin-top:.45em}.box .b:first-child{margin-top:0}.box .dot{display:inline-block;width:1.1em;text-indent:0;color:#%s}
.no{position:absolute;right:4%%;bottom:1.8%%;font-size:%s;color:#%s}
.notes{margin-top:16px}.notes h2{margin:0 0 4px;font-size:12px;letter-spacing:.08em;color:#%s}
.notes .body{white-space:pre-wrap}.notes .body strong.sub{display:block;margin-top:8px}
@media print{body{background:#fff}main{margin:0;max-width:none;padding:0}.page{box-shadow:none;border-radius:0;margin:0;padding:0;break-after:page}}
</style></head><body><main><header><p>%s</p><h1>%s</h1></header>`,
		a.Language, html.EscapeString(title), html.EscapeString(t.font), t.accent, ratio, t.bg, html.EscapeString(t.font), cqw(3.6), cqw(7.2), t.accent, cqw(11), t.muted, t.accent, html.EscapeString(documentKicker(a)), html.EscapeString(title))
	figures := SectionFigures(a, k)
	texs := []string{}
	for _, sec := range a.Sections {
		for _, el := range sec.Layout {
			if el.Kind == "formula" && el.Tex != "" {
				texs = append(texs, "$$"+el.Tex+"$$")
			}
		}
	}
	math := renderFormulas(texs...)
	for i, sec := range a.Sections {
		if len(sec.Layout) == 0 {
			sec.Layout = DefaultLayout(sec, figures[i], GeometryOf(style))
		}
		kind := slideKind(sec)
		fmt.Fprintf(&out, `<section class="page"><div class="slide" style="%s" aria-label="%s %d">`, backgrounds[kind], slideLabel, i+1)
		for _, d := range t.decorations(kind) {
			fmt.Fprintf(&out, `<div class="decor" style="left:%.2f%%;top:%.2f%%;width:%.2f%%;height:%.2f%%;background:#%s"></div>`, d.X*100, d.Y*100, d.W*100, d.H*100, d.Color)
		}
		for _, el := range sec.Layout {
			pos := fmt.Sprintf("left:%.2f%%;top:%.2f%%;width:%.2f%%;height:%.2f%%", el.X*100, el.Y*100, el.W*100, el.H*100)
			if el.Kind == "image" {
				if data := inlineImage(s, el.Path); data != "" {
					fmt.Fprintf(&out, `<div class="box" style="%s;padding:0"><img src="%s" alt=""></div>`, pos, data)
				}
				continue
			}
			if el.Kind == "formula" {
				if r, ok := math.get(mathPiece{TeX: el.Tex, Display: true}); ok {
					fmt.Fprintf(&out, `<div class="box formula" style="%s;padding:0;display:flex;align-items:center;justify-content:center;font-size:%s;color:#%s">%s</div>`, pos, cqw(formulaPoints), t.fg, strings.Replace(r.SVG, "<svg ", `<svg style="max-width:100%;max-height:100%" `, 1))
				} else {
					fmt.Fprintf(&out, `<div class="box" style="%s;text-align:center">%s</div>`, pos, html.EscapeString(TexText(el.Tex)))
				}
				continue
			}
			size := el.Size
			if size <= 0 {
				size = slideBodySize
			}
			align := el.Align
			if align == "" && el.Role == "caption" {
				align = "center"
			}
			if align == "" {
				align = "left"
			}
			weight := 400
			if el.Bold {
				weight = 700
			}
			class := "box"
			if el.Role == "title" {
				class = "box mid"
			}
			fmt.Fprintf(&out, `<div class="%s" style="%s;font-size:%s;font-weight:%d;text-align:%s;color:#%s">`, class, pos, cqw(size), weight, align, t.textColor(el.Role, kind))
			for _, line := range strings.Split(PlainText(el.Text), "\n") {
				if rest, ok := cutBullet(strings.TrimSpace(line)); ok {
					fmt.Fprintf(&out, `<div class="b"><span class="dot">•</span>%s</div>`, html.EscapeString(rest))
				} else {
					fmt.Fprintf(&out, `<div class="l">%s</div>`, html.EscapeString(line))
				}
			}
			out.WriteString(`</div>`)
		}
		if kind == "content" && !t.template() {
			fmt.Fprintf(&out, `<span class="no">%d</span>`, i+1)
		}
		out.WriteString(`</div>`)
		if notes := strings.TrimSpace(sec.Notes); notes != "" {
			fmt.Fprintf(&out, `<div class="notes"><h2>%s · %d</h2><div class="body">%s</div></div>`, notesLabel, i+1, bodyHTML(notes))
		}
		out.WriteString(`</section>`)
	}
	out.WriteString(`</main></body></html>`)
	return []byte(out.String())
}
