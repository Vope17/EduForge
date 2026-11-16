<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { createWorkspace } from './lib/workspace.ts'
import { activeTasks } from './lib/status.ts'
import AppSidebar from './components/shell/AppSidebar.vue'
import TopBar from './components/shell/TopBar.vue'
import TaskDrawer from './components/shell/TaskDrawer.vue'
import CourseHome from './components/views/CourseHome.vue'
import LessonView from './components/views/LessonView.vue'
import BatchView from './components/views/BatchView.vue'
import SettingsView from './components/views/SettingsView.vue'

const { state, route, go, t, init, refresh, explain } = createWorkspace()
const ready = ref(false)

// Poll quickly only while something is running; otherwise stay quiet.
const busyRemote = computed(() => activeTasks(state.snapshot).length > 0)
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
watch(() => [route.name, route.course, route.lesson], async () => {
  if (route.course && route.course !== state.courseID) {
    state.courseID = route.course
    state.selected = []
    await refresh().catch(() => {})
  }
  state.lessonID = route.name === 'lesson' ? route.lesson ?? '' : ''
  state.nav = false
}, { immediate: true })
watch(() => [route.name, route.step, route.tab], () => { state.nav = false })

// Normalize the URL once courses are known (e.g. "#/" → first course).
watch(() => [ready.value, state.courseID], () => {
  if (!ready.value) return
  if (route.name === 'home' && !route.course && state.courseID) go({ name: 'home', course: state.courseID }, { replace: true })
  if (route.name === 'lesson' && !(state.snapshot.lesson || []).some(p => p.id === route.lesson)) go({ name: 'home', course: state.courseID }, { replace: true })
})
watch(() => state.language, lang => { document.documentElement.lang = lang }, { immediate: true })
</script>

<template>
  <div class="app-shell">
    <AppSidebar />
    <main class="workspace">
      <TopBar />
      <div v-if="state.session?.read_only" class="readonly-banner" role="note">{{ t('readOnlyNotice') }}</div>
      <div v-if="state.error" class="error-banner" role="alert">
        <span>{{ explain(state.error) }}</span>
        <button :aria-label="t('close')" @click="state.error = ''">×</button>
      </div>
      <div v-if="!ready" class="page-content"><p class="empty-inline">{{ t('loading') }}</p></div>
      <SettingsView v-else-if="route.name === 'settings'" />
      <LessonView v-else-if="route.name === 'lesson' && state.lessonID" :key="state.lessonID" />
      <BatchView v-else-if="route.name === 'batch'" />
      <CourseHome v-else />
    </main>
    <TaskDrawer />
    <div v-if="state.toast" class="toast" role="status">✓ {{ state.toast }}</div>
  </div>
</template>
