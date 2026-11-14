// Library helpers: chapters (one level per course).
import { reactive } from 'vue'
import type { Chapter, Lesson, Rec, Summary } from '../api/types.ts'
import type { Workspace } from './workspace.ts'

export function chapterList(snapshot: Summary): Rec<Chapter>[] {
  return (snapshot.chapter || []).slice().sort((a, b) => a.data.name.localeCompare(b.data.name))
}

export interface LessonGroup {
  key: string
  chapter: Rec<Chapter> | null
  lessons: Rec<Lesson>[]
}

// Groups lessons by chapter: every chapter in name order, then unfiled studies.
// A lesson whose chapter no longer exists counts as unfiled.
export function groupLessons(lessons: Rec<Lesson>[], chapters: Rec<Chapter>[]): LessonGroup[] {
  const ids = new Set(chapters.map(f => f.id))
  const groups: LessonGroup[] = chapters.map(chapter => ({ key: chapter.id, chapter, lessons: lessons.filter(p => p.data.chapter === chapter.id) }))
  groups.push({ key: '', chapter: null, lessons: lessons.filter(p => !ids.has(p.data.chapter ?? '')) })
  return groups
}

const COLLAPSED_KEY = 'eduforge-collapsed-chapters'
function loadCollapsed(): Record<string, boolean> {
  try { return JSON.parse(localStorage.getItem(COLLAPSED_KEY) || '{}') || {} } catch { return {} }
}
// Which chapters are collapsed, remembered per browser.
export const collapsed = reactive(loadCollapsed())
export function toggleCollapsed(id: string) {
  collapsed[id] = !collapsed[id]
  if (!collapsed[id]) delete collapsed[id]
  try { localStorage.setItem(COLLAPSED_KEY, JSON.stringify(collapsed)) } catch {}
}

// Chapter and move actions bound to a workspace ({ state, api, act, t }).
export function libraryActions({ state, api, act, t, go }: Pick<Workspace, 'state' | 'api' | 'act' | 't' | 'go'>) {
  return {
    // Deletes the current course after a confirmation that names it.
    async deleteCourse() {
      const course = state.courses.find(x => x.id === state.courseID)
      if (!course || !confirm(t('deleteCourse').replace('{name}', course.data.name))) return
      const result = await act(() => api(`/courses/${course.id}?confirm=delete-course-data`, { method: 'DELETE' }))
      if (result) go({ name: 'home', course: '' })
    },
    async createChapter() {
      const name = prompt(t('chapterName'))?.trim()
      if (name) await act(() => api(`/courses/${state.courseID}/chapters`, { method: 'POST', body: { name } }))
    },
    async renameChapter(chapter: Rec<Chapter>) {
      const name = prompt(t('chapterName'), chapter.data.name)?.trim()
      if (name && name !== chapter.data.name) await act(() => api(`/chapters/${chapter.id}`, { method: 'PUT', body: { name } }))
    },
    async deleteChapter(chapter: Rec<Chapter>) {
      if (confirm(t('deleteChapterConfirm').replace('{name}', chapter.data.name))) await act(() => api(`/chapters/${chapter.id}`, { method: 'DELETE' }))
    },
    async moveLesson(lessonID: string, chapterID: string) {
      const lesson = (state.snapshot.lesson || []).find(p => p.id === lessonID)
      if (!lesson || (lesson.data.chapter || '') === chapterID) return
      await act(() => api(`/lessons/${lessonID}/chapter`, { method: 'PUT', body: { chapter: chapterID } }))
    },
  }
}
