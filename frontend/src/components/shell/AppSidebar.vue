<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { paperSteps } from '../../lib/status.ts'
import { folderList, groupPapers, collapsed, toggleCollapsed, libraryActions } from '../../lib/library.ts'
import Icon from '../Icon.vue'
import NewPaperDialog from '../dialogs/NewPaperDialog.vue'
import NewProjectDialog from '../dialogs/NewProjectDialog.vue'
import type { Paper, Rec } from '../../api/types.ts'

const workspace = useWorkspace()
const { state, route, go, t, setLanguage, openPaper } = workspace
const { createFolder, renameFolder, deleteFolder, deleteProject, movePaper } = libraryActions(workspace)
const addingPaper = ref(false), addingProject = ref(false)
const papers = computed(() => state.snapshot.paper || [])
const steps = (paper: Rec<Paper>) => paperSteps(paper, state.snapshot)
const folders = computed(() => folderList(state.snapshot))
const groups = computed(() => groupPapers(papers.value, folders.value))
// Drag a study onto a folder (or onto "Unfiled") to move it.
const dragging = ref(''), dropTarget = ref<string | null>(null)
function dragStart(event: DragEvent, paper: Rec<Paper>) {
  dragging.value = paper.id
  if (!event.dataTransfer) return
  event.dataTransfer.effectAllowed = 'move'
  event.dataTransfer.setData('text/plain', paper.id)
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
  if (id) void movePaper(id, key)
}
function pickProject(id: string) { state.selected = []; go({ name: 'home', project: id }) }
function toggle(id: string) {
  const i = state.selected.indexOf(id)
  if (i >= 0) state.selected.splice(i, 1); else state.selected.push(id)
}
</script>

<template>
  <div v-if="state.nav" class="nav-backdrop" @click="state.nav = false"></div>
  <aside class="sidebar" :class="{ open: state.nav }">
    <a class="brand" href="#/" @click.prevent="go({ name: 'home', project: state.projectID })">
      <span class="brand-symbol">P<span>2</span>L</span><span>Paper<span class="brand-light">2</span>Lab</span>
    </a>

    <div class="sidebar-label">{{ t('projects') }}</div>
    <div class="project-row">
      <label class="sr-only" for="project-picker">{{ t('projects') }}</label>
      <select id="project-picker" class="project-picker" :value="state.projectID" @change="pickProject(($event.target as HTMLSelectElement).value)">
        <option value="" disabled>{{ t('selectProject') }}</option>
        <option v-for="project in state.projects" :key="project.id" :value="project.id">{{ project.data.name }}</option>
      </select>
      <button class="side-icon-button" :title="t('newProject')" :aria-label="t('newProject')" @click="addingProject = true"><Icon name="plus" /></button>
      <button v-if="state.projectID" class="side-icon-button" :title="t('deleteProjectButton')" :aria-label="t('deleteProjectButton')" @click="deleteProject"><Icon name="trash" /></button>
    </div>

    <div class="sidebar-label papers-label">
      <span>{{ t('library') }}</span>
      <span v-if="papers.length" class="side-count">{{ papers.length }}</span>
      <button v-if="state.projectID" class="side-icon-button small push-right" :title="t('newFolder')" :aria-label="t('newFolder')" @click="createFolder"><Icon name="folderPlus" /></button>
    </div>
    <nav class="paper-nav" :aria-label="t('library')">
      <template v-for="group in groups" :key="group.key">
        <div
          v-if="folders.length && (group.folder || group.papers.length || dragging)"
          class="folder-row" :class="{ 'drop-target': dropTarget === group.key }"
          @dragover="dragOver($event, group.key)" @dragleave="dropTarget = dropTarget === group.key ? null : dropTarget" @drop.prevent="drop(group.key)"
        >
          <button class="folder-toggle" :aria-expanded="!collapsed[group.key || 'unfiled']" @click="toggleCollapsed(group.key || 'unfiled')">
            <span class="folder-caret" :class="{ closed: collapsed[group.key || 'unfiled'] }">▾</span>
            <Icon name="folder" />
            <span class="folder-name">{{ group.folder ? group.folder.data.name : t('unfiled') }}</span>
            <span class="side-count">{{ group.papers.length }}</span>
          </button>
          <span v-if="group.folder" class="folder-actions">
            <button class="side-icon-button small" :title="t('renameFolder')" :aria-label="t('renameFolder') + ' · ' + group.folder.data.name" @click="renameFolder(group.folder)"><Icon name="pencil" /></button>
            <button class="side-icon-button small" :title="t('deleteFolder')" :aria-label="t('deleteFolder') + ' · ' + group.folder.data.name" @click="deleteFolder(group.folder)"><Icon name="trash" /></button>
          </span>
        </div>
        <template v-if="!folders.length || !collapsed[group.key || 'unfiled']">
          <div
            v-for="paper in group.papers" :key="paper.id" class="paper-link"
            :class="{ active: route.name === 'paper' && route.paper === paper.id, checked: state.selected.includes(paper.id), 'in-folder': folders.length, dragged: dragging === paper.id }"
            draggable="true" @dragstart="dragStart($event, paper)" @dragend="dragEnd"
            @dragover="dragOver($event, group.key)" @drop.prevent="drop(group.key)"
          >
            <input type="checkbox" :checked="state.selected.includes(paper.id)" :aria-label="t('selectForBatch') + ' · ' + paper.data.title" @change="toggle(paper.id)">
            <button class="paper-link-main" @click="openPaper(paper.id)">
              <span class="paper-link-title">{{ paper.data.title }}</span>
              <span class="step-dots" aria-hidden="true">
                <i v-for="step in steps(paper)" :key="step.key" :class="step.state"></i>
              </span>
            </button>
          </div>
          <p v-if="group.folder && !group.papers.length" class="folder-empty">{{ t('folderEmpty') }}</p>
        </template>
      </template>
      <p v-if="state.projectID && !papers.length" class="side-empty">{{ t('noPapersYet') }}</p>
    </nav>
    <button v-if="state.projectID" class="sidebar-new" @click="addingPaper = true"><Icon name="plus" /> {{ t('addPaper') }}</button>
    <button v-if="state.selected.length" class="batch-button" @click="go({ name: 'batch', project: state.projectID })">
      <Icon name="layers" /> {{ t('batchStudio') }} · {{ state.selected.length }}
    </button>

    <div class="sidebar-bottom">
      <button class="side-link" :class="{ active: route.name === 'settings' }" @click="go({ name: 'settings', tab: 'providers' })">
        <Icon name="gear" /> {{ t('settings') }}
      </button>
      <div class="language-switch">
        <button :class="{ active: state.language === 'zh-TW' }" @click="setLanguage('zh-TW')">繁中</button>
        <button :class="{ active: state.language === 'en' }" @click="setLanguage('en')">EN</button>
      </div>
    </div>
    <NewPaperDialog v-if="addingPaper" @close="addingPaper = false" />
    <NewProjectDialog v-if="addingProject" @close="addingProject = false" />
  </aside>
</template>
