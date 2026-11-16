import { computed, reactive, provide, inject, type InjectionKey } from 'vue'
import { t, errorText } from './i18n.ts'
import { createRouter, type Step } from './router.ts'
import { lessonSteps, nextStep } from './status.ts'
import type { ModelOption, Course, Provider, Rec, Session, Summary } from '../api/types.ts'

const MODEL_KINDS = ['codex', 'claude-cli', 'openai', 'anthropic']

/** Which provider generation will use (see checkConnections). */
export interface Connection {
  checking: boolean
  ready: boolean
  mode: 'model' | 'extract' | 'none'
  provider: string
  providerID?: string
  model: string
  /** Error code per provider id. */
  errors: Record<string, string>
}

export interface WorkspaceState {
  language: string
  session: Session | null
  courseID: string
  lessonID: string
  selected: string[]
  courses: Rec<Course>[]
  snapshot: Summary
  busy: number
  error: string
  toast: string
  drawer: boolean
  nav: boolean
  connection: Connection
  /** Models each provider offers, by provider id. */
  models: Record<string, ModelOption[]>
}

/** Options for api(): fetch options with a JSON (or FormData) body. */
export interface ApiOptions extends Omit<RequestInit, 'body' | 'method' | 'headers'> {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  headers?: Record<string, string>
}

function storedLanguage() {
  try { return localStorage.getItem('eduforge-language') || 'zh-TW' } catch { return 'zh-TW' }
}

export function createWorkspace() {
  const { route, go } = createRouter()
  const state = reactive<WorkspaceState>({
    language: storedLanguage(), session: null, courseID: '', lessonID: '', selected: [],
    courses: [], snapshot: {}, busy: 0, error: '', toast: '', drawer: false, nav: false,
    connection: { checking: true, ready: false, mode: 'none', provider: '', model: '', errors: {} },
    models: {},
  })
  const tr = (name: string) => t(name, state.language)
  let toastTimer: ReturnType<typeof setTimeout> | undefined, refreshVersion = 0

  function notify(text: string = tr('saved')) { state.toast = text; clearTimeout(toastTimer); toastTimer = setTimeout(() => { state.toast = '' }, 4000) }

  // Calls the JSON API. T is the reply's type; errors throw the server's code.
  async function api<T = unknown>(path: string, { method = 'GET', body, ...options }: ApiOptions = {}): Promise<T> {
    // Showcase mode: refuse changes here too, so visitors get the explanation without a round trip.
    if (state.session?.read_only && method !== 'GET' && path !== '/math') throw new Error('read_only')
    const headers: Record<string, string> = { ...options.headers }
    if (method !== 'GET') headers['X-EduForge-Token'] = state.session?.csrf_token || ''
    const multipart = body instanceof FormData
    if (body !== undefined && !multipart) headers['Content-Type'] = 'application/json'
    const res = await fetch('/api' + path, { ...options, method, headers, body: body === undefined ? undefined : multipart ? body : JSON.stringify(body) })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error?.code || `http_${res.status}`)
    return data as T
  }

  async function refresh() {
    const version = ++refreshVersion
    const courses = await api<Rec<Course>[]>('/courses')
    const wanted = route.course || state.courseID
    const courseID = courses.some(course => course.id === wanted) ? wanted : courses[0]?.id || ''
    const snapshot = await api<Summary>('/summary' + (courseID ? `?course_id=${courseID}` : ''))
    if (version !== refreshVersion) return
    state.courses = courses
    state.courseID = courseID
    state.snapshot = snapshot
    const lessonIDs = new Set((snapshot.lesson || []).map(p => p.id))
    state.selected = state.selected.filter(id => lessonIDs.has(id))
    if (state.lessonID && !lessonIDs.has(state.lessonID)) state.lessonID = ''
  }

  // Runs a change, then reloads the summary; shows the error instead of throwing.
  async function act<T>(work: () => Promise<T> | T, { success = true }: { success?: boolean } = {}): Promise<T | null> {
    state.busy++; state.error = ''
    try { const value = await work(); await refresh(); if (success) notify(); return value }
    catch (err) { state.error = (err as Error).message; return null }
    finally { state.busy-- }
  }

  const providers = computed(() => {
    const order = (state.snapshot.preference || []).find(x => x.data.key === 'provider_order' && !x.data.imported_candidate)?.data.ids || []
    // The offline test mode is always the last resort (the server agrees).
    const rank = (p: Rec<Provider>) => (p.data.kind === 'source-extract' ? 10000 : 0) + (order.includes(p.id) ? order.indexOf(p.id) : 1000)
    return (state.snapshot.provider || []).slice().sort((a, b) => rank(a) - rank(b))
  })

  async function loadModels(p: Rec<Provider>): Promise<ModelOption[]> {
    try {
      const models = await api<ModelOption[]>('/providers/' + p.id + '/models')
      state.models[p.id] = models
      delete state.connection.errors[p.id]
      return models
    } catch (err) {
      state.models[p.id] = []
      state.connection.errors[p.id] = (err as Error).message
      return []
    }
  }

  // Determines which provider generation will actually use: the first enabled
  // provider (in the user's order) that can list models, else source excerpts.
  async function checkConnections() {
    state.connection.checking = true
    try {
      const enabled = providers.value.filter(p => p.data.enabled)
      const results = await Promise.all(enabled.map(async p => {
        if (!MODEL_KINDS.includes(p.data.kind)) return { p, ok: p.data.kind === 'source-extract' }
        if (['openai', 'anthropic'].includes(p.data.kind) && !p.data.has_credential) {
          state.connection.errors[p.id] = 'provider_credentials_or_models_unavailable'
          return { p, ok: false }
        }
        const models = await loadModels(p)
        return { p, ok: models.length > 0 }
      }))
      const model = results.find(r => r.ok && MODEL_KINDS.includes(r.p.data.kind))
      const extract = results.find(r => r.ok && r.p.data.kind === 'source-extract')
      const chosen = model || extract
      Object.assign(state.connection, {
        ready: !!chosen,
        mode: model ? 'model' : extract ? 'extract' : 'none',
        provider: chosen?.p.data.name || '',
        providerID: chosen?.p.id || '',
        model: model ? (model.p.data.model || state.models[model.p.id]?.[0]?.id || '') : '',
      })
    } finally { state.connection.checking = false }
  }

  async function init() {
    await act(async () => {
      state.session = await api<Session>('/session')
      await refresh()
      const pref = state.snapshot.preference?.find(x => x.data.key === 'ui_language' && !x.data.imported_candidate)
      if (pref) state.language = String(pref.data.value)
    }, { success: false })
    void checkConnections()
  }

  async function setLanguage(lang: string) {
    state.language = lang
    try { localStorage.setItem('eduforge-language', lang) } catch {}
    if (state.session?.read_only) return
    await act(() => api('/preferences', { method: 'PUT', body: { key: 'ui_language', value: lang } }), { success: false })
  }

  function fileURL(record: { id: string }, path: string) { return `/api/files/${record.id}?path=${encodeURIComponent(path)}` }
  const lesson = computed(() => (state.snapshot.lesson || []).find(x => x.id === state.lessonID))
  const knowledge = computed(() => (state.snapshot.knowledge || []).find(x => x.id === lesson.value?.data.knowledge_id))
  function citationLabel(id: string) {
    for (const source of state.snapshot.source || []) {
      const unit = source.data.parsed?.units?.find(x => x.id === id)
      if (unit) return `${source.data.name} · ${tr(unit.kind === 'slide' ? 'unitSlide' : 'unitPage')} ${unit.locator}`
    }
    return id
  }
  // Knowledge or an output written when other lessons came before its lesson.
  const changedSet = computed(() => new Set(state.snapshot.background_changed || []))
  const backgroundChanged = (id: string | undefined) => !!id && changedSet.value.has(id)
  const explain = (code: string) => errorText(code, state.language)
  // Without an explicit step, open the step that needs attention next.
  function openLesson(id: string, step?: Step) {
    const target = (state.snapshot.lesson || []).find(p => p.id === id)
    go({ name: 'lesson', course: state.courseID, lesson: id, step: step || nextStep(lessonSteps(target, state.snapshot)) })
  }

  const workspace = {
    state, route, go, t: tr, api, refresh, act, init, setLanguage, notify, fileURL, lesson, knowledge,
    citationLabel, explain, backgroundChanged, providers, loadModels, checkConnections, openLesson,
  }
  provide(key, workspace)
  return workspace
}

export type Workspace = ReturnType<typeof createWorkspace>
const key: InjectionKey<Workspace> = Symbol('eduforge')

export function useWorkspace(): Workspace {
  const workspace = inject(key)
  if (!workspace) throw new Error('Workspace not provided')
  return workspace
}
