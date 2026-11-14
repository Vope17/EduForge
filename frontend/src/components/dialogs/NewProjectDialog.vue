<script setup lang="ts">
import { ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import type { Project, Rec } from '../../api/types.ts'
const emit = defineEmits<{ close: [] }>()
const { state, t, api, act, go } = useWorkspace()
const name = ref(''), description = ref(''), personal = ref(false)
async function create() {
  const result = await act(() => api<Rec<Project>>('/projects', { method: 'POST', body: { name: name.value, description: description.value, style: {} } }))
  if (result) { emit('close'); go({ name: 'home', project: result.id }) }
}
async function importProject(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const form = new FormData()
  form.append('file', file)
  form.append('personal', String(personal.value))
  const result = await act(() => api<Rec<Project>>('/projects/import', { method: 'POST', body: form }))
  input.value = ''
  if (result) { emit('close'); go({ name: 'home', project: result.id }) }
}
</script>

<template>
  <Teleport to="body">
  <div class="modal-backdrop" @click.self="emit('close')">
    <form class="modal" @submit.prevent="create">
      <div class="section-heading">
        <h2>{{ t('newProject') }}</h2>
        <button type="button" class="icon-button" :aria-label="t('close')" @click="emit('close')">×</button>
      </div>
      <label>{{ t('projectName') }}<input v-model="name" required maxlength="150" autofocus></label>
      <label>{{ t('description') }}<textarea v-model="description" rows="3"></textarea></label>
      <button class="primary" :disabled="!!state.busy">{{ t('create') }}</button>
      <div class="modal-divider"><span>{{ t('or') }}</span></div>
      <div class="button-row">
        <label class="file-picker">↙ {{ t('importProject') }}<input type="file" accept=".zip" @change="importProject"></label>
        <label class="check"><input v-model="personal" type="checkbox">{{ t('includePersonal') }}</label>
      </div>
    </form>
  </div>
  </Teleport>
</template>
