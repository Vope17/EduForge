import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '../App.vue'
import { createAppI18n } from '../i18n'
import { routes } from '../router'
import type { DocumentRequest, SavedDocument, TemplateSpec } from '../api/documents'

const spec: TemplateSpec = {
  palette: 'ocean',
  font: 'sans',
  layout: 'worksheet',
  tone: 'friendly',
  density: 'balanced',
  example: [{ kind: 'heading', text: '水循環範例' }],
}

const pending: DocumentRequest = {
  id: 'water',
  state: 'awaiting_confirmation',
  topic: '水循環',
  audience: '國小五年級',
  kind: 'worksheet',
  language: 'zh-TW',
  template_version: 2,
  confirmed_version: 0,
  template: spec,
}

const ready: SavedDocument = {
  id: pending.id,
  language: pending.language,
  revision: 1,
  content: {
    title: '水循環學習單',
    blocks: [
      { id: 'heading', kind: 'heading', text: '水循環' },
      { id: 'paragraph', kind: 'paragraph', text: '觀察水的變化。' },
    ],
  },
  can_undo: false,
  can_redo: false,
}

let wrapper: VueWrapper | undefined

function json(value: unknown) {
  return new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
}

async function open(path: string) {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { global: { plugins: [createPinia(), createAppI18n(), router] } })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
})

describe('teaching documents', () => {
  it('confirms the visible template version before offering full generation', async () => {
    const confirmed = { ...pending, state: 'confirmed', confirmed_version: 2 }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ request: pending }))
      .mockResolvedValueOnce(json(confirmed))
      .mockResolvedValueOnce(json(ready))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/documents/water')
    expect(app.text()).toContain('樣板第 2 版')
    expect(app.text()).not.toContain('生成完整文檔')

    await app.findAll('button').find((button) => button.text() === '確認這一版樣板')!.trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/documents/water/confirm', expect.objectContaining({
      body: JSON.stringify({ template_version: 2 }),
    }))
    expect(app.text()).toContain('生成完整文檔')

    await app.findAll('button').find((button) => button.text() === '生成完整文檔')!.trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/v1/documents/water/generate', expect.objectContaining({
      body: JSON.stringify({ template_version: 2 }),
    }))
    expect(app.get('h1').text()).toBe(ready.content.title)
    expect(app.text()).toContain('修訂第 1 版')
  })

  it('selects multiple blocks and keeps their IDs when saving edits', async () => {
    const edited = {
      ...ready,
      revision: 2,
      content: {
        ...ready.content,
        blocks: [ready.content.blocks[0], { ...ready.content.blocks[1], text: '觀察蒸發與凝結。' }],
      },
      can_undo: true,
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ request: { ...pending, state: 'ready' }, document: ready }))
      .mockResolvedValueOnce(json(edited))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open('/documents/water')

    const buttons = app.findAll('main button[aria-pressed]')
    await buttons[0].trigger('click')
    await buttons[1].trigger('click')
    expect(app.findAll('main button[aria-pressed="true"]')).toHaveLength(2)

    await app.get('textarea[aria-label="區塊 2 的文字"]').setValue('觀察蒸發與凝結。')
    expect(app.findAll('main button[aria-pressed="true"]')).toHaveLength(1)
    await app.get('form').trigger('submit')
    await flushPromises()
    const [, options] = fetchMock.mock.calls[1]
    const sent = JSON.parse(options.body)
    expect(sent).toMatchObject({
      base_revision: 1,
      action: 'save',
      content: { blocks: [{ id: 'heading' }, { id: 'paragraph', text: '觀察蒸發與凝結。' }] },
    })
    expect(app.text()).toContain('修訂第 2 版')
  })

  it('uses the UI locale as the default generation language until overridden', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json([])))
    const app = await open('/documents')
    const [, language] = app.findAll('select')
    const uiLocale = (code: 'zh-TW' | 'en') => app.get(`.language-switch button:${code === 'en' ? 'last' : 'first'}-child`).trigger('click')
    expect(language.element.value).toBe('zh-TW')
    await uiLocale('en')
    expect(language.element.value).toBe('en')
    await language.setValue('zh-TW')
    await uiLocale('zh-TW')
    await uiLocale('en')
    expect(language.element.value).toBe('zh-TW')
  })
})
