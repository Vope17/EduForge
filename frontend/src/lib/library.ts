// Library helpers: chapters (one level per course).
import { reactive } from 'vue'
import type { Chapter, Lesson, Rec, Summary } from '../api/types.ts'
import type { Workspace } from './workspace.ts'

// Teaching order: unfiled lessons first, then each chapter in position order,
// each with its lessons in position order. Ties keep the order they arrive in
// (creation order), as the server does.
const byPosition = <T extends { data: { position: number } }>(items: T[]): T[] =>
  items.map((item, index) => ({ item, index })).sort((a, b) => a.item.data.position - b.item.data.position || a.index - b.index).map(x => x.item)

export function chapterList(snapshot: Summary): Rec<Chapter>[] {
  return byPosition(snapshot.chapter || [])
}

export interface LessonGroup {
  key: string
  chapter: Rec<Chapter> | null
  lessons: Rec<Lesson>[]
}

// Groups lessons for display and teaching order: the unfiled lessons first
// (key ''), then every chapter. A lesson whose chapter no longer exists counts
// as unfiled.
export function groupLessons(lessons: Rec<Lesson>[], chapters: Rec<Chapter>[]): LessonGroup[] {
  const ids = new Set(chapters.map(c => c.id))
  const groups: LessonGroup[] = [{ key: '', chapter: null, lessons: byPosition(lessons.filter(l => !ids.has(l.data.chapter ?? ''))) }]
  for (const chapter of chapters) groups.push({ key: chapter.id, chapter, lessons: byPosition(lessons.filter(l => l.data.chapter === chapter.id)) })
  return groups
}

// The lessons in teaching order.
export function orderedLessons(lessons: Rec<Lesson>[], chapters: Rec<Chapter>[]): Rec<Lesson>[] {
  return groupLessons(lessons, chapters).flatMap(g => g.lessons)
}

/** The body of PUT /courses/{id}/order. */
export interface CourseOrder {
  chapters: string[]
  groups: { chapter: string; lessons: string[] }[]
}

export function orderOf(groups: LessonGroup[]): CourseOrder {
  return {
    chapters: groups.filter(g => g.chapter).map(g => g.key),
    groups: groups.map(g => ({ chapter: g.key, lessons: g.lessons.map(l => l.id) })),
  }
}

// Moves a lesson to a chapter ('' for unfiled) just before another lesson, or
// to the end when before is empty or not in that chapter. Returns a new order.
export function moveLessonTo(order: CourseOrder, lessonID: string, chapter: string, before = ''): CourseOrder {
  const groups = order.groups.map(g => ({ chapter: g.chapter, lessons: g.lessons.filter(id => id !== lessonID) }))
  let target = groups.find(g => g.chapter === chapter)
  if (!target) { target = { chapter, lessons: [] }; groups.push(target) }
  const at = target.lessons.indexOf(before)
  if (at >= 0) target.lessons.splice(at, 0, lessonID); else target.lessons.push(lessonID)
  return { chapters: order.chapters, groups }
}

// Moves a lesson one place earlier (-1) or later (+1) in the teaching order,
// crossing into the neighbouring chapter at the ends of a chapter.
export function moveLessonBy(order: CourseOrder, lessonID: string, offset: -1 | 1): CourseOrder {
  const sequence = [{ chapter: '', lessons: order.groups.find(g => g.chapter === '')?.lessons ?? [] }, ...order.chapters.map(c => ({ chapter: c, lessons: order.groups.find(g => g.chapter === c)?.lessons ?? [] }))]
  const flat = sequence.flatMap(g => g.lessons.map(id => ({ id, chapter: g.chapter })))
  const i = flat.findIndex(x => x.id === lessonID)
  const j = i + offset
  if (i < 0 || j < 0 || j >= flat.length) return order
  const neighbour = flat[j]
  // Swapping with the neighbour keeps both in their chapters unless the lesson
  // is first/last in its chapter, in which case it joins the neighbour's chapter.
  const here = flat[i].chapter
  if (neighbour.chapter === here) {
    const lessons = (order.groups.find(g => g.chapter === here)?.lessons ?? []).slice()
    const a = lessons.indexOf(lessonID), b = lessons.indexOf(neighbour.id);
    [lessons[a], lessons[b]] = [lessons[b], lessons[a]]
    return { chapters: order.chapters, groups: order.groups.map(g => g.chapter === here ? { chapter: here, lessons } : g) }
  }
  return moveLessonTo(order, lessonID, neighbour.chapter, offset > 0 ? (order.groups.find(g => g.chapter === neighbour.chapter)?.lessons[0] ?? '') : '')
}

// Moves a chapter one place earlier (-1) or later (+1).
export function moveChapterBy(order: CourseOrder, chapterID: string, offset: -1 | 1): CourseOrder {
  const chapters = order.chapters.slice()
  const i = chapters.indexOf(chapterID), j = i + offset
  if (i < 0 || j < 0 || j >= chapters.length) return order;
  [chapters[i], chapters[j]] = [chapters[j], chapters[i]]
  return { chapters, groups: order.groups }
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
    // Saves a new teaching order (drag and drop or the arrows).
    async reorder(order: CourseOrder) {
      await act(() => api(`/courses/${state.courseID}/order`, { method: 'PUT', body: order }), { success: false })
    },
    async createChapter() {
      const name = prompt(t('chapterName'))?.trim()
      if (name) await act(() => api(`/courses/${state.courseID}/chapters`, { method: 'POST', body: { name } }))
    },
    async renameChapter(chapter: Rec<Chapter>) {
      const name = prompt(t('chapterName'), chapter.data.name)?.trim()
      if (name && name !== chapter.data.name) await act(() => api(`/chapters/${chapter.id}`, { method: 'PUT', body: { name, settings: chapter.data.settings } }))
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
