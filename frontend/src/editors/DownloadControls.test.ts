import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createAppI18n } from '../i18n'
import DownloadControls from './DownloadControls.vue'
import type { SavedDocument } from '../api/documents'

const document: SavedDocument = { id: 'deck', language: 'zh-TW', revision: 4, content: { title: '水循環', blocks: [] }, can_undo: true, can_redo: false }
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.useRealTimers() })
describe('artifact downloads', () => {
  it('downloads the displayed saved revision and releases its object URL', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn().mockImplementation(async () => new Response(new Blob(['office'])))
    vi.stubGlobal('fetch', fetchMock)
    const create = vi.fn().mockReturnValue('blob:download')
    const revoke = vi.fn()
    vi.stubGlobal('URL', class extends URL { static createObjectURL = create; static revokeObjectURL = revoke })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const wrapper = mount(DownloadControls, { props: { document, kind: 'slides' }, global: { plugins: [createAppI18n()] } })
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/slides/deck/download?revision=4&format=pptx')
    expect(click).toHaveBeenCalledOnce()
    await wrapper.findAll('button')[1].trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls[1][0]).toContain('format=pdf')
    wrapper.unmount()
    expect(revoke).toHaveBeenCalledWith('blob:download')
  })
  it('requires saving edits and displays errors instead of downloading error JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'export_assets_failed' } }), { status: 422 }))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(DownloadControls, { props: { document, kind: 'document', dirty: true }, global: { plugins: [createAppI18n()] } })
    expect(wrapper.findAll('button')[0].attributes('disabled')).toBeDefined()
    await wrapper.setProps({ dirty: false })
    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('圖片')
    expect(fetchMock.mock.calls[0][0]).toContain('format=docx')
    wrapper.unmount()
  })
})
