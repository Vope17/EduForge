import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import App from './App.vue'
import { createAppI18n } from './i18n'
import { routes } from './router'

let wrapper: VueWrapper | undefined

function healthyResponse() {
  return new Response(JSON.stringify({ status: 'ok', service: 'eduforge' }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

async function mountApp() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/')
  await router.isReady()
  wrapper = mount(App, {
    global: { plugins: [createPinia(), createAppI18n(), router] },
  })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('M0 home', () => {
  it('switches the visible interface, page title, and document language in both directions', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(async () => healthyResponse()))
    const app = await mountApp()

    expect(app.get('h1').text()).toContain('把備課時間')
    expect(app.get('[role="status"]').text()).toContain('服務已連線')
    expect(document.documentElement.lang).toBe('zh-TW')
    expect(app.findAll('.format-card')).toHaveLength(4)

    await app.get('.language-switch button:last-child').trigger('click')
    expect(app.get('h1').text()).toContain('Less time preparing.')
    expect(app.get('[role="status"]').text()).toContain('Connected.')
    expect(app.get('[data-health-retry]').text()).toContain('Check again')
    expect(document.documentElement.lang).toBe('en')
    expect(document.title).toBe('EduForge | A creative space for educators')

    await app.get('.language-switch button:first-child').trigger('click')
    expect(app.get('h1').text()).toContain('把備課時間')
    expect(document.documentElement.lang).toBe('zh-TW')
    expect(document.title).toBe('EduForge｜教育工作者的創作空間')
  })

  it('shows a connection failure and recovers when the teacher retries', async () => {
    const fetchMock = vi.fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(healthyResponse())
    vi.stubGlobal('fetch', fetchMock)
    const app = await mountApp()

    expect(app.get('[role="status"]').text()).toContain('暫時無法連線')
    expect(app.get('[data-health-retry]').attributes('disabled')).toBeUndefined()
    await app.get('[data-health-retry]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/health', expect.objectContaining({
      signal: expect.any(AbortSignal),
    }))
    expect(app.get('[role="status"]').text()).toContain('服務已連線')
  })

  it('times out a stalled health request and allows another check', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_url: string, options: RequestInit) =>
      new Promise((_resolve, reject) => {
        options.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true })
      }),
    ))
    const app = await mountApp()
    expect(app.get('[data-health-retry]').attributes('disabled')).toBeDefined()

    await vi.advanceTimersByTimeAsync(5000)
    expect(app.get('[role="status"]').text()).toContain('暫時無法連線')
    expect(app.get('[data-health-retry]').attributes('disabled')).toBeUndefined()
  })

  it('cancels the health request when the view is removed', async () => {
    let requestSignal: AbortSignal | null | undefined
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_url: string, options: RequestInit) => {
      requestSignal = options.signal
      return new Promise((_resolve, reject) => {
        options.signal?.addEventListener('abort', () => reject(new Error('aborted')), { once: true })
      })
    }))
    const app = await mountApp()
    expect(requestSignal?.aborted).toBe(false)
    app.unmount()
    wrapper = undefined
    expect(requestSignal?.aborted).toBe(true)
  })
})

