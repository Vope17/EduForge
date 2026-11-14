// Formulas (TeX) rendered by the server's MathJax as SVG, batched and cached,
// so the editors show them as the exports do.
import type { Rendered } from '../api/types.ts'
import type { Workspace } from './workspace.ts'

type Api = Workspace['api']
interface Queued { tex: string; display: boolean; resolve: (result: Rendered) => void }

const cache = new Map<string, Promise<Rendered>>()
let queue: Queued[] = []
let pending: ReturnType<typeof setTimeout> | null = null

const keyOf = (tex: string, display: boolean) => (display ? 'D:' : 'I:') + tex

// renderFormula resolves to { svg, width, height, depth } or { error }.
// api is the workspace's api(path, options) helper.
export function renderFormula(api: Api, tex: string, display: boolean = false): Promise<Rendered> {
  const key = keyOf(tex, display)
  if (!cache.has(key)) {
    cache.set(key, new Promise<Rendered>(resolve => queue.push({ tex, display, resolve })))
    pending ||= setTimeout(() => flush(api), 15)
  }
  return cache.get(key)!
}

async function flush(api: Api) {
  const batch = queue
  queue = []
  pending = null
  try {
    const results = await api<Rendered[]>('/math', { method: 'POST', body: { formulas: batch.map(({ tex, display }) => ({ tex, display })) } })
    batch.forEach((item, i) => item.resolve(results[i] || { error: 'not rendered' }))
  } catch (err) {
    batch.forEach(item => { cache.delete(keyOf(item.tex, item.display)); item.resolve({ error: String((err as Error).message || err) }) })
  }
}

// hydrate replaces the TeX source in formula chips inside root with their
// rendered SVG (chips carry data-tex and data-display).
export async function hydrate(api: Api, root: ParentNode) {
  const chips = [...root.querySelectorAll<HTMLElement>('[data-tex]')]
  await Promise.all(chips.map(async chip => {
    const tex = chip.dataset.tex ?? ''
    if (chip.dataset.rendered === tex) return
    const result = await renderFormula(api, tex, chip.dataset.display === '1')
    if (chip.dataset.tex !== tex) return // edited meanwhile
    chip.classList.toggle('math-error', !!result.error)
    chip.dataset.rendered = tex
    if (result.svg) {
      chip.innerHTML = result.svg
      chip.title = tex
      const svg = chip.querySelector('svg')
      if (svg && chip.dataset.display !== '1') svg.style.verticalAlign = `-${result.depth || 0}ex`
    } else chip.title = result.error || ''
  }))
}
