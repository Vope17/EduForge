<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import ConnectionNotice from './ConnectionNotice.vue'
import TemplatePicker from '../editor/TemplatePicker.vue'
import { chapterList } from '../../lib/library.ts'
import { effectiveSettings } from '../../lib/settings.ts'
import type { Rec, Request } from '../../api/types.ts'

const props = defineProps<{ lessonIds: string[] }>()
const emit = defineEmits<{ started: [requestID: string] }>()
const { state, t, api, act, explain } = useWorkspace()
const form = reactive({
  selected: ['A'], use_lesson_settings: false, languages: { A: '', B: '', C: '', D: '' } as Record<string, string>, profile_id: '',
  settings: { depth: 'standard', level: 'intermediate', audience: '', minutes: 50, template_id: '', reader_template_id: '', plan_form_id: '', languages: {} as Record<string, string>, instructions: { A: '', B: '', C: '', D: '' } as Record<string, string>, questions: { choice: 4, fill: 3, short: 3, calc: 0, practice: 0 } as Record<string, number> },
  overrides: { A: {}, B: {}, C: {}, D: {} },
})
const lessons = computed(() => (state.snapshot.lesson || []).filter(p => props.lessonIds.includes(p.id)))
const single = computed(() => lessons.value.length === 1 ? lessons.value[0] : null)
const KINDS = ['A', 'B', 'C', 'D']
const savedKinds = computed(() => KINDS.filter(kind => lessons.value.some(p => p.data.settings?.selected?.includes(kind))))
const activeKinds = computed(() => form.use_lesson_settings ? savedKinds.value : form.selected)
const unconfirmed = computed(() => lessons.value.filter(p => !(state.snapshot.knowledge || []).find(k => k.id === p.data.knowledge_id)?.data.confirmed))
const planForms = computed(() => (state.snapshot.template || []).filter(x => x.data.kind === 'plan-form'))
const has = (kind: string) => !form.use_lesson_settings && form.selected.includes(kind)

// Start from what the lesson inherits (course defaults, then its chapter, then
// itself), then from the settings it saved with its outputs, if any.
watch(single, lesson => {
  if (lesson) {
    const chapter = chapterList(state.snapshot).find(c => c.id === lesson.data.chapter)
    const base = effectiveSettings(state.snapshot.course?.data.settings, chapter?.data.settings, lesson.data.settings)
    for (const key of ['depth', 'level', 'audience', 'minutes', 'template_id', 'reader_template_id', 'plan_form_id'] as const) if (base[key]) Object.assign(form.settings, { [key]: base[key] })
    Object.assign(form.settings.instructions, { A: '', B: '', C: '', D: '' }, base.instructions || {})
  }
  const saved = lesson?.data.settings
  if (!saved?.selected?.length) return
  form.selected = [...saved.selected]
  Object.assign(form.languages, { A: '', B: '', C: '', D: '' }, saved.languages || {})
  for (const key of ['depth', 'level', 'audience', 'minutes', 'template_id', 'reader_template_id', 'plan_form_id'] as const) if (saved[key]) Object.assign(form.settings, { [key]: saved[key] })
  Object.assign(form.settings.instructions, { A: '', B: '', C: '', D: '' }, saved.instructions || {})
}, { immediate: true })

// Create the request and start it straight away: templates, not style
// previews, decide how outputs look.
async function generate() {
  const result = await act(async () => {
    const request = await api<Rec<Request>>(`/courses/${state.courseID}/requests`, {
      method: 'POST',
      body: {
        ...form,
        lesson_ids: props.lessonIds,
        ui_language: state.language,
      },
    })
    await api(`/requests/${request.id}/start`, { method: 'POST', body: { use_defaults: true } })
    return request
  })
  if (result) emit('started', result.id)
}
async function saveStudySettings() {
  const lesson = single.value
  if (!lesson) return
  await act(() => api(`/lessons/${lesson.id}`, {
    method: 'PUT',
    body: { version: lesson.version, lesson: { ...lesson.data, settings: { ...form.settings, selected: form.selected, languages: form.languages } } },
  }))
}
</script>

<template>
  <form class="panel" @submit.prevent="generate">
    <div class="section-heading">
      <h2>{{ t('outputs') }}</h2>
      <span class="pill">{{ lessons.length }} {{ t('selected') }}</span>
    </div>
    <ConnectionNotice />
    <div v-if="unconfirmed.length" class="draft-notice">
      {{ explain('confirm_knowledge_first') }} · {{ unconfirmed.map(p => p.data.title).join('、') }}
    </div>
    <label v-if="lessons.length > 1" class="check"><input v-model="form.use_lesson_settings" type="checkbox">{{ t('useStudySettings') }}</label>

    <div v-if="!form.use_lesson_settings" class="output-choices">
      <label v-for="kind in KINDS" :key="kind" class="output-choice" :class="{ chosen: form.selected.includes(kind) }">
        <input v-model="form.selected" type="checkbox" :value="kind">
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
      <label v-if="has('A') || has('B')">{{ t('level') }}
        <select v-model="form.settings.level"><option v-for="v in ['beginner', 'intermediate', 'advanced']" :key="v" :value="v">{{ t('level_' + v) }}</option></select>
      </label>
      <label v-if="has('A') || has('B')">{{ t('audience') }}
        <input v-model="form.settings.audience" :placeholder="t('settingsAudiencePlaceholder')" maxlength="200">
      </label>
      <label v-if="has('B') || has('C')">{{ t('minutes') }}<input v-model.number="form.settings.minutes" type="number" min="1" max="180"></label>
    </div>
    <div v-if="has('D')" class="field-block">
      <span class="field-label">{{ t('questionMix') }}</span>
      <div class="mix-grid">
        <label v-for="ty in ['choice', 'fill', 'short', 'calc', 'practice']" :key="ty" class="inline-field">{{ t('qt_' + ty) }}
          <input v-model.number="form.settings.questions[ty]" type="number" min="0" max="20">
        </label>
      </div>
    </div>
    <label v-for="kind in KINDS.filter(has)" :key="'wish' + kind" class="field-block wish-field">
      <span class="field-label">{{ t('wishLabel').replace('{kind}', t(kind)) }}</span>
      <textarea v-model="form.settings.instructions[kind]" rows="3" maxlength="2000" :class="'wish-' + kind" :placeholder="t('wishPlaceholder' + kind)"></textarea>
      <span class="caption">{{ t('wishHint') }}</span>
    </label>
    <div v-if="has('A')" class="field-block">
      <span class="field-label">{{ t('readerTemplate') }}</span>
      <TemplatePicker v-model="form.settings.reader_template_id" kind="docx" />
    </div>
    <div v-if="has('C')" class="field-block">
      <span class="field-label">{{ t('planForm') }}</span>
      <select v-model="form.settings.plan_form_id">
        <option value="">{{ t('builtInForm') }}</option>
        <option v-for="f in planForms" :key="f.id" :value="f.id">{{ f.data.name }}</option>
      </select>
      <span class="caption">{{ t('planFormHint') }}</span>
    </div>
    <div v-if="has('B')" class="field-block">
      <span class="field-label">{{ t('template') }}</span>
      <TemplatePicker v-model="form.settings.template_id" />
    </div>

    <div class="form-actions">
      <p class="caption">{{ t('generateHint') }}</p>
      <div class="button-row">
        <button v-if="single && !form.use_lesson_settings" type="button" class="secondary" :disabled="!!state.busy || !form.selected.length" @click="saveStudySettings">{{ t('saveStudySettings') }}</button>
        <button class="primary" :disabled="!!state.busy || !activeKinds.length || unconfirmed.length > 0 || state.connection.mode === 'none'">{{ t('generate') }} →</button>
      </div>
    </div>
  </form>
</template>
