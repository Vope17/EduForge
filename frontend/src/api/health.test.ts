import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkHealth } from './health'

afterEach(() => vi.unstubAllGlobals())

describe('health response validation', () => {
  it.each([
    { status: 503, body: { status: 'ok', service: 'eduforge' } },
    { status: 200, body: null },
    { status: 200, body: { status: 'unhealthy', service: 'eduforge' } },
    { status: 200, body: { status: 'ok', service: 'another-service' } },
  ])('rejects an unhealthy or invalid response: $status, $body', async ({ status, body }) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status })))
    await expect(checkHealth(new AbortController().signal)).rejects.toThrow()
  })
})

