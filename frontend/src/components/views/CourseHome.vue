<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { lessonSteps, nextStep } from '../../lib/status.ts'
import { chapterList, groupLessons, libraryActions, moveChapterBy, moveLessonBy, moveLessonTo, orderOf } from '../../lib/library.ts'
import { effectiveSettings, type SettingsOverride } from '../../lib/settings.ts'
import SettingsFields from '../settings/SettingsFields.vue'
import ArtifactList from '../generation/ArtifactList.vue'
import Icon from '../Icon.vue'
import StepDots from '../lesson/StepDots.vue'
import NewLessonDialog from '../dialogs/NewLessonDialog.vue'
import NewCourseDialog from '../dialogs/NewCourseDialog.vue'
import type { Chapter, Lesson, Rec } from '../../api/types.ts'

const workspace = useWorkspace()
const { state, t, api, act, go, openLesson } = workspace
const { createChapter, renameChapter, deleteChapter, moveLesson, reorder, deleteCourse } = libraryActions(workspace)
const chapters = computed(() => chapterList(state.snapshot))
// The unfiled group is shown only when it has lessons; empty chapters stay.
const allGroups = computed(() => groupLessons(lessons.value, chapters.value))
const groups = computed(() => allGroups.value.filter(g => g.chapter || g.lessons.length))
const order = computed(() => orderOf(allGroups.value))
const addingLesson = ref(false), addingCourse = ref(false), personal = ref(false)
const lessons = computed(() => state.snapshot.lesson || [])
const anyParsed = computed(() => lessons.value.some(p => p.data.knowledge_id))
const onboarding = computed(() => [
  { key: 'connect', done: state.connection.mode === 'model', label: t('onboardConnect'), action: () => go({ name: 'settings', tab: 'providers' }) },
  { key: 'course', done: !!state.courseID, label: t('onboardCourse'), action: () => { addingCourse.value = true } },
  { key: 'lesson', done: lessons.value.length > 0, label: t('onboardLesson'), action: () => { addingLesson.value = true } },
  { key: 'parse', done: anyParsed.value, label: t('onboardParse'), action: () => lessons.value[0] && openLesson(lessons.value[0].id, 'sources') },
])
const showOnboarding = computed(() => onboarding.value.some(step => !step.done))
// Course defaults and chapter overrides.
const course = computed(() => state.snapshot.course)
const settingsOpen = ref(false), chapterSettings = ref('')
async function saveCourseSettings(value: SettingsOverride) {
  const c = course.value
  if (!c) return
  await act(() => api(`/courses/${c.id}`, { method: 'PUT', body: { version: c.version, name: c.data.name, description: c.data.description, settings: { ...c.data.settings, ...value } } }))
}
async function saveChapterSettings(chapter: Rec<Chapter>, value: SettingsOverride) {
  await act(() => api(`/chapters/${chapter.id}`, { method: 'PUT', body: { name: chapter.data.name, settings: { ...chapter.data.settings, ...value } } }))
  chapterSettings.value = ''
}
// The course's syllabus: written from its lessons in teaching order.
const syllabi = computed(() => (state.snapshot.artifact || []).filter(a => a.data.type === 'S').slice().reverse())
const weeks = ref(16)
const syllabusBusy = computed(() => (state.snapshot.task || []).some(x => x.data.type === 'syllabus' && ['queued', 'running'].includes(x.data.status)))
async function writeSyllabus() {
  await act(() => api(`/courses/${state.courseID}/syllabus`, { method: 'POST', body: { weeks: weeks.value, language: state.language } }))
}
const courseDefaults = computed(() => effectiveSettings(course.value?.data.settings))
const inheritedForChapter = computed(() => courseDefaults.value)

// Teaching order: drag a lesson onto another lesson (it goes before it) or onto
// a chapter (it goes last); the arrows do the same for the keyboard.
const dragging = ref(''), over = ref('')
function onDragStart(event: DragEvent, id: string) {
  dragging.value = id
  event.dataTransfer?.setData('text/plain', id)
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}
function onDragEnd() { dragging.value = ''; over.value = '' }
function dropOn(chapter: string, before = '') {
  const id = dragging.value
  onDragEnd()
  if (!id || id === before) return
  void reorder(moveLessonTo(order.value, id, chapter, before))
}
const shiftLesson = (id: string, offset: -1 | 1) => reorder(moveLessonBy(order.value, id, offset))
const shiftChapter = (id: string, offset: -1 | 1) => reorder(moveChapterBy(order.value, id, offset))
function openNext(lesson: Rec<Lesson>) { openLesson(lesson.id, nextStep(lessonSteps(lesson, state.snapshot))) }
</script>

<template>
  <section class="page-content">
    <div class="page-heading">
      <div>
        <h1>{{ state.snapshot.course?.data.name || t('welcomeTitle') }}</h1>
        <p class="lede">{{ state.snapshot.course?.data.description || t('libraryIntro') }}</p>
      </div>
      <div v-if="state.courseID" class="button-row">
        <button class="secondary" @click="createChapter"><Icon name="chapterPlus" /> {{ t('newChapter') }}</button>
        <button class="primary" @click="addingLesson = true"><Icon name="plus" /> {{ t('addLesson') }}</button>
      </div>
      <button v-else class="primary" @click="addingCourse = true"><Icon name="plus" /> {{ t('newCourse') }}</button>
    </div>

    <section v-if="showOnboarding" class="panel onboarding">
      <h2>{{ t('onboardTitle') }}</h2>
      <ol class="onboard-list">
        <li v-for="(step, i) in onboarding" :key="step.key" :class="{ done: step.done }">
          <span class="onboard-mark">{{ step.done ? '✓' : i + 1 }}</span>
          <span class="grow">{{ step.label }}</span>
          <button v-if="!step.done" class="secondary small" @click="step.action()">{{ t('doIt') }} →</button>
        </li>
      </ol>
    </section>

    <template v-if="state.courseID">
      <section v-if="course" class="panel course-settings">
        <button type="button" class="chapter-toggle" :aria-expanded="settingsOpen" @click="settingsOpen = !settingsOpen">
          <span class="chapter-caret" :class="{ closed: !settingsOpen }">▾</span> <strong>{{ t('courseSettings') }}</strong>
          <span class="caption">{{ courseDefaults.audience || '—' }} · {{ t('level_' + courseDefaults.level) }} · {{ courseDefaults.minutes }} {{ t('minutesShort') }} · {{ t('depth_' + courseDefaults.depth) }}</span>
        </button>
        <template v-if="settingsOpen">
          <p class="caption">{{ t('courseSettingsHint') }}</p>
          <SettingsFields mode="default" :model-value="course.data.settings" :busy="!!state.busy" @save="saveCourseSettings" />
        </template>
      </section>
      <section v-if="lessons.length" class="panel syllabus-panel">
        <div class="section-heading"><h2>{{ t('S') }}</h2></div>
        <p class="caption">{{ t('syllabusHint') }}</p>
        <div class="button-row">
          <label class="inline-field">{{ t('syllabusWeeks') }}<input v-model.number="weeks" type="number" min="1" max="40"></label>
          <button class="secondary" :disabled="!!state.busy || syllabusBusy || state.connection.mode === 'none'" @click="writeSyllabus">{{ t('generateSyllabus') }} →</button>
        </div>
        <ArtifactList v-if="syllabi.length" :artifacts="syllabi" />
      </section>
      <p v-if="lessons.length > 1" class="caption order-hint">{{ t('teachingOrderHint') }}</p>
      <section v-for="group in groups" :key="group.key" class="lesson-chapter" :class="{ 'drop-target': over === 'group:' + group.key }" @dragover.prevent="over = 'group:' + group.key" @dragleave.self="over = ''" @drop.prevent.stop="dropOn(group.key)">
        <div v-if="chapters.length" class="chapter-head">
          <h2 class="chapter-heading"><Icon name="chapter" /> {{ group.chapter ? group.chapter.data.name : t('unfiled') }} <span class="count">{{ group.lessons.length }}</span></h2>
          <div v-if="group.chapter" class="button-row chapter-tools">
            <button class="icon-button" :title="t('moveUp')" :aria-label="t('moveUp') + ' · ' + group.chapter.data.name" @click="shiftChapter(group.key, -1)">↑</button>
            <button class="icon-button" :title="t('moveDown')" :aria-label="t('moveDown') + ' · ' + group.chapter.data.name" @click="shiftChapter(group.key, 1)">↓</button>
            <button class="text-button" @click="chapterSettings = chapterSettings === group.key ? '' : group.key">{{ t('chapterSettings') }}</button>
            <button class="text-button" @click="renameChapter(group.chapter)">{{ t('renameChapter') }}</button>
            <button class="text-button danger" @click="deleteChapter(group.chapter)">{{ t('deleteChapter') }}</button>
          </div>
        </div>
        <div v-if="group.chapter && chapterSettings === group.key" class="panel chapter-settings">
          <SettingsFields mode="override" :model-value="group.chapter.data.settings" :inherited="inheritedForChapter" :busy="!!state.busy" @save="saveChapterSettings(group.chapter, $event)" />
        </div>
        <p v-if="!group.lessons.length" class="empty-inline">{{ t('chapterEmpty') }}</p>
        <div class="lesson-grid">
          <article v-for="lesson in group.lessons" :key="lesson.id" class="lesson-card" :class="{ dragging: dragging === lesson.id, 'drop-before': over === lesson.id }" draggable="true" @dragstart="onDragStart($event, lesson.id)" @dragend="onDragEnd" @dragover.prevent.stop="over = lesson.id" @dragleave.self="over = ''" @drop.prevent.stop="dropOn(group.key, lesson.id)">
            <div class="lesson-card-top">
              <label class="check"><input v-model="state.selected" type="checkbox" :value="lesson.id" :aria-label="t('selectForBatch') + ' · ' + lesson.data.title"></label>
              <span class="button-row">
                <button class="icon-button" :title="t('moveUp')" :aria-label="t('moveUp') + ' · ' + lesson.data.title" @click="shiftLesson(lesson.id, -1)">←</button>
                <button class="icon-button" :title="t('moveDown')" :aria-label="t('moveDown') + ' · ' + lesson.data.title" @click="shiftLesson(lesson.id, 1)">→</button>
              </span>
            </div>
            <h2><button class="lesson-title" @click="openNext(lesson)">{{ lesson.data.title }}</button></h2>
            <StepDots :lesson="lesson" labelled />
            <div class="lesson-card-actions">
              <select v-if="chapters.length" class="chapter-select" :aria-label="t('moveTo') + ' · ' + lesson.data.title" :value="group.key" @change="moveLesson(lesson.id, ($event.target as HTMLSelectElement).value)">
                <option value="">{{ t('unfiled') }}</option>
                <option v-for="chapter in chapters" :key="chapter.id" :value="chapter.id">{{ chapter.data.name }}</option>
              </select>
              <span v-else class="caption">{{ t('unfiled') }}</span>
              <button class="text-button" @click="openNext(lesson)">{{ t('continueWork') }} →</button>
            </div>
          </article>
        </div>
      </section>
      <div v-if="!lessons.length" class="empty-state">
        <h2>{{ t('addLesson') }}</h2>
        <p>{{ t('libraryIntro') }}</p>
      </div>

      <footer class="course-footer">
        <label class="check"><input v-model="personal" type="checkbox">{{ t('includePersonal') }}</label>
        <a class="text-button" :href="`/api/courses/${state.courseID}/export?personal=${personal}`">{{ t('exportCourse') }} ↗</a>
        <button class="text-button danger" @click="deleteCourse"><Icon name="trash" /> {{ t('deleteCourseButton') }}</button>
      </footer>
    </template>
    <NewLessonDialog v-if="addingLesson" @close="addingLesson = false" />
    <NewCourseDialog v-if="addingCourse" @close="addingCourse = false" />
  </section>
</template>
