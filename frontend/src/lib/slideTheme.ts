// Slide looks, mirrored from internal/artifacts/themes.go so the editor and
// template previews look like the exported PPTX. With an imported PowerPoint
// template, the rendered layout backgrounds stand in for its masters.

import type { Element, Section, Style, Template } from '../api/types.ts'

const hex = (value: unknown): string | null => typeof value === 'string' && /^[0-9a-f]{6}$/i.test(value) ? value.toUpperCase() : null
const str = (value: unknown): string => (typeof value === 'string' ? value : '')
const DECORS = ['bar', 'line', 'rule', 'band']

/** A slide's resolved look; colours are hex without '#'. */
export interface Theme {
  bg: string
  fg: string
  accent: string
  decor: string
  template: boolean
  muted: string
  line: string
  title: string
  font: string
  /** Slide width in points. */
  points: number
  /** CSS aspect-ratio. */
  ratio: string
  coverImage: string
  contentImage: string
}

/** A decoration rectangle: x, y, width, height (fractions) and colour. */
export type Rect = [number, number, number, number, string]
export type SlideKind = 'display' | 'content'

function mix(a: string, b: string, f: number): string {
  let out = ''
  for (let i = 0; i < 6; i += 2) {
    const v = Math.round(parseInt(a.slice(i, i + 2), 16) * (1 - f) + parseInt(b.slice(i, i + 2), 16) * f)
    out += v.toString(16).padStart(2, '0').toUpperCase()
  }
  return out
}

export function themeOf(style: Style = {}): Theme {
  const bg = hex(style.background) || 'FFFFFF', fg = hex(style.foreground) || '1F2D3A', accent = hex(style.accent) || '0F7B6C'
  const template = !!style.template_file
  const decor = template ? 'none' : DECORS.includes(str(style.decor)) ? str(style.decor) : 'bar'
  const four = style.ratio === '4:3'
  const width = Number(style.slide_width) || 0, height = Number(style.slide_height) || 0
  const sized = template && width > 0 && height > 0
  return {
    bg, fg, accent, decor, template,
    muted: mix(fg, bg, 0.45), line: mix(fg, bg, 0.85),
    title: template ? (hex(style.title_color) || fg) : decor === 'band' ? (hex(style.title_color) || 'FFFFFF') : (hex(style.title_color) || fg),
    font: str(style.font) || 'Noto Sans CJK TC',
    points: sized ? width / 12700 : four ? 720 : 960,
    ratio: sized ? `${width} / ${height}` : four ? '4 / 3' : '16 / 9',
    coverImage: str(style.cover_image), contentImage: str(style.content_image),
  }
}

// The style an imported template applies (see artifacts.TemplateStyle).
export function templateStyle(data: Partial<Template> = {}): Style {
  if (!data.file || data.kind === 'docx') return {}
  return {
    template_file: data.file, background: data.background, foreground: data.foreground, accent: data.accent, title_color: data.title_color,
    font: data.font, ratio: data.ratio, slide_width: data.width, slide_height: data.height,
    title_box: data.title_box, body_box: data.body_box, cover_title_box: data.cover_title_box, cover_subtitle_box: data.cover_subtitle_box,
    title_align: data.title_align, cover_align: data.cover_align, cover_image: data.cover_image, content_image: data.content_image,
  }
}

// The template's rendered layout as the slide background, when there is one.
export function backgroundStyle(t: Theme, kind: SlideKind, url?: (path: string) => string): Record<string, string> {
  const path = kind === 'display' ? t.coverImage : t.contentImage
  return path && url ? { backgroundImage: `url("${url(path)}")`, backgroundSize: '100% 100%' } : {}
}

// Decks made before display boxes existed placed the cover title mid-slide.
export const slideKind = (slide: Pick<Partial<Section>, 'layout'> | undefined): SlideKind => (slide?.layout || []).some(el => el.role === 'display' || (el.role === 'title' && el.y >= 0.25)) ? 'display' : 'content'

export function decorations(t: Theme, kind: SlideKind): Rect[] {
  if (t.decor === 'none') return []
  if (kind === 'display') return t.decor === 'band' ? [[0, 0.80, 1, 0.20, t.accent]] : [[0, 0, 0.025, 1, t.accent]]
  switch (t.decor) {
    case 'band': return [[0, 0, 1, 0.19, t.accent]]
    case 'line': return [[0.06, 0.175, 0.07, 0.008, t.accent]]
    case 'rule': return [[0.06, 0.18, 0.88, 0.003, t.accent], [0.06, 0.93, 0.88, 0.0015, t.line]]
    default: return [[0.04, 0.065, 0.007, 0.09, t.accent], [0.06, 0.195, 0.88, 0.002, t.line]]
  }
}

export function textColor(t: Theme, role: string | undefined, kind: SlideKind): string {
  if (role === 'title') return kind === 'content' || t.template ? t.title : t.fg
  if (role === 'display') return t.template ? t.title : t.fg
  if (role === 'caption' || role === 'subtitle') return t.muted
  if (role === 'kicker') return t.accent
  return t.fg
}

const pct = (v: number) => `${(v * 100).toFixed(3)}%`
export const rectStyle = ([x, y, w, h, c]: Rect) => ({ left: pct(x), top: pct(y), width: pct(w), height: pct(h), background: '#' + c })
export const boxStyle = (el: Pick<Element, 'x' | 'y' | 'w' | 'h'>) => ({ left: pct(el.x), top: pct(el.y), width: pct(el.w), height: pct(el.h) })

// Sizes are points on a slide 960pt (16:9) or 720pt (4:3) wide; the surface is
// a size container, so cqw scales them with the rendered slide.
export const cqw = (pt: number, t: Theme) => `${(pt / t.points * 100).toFixed(3)}cqw`

export function surfaceStyle(t: Theme): Record<string, string> {
  return { backgroundColor: '#' + t.bg, color: '#' + t.fg, fontFamily: `'${t.font}', 'Noto Sans CJK TC', sans-serif`, aspectRatio: t.ratio, '--accent': '#' + t.accent, '--muted': '#' + t.muted }
}

export function textStyle(el: Element, t: Theme, kind: SlideKind): Record<string, string | number> {
  const align = el.align || (el.role === 'caption' ? 'center' : 'left')
  return {
    fontSize: cqw(el.size || 18, t), fontWeight: el.bold ? 700 : 400, textAlign: align,
    color: '#' + textColor(t, el.role, kind), padding: `${cqw(3.6, t)} ${cqw(7.2, t)}`,
    justifyContent: el.role === 'title' ? 'center' : 'flex-start',
  }
}

// Lines starting with "• " or "- " are bullets, as in the PPTX export.
export function textLines(text: string | undefined): { bullet: boolean; text: string }[] {
  return String(text || '').split('\n').map(line => {
    const m = line.trim().match(/^(?:• |- |・)\s*(.*)$/)
    return m ? { bullet: true, text: m[1]! } : { bullet: false, text: line }
  })
}

// Box geometry shared with BuildDeck/DefaultLayout in Go.
export const TITLE_BOX = { x: 0.06, y: 0.035, w: 0.88, h: 0.12 }

export function defaultLayout(slide: Section, plainText: (text: string) => string): Element[] {
  const image = slide.image || (Array.isArray(slide.figures) ? slide.figures[0] : '')
  const layout: Element[] = [{ id: 'title', kind: 'text', role: 'title', ...TITLE_BOX, text: plainText(slide.title), size: 28, bold: true }]
  if (image) {
    layout.push({ id: 'figure', kind: 'image', role: 'figure', x: 0.06, y: 0.23, w: 0.55, h: 0.68, path: image })
    layout.push({ id: 'points', kind: 'text', role: 'body', x: 0.645, y: 0.26, w: 0.295, h: 0.62, text: plainText(slide.body), size: 20 })
  } else {
    layout.push({ id: 'points', kind: 'text', role: 'body', x: 0.10, y: 0.27, w: 0.80, h: 0.62, text: plainText(slide.body), size: 22 })
  }
  return layout
}

// Keeps a slide's own fields (title, body, figures) in step with its boxes,
// for lists, checks and exports.
export function syncSlideFields(s: Section) {
  // Only slides with boxes are synced; layout is set for them.
  const layout = s.layout!
  const texts = layout.filter(el => el.kind === 'text')
  const title = texts.find(el => el.role === 'title' || el.role === 'display')
  s.title = title ? (title.text ?? '') : (s.title || '')
  s.body = texts.filter(el => el !== title && el.role !== 'kicker').map(el => el.text).join('\n')
  s.figures = layout.filter(el => el.kind === 'image' && el.path).map(el => el.path!)
}
