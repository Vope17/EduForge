<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { documentErrorKey, uploadSlideImage } from '../../api/documents'
import type { Block, BlockKind, Change, Content, PatchProposal, RevisionSummary, SavedDocument, SlideLayout, TemplateSpec } from '../../api/documents'
import { useSelection, type Anchor } from '../../selection/useSelection'
import AppIcon from '../../components/AppIcon.vue'
import DownloadControls from '../DownloadControls.vue'
import EditorTabs from '../EditorTabs.vue'
import AIEditPanel from '../AIEditPanel.vue'
import RevisionPanel from '../RevisionPanel.vue'
import { useUndo } from '../useUndo'
import SlideCanvas from './SlideCanvas.vue'

// PowerPoint-style editing as in Paper2Lab: slide rail, toolbar, the slide on
// a stage, the selected block and the speaker notes underneath, and the AI
// side panel. Blocks are picked by clicking (Shift adds), by dragging a box
// from the slide's empty area, or by highlighting text.
const props = defineProps<{
  document: SavedDocument
  spec?: TemplateSpec
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
const layouts: SlideLayout[] = ['title_body', 'two_columns', 'image_text']
const kinds: BlockKind[] = ['paragraph', 'list', 'image']
const limits = { pages: 12, elements: 8, items: 8 }
const uploading = ref(false)
const uploadError = ref('')
const uploadController = new AbortController()
const pending = computed(() => props.pending || uploading.value)
const draft = ref<Content>({ title: '', blocks: [] })
const saved = ref('')
const dirty = computed(() => JSON.stringify(draft.value) !== saved.value)
const tab = ref('edit')
const aiOpen = ref(false)
const pageIndex = ref(0)
const elementIndex = ref(0)
const canvasHost = ref<HTMLElement>()
const player = ref<HTMLDialogElement>()
const playing = ref(false)
const playerIndex = ref(0)
const historyIndex = ref(0)
const fullscreenFailed = ref(false)
let ownsFullscreen = false

const page = computed(() => draft.value.blocks[pageIndex.value])
const elements = computed(() => page.value?.children ?? [])
const element = computed(() => elements.value[elementIndex.value])
const originalPage = computed(() => props.previous?.content.blocks[historyIndex.value])

const selection = useSelection(() => props.document.id, () => {
  const root = canvasHost.value
  if (!root) return []
  return Array.from(root.querySelectorAll<HTMLElement>('[data-slide-block-id]')).flatMap((target) => {
    const blockID = target.dataset.slideBlockId
    if (!blockID) return []
    const rect = target.getBoundingClientRect()
    return [{ blockID, rect: { left: rect.left, top: rect.top, width: rect.width, height: rect.height } }]
  })
})
const { anchors, rectangle } = selection
const selected = computed(() => new Set(aiOpen.value ? anchors.value.map((anchor) => anchor.block_id) : []))
const notesSelected = computed(() => anchors.value.some((anchor) => anchor.block_id === page.value?.id && anchor.field === 'notes'))

const undo = useUndo(
  () => draft.value,
  (value: Content) => {
    draft.value = value
    pageIndex.value = Math.min(pageIndex.value, value.blocks.length - 1)
    elementIndex.value = Math.min(elementIndex.value, (value.blocks[pageIndex.value]?.children?.length ?? 1) - 1)
  },
  { enabled: () => tab.value === 'edit' && !playing.value },
)

function reset() {
  draft.value = JSON.parse(JSON.stringify(props.document.content)) as Content
  saved.value = JSON.stringify(draft.value)
  pageIndex.value = Math.max(0, Math.min(pageIndex.value, draft.value.blocks.length - 1))
  elementIndex.value = 0
  selection.clear()
  void nextTick(() => undo.reset())
}

watch(() => props.document, reset, { immediate: true })
watch(() => props.previous, () => { historyIndex.value = 0 })
onBeforeRouteLeave(() => !dirty.value || window.confirm(t('documents.editor.discardPrompt')))

function save() {
  if (!dirty.value || pending.value) return
  ;(document.activeElement as HTMLElement | null)?.blur?.()
  emit('change', { action: 'save', content: draft.value })
}

function discard() {
  if (window.confirm(t('documents.editor.discardConfirm'))) reset()
}

// Editing text invalidates the AI selection inside that block.
function changed(block?: Block) {
  if (block?.id) selection.remove(block.id)
}

async function upload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const target = element.value
  input.value = ''
  if (!file || !target || pending.value) return
  uploadError.value = ''
  if (file.size > 8 * 1024 * 1024) { uploadError.value = 'slides.imageSizeError'; return }
  uploading.value = true
  try {
    target.image_url = await uploadSlideImage(props.document.id, file, uploadController.signal)
    changed(target)
  } catch (error) {
    if (!uploadController.signal.aborted) uploadError.value = documentErrorKey(error)
  } finally {
    uploading.value = false
  }
}

function showPage(index: number) {
  pageIndex.value = index
  elementIndex.value = 0
}

function blankPage(): Block {
  return {
    kind: 'slide', text: '', layout: layouts.includes(props.spec?.layout as SlideLayout) ? props.spec?.layout as SlideLayout : 'title_body', notes: '',
    children: [{ kind: 'heading', text: t('slides.newSlideTitle') }, { kind: 'paragraph', text: '' }],
  }
}

function addPage() {
  draft.value.blocks.splice(pageIndex.value + 1, 0, blankPage())
  showPage(pageIndex.value + 1)
}

function duplicatePage() {
  const copy = JSON.parse(JSON.stringify(page.value)) as Block
  delete copy.id
  for (const child of copy.children ?? []) delete child.id
  draft.value.blocks.splice(pageIndex.value + 1, 0, copy)
  showPage(pageIndex.value + 1)
}

function removePage() {
  if (draft.value.blocks.length <= 1 || !window.confirm(t('slides.removePageConfirm'))) return
  const current = draft.value.blocks.splice(pageIndex.value, 1)[0]
  for (const block of [current, ...(current.children ?? [])]) if (block.id) selection.remove(block.id)
  showPage(Math.min(pageIndex.value, draft.value.blocks.length - 1))
}

function movePage(delta: number) {
  const target = pageIndex.value + delta
  if (target < 0 || target >= draft.value.blocks.length) return
  const [current] = draft.value.blocks.splice(pageIndex.value, 1)
  draft.value.blocks.splice(target, 0, current)
  pageIndex.value = target
}

// Clicking a block edits it; with the AI panel open it also selects it
// (Shift/Ctrl/⌘ adds to the selection, which is kept across slides).
function selectElement(index: number, additive: boolean) {
  elementIndex.value = index
  const id = elements.value[index]?.id
  if (!aiOpen.value || !id) return
  if (!additive) selection.clear()
  selection.toggle(id)
}

function addElement(kind: BlockKind) {
  const children = page.value?.children
  if (!children || children.length >= limits.elements) return
  children.push({
    kind, text: kind === 'image' ? t('slides.imagePlaceholder') : '',
    ...(kind === 'list' ? { items: [''] } : {}),
    ...(kind === 'image' ? { image_url: '' } : {}),
  })
  elementIndex.value = children.length - 1
}

function removeElement() {
  const current = page.value?.children?.splice(elementIndex.value, 1)[0]
  if (current?.id) selection.remove(current.id)
  elementIndex.value = 0
}

function moveElement(delta: number) {
  const children = page.value?.children
  const target = elementIndex.value + delta
  if (!children || target < 0 || target >= children.length) return
  const [current] = children.splice(elementIndex.value, 1)
  children.splice(target, 0, current)
  elementIndex.value = target
}

function captureText(block: Block | undefined, event: Event, field = 'text') {
  if (!aiOpen.value || !block?.id) return
  const target = event.target as HTMLTextAreaElement
  selection.addText(block.id, target.selectionStart, target.selectionEnd, field)
}

function beginBoxSelect(event: PointerEvent) {
  if (aiOpen.value && !pending.value) selection.beginRectangle(event)
}

const pageNumber = (blockID: string) => props.document.content.blocks.findIndex((root) => root.id === blockID || root.children?.some((child) => child.id === blockID)) + 1
function fieldLabel(blockID: string, field?: string) {
  const where = t('slides.pageNumber', { number: pageNumber(blockID) })
  if (field === 'notes') return where + ' · ' + t('slides.notes')
  if (field?.startsWith('items.')) return where + ' · ' + t('documents.editor.item', { number: Number(field.slice(6)) + 1 })
  const block = props.document.content.blocks.flatMap((root) => root.children ?? []).find((child) => child.id === blockID)
  return where + ' · ' + (block ? t('documents.blocks.' + block.kind) : t('slides.elementText'))
}
const labels = computed(() => anchors.value.map((anchor) =>
  fieldLabel(anchor.block_id, anchor.field) + (anchor.start === undefined ? '' : ' · ' + t('ai.partialText'))))
const describe = (change: PatchProposal['changes'][number]) => fieldLabel(change.block_id, change.field)

function propose(instruction: string) {
  emit('propose', instruction, anchors.value.map((anchor) => ({ ...anchor })))
}

// About 260 Han characters or 130 English words per spoken minute (as Paper2Lab).
const notesSeconds = computed(() => {
  const text = page.value?.notes ?? ''
  const han = (text.match(/\p{Script=Han}/gu) ?? []).length
  const words = (text.replace(/\p{Script=Han}/gu, ' ').match(/[A-Za-z0-9]+/g) ?? []).length
  return Math.round((han / 260 + words / 130) * 60)
})

async function present() {
  await document.fonts?.ready
  playerIndex.value = pageIndex.value
  playing.value = true
  await nextTick()
  player.value?.showModal()
}

function closePlayer() {
  player.value?.close()
  playing.value = false
  if (ownsFullscreen && document.fullscreenElement) void document.exitFullscreen().catch(() => { fullscreenFailed.value = true })
  ownsFullscreen = false
}

async function fullscreen() {
  fullscreenFailed.value = false
  try {
    await document.documentElement.requestFullscreen()
    ownsFullscreen = true
  } catch {
    fullscreenFailed.value = true
  }
}

function playerKey(event: KeyboardEvent) {
  if (event.key === 'ArrowRight' || event.key === ' ') {
    event.preventDefault()
    playerIndex.value = Math.min(playerIndex.value + 1, draft.value.blocks.length - 1)
  } else if (event.key === 'ArrowLeft') {
    event.preventDefault()
    playerIndex.value = Math.max(playerIndex.value - 1, 0)
  } else if (event.key === 'Escape') {
    event.preventDefault()
    closePlayer()
  }
}

const tabs = computed(() => [
  { key: 'edit', label: t('editor.tabs.edit'), icon: 'pencil' as const },
  { key: 'history', label: t('editor.tabs.history'), icon: 'history' as const },
])

onBeforeUnmount(() => {
  uploadController.abort()
  selection.cancelRectangle()
  if (ownsFullscreen && document.fullscreenElement) void document.exitFullscreen().catch(() => { fullscreenFailed.value = true })
})
</script>

<template>
  <section class="editor-sheet slides-workspace">
    <header class="editor-head">
      <div class="grow">
        <span class="eyebrow">{{ t('documents.editor.revision', { number: document.revision }) }}</span>
        <label class="inline-field">
          {{ t('slides.deckTitle') }}
          <input
            v-model="draft.title"
            maxlength="160"
            required
            :disabled="pending"
          >
        </label>
      </div>
      <DownloadControls
        :document="document"
        kind="slides"
        :disabled="pending"
        :dirty="dirty"
      />
      <button
        type="button"
        class="small"
        :disabled="pending"
        @click="present"
      >
        <AppIcon name="present" />{{ t('slides.present') }}
      </button>
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
        <div class="slide-editor">
          <aside
            class="slide-rail"
            :aria-label="t('slides.pages')"
          >
            <button
              v-for="(item, index) in draft.blocks"
              :key="item.id ?? index"
              type="button"
              class="slide-thumb plain"
              :class="{ active: index === pageIndex }"
              :aria-pressed="index === pageIndex"
              :aria-label="t('slides.pageNumber', { number: index + 1 })"
              @click="showPage(index)"
            >
              <span class="slide-thumb-no">{{ index + 1 }}</span>
              <SlideCanvas
                :page="item"
                :spec="spec"
              />
            </button>
            <button
              type="button"
              class="small"
              :disabled="pending || draft.blocks.length >= limits.pages"
              @click="addPage"
            >
              <AppIcon name="plus" />{{ t('slides.addPage') }}
            </button>
          </aside>

          <div
            v-if="page"
            class="slide-main"
          >
            <div
              class="slide-toolbar"
              role="toolbar"
            >
              <button
                v-for="kind in kinds"
                :key="kind"
                type="button"
                class="tool wide"
                :disabled="pending || elements.length >= limits.elements"
                @click="addElement(kind)"
              >
                <AppIcon name="plus" />{{ t('documents.blocks.' + kind) }}
              </button>
              <span class="tool-sep" />
              <label class="inline-field">
                {{ t('documents.template.layout') }}
                <select
                  v-model="page.layout"
                  :disabled="pending"
                >
                  <option
                    v-for="layout in layouts"
                    :key="layout"
                    :value="layout"
                  >{{ t('slides.layouts.' + layout) }}</option>
                </select>
              </label>
              <span class="grow" />
              <button
                type="button"
                class="icon-button"
                :title="t('slides.previousPosition')"
                :aria-label="t('slides.previousPosition')"
                :disabled="pending || pageIndex === 0"
                @click="movePage(-1)"
              >
                ↑
              </button>
              <button
                type="button"
                class="icon-button"
                :title="t('slides.nextPosition')"
                :aria-label="t('slides.nextPosition')"
                :disabled="pending || pageIndex === draft.blocks.length - 1"
                @click="movePage(1)"
              >
                ↓
              </button>
              <button
                type="button"
                class="text-button"
                :disabled="pending || draft.blocks.length >= limits.pages"
                @click="duplicatePage"
              >
                {{ t('slides.duplicatePage') }}
              </button>
              <button
                type="button"
                class="text-button danger"
                :disabled="pending || draft.blocks.length === 1"
                @click="removePage"
              >
                {{ t('slides.removePage') }}
              </button>
            </div>

            <div class="slide-stage">
              <p
                v-if="aiOpen"
                class="caption slide-stage-hint"
              >
                {{ t('slides.selectionHint') }}
              </p>
              <div
                ref="canvasHost"
                class="slide-stage-canvas"
              >
                <SlideCanvas
                  :page="page"
                  :spec="spec"
                  :selected="selected"
                  :editing-index="elementIndex"
                  :interactive="!pending"
                  @select="selectElement"
                  @pointerdown="beginBoxSelect"
                  @pointermove="selection.moveRectangle"
                  @pointerup="selection.finishRectangle"
                  @pointercancel="selection.cancelRectangle"
                >
                  <div
                    v-if="rectangle"
                    class="slide-selection-box"
                    :style="{ left: rectangle.left + 'px', top: rectangle.top + 'px', width: rectangle.width + 'px', height: rectangle.height + 'px' }"
                  />
                </SlideCanvas>
              </div>
            </div>

            <div class="slide-panels">
              <section
                v-if="element"
                class="slide-panel"
                :class="{ 'ai-selected': aiOpen && element.id && selected.has(element.id) }"
                :aria-label="t('slides.element')"
              >
                <div class="slide-panel-head">
                  <strong>{{ t('documents.blocks.' + element.kind) }}</strong>
                  <span class="caption">{{ t('slides.elementPosition', { number: elementIndex + 1, total: elements.length }) }}</span>
                  <span class="grow" />
                  <button
                    type="button"
                    class="icon-button"
                    :aria-label="t('documents.editor.up')"
                    :title="t('documents.editor.up')"
                    :disabled="pending || elementIndex === 0"
                    @click="moveElement(-1)"
                  >
                    ↑
                  </button>
                  <button
                    type="button"
                    class="icon-button"
                    :aria-label="t('documents.editor.down')"
                    :title="t('documents.editor.down')"
                    :disabled="pending || elementIndex === elements.length - 1"
                    @click="moveElement(1)"
                  >
                    ↓
                  </button>
                  <button
                    type="button"
                    class="icon-button danger"
                    :aria-label="t('documents.editor.remove')"
                    :title="t('documents.editor.remove')"
                    :disabled="pending || element.kind === 'heading' || elements.length <= 2"
                    @click="removeElement"
                  >
                    ×
                  </button>
                </div>
                <textarea
                  v-model="element.text"
                  rows="3"
                  :maxlength="element.kind === 'heading' ? 120 : element.kind === 'image' ? 300 : 800"
                  :disabled="pending"
                  :aria-label="t('slides.elementText')"
                  :placeholder="t('slides.elementText')"
                  @input="changed(element)"
                  @select="captureText(element, $event)"
                />
                <template v-if="element.kind === 'list'">
                  <div
                    v-for="(_, index) in element.items"
                    :key="index"
                    class="slide-item"
                  >
                    <span aria-hidden="true">•</span>
                    <textarea
                      v-model="element.items![index]"
                      rows="1"
                      maxlength="400"
                      :disabled="pending"
                      :aria-label="t('documents.editor.item', { number: index + 1 })"
                      @input="changed(element)"
                      @select="captureText(element, $event, 'items.' + index)"
                    />
                    <button
                      type="button"
                      class="icon-button"
                      :aria-label="t('documents.editor.removeItem')"
                      :disabled="pending || (element.items?.length ?? 0) <= 1"
                      @click="element.items?.splice(index, 1); changed(element)"
                    >
                      ×
                    </button>
                  </div>
                  <button
                    type="button"
                    class="doc-insert plain visible"
                    :disabled="pending || (element.items?.length ?? 0) >= limits.items"
                    @click="element.items?.push(''); changed(element)"
                  >
                    <AppIcon name="plus" />{{ t('documents.editor.addItem') }}
                  </button>
                </template>
                <template v-if="element.kind === 'image'">
                  <label class="document-field">
                    {{ t('documents.editor.imageURL') }}
                    <input
                      v-model="element.image_url"
                      type="text"
                      maxlength="2048"
                      :disabled="pending"
                      @input="changed(element)"
                    >
                  </label>
                  <label class="file-picker">
                    <AppIcon name="upload" />{{ t('slides.uploadImage') }}
                    <input
                      type="file"
                      accept="image/png,image/jpeg"
                      :disabled="pending"
                      @change="upload"
                    >
                  </label>
                  <p class="caption">
                    {{ t('slides.uploadHint') }}
                  </p>
                  <p
                    v-if="uploading"
                    class="caption"
                    role="status"
                  >
                    {{ t('slides.uploadingImage') }}
                  </p>
                  <p
                    v-if="uploadError"
                    class="document-error"
                    role="alert"
                  >
                    {{ t(uploadError) }}
                  </p>
                </template>
              </section>

              <section
                class="slide-panel slide-notes"
                :class="{ 'ai-selected': aiOpen && notesSelected }"
                :aria-label="t('slides.notes')"
              >
                <div class="slide-panel-head">
                  <strong>{{ t('slides.notes') }}</strong>
                  <span class="caption">{{ t('slides.pageCounter', { current: pageIndex + 1, total: draft.blocks.length }) }}</span>
                  <span class="grow" />
                  <span class="caption">{{ t('slides.speakingTime', { seconds: notesSeconds }) }}</span>
                  <label
                    v-if="aiOpen && page.id"
                    class="check ai-select"
                  ><input
                    type="checkbox"
                    :checked="notesSelected"
                    @change="page.id && selection.toggleField(page.id, 'notes')"
                  >{{ t('slides.selectNotes') }}</label>
                </div>
                <textarea
                  v-model="page.notes"
                  rows="6"
                  maxlength="2500"
                  :disabled="pending"
                  :aria-label="t('slides.notes')"
                  :placeholder="t('slides.notesPlaceholder')"
                  @input="changed(page)"
                  @select="captureText(page, $event, 'notes')"
                />
              </section>
            </div>
          </div>
        </div>
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
          {{ t('slides.selectionHint') }}
        </template>
      </AIEditPanel>
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
        <template v-if="previous && originalPage">
          <label class="document-field revision-page">
            {{ t('slides.pages') }}
            <select v-model="historyIndex">
              <option
                v-for="(_, index) in previous.content.blocks"
                :key="index"
                :value="index"
              >{{ t('slides.pageNumber', { number: index + 1 }) }}</option>
            </select>
          </label>
          <SlideCanvas
            :page="originalPage"
            :spec="spec"
          />
          <p class="slide-history-notes">
            {{ originalPage.notes }}
          </p>
        </template>
      </RevisionPanel>
    </div>

    <dialog
      ref="player"
      class="slide-player"
      :aria-label="t('slides.present')"
      @keydown="playerKey"
      @cancel.prevent="closePlayer"
      @close="playing = false"
    >
      <template v-if="playing">
        <SlideCanvas
          :page="draft.blocks[playerIndex]"
          :spec="spec"
        />
        <div class="slide-player-controls">
          <button
            type="button"
            :disabled="playerIndex === 0"
            @click="playerIndex--"
          >
            {{ t('slides.previousPage') }}
          </button>
          <span>{{ t('slides.pageCounter', { current: playerIndex + 1, total: draft.blocks.length }) }}</span>
          <button
            type="button"
            :disabled="playerIndex === draft.blocks.length - 1"
            @click="playerIndex++"
          >
            {{ t('slides.nextPage') }}
          </button>
          <button
            type="button"
            @click="fullscreen"
          >
            {{ t('slides.fullscreen') }}
          </button>
          <button
            type="button"
            @click="closePlayer"
          >
            {{ t('slides.closePresentation') }}
          </button>
        </div>
        <p
          v-if="fullscreenFailed"
          role="status"
        >
          {{ t('slides.fullscreenFailed') }}
        </p>
        <details>
          <summary>{{ t('slides.notes') }}</summary><p class="slide-history-notes">
            {{ draft.blocks[playerIndex].notes }}
          </p>
        </details>
      </template>
    </dialog>
  </section>
</template>
