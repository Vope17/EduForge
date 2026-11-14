<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import ArtifactEditor from '../ArtifactEditor.vue'
import type { Artifact, Rec } from '../../api/types.ts'

const props = defineProps<{ artifacts: Rec<Artifact>[] }>()
const { state, t, api, act } = useWorkspace()
const editingID = ref('')
const editing = computed(() => (state.snapshot.artifact || []).find(x => x.id === editingID.value))
function open(artifact: Rec<Artifact>) { editingID.value = artifact.id }
function studyTitle(id: string) { return (state.snapshot.lesson || []).find(x => x.id === id)?.data.title || id }
// Group versions: newest artifact per (type, language, grouping) first.
// The server's "delete all versions" uses the same key.
const outputKey = (a: Rec<Artifact>) => [a.data.type, a.data.language, (a.data.grouped_lesson_ids || []).join(',')].join('|')
const latest = computed(() => {
  const seen = new Set<string>()
  return props.artifacts.filter(a => {
    const key = outputKey(a)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
})
const versionCount = (artifact: Rec<Artifact>) => props.artifacts.filter(a => outputKey(a) === outputKey(artifact)).length
async function removeOutput(artifact: Rec<Artifact>) {
  if (!confirm(t('deleteOutputConfirm').replace('{n}', String(versionCount(artifact))))) return
  const group = props.artifacts.filter(a => outputKey(a) === outputKey(artifact)).map(a => a.id)
  if (group.includes(editingID.value)) editingID.value = ''
  await act(() => api(`/artifacts/${artifact.id}?all=1`, { method: 'DELETE' }))
}
async function removeVersion(artifact: Rec<Artifact>) {
  if (!confirm(t('deleteVersionConfirm'))) return
  if (editingID.value === artifact.id) editingID.value = ''
  await act(() => api(`/artifacts/${artifact.id}`, { method: 'DELETE' }))
}
const older = computed(() => props.artifacts.filter(a => !latest.value.includes(a)))
</script>

<template>
  <section class="panel">
    <div class="section-heading">
      <h2>{{ t('artifacts') }}</h2>
      <span class="caption">{{ t('previousVersions') }}</span>
    </div>
    <p v-if="!artifacts.length" class="empty-inline">{{ t('noOutputs') }}</p>
    <div class="artifact-grid">
      <article v-for="artifact in latest" :key="artifact.id" class="artifact-card">
        <div class="form-row">
          <span class="output-letter small-letter">{{ artifact.data.type }}</span>
        </div>
        <h3>{{ artifact.data.grouped_lesson_ids?.length ? artifact.data.grouped_lesson_ids.map(studyTitle).join(' / ') : t(artifact.data.type) }}</h3>
        <p class="caption">{{ artifact.data.language }} · v{{ artifact.version }} · {{ new Date(artifact.created_at).toLocaleString(state.language) }}</p>
        <p v-if="artifact.data.mode === 'source-extract'" class="draft-notice">{{ t('extractive') }}</p>
        <p v-if="artifact.data.type === 'B'" class="caption">{{ t('estimatedTime') }}: {{ artifact.data.estimated_minutes?.toFixed(1) }} min</p>
        <div class="button-row">
          <button class="primary small" @click="open(artifact)">{{ t('openReader') }}</button>
          <button class="text-button danger" @click="removeOutput(artifact)">{{ t('deleteOutput') }}</button>
        </div>
        <div class="download-links">
          <span class="caption">{{ t('download') }}</span>
          <a :href="`/api/artifacts/${artifact.id}/download?format=html`" download>HTML</a>
          <a :href="`/api/artifacts/${artifact.id}/download?format=pdf`" download>PDF</a>
          <a v-if="artifact.data.type === 'B'" :href="`/api/artifacts/${artifact.id}/download?format=pptx`" download>PPTX</a>
        </div>
      </article>
    </div>
    <details v-if="older.length">
      <summary>{{ t('olderVersions') }} <span class="count">{{ older.length }}</span></summary>
      <div v-for="artifact in older" :key="artifact.id" class="source-row">
        <span class="file-icon">{{ artifact.data.type }}</span>
        <div class="grow"><strong>{{ t(artifact.data.type) }}</strong><small>{{ artifact.data.language }} · v{{ artifact.version }} · {{ new Date(artifact.created_at).toLocaleString(state.language) }}</small></div>
        <button class="text-button" @click="open(artifact)">{{ t('openReader') }}</button>
        <button class="text-button danger" @click="removeVersion(artifact)">{{ t('deleteVersion') }}</button>
      </div>
    </details>
    <ArtifactEditor v-if="editing" :key="editing.id" :record="editing" @close="editingID = ''" @saved="editingID = $event.id" />
  </section>
</template>
