<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { paperSteps, nextStep } from '../../lib/status.ts'
import { folderList, groupPapers, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import StepDots from '../paper/StepDots.vue'
import NewPaperDialog from '../dialogs/NewPaperDialog.vue'
import NewProjectDialog from '../dialogs/NewProjectDialog.vue'
import type { Paper, Rec } from '../../api/types.ts'

const workspace = useWorkspace()
const { state, t, go, openPaper } = workspace
const { createFolder, movePaper, deleteProject } = libraryActions(workspace)
const folders = computed(() => folderList(state.snapshot))
const groups = computed(() => groupPapers(papers.value, folders.value).filter(g => g.folder || g.papers.length))
const addingPaper = ref(false), addingProject = ref(false), personal = ref(false)
const papers = computed(() => state.snapshot.paper || [])
const anyParsed = computed(() => papers.value.some(p => p.data.knowledge_id))
const onboarding = computed(() => [
  { key: 'connect', done: state.connection.mode === 'model', label: t('onboardConnect'), action: () => go({ name: 'settings', tab: 'providers' }) },
  { key: 'project', done: !!state.projectID, label: t('onboardProject'), action: () => { addingProject.value = true } },
  { key: 'paper', done: papers.value.length > 0, label: t('onboardPaper'), action: () => { addingPaper.value = true } },
  { key: 'parse', done: anyParsed.value, label: t('onboardParse'), action: () => papers.value[0] && openPaper(papers.value[0].id, 'sources') },
])
const showOnboarding = computed(() => onboarding.value.some(step => !step.done))
function openNext(paper: Rec<Paper>) { openPaper(paper.id, nextStep(paperSteps(paper, state.snapshot))) }
</script>

<template>
  <section class="page-content">
    <div class="page-heading">
      <div>
        <h1>{{ state.snapshot.project?.data.name || t('welcomeTitle') }}</h1>
        <p class="lede">{{ state.snapshot.project?.data.description || t('libraryIntro') }}</p>
      </div>
      <div v-if="state.projectID" class="button-row">
        <button class="secondary" @click="createFolder"><Icon name="folderPlus" /> {{ t('newFolder') }}</button>
        <button class="primary" @click="addingPaper = true"><Icon name="plus" /> {{ t('addPaper') }}</button>
      </div>
      <button v-else class="primary" @click="addingProject = true"><Icon name="plus" /> {{ t('newProject') }}</button>
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

    <template v-if="state.projectID">
      <section v-for="group in groups" :key="group.key" class="paper-folder">
        <h2 v-if="folders.length" class="folder-heading"><Icon name="folder" /> {{ group.folder ? group.folder.data.name : t('unfiled') }} <span class="count">{{ group.papers.length }}</span></h2>
        <p v-if="!group.papers.length" class="empty-inline">{{ t('folderEmpty') }}</p>
        <div class="paper-grid">
          <article v-for="paper in group.papers" :key="paper.id" class="paper-card">
            <div class="paper-card-top">
              <label class="check"><input v-model="state.selected" type="checkbox" :value="paper.id" :aria-label="t('selectForBatch') + ' · ' + paper.data.title"></label>
            </div>
            <h2><button class="paper-title" @click="openNext(paper)">{{ paper.data.title }}</button></h2>
            <StepDots :paper="paper" labelled />
            <div class="paper-card-actions">
              <select v-if="folders.length" class="folder-select" :aria-label="t('moveTo') + ' · ' + paper.data.title" :value="group.key" @change="movePaper(paper.id, ($event.target as HTMLSelectElement).value)">
                <option value="">{{ t('unfiled') }}</option>
                <option v-for="folder in folders" :key="folder.id" :value="folder.id">{{ folder.data.name }}</option>
              </select>
              <span v-else class="caption">{{ paper.data.group || t('independent') }}</span>
              <button class="text-button" @click="openNext(paper)">{{ t('continueWork') }} →</button>
            </div>
          </article>
        </div>
      </section>
      <div v-if="!papers.length" class="empty-state">
        <h2>{{ t('addPaper') }}</h2>
        <p>{{ t('libraryIntro') }}</p>
      </div>

      <footer class="project-footer">
        <label class="check"><input v-model="personal" type="checkbox">{{ t('includePersonal') }}</label>
        <a class="text-button" :href="`/api/projects/${state.projectID}/export?personal=${personal}`">{{ t('exportProject') }} ↗</a>
        <button class="text-button danger" @click="deleteProject"><Icon name="trash" /> {{ t('deleteProjectButton') }}</button>
      </footer>
    </template>
    <NewPaperDialog v-if="addingPaper" @close="addingPaper = false" />
    <NewProjectDialog v-if="addingProject" @close="addingProject = false" />
  </section>
</template>
