"""Writes tests/fixtures/template.potx and reader-template.dotx: small Office templates used by the
UI flow test. A dark navy master with an orange band, a centred amber title
style, a title layout and a title-only layout with a slide number.

Run: python3 tests/fixtures/make-template.py
"""
import os
import zipfile

A = "http://schemas.openxmlformats.org/drawingml/2006/main"
R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
P = "http://schemas.openxmlformats.org/presentationml/2006/main"
NS = f'xmlns:a="{A}" xmlns:r="{R}" xmlns:p="{P}"'
HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
GROUP = '<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>'
W, H = 12192000, 6858000


def rels(*items):
    body = "".join(f'<Relationship Id="{i}" Type="{R}/{t}" Target="{target}"/>' for i, t, target in items)
    return HEAD + f'<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">{body}</Relationships>'


def ph(n, typ, idx="", box=None):
    attrs = (f' type="{typ}"' if typ else "") + (f' idx="{idx}"' if idx else "")
    geom = f'<a:xfrm><a:off x="{box[0]}" y="{box[1]}"/><a:ext cx="{box[2]}" cy="{box[3]}"/></a:xfrm>' if box else ""
    return f'<p:sp><p:nvSpPr><p:cNvPr id="{n}" name="ph{n}"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph{attrs}/></p:nvPr></p:nvSpPr><p:spPr>{geom}</p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody></p:sp>'


def rect(n, box, color):
    return f'<p:sp><p:nvSpPr><p:cNvPr id="{n}" name="band"/><p:cNvSpPr/><p:nvPr userDrawn="1"/></p:nvSpPr><p:spPr><a:xfrm><a:off x="{box[0]}" y="{box[1]}"/><a:ext cx="{box[2]}" cy="{box[3]}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="{color}"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr></p:sp>'


clrmap = '<p:clrMap bg1="dk1" tx1="lt1" bg2="dk2" tx2="lt2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>'
parts = {
    "[Content_Types].xml": HEAD + '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>'
    + "".join(f'<Override PartName="/{name}" ContentType="application/vnd.openxmlformats-officedocument.{typ}+xml"/>' for name, typ in [
        ("ppt/presentation.xml", "presentationml.template.main"), ("ppt/slideMasters/slideMaster1.xml", "presentationml.slideMaster"),
        ("ppt/slideLayouts/slideLayout1.xml", "presentationml.slideLayout"), ("ppt/slideLayouts/slideLayout2.xml", "presentationml.slideLayout"),
        ("ppt/theme/theme1.xml", "theme"), ("ppt/notesMasters/notesMaster1.xml", "presentationml.notesMaster"), ("ppt/slides/slide1.xml", "presentationml.slide")])
    + "</Types>",
    "_rels/.rels": rels(("rId1", "officeDocument", "ppt/presentation.xml")),
    "ppt/presentation.xml": HEAD + f'<p:presentation {NS}><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:notesMasterIdLst><p:notesMasterId r:id="rId2"/></p:notesMasterIdLst><p:sldIdLst><p:sldId id="256" r:id="rId3"/></p:sldIdLst><p:sldSz cx="{W}" cy="{H}"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>',
    "ppt/_rels/presentation.xml.rels": rels(("rId1", "slideMaster", "slideMasters/slideMaster1.xml"), ("rId2", "notesMaster", "notesMasters/notesMaster1.xml"), ("rId3", "slide", "slides/slide1.xml"), ("rId4", "theme", "theme/theme1.xml")),
    "ppt/slideMasters/slideMaster1.xml": HEAD + f'<p:sldMaster {NS}><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="14213D"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>{GROUP}'
    + rect(9, (0, H - 260000, W, 260000), "FCA311") + ph(2, "title", "", (609600, 304800, 10972800, 1143000)) + ph(3, "body", "1", (609600, 1600200, 10972800, 4525963))
    + f'</p:spTree></p:cSld>{clrmap}<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/><p:sldLayoutId id="2147483650" r:id="rId2"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle><a:lvl1pPr algn="ctr"><a:defRPr sz="4000" b="1"><a:solidFill><a:srgbClr val="FCA311"/></a:solidFill><a:latin typeface="+mj-lt"/></a:defRPr></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr><a:defRPr sz="2400"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:defRPr></a:lvl1pPr></p:bodyStyle><p:otherStyle/></p:txStyles></p:sldMaster>',
    "ppt/slideMasters/_rels/slideMaster1.xml.rels": rels(("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"), ("rId2", "slideLayout", "../slideLayouts/slideLayout2.xml"), ("rId3", "theme", "../theme/theme1.xml")),
    "ppt/slideLayouts/slideLayout1.xml": HEAD + f'<p:sldLayout {NS} type="title" preserve="1"><p:cSld name="Title Slide"><p:spTree>{GROUP}'
    + rect(9, (0, 0, 420000, H), "FCA311") + ph(2, "ctrTitle", "", (1524000, 1900000, 9144000, 1700000)) + ph(3, "subTitle", "1", (1524000, 3800000, 9144000, 900000))
    + '</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>',
    "ppt/slideLayouts/_rels/slideLayout1.xml.rels": rels(("rId1", "slideMaster", "../slideMasters/slideMaster1.xml")),
    "ppt/slideLayouts/slideLayout2.xml": HEAD + f'<p:sldLayout {NS} type="titleOnly" preserve="1"><p:cSld name="Title Only"><p:spTree>{GROUP}'
    + ph(2, "title") + ph(3, "sldNum", "12", (8610600, 6356350, 2743200, 365125))
    + '</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>',
    "ppt/slideLayouts/_rels/slideLayout2.xml.rels": rels(("rId1", "slideMaster", "../slideMasters/slideMaster1.xml")),
    "ppt/theme/theme1.xml": HEAD + f'<a:theme xmlns:a="{A}" name="EduForge fixture"><a:themeElements><a:clrScheme name="Fixture"><a:dk1><a:srgbClr val="14213D"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="000000"/></a:dk2><a:lt2><a:srgbClr val="E5E5E5"/></a:lt2><a:accent1><a:srgbClr val="FCA311"/></a:accent1><a:accent2><a:srgbClr val="4F6D7A"/></a:accent2><a:accent3><a:srgbClr val="56A3A6"/></a:accent3><a:accent4><a:srgbClr val="DB504A"/></a:accent4><a:accent5><a:srgbClr val="E3B505"/></a:accent5><a:accent6><a:srgbClr val="84A98C"/></a:accent6><a:hlink><a:srgbClr val="FCA311"/></a:hlink><a:folHlink><a:srgbClr val="56A3A6"/></a:folHlink></a:clrScheme><a:fontScheme name="Fixture"><a:majorFont><a:latin typeface="DejaVu Serif"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="DejaVu Sans"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Fixture"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="25400"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="38100"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>',
    "ppt/notesMasters/notesMaster1.xml": HEAD + f'<p:notesMaster {NS}><p:cSld><p:spTree>{GROUP}</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/></p:notesMaster>',
    "ppt/notesMasters/_rels/notesMaster1.xml.rels": rels(("rId1", "theme", "../theme/theme1.xml")),
    "ppt/slides/slide1.xml": HEAD + f'<p:sld {NS}><p:cSld><p:spTree>{GROUP}{ph(2, "title")}</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>',
    "ppt/slides/_rels/slide1.xml.rels": rels(("rId1", "slideLayout", "../slideLayouts/slideLayout2.xml")),
}

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "template.potx")
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for name in ["[Content_Types].xml"] + sorted(n for n in parts if n != "[Content_Types].xml"):
        z.writestr(name, parts[name])
print(out)

# Word template for readers: an amber Title, navy Heading 1 and a running header.
W = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
WNS = f'xmlns:w="{W}" xmlns:r="{R}"'


def wstyle(sid, name, ppr="", rpr=""):
    return f'<w:style w:type="paragraph" w:styleId="{sid}"><w:name w:val="{name}"/><w:basedOn w:val="Normal"/><w:qFormat/>{ppr}{rpr}</w:style>'


word = {
    "[Content_Types].xml": HEAD + '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>'
    + '<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/></Types>',
    "_rels/.rels": rels(("rId1", "officeDocument", "word/document.xml")),
    "word/document.xml": HEAD + f'<w:document {WNS}><w:body><w:p><w:pPr><w:pStyle w:val="Title"/></w:pPr><w:r><w:t>Template title</w:t></w:r></w:p><w:sectPr><w:headerReference w:type="default" r:id="rId2"/><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1700" w:right="1300" w:bottom="1300" w:left="1300" w:header="600" w:footer="600" w:gutter="0"/></w:sectPr></w:body></w:document>',
    "word/_rels/document.xml.rels": rels(("rId1", "styles", "styles.xml"), ("rId2", "header", "header1.xml")),
    "word/styles.xml": HEAD + f'<w:styles {WNS}><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="DejaVu Serif" w:hAnsi="DejaVu Serif"/><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>'
    + '<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:pPr><w:spacing w:after="120" w:line="300" w:lineRule="auto"/></w:pPr></w:style>'
    + wstyle("Title", "Title", '<w:pPr><w:spacing w:after="240"/></w:pPr>', '<w:rPr><w:b/><w:color w:val="FCA311"/><w:sz w:val="48"/></w:rPr>')
    + wstyle("Heading1", "heading 1", '<w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr>', '<w:rPr><w:b/><w:color w:val="14213D"/><w:sz w:val="32"/></w:rPr>')
    + '</w:styles>',
    "word/header1.xml": HEAD + f'<w:hdr {WNS}><w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:r><w:rPr><w:color w:val="FCA311"/></w:rPr><w:t>EduForge fixture header</w:t></w:r></w:p></w:hdr>',
}
out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "reader-template.dotx")
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for name in ["[Content_Types].xml"] + sorted(n for n in word if n != "[Content_Types].xml"):
        z.writestr(name, word[name])
print(out)
