<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { lessonSteps, nextStep } from '../../lib/status.ts'
import { chapterList, groupLessons, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import StepDots from '../lesson/StepDots.vue'
import NewLessonDialog from '../dialogs/NewLessonDialog.vue'
import NewCourseDialog from '../dialogs/NewCourseDialog.vue'
import type { Lesson, Rec } from '../../api/types.ts'

const workspace = useWorkspace()
const { state, t, go, openLesson } = workspace
const { createChapter, moveLesson, deleteCourse } = libraryActions(workspace)
const chapters = computed(() => chapterList(state.snapshot))
const groups = computed(() => groupLessons(lessons.value, chapters.value).filter(g => g.chapter || g.lessons.length))
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
      <section v-for="group in groups" :key="group.key" class="lesson-chapter">
        <h2 v-if="chapters.length" class="chapter-heading"><Icon name="chapter" /> {{ group.chapter ? group.chapter.data.name : t('unfiled') }} <span class="count">{{ group.lessons.length }}</span></h2>
        <p v-if="!group.lessons.length" class="empty-inline">{{ t('chapterEmpty') }}</p>
        <div class="lesson-grid">
          <article v-for="lesson in group.lessons" :key="lesson.id" class="lesson-card">
            <div class="lesson-card-top">
              <label class="check"><input v-model="state.selected" type="checkbox" :value="lesson.id" :aria-label="t('selectForBatch') + ' · ' + lesson.data.title"></label>
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
