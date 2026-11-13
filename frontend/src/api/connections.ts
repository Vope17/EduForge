export interface Connection {
  connected: boolean
  mode: '' | 'api_key' | 'chatgpt'
  source: '' | 'environment' | 'session' | 'codex'
  endpoint: string
  model: string
  account: string
  verified: boolean
  version: number
  csrf_token: string
  /** Codex CLI is installed in the backend, so ChatGPT sign-in can be used. */
  chatgpt_available: boolean
}

/** ChatGPT device-code sign-in through Codex CLI. */
export interface LoginState {
  status: 'idle' | 'pending' | 'connected' | 'failed'
  user_code?: string
  verification_url?: string
  expires_at?: string
}

export interface APIKeyInput {
  endpoint: string
  model: string
  api_key: string
  version: number
}

export interface ModelChoice {
  id: string
  name: string
}

class ConnectionError extends Error {
  readonly code: string

  constructor(code: string) {
    super(code)
    this.code = code
  }
}

async function request<T>(
  path: string,
  signal: AbortSignal,
  method = 'GET',
  body?: object,
  csrf?: string,
): Promise<T> {
  const response = await fetch('/api/v1/model-connection' + path, {
    method,
    signal,
    credentials: 'same-origin',
    cache: 'no-store',
    headers: {
      Accept: 'application/json',
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(csrf ? { 'X-EduForge-CSRF': csrf } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    const result: { error?: { code?: string } } | undefined = await response.json().catch(() => undefined)
    throw new ConnectionError(result?.error?.code ?? 'connection_failed')
  }
  return response.json() as Promise<T>
}

export const modelConnection = {
  get: (signal: AbortSignal) => request<Connection>('', signal),
  save: (input: APIKeyInput, csrf: string, signal: AbortSignal) =>
    request<Connection>('', signal, 'PUT', input, csrf),
  test: (version: number, csrf: string, signal: AbortSignal) =>
    request<Connection>('/test', signal, 'POST', { version }, csrf),
  disconnect: (version: number, csrf: string, signal: AbortSignal) =>
    request<{ connection: Connection; signed_out: boolean }>('', signal, 'DELETE', { version }, csrf),
  login: (csrf: string, signal: AbortSignal) =>
    request<LoginState>('/chatgpt/login', signal, 'POST', {}, csrf),
  loginStatus: (signal: AbortSignal) => request<LoginState>('/chatgpt/login', signal),
  models: (signal: AbortSignal) => request<ModelChoice[]>('/models', signal),
  selectModel: (model: string, version: number, csrf: string, signal: AbortSignal) =>
    request<Connection>('', signal, 'PATCH', { model, version }, csrf),
}

const errorCodes = [
  'invalid_connection', 'connection_changed', 'connection_forbidden', 'provider_unavailable',
  'plan_permission', 'connection_limit', 'connection_failed', 'invalid_input', 'body_too_large',
]

export function connectionErrorKey(error: unknown) {
  const code = error instanceof ConnectionError && errorCodes.includes(error.code) ? error.code : 'connection_failed'
  return 'models.errors.' + code
}
