<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useWorkspace } from '../lib/workspace.ts'
import Icon from './Icon.vue'
import type { Knowledge, Unit } from '../api/types.ts'

// The shared knowledge every output of a study is written from: key points
// per section (written by AI when a model is connected, otherwise excerpts
// of the source), key terms and the evaluation conditions.
const { state, t, api, act, knowledge, fileURL, citationLabel, explain } = useWorkspace()
const draft = ref<Knowledge | null>(null), version = ref(0), loaded = ref('')
// The source passage shown under a point: "<claim index>:<unit id>".
const open = ref('')
watch(() => knowledge.value?.id, () => {
  draft.value = knowledge.value ? JSON.parse(JSON.stringify(knowledge.value.data)) : null
  version.value = knowledge.value?.version || 0
  loaded.value = JSON.stringify(draft.value)
  open.value = ''
}, { immediate: true })
const dirty = computed(() => draft.value && JSON.stringify(draft.value) !== loaded.value)
const byAI = computed(() => draft.value?.author === 'ai')
const modelReady = computed(() => state.connection.mode === 'model')
// Related work (6) follows the motivation, as in the outputs.
const SECTIONS = [0, 6, 1, 2, 3, 4, 5]
const points = (i: number) => (draft.value?.claims || []).map((c, index) => ({ c, index })).filter(x => x.c.section === i)
const units = computed(() => draft.value?.units || [])
const unitByID = computed((): Record<string, Unit> => Object.fromEntries(units.value.map(u => [u.id, u])))

const toggleSource = (index: number, id: string) => { open.value = open.value === `${index}:${id}` ? '' : `${index}:${id}` }
const openUnit = (index: number) => open.value.startsWith(index + ':') ? unitByID.value[open.value.slice(String(index).length + 1)] : null

// Points and terms are added only while a draft is shown.
function addPoint(section: number) {
  (draft.value!.claims ||= []).push({ section, text: '', citations: units.value[0] ? [units.value[0].id] : [], verified: false, kind: 'manual' })
}
function removePoint(index: number) { draft.value?.claims?.splice(index, 1); open.value = '' }
function addTerm() { (draft.value!.terms ||= []).push({ original: '', en: '', 'zh-TW': '', abbreviation: '', definition: '', citations: units.value[0] ? [units.value[0].id] : [], context: draft.value!.title }) }
function sourceRecord(id: string) { return (state.snapshot.source || []).find(x => x.id === id) }

async function save() { await act(() => api(`/papers/${state.paperID}/knowledge`, { method: 'PUT', body: { version: version.value, knowledge: draft.value } })) }
const writing = ref(false)
async function rewrite() {
  if ((dirty.value || byAI.value) && !confirm(t(dirty.value ? 'knowledgeRewriteDiscard' : 'knowledgeRewriteConfirm'))) return
  writing.value = true
  await act(() => api(`/papers/${state.paperID}/knowledge/ai`, { method: 'POST', body: { version: version.value } }))
  writing.value = false
}
const rows = (text: string | undefined) => Math.min(8, Math.max(2, Math.ceil((text || '').length / 60)))
</script>

<template>
  <section class="panel knowledge-panel">
    <div class="section-heading">
      <div><p class="eyebrow">SHARED KNOWLEDGE</p><h2>{{ t('knowledge') }}</h2><p class="muted">{{ t('knowledgeIntro') }}</p></div>
      <span v-if="knowledge" class="pill">v{{ knowledge.version }}</span>
    </div>
    <p v-if="!draft" class="empty-inline">{{ t('noClaims') }}</p>
    <form v-else @submit.prevent="save">
      <div class="knowledge-source" :class="{ ai: byAI }">
        <Icon :name="byAI ? 'sparkle' : 'file'" />
        <div class="grow">
          <strong>{{ t(byAI ? 'knowledgeByAI' : 'knowledgeByRules') }}</strong>
          <p class="caption">{{ byAI ? t('knowledgeByAIHint') : (draft.ai_error ? t('knowledgeNoAI').replace('{reason}', explain(draft.ai_error)) : t('knowledgeByRulesHint')) }}</p>
        </div>
        <button type="button" class="secondary small knowledge-rewrite" :disabled="!modelReady || writing || !!state.busy" :title="modelReady ? '' : t('aiNeedsModel')" @click="rewrite">
          <Icon name="sparkle" /> {{ t(writing ? 'knowledgeWriting' : byAI ? 'knowledgeRewrite' : 'knowledgeWrite') }}
        </button>
      </div>

      <div class="knowledge-sections">
        <section v-for="i in SECTIONS" :key="i" class="knowledge-section">
          <h3>{{ t('ksec' + i) }} <span class="count">{{ points(i).length }}</span></h3>
          <p v-if="!points(i).length" class="caption">{{ t('knowledgeEmptySection') }}</p>
          <div v-for="{ c, index } in points(i)" :key="index" class="knowledge-point">
            <div class="knowledge-point-row">
              <textarea v-model="c.text" :rows="rows(c.text)" :aria-label="t('ksec' + i)"></textarea>
              <button type="button" class="icon-button danger" :title="t('delete')" :aria-label="t('delete')" @click="removePoint(index)">×</button>
            </div>
            <div class="citation-list">
              <button v-for="id in c.citations" :key="id" type="button" class="citation" :class="{ on: open === `${index}:${id}` }" :title="t('knowledgeShowSource')" @click="toggleSource(index, id)">{{ citationLabel(id) }}</button>
              <select v-if="c.kind === 'manual'" v-model="c.citations![0]" class="citation-select" :aria-label="t('citation')"><option v-for="u in units" :key="u.id" :value="u.id">{{ citationLabel(u.id) }}</option></select>
              <span v-if="c.unsupported?.length" class="pill warning knowledge-unsupported">{{ t('knowledgeUnsupported').replace('{numbers}', c.unsupported.join('、')) }}</span>
            </div>
            <blockquote v-if="openUnit(index)" class="knowledge-quote">{{ openUnit(index)?.text }}</blockquote>
          </div>
          <button type="button" class="text-button" @click="addPoint(i)">＋ {{ t('knowledgeAddPoint') }}</button>
        </section>
      </div>

      <details><summary>{{ t('terms') }} <span class="count">{{ (draft.terms || []).length }}</span></summary><div class="table-scroll"><table class="term-table"><thead><tr><th>{{ t('original') }}</th><th>{{ t('english') }}</th><th>{{ t('chinese') }}</th><th>{{ t('definition') }}</th><th>{{ t('citation') }}</th><th></th></tr></thead><tbody><tr v-for="(term,index) in draft.terms" :key="index"><td><input v-model="term.original" required></td><td><input v-model="term.en"></td><td><input v-model="term['zh-TW']"></td><td><textarea v-model="term.definition" rows="2"></textarea></td><td><select v-model="term.citations![0]"><option v-for="unit in units" :key="unit.id" :value="unit.id">{{ citationLabel(unit.id) }}</option></select></td><td><button type="button" class="icon-button danger" @click="draft.terms!.splice(index,1)">×</button></td></tr></tbody></table></div><button type="button" class="text-button" @click="addTerm">＋ {{ t('addTerm') }}</button></details>
      <details open><summary>{{ t('conditions') }}</summary><p class="caption">{{ t('conditionsHint') }}</p><div class="form-grid"><label v-for="key in ['task','dataset','split','metric','evaluation']" :key="key">{{ t(key) }}<input v-model="draft.conditions[key]"></label></div></details>
      <details><summary>{{ t('sourceText') }}</summary><details v-for="unit in units" :key="unit.id" class="source-unit"><summary>{{ citationLabel(unit.id) }}</summary><a v-if="unit.preview && sourceRecord(unit.source_id)" :href="fileURL(sourceRecord(unit.source_id)!, unit.preview)" target="_blank" rel="noopener"><img :src="fileURL(sourceRecord(unit.source_id)!, unit.preview)" class="source-preview" :alt="citationLabel(unit.id)" loading="lazy"></a><pre>{{ unit.text }}</pre></details></details>

      <div class="form-actions"><label class="check"><input v-model="draft.confirmed" type="checkbox">{{ t('confirmKnowledge') }}</label><button class="primary" :disabled="!!state.busy">{{ t('saveKnowledge') }}</button></div>
    </form>
  </section>
</template>
