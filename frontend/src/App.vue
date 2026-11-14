<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { createWorkspace } from './lib/workspace.ts'
import { activeTasks } from './lib/status.ts'
import AppSidebar from './components/shell/AppSidebar.vue'
import TopBar from './components/shell/TopBar.vue'
import TaskDrawer from './components/shell/TaskDrawer.vue'
import ProjectHome from './components/views/ProjectHome.vue'
import PaperView from './components/views/PaperView.vue'
import BatchView from './components/views/BatchView.vue'
import SettingsView from './components/views/SettingsView.vue'

const { state, route, go, t, init, refresh, explain } = createWorkspace()
const ready = ref(false)

// Poll quickly only while something is running; otherwise stay quiet.
const busyRemote = computed(() => activeTasks(state.snapshot).length > 0 ||
  (state.snapshot.run || []).some(r => !['finished', 'manual', 'needs_environment', 'cancelled', 'failed'].includes(r.data.progress)))
let timer: ReturnType<typeof setTimeout> | undefined
function schedule() {
  clearTimeout(timer)
  timer = setTimeout(async () => {
    if (!document.hidden) await refresh().catch(() => {})
    schedule()
  }, busyRemote.value ? 2500 : 15000)
}
watch(busyRemote, schedule)
onMounted(async () => {
  await init()
  ready.value = true
  schedule()
})
onUnmounted(() => clearTimeout(timer))

// Route → workspace context
watch(() => [route.name, route.project, route.paper], async () => {
  if (route.project && route.project !== state.projectID) {
    state.projectID = route.project
    state.selected = []
    await refresh().catch(() => {})
  }
  state.paperID = route.name === 'paper' ? route.paper ?? '' : ''
  state.nav = false
}, { immediate: true })
watch(() => [route.name, route.step, route.tab], () => { state.nav = false })

// Normalize the URL once projects are known (e.g. "#/" → first project).
watch(() => [ready.value, state.projectID], () => {
  if (!ready.value) return
  if (route.name === 'home' && !route.project && state.projectID) go({ name: 'home', project: state.projectID }, { replace: true })
  if (route.name === 'paper' && !(state.snapshot.paper || []).some(p => p.id === route.paper)) go({ name: 'home', project: state.projectID }, { replace: true })
})
watch(() => state.language, lang => { document.documentElement.lang = lang }, { immediate: true })
</script>

<template>
  <div class="app-shell">
    <AppSidebar />
    <main class="workspace">
      <TopBar />
      <div v-if="state.error" class="error-banner" role="alert">
        <span>{{ explain(state.error) }}</span>
        <button :aria-label="t('close')" @click="state.error = ''">×</button>
      </div>
      <div v-if="!ready" class="page-content"><p class="empty-inline">{{ t('loading') }}</p></div>
      <SettingsView v-else-if="route.name === 'settings'" />
      <PaperView v-else-if="route.name === 'paper' && state.paperID" :key="state.paperID" />
      <BatchView v-else-if="route.name === 'batch'" />
      <ProjectHome v-else />
    </main>
    <TaskDrawer />
    <div v-if="state.toast" class="toast" role="status">✓ {{ state.toast }}</div>
  </div>
</template>
