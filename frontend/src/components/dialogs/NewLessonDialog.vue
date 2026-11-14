<script setup lang="ts">
import { ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import type { Lesson, Rec } from '../../api/types.ts'
const emit = defineEmits<{ close: [] }>()
const { state, t, api, act, openLesson } = useWorkspace()
const title = ref('')
async function create() {
  const result = await act(() => api<Rec<Lesson>>(`/courses/${state.courseID}/lessons`, {
    method: 'POST', body: { title: title.value, knowledge_id: '', settings: {} },
  }))
  if (result) { emit('close'); openLesson(result.id, 'sources') }
}
</script>

<template>
  <Teleport to="body">
  <div class="modal-backdrop" @click.self="emit('close')">
    <form class="modal" @submit.prevent="create">
      <div class="section-heading">
        <h2>{{ t('addLesson') }}</h2>
        <button type="button" class="icon-button" :aria-label="t('close')" @click="emit('close')">×</button>
      </div>
      <label>{{ t('lessonTitle') }}<input v-model="title" required autofocus></label>
      <p class="caption">{{ t('addLessonHint') }}</p>
      <button class="primary" :disabled="!!state.busy">{{ t('create') }}</button>
    </form>
  </div>
  </Teleport>
</template>
