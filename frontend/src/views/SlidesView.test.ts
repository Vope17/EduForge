import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '../App.vue'
import { createAppI18n } from '../i18n'
import { routes } from '../router'
import type { DocumentRequest, SavedDocument, TemplateSpec } from '../api/documents'

const page = (id: string) => ({ id, kind: 'slide' as const, text: '', layout: 'two_columns' as const, notes: '問學生水去了哪裡？', children: [
  { id: id + '-title', kind: 'heading' as const, text: '水循環' },
  { id: id + '-body', kind: 'paragraph' as const, text: '觀察蒸發與凝結。' },
] })
const spec: TemplateSpec = { palette: 'ocean', font: 'sans', layout: 'two_columns', tone: 'friendly', density: 'balanced', example: [page('sample')] }
const pending: DocumentRequest = { id: 'water', state: 'awaiting_confirmation', topic: '水循環', audience: '五年級', kind: 'presentation', artifact_type: 'slides', slide_count: 2, language: 'zh-TW', template_version: 2, confirmed_version: 0, template: spec }
const ready: SavedDocument = { artifact_type: 'slides', id: 'water', language: 'zh-TW', revision: 1, can_undo: false, can_redo: false, content: { title: '水循環簡報', blocks: [page('p1'), page('p2')] } }
const job = { job_id: 'job', artifact_id: 'water', state: 'queued', progress: 0, error_code: '', cancel_requested: false }
let wrapper: VueWrapper | undefined

function json(value: unknown) { return new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } }) }
async function open(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { global: { plugins: [createPinia(), createAppI18n(), router] } })
  await flushPromises()
  return wrapper
}
function button(app: VueWrapper, text: string) { return app.findAll('button').find((item) => item.text() === text)! }

beforeEach(() => { localStorage.clear() })
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('classroom slides', () => {
  it('defaults language to the UI, preserves an override and sends the slide count', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(json([])).mockResolvedValueOnce(json({ ...pending, state: 'requested' })).mockResolvedValueOnce(json({ request: { ...pending, state: 'requested' } }))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/slides')
    const [language] = app.findAll('select')
    const uiLocale = (code: 'zh-TW' | 'en') => app.get(`.language-switch button:${code === 'en' ? 'last' : 'first'}-child`).trigger('click')
    await uiLocale('en')
    expect(language.element.value).toBe('en')
    await language.setValue('zh-TW')
    await uiLocale('zh-TW')
    await uiLocale('en')
    expect(language.element.value).toBe('zh-TW')
    await app.get('input[maxlength="160"]').setValue('水循環')
    await app.get('input[type="number"]').setValue(4)
    await app.get('form').trigger('submit')
    await flushPromises()
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toMatchObject({ topic: '水循環', kind: 'presentation', language: 'zh-TW', slide_count: 4 })
  })

  it('requires confirmation, queues generation and reloads the completed deck', async () => {
    const confirmed = { ...pending, state: 'confirmed', confirmed_version: 2 }
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ request: pending })).mockResolvedValueOnce(json(confirmed)).mockResolvedValueOnce(json(job)).mockResolvedValueOnce(json({ ...job, state: 'completed', progress: 100 })).mockResolvedValueOnce(json({ request: { ...confirmed, state: 'ready' }, document: ready }))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/slides/water')
    expect(app.text()).not.toContain('生成完整簡報／重新嘗試')
    expect(app.findAll('.slide-layout-choices button')).toHaveLength(3)
    await button(app, '確認這一版樣板').trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/slides/water/confirm')
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ template_version: 2 })
    vi.useFakeTimers()
    await button(app, '生成完整簡報／重新嘗試').trigger('click')
    await flushPromises()
    expect(app.text()).toContain('等待生成')
    await vi.advanceTimersByTimeAsync(1300)
    await flushPromises()
    expect(app.get('h1').text()).toBe('水循環簡報')
    expect(app.findAll('.slides-filmstrip .slide-canvas')).toHaveLength(2)
  })

  it('recovers a running job on reload, cancels it and offers a retry', async () => {
    const confirmed = { ...pending, state: 'confirmed', confirmed_version: 2 }
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ request: { ...confirmed, state: 'generating' }, job: { ...job, state: 'running', progress: 25 } })).mockResolvedValueOnce(json({ ...job, state: 'cancelled', cancel_requested: true })).mockResolvedValueOnce(json({ request: confirmed, job: { ...job, state: 'cancelled', cancel_requested: true } }))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/slides/water')
    expect(app.text()).toContain('正在生成簡報')
    await button(app, '取消生成').trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/jobs/job/cancel')
    expect(button(app, '生成完整簡報／重新嘗試').exists()).toBe(true)
    expect(app.text()).toContain('已取消生成')
  })

  it('saves notes and reordered slides with stable nested IDs', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ request: { ...pending, state: 'ready' }, document: ready })).mockImplementation(async (_url, options) => json({ ...ready, revision: 2, content: JSON.parse(options.body).content, can_undo: true }))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/slides/water')
    await app.get('.slides-stage textarea').setValue('請學生分享觀察。')
    await button(app, '往後移動此頁').trigger('click')
    await button(app, '儲存新版本').trigger('click')
    await flushPromises()
    const sent = JSON.parse(fetchMock.mock.calls[1][1].body)
    expect(sent).toMatchObject({ base_revision: 1, action: 'save', content: { blocks: [ { id: 'p2' }, { id: 'p1', notes: '請學生分享觀察。', children: [{ id: 'p1-title' }, { id: 'p1-body' }] } ] } })
    expect(app.text()).toContain('修訂第 2 版')
  })

  it('keeps cross-slide selections and reviews a diff before accepting a new revision', async () => {
    const proposed = { proposal_id: 'proposal', base_revision: 1, changes: [{ block_id: 'p1-body', field: 'text', before: '觀察蒸發與凝結。', after: '觀察水的變化。' }] }
    const edited = structuredClone(ready)
    edited.revision = 2
    edited.content.blocks[0].children![1].text = '觀察水的變化。'
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ request: { ...pending, state: 'ready' }, document: ready })).mockResolvedValueOnce(json(proposed)).mockResolvedValueOnce(json(edited))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/slides/water')
    await app.get('.slides-stage [data-slide-block-id="p1-body"]').trigger('click')
    await app.findAll('.slides-filmstrip > button')[1].trigger('click')
    await app.get('.slides-stage [data-slide-block-id="p2-body"]').trigger('click', { shiftKey: true })
    await button(app, '選取此頁講者備註').trigger('click')
    expect(app.text()).toContain('已選取 3 處')
    await app.get('textarea[maxlength="1000"]').setValue('改得更口語')
    await app.get('.slides-workspace form').trigger('submit')
    await flushPromises()
    const sent = JSON.parse(fetchMock.mock.calls[1][1].body)
    expect(sent.selection).toEqual([{ artifact_id: 'water', block_id: 'p1-body' }, { artifact_id: 'water', block_id: 'p2-body' }, { artifact_id: 'water', block_id: 'p2', field: 'notes' }])
    expect(app.get('.slide-diff del').text()).toBe('觀察蒸發與凝結。')
    expect(app.get('.slide-diff ins').text()).toBe('觀察水的變化。')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await button(app, '接受修改並建立新版本').trigger('click')
    await flushPromises()
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/slides/water/patches/proposal/accept')
    expect(app.text()).toContain('修訂第 2 版')
    expect(app.find('.slide-diff').exists()).toBe(false)
  })

  it('uploads an image into the draft and saves it with the same block ID', async () => {
    const deck = structuredClone(ready)
    const id = 'a'.repeat(32)
    deck.id = id
    deck.content.blocks[0].children!.push({ id: 'illustration', kind: 'image', text: '水循環', image_url: '' })
    const url = '/api/v1/slides/' + id + '/images/' + 'b'.repeat(32)
    const fetchMock = vi.fn().mockResolvedValueOnce(json({ request: { ...pending, id, state: 'ready' }, document: deck }))
      .mockResolvedValueOnce(json({ image_url: url }))
      .mockImplementation(async (_url, options) => json({ ...deck, revision: 2, content: JSON.parse(options.body).content }))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/slides/' + id)
    await app.get('.slides-stage [data-slide-block-id="illustration"]').trigger('click')
    const input = app.get('input[type="file"]')
    const file = new File(['image'], 'diagram.png', { type: 'image/png' })
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    await flushPromises()
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/slides/' + id + '/images')
    expect(fetchMock.mock.calls[1][1].body).toBe(file)
    expect(app.get('.slides-stage img').attributes('src')).toBe(url)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    await button(app, '儲存新版本').trigger('click')
    await flushPromises()
    expect(JSON.parse(fetchMock.mock.calls[2][1].body).content.blocks[0].children[2]).toMatchObject({ id: 'illustration', image_url: url })
  })

  it('presents with keyboard navigation and speaker notes', async () => {
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.open = true } })
    Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.open = false } })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ request: { ...pending, state: 'ready' }, document: ready })))
    const app = await open('/slides/water')
    await button(app, '播放簡報').trigger('click')
    await flushPromises()
    expect(app.get('dialog').element.open).toBe(true)
    expect(app.get('dialog').text()).toContain('第 1 / 2 頁')
    await app.get('dialog').trigger('keydown', { key: 'ArrowRight' })
    expect(app.get('dialog').text()).toContain('第 2 / 2 頁')
    expect(app.get('dialog details').text()).toContain('問學生水去了哪裡？')
    await app.get('dialog').trigger('keydown', { key: 'Escape' })
    expect(app.get('dialog').element.open).toBe(false)
  })
})
