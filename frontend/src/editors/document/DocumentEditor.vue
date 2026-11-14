<script setup lang="ts">
import { computed, nextTick, ref, watch, type Directive } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Block, BlockKind, Change, Content, PatchProposal, RevisionSummary, SavedDocument, TemplateSpec } from '../../api/documents'
import { useSelection, type Anchor } from '../../selection/useSelection'
import AppIcon from '../../components/AppIcon.vue'
import DownloadControls from '../DownloadControls.vue'
import EditorTabs from '../EditorTabs.vue'
import AIEditPanel from '../AIEditPanel.vue'
import RevisionPanel from '../RevisionPanel.vue'
import { useUndo } from '../useUndo'
import DocumentPreview from './DocumentPreview.vue'

// Word-style editing as in Paper2Lab: one page, each block edited in place,
// block tools on hover, an AI side panel for the selected parts and the
// saved versions in their own tab.
const props = defineProps<{
  document: SavedDocument
  spec?: TemplateSpec
  /** Kind of document, shown above the title (e.g. "Worksheet"). */
  kicker: string
  pending: boolean
  history: RevisionSummary[]
  previous?: SavedDocument
  proposal?: PatchProposal
}>()
const emit = defineEmits<{
  change: [change: Change]
  history: []
  viewRevision: [revision: number]
  propose: [instruction: string, anchors: Anchor[]]
  accept: []
  reject: []
}>()
const { t } = useI18n()
const kinds: BlockKind[] = ['heading', 'paragraph', 'list', 'table', 'image']
const limits = { blocks: 80, items: 20, rows: 20, columns: 8 }

interface EditableBlock {
  key: string
  id?: string
  kind: BlockKind
  text: string
  items: string[]
  rows: string[][]
  imageURL: string
}
interface Draft { title: string; blocks: EditableBlock[] }

let nextKey = 0
const draft = ref<Draft>({ title: '', blocks: [] })
const saved = ref('')
const tab = ref('edit')
const aiOpen = ref(false)
// The block the caret was last in; new blocks go below it.
const focused = ref(-1)
const page = ref<HTMLElement>()

// Compares content only; the per-block render keys are not part of it.
const snapshot = (value: Draft) => JSON.stringify(value, (name, field) => name === 'key' ? undefined : field)
const dirty = computed(() => snapshot(draft.value) !== saved.value)

const selection = useSelection(() => props.document.id, () => Array.from(page.value?.querySelectorAll<HTMLElement>('[data-block-id]') ?? [])
  .map((element) => {
    const rect = element.getBoundingClientRect()
    return { blockID: element.dataset.blockId!, rect: { left: rect.left, top: rect.top, width: rect.width, height: rect.height } }
  }))
const { anchors, rectangle } = selection
const selected = computed(() => new Set(anchors.value.filter((anchor) => anchor.start === undefined).map((anchor) => anchor.block_id)))

function editable(block: Block): EditableBlock {
  return {
    key: block.id ?? 'new-' + nextKey++,
    id: block.id,
    kind: block.kind,
    text: block.text,
    items: block.items?.slice() ?? [],
    rows: block.rows?.map((row) => row.slice()) ?? [],
    imageURL: block.image_url ?? '',
  }
}

function reset() {
  draft.value = { title: props.document.content.title, blocks: props.document.content.blocks.map(editable) }
  saved.value = snapshot(draft.value)
  selection.clear()
  void nextTick(() => undo.reset())
}

const undo = useUndo(
  () => draft.value,
  (value: Draft) => { draft.value = value },
  { enabled: () => tab.value === 'edit' },
)
watch(() => props.document, reset, { immediate: true })
onBeforeRouteLeave(() => !dirty.value || window.confirm(t('documents.editor.discardPrompt')))

const content = computed<Content>(() => ({
  title: draft.value.title,
  blocks: draft.value.blocks.map((block): Block => ({
    id: block.id,
    kind: block.kind,
    text: block.text,
    items: block.kind === 'list' ? block.items : undefined,
    rows: block.kind === 'table' ? block.rows : undefined,
    image_url: block.kind === 'image' ? block.imageURL : undefined,
  })),
}))

function save() {
  if (!dirty.value || props.pending) return
  ;(document.activeElement as HTMLElement | null)?.blur?.()
  emit('change', { action: 'save', content: content.value })
}

function discard() {
  if (window.confirm(t('documents.editor.discardConfirm'))) reset()
}

// Editing a block's text invalidates the AI selection inside it.
function edited(block: EditableBlock) {
  if (block.id) selection.remove(block.id)
}

function addBlock(kind: BlockKind) {
  const blocks = draft.value.blocks
  if (blocks.length >= limits.blocks) return
  const index = focused.value >= 0 && focused.value < blocks.length ? focused.value + 1 : blocks.length
  blocks.splice(index, 0, {
    key: 'new-' + nextKey++, kind, text: kind === 'heading' ? t('documents.editor.newHeading') : '',
    items: [''], rows: [['', ''], ['', '']], imageURL: '',
  })
  focused.value = index
  void nextTick(() => page.value?.querySelector<HTMLElement>(`[data-index="${index}"] :is(input, textarea)`)?.focus())
}

function removeBlock(index: number) {
  const block = draft.value.blocks[index]
  if (draft.value.blocks.length === 1) return
  if (block.id) selection.remove(block.id)
  draft.value.blocks.splice(index, 1)
}

function moveBlock(index: number, delta: number) {
  const target = index + delta
  if (target < 0 || target >= draft.value.blocks.length) return
  const [block] = draft.value.blocks.splice(index, 1)
  draft.value.blocks.splice(target, 0, block)
}

function changeTable(block: EditableBlock, direction: 'row' | 'column', add: boolean) {
  if (direction === 'row') {
    if (add) block.rows.push(block.rows[0].map(() => ''))
    else block.rows.pop()
  } else {
    for (const row of block.rows) {
      if (add) row.push('')
      else row.pop()
    }
  }
}

// Highlighting text while the AI panel is open selects just that text.
function captureText(block: EditableBlock, event: Event, field = 'text') {
  if (!aiOpen.value || !block.id) return
  const area = event.target as HTMLTextAreaElement
  selection.addText(block.id, area.selectionStart, area.selectionEnd, field)
}

function beginBoxSelect(event: PointerEvent) {
  if (aiOpen.value && !props.pending) selection.beginRectangle(event)
}

const blockNumber = (id: string) => props.document.content.blocks.findIndex((block) => block.id === id) + 1
function fieldLabel(id: string, field?: string) {
  const block = props.document.content.blocks.find((item) => item.id === id)
  const kind = block ? t('documents.blocks.' + block.kind) : ''
  const where = t('documents.editor.blockLabel', { number: blockNumber(id), kind })
  if (field?.startsWith('items.')) return where + ' · ' + t('documents.editor.item', { number: Number(field.slice(6)) + 1 })
  return where
}
const labels = computed(() => anchors.value.map((anchor) =>
  fieldLabel(anchor.block_id, anchor.field) + (anchor.start === undefined ? '' : ' · ' + t('ai.partialText'))))
const describe = (change: PatchProposal['changes'][number]) => fieldLabel(change.block_id, change.field)

function propose(instruction: string) {
  emit('propose', instruction, anchors.value.map((anchor) => ({ ...anchor })))
}

// Textareas grow with their text, like paragraphs on a page.
const vAutosize: Directive<HTMLTextAreaElement> = {
  mounted: (element) => fit(element),
  updated: (element) => fit(element),
}
function fit(element: HTMLTextAreaElement) {
  element.style.height = 'auto'
  element.style.height = element.scrollHeight + 'px'
}
function grow(event: Event) { fit(event.target as HTMLTextAreaElement) }

const tabs = computed(() => [
  { key: 'edit', label: t('editor.tabs.edit'), icon: 'pencil' as const },
  { key: 'preview', label: t('editor.tabs.preview'), icon: 'eye' as const },
  { key: 'history', label: t('editor.tabs.history'), icon: 'history' as const },
])
</script>

<template>
  <section class="editor-sheet">
    <header class="editor-head">
      <div class="grow">
        <span class="eyebrow">{{ t('documents.editor.revision', { number: document.revision }) }}</span>
        <p class="caption">
          {{ t('documents.editor.aiNotice') }}
        </p>
      </div>
      <DownloadControls
        :document="document"
        kind="document"
        :disabled="pending"
        :dirty="dirty"
      />
      <button
        v-if="dirty"
        type="button"
        class="text-button"
        :disabled="pending"
        @click="discard"
      >
        {{ t('documents.editor.discard') }}
      </button>
      <button
        type="button"
        class="document-primary"
        data-save
        :disabled="pending || !dirty"
        @click="save"
      >
        {{ t('documents.editor.save') }}
      </button>
    </header>

    <div class="editor-tabs">
      <EditorTabs
        v-model="tab"
        :tabs="tabs"
      />
      <span
        v-if="dirty"
        class="save-indicator"
        role="status"
      >{{ t('documents.editor.unsaved') }}</span>
      <span
        v-else
        class="caption"
      >{{ t('editor.saveCreatesVersion') }}</span>
      <span class="grow" />
      <span
        v-if="tab === 'edit'"
        class="undo-tools"
      >
        <button
          type="button"
          class="icon-button"
          :disabled="!undo.canUndo.value"
          :title="t('editor.undo') + ' (Ctrl+Z)'"
          :aria-label="t('editor.undo')"
          @click="undo.undo()"
        ><AppIcon name="undo" /></button>
        <button
          type="button"
          class="icon-button"
          :disabled="!undo.canRedo.value"
          :title="t('editor.redo') + ' (Ctrl+Shift+Z)'"
          :aria-label="t('editor.redo')"
          @click="undo.redo()"
        ><AppIcon name="redo" /></button>
      </span>
      <button
        v-if="tab === 'edit'"
        type="button"
        class="small ai-toggle"
        :class="{ on: aiOpen }"
        :aria-pressed="aiOpen"
        @click="aiOpen = !aiOpen"
      >
        <AppIcon name="sparkle" />{{ t('ai.title') }}
      </button>
    </div>

    <div
      v-if="tab === 'edit'"
      class="editor-body"
      :class="{ 'with-ai': aiOpen }"
    >
      <div class="editor-main">
        <div
          class="doc-toolbar"
          role="toolbar"
          :aria-label="t('documents.editor.insert')"
        >
          <span class="caption">{{ t('documents.editor.insert') }}</span>
          <button
            v-for="kind in kinds"
            :key="kind"
            type="button"
            class="tool wide"
            :disabled="pending || draft.blocks.length >= limits.blocks"
            @click="addBlock(kind)"
          >
            <AppIcon name="plus" />{{ t('documents.blocks.' + kind) }}
          </button>
          <span
            v-if="aiOpen"
            class="caption grow doc-toolbar-hint"
          >{{ t('documents.editor.selectionHint') }}</span>
        </div>

        <article
          ref="page"
          class="doc-page"
          :class="{ selecting: aiOpen }"
          :data-palette="spec?.palette"
          :data-font="spec?.font"
          @pointerdown="beginBoxSelect"
          @pointermove="selection.moveRectangle"
          @pointerup="selection.finishRectangle"
          @pointercancel="selection.cancelRectangle"
        >
          <header class="doc-title">
            <p class="doc-kicker">
              {{ kicker }}
            </p>
            <input
              v-model="draft.title"
              class="doc-title-input"
              required
              maxlength="160"
              :disabled="pending"
              :aria-label="t('documents.editor.title')"
            >
          </header>

          <section
            v-for="(block, index) in draft.blocks"
            :key="block.key"
            class="doc-block"
            :class="['doc-' + block.kind, { 'ai-selected': aiOpen && block.id && selected.has(block.id) }]"
            :data-block-id="block.id"
            :data-index="index"
            @focusin="focused = index"
          >
            <label
              v-if="aiOpen && block.id"
              class="ai-select doc-select"
              :title="t('documents.editor.select')"
            >
              <input
                type="checkbox"
                :checked="selected.has(block.id)"
                :aria-label="t('documents.editor.select') + ' · ' + (index + 1)"
                @change="block.id && selection.toggle(block.id)"
              >
            </label>
            <div class="doc-block-tools">
              <span class="doc-block-kind">{{ t('documents.blocks.' + block.kind) }}</span>
              <button
                type="button"
                class="icon-button"
                :title="t('documents.editor.up')"
                :aria-label="t('documents.editor.up')"
                :disabled="pending || index === 0"
                @click="moveBlock(index, -1)"
              >
                ↑
              </button>
              <button
                type="button"
                class="icon-button"
                :title="t('documents.editor.down')"
                :aria-label="t('documents.editor.down')"
                :disabled="pending || index === draft.blocks.length - 1"
                @click="moveBlock(index, 1)"
              >
                ↓
              </button>
              <button
                type="button"
                class="icon-button danger"
                :title="t('documents.editor.remove')"
                :aria-label="t('documents.editor.remove')"
                :disabled="pending || draft.blocks.length === 1"
                @click="removeBlock(index)"
              >
                ×
              </button>
            </div>

            <input
              v-if="block.kind === 'heading'"
              v-model="block.text"
              class="doc-heading"
              maxlength="2500"
              :disabled="pending"
              :aria-label="t('documents.editor.blockText', { number: index + 1 })"
              @input="edited(block)"
              @select="captureText(block, $event)"
            >
            <textarea
              v-else
              v-model="block.text"
              v-autosize
              class="doc-text"
              :class="{ 'doc-caption': block.kind !== 'paragraph' }"
              rows="1"
              maxlength="2500"
              :disabled="pending"
              :placeholder="t('documents.editor.placeholders.' + block.kind)"
              :aria-label="t('documents.editor.blockText', { number: index + 1 })"
              @input="edited(block); grow($event)"
              @select="captureText(block, $event)"
            />

            <template v-if="block.kind === 'list'">
              <ul class="doc-list">
                <li
                  v-for="(_, itemIndex) in block.items"
                  :key="itemIndex"
                >
                  <textarea
                    v-model="block.items[itemIndex]"
                    v-autosize
                    class="doc-text"
                    rows="1"
                    maxlength="400"
                    :disabled="pending"
                    :aria-label="t('documents.editor.item', { number: itemIndex + 1 })"
                    @input="edited(block); grow($event)"
                    @select="captureText(block, $event, 'items.' + itemIndex)"
                  />
                  <button
                    type="button"
                    class="icon-button"
                    :aria-label="t('documents.editor.removeItem')"
                    :disabled="pending || block.items.length === 1"
                    @click="block.items.splice(itemIndex, 1)"
                  >
                    ×
                  </button>
                </li>
              </ul>
              <button
                type="button"
                class="doc-insert plain"
                :disabled="pending || block.items.length >= limits.items"
                @click="block.items.push('')"
              >
                <AppIcon name="plus" />{{ t('documents.editor.addItem') }}
              </button>
            </template>

            <template v-if="block.kind === 'table'">
              <div class="document-table doc-table">
                <table>
                  <tbody>
                    <tr
                      v-for="(row, rowIndex) in block.rows"
                      :key="rowIndex"
                    >
                      <td
                        v-for="(_, column) in row"
                        :key="column"
                      >
                        <input
                          v-model="row[column]"
                          maxlength="400"
                          :disabled="pending"
                          :aria-label="t('documents.editor.cell', { row: rowIndex + 1, column: column + 1 })"
                        >
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <div class="doc-insert-row">
                <button
                  type="button"
                  class="doc-insert plain"
                  :disabled="pending || block.rows.length >= limits.rows"
                  @click="changeTable(block, 'row', true)"
                >
                  <AppIcon name="plus" />{{ t('documents.editor.addRow') }}
                </button>
                <button
                  type="button"
                  class="doc-insert plain"
                  :disabled="pending || block.rows.length === 1"
                  @click="changeTable(block, 'row', false)"
                >
                  {{ t('documents.editor.removeRow') }}
                </button>
                <button
                  type="button"
                  class="doc-insert plain"
                  :disabled="pending || block.rows[0].length >= limits.columns"
                  @click="changeTable(block, 'column', true)"
                >
                  <AppIcon name="plus" />{{ t('documents.editor.addColumn') }}
                </button>
                <button
                  type="button"
                  class="doc-insert plain"
                  :disabled="pending || block.rows[0].length === 1"
                  @click="changeTable(block, 'column', false)"
                >
                  {{ t('documents.editor.removeColumn') }}
                </button>
              </div>
            </template>

            <figure
              v-if="block.kind === 'image'"
              class="doc-figure"
            >
              <img
                v-if="block.imageURL.startsWith('https://')"
                :src="block.imageURL"
                :alt="block.text"
                referrerpolicy="no-referrer"
                loading="lazy"
              >
              <span
                v-else
                class="doc-figure-empty"
              >{{ t('documents.editor.imageEmpty') }}</span>
              <label class="doc-figure-url">
                {{ t('documents.editor.imageURL') }}
                <input
                  v-model="block.imageURL"
                  type="url"
                  required
                  pattern="https://.+"
                  maxlength="2048"
                  :disabled="pending"
                >
              </label>
            </figure>
          </section>
          <div
            v-if="rectangle"
            class="selection-box"
            :style="{ left: rectangle.left + 'px', top: rectangle.top + 'px', width: rectangle.width + 'px', height: rectangle.height + 'px' }"
          />
        </article>
      </div>

      <AIEditPanel
        v-if="aiOpen"
        :labels="labels"
        :dirty="dirty"
        :pending="pending"
        :proposal="proposal"
        :revision="document.revision"
        :describe="describe"
        @propose="propose"
        @accept="emit('accept')"
        @reject="emit('reject')"
        @clear="selection.clear"
        @close="aiOpen = false"
      >
        <template #hint>
          {{ t('documents.editor.aiHint') }}
        </template>
      </AIEditPanel>
    </div>

    <div
      v-else-if="tab === 'preview'"
      class="editor-body padded"
    >
      <DocumentPreview
        :title="content.title"
        :blocks="content.blocks"
        :spec="spec"
      />
    </div>

    <div
      v-else
      class="editor-body padded"
    >
      <RevisionPanel
        :history="history"
        :current="document.revision"
        :selected="previous?.revision"
        :pending="pending"
        :dirty="dirty"
        :can-undo="document.can_undo"
        :can-redo="document.can_redo"
        @load="emit('history')"
        @view="emit('viewRevision', $event)"
        @restore="emit('change', { action: 'restore', target_revision: $event })"
        @undo="emit('change', { action: 'undo' })"
        @redo="emit('change', { action: 'redo' })"
      >
        <DocumentPreview
          v-if="previous"
          :title="previous.content.title"
          :blocks="previous.content.blocks"
          :spec="spec"
        />
      </RevisionPanel>
    </div>
  </section>
</template>
