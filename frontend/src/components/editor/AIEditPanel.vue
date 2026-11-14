<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { plainText } from '../../lib/markup.ts'
import { applyBlock, blockLabel, blockText, selectionBlocks } from '../../lib/aiEdit.ts'
import Icon from '../Icon.vue'
import type { Artifact, EditResult, Rec, Section } from '../../api/types.ts'

type Proposal = EditResult & { before: string | null | undefined; label: string; status: 'open' | 'accepted' | 'discarded' | 'stale' }

// Select blocks → instruction → the model proposes new text per block →
// review before/after → accept into the draft (saved later as a new version).
const props = withDefaults(defineProps<{
  record: Rec<Artifact>
  /** The editor's draft. */
  sections: Section[]
  selection: string[]
  /** Slide width in points and aspect ratio. */
  geometry?: { points?: number; ratio?: number }
}>(), { geometry: () => ({}) })
const emit = defineEmits<{ close: []; clear: [] }>()
const { state, t, api, explain } = useWorkspace()
const type = computed(() => props.record.data.type)
const language = computed(() => props.record.data.language)
const instruction = ref(''), working = ref(false), error = ref('')
const proposals = ref<Proposal[]>([])
const blocks = computed(() => selectionBlocks(type.value, props.sections, props.selection, { ...props.geometry, language: language.value }))
const ready = computed(() => state.connection.mode === 'model')
const hasNotes = computed(() => blocks.value.some(b => b.kind === 'notes'))
const quick = computed(() => [
  'aiQuickShorten', 'aiQuickSpoken', 'aiQuickAcademic', 'aiQuickBullets', 'aiQuickExplain',
  language.value === 'en' ? 'aiQuickToChinese' : 'aiQuickToEnglish',
  ...(hasNotes.value ? ['aiQuickScript'] : []),
])
const open = computed(() => proposals.value.filter(p => p.status === 'open'))
const show = (text: string, id: string) => id.endsWith(':body') ? plainText(text) : text

async function run(text = instruction.value) {
  instruction.value = text
  if (!text.trim() || !blocks.value.length || working.value) return
  working.value = true; error.value = ''
  const sent = blocks.value
  try {
    const result = await api<{ edits: EditResult[] | null }>(`/artifacts/${props.record.id}/ai-edit`, { method: 'POST', body: { instruction: text, blocks: sent } })
    const before: Record<string, string | null> = Object.fromEntries(sent.map(b => [b.id, b.text]))
    proposals.value = (result.edits || []).map((e): Proposal => ({ ...e, before: before[e.id], label: blockLabel(type.value, props.sections, e.id, t), status: e.text === before[e.id] ? 'discarded' : 'open' }))
    if (!proposals.value.some(p => p.status === 'open')) error.value = t('aiNoChange')
  } catch (err) { error.value = explain((err as Error).message) }
  finally { working.value = false }
}
// A proposal applies only to the text it was made from.
function accept(p: Proposal) {
  if (blockText(type.value, props.sections, p.id) !== p.before) { p.status = 'stale'; return }
  p.status = applyBlock(type.value, props.sections, p.id, p.text) ? 'accepted' : 'stale'
}
function acceptAll() { for (const p of open.value) accept(p) }
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
          <span>{{ t('aiSelected').replace('{n}', String(blocks.length)) }}</span>
          <button v-if="selection.length" class="text-button" @click="emit('clear')">{{ t('clearSelection') }}</button>
        </div>
        <p v-if="!blocks.length" class="caption">{{ t(type === 'B' ? 'aiHintSlides' : 'aiHintDoc') }}</p>
        <div v-else class="ai-chips">
          <span v-for="b in blocks" :key="b.id" class="pill">{{ blockLabel(type, sections, b.id, t) }}</span>
        </div>
      </section>

      <div class="ai-quick">
        <button v-for="key in quick" :key="key" class="chip-button" :disabled="!ready || !blocks.length || working" @click="run(t(key + 'Text'))">{{ t(key) }}</button>
      </div>
      <form class="ai-instruction" @submit.prevent="run()">
        <textarea v-model="instruction" rows="3" :placeholder="t('aiInstruction')" :disabled="working" @keydown.enter.exact.prevent="run()"></textarea>
        <button class="primary" :disabled="!ready || !blocks.length || !instruction.trim() || working">
          <Icon name="sparkle" /> {{ t(working ? 'aiWorking' : 'aiGenerate') }}
        </button>
      </form>
      <p v-if="error" class="issue-text">{{ error }}</p>

      <section v-if="proposals.length" class="ai-results">
        <div class="ai-selection-head">
          <strong>{{ t('aiProposals') }}</strong>
          <button v-if="open.length > 1" class="secondary small" @click="acceptAll">{{ t('aiAcceptAll') }}</button>
        </div>
        <article v-for="p in proposals" :key="p.id" class="ai-proposal" :class="p.status">
          <div class="ai-proposal-head">
            <strong class="grow">{{ p.label }}</strong>
            <span v-if="p.status === 'accepted'" class="pill success">{{ t('aiApplied') }}</span>
            <span v-else-if="p.status === 'discarded'" class="pill">{{ t('aiDiscarded') }}</span>
          </div>
          <div class="ai-diff">
            <div class="ai-before"><small>{{ t('aiBefore') }}</small><p>{{ show(p.before ?? '', p.id) || '—' }}</p></div>
            <div class="ai-after"><small>{{ t('aiAfter') }}</small><p>{{ show(p.text, p.id) }}</p></div>
          </div>
          <p v-if="p.unsupported_numbers?.length" class="issue-text">{{ t('aiUnsupportedNumbers').replace('{n}', p.unsupported_numbers.join(', ')) }}</p>
          <p v-if="p.overflow" class="issue-text">{{ t('aiOverflow') }}</p>
          <p v-if="p.status === 'stale'" class="issue-text">{{ t('aiStale') }}</p>
          <div v-if="p.status === 'open'" class="button-row">
            <button class="primary small" @click="accept(p)">{{ t('aiAccept') }}</button>
            <button class="text-button" @click="p.status = 'discarded'">{{ t('aiDiscard') }}</button>
          </div>
        </article>
        <p class="caption">{{ t('aiSaveHint') }}</p>
      </section>
    </div>
  </aside>
</template>
