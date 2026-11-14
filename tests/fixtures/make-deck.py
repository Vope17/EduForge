"""Writes two ordinary presentations (not templates) to extract templates from.

tests/fixtures/sample-deck.pptx: a cover slide with a teal band, and three
content slides that repeat a coral top bar, a footer text and a logo picture
around slide-specific titles and text.

tests/fixtures/flat-deck.pptx: a deck drawn entirely on its slides, as decks
written by code often are: one layout, the Office theme untouched, and every
title, colour and font set on the slide's own text boxes. Content slides
repeat a section label that changes (MOTIVATION, DESIGN, …), a title and lede
in fixed places, a source line and a page counter; a blue side bar is the
only repeated design; two slides share a content card.

Run: python3 tests/fixtures/make-deck.py
"""
import os
import struct
import zipfile
import zlib

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


def xfrm(box):
    return f'<a:xfrm><a:off x="{box[0]}" y="{box[1]}"/><a:ext cx="{box[2]}" cy="{box[3]}"/></a:xfrm>'


def para(text):
    return f'<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t>{text}</a:t></a:r></a:p></p:txBody>'


def ph(n, typ, idx="", box=None, text=""):
    attrs = (f' type="{typ}"' if typ else "") + (f' idx="{idx}"' if idx else "")
    body = para(text) if text else '<p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr lang="en-US"/></a:p></p:txBody>'
    return f'<p:sp><p:nvSpPr><p:cNvPr id="{n}" name="ph{n}"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph{attrs}/></p:nvPr></p:nvSpPr><p:spPr>{xfrm(box) if box else ""}</p:spPr>{body}</p:sp>'


def rect(n, name, box, color):
    return f'<p:sp><p:nvSpPr><p:cNvPr id="{n}" name="{name}"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr>{xfrm(box)}<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="{color}"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr></p:sp>'


def textbox(n, name, box, text):
    return f'<p:sp><p:nvSpPr><p:cNvPr id="{n}" name="{name}"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr>{xfrm(box)}<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>{para(text)}</p:sp>'


def picture(n, name, box, rid):
    return f'<p:pic><p:nvPicPr><p:cNvPr id="{n}" name="{name}"><a:hlinkClick r:id="rId9"/></p:cNvPr><p:cNvPicPr><a:picLocks noChangeAspect="1"/></p:cNvPicPr><p:nvPr/></p:nvPicPr><p:blipFill><a:blip r:embed="{rid}"/><a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr>{xfrm(box)}<a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr></p:pic>'


def png(width, height, rgb):
    raw = b"".join(b"\x00" + bytes(rgb) * width for _ in range(height))
    chunk = lambda kind, data: struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")


def slide(shapes):
    return HEAD + f'<p:sld {NS}><p:cSld><p:spTree>{GROUP}{shapes}</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>'


BAR = (0, 0, W, 300000)
FOOTER = (609600, 6324600, 6096000, 400000)
LOGO = (11049000, 6248400, 609600, 457200)
contents = [
    ("Motivation", "Why the task matters"),
    ("Method", "A two-stage network"),
    ("Results", "Accuracy improves on all benchmarks"),
]
clrmap = '<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>'
parts = {
    "_rels/.rels": rels(("rId1", "officeDocument", "ppt/presentation.xml")),
    "ppt/presentation.xml": HEAD + f'<p:presentation {NS}><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>'
    + "".join(f'<p:sldId id="{256 + i}" r:id="rId{3 + i}"/>' for i in range(4)) + f'</p:sldIdLst><p:sldSz cx="{W}" cy="{H}"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>',
    "ppt/_rels/presentation.xml.rels": rels(("rId1", "slideMaster", "slideMasters/slideMaster1.xml"), ("rId2", "theme", "theme/theme1.xml"), *[(f"rId{3 + i}", "slide", f"slides/slide{i + 1}.xml") for i in range(4)]),
    "ppt/slideMasters/slideMaster1.xml": HEAD + f'<p:sldMaster {NS}><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>{GROUP}'
    + ph(2, "title", "", (609600, 457200, 10972800, 1143000)) + ph(3, "body", "1", (609600, 1700000, 10972800, 4300000))
    + f'</p:spTree></p:cSld>{clrmap}<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/><p:sldLayoutId id="2147483650" r:id="rId2"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle><a:lvl1pPr><a:defRPr sz="3600" b="1"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/></a:defRPr></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr><a:defRPr sz="2200"/></a:lvl1pPr></p:bodyStyle><p:otherStyle/></p:txStyles></p:sldMaster>',
    "ppt/slideMasters/_rels/slideMaster1.xml.rels": rels(("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"), ("rId2", "slideLayout", "../slideLayouts/slideLayout2.xml"), ("rId3", "theme", "../theme/theme1.xml")),
    "ppt/slideLayouts/slideLayout1.xml": HEAD + f'<p:sldLayout {NS} type="title" preserve="1"><p:cSld name="Title Slide"><p:spTree>{GROUP}'
    + ph(2, "ctrTitle", "", (1524000, 2000000, 9144000, 1500000)) + ph(3, "subTitle", "1", (1524000, 3700000, 9144000, 800000))
    + '</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>',
    "ppt/slideLayouts/_rels/slideLayout1.xml.rels": rels(("rId1", "slideMaster", "../slideMasters/slideMaster1.xml")),
    "ppt/slideLayouts/slideLayout2.xml": HEAD + f'<p:sldLayout {NS} type="obj" preserve="1"><p:cSld name="Title and Content"><p:spTree>{GROUP}'
    + ph(2, "title") + ph(3, "", "1")
    + '</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>',
    "ppt/slideLayouts/_rels/slideLayout2.xml.rels": rels(("rId1", "slideMaster", "../slideMasters/slideMaster1.xml")),
    "ppt/theme/theme1.xml": HEAD + f'<a:theme xmlns:a="{A}" name="Sample deck"><a:themeElements><a:clrScheme name="Deck"><a:dk1><a:srgbClr val="1B1B1E"/></a:dk1><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="0E3B43"/></a:dk2><a:lt2><a:srgbClr val="EEF2F2"/></a:lt2><a:accent1><a:srgbClr val="0E7C7B"/></a:accent1><a:accent2><a:srgbClr val="F25F5C"/></a:accent2><a:accent3><a:srgbClr val="FFE066"/></a:accent3><a:accent4><a:srgbClr val="247BA0"/></a:accent4><a:accent5><a:srgbClr val="70C1B3"/></a:accent5><a:accent6><a:srgbClr val="50514F"/></a:accent6><a:hlink><a:srgbClr val="247BA0"/></a:hlink><a:folHlink><a:srgbClr val="70C1B3"/></a:folHlink></a:clrScheme><a:fontScheme name="Deck"><a:majorFont><a:latin typeface="DejaVu Sans"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="DejaVu Sans"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme><a:fmtScheme name="Deck"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="25400"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="38100"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>',
    "ppt/media/logo.png": png(16, 12, (14, 124, 123)),
    "ppt/slides/slide1.xml": slide(rect(4, "Cover band", (0, 0, 914400, H), "0E7C7B") + ph(2, "ctrTitle", text="Two-stage networks for vision") + ph(3, "subTitle", "1", text="Lab meeting")),
    "ppt/slides/_rels/slide1.xml.rels": rels(("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml")),
}
for i, (title, body) in enumerate(contents):
    n = i + 2
    parts[f"ppt/slides/slide{n}.xml"] = slide(
        rect(4, "Top bar", BAR, "F25F5C") + ph(2, "title", text=title)
        + textbox(5, "Content", (609600, 1700000, 10972800, 4000000), body)
        + textbox(6, "Footer", FOOTER, "Vision Lab · 2026") + picture(7, "Logo", LOGO, "rId2"))
    parts[f"ppt/slides/_rels/slide{n}.xml.rels"] = rels(("rId1", "slideLayout", "../slideLayouts/slideLayout2.xml"), ("rId2", "image", "../media/logo.png")) \
        .replace("</Relationships>", f'<Relationship Id="rId9" Type="{R}/hyperlink" Target="https://example.org" TargetMode="External"/></Relationships>')
parts["[Content_Types].xml"] = HEAD + '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>' \
    + "".join(f'<Override PartName="/{name}" ContentType="application/vnd.openxmlformats-officedocument.{typ}+xml"/>' for name, typ in [
        ("ppt/presentation.xml", "presentationml.presentation.main"), ("ppt/slideMasters/slideMaster1.xml", "presentationml.slideMaster"),
        ("ppt/slideLayouts/slideLayout1.xml", "presentationml.slideLayout"), ("ppt/slideLayouts/slideLayout2.xml", "presentationml.slideLayout"),
        ("ppt/theme/theme1.xml", "theme"), *[(f"ppt/slides/slide{i + 1}.xml", "presentationml.slide") for i in range(4)]]) + "</Types>"

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "sample-deck.pptx")
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for name in ["[Content_Types].xml"] + sorted(n for n in parts if n != "[Content_Types].xml"):
        data = parts[name]
        z.writestr(name, data if isinstance(data, bytes) else data.encode())
print(out)


# ---- flat deck ----

def emu(box):
    return tuple(int(round(v * d)) for v, d in zip(box, (W, H, W, H)))


def run_text(n, name, box, text, size, color, font="Arial", bold=False, ea="Noto Sans CJK TC"):
    rpr = f'<a:rPr lang="en-US" sz="{int(size * 100)}"{" b=\"1\"" if bold else ""}><a:solidFill><a:srgbClr val="{color}"/></a:solidFill><a:latin typeface="{font}"/><a:ea typeface="{ea}"/></a:rPr>'
    body = f'<p:txBody><a:bodyPr wrap="square"/><a:lstStyle/><a:p><a:r>{rpr}<a:t>{text}</a:t></a:r></a:p></p:txBody>'
    return f'<p:sp><p:nvSpPr><p:cNvPr id="{n}" name="{name}"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr>{xfrm(emu(box))}<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr>{body}</p:sp>'


def flat_rect(n, name, box, color):
    return rect(n, name, emu(box), color)


TOTAL = 6
flat_slides = [
    run_text(2, "Kicker", (0.052, 0.087, 0.165, 0.033), "PAPER REVIEW", 10, "2563EB", bold=True)
    + run_text(3, "Name", (0.052, 0.153, 0.24, 0.096), "FLAT", 40, "111827", font="Georgia", bold=True)
    + run_text(4, "Paper title", (0.052, 0.26, 0.907, 0.147), "Measuring design from slides", 24, "111827", font="Georgia")
    + run_text(5, "Authors", (0.052, 0.409, 0.907, 0.077), "A. Author, B. Author", 12.5, "667085")
    + run_text(6, "Venue", (0.05, 0.486, 0.45, 0.04), "Conference 2026", 12, "344054")
    + run_text(7, "Source", (0.045, 0.917, 0.667, 0.023), "Primary source: the paper", 8.5, "98A2B3")
    + run_text(8, "Page", (0.889, 0.956, 0.071, 0.024), f"1 / {TOTAL}", 8.5, "98A2B3"),
]
sections = ["MOTIVATION", "MOTIVATION", "DESIGN", "DESIGN", "EVALUATION"]
for i, section in enumerate(sections):
    n = i + 2
    shapes = flat_rect(2, "Side bar", (0, 0, 0.012, 1), "2563EB")
    shapes += run_text(3, "Section", (0.045, 0.043, 0.18, 0.027), section, 9.5, "2563EB", bold=True)
    shapes += run_text(4, "Title", (0.045, 0.083, 0.904, 0.069), f"Slide {n} headline", 27, "111827", font="Georgia", bold=True)
    shapes += run_text(5, "Lede", (0.045, 0.151, 0.904, 0.037), f"What slide {n} shows", 12.5, "667085")
    if n in (2, 3):
        shapes += flat_rect(6, "Card", (0.05, 0.216, 0.454, 0.414), "EFF6FF")
    shapes += run_text(7, "Body", (0.06, 0.3, 0.8, 0.4), f"Point for slide {n}", 18, "111827")
    shapes += run_text(8, "Source", (0.045, 0.917, 0.667, 0.023), f"Source: figure {n}", 8.5, "98A2B3")
    shapes += run_text(9, "Page", (0.889, 0.956, 0.071, 0.024), f"{n} / {TOTAL}", 8.5, "98A2B3")
    flat_slides.append(shapes)

flat = {
    "_rels/.rels": rels(("rId1", "officeDocument", "ppt/presentation.xml")),
    "ppt/presentation.xml": HEAD + f'<p:presentation {NS}><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>'
    + "".join(f'<p:sldId id="{256 + i}" r:id="rId{3 + i}"/>' for i in range(TOTAL)) + f'</p:sldIdLst><p:sldSz cx="{W}" cy="{H}"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>',
    "ppt/_rels/presentation.xml.rels": rels(("rId1", "slideMaster", "slideMasters/slideMaster1.xml"), ("rId2", "theme", "theme/theme1.xml"), *[(f"rId{3 + i}", "slide", f"slides/slide{i + 1}.xml") for i in range(TOTAL)]),
    "ppt/slideMasters/slideMaster1.xml": HEAD + f'<p:sldMaster {NS}><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="F8FAFC"/></a:solidFill><a:effectLst/></p:bgPr></p:bg><p:spTree>{GROUP}'
    + ph(2, "title", "", (838200, 365125, 10515600, 1325563)) + ph(3, "body", "1", (838200, 1825625, 10515600, 4351338))
    + f'</p:spTree></p:cSld>{clrmap}<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst><p:txStyles><p:titleStyle><a:lvl1pPr><a:defRPr sz="4400"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/></a:defRPr></a:lvl1pPr></p:titleStyle><p:bodyStyle><a:lvl1pPr><a:defRPr sz="2800"/></a:lvl1pPr></p:bodyStyle><p:otherStyle/></p:txStyles></p:sldMaster>',
    "ppt/slideMasters/_rels/slideMaster1.xml.rels": rels(("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"), ("rId2", "theme", "../theme/theme1.xml")),
    "ppt/slideLayouts/slideLayout1.xml": HEAD + f'<p:sldLayout {NS} type="obj" preserve="1"><p:cSld name="Title and Content"><p:spTree>{GROUP}'
    + ph(2, "title") + ph(3, "", "1") + ph(4, "sldNum", "12", (8610600, 6356350, 2743200, 365125))
    + '</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>',
    "ppt/slideLayouts/_rels/slideLayout1.xml.rels": rels(("rId1", "slideMaster", "../slideMasters/slideMaster1.xml")),
    "ppt/theme/theme1.xml": HEAD + f'<a:theme xmlns:a="{A}" name="Office Theme"><a:themeElements><a:clrScheme name="Office"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="44546A"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2><a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4><a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6><a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme><a:fontScheme name="Office"><a:majorFont><a:latin typeface="Aptos Display"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Aptos"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>'
    + parts["ppt/theme/theme1.xml"].split("</a:fontScheme>", 1)[1],
}
for i, shapes in enumerate(flat_slides):
    flat[f"ppt/slides/slide{i + 1}.xml"] = slide(shapes)
    flat[f"ppt/slides/_rels/slide{i + 1}.xml.rels"] = rels(("rId1", "slideLayout", "../slideLayouts/slideLayout1.xml"))
flat["[Content_Types].xml"] = HEAD + '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>' \
    + "".join(f'<Override PartName="/{name}" ContentType="application/vnd.openxmlformats-officedocument.{typ}+xml"/>' for name, typ in [
        ("ppt/presentation.xml", "presentationml.presentation.main"), ("ppt/slideMasters/slideMaster1.xml", "presentationml.slideMaster"),
        ("ppt/slideLayouts/slideLayout1.xml", "presentationml.slideLayout"),
        ("ppt/theme/theme1.xml", "theme"), *[(f"ppt/slides/slide{i + 1}.xml", "presentationml.slide") for i in range(TOTAL)]]) + "</Types>"

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "flat-deck.pptx")
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for name in ["[Content_Types].xml"] + sorted(n for n in flat if n != "[Content_Types].xml"):
        z.writestr(name, flat[name].encode())
print(out)
