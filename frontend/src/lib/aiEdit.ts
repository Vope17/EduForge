// AI-assisted editing of an output: turns the user's selection into blocks
// the server sends to a model, and applies accepted proposals to the draft.
//
// Selection keys:   readers  sec:<i>            (a section: its title and body)
//                   slides   slide:<i>          (every text box and the script)
//                            el:<i>:<box id>    (one text box)
//                            notes:<i>          (the speaker script)
// Block ids:        sec:<i>:title, sec:<i>:body, el:<i>:<box id>, notes:<i>
import { syncSlideFields } from './slideTheme.ts'
import { plainText } from './markup.ts'
import type { Element, Section } from '../api/types.ts'

type BlockKind = 'title' | 'body' | 'notes' | 'slide_title' | 'slide_text' | 'caption'
type TextField = 'title' | 'body' | 'notes'

/** A piece of the output sent to the model for rewriting. */
export interface Block {
  id: string
  text: string | null
  context: string
  citations: string[]
  kind: BlockKind
  max_chars?: number
}

type Located =
  | { sec: Section; index: number; field: TextField; el?: undefined }
  | { sec: Section; index: number; el: Element; field?: undefined }

const boxKind = (el: Element): BlockKind => el.role === 'title' || el.role === 'display' ? 'slide_title' : el.role === 'caption' ? 'caption' : 'slide_text'

// Roughly how many characters fit a text box: characters per line (CJK
// glyphs are about one font size wide) times lines; Latin text fits ~1.8×.
export function boxCapacity(el: Pick<Element, 'w' | 'h' | 'size'>, points: number, ratio: number, language: string): number {
  const size = el.size || 18
  const height = points / ratio
  const perLine = (el.w * points) / size, lines = (el.h * height) / (size * 1.35)
  const capacity = Math.max(1, Math.floor(perLine) * Math.max(1, Math.floor(lines)))
  return language === 'en' ? Math.round(capacity * 1.8) : capacity
}

// Expands selection keys into block ids, in document order, without duplicates.
export function blockIDs(type: string, sections: Section[], keys: Iterable<string>): string[] {
  const set = new Set(keys), ids: string[] = []
  sections.forEach((sec, i) => {
    if (type !== 'B') {
      if (set.has(`sec:${i}`)) ids.push(`sec:${i}:title`, `sec:${i}:body`)
      return
    }
    const whole = set.has(`slide:${i}`)
    for (const el of sec.layout || []) {
      if (el.kind === 'text' && (whole || set.has(`el:${i}:${el.id}`))) ids.push(`el:${i}:${el.id}`)
    }
    if (whole || set.has(`notes:${i}`)) ids.push(`notes:${i}`)
  })
  return ids
}

function locate(_type: string, sections: Section[], id: string): Located | null {
  const [kind, index, rest] = id.split(':')
  const sec = sections[Number(index)]
  if (!sec) return null
  if (kind === 'sec') return { sec, index: Number(index), field: rest as TextField }
  if (kind === 'notes') return { sec, index: Number(index), field: 'notes' }
  const el = (sec.layout || []).find(x => x.id === id.slice(`el:${index}:`.length))
  return el ? { sec, index: Number(index), el } : null
}

// The current text of a block, or null when it no longer exists.
export function blockText(type: string, sections: Section[], id: string): string | null {
  const at = locate(type, sections, id)
  if (!at) return null
  return at.el ? (at.el.text || '') : (at.sec[at.field] || '')
}

// Blocks to send for the given selection.
export function selectionBlocks(
  type: string, sections: Section[], keys: Iterable<string>,
  { points = 960, ratio = 16 / 9, language = 'zh-TW' }: { points?: number; ratio?: number; language?: string } = {},
): Block[] {
  return blockIDs(type, sections, keys).map(id => {
    // blockIDs only returns ids that exist in sections.
    const at = locate(type, sections, id)!
    const context = plainText(at.sec.title || '').slice(0, 200)
    const base = { id, text: blockText(type, sections, id), context, citations: at.sec.citations || [] }
    if (at.el) return { ...base, kind: boxKind(at.el), max_chars: boxCapacity(at.el, points, ratio, language) }
    if (at.field === 'notes') return { ...base, kind: 'notes' }
    return { ...base, kind: at.field === 'title' ? 'title' : 'body' }
  })
}

// Writes an accepted proposal into the draft.
export function applyBlock(type: string, sections: Section[], id: string, text: string): boolean {
  const at = locate(type, sections, id)
  if (!at) return false
  if (at.el) { at.el.text = text; syncSlideFields(at.sec) }
  else at.sec[at.field] = at.field === 'title' ? text.replace(/\s*\n\s*/g, ' ') : text
  return true
}

// A short human label for a block id, e.g. "II · 內文" or "3 · 講稿".
export function blockLabel(type: string, sections: Section[], id: string, t: (key: string) => string): string {
  const at = locate(type, sections, id)
  if (!at) return id
  const n = String(at.index + 1)
  if (type !== 'B') return t(at.field === 'title' ? 'blockTitle' : 'blockBody').replace('{n}', n)
  if (at.field === 'notes') return t('blockNotes').replace('{n}', n)
  const kind = at.el ? boxKind(at.el) : 'slide_text'
  return t(kind === 'slide_title' ? 'blockSlideTitle' : 'blockSlideText').replace('{n}', n)
}
