<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Block, BlockKind, Change, Content, RevisionSummary, SavedDocument, TemplateSpec } from '../../api/documents'
import { useSelection } from '../../selection/useSelection'
import DocumentPreview from './DocumentPreview.vue'

const props = defineProps<{
  document: SavedDocument
  spec?: TemplateSpec
  pending: boolean
  history: RevisionSummary[]
  previous?: SavedDocument
}>()
const emit = defineEmits<{
  change: [change: Change]
  history: []
  viewRevision: [revision: number]
}>()
const { t } = useI18n()
const kinds: BlockKind[] = ['heading', 'paragraph', 'list', 'table', 'image']

interface EditableBlock {
  key: string
  id?: string
  kind: BlockKind
  text: string
  items: string[]
  rows: string[][]
  imageURL: string
}

let nextKey = 0
const title = ref('')
const blocks = ref<EditableBlock[]>([])
const newKind = ref<BlockKind>('paragraph')
const dirty = ref(false)
const showPreview = ref(false)
const { anchors, toggle, addText, remove, clear } = useSelection(() => props.document.id)
const selected = computed(() => new Set(anchors.value.map((anchor) => anchor.block_id)))

function reset() {
  title.value = props.document.content.title
  blocks.value = props.document.content.blocks.map((block) => ({
    key: block.id ?? 'new-' + nextKey++,
    id: block.id,
    kind: block.kind,
    text: block.text,
    items: block.items?.slice() ?? [],
    rows: block.rows?.map((row) => row.slice()) ?? [],
    imageURL: block.image_url ?? '',
  }))
  dirty.value = false
  clear()
}

watch(() => props.document, reset, { immediate: true })
onBeforeRouteLeave(() => !dirty.value || window.confirm(t('documents.editor.discardPrompt')))

const content = computed<Content>(() => ({
  title: title.value,
  blocks: blocks.value.map((block): Block => ({
    id: block.id,
    kind: block.kind,
    text: block.text,
    items: block.kind === 'list' ? block.items : undefined,
    rows: block.kind === 'table' ? block.rows : undefined,
    image_url: block.kind === 'image' ? block.imageURL : undefined,
  })),
}))

function addBlock() {
  blocks.value.push({
    key: 'new-' + nextKey++,
    kind: newKind.value,
    text: '',
    items: [''],
    rows: [['', '']],
    imageURL: '',
  })
  dirty.value = true
}

function removeBlock(index: number) {
  const block = blocks.value[index]
  if (block.id) remove(block.id)
  blocks.value.splice(index, 1)
  dirty.value = true
}

function moveBlock(index: number, delta: number) {
  const [block] = blocks.value.splice(index, 1)
  blocks.value.splice(index + delta, 0, block)
  dirty.value = true
}

function captureText(block: EditableBlock, event: Event) {
  if (!block.id) return
  const area = event.target as HTMLTextAreaElement
  addText(block.id, area.selectionStart, area.selectionEnd)
}

function changeList(block: EditableBlock, index?: number) {
  if (index === undefined) block.items.push('')
  else block.items.splice(index, 1)
  dirty.value = true
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
  dirty.value = true
}

function save() {
  if (!dirty.value || props.pending) return
  emit('change', { action: 'save', content: content.value })
}
</script>

<template>
  <section class="document-panel">
    <div class="document-heading">
      <div>
        <p class="document-muted">{{ t('documents.editor.aiNotice') }}</p>
        <h2>{{ t('documents.editor.revision', { number: document.revision }) }}</h2>
      </div>
      <span v-if="dirty" role="status">{{ t('documents.editor.unsaved') }}</span>
    </div>

    <form @submit.prevent="save">
      <fieldset class="document-fields" :disabled="pending" @input="dirty = true">
        <label class="document-field">
          {{ t('documents.editor.title') }}
          <input v-model="title" required maxlength="160">
        </label>
        <article v-for="(block, index) in blocks" :key="block.key" class="editor-block">
          <div class="document-actions">
            <strong>{{ t('documents.blocks.' + block.kind) }}</strong>
            <button
              v-if="block.id"
              type="button"
              :aria-pressed="selected.has(block.id)"
              @click="block.id && toggle(block.id)"
            >
              {{ t(selected.has(block.id) ? 'documents.editor.deselect' : 'documents.editor.select') }}
            </button>
            <button type="button" :disabled="index === 0" @click="moveBlock(index, -1)">
              {{ t('documents.editor.up') }}
            </button>
            <button type="button" :disabled="index === blocks.length - 1" @click="moveBlock(index, 1)">
              {{ t('documents.editor.down') }}
            </button>
            <button type="button" :disabled="blocks.length === 1" @click="removeBlock(index)">
              {{ t('documents.editor.remove') }}
            </button>
          </div>

          <textarea
            v-model="block.text"
            rows="3"
            maxlength="2500"
            :aria-label="t('documents.editor.blockText', { number: index + 1 })"
            @input="block.id && remove(block.id)"
            @select="captureText(block, $event)"
          />

          <div v-if="block.kind === 'list'" class="document-fields">
            <div v-for="(_, itemIndex) in block.items" :key="itemIndex" class="document-actions">
              <input
                v-model="block.items[itemIndex]"
                maxlength="400"
                :aria-label="t('documents.editor.item', { number: itemIndex + 1 })"
              >
              <button type="button" :disabled="block.items.length === 1" @click="changeList(block, itemIndex)">
                {{ t('documents.editor.removeItem') }}
              </button>
            </div>
            <button type="button" :disabled="block.items.length >= 20" @click="changeList(block)">
              {{ t('documents.editor.addItem') }}
            </button>
          </div>

          <div v-if="block.kind === 'table'" class="document-fields">
            <div class="document-table">
              <table>
                <tbody>
                  <tr v-for="(row, rowIndex) in block.rows" :key="rowIndex">
                    <td v-for="(_, column) in row" :key="column">
                      <input
                        v-model="row[column]"
                        maxlength="400"
                        :aria-label="t('documents.editor.cell', { row: rowIndex + 1, column: column + 1 })"
                      >
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="document-actions">
              <button type="button" :disabled="block.rows.length >= 20" @click="changeTable(block, 'row', true)">
                {{ t('documents.editor.addRow') }}
              </button>
              <button type="button" :disabled="block.rows.length === 1" @click="changeTable(block, 'row', false)">
                {{ t('documents.editor.removeRow') }}
              </button>
              <button type="button" :disabled="block.rows[0].length >= 8" @click="changeTable(block, 'column', true)">
                {{ t('documents.editor.addColumn') }}
              </button>
              <button type="button" :disabled="block.rows[0].length === 1" @click="changeTable(block, 'column', false)">
                {{ t('documents.editor.removeColumn') }}
              </button>
            </div>
          </div>

          <label v-if="block.kind === 'image'" class="document-field">
            {{ t('documents.editor.imageURL') }}
            <input v-model="block.imageURL" type="url" required pattern="https://.+" maxlength="2048">
          </label>
        </article>
      </fieldset>

      <div class="document-actions editor-toolbar">
        <select v-model="newKind" :aria-label="t('documents.editor.blockKind')" :disabled="pending">
          <option v-for="kind in kinds" :key="kind" :value="kind">{{ t('documents.blocks.' + kind) }}</option>
        </select>
        <button type="button" :disabled="pending || blocks.length >= 80" @click="addBlock">
          {{ t('documents.editor.add') }}
        </button>
        <button type="submit" class="document-primary" :disabled="pending || !dirty">
          {{ t('documents.editor.save') }}
        </button>
        <button v-if="dirty" type="button" :disabled="pending" @click="reset">
          {{ t('documents.editor.discard') }}
        </button>
      </div>
    </form>

    <div class="document-actions editor-toolbar">
      <button type="button" :aria-expanded="showPreview" @click="showPreview = !showPreview">
        {{ t(showPreview ? 'documents.editor.hidePreview' : 'documents.editor.preview') }}
      </button>
      <button type="button" :disabled="pending || dirty || !document.can_undo" @click="emit('change', { action: 'undo' })">
        {{ t('documents.editor.undo') }}
      </button>
      <button type="button" :disabled="pending || dirty || !document.can_redo" @click="emit('change', { action: 'redo' })">
        {{ t('documents.editor.redo') }}
      </button>
      <button type="button" :disabled="pending" @click="emit('history')">{{ t('documents.editor.history') }}</button>
      <button v-if="anchors.length" type="button" @click="clear">
        {{ t('documents.editor.clearSelection', { count: anchors.length }) }}
      </button>
    </div>

    <DocumentPreview v-if="showPreview" :title="content.title" :blocks="content.blocks" :spec="spec" />

    <label v-if="history.length" class="document-field">
      {{ t('documents.editor.chooseRevision') }}
      <select
        :value="previous?.revision ?? ''"
        :disabled="pending"
        @change="emit('viewRevision', Number(($event.target as HTMLSelectElement).value))"
      >
        <option value="" disabled>{{ t('documents.editor.chooseRevision') }}</option>
        <option v-for="item in history" :key="item.revision" :value="item.revision">
          {{ t('documents.editor.revisionOption', { number: item.revision, title: item.title }) }}
        </option>
      </select>
    </label>
    <div v-if="previous" class="document-fields">
      <DocumentPreview :title="previous.content.title" :blocks="previous.content.blocks" :spec="spec" />
      <button
        type="button"
        :disabled="pending || dirty || previous.revision === document.revision"
        @click="previous && emit('change', { action: 'restore', target_revision: previous.revision })"
      >
        {{ t('documents.editor.restore') }}
      </button>
    </div>
  </section>
</template>
