import { computed, ref } from 'vue'

export interface Anchor {
  artifact_id: string
  block_id: string
  field?: string
  start?: number
  end?: number
}

export interface SelectionRect {
  left: number
  top: number
  width: number
  height: number
}

export interface SelectableRegion {
  blockID: string
  rect: SelectionRect
}

export function rectangleIntersects(a: SelectionRect, b: SelectionRect) {
  return a.left < b.left + b.width && a.left + a.width > b.left &&
    a.top < b.top + b.height && a.top + a.height > b.top
}

// Text offsets use UTF-16 indices with an exclusive end.
export function useSelection(artifactId: () => string, regions: () => SelectableRegion[] = () => []) {
  const selected = ref<Anchor[]>([])
  const anchors = computed(() => selected.value)
  const rectangle = ref<SelectionRect>()
  let drag: { x: number; y: number; origin: DOMRect; additive: boolean; target: HTMLElement; pointer: number } | undefined

  function remove(blockId: string) {
    selected.value = selected.value.filter((anchor) => anchor.block_id !== blockId)
  }

  function toggle(blockId: string) {
    if (selected.value.some((anchor) => anchor.block_id === blockId)) remove(blockId)
    else selected.value.push({ artifact_id: artifactId(), block_id: blockId })
  }

  function toggleField(blockId: string, field: string) {
    if (selected.value.some((anchor) => anchor.block_id === blockId && anchor.field === field)) {
      selected.value = selected.value.filter((anchor) => anchor.block_id !== blockId || anchor.field !== field)
    } else {
      selected.value.push({ artifact_id: artifactId(), block_id: blockId, field })
    }
  }

  function addText(blockId: string, start: number, end: number, field = 'text') {
    if (start >= end) return
    if (selected.value.some((anchor) => anchor.block_id === blockId && (anchor.field ?? 'text') === field &&
      anchor.start === start && anchor.end === end)) return
    selected.value = selected.value.filter((anchor) => anchor.block_id !== blockId || anchor.start !== undefined ||
      anchor.field !== undefined && anchor.field !== field)
    selected.value.push({ artifact_id: artifactId(), block_id: blockId, field, start, end })
  }

  function clear() { selected.value = [] }

  function selectRectangle(rect: SelectionRect, items: SelectableRegion[], additive = false) {
    if (!additive) clear()
    for (const item of items) {
      if (rectangleIntersects(rect, item.rect) && !selected.value.some((anchor) => anchor.block_id === item.blockID)) {
        selected.value.push({ artifact_id: artifactId(), block_id: item.blockID })
      }
    }
  }

  function beginRectangle(event: PointerEvent) {
    if (event.button !== 0 || event.target !== event.currentTarget) return
    const target = event.currentTarget as HTMLElement
    drag = { x: event.clientX, y: event.clientY, origin: target.getBoundingClientRect(), additive: event.shiftKey, target, pointer: event.pointerId }
    target.setPointerCapture?.(event.pointerId)
    rectangle.value = { left: event.clientX - drag.origin.left, top: event.clientY - drag.origin.top, width: 0, height: 0 }
    event.preventDefault()
  }

  function moveRectangle(event: PointerEvent) {
    if (!drag) return
    rectangle.value = {
      left: Math.min(drag.x, event.clientX) - drag.origin.left,
      top: Math.min(drag.y, event.clientY) - drag.origin.top,
      width: Math.abs(event.clientX - drag.x),
      height: Math.abs(event.clientY - drag.y),
    }
  }

  function finishRectangle(event: PointerEvent) {
    if (!drag) return
    moveRectangle(event)
    const rect = rectangle.value
    if (rect && rect.width >= 5 && rect.height >= 5) {
      selectRectangle({ ...rect, left: rect.left + drag.origin.left, top: rect.top + drag.origin.top }, regions(), drag.additive)
    }
    cancelRectangle()
  }

  function cancelRectangle() {
    if (drag?.target.hasPointerCapture?.(drag.pointer)) drag.target.releasePointerCapture(drag.pointer)
    drag = undefined
    rectangle.value = undefined
  }

  return { anchors, toggle, toggleField, addText, remove, clear, selectRectangle, rectangle, beginRectangle, moveRectangle, finishRectangle, cancelRectangle }
}
