<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { applySpecValue, sameValue, specKeyLabel, specValue, styleChanges } from '../../lib/templateSpec.ts'
import Icon from '../Icon.vue'
import type { Rec, SpecChange, Template, TemplateSpec } from '../../api/types.ts'
import type { SpecValue } from '../../lib/templateSpec.ts'

type Proposal = SpecChange & { status: 'open' | 'accepted' | 'discarded' | 'stale' }

// AI editing of a template: selected settings + instruction → the model
// proposes new values → review each → accept into the working spec, which
// the editor then saves as a new version.
const props = defineProps<{
  record: Rec<Template>
  /** The editor's working copy. */
  spec: TemplateSpec
  selection: string[]
}>()
const emit = defineEmits<{ close: []; clear: [] }>()
const { state, t, api, explain } = useWorkspace()
const word = computed(() => props.record.data.kind === 'docx')
const instruction = ref(''), working = ref(false), error = ref('')
const proposals = ref<Proposal[]>([])
const ready = computed(() => state.connection.mode === 'model')
const quick = computed(() => word.value
  ? ['tplAiPrint', 'tplAiHeadings', 'tplAiSpacing', 'tplAiAcademic']
  : ['tplAiCalm', 'tplAiVivid', 'tplAiContrast', 'tplAiMinimal', 'tplAiTitleLeft'])
const open = computed(() => proposals.value.filter(p => p.status === 'open'))

async function run(text = instruction.value) {
  instruction.value = text
  if (!text.trim() || !props.selection.length || working.value) return
  working.value = true; error.value = ''
  try {
    const result = await api<{ changes: SpecChange[] | null }>(`/templates/${props.record.id}/ai-edit`, { method: 'POST', body: { instruction: text, spec: props.spec, selection: props.selection } })
    proposals.value = (result.changes || []).map(c => ({ ...c, status: 'open' }))
  } catch (err) { error.value = explain((err as Error).message) }
  finally { working.value = false }
}
function accept(p: Proposal) {
  if (!sameValue(specValue(props.spec, p.key), p.before)) { p.status = 'stale'; return }
  applySpecValue(props.spec, p.key, p.after as SpecValue)
  p.status = 'accepted'
}
function acceptAll() { for (const p of open.value) accept(p) }
const pct = (v: number) => `${Math.round(v * 1000) / 10}%`
const boxText = (b: unknown) => Array.isArray(b) ? `x ${pct(b[0])} · y ${pct(b[1])} · ${t('tplWidth')} ${pct(b[2])} · ${t('tplHeight')} ${pct(b[3])}` : '—'
const kindOf = (key: string) => key.startsWith('colors.') ? 'color' : key.endsWith('_box') ? 'box' : key.startsWith('styles.') || key === 'margins' ? 'fields' : 'text'
</script>

<template>
  <aside class="ai-panel" data-native-undo :aria-label="t('aiEdit')">
    <header class="ai-panel-head">
      <Icon name="sparkle" />
      <strong class="grow">{{ t('aiEdit') }}</strong>
      <button class="icon-button" :aria-label="t('close')" @click="emit('close')">×</button>
    </header>
    <div class="ai-panel-body">
      <p v-if="!ready" class="draft-notice">{{ t('aiNeedsModel') }}</p>
      <section class="ai-selection">
        <div class="ai-selection-head">
          <span>{{ t('tplAiSelected').replace('{n}', String(selection.length)) }}</span>
          <button v-if="selection.length" class="text-button" @click="emit('clear')">{{ t('clearSelection') }}</button>
        </div>
        <p v-if="!selection.length" class="caption">{{ t(word ? 'tplAiHintWord' : 'tplAiHintSlides') }}</p>
        <div v-else class="ai-chips"><span v-for="key in selection" :key="key" class="pill">{{ specKeyLabel(key, t) }}</span></div>
      </section>
      <div class="ai-quick">
        <button v-for="key in quick" :key="key" class="chip-button" :disabled="!ready || !selection.length || working" @click="run(t(key + 'Text'))">{{ t(key) }}</button>
      </div>
      <form class="ai-instruction" @submit.prevent="run()">
        <textarea v-model="instruction" rows="3" :placeholder="t(word ? 'tplAiInstructionWord' : 'tplAiInstruction')" :disabled="working" @keydown.enter.exact.prevent="run()"></textarea>
        <button class="primary" :disabled="!ready || !selection.length || !instruction.trim() || working"><Icon name="sparkle" /> {{ t(working ? 'aiWorking' : 'aiGenerate') }}</button>
      </form>
      <p v-if="error" class="issue-text">{{ error }}</p>

      <section v-if="proposals.length" class="ai-results">
        <div class="ai-selection-head">
          <strong>{{ t('aiProposals') }}</strong>
          <button v-if="open.length > 1" class="secondary small" @click="acceptAll">{{ t('aiAcceptAll') }}</button>
        </div>
        <article v-for="p in proposals" :key="p.key" class="ai-proposal" :class="p.status">
          <div class="ai-proposal-head">
            <strong class="grow">{{ specKeyLabel(p.key, t) }}</strong>
            <span v-if="p.status === 'accepted'" class="pill success">{{ t('aiApplied') }}</span>
            <span v-else-if="p.status === 'discarded'" class="pill">{{ t('aiDiscarded') }}</span>
          </div>
          <div v-if="kindOf(p.key) === 'color'" class="color-change">
            <span class="color-chip" :style="{ background: '#' + p.before }"></span><code>{{ p.before }}</code>
            <span>→</span>
            <span class="color-chip" :style="{ background: '#' + p.after }"></span><code>{{ p.after }}</code>
          </div>
          <ul v-else-if="kindOf(p.key) === 'fields'" class="field-changes">
            <li v-for="c in styleChanges(p.before as Record<string, unknown>, p.after as Record<string, unknown>, t)" :key="c.field"><span>{{ c.label }}</span> <s>{{ c.before }}</s> → <strong>{{ c.after }}</strong></li>
          </ul>
          <div v-else class="ai-diff">
            <div class="ai-before"><small>{{ t('aiBefore') }}</small><p>{{ kindOf(p.key) === 'box' ? boxText(p.before) : (p.before || '—') }}</p></div>
            <div class="ai-after"><small>{{ t('aiAfter') }}</small><p>{{ kindOf(p.key) === 'box' ? boxText(p.after) : p.after }}</p></div>
          </div>
          <p v-if="p.status === 'stale'" class="issue-text">{{ t('aiStale') }}</p>
          <div v-if="p.status === 'open'" class="button-row">
            <button class="primary small" @click="accept(p)">{{ t('aiAccept') }}</button>
            <button class="text-button" @click="p.status = 'discarded'">{{ t('aiDiscard') }}</button>
          </div>
        </article>
        <p class="caption">{{ t('tplAiSaveHint') }}</p>
      </section>
    </div>
  </aside>
</template>
