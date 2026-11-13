import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from '../App.vue'
import { createAppI18n } from '../i18n'
import { routes } from '../router'
import type { Connection } from '../api/connections'

const disconnected: Connection = {
  connected: false,
  mode: '',
  source: '',
  endpoint: '',
  model: '',
  account: '',
  verified: false,
  version: 0,
  csrf_token: 'fixture-csrf',
  chatgpt_available: true,
}

let wrapper: VueWrapper | undefined

function json(value: unknown) {
  return new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } })
}

async function open(path = '/settings/models') {
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
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('model connection settings', () => {
  it('submits the API key once and clears the field without browser persistence', async () => {
    const storage = vi.spyOn(Storage.prototype, 'setItem')
    const connected = {
      ...disconnected, connected: true, mode: 'api_key', source: 'session',
      endpoint: 'https://provider.example/v1/chat/completions', model: 'fixture', version: 1,
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json(disconnected))
      .mockResolvedValueOnce(json({ status: 'idle' }))
      .mockResolvedValueOnce(json(connected))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open()
    await app.get('input[type="radio"][value="api_key"]').setValue()
    await app.get('input[type="url"]').setValue(connected.endpoint)
    await app.get('input[placeholder]').setValue('fixture')
    await app.get('input[type="password"]').setValue('private-test-key')
    await app.get('form').trigger('submit')
    await flushPromises()

    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/model-connection', expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'X-EduForge-CSRF': 'fixture-csrf' }),
      body: JSON.stringify({
        endpoint: connected.endpoint, model: 'fixture', api_key: 'private-test-key', version: 0,
      }),
    }))
    expect((app.get('input[type="password"]').element as HTMLInputElement).value).toBe('')
    expect(app.text()).not.toContain('private-test-key')
    expect(storage).not.toHaveBeenCalled()
    expect(app.text()).toContain('已設定，尚未測試')
  })

  it('signs in with a device code, then loads models and applies the selection', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const signedIn: Connection = {
      ...disconnected, connected: true, mode: 'chatgpt', source: 'codex', account: 'teacher@example.com', version: 1,
    }
    const pending = {
      status: 'pending', user_code: 'ABCD-1234',
      verification_url: 'https://auth.openai.com/codex/device', expires_at: '2026-11-14T08:15:00Z',
    }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json(disconnected))
      .mockResolvedValueOnce(json({ status: 'idle' }))
      .mockResolvedValueOnce(json(pending))
      .mockResolvedValueOnce(json(pending))
      .mockResolvedValueOnce(json({ status: 'connected' }))
      .mockResolvedValueOnce(json(signedIn))
      .mockResolvedValueOnce(json([{ id: 'fixture-model', name: 'Fixture model' }]))
      .mockResolvedValueOnce(json({ ...signedIn, model: 'fixture-model', version: 2 }))
    vi.stubGlobal('fetch', fetchMock)
    const app = await open()
    await app.get('button[data-sign-in]').trigger('click')
    await flushPromises()
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/model-connection/chatgpt/login', expect.objectContaining({
      method: 'POST', headers: expect.objectContaining({ 'X-EduForge-CSRF': 'fixture-csrf' }),
    }))
    expect(app.get('[data-sign-in-code]').text()).toBe('ABCD-1234')
    expect(app.get('a[href="https://auth.openai.com/codex/device"]').attributes('target')).toBe('_blank')

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(app.find('[data-sign-in-code]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(app.find('[data-sign-in-code]').exists()).toBe(false)
    expect(app.text()).toContain('teacher@example.com')
    expect(app.text()).toContain('Codex 預設模型')
    expect(app.text()).toContain('ChatGPT 登入完成')

    const picker = app.findAll('select').find((select) => select.text().includes('Fixture model'))!
    await picker.setValue('fixture-model')
    await app.get('form').trigger('submit')
    await flushPromises()
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/model-connection', expect.objectContaining({
      method: 'PATCH',
      body: JSON.stringify({ model: 'fixture-model', version: 1 }),
    }))
    expect(app.text()).toContain('fixture-model')
    vi.useRealTimers()
  })

  it('explains a missing Codex CLI and keeps API-key configuration accessible', async () => {
    vi.stubGlobal('fetch', vi.fn()
      .mockResolvedValueOnce(json({ ...disconnected, chatgpt_available: false }))
      .mockResolvedValueOnce(json({ status: 'idle' })))
    const app = await open()
    expect(app.text()).toContain('後端未安裝 Codex CLI')
    expect(app.get('button[data-sign-in]').attributes('disabled')).toBeDefined()
    await app.get('input[type="radio"][value="api_key"]').setValue()
    expect(app.get('input[type="password"]').attributes('autocomplete')).toBe('off')
  })
})
