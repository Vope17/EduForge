import type { Locale } from '../i18n'
import type { Anchor } from '../selection/useSelection'

export type DocumentKind = 'lesson_plan' | 'worksheet' | 'handout'
export type BlockKind = 'heading' | 'paragraph' | 'list' | 'table' | 'image' | 'slide'
export type SlideLayout = 'title_body' | 'two_columns' | 'image_text'
export type DocumentState = 'requested' | 'template_generated' | 'awaiting_confirmation' | 'confirmed' | 'generating' | 'ready'

export interface Block {
  id?: string
  kind: BlockKind
  text: string
  items?: string[]
  rows?: string[][]
  image_url?: string
  children?: Block[]
  notes?: string
  layout?: SlideLayout
}

export interface Content {
  title: string
  blocks: Block[]
}

export interface TemplateSpec {
  palette: 'forest' | 'ocean' | 'warm'
  font: 'sans' | 'serif'
  layout: 'sections' | 'worksheet' | SlideLayout
  tone: 'friendly' | 'formal'
  density: 'compact' | 'balanced' | 'detailed'
  example: Block[]
}

export interface DocumentInput {
  topic: string
  audience: string
  kind: DocumentKind | 'presentation'
  artifact_type?: 'document' | 'slides'
  slide_count?: number
  language: Locale
}

export interface DocumentRequest extends DocumentInput {
  id: string
  state: DocumentState
  template_version: number
  confirmed_version: number
  template?: TemplateSpec
}

export interface SavedDocument {
  artifact_type?: 'document' | 'slides'
  id: string
  language: Locale
  revision: number
  content: Content
  can_undo: boolean
  can_redo: boolean
}

export interface DocumentDetails {
  request: DocumentRequest
  document?: SavedDocument
  job?: GenerationJob
}

export interface DocumentSummary {
  id: string
  title: string
  state: DocumentState
  created_at: string
}

export interface RevisionSummary {
  revision: number
  title: string
  created_at: string
}


export interface GenerationJob {
  job_id: string
  artifact_id: string
  state: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'
  progress: number
  error_code: string
  cancel_requested: boolean
}

export interface PatchProposal {
  proposal_id: string
  base_revision: number
  changes: { block_id: string; field: string; before: string; after: string }[]
}

export type Change =
  | { action: 'save'; content: Content }
  | { action: 'undo' | 'redo' }
  | { action: 'restore'; target_revision: number }

export class DocumentError extends Error {
  readonly code: string

  constructor(code: string) {
    super(code)
    this.code = code
  }
}

async function request<T>(base: string, path: string, signal: AbortSignal, body?: object): Promise<T> {
  const response = await fetch(base + path, {
    signal,
    method: body ? 'POST' : 'GET',
    headers: body ? { 'Content-Type': 'application/json', Accept: 'application/json' } : { Accept: 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    if (response.status === 413) throw new DocumentError('body_too_large')
    const result: { error?: { code?: string } } | undefined = await response.json().catch(() => undefined)
    throw new DocumentError(result?.error?.code ?? 'connection')
  }
  return response.json() as Promise<T>
}

const documentPath = (id: string) => '/' + encodeURIComponent(id)

function artifactClient<GenerationResult>(base: string) {
  const call = <T>(path: string, signal: AbortSignal, body?: object) => request<T>(base, path, signal, body)
  return {
    list: (signal: AbortSignal) => call<DocumentSummary[]>('', signal),
    create: (input: DocumentInput, signal: AbortSignal) => call<DocumentRequest>('', signal, input),
    get: (id: string, signal: AbortSignal) => call<DocumentDetails>(documentPath(id), signal),
    preview: (id: string, version: number, feedback: string, signal: AbortSignal) =>
      call<DocumentRequest>(documentPath(id) + '/preview', signal, { template_version: version, feedback }),
    confirm: (id: string, version: number, signal: AbortSignal) =>
      call<DocumentRequest>(documentPath(id) + '/confirm', signal, { template_version: version }),
    generate: (id: string, version: number, signal: AbortSignal) =>
      call<GenerationResult>(documentPath(id) + '/generate', signal, { template_version: version }),
    change: (id: string, revision: number, change: Change, signal: AbortSignal) =>
      call<SavedDocument>(documentPath(id) + '/revisions', signal, { base_revision: revision, ...change }),
    history: (id: string, signal: AbortSignal) => call<RevisionSummary[]>(documentPath(id) + '/revisions', signal),
    revision: (id: string, revision: number, signal: AbortSignal) =>
      call<SavedDocument>(documentPath(id) + '/revisions/' + revision, signal),
    propose: (id: string, revision: number, instruction: string, selection: Anchor[], signal: AbortSignal) =>
      call<PatchProposal>(documentPath(id) + '/patches', signal, { base_revision: revision, instruction, selection }),
    accept: (id: string, proposal: string, signal: AbortSignal) =>
      call<SavedDocument>(documentPath(id) + '/patches/' + encodeURIComponent(proposal) + '/accept', signal, {}),
    reject: (id: string, proposal: string, signal: AbortSignal) =>
      call<void>(documentPath(id) + '/patches/' + encodeURIComponent(proposal) + '/reject', signal, {}),
  }
}

export const documents = artifactClient<SavedDocument>('/api/v1/documents')
export const slides = artifactClient<GenerationJob>('/api/v1/slides')

export const generationJobs = {
  get: (id: string, signal: AbortSignal) => request<GenerationJob>('/api/v1/jobs', '/' + encodeURIComponent(id), signal),
  cancel: (id: string, signal: AbortSignal) => request<GenerationJob>('/api/v1/jobs', '/' + encodeURIComponent(id) + '/cancel', signal, {}),
}

const errors = [
  'invalid_selection', 'plan_permission', 'subscription_limit', 'not_found', 'invalid_input', 'version_conflict', 'generation_limit',
  'provider_unavailable', 'invalid_generation', 'generation_failed', 'body_too_large',
] as const

export function documentErrorKey(error: unknown) {
  const code = error instanceof DocumentError ? errors.find((item) => item === error.code) : undefined
  return 'documents.errors.' + (code ?? 'connection')
}
