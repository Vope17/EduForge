<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { paperSteps, nextStep } from '../../lib/status.ts'
import { isRead, folderList, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import StepSources from '../paper/StepSources.vue'
import StepOutputs from '../paper/StepOutputs.vue'
import KnowledgePanel from '../KnowledgePanel.vue'
import ReproductionPlan from '../plan/ReproductionPlan.vue'
import type { IconName } from '../../lib/icons.ts'
import type { Step } from '../../lib/router.ts'
import type { Paper } from '../../api/types.ts'

const workspace = useWorkspace()
const { state, route, go, t, api, act, paper, openPaper, explain } = workspace
const { movePaper } = libraryActions(workspace)
const folders = computed(() => folderList(state.snapshot))
const steps = computed(() => paperSteps(paper.value, state.snapshot))
const current = computed(() => steps.value.find(s => s.key === route.step) || steps.value[0])
const icons: Record<Step, IconName> = { sources: 'file', knowledge: 'brain', outputs: 'layers', experiment: 'flask' }
// Where to send the user to unblock the current step: the first step that
// still needs work (a blocked step's direct predecessor may itself be blocked).
const fixStep = computed(() => {
  const target = nextStep(steps.value)
  return target !== current.value?.key ? target : ''
})
const read = computed(() => !!paper.value && isRead(state.snapshot, paper.value.id))
async function updatePaper(patch: Partial<Paper>) {
  if (!paper.value) return
  const { id, version, data } = paper.value
  await act(() => api(`/papers/${id}`, { method: 'PUT', body: { version, paper: { ...data, ...patch } } }))
}
async function removePaper() {
  if (!paper.value) return
  const { id, data } = paper.value
  if (!confirm(t('deletePaperConfirm').replace('{title}', data.title))) return
  const result = await act(() => api(`/papers/${id}?confirm=delete-paper-data`, { method: 'DELETE' }))
  if (result) go({ name: 'home', project: state.projectID }, { replace: true })
}
async function toggleRead() {
  if (!paper.value) return
  const id = paper.value.id
  await act(() => api(`/papers/${id}/reading`, { method: 'POST', body: { read: !read.value } }))
}
</script>

<template>
  <section v-if="paper" class="page-content paper-view">
    <div class="paper-head">
      <div class="grow">
        <h1><span v-if="read" class="read-mark" :title="t('readDone')" :aria-label="t('readDone')"><Icon name="check" /></span>{{ paper.data.title }}</h1>
        <div class="paper-meta">
          <label class="inline-field">{{ t('group') }}
            <input :value="paper.data.group" :placeholder="t('independent')" @change="updatePaper({ group: ($event.target as HTMLInputElement).value })">
          </label>
          <label class="inline-field wide">{{ t('repo') }}
            <input :value="paper.data.repo" placeholder="https://…" @change="updatePaper({ repo: ($event.target as HTMLInputElement).value })">
          </label>
          <label v-if="folders.length" class="inline-field">{{ t('folderLabel') }}
            <select :value="folders.some(f => f.id === paper!.data.folder) ? paper.data.folder : ''" @change="movePaper(paper!.id, ($event.target as HTMLSelectElement).value)">
              <option value="">{{ t('unfiled') }}</option>
              <option v-for="folder in folders" :key="folder.id" :value="folder.id">{{ folder.data.name }}</option>
            </select>
          </label>
          <button class="chip-button" :class="{ on: read }" :title="t(read ? 'unread' : 'markRead')" @click="toggleRead">✓ {{ t(read ? 'readDone' : 'markRead') }}</button>
        </div>
      </div>
      <button class="text-button danger" @click="removePaper">{{ t('deletePaper') }}</button>
    </div>

    <ol class="stepper" role="tablist">
      <li v-for="(step, i) in steps" :key="step.key" :class="[step.state, { current: step.key === route.step }]">
        <button role="tab" :aria-selected="step.key === route.step" @click="openPaper(paper.id, step.key)">
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
      <button v-if="fixStep" class="primary" @click="openPaper(paper.id, fixStep)">{{ t('goTo') }} {{ t('step_' + fixStep) }} →</button>
    </div>

    <div class="subpage" :key="route.step">
      <StepSources v-if="route.step === 'sources'" />
      <KnowledgePanel v-else-if="route.step === 'knowledge' && current.state !== 'blocked'" />
      <StepOutputs v-else-if="route.step === 'outputs' && current.state !== 'blocked'" />
      <ReproductionPlan v-else-if="route.step === 'experiment' && current.state !== 'blocked'" />
    </div>
  </section>
</template>
