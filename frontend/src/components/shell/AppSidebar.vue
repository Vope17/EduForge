<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { lessonSteps } from '../../lib/status.ts'
import { chapterList, groupLessons, collapsed, toggleCollapsed, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import NewLessonDialog from '../dialogs/NewLessonDialog.vue'
import NewCourseDialog from '../dialogs/NewCourseDialog.vue'
import type { Lesson, Rec } from '../../api/types.ts'

const workspace = useWorkspace()
const { state, route, go, t, setLanguage, openLesson } = workspace
const { createChapter, renameChapter, deleteChapter, deleteCourse, moveLesson } = libraryActions(workspace)
const addingLesson = ref(false), addingCourse = ref(false)
const lessons = computed(() => state.snapshot.lesson || [])
const steps = (lesson: Rec<Lesson>) => lessonSteps(lesson, state.snapshot)
const chapters = computed(() => chapterList(state.snapshot))
const groups = computed(() => groupLessons(lessons.value, chapters.value))
// Drag a study onto a chapter (or onto "Unfiled") to move it.
const dragging = ref(''), dropTarget = ref<string | null>(null)
function dragStart(event: DragEvent, lesson: Rec<Lesson>) {
  dragging.value = lesson.id
  if (!event.dataTransfer) return
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', lesson.id)
}
function dragEnd() { dragging.value = ''; dropTarget.value = null }
function dragOver(event: DragEvent, key: string) {
  if (!dragging.value) return
  event.preventDefault()
  dropTarget.value = key
}
function drop(key: string) {
  const id = dragging.value
  dragEnd()
  if (id) void moveLesson(id, key)
}
function pickCourse(id: string) { state.selected = []; go({ name: 'home', course: id }) }
function toggle(id: string) {
  const i = state.selected.indexOf(id)
  if (i >= 0) state.selected.splice(i, 1); else state.selected.push(id)
}
</script>

<template>
  <div v-if="state.nav" class="nav-backdrop" @click="state.nav = false"></div>
  <aside class="sidebar" :class="{ open: state.nav }">
    <a class="brand" href="#/" @click.prevent="go({ name: 'home', course: state.courseID })">
      <span class="brand-symbol">E<span>F</span></span><span>Edu<span class="brand-light">Forge</span></span>
    </a>

    <div class="sidebar-label">{{ t('courses') }}</div>
    <div class="course-row">
      <label class="sr-only" for="course-picker">{{ t('courses') }}</label>
      <select id="course-picker" class="course-picker" :value="state.courseID" @change="pickCourse(($event.target as HTMLSelectElement).value)">
        <option value="" disabled>{{ t('selectCourse') }}</option>
        <option v-for="course in state.courses" :key="course.id" :value="course.id">{{ course.data.name }}</option>
      </select>
      <button v-if="!state.session?.read_only" class="side-icon-button" :title="t('newCourse')" :aria-label="t('newCourse')" @click="addingCourse = true"><Icon name="plus" /></button>
      <button v-if="state.courseID" class="side-icon-button" :title="t('deleteCourseButton')" :aria-label="t('deleteCourseButton')" @click="deleteCourse"><Icon name="trash" /></button>
    </div>

    <div class="sidebar-label lessons-label">
      <span>{{ t('library') }}</span>
      <span v-if="lessons.length" class="side-count">{{ lessons.length }}</span>
      <button v-if="state.courseID" class="side-icon-button small push-right" :title="t('newChapter')" :aria-label="t('newChapter')" @click="createChapter"><Icon name="chapterPlus" /></button>
    </div>
    <nav class="lesson-nav" :aria-label="t('library')">
      <template v-for="group in groups" :key="group.key">
        <div
          v-if="chapters.length && (group.chapter || group.lessons.length || dragging)"
          class="chapter-row" :class="{ 'drop-target': dropTarget === group.key }"
          @dragover="dragOver($event, group.key)" @dragleave="dropTarget = dropTarget === group.key ? null : dropTarget" @drop.prevent="drop(group.key)"
        >
          <button class="chapter-toggle" :aria-expanded="!collapsed[group.key || 'unfiled']" @click="toggleCollapsed(group.key || 'unfiled')">
            <span class="chapter-caret" :class="{ closed: collapsed[group.key || 'unfiled'] }">▾</span>
            <Icon name="chapter" />
            <span class="chapter-name">{{ group.chapter ? group.chapter.data.name : t('unfiled') }}</span>
            <span class="side-count">{{ group.lessons.length }}</span>
          </button>
          <span v-if="group.chapter" class="chapter-actions">
            <button class="side-icon-button small" :title="t('renameChapter')" :aria-label="t('renameChapter') + ' · ' + group.chapter.data.name" @click="renameChapter(group.chapter)"><Icon name="pencil" /></button>
            <button class="side-icon-button small" :title="t('deleteChapter')" :aria-label="t('deleteChapter') + ' · ' + group.chapter.data.name" @click="deleteChapter(group.chapter)"><Icon name="trash" /></button>
          </span>
        </div>
        <template v-if="!chapters.length || !collapsed[group.key || 'unfiled']">
          <div
            v-for="lesson in group.lessons" :key="lesson.id" class="lesson-link"
            :class="{ active: route.name === 'lesson' && route.lesson === lesson.id, checked: state.selected.includes(lesson.id), 'in-chapter': chapters.length, dragged: dragging === lesson.id }"
            draggable="true" @dragstart="dragStart($event, lesson)" @dragend="dragEnd"
            @dragover="dragOver($event, group.key)" @drop.prevent="drop(group.key)"
          >
            <input type="checkbox" :checked="state.selected.includes(lesson.id)" :aria-label="t('selectForBatch') + ' · ' + lesson.data.title" @change="toggle(lesson.id)">
            <button class="lesson-link-main" @click="openLesson(lesson.id)">
              <span class="lesson-link-title">{{ lesson.data.title }}</span>
              <span class="step-dots" aria-hidden="true">
                <i v-for="step in steps(lesson)" :key="step.key" :class="step.state"></i>
              </span>
            </button>
          </div>
          <p v-if="group.chapter && !group.lessons.length" class="chapter-empty">{{ t('chapterEmpty') }}</p>
        </template>
      </template>
      <p v-if="state.courseID && !lessons.length" class="side-empty">{{ t('noLessonsYet') }}</p>
    </nav>
    <button v-if="state.courseID && !state.session?.read_only" class="sidebar-new" @click="addingLesson = true"><Icon name="plus" /> {{ t('addLesson') }}</button>
    <button v-if="state.selected.length" class="batch-button" @click="go({ name: 'batch', course: state.courseID })">
      <Icon name="layers" /> {{ t('batchStudio') }} · {{ state.selected.length }}
    </button>

    <div class="sidebar-bottom">
      <button v-if="!state.session?.read_only" class="side-link" :class="{ active: route.name === 'settings' }" @click="go({ name: 'settings', tab: 'providers' })">
        <Icon name="gear" /> {{ t('settings') }}
      </button>
      <div class="language-switch">
        <button :class="{ active: state.language === 'zh-TW' }" @click="setLanguage('zh-TW')">繁中</button>
        <button :class="{ active: state.language === 'en' }" @click="setLanguage('en')">EN</button>
      </div>
    </div>
    <NewLessonDialog v-if="addingLesson" @close="addingLesson = false" />
    <NewCourseDialog v-if="addingCourse" @close="addingCourse = false" />
  </aside>
</template>
