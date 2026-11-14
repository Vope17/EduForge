<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import ConnectionNotice from './ConnectionNotice.vue'
import TemplatePicker from '../editor/TemplatePicker.vue'
import type { Rec, Request } from '../../api/types.ts'

const props = defineProps<{ paperIds: string[] }>()
const emit = defineEmits<{ started: [requestID: string] }>()
const { state, t, api, act, explain } = useWorkspace()
const form = reactive({
  selected: ['A'], combined: false, use_paper_settings: false, languages: { A: '', B: '', C: '' }, profile_id: '',
  settings: { depth: 'standard', level: 'intermediate', audience: 'classmates', minutes: 10, template_id: '', reader_template_id: '', languages: {} as Record<string, string>, instructions: { A: '', B: '' } as Record<string, string> },
  overrides: { A: {}, B: {}, C: {} },
})
const papers = computed(() => (state.snapshot.paper || []).filter(p => props.paperIds.includes(p.id)))
const single = computed(() => papers.value.length === 1 ? papers.value[0] : null)
const savedKinds = computed(() => ['A', 'B', 'C'].filter(kind => papers.value.some(p => p.data.settings?.selected?.includes(kind))))
const activeKinds = computed(() => form.use_paper_settings ? savedKinds.value : form.selected)
const unconfirmed = computed(() => papers.value.filter(p => !(state.snapshot.knowledge || []).find(k => k.id === p.data.knowledge_id)?.data.confirmed))
const has = (kind: string) => !form.use_paper_settings && form.selected.includes(kind)

// Start from the single paper's saved settings when it has them.
watch(single, paper => {
  const saved = paper?.data.settings
  if (!saved?.selected?.length) return
  form.selected = [...saved.selected]
  Object.assign(form.languages, { A: '', B: '', C: '' }, saved.languages || {})
  for (const key of ['depth', 'level', 'audience', 'minutes', 'template_id', 'reader_template_id'] as const) if (saved[key]) Object.assign(form.settings, { [key]: saved[key] })
  Object.assign(form.settings.instructions, { A: '', B: '' }, saved.instructions || {})
}, { immediate: true })

// Create the request and start it straight away: templates, not style
// previews, decide how outputs look.
async function generate() {
  const result = await act(async () => {
    const request = await api<Rec<Request>>(`/projects/${state.projectID}/requests`, {
      method: 'POST',
      body: {
        ...form,
        combined: !form.use_paper_settings && form.combined && props.paperIds.length > 1,
        paper_ids: props.paperIds,
        ui_language: state.language,
      },
    })
    await api(`/requests/${request.id}/start`, { method: 'POST', body: { use_defaults: true } })
    return request
  })
  if (result) emit('started', result.id)
}
async function saveStudySettings() {
  const paper = single.value
  if (!paper) return
  await act(() => api(`/papers/${paper.id}`, {
    method: 'PUT',
    body: { version: paper.version, paper: { ...paper.data, settings: { ...form.settings, selected: form.selected, languages: form.languages } } },
  }))
}
</script>

<template>
  <form class="panel" @submit.prevent="generate">
    <div class="section-heading">
      <h2>{{ t('outputs') }}</h2>
      <span class="pill">{{ papers.length }} {{ t('selected') }}</span>
    </div>
    <ConnectionNotice />
    <div v-if="unconfirmed.length" class="draft-notice">
      {{ explain('confirm_knowledge_first') }} · {{ unconfirmed.map(p => p.data.title).join('、') }}
    </div>
    <label v-if="papers.length > 1" class="check"><input v-model="form.use_paper_settings" type="checkbox">{{ t('useStudySettings') }}</label>

    <div v-if="!form.use_paper_settings" class="output-choices">
      <label v-for="kind in ['A', 'B']" :key="kind" class="output-choice" :class="{ chosen: form.selected.includes(kind) }">
        <input v-model="form.selected" type="checkbox" :value="kind">
        <span class="output-letter">{{ kind }}</span>
        <strong>{{ t(kind) }}</strong>
        <select v-model="form.languages[kind]" :aria-label="t('outputLanguage')">
          <option value="">{{ t('followUI') }}</option>
          <option value="zh-TW">繁體中文</option>
          <option value="en">English</option>
        </select>
      </label>
    </div>

    <div class="form-grid">
      <label v-if="has('A')">{{ t('depth') }}
        <select v-model="form.settings.depth"><option v-for="v in ['brief', 'standard', 'deep']" :key="v" :value="v">{{ t(v) }}</option></select>
      </label>
      <label v-if="has('A')">{{ t('level') }}
        <select v-model="form.settings.level"><option v-for="v in ['beginner', 'intermediate', 'advanced']" :key="v" :value="v">{{ t(v) }}</option></select>
      </label>
      <label v-if="has('B')">{{ t('audience') }}
        <select v-model="form.settings.audience"><option v-for="v in ['classmates', 'advisor', 'crossField']" :key="v" :value="v">{{ t(v) }}</option></select>
      </label>
      <label v-if="has('B')">{{ t('minutes') }}<input v-model.number="form.settings.minutes" type="number" min="1" max="180"></label>
    </div>
    <label v-for="kind in ['A', 'B'].filter(has)" :key="'wish' + kind" class="field-block wish-field">
      <span class="field-label">{{ t('wishLabel').replace('{kind}', t(kind)) }}</span>
      <textarea v-model="form.settings.instructions[kind]" rows="3" maxlength="2000" :class="'wish-' + kind" :placeholder="t('wishPlaceholder' + kind)"></textarea>
      <span class="caption">{{ t('wishHint') }}</span>
    </label>
    <div v-if="has('A')" class="field-block">
      <span class="field-label">{{ t('readerTemplate') }}</span>
      <TemplatePicker v-model="form.settings.reader_template_id" kind="docx" />
    </div>
    <div v-if="has('B')" class="field-block">
      <span class="field-label">{{ t('template') }}</span>
      <TemplatePicker v-model="form.settings.template_id" />
    </div>

    <label v-if="!form.use_paper_settings && papers.length > 1" class="check">
      <input v-model="form.combined" type="checkbox">{{ t('combinedOutput') }}
    </label>

    <div class="form-actions">
      <p class="caption">{{ t('generateHint') }}</p>
      <div class="button-row">
        <button v-if="single && !form.use_paper_settings" type="button" class="secondary" :disabled="!!state.busy || !form.selected.length" @click="saveStudySettings">{{ t('saveStudySettings') }}</button>
        <button class="primary" :disabled="!!state.busy || !activeKinds.length || unconfirmed.length > 0 || state.connection.mode === 'none'">{{ t('generate') }} →</button>
      </div>
    </div>
  </form>
</template>
