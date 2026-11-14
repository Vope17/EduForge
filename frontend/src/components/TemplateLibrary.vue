<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../lib/workspace.ts'
import { templateStyle, themeOf } from '../lib/slideTheme.ts'
import SlideView from './editor/SlideView.vue'
import Icon from './Icon.vue'
import TemplateEditor from './templates/TemplateEditor.vue'
import TemplateExtraction from './templates/TemplateExtraction.vue'
import type { Element, Rec, Template } from '../api/types.ts'

// Templates are the user's own Office files: PowerPoint (.pptx / .potx) for
// slides, Word (.docx / .dotx) for readers.
const { state, t, api, act, fileURL } = useWorkspace()
const all = computed(() => (state.snapshot.template || []).filter(x => x.data.file))
const groups = computed(() => [
  { kind: 'pptx', title: t('slideTemplates'), items: all.value.filter(x => (x.data.kind || 'pptx') === 'pptx') },
  { kind: 'docx', title: t('readerTemplates'), items: all.value.filter(x => x.data.kind === 'docx') },
])
const fileInput = ref<HTMLInputElement | null>(null), name = ref(''), importing = ref(false), editingID = ref('')
// Extracting a template from an ordinary presentation.
const deckInput = ref<HTMLInputElement | null>(null), extracting = ref(false), reviewingID = ref('')
const drafts = computed(() => (state.snapshot.template || []).filter(x => x.data.draft))
async function extractDeck(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const body = new FormData()
  body.append('file', file)
  body.append('language', state.language)
  extracting.value = true
  const result = await act(() => api<Rec<Template>>('/templates/extract', { method: 'POST', body }), { success: false })
  extracting.value = false
  input.value = ''
  if (result) reviewingID.value = result.id
}
function extracted(id: string) { reviewingID.value = ''; editingID.value = id }
async function discardDraft(item: Rec<Template>) {
  if (confirm(t('extractDiscardConfirm'))) await act(() => api('/records/' + item.id, { method: 'DELETE' }))
}
// Empty slides show a PowerPoint template's two layouts: title and content.
const coverSlide: { layout: Element[] } = { layout: [{ id: 'cover', kind: 'text', role: 'display', x: 0, y: 0, w: 0, h: 0, text: '' }] }
const contentSlide: { layout: Element[] } = { layout: [] }

async function importTemplate(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const body = new FormData()
  body.append('file', file)
  body.append('name', name.value)
  importing.value = true
  const result = await act(() => api<Rec<Template>>('/templates', { method: 'POST', body }))
  importing.value = false
  input.value = ''
  if (result) name.value = ''
}
// Lesson plan forms: Word files whose labels (單元名稱, 教學活動…) a plan is written by.
const planForms = computed(() => all.value.filter(x => x.data.kind === 'plan-form'))
const importingForm = ref(false)
async function importPlanForm(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const body = new FormData()
  body.append('file', file)
  body.append('kind', 'plan-form')
  importingForm.value = true
  await act(() => api<Rec<Template>>('/templates', { method: 'POST', body }))
  importingForm.value = false
  input.value = ''
}
async function rename(item: Rec<Template>) {
  const next = prompt(t('templateName'), item.data.name)
  if (!next || next === item.data.name) return
  await act(() => api(`/templates/${item.id}`, { method: 'PUT', body: { version: item.version, template: { ...item.data, name: next } } }))
}
async function remove(item: Rec<Template>) {
  if (confirm(t('deleteConfirm'))) await act(() => api('/records/' + item.id, { method: 'DELETE' }))
}
</script>

<template>
  <section class="panel template-library">
    <div class="section-heading">
      <div>
        <h2>{{ t('templateLibrary') }}</h2>
        <p class="muted">{{ t('templateImportHint') }}</p>
      </div>
    </div>
    <div class="template-import">
      <label class="grow">{{ t('templateName') }}<input v-model="name" :placeholder="t('templateNameOptional')"></label>
      <button class="primary" :disabled="importing || !!state.busy" @click="fileInput?.click()">
        <Icon name="upload" /> {{ importing ? t('templateImporting') : t('importTemplate') }}
      </button>
      <input ref="fileInput" type="file" accept=".pptx,.potx,.docx,.dotx" hidden @change="importTemplate">
    </div>
    <div class="extract-entry">
      <div class="grow">
        <strong>{{ t('extractTemplate') }}</strong>
        <p class="caption">{{ t('extractHint') }}</p>
      </div>
      <button class="secondary" :disabled="extracting || !!state.busy" @click="deckInput?.click()"><Icon name="sparkle" /> {{ extracting ? t('extracting') : t('extractChoose') }}</button>
      <input ref="deckInput" type="file" accept=".pptx,.potx" hidden @change="extractDeck">
    </div>
    <section v-if="drafts.length" class="template-group">
      <h3>{{ t('extractDrafts') }}</h3>
      <div v-for="item in drafts" :key="item.id" class="source-row">
        <span class="file-icon">PPTX</span>
        <div class="grow"><strong>{{ item.data.name }}</strong><small>{{ item.data.extraction?.source_name }}</small></div>
        <button class="primary small" @click="reviewingID = item.id">{{ t('continueReview') }}</button>
        <button class="text-button danger" @click="discardDraft(item)">{{ t('extractDiscard') }}</button>
      </div>
    </section>
    <section v-for="group in groups" :key="group.kind" class="template-group">
      <h3>{{ group.title }}</h3>
      <p v-if="!group.items.length" class="empty-inline">{{ t(group.kind === 'pptx' ? 'noSlideTemplates' : 'noReaderTemplates') }}</p>
      <div class="template-picker" :class="{ word: group.kind === 'docx' }">
        <article v-for="item in group.items" :key="item.id" class="template-option static">
          <div v-if="group.kind === 'pptx'" class="template-pair">
            <SlideView :slide="coverSlide" :theme="themeOf(templateStyle(item.data))" :background="path => fileURL(item, path)" />
            <SlideView :slide="contentSlide" :theme="themeOf(templateStyle(item.data))" :background="path => fileURL(item, path)" />
          </div>
          <img v-else-if="item.data.preview" class="word-preview" :src="fileURL(item, item.data.preview)" :alt="item.data.name">
          <span class="template-option-name">{{ item.data.name }}<small>{{ group.kind === 'pptx' ? item.data.ratio : 'Word' }} · v{{ item.version }}</small></span>
          <p v-for="(issue, i) in item.data.issues || []" :key="i" class="issue-text">{{ t(issue.code) }}</p>
          <div class="button-row">
            <button class="primary small" @click="editingID = item.id"><Icon name="pencil" /> {{ t('editTemplate') }}</button>
            <button class="text-button" @click="rename(item)">{{ t('rename') }}</button>
            <button class="text-button danger" @click="remove(item)">{{ t('delete') }}</button>
          </div>
        </article>
      </div>
    </section>
    <section class="template-group">
      <h3>{{ t('planForm') }}</h3>
      <p class="caption">{{ t('planFormHint') }}</p>
      <p v-if="!planForms.length" class="empty-inline">{{ t('builtInForm') }}</p>
      <div v-for="item in planForms" :key="item.id" class="source-row">
        <span class="file-icon">DOC</span>
        <div class="grow"><strong>{{ item.data.name }}</strong></div>
        <button class="text-button" @click="rename(item)">{{ t('rename') }}</button>
        <button class="text-button danger" @click="remove(item)">{{ t('delete') }}</button>
      </div>
      <label class="file-picker"><Icon name="upload" /> {{ importingForm ? t('templateImporting') : t('importPlanForm') }}
        <input type="file" accept=".docx,.dotx" :disabled="importingForm" @change="importPlanForm">
      </label>
    </section>
    <TemplateEditor v-if="editingID" :id="editingID" @close="editingID = ''" />
    <TemplateExtraction v-if="reviewingID" :id="reviewingID" @close="reviewingID = ''" @done="extracted" />
  </section>
</template>
