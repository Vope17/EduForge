// API shapes that are not plain Go structs: records with typed data, the
// workspace summary (internal/app App.Summary) and small endpoint replies.
// The structs themselves are generated into ./domain.ts.
import type * as D from './domain.ts'

export type * from './domain.ts'

/** A stored record (domain.Record) whose `data` is the struct for its kind. */
export type Rec<T> = Omit<D.DomainRecord, 'data'> & { data: T }

/** Data of "event" records (store.Event). */
export interface EventData {
  action: string
  detail: unknown
}

/** Data of "preference" records (app.SetPreference and provider order). */
export interface PreferenceData {
  key: string
  value?: unknown
  ids?: string[]
  imported_candidate?: boolean
}

/**
 * GET /api/summary: every record list of the current project plus the
 * user-wide lists. Go writes an empty list as null.
 */
export interface Summary {
  project?: Rec<D.Project>
  paper?: Rec<D.Paper>[] | null
  source?: Rec<D.Source>[] | null
  knowledge?: Rec<D.Knowledge>[] | null
  request?: Rec<D.Request>[] | null
  /** Template preview decks, stored like artifacts. */
  preview?: Rec<D.Artifact>[] | null
  task?: Rec<D.Task>[] | null
  step?: Rec<D.Step>[] | null
  artifact?: Rec<D.Artifact>[] | null
  folder?: Rec<D.Folder>[] | null
  event?: Rec<EventData>[] | null
  profile?: Rec<D.Profile>[] | null
  profile_version?: Rec<D.ProfileVersion>[] | null
  template?: Rec<D.Template>[] | null
  provider?: Rec<D.Provider>[] | null
  preference?: Rec<PreferenceData>[] | null
}

/** GET /api/session */
export interface Session {
  owner_user_id: string
  csrf_token: string
  single_user: boolean
}
