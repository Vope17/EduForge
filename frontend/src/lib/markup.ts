import type { Image, Section } from '../api/types.ts'

// Mirrors internal/artifacts/markup.go PlainText: drop the Markdown subset
// (**bold**, # headings, - bullets) that model output may contain.
const CITE = String.raw`(?:[0-9a-f]{32}:)?(?:p|s|page|slide)\s*\d+`
const BRACKETED_CITES = new RegExp(String.raw`[〔【\[（(]\s*${CITE}(?:\s*[,，、;；]\s*${CITE})*\s*[〕】\]）)]`, 'g')

// Mirrors domain.StripCitationMarkers: citations are shown separately, so
// inline markers such as 〔<unit id>:p1〕 or 【p2】 are dropped from prose.
export function stripCitations(text: string = ''): string {
  return String(text).replace(BRACKETED_CITES, '').replace(/\b[0-9a-f]{32}:[ps]\d+\b/g, '')
    .replace(/[ \t]+([。，、；：！？.,;:!?)）])/g, '$1').replace(/[ \t]+(\n|$)/g, '$1')
}

export function plainText(text: string = ''): string {
  return stripCitations(text).split('\n').map(line => {
    let m: RegExpMatchArray | null = line.match(/^\s*#{1,6}\s+(.+?)\s*#*\s*$/) || line.match(/^\s*\*\*(.+?)\*\*\s*[:：]?\s*$/)
    if (m) line = m[1]!
    else if ((m = line.match(/^(\s*)[-*•]\s+(.*)$/))) line = m[1] + '• ' + m[2]
    return line.replace(/`([^`\n]+)`/g, '$1').replace(/\*\*([^*\n]+?)\*\*/g, '$1')
  }).join('\n')
}

// ---- Word-style editing: stored markup <-> editable HTML ----
// Stored body text uses the same subset the exporters render:
//   **Subheading** on its own line, "- " bullets, inline **bold**.

const esc = (s: unknown) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const escAttr = (s: unknown) => esc(s).replace(/"/g, '&quot;')
const bold = (s: string) => esc(s).replace(/\*\*([^*\n]+?)\*\*/g, '<b>$1</b>')

// ---- Formulas (mirrors artifacts/math.go) ----
// Inline TeX between $…$ or \(…\); a line that is only $$…$$ or \[…\] is a
// display formula. A "$" followed by a digit after the closing one is a
// price, not a formula, and \$ is a literal dollar sign.
const DISPLAY_LINE = /^\s*(?:\$\$([\s\S]+?)\$\$|\\\[([\s\S]+?)\\\])\s*$/
const INLINE_MATH = /\\\((.+?)\\\)|\$([^\s$](?:[^$]*[^\s$\\])?)\$/g

export function displayFormula(line: string): string | null {
  const m = String(line).match(DISPLAY_LINE)
  const tex = m ? (m[1] || m[2] || '').trim() : ''
  return tex || null
}

/** A piece of a line: plain text, or an inline formula's TeX. */
export type MathPiece = { text: string; tex?: undefined } | { tex: string; text?: undefined }

// splitMath splits a line into { text } and { tex } pieces.
export function splitMath(line: string): MathPiece[] {
  line = String(line)
  const out: MathPiece[] = []
  let last = 0
  for (const m of line.matchAll(INLINE_MATH)) {
    const start = m.index!, end = start + m[0].length
    if (start > 0 && line[start - 1] === '\\') continue
    if (/[0-9]/.test(line[end] || '')) continue
    if (start > last) out.push({ text: line.slice(last, start) })
    out.push({ tex: (m[1] ?? m[2] ?? '').trim() })
    last = end
  }
  if (last < line.length) out.push({ text: line.slice(last) })
  return out.map(p => (p.tex === undefined ? { text: p.text.replace(/\\\$/g, '$') } : p))
}

// A formula chip shows its TeX until the rendered SVG replaces it (hydrate).
export const formulaChip = (tex: string) => `<span class="math-chip" contenteditable="false" data-tex="${escAttr(tex)}">${esc('$' + tex + '$')}</span>`
const blockChip = (tex: string) => `<div class="math-block" contenteditable="false" data-tex="${escAttr(tex)}" data-display="1">${esc('$$' + tex + '$$')}</div>`
const inline = (s: string) => splitMath(s).map(p => (p.tex !== undefined ? formulaChip(p.tex) : bold(p.text))).join('')

export function markupToHtml(markup: string = ''): string {
  const out: string[] = []
  let list: string[] | null = null
  for (const raw of stripCitations(markup).split('\n')) {
    const display = displayFormula(raw)
    if (display) {
      if (list) { out.push(`<ul>${list.join('')}</ul>`); list = null }
      out.push(blockChip(display))
      continue
    }
    const heading = raw.match(/^\s*#{1,6}\s+(.+?)\s*#*\s*$/) || raw.match(/^\s*\*\*(.+?)\*\*\s*[:：]?\s*$/)
    const bullet = raw.match(/^\s*[-*•]\s+(.*)$/)
    if (bullet) { (list ||= []).push(`<li>${inline(bullet[1])}</li>`); continue }
    if (list) { out.push(`<ul>${list.join('')}</ul>`); list = null }
    if (heading) out.push(`<h3>${esc(heading[1])}</h3>`)
    else out.push(raw.trim() ? `<div>${inline(raw)}</div>` : '<div><br></div>')
  }
  if (list) out.push(`<ul>${list.join('')}</ul>`)
  return out.join('')
}

// A formula chip or block (see formulaChip) is an element carrying data-tex.
// Checked structurally so this also works on documents from other realms.
const texOf = (node: Node): string | undefined => (node.nodeType === 1 ? (node as HTMLElement).dataset?.tex : undefined)

function inlineMarkup(node: { childNodes: Iterable<ChildNode> }): string {
  let s = ''
  for (const child of node.childNodes) {
    const tex = texOf(child)
    if (tex !== undefined) s += (child as HTMLElement).dataset.display === '1' ? `$$${tex}$$` : `$${tex}$`
    else if (child.nodeType === 3) s += child.textContent
    else if (child.nodeName === 'BR') s += '\n'
    else if (['B', 'STRONG'].includes(child.nodeName) || ((child as HTMLElement).style && Number((child as HTMLElement).style.fontWeight) >= 600)) {
      const text = inlineMarkup(child)
      s += text.trim() ? `**${text.trim()}**` : text
    } else s += inlineMarkup(child)
  }
  return s
}

export function htmlToMarkup(html: string = ''): string {
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
  const lines: string[] = []
  let pending = ''
  const flush = () => { if (pending !== '') { lines.push(...pending.split('\n')); pending = '' } }
  for (const node of doc.body.childNodes) {
    const name = node.nodeName
    if (node.nodeType === 3 || ['B', 'STRONG', 'SPAN', 'I', 'EM', 'BR'].includes(name)) {
      pending += node.nodeType === 3 ? node.textContent : name === 'BR' ? '\n' : inlineMarkup({ childNodes: [node] })
      continue
    }
    flush()
    const tex = texOf(node)
    if (tex !== undefined && (node as HTMLElement).dataset.display === '1') lines.push(`$$${tex}$$`)
    else if (/^H[1-6]$/.test(name)) { const t = (node.textContent ?? '').trim(); if (t) lines.push(`**${t}**`) }
    else if (name === 'UL' || name === 'OL') for (const li of (node as Element).querySelectorAll('li')) lines.push(`- ${inlineMarkup(li).trim()}`)
    else { const t = inlineMarkup(node); lines.push(...(t === '\n' ? [''] : t.replace(/\n$/, '').split('\n'))) }
  }
  flush()
  return lines.join('\n').replace(/\n{3,}/g, '\n\n').trim()
}

// Mirrors artifacts.SectionFigures: explicit lists win, otherwise up to three
// unused figures from the pages a section cites.
export function sectionFigures(sections: Section[], images: Image[]): string[][] {
  const used = new Set<string>()
  return sections.map(sec => {
    if (Array.isArray(sec.figures)) { sec.figures.forEach(p => used.add(p)); return [...sec.figures] }
    const picks: string[] = []
    if (sec.image && !used.has(sec.image)) { picks.push(sec.image); used.add(sec.image) }
    for (const img of images) {
      if (picks.length >= 3) break
      if ((sec.citations || []).includes(img.citation) && !used.has(img.path)) { picks.push(img.path); used.add(img.path) }
    }
    return picks
  })
}

// Reader sections are numbered I., II., … (see artifacts.Roman in Go).
export function roman(n: number): string {
  const table: [number, string][] = [[1000, 'M'], [900, 'CM'], [500, 'D'], [400, 'CD'], [100, 'C'], [90, 'XC'], [50, 'L'], [40, 'XL'], [10, 'X'], [9, 'IX'], [5, 'V'], [4, 'IV'], [1, 'I']]
  let out = ''
  for (const [v, s] of table) while (n >= v) { out += s; n -= v }
  return out
}

// A heading without numbering the model may have written (see artifacts.SectionTitle).
const LEADING_NUMBER = /^\s*(第?[一二三四五六七八九十]+[、.．章節]\s*|[IVXLC]+[.、．]\s*|\d+(\.\d+)*[.、．)]?\s+|\(\d+\)\s*)/
export function sectionTitle(title: string | undefined): string { return plainText(title || '').replace(LEADING_NUMBER, '').trim() }

// ---- Figures inside a reader section (mirrors artifacts.PlaceFigures) ----
const FIGURE_LABEL = /^\s*(fig(?:ure)?\.?|圖|table|表)\s*(\d+)/i
const FIGURE_MENTION = /(fig(?:ure)?s?\.?|圖|tables?|表)\s*(\d+)/gi
const figureKey = (word: string, n: string) => (/^tab/i.test(word) || word === '表' ? 'table ' : 'fig ') + n
const isHeading = (line: string) => /^\s*#{1,6}\s+(.+?)\s*#*\s*$/.test(line) || /^\s*\*\*(.+?)\*\*\s*[:：]?\s*$/.test(line)
const trimBlankLines = (text: string) => text.replace(/^(?:[ \t]*\n)+/, '').replace(/(?:\n[ \t]*)+$/, '')

// Splits a section body into blocks of text, each followed by the figures
// shown under it: a figure goes right after the paragraph that first
// mentions it ("Figure 3", "圖 3", "Table 2"); the others are spread evenly
// between paragraphs. captions maps a figure path to its caption.
/** Text of a reader section followed by the figures shown under it. */
export interface FigureBlock {
  text: string
  figures: string[]
}

export function figureBlocks(body: string = '', figures: string[] = [], captions: Record<string, string> = {}): FigureBlock[] {
  const lines = String(body).split('\n')
  const after = lines.map((): string[] => [])
  if (figures.length) {
    const ends: number[] = []
    lines.forEach((line, i) => {
      if (!line.trim() || isHeading(line)) return
      const next = lines[i + 1]
      if (next === undefined || !next.trim() || isHeading(next)) ends.push(i)
    })
    if (!ends.length) ends.push(lines.length - 1)
    const endOf = (line: number) => ends.find(e => e >= line) ?? ends[ends.length - 1]!
    const first: Record<string, number> = {}
    lines.forEach((line, i) => {
      for (const m of line.matchAll(FIGURE_MENTION)) {
        const key = figureKey(m[1]!, m[2]!)
        if (!(key in first)) first[key] = i
      }
    })
    const unplaced: string[] = []
    for (const path of figures) {
      const m = String(captions[path] || '').match(FIGURE_LABEL)
      const key = m && figureKey(m[1]!, m[2]!)
      if (key && key in first) after[endOf(first[key]!)]!.push(path)
      else unplaced.push(path)
    }
    unplaced.forEach((path, j) => {
      let at = ends[Math.min(ends.length - 1, Math.floor((j + 1) * ends.length / (unplaced.length + 1)))]
      if (unplaced.length >= ends.length) at = ends[Math.min(ends.length - 1, j)]
      after[at!]!.push(path)
    })
  }
  const blocks: FigureBlock[] = []
  let start = 0
  lines.forEach((_, j) => {
    if (!after[j]!.length && j < lines.length - 1) return
    blocks.push({ text: trimBlankLines(lines.slice(start, j + 1).join('\n')), figures: after[j]! })
    start = j + 1
  })
  return blocks
}

// Joins edited blocks back into one body, paragraphs apart.
export function joinBlocks(blocks: { text?: string }[]): string {
  return blocks.map(b => trimBlankLines(b.text || '')).filter(t => t.trim()).join('\n\n')
}
