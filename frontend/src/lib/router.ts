import { reactive } from 'vue'

// Hash routes:
//   #/                                 → first course home
//   #/p/:course                       → course home
//   #/p/:course/lesson/:lesson/:step    → lesson workspace (step: sources|knowledge|outputs)
//   #/p/:course/batch                 → batch generation for selected lessons
//   #/settings/:tab                    → settings
export const STEPS = ['sources', 'knowledge', 'outputs'] as const
export const SETTINGS_TABS = ['providers', 'templates'] as const
export type Step = typeof STEPS[number]
export type SettingsTab = typeof SETTINGS_TABS[number]

export type Route =
  | { name: 'settings'; tab: SettingsTab }
  | { name: 'lesson'; course: string; lesson: string; step: Step }
  | { name: 'batch'; course: string }
  | { name: 'home'; course: string }

/** The reactive route: every field of every route kind, unset ones undefined. */
export interface CurrentRoute {
  name: Route['name']
  course?: string
  lesson?: string
  step?: Step
  tab?: SettingsTab
}

const isStep = (value: string | undefined): value is Step => (STEPS as readonly string[]).includes(value ?? '')
const isTab = (value: string | undefined): value is SettingsTab => (SETTINGS_TABS as readonly string[]).includes(value ?? '')

export function parseHash(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent)
  if (parts[0] === 'settings') return { name: 'settings', tab: isTab(parts[1]) ? parts[1] : 'providers' }
  if (parts[0] === 'p' && parts[1]) {
    const course = parts[1]
    if (parts[2] === 'lesson' && parts[3]) return { name: 'lesson', course, lesson: parts[3], step: isStep(parts[4]) ? parts[4] : 'sources' }
    if (parts[2] === 'batch') return { name: 'batch', course }
    return { name: 'home', course }
  }
  return { name: 'home', course: '' }
}

export function buildHash(route: CurrentRoute): string {
  const enc = encodeURIComponent
  if (route.name === 'settings') return `#/settings/${route.tab || 'providers'}`
  if (!route.course) return '#/'
  if (route.name === 'lesson') return `#/p/${enc(route.course)}/lesson/${enc(route.lesson ?? '')}/${route.step || 'sources'}`
  if (route.name === 'batch') return `#/p/${enc(route.course)}/batch`
  return `#/p/${enc(route.course)}`
}

export type Go = (next: CurrentRoute, options?: { replace?: boolean }) => void

export function createRouter(): { route: CurrentRoute; go: Go } {
  const route = reactive<CurrentRoute>(parseHash(location.hash))
  // course is kept when leaving for settings, so the workspace stays on it.
  const sync = () => { Object.assign(route, { tab: undefined, lesson: undefined, step: undefined }, parseHash(location.hash)) }
  window.addEventListener('hashchange', sync)
  const go: Go = (next, { replace = false } = {}) => {
    const hash = buildHash(next)
    if (hash === location.hash) return sync()
    if (replace) { history.replaceState(null, '', hash); sync() } else location.hash = hash
  }
  return { route, go }
}
