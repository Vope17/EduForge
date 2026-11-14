<script setup lang="ts">
import { ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import type { Paper, Rec } from '../../api/types.ts'
const emit = defineEmits<{ close: [] }>()
const { state, t, api, act, openPaper } = useWorkspace()
const title = ref(''), repo = ref('')
async function create() {
  const result = await act(() => api<Rec<Paper>>(`/projects/${state.projectID}/papers`, {
    method: 'POST', body: { title: title.value, repo: repo.value, group: '', knowledge_id: '', settings: {} },
  }))
  if (result) { emit('close'); openPaper(result.id, 'sources') }
}
</script>

<template>
  <Teleport to="body">
  <div class="modal-backdrop" @click.self="emit('close')">
    <form class="modal" @submit.prevent="create">
      <div class="section-heading">
        <h2>{{ t('addPaper') }}</h2>
        <button type="button" class="icon-button" :aria-label="t('close')" @click="emit('close')">×</button>
      </div>
      <label>{{ t('paperTitle') }}<input v-model="title" required autofocus></label>
      <label>{{ t('repo') }}<input v-model="repo" placeholder="https://… / /workspace/…"></label>
      <p class="caption">{{ t('addPaperHint') }}</p>
      <button class="primary" :disabled="!!state.busy">{{ t('create') }}</button>
    </form>
  </div>
  </Teleport>
</template>
