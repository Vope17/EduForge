package render

import (
	"context"
	"math"
	"strings"
)

const group = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
const namespaces = ` xmlns:p="` + pNS + `" xmlns:a="` + aNS + `" xmlns:r="` + rNS + `"`
const colorMap = `<p:clrMap accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" bg1="lt1" bg2="lt2" folHlink="folHlink" hlink="hlink" tx1="dk1" tx2="dk2"/>`

func slides(ctx context.Context, in Input) ([]byte, error) {
	p := &packageBuilder{}
	p.xml("_rels/.rels", "", relationships(rel("rId1", "officeDocument", "ppt/presentation.xml")))
	family := esc(in.family())
	ids := ""
	rels := rel("master", "slideMaster", "slideMasters/slideMaster1.xml") + rel("notesMaster", "notesMaster", "notesMasters/notesMaster1.xml") + rel("font", "font", "fonts/font.ttf")
	for i, page := range in.Artifact.Content.Blocks {
		index := num(i + 1)
		ids += `<p:sldId id="` + num(256+i) + `" r:id="slide` + index + `"/>`
		rels += rel("slide"+index, "slide", "slides/slide"+index+".xml")
		srels := rel("layout", "slideLayout", "../slideLayouts/slideLayout1.xml") + rel("notes", "notesSlide", "../notesSlides/notesSlide"+index+".xml")
		shapes := group
		shapeID := 2
		textBox := func(text string, x, y, w, h, size int, bold bool) string {
			id := num(shapeID)
			shapeID++
			weight := ""
			if bold {
				weight = ` b="1"`
			}
			paras := ""
			for _, line := range strings.Split(text, "\n") {
				paras += `<a:p><a:r><a:rPr lang="` + string(in.Artifact.Language) + `" sz="` + num(size) + `"` + weight + `><a:solidFill><a:srgbClr val="` + in.color() + `"/></a:solidFill><a:latin typeface="` + family + `"/><a:ea typeface="` + family + `"/></a:rPr><a:t>` + esc(line) + `</a:t></a:r></a:p>`
			}
			return `<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name="Text ` + id + `"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="` + num(x) + `" y="` + num(y) + `"/><a:ext cx="` + num(w) + `" cy="` + num(h) + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="0" rIns="0" tIns="0" bIns="0"><a:normAutofit/></a:bodyPr><a:lstStyle/>` + paras + `</p:txBody></p:sp>`
		}
		contentCount := len(page.Children) - 1
		slot := 0
		for _, block := range page.Children {
			if block.Kind == "heading" {
				size := fittedSize(block.Text, 11094720, 1097280, 3200, 1600)
				if size == 0 {
					return nil, ErrLayout
				}
				shapes += textBox(block.Text, 548640, 365760, 11094720, 1097280, size, true)
				continue
			}
			x, y, w, h := 548640, 1645920, 11094720, 4663440
			columns := page.Layout != "title_body"
			rows := contentCount
			if columns {
				rows = (contentCount + 1) / 2
				w = 5303520
				x += (slot % 2) * 5791200
				y += (slot / 2) * (h / rows)
				h = h/rows - 182880
			} else {
				y += slot * (h / rows)
				h = h/rows - 182880
			}
			slot++
			if block.Kind == "image" && block.ImageURL != "" {
				img := in.Images[block.ImageURL]
				name := "image" + index + "_" + num(slot) + ".png"
				id := "image" + num(slot)
				p.add("ppt/media/"+name, "image/png", img.Data)
				srels += rel(id, "image", "../media/"+name)
				iw, ih := fit(img, w, h)
				x += (w - iw) / 2
				y += (h - ih) / 2
				shapes += `<p:pic><p:nvPicPr><p:cNvPr id="` + num(shapeID) + `" name="` + name + `" descr="` + esc(block.Text) + `"/><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="` + id + `"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm><a:off x="` + num(x) + `" y="` + num(y) + `"/><a:ext cx="` + num(iw) + `" cy="` + num(ih) + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>`
				shapeID++
			} else {
				text := block.Text
				for _, item := range block.Items {
					if text != "" {
						text += "\n"
					}
					text += "• " + item
				}
				size := fittedSize(text, w, h, 2400, 1000)
				if size == 0 {
					return nil, ErrLayout
				}
				shapes += textBox(text, x, y, w, h, size, false)
			}
		}
		p.xml("ppt/slides/slide"+index+".xml", "application/vnd.openxmlformats-officedocument.presentationml.slide+xml", `<p:sld`+namespaces+`><p:cSld><p:spTree>`+shapes+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`)
		p.xml("ppt/slides/_rels/slide"+index+".xml.rels", "", relationships(srels))
		p.xml("ppt/notesSlides/notesSlide"+index+".xml", "application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml", `<p:notes`+namespaces+`><p:cSld><p:spTree>`+group+`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Notes"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>`+esc(page.Notes)+`</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:notes>`)
		p.xml("ppt/notesSlides/_rels/notesSlide"+index+".xml.rels", "", relationships(rel("master", "notesMaster", "../notesMasters/notesMaster1.xml")+rel("slide", "slide", "../slides/slide"+index+".xml")))
	}
	p.xml("ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml", `<p:presentation`+namespaces+` embedTrueTypeFonts="1" saveSubsetFonts="0"><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="master"/></p:sldMasterIdLst><p:notesMasterIdLst><p:notesMasterId r:id="notesMaster"/></p:notesMasterIdLst><p:sldIdLst>`+ids+`</p:sldIdLst><p:sldSz cx="12192000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/><p:embeddedFontLst><p:embeddedFont><p:font typeface="`+family+`"/><p:regular r:id="font"/></p:embeddedFont></p:embeddedFontLst></p:presentation>`)
	p.xml("ppt/_rels/presentation.xml.rels", "", relationships(rels))
	p.add("ppt/fonts/font.ttf", "application/x-font-ttf", in.Font)
	p.xml("ppt/slideMasters/slideMaster1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml", `<p:sldMaster`+namespaces+`><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>`+group+`</p:spTree></p:cSld>`+colorMap+`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="layout"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle/><p:bodyStyle/><p:otherStyle/></p:txStyles></p:sldMaster>`)
	p.xml("ppt/slideMasters/_rels/slideMaster1.xml.rels", "", relationships(rel("layout", "slideLayout", "../slideLayouts/slideLayout1.xml")+rel("theme", "theme", "../theme/theme1.xml")))
	p.xml("ppt/slideLayouts/slideLayout1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml", `<p:sldLayout`+namespaces+` type="blank" preserve="1"><p:cSld name="Blank"><p:spTree>`+group+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`)
	p.xml("ppt/slideLayouts/_rels/slideLayout1.xml.rels", "", relationships(rel("master", "slideMaster", "../slideMasters/slideMaster1.xml")))
	p.xml("ppt/notesMasters/notesMaster1.xml", "application/vnd.openxmlformats-officedocument.presentationml.notesMaster+xml", `<p:notesMaster`+namespaces+`><p:cSld><p:spTree>`+group+`</p:spTree></p:cSld>`+colorMap+`<p:notesStyle/></p:notesMaster>`)
	p.xml("ppt/notesMasters/_rels/notesMaster1.xml.rels", "", relationships(rel("theme", "theme", "../theme/theme1.xml")))
	colors := ""
	for _, c := range []struct{ name, value string }{{"dk1", "000000"}, {"lt1", "FFFFFF"}, {"dk2", "222222"}, {"lt2", "F5F5F5"}, {"accent1", in.color()}, {"accent2", "447788"}, {"accent3", "779944"}, {"accent4", "BB8844"}, {"accent5", "886699"}, {"accent6", "448877"}, {"hlink", "0563C1"}, {"folHlink", "954F72"}} {
		colors += `<a:` + c.name + `><a:srgbClr val="` + c.value + `"/></a:` + c.name + `>`
	}
	solid := `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	font := `<a:latin typeface="` + family + `"/><a:ea typeface="` + family + `"/><a:cs typeface="` + family + `"/>`
	p.xml("ppt/theme/theme1.xml", "application/vnd.openxmlformats-officedocument.theme+xml", `<a:theme xmlns:a="`+aNS+`" name="EduForge"><a:themeElements><a:clrScheme name="EduForge">`+colors+`</a:clrScheme><a:fontScheme name="EduForge"><a:majorFont>`+font+`</a:majorFont><a:minorFont>`+font+`</a:minorFont></a:fontScheme><a:fmtScheme name="EduForge"><a:fillStyleLst>`+strings.Repeat(solid, 3)+`</a:fillStyleLst><a:lnStyleLst>`+strings.Repeat(`<a:ln w="9525">`+solid+`<a:prstDash val="solid"/></a:ln>`, 3)+`</a:lnStyleLst><a:effectStyleLst>`+strings.Repeat(`<a:effectStyle><a:effectLst/></a:effectStyle>`, 3)+`</a:effectStyleLst><a:bgFillStyleLst>`+strings.Repeat(solid, 3)+`</a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`)
	return p.finish(ctx)
}

// Estimate conservatively before packaging; avoid silently clipping dense slides.
func fittedSize(text string, width, height, maximum, minimum int) int {
	for size := maximum; size >= minimum; size -= 100 {
		points := float64(size) / 100
		capacity := float64(width) / 12700 / points
		lines := 0
		for _, line := range strings.Split(text, "\n") {
			units := 0.0
			for _, r := range line {
				if r > 255 {
					units += 1.1
				} else {
					units += 0.65
				}
			}
			lines += max(1, int(math.Ceil(units/capacity)))
		}
		if float64(lines)*points*1.35 <= float64(height)/12700 {
			return size
		}
	}
	return 0
}
