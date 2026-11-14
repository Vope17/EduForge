<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import SubNav from '../SubNav.vue'
import TaskRow from '../tasks/TaskRow.vue'
import type { Rec, Request } from '../../api/types.ts'

const { state, t, api, act } = useWorkspace()
const tab = ref('active')
const tasks = computed(() => (state.snapshot.task || []).slice().reverse())
const active = computed(() => tasks.value.filter(x => ['queued', 'running', 'paused', 'failed'].includes(x.data.status)))
const requests = computed(() => (state.snapshot.request || []).slice().reverse())
const events = computed(() => (state.snapshot.event || []).slice().reverse().slice(0, 50))
async function controlRequest(request: Rec<Request>, action: string) {
  await act(() => api(`/requests/${request.id}/control`, { method: 'POST', body: { action, refresh_providers: action === 'resume' } }))
}
</script>

<template>
  <div v-if="state.drawer" class="drawer-backdrop" @click="state.drawer = false"></div>
  <aside class="drawer" :class="{ open: state.drawer }" :aria-hidden="!state.drawer" :inert="!state.drawer">
    <div class="drawer-head">
      <h2>{{ t('tasks') }}</h2>
      <button class="icon-button" :aria-label="t('close')" @click="state.drawer = false">×</button>
    </div>
    <SubNav v-model="tab" class="drawer-tabs" :tabs="[
      { key: 'active', label: t('drawerActive'), icon: 'activity', count: active.length },
      { key: 'all', label: t('drawerAll'), icon: 'queue', count: tasks.length },
      { key: 'requests', label: t('task'), icon: 'inbox', count: requests.length },
      { key: 'log', label: t('activity'), icon: 'history' },
    ]" />
    <div class="drawer-body">
      <template v-if="tab === 'active' || tab === 'all'">
        <TaskRow v-for="task in (tab === 'active' ? active : tasks)" :key="task.id" :task="task" />
        <p v-if="!(tab === 'active' ? active : tasks).length" class="empty-inline">{{ t('noActiveTasks') }}</p>
      </template>
      <template v-else-if="tab === 'requests'">
        <div v-for="request in requests" :key="request.id" class="request-row">
          <div>
            <strong>{{ request.id.slice(0, 8) }}</strong> <span class="pill">{{ t(request.data.status) }}</span>
            <p class="caption">{{ (request.data.selected || []).join(' · ') }} · {{ new Date(request.created_at).toLocaleString(state.language) }}</p>
          </div>
          <div v-if="request.data.status !== 'cancelled'" class="button-row">
            <button class="text-button" @click="controlRequest(request, 'resume')">{{ t('resume') }}</button>
            <button class="text-button" @click="controlRequest(request, 'pause')">{{ t('pause') }}</button>
            <button class="text-button danger" @click="controlRequest(request, 'cancel')">{{ t('cancel') }}</button>
          </div>
        </div>
        <p v-if="!requests.length" class="empty-inline">{{ t('empty') }}</p>
      </template>
      <div v-else class="timeline">
        <article v-for="event in events" :key="event.id">
          <time>{{ new Date(event.created_at).toLocaleString(state.language) }}</time>
          <strong>{{ event.data.action }}</strong>
          <details><summary>{{ t('description') }}</summary><pre>{{ JSON.stringify(event.data.detail, null, 2) }}</pre></details>
        </article>
        <p v-if="!events.length" class="empty-inline">{{ t('empty') }}</p>
      </div>
    </div>
  </aside>
</template>
