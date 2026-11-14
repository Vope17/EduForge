<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { lessonSteps, nextStep } from '../../lib/status.ts'
import { chapterList, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import StepSources from '../lesson/StepSources.vue'
import StepOutputs from '../lesson/StepOutputs.vue'
import KnowledgePanel from '../KnowledgePanel.vue'
import type { IconName } from '../../lib/icons.ts'
import type { Step } from '../../lib/router.ts'
import type { Lesson } from '../../api/types.ts'

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
async function updateLesson(patch: Partial<Lesson>) {
  if (!lesson.value) return
  const { id, version, data } = lesson.value
  await act(() => api(`/lessons/${id}`, { method: 'PUT', body: { version, lesson: { ...data, ...patch } } }))
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
          <label class="inline-field">{{ t('group') }}
            <input :value="lesson.data.group" :placeholder="t('independent')" @change="updateLesson({ group: ($event.target as HTMLInputElement).value })">
          </label>
          <label class="inline-field wide">{{ t('repo') }}
            <input :value="lesson.data.repo" placeholder="https://…" @change="updateLesson({ repo: ($event.target as HTMLInputElement).value })">
          </label>
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
