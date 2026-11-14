<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useWorkspace } from '../lib/workspace.ts'
import SubNav from './SubNav.vue'
import Icon from './Icon.vue'
import DocEditor from './editor/DocEditor.vue'
import SlideEditor from './editor/SlideEditor.vue'
import TemplatePicker from './editor/TemplatePicker.vue'
import AIEditPanel from './editor/AIEditPanel.vue'
import { themeOf } from '../lib/slideTheme.ts'
import { useUndo } from '../lib/undo.ts'
import type { Tab } from './SubNav.vue'
import type { Artifact, Rec, Section } from '../api/types.ts'

const props = defineProps<{ record: Rec<Artifact> }>()
const emit = defineEmits<{ close: []; saved: [artifact: Rec<Artifact>] }>()
const { state, t, api, act, fileURL } = useWorkspace()
const draft = ref<Artifact | null>(null), themeID = ref((props.record.data.type === 'B' ? props.record.data.settings?.template_id : props.record.data.settings?.reader_template_id) || '')
const openedAt = ref(performance.now())
const saved = ref('')
const type = computed(() => props.record.data.type)
const tab = ref('edit')
// AI editing: the panel and the blocks selected in the editor.
const aiOpen = ref(false), selection = ref<string[]>([])
const geometry = computed(() => { const th = themeOf(theme.value); const [w = 16, h = 9] = th.ratio.split('/').map(Number); return { points: th.points, ratio: w / h } })
const dirty = computed(() => !!draft.value && JSON.stringify(draft.value.sections) !== saved.value)
// The draft's sections, edited in place by the editors (never null once loaded).
const sections = computed(() => draft.value?.sections ?? [])

// The knowledge versions behind this output hold its original figures.
const knowledgeRecords = computed(() => {
  const ids = [props.record.data.knowledge_id, ...(props.record.data.related_knowledge_ids || [])]
  return (state.snapshot.knowledge || []).filter(item => ids.includes(item.id))
})
const images = computed(() => knowledgeRecords.value.flatMap(item => item.data.images || []))
const sourceUnits = computed(() => knowledgeRecords.value.flatMap(item => item.data.units || []))
function imageURL(path: string) {
  const owner = knowledgeRecords.value.find(item => (item.data.images || []).some(img => img.path === path))
  return fileURL(owner || props.record, path)
}
const references = computed(() => [...new Set(sourceUnits.value.map(u => (u.source_title || (u.source_name || '').replace(/\.[^.]+$/, '')).trim()).filter(Boolean))])
const documentTitle = computed(() => references.value.join(' / ') || knowledgeRecords.value[0]?.data.title || '')
const theme = computed(() => props.record.data.style_snapshot?.resolved?.B || {})
const downloads = computed(() => [
  ...(type.value === 'B' ? [{ format: 'pptx', label: 'PPTX' }] : []),
  ...(type.value !== 'B' ? [{ format: 'docx', label: 'Word' }] : []),
  { format: 'pdf', label: type.value === 'B' ? t('slidesPdf') : 'PDF' },
  ...(type.value === 'B' ? [{ format: 'notes', label: t('notesPdf') }] : []),
  { format: 'html', label: 'HTML' },
])
const tabs = computed((): Tab[] => {
  const slides = type.value === 'B'
  return [
    { key: 'edit', label: t('edit'), icon: 'sliders' },
    { key: 'read', label: t(slides ? 'notesPreview' : 'tabRead'), icon: 'eye' },
    { key: 'template', label: t(slides ? 'template' : 'readerTemplate'), icon: 'palette' },
  ]
})

watch(() => props.record.id, () => {
  const copy: Artifact = JSON.parse(JSON.stringify(props.record.data))
  copy.sections ||= []
  draft.value = copy
  saved.value = JSON.stringify(copy.sections)
  openedAt.value = performance.now()
}, { immediate: true })
// The editors normalise older outputs (explicit figures, default slide boxes) on open;
// that is not a user change.
onMounted(() => { setTimeout(() => { saved.value = JSON.stringify(draft.value?.sections); undoer.reset() }) })

// Undo/redo covers the content and the presentation length.
const undoer = useUndo(
  () => draft.value ? { sections: draft.value.sections, minutes: draft.value.settings?.minutes ?? null } : null,
  (snap: { sections: Section[] | null; minutes: number | null }) => {
    const current = draft.value
    if (!current) return
    ;(current.sections ||= []).splice(0, current.sections.length, ...(snap.sections || []))
    if (snap.minutes !== null && current.settings) current.settings.minutes = snap.minutes
  },
  { enabled: () => tab.value === 'edit' && !document.querySelector('.picker-backdrop') },
)

function close() {
  if (dirty.value && !confirm(t('discardChanges'))) return
  emit('close')
}
function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape' || document.querySelector('.picker-backdrop')) return
  if (aiOpen.value) { aiOpen.value = false; return }
  close()
}
onMounted(() => { document.addEventListener('keydown', onKey); document.body.classList.add('no-scroll') })
onUnmounted(() => { document.removeEventListener('keydown', onKey); document.body.classList.remove('no-scroll') })

async function save() {
  ;(document.activeElement as HTMLElement | null)?.blur?.()
  const artifact = { ...draft.value, editing_seconds: (performance.now() - openedAt.value) / 1000 }
  const result = await act(() => api<Rec<Artifact>>(`/artifacts/${props.record.id}`, { method: 'PUT', body: { version: props.record.version, artifact } }))
  if (result) emit('saved', result)
}
async function reapply() {
  const result = await act(() => api<Rec<Artifact>>(`/artifacts/${props.record.id}/template`, { method: 'POST', body: { template_id: themeID.value } }))
  if (result) emit('saved', result)
}
</script>

<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="close">
      <section class="sheet" role="dialog" aria-modal="true" :aria-label="t('openReader')">
        <header class="sheet-head">
          <div class="grow">
            <span class="eyebrow">{{ t(type) }} · {{ record.data.language }} · v{{ record.version }}</span>
            <h2>{{ t('openReader') }}</h2>
          </div>
          <div class="sheet-downloads" :aria-label="t('download')">
            <a v-for="d in downloads" :key="d.format" class="secondary small" :href="`/api/artifacts/${record.id}/download?format=${d.format}`" download>
              <Icon name="download" /> {{ d.label }}
            </a>
          </div>
          <button class="primary" :disabled="!!state.busy || !dirty" @click="save">{{ t('saveRevision') }}</button>
          <button class="icon-button" :aria-label="t('close')" @click="close">×</button>
        </header>
        <div class="sheet-tabs">
          <SubNav v-model="tab" :tabs="tabs" />
          <span v-if="type === 'B'" class="edit-toolbar-inline">
            <label class="inline-field">{{ t('minutes') }}<input v-model.number="draft!.settings.minutes" type="number" min="1" max="180"></label>
          </span>
          <span class="caption">{{ t('saveCreatesVersion') }}</span>
          <span v-if="tab === 'edit'" class="undo-tools">
            <button class="icon-button undo-button" :disabled="!undoer.canUndo.value" :title="t('undo') + ' (Ctrl+Z)'" :aria-label="t('undo')" @click="undoer.undo()"><Icon name="undo" /></button>
            <button class="icon-button redo-button" :disabled="!undoer.canRedo.value" :title="t('redo') + ' (Ctrl+Shift+Z)'" :aria-label="t('redo')" @click="undoer.redo()"><Icon name="redo" /></button>
          </span>
          <button v-if="tab === 'edit'" class="secondary small ai-toggle" :class="{ on: aiOpen }" :aria-pressed="aiOpen" @click="aiOpen = !aiOpen"><Icon name="sparkle" /> {{ t('aiEdit') }}</button>
        </div>

        <div class="sheet-body" :class="{ flush: tab === 'edit', 'with-ai': tab === 'edit' && aiOpen }">
          <p v-if="record.data.mode === 'source-extract'" class="draft-notice">{{ t('extractive') }}</p>

          <template v-if="tab === 'edit'">
            <div class="editor-main">
              <SlideEditor v-if="type === 'B'" v-model:selection="selection" :selectable="aiOpen" :slides="sections" :images="images" :url="imageURL" :theme="theme" :background="(path: string) => fileURL(record, path)" />
              <DocEditor v-else v-model:selection="selection" :selectable="aiOpen" :sections="sections" :images="images" :url="imageURL" :references="references" :title="documentTitle" :language="record.data.language" :look="record.data.style_snapshot?.resolved?.[type] || {}" />
            </div>
            <AIEditPanel v-if="aiOpen" :record="record" :sections="sections" :selection="selection" :geometry="geometry" @close="aiOpen = false" @clear="selection = []" />
          </template>

          <iframe v-else-if="tab === 'read'" class="reader-frame" :title="t('previewReader')" sandbox="" :src="'/api/artifacts/' + record.id + '/download?format=html&preview=true'"></iframe>

          <div v-else-if="tab === 'template'" class="template-tab">
            <p class="caption">{{ t('templateHint') }}</p>
            <TemplatePicker v-model="themeID" :kind="type === 'B' ? 'pptx' : 'docx'" />
            <div class="button-row">
              <button class="primary" :disabled="!!state.busy || dirty" @click="reapply">{{ t('applyTemplate') }}</button>
              <span v-if="dirty" class="caption">{{ t('saveBeforeTemplate') }}</span>
            </div>
          </div>

        </div>
      </section>
    </div>
  </Teleport>
</template>
