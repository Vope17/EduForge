// Helpers for editing a template's design spec (see artifacts.TemplateSpec).
import type { Margins, Template, TemplateSpec, WordStyleSpec } from '../api/types.ts'

type T = (key: string) => string

export const THEME_SLOTS = ['dk1', 'lt1', 'dk2', 'lt2', 'accent1', 'accent2', 'accent3', 'accent4', 'accent5', 'accent6']
export const WORD_ROLES = ['title', 'subtitle', 'h1', 'h2', 'normal', 'caption']
export const FONT_SUGGESTIONS = [
  'Noto Sans CJK TC', 'Noto Serif CJK TC', '微軟正黑體', 'Microsoft JhengHei', '新細明體', 'PMingLiU', '標楷體', 'DFKai-SB',
  'Calibri', 'Calibri Light', 'Arial', 'Helvetica', 'Georgia', 'Times New Roman', 'Cambria', 'Segoe UI', 'Inter', 'Source Serif 4',
]

export function slotLabel(slot: string, t: T): string {
  const m = slot.match(/^accent(\d)$/)
  return m ? t('tplAccent').replace('{n}', m[1]!) : t('tplSlot_' + slot)
}
export const wordRoleLabel = (role: string, t: T) => t('tplRole_' + role)

const family = (...names: (string | undefined)[]) => names.filter(Boolean).map(n => `'${n}'`).concat(["'Noto Sans CJK TC'", 'sans-serif']).join(', ')

// Colours and fonts for a live slide preview. The template record's derived
// colours (title, text, background) come from theme slots; follow whichever
// slot they matched so editing that slot shows immediately.
/** Colours (hex) and CSS font families for a template preview. */
export interface SpecTheme {
  colors: { bg: string; title: string; text: string; accent: string }
  fonts: { heading: string; body: string }
}

export function specTheme(spec: TemplateSpec, data: Partial<Template>, original: TemplateSpec): SpecTheme {
  const colors = spec.colors ?? {}
  const follow = (value: string | undefined, fallback: string) => {
    const slot = THEME_SLOTS.find(s => original.colors?.[s] && original.colors[s] === value)
    return (slot && colors[slot]) || value || fallback
  }
  return {
    colors: {
      bg: follow(data.background, colors.lt1 || 'FFFFFF'),
      title: follow(data.title_color, colors.dk1 || '000000'),
      text: follow(data.foreground, colors.dk1 || '000000'),
      accent: colors.accent1 || data.accent || '0F7B6C',
    },
    fonts: { heading: family(spec.heading_font, spec.heading_font_ea), body: family(spec.body_font, spec.body_font_ea) },
  }
}

// ---- AI editing: spec keys (see app.specKeys) ----

export const FONT_KEYS = ['heading_font', 'heading_font_ea', 'body_font', 'body_font_ea']
export const BOX_KEYS = ['cover_title_box', 'cover_subtitle_box', 'title_box', 'body_box'] as const satisfies readonly (keyof TemplateSpec)[]
const BOX_LABELS: Record<string, string> = { cover_title_box: 'tplCoverTitle', cover_subtitle_box: 'tplCoverSubtitle', title_box: 'tplTitleArea', body_box: 'tplBodyArea' }
const FONT_LABELS: Record<string, string> = { heading_font: 'tplHeadingFont', heading_font_ea: 'tplHeadingFontEA', body_font: 'tplBodyFont', body_font_ea: 'tplBodyFontEA' }

export function specKeyLabel(key: string, t: T): string {
  if (key.startsWith('colors.')) return slotLabel(key.slice(7), t)
  if (key.startsWith('styles.')) return wordRoleLabel(key.slice(7), t)
  if (key === 'margins') return t('tplMargins')
  if (key === 'header' || key === 'footer') return t('tpl_' + key)
  return t(BOX_LABELS[key] || FONT_LABELS[key] || key)
}

/**
 * A spec key: "colors.<slot>", "styles.<role>", "margins", "header",
 * "footer" or one of FONT_KEYS / BOX_KEYS.
 */
export type SpecValue = string | number[] | WordStyleSpec | Margins | undefined

export function specValue(spec: TemplateSpec, key: string): SpecValue {
  if (key.startsWith('colors.')) return spec.colors?.[key.slice(7)] ?? ''
  if (key.startsWith('styles.')) return spec.styles?.[key.slice(7)]
  if (key === 'header' || key === 'footer') return spec[key]?.text ?? ''
  return spec[key as keyof TemplateSpec] as SpecValue
}

// The spec is expected to hold the part being written (colors, styles,
// header/footer), as the editor loads it from the server.
export function applySpecValue(spec: TemplateSpec, key: string, value: SpecValue) {
  if (key.startsWith('colors.')) spec.colors![key.slice(7)] = value as string
  else if (key.startsWith('styles.')) spec.styles![key.slice(7)] = { ...(value as WordStyleSpec) }
  else if (key === 'margins') spec.margins = { ...(value as Margins) }
  else if (key === 'header' || key === 'footer') spec[key]!.text = value as string
  else (spec as unknown as Record<string, SpecValue>)[key] = Array.isArray(value) ? [...value] : value
}

const canonical = (v: unknown) => JSON.stringify(v, (_, x) => x && typeof x === 'object' && !Array.isArray(x) ? Object.fromEntries(Object.entries(x).sort(([a], [b]) => a.localeCompare(b))) : x)
export const sameValue = (a: unknown, b: unknown) => canonical(a ?? null) === canonical(b ?? null)

// Field-by-field differences of a Word style or margins change.
export interface StyleChange { field: string; label: string; before: unknown; after: unknown }

export function styleChanges(before: Record<string, unknown> = {}, after: Record<string, unknown> = {}, t: T): StyleChange[] {
  const labels: Record<string, string> = { font: 'tplFontLatin', font_ea: 'tplFontEA', size: 'tplSize', color: 'tplColor', bold: 'bold', before: 'tplBefore', after: 'tplAfter', top: 'tplMargin_top', bottom: 'tplMargin_bottom', left: 'tplMargin_left', right: 'tplMargin_right' }
  const show = (field: string, v: unknown) => field === 'color' ? (v || t('tplAutoColor')) : field === 'bold' ? (v ? '✓' : '—') : (v ?? '—')
  return Object.keys(after).filter(f => !sameValue(before[f], after[f])).map(f => ({ field: f, label: t(labels[f] || f), before: show(f, before[f]), after: show(f, after[f]) }))
}
