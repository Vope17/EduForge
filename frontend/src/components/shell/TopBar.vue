<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { activeTasks } from '../../lib/status.ts'
import Icon from '../Icon.vue'

const { state, route, go, t, paper } = useWorkspace()
const running = computed(() => activeTasks(state.snapshot).length)
const conn = computed(() => state.connection)
const connLabel = computed(() => {
  if (conn.value.checking) return t('connectionChecking')
  if (conn.value.mode === 'model') return `${conn.value.provider}${conn.value.model ? ' · ' + conn.value.model : ''}`
  if (conn.value.mode === 'extract') return t('connectionExtractOnly')
  return t('connectionNone')
})
const crumbs = computed(() => {
  const project = state.snapshot.project?.data.name || ''
  if (route.name === 'settings') return [t('settings'), t('settingsTab_' + route.tab)]
  if (route.name === 'batch') return [project, t('batchStudio')]
  if (route.name === 'paper' && paper.value) return [project, paper.value.data.title, t('step_' + route.step)]
  return [project || 'EduForge']
})
</script>

<template>
  <header class="topbar">
    <button class="menu-button" :aria-label="t('menu')" :aria-expanded="state.nav" @click="state.nav = true"><Icon name="menu" /></button>
    <nav class="crumbs" aria-label="Breadcrumb">
      <template v-for="(crumb, i) in crumbs" :key="i">
        <span v-if="i" class="crumb-sep">/</span>
        <span :class="{ 'crumb-current': i === crumbs.length - 1 }">{{ crumb }}</span>
      </template>
    </nav>
    <div class="topbar-actions">
      <button
        class="conn-chip" :class="conn.checking ? 'checking' : conn.mode"
        :title="t('connectionTitle')" @click="go({ name: 'settings', tab: 'providers' })"
      >
        <span class="conn-dot"></span><span class="conn-label">{{ connLabel }}</span>
      </button>
      <button class="tasks-button" :class="{ active: state.drawer }" :aria-expanded="state.drawer" @click="state.drawer = !state.drawer">
        <Icon name="tasks" /><span class="tasks-label">{{ t('tasks') }}</span>
        <span v-if="running" class="tasks-badge">{{ running }}</span>
      </button>
    </div>
  </header>
</template>
