<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { lessonSteps, nextStep } from '../../lib/status.ts'
import { chapterList, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import StepSources from '../lesson/StepSources.vue'
import StepOutputs from '../lesson/StepOutputs.vue'
import KnowledgePanel from '../KnowledgePanel.vue'
import SettingsFields from '../settings/SettingsFields.vue'
import { effectiveSettings, type SettingsOverride } from '../../lib/settings.ts'
import type { IconName } from '../../lib/icons.ts'
import type { Step } from '../../lib/router.ts'

const workspace = useWorkspace()
const { state, route, go, t, api, act, lesson, openLesson, explain } = workspace
const { moveLesson } = libraryActions(workspace)
const chapters = computed(() => chapterList(state.snapshot))
const steps = computed(() => lessonSteps(lesson.value, state.snapshot))
const current = computed(() => steps.value.find(s => s.key === route.step) || steps.value[0])
const icons: Record<Step, IconName> = { sources: 'file', knowledge: 'brain', outputs: 'layers' }
// Where to send the user to unblock the current step: the first step that
// still needs work (a blocked step's direct predecessor may itself be blocked).
const fixStep = computed(() => {
  const target = nextStep(steps.value)
  return target !== current.value?.key ? target : ''
})
// This lesson's own settings, over its chapter's and the course's.
const inherited = computed(() => {
  const chapter = chapters.value.find(c => c.id === lesson.value?.data.chapter)
  return effectiveSettings(state.snapshot.course?.data.settings, chapter?.data.settings)
})
async function saveSettings(value: SettingsOverride) {
  const l = lesson.value
  if (!l) return
  await act(() => api(`/lessons/${l.id}`, { method: 'PUT', body: { version: l.version, lesson: { ...l.data, settings: { ...l.data.settings, ...value } } } }))
}
async function removeLesson() {
  if (!lesson.value) return
  const { id, data } = lesson.value
  if (!confirm(t('deleteLessonConfirm').replace('{title}', data.title))) return
  const result = await act(() => api(`/lessons/${id}?confirm=delete-lesson-data`, { method: 'DELETE' }))
  if (result) go({ name: 'home', course: state.courseID }, { replace: true })
}
</script>

<template>
  <section v-if="lesson" class="page-content lesson-view">
    <div class="lesson-head">
      <div class="grow">
        <h1>{{ lesson.data.title }}</h1>
        <div class="lesson-meta">
          <label v-if="chapters.length" class="inline-field">{{ t('chapterLabel') }}
            <select :value="chapters.some(f => f.id === lesson!.data.chapter) ? lesson.data.chapter : ''" @change="moveLesson(lesson!.id, ($event.target as HTMLSelectElement).value)">
              <option value="">{{ t('unfiled') }}</option>
              <option v-for="chapter in chapters" :key="chapter.id" :value="chapter.id">{{ chapter.data.name }}</option>
            </select>
          </label>
        </div>
      </div>
      <button class="text-button danger" @click="removeLesson">{{ t('deleteLesson') }}</button>
    </div>

    <details class="panel lesson-settings">
      <summary>{{ t('lessonSettings') }}</summary>
      <p class="caption">{{ t('courseSettingsHint') }}</p>
      <SettingsFields mode="override" :model-value="lesson.data.settings" :inherited="inherited" :busy="!!state.busy" @save="saveSettings" />
    </details>

    <ol class="stepper" role="tablist">
      <li v-for="(step, i) in steps" :key="step.key" :class="[step.state, { current: step.key === route.step }]">
        <button role="tab" :aria-selected="step.key === route.step" @click="openLesson(lesson.id, step.key)">
          <span class="step-badge">
            <template v-if="step.state === 'done'">✓</template>
            <Icon v-else :name="icons[step.key]" />
          </span>
          <span class="step-text">
            <span class="step-name"><small>{{ i + 1 }}</small> {{ t('step_' + step.key) }}</span>
            <span class="step-state">{{ step.reason ? explain(step.reason) : t('state_' + step.state) }}</span>
          </span>
        </button>
      </li>
    </ol>

    <div v-if="current.state === 'blocked'" class="blocked-card">
      <Icon name="lock" />
      <div class="grow">
        <strong>{{ t('stepBlocked') }}</strong>
        <p>{{ explain(current.reason ?? '') }}</p>
      </div>
      <button v-if="fixStep" class="primary" @click="openLesson(lesson.id, fixStep)">{{ t('goTo') }} {{ t('step_' + fixStep) }} →</button>
    </div>

    <div class="subpage" :key="route.step">
      <StepSources v-if="route.step === 'sources'" />
      <KnowledgePanel v-else-if="route.step === 'knowledge' && current.state !== 'blocked'" />
      <StepOutputs v-else-if="route.step === 'outputs' && current.state !== 'blocked'" />
    </div>
  </section>
</template>
