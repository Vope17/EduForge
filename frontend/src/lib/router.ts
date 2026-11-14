import { reactive } from 'vue'

// Hash routes:
//   #/                                 → first project home
//   #/p/:project                       → project home
//   #/p/:project/paper/:paper/:step    → paper workspace (step: sources|knowledge|outputs|experiment)
//   #/p/:project/batch                 → batch generation for selected papers
//   #/settings/:tab                    → settings
export const STEPS = ['sources', 'knowledge', 'outputs', 'experiment'] as const
export const SETTINGS_TABS = ['providers', 'templates', 'reading', 'ssh'] as const
export type Step = typeof STEPS[number]
export type SettingsTab = typeof SETTINGS_TABS[number]

export type Route =
  | { name: 'settings'; tab: SettingsTab }
  | { name: 'paper'; project: string; paper: string; step: Step }
  | { name: 'batch'; project: string }
  | { name: 'home'; project: string }

/** The reactive route: every field of every route kind, unset ones undefined. */
export interface CurrentRoute {
  name: Route['name']
  project?: string
  paper?: string
  step?: Step
  tab?: SettingsTab
}

const isStep = (value: string | undefined): value is Step => (STEPS as readonly string[]).includes(value ?? '')
const isTab = (value: string | undefined): value is SettingsTab => (SETTINGS_TABS as readonly string[]).includes(value ?? '')

export function parseHash(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent)
  if (parts[0] === 'settings') return { name: 'settings', tab: isTab(parts[1]) ? parts[1] : 'providers' }
  if (parts[0] === 'p' && parts[1]) {
    const project = parts[1]
    if (parts[2] === 'paper' && parts[3]) return { name: 'paper', project, paper: parts[3], step: isStep(parts[4]) ? parts[4] : 'sources' }
    if (parts[2] === 'batch') return { name: 'batch', project }
    return { name: 'home', project }
  }
  return { name: 'home', project: '' }
}

export function buildHash(route: CurrentRoute): string {
  const enc = encodeURIComponent
  if (route.name === 'settings') return `#/settings/${route.tab || 'providers'}`
  if (!route.project) return '#/'
  if (route.name === 'paper') return `#/p/${enc(route.project)}/paper/${enc(route.paper ?? '')}/${route.step || 'sources'}`
  if (route.name === 'batch') return `#/p/${enc(route.project)}/batch`
  return `#/p/${enc(route.project)}`
}

export type Go = (next: CurrentRoute, options?: { replace?: boolean }) => void

export function createRouter(): { route: CurrentRoute; go: Go } {
  const route = reactive<CurrentRoute>(parseHash(location.hash))
  // project is kept when leaving for settings, so the workspace stays on it.
  const sync = () => { Object.assign(route, { tab: undefined, paper: undefined, step: undefined }, parseHash(location.hash)) }
  window.addEventListener('hashchange', sync)
  const go: Go = (next, { replace = false } = {}) => {
    const hash = buildHash(next)
    if (hash === location.hash) return sync()
    if (replace) { history.replaceState(null, '', hash); sync() } else location.hash = hash
  }
  return { route, go }
}
