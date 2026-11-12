export async function checkHealth(signal: AbortSignal): Promise<void> {
  const response = await fetch('/api/v1/health', {
    signal,
    headers: { Accept: 'application/json' },
    cache: 'no-store',
  })
  if (!response.ok) throw new Error('health_request_failed')

  const body: unknown = await response.json()
  if (
    typeof body !== 'object' ||
    body === null ||
    !('status' in body) ||
    body.status !== 'ok' ||
    !('service' in body) ||
    body.service !== 'eduforge'
  ) {
    throw new Error('invalid_health_response')
  }
}

