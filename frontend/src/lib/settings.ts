// Settings inherit: a course holds the defaults, a chapter and a lesson store
// only what they override (zero values mean "inherit"). This mirrors
// app.MergeSettings on the server.
import type { OutputSettings } from '../api/types.ts'

export type SettingsOverride = Partial<OutputSettings>

export const DEFAULT_SETTINGS = { depth: 'standard', level: 'intermediate', minutes: 50 } as const

export function mergeSettings(base: SettingsOverride, over: SettingsOverride | undefined): SettingsOverride {
  const out: SettingsOverride = { ...base, languages: { ...(base.languages || {}) }, instructions: { ...(base.instructions || {}) } }
  if (!over) return out
  if (over.selected?.length) out.selected = [...over.selected]
  for (const key of ['depth', 'level', 'audience', 'template_id', 'reader_template_id'] as const) {
    if (over[key]) out[key] = over[key]
  }
  if (over.minutes) out.minutes = over.minutes
  for (const [kind, lang] of Object.entries(over.languages || {})) if (lang) out.languages![kind] = lang
  for (const [kind, text] of Object.entries(over.instructions || {})) if (text) out.instructions![kind] = text
  return out
}

// The settings a lesson ends up with: course defaults, then its chapter, then itself.
export function effectiveSettings(course?: SettingsOverride, chapter?: SettingsOverride, lesson?: SettingsOverride): SettingsOverride {
  const base: SettingsOverride = { ...DEFAULT_SETTINGS, ...(course || {}) }
  if (!base.depth) base.depth = DEFAULT_SETTINGS.depth
  if (!base.level) base.level = DEFAULT_SETTINGS.level
  if (!base.minutes) base.minutes = DEFAULT_SETTINGS.minutes
  return mergeSettings(mergeSettings(base, chapter), lesson)
}
