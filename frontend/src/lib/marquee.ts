// Box selection: drag a rectangle over empty space to select every item it
// touches, for choosing what an AI edit should change. Hold Shift (or Ctrl, or
// ⌘) to add to the current selection instead of replacing it.
import { onMounted, onUnmounted, reactive, type Ref } from 'vue'

export interface Rect { left: number; top: number; right: number; bottom: number }

const touches = (a: Rect, b: Rect) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top

// The keys of the items a rectangle touches, in the order given.
export function marqueeKeys(rect: Rect, items: { key: string; rect: Rect }[]): string[] {
  return items.filter(item => touches(rect, item.rect)).map(item => item.key)
}

// Keys to select: the current ones too when adding, each once.
export function combine(current: string[], hits: string[], additive: boolean): string[] {
  return [...new Set(additive ? [...current, ...hits] : hits)]
}

export const TINY = 4

export interface MarqueeOptions {
  container: Ref<HTMLElement | null>
  /** CSS selector of the selectable items; each carries data-select-key. */
  items: string
  enabled: () => boolean
  current: () => string[]
  commit: (keys: string[]) => void
  /** A drag that starts on a match of this selector is not a box selection. */
  ignore?: string
  /** Where drags may start, when wider than the container (its page margins). */
  listenOn?: () => HTMLElement | null
}

const CONTROLS = 'input, textarea, button, select, a, label, [contenteditable="true"], .rich-text'

// Wires box selection onto a container, which must be positioned. The box to
// draw is returned in the container's coordinates.
export function useMarquee(o: MarqueeOptions) {
  const box = reactive({ active: false, x: 0, y: 0, w: 0, h: 0 })
  let start = { x: 0, y: 0 }, base: string[] = [], additive = false

  function itemsIn(container: HTMLElement) {
    return [...container.querySelectorAll<HTMLElement>(o.items)].map(el => ({ key: el.dataset.selectKey || '', rect: el.getBoundingClientRect() })).filter(item => item.key)
  }
  function onDown(event: PointerEvent) {
    const container = o.container.value
    if (!container || !o.enabled() || event.button !== 0 || !(event.target instanceof Element)) return
    if (event.target.closest(CONTROLS) || (o.ignore && event.target.closest(o.ignore))) return
    additive = event.shiftKey || event.ctrlKey || event.metaKey
    base = o.current()
    start = { x: event.clientX, y: event.clientY }
    // No text selection while dragging a box.
    event.preventDefault()
    const move = (e: PointerEvent) => {
      const rect: Rect = { left: Math.min(start.x, e.clientX), top: Math.min(start.y, e.clientY), right: Math.max(start.x, e.clientX), bottom: Math.max(start.y, e.clientY) }
      if (!box.active && rect.right - rect.left < TINY && rect.bottom - rect.top < TINY) return
      const origin = container.getBoundingClientRect()
      Object.assign(box, { active: true, x: rect.left - origin.left + container.scrollLeft, y: rect.top - origin.top + container.scrollTop, w: rect.right - rect.left, h: rect.bottom - rect.top })
      o.commit(combine(base, marqueeKeys(rect, itemsIn(container)), additive))
    }
    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', stop)
      // A plain click on empty space clears the selection.
      if (!box.active && !additive) o.commit([])
      box.active = false
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop)
  }
  let host: HTMLElement | null = null
  onMounted(() => { host = o.listenOn?.() ?? o.container.value; host?.addEventListener('pointerdown', onDown) })
  onUnmounted(() => host?.removeEventListener('pointerdown', onDown))
  return box
}
