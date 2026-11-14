<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import type { Rec, Task } from '../../api/types.ts'
const props = defineProps<{ task: Rec<Task>; compact?: boolean }>()
const { state, t, api, act, explain, openLesson } = useWorkspace()
const d = computed(() => props.task.data)
const label = computed(() => d.value.type === 'parse' ? t('parseType') : d.value.type === 'knowledge' ? t('knowledgeType') : d.value.type === 'syllabus' ? t('S')
  : d.value.type.startsWith('preview:') ? t('previews') + ' · ' + d.value.type.slice(8) : t(d.value.type))
const lesson = computed(() => (state.snapshot.lesson || []).find(x => x.id === props.task.lesson_id))
const tone = computed(() => d.value.status === 'completed' ? 'success' : ['failed', 'paused'].includes(d.value.status) ? 'warning' : '')
async function control(action: string) { await act(() => api(`/tasks/${props.task.id}/control`, { method: 'POST', body: { action } })) }
</script>

<template>
  <div class="task-row">
    <span class="task-status" :class="d.status"></span>
    <div class="grow">
      <strong>{{ label }}</strong>
      <p v-if="!compact" class="caption">
        <button v-if="lesson" class="text-button inline" @click="openLesson(lesson.id); state.drawer = false">{{ lesson.data.title }}</button>
        · {{ d.language || '—' }} · {{ task.id.slice(0, 8) }}
      </p>
      <progress :value="d.completed" :max="d.total || 1"></progress>
      <p v-if="['parse', 'knowledge'].includes(d.type) && d.stage === 'knowledge' && d.status === 'running'" class="caption">{{ t('stageKnowledge') }}</p>
      <p v-if="d.error" class="issue-text">{{ explain(d.error) }}</p>
    </div>
    <span class="pill" :class="tone">{{ t(d.status) }} · {{ d.completed }}/{{ d.total }}</span>
    <div class="button-row">
      <button v-if="['queued', 'running'].includes(d.status)" class="text-button" @click="control('pause')">{{ t('pause') }}</button>
      <button v-if="['paused', 'failed'].includes(d.status)" class="text-button" @click="control('resume')">{{ t('resume') }}</button>
      <button v-if="!['completed', 'cancelled'].includes(d.status)" class="text-button danger" @click="control('cancel')">{{ t('cancel') }}</button>
    </div>
  </div>
</template>
