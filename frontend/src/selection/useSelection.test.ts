import { describe, expect, it } from 'vitest'
import { rectangleIntersects, useSelection } from './useSelection'

describe('shared artifact selection', () => {
  it('selects intersecting regions and adds across pages without duplicates', () => {
    const selection = useSelection(() => 'deck')
    const regions = [
      { blockID: 'heading', rect: { left: 20, top: 20, width: 100, height: 30 } },
      { blockID: 'body', rect: { left: 20, top: 70, width: 100, height: 80 } },
      { blockID: 'image', rect: { left: 200, top: 70, width: 100, height: 80 } },
    ]
    selection.selectRectangle({ left: 10, top: 10, width: 150, height: 140 }, regions)
    expect(selection.anchors.value.map((item) => item.block_id)).toEqual(['heading', 'body'])
    selection.selectRectangle({ left: 190, top: 60, width: 120, height: 100 }, regions, true)
    selection.toggleField('page-2', 'notes')
    expect(selection.anchors.value).toHaveLength(4)
    selection.selectRectangle({ left: 10, top: 10, width: 150, height: 140 }, regions, true)
    expect(selection.anchors.value).toHaveLength(4)
    expect(rectangleIntersects(regions[0].rect, regions[2].rect)).toBe(false)
  })

  it('preserves disjoint UTF-16 text ranges and separate note selections', () => {
    const selection = useSelection(() => 'deck')
    selection.toggle('body')
    selection.addText('body', 1, 3)
    selection.addText('body', 5, 7)
    selection.addText('body', 1, 3)
    selection.addText('body', 2, 2)
    selection.toggleField('page', 'notes')
    expect(selection.anchors.value).toEqual([
      { artifact_id: 'deck', block_id: 'body', field: 'text', start: 1, end: 3 },
      { artifact_id: 'deck', block_id: 'body', field: 'text', start: 5, end: 7 },
      { artifact_id: 'deck', block_id: 'page', field: 'notes' },
    ])
    selection.remove('body')
    expect(selection.anchors.value).toHaveLength(1)
  })

  it('normalizes pointer drag coordinates and releases the overlay', () => {
    const target = document.createElement('div')
    target.getBoundingClientRect = () => ({ left: 100, top: 200, width: 500, height: 300 }) as DOMRect
    const selection = useSelection(() => 'deck', () => [
      { blockID: 'a', rect: { left: 120, top: 220, width: 50, height: 50 } },
      { blockID: 'b', rect: { left: 400, top: 400, width: 50, height: 50 } },
    ])
    const event = (x: number, y: number) => ({ button: 0, target, currentTarget: target, clientX: x, clientY: y, pointerId: 1, preventDefault() {} }) as unknown as PointerEvent
    selection.beginRectangle(event(200, 300))
    selection.moveRectangle(event(110, 210))
    expect(selection.rectangle.value).toEqual({ left: 10, top: 10, width: 90, height: 90 })
    selection.finishRectangle(event(110, 210))
    expect(selection.anchors.value.map((item) => item.block_id)).toEqual(['a'])
    expect(selection.rectangle.value).toBeUndefined()
  })
})
