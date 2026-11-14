// Library helpers: folders (one level per project).
import { reactive } from 'vue'
import type { Folder, Paper, Rec, Summary } from '../api/types.ts'
import type { Workspace } from './workspace.ts'

export function folderList(snapshot: Summary): Rec<Folder>[] {
  return (snapshot.folder || []).slice().sort((a, b) => a.data.name.localeCompare(b.data.name))
}

export interface PaperGroup {
  key: string
  folder: Rec<Folder> | null
  papers: Rec<Paper>[]
}

// Groups papers by folder: every folder in name order, then unfiled studies.
// A paper whose folder no longer exists counts as unfiled.
export function groupPapers(papers: Rec<Paper>[], folders: Rec<Folder>[]): PaperGroup[] {
  const ids = new Set(folders.map(f => f.id))
  const groups: PaperGroup[] = folders.map(folder => ({ key: folder.id, folder, papers: papers.filter(p => p.data.folder === folder.id) }))
  groups.push({ key: '', folder: null, papers: papers.filter(p => !ids.has(p.data.folder ?? '')) })
  return groups
}

const COLLAPSED_KEY = 'eduforge-collapsed-folders'
function loadCollapsed(): Record<string, boolean> {
  try { return JSON.parse(localStorage.getItem(COLLAPSED_KEY) || '{}') || {} } catch { return {} }
}
// Which folders are collapsed, remembered per browser.
export const collapsed = reactive(loadCollapsed())
export function toggleCollapsed(id: string) {
  collapsed[id] = !collapsed[id]
  if (!collapsed[id]) delete collapsed[id]
  try { localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsed)) } catch {}
}

// Folder and move actions bound to a workspace ({ state, api, act, t }).
export function libraryActions({ state, api, act, t, go }: Pick<Workspace, 'state' | 'api' | 'act' | 't' | 'go'>) {
  return {
    // Deletes the current project after a confirmation that names it.
    async deleteProject() {
      const project = state.projects.find(x => x.id === state.projectID)
      if (!project || !confirm(t('deleteProject').replace('{name}', project.data.name))) return
      const result = await act(() => api(`/projects/${project.id}?confirm=delete-project-data`, { method: 'DELETE' }))
      if (result) go({ name: 'home', project: '' })
    },
    async createFolder() {
      const name = prompt(t('folderName'))?.trim()
      if (name) await act(() => api(`/projects/${state.projectID}/folders`, { method: 'POST', body: { name } }))
    },
    async renameFolder(folder: Rec<Folder>) {
      const name = prompt(t('folderName'), folder.data.name)?.trim()
      if (name && name !== folder.data.name) await act(() => api(`/folders/${folder.id}`, { method: 'PUT', body: { name } }))
    },
    async deleteFolder(folder: Rec<Folder>) {
      if (confirm(t('deleteFolderConfirm').replace('{name}', folder.data.name))) await act(() => api(`/folders/${folder.id}`, { method: 'DELETE' }))
    },
    async movePaper(paperID: string, folderID: string) {
      const paper = (state.snapshot.paper || []).find(p => p.id === paperID)
      if (!paper || (paper.data.folder || '') === folderID) return
      await act(() => api(`/papers/${paperID}/folder`, { method: 'PUT', body: { folder: folderID } }))
    },
  }
}
