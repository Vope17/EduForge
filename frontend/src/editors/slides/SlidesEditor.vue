<script setup lang="ts">
import DownloadControls from '../DownloadControls.vue'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { documentErrorKey, uploadSlideImage } from '../../api/documents'
import type { Block, BlockKind, Change, Content, PatchProposal, RevisionSummary, SavedDocument, SlideLayout, TemplateSpec } from '../../api/documents'
import { useSelection, type Anchor } from '../../selection/useSelection'
import SlideCanvas from './SlideCanvas.vue'

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
const uploading = ref(false)
const uploadError = ref('')
const uploadController = new AbortController()
const pending = computed(() => props.pending || uploading.value)
const draft = ref<Content>({ title: '', blocks: [] })
const pageIndex = ref(0)
const elementIndex = ref(0)
const dirty = ref(false)
const newKind = ref<BlockKind>('paragraph')
const command = ref('')
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
const selected = computed(() => new Set(anchors.value.map((anchor) => anchor.block_id)))
const notesSelected = computed(() => anchors.value.some((anchor) => anchor.block_id === page.value?.id && anchor.field === 'notes'))

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
    const url = await uploadSlideImage(props.document.id, file, uploadController.signal)
    target.image_url = url
    changed(target)
  } catch (error) {
    if (!uploadController.signal.aborted) uploadError.value = documentErrorKey(error)
  } finally {
    uploading.value = false
  }
}

function reset() {
  draft.value = JSON.parse(JSON.stringify(props.document.content)) as Content
  pageIndex.value = Math.min(pageIndex.value, draft.value.blocks.length - 1)
  elementIndex.value = 0
  dirty.value = false
  selection.clear()
}

watch(() => props.document, reset, { immediate: true })
watch(() => props.previous, () => { historyIndex.value = 0 })
onBeforeRouteLeave(() => !dirty.value || window.confirm(t('documents.editor.discardPrompt')))

function changed(block?: Block) {
  dirty.value = true
  if (block?.id) selection.remove(block.id)
}

function blankPage(): Block {
  return {
    kind: 'slide', text: '', layout: layouts.includes(props.spec?.layout as SlideLayout) ? props.spec?.layout as SlideLayout : 'title_body', notes: '',
    children: [{ kind: 'heading', text: t('slides.newSlideTitle') }, { kind: 'paragraph', text: '' }],
  }
}

function addPage() {
  draft.value.blocks.push(blankPage())
  pageIndex.value = draft.value.blocks.length - 1
  elementIndex.value = 0
  changed()
}

function duplicatePage() {
  const copy = JSON.parse(JSON.stringify(page.value)) as Block
  delete copy.id
  for (const child of copy.children ?? []) delete child.id
  draft.value.blocks.splice(pageIndex.value + 1, 0, copy)
  pageIndex.value++
  elementIndex.value = 0
  changed()
}

function removePage() {
  const current = draft.value.blocks.splice(pageIndex.value, 1)[0]
  for (const block of [current, ...(current.children ?? [])]) if (block.id) selection.remove(block.id)
  pageIndex.value = Math.min(pageIndex.value, draft.value.blocks.length - 1)
  elementIndex.value = 0
  changed()
}

function movePage(delta: number) {
  const target = pageIndex.value + delta
  const [current] = draft.value.blocks.splice(pageIndex.value, 1)
  draft.value.blocks.splice(target, 0, current)
  pageIndex.value = target
  changed()
}

function selectElement(index: number, additive: boolean) {
  elementIndex.value = index
  const id = elements.value[index]?.id
  if (!id) return
  if (!additive) selection.clear()
  selection.toggle(id)
}

function addElement() {
  if (!page.value.children) return
  page.value.children.push({
    kind: newKind.value, text: newKind.value === 'image' ? t('slides.imagePlaceholder') : '',
    ...(newKind.value === 'list' ? { items: [''] } : {}),
    ...(newKind.value === 'image' ? { image_url: '' } : {}),
  })
  elementIndex.value = page.value.children.length - 1
  changed()
}

function removeElement() {
  const current = page.value.children?.splice(elementIndex.value, 1)[0]
  if (current?.id) selection.remove(current.id)
  elementIndex.value = 0
  changed()
}

function moveElement(delta: number) {
  const children = page.value.children
  if (!children) return
  const target = elementIndex.value + delta
  const [current] = children.splice(elementIndex.value, 1)
  children.splice(target, 0, current)
  elementIndex.value = target
  changed()
}

function captureText(block: Block | undefined, event: Event, field = 'text') {
  if (!block?.id) return
  const target = event.target as HTMLTextAreaElement
  selection.addText(block.id, target.selectionStart, target.selectionEnd, field)
}

function propose() {
  if (!dirty.value && command.value.trim() && anchors.value.length) {
    emit('propose', command.value, anchors.value.map((anchor) => ({ ...anchor })))
  }
}

function changePageNumber(blockID: string) {
  return props.document.content.blocks.findIndex((root) => root.id === blockID || root.children?.some((child) => child.id === blockID)) + 1
}

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

onBeforeUnmount(() => {
  uploadController.abort()
  selection.cancelRectangle()
  if (ownsFullscreen && document.fullscreenElement) void document.exitFullscreen().catch(() => { fullscreenFailed.value = true })
})
</script>

<template>
  <section class="slides-workspace">
    <DownloadControls :document="document" kind="slides" :disabled="pending" :dirty="dirty" />
    <div class="document-actions">
      <strong>{{ t('documents.editor.revision', { number: document.revision }) }}</strong>
      <span
        v-if="dirty"
        role="status"
      >{{ t('documents.editor.unsaved') }}</span>
      <button
        type="button"
        class="document-primary"
        :disabled="pending || !dirty"
        @click="emit('change', { action: 'save', content: draft })"
      >
        {{ t('documents.editor.save') }}
      </button>
      <button
        v-if="dirty"
        type="button"
        :disabled="pending"
        @click="reset"
      >
        {{ t('documents.editor.discard') }}
      </button>
      <button
        type="button"
        :disabled="pending || dirty || !document.can_undo"
        @click="emit('change', { action: 'undo' })"
      >
        {{ t('documents.editor.undo') }}
      </button>
      <button
        type="button"
        :disabled="pending || dirty || !document.can_redo"
        @click="emit('change', { action: 'redo' })"
      >
        {{ t('documents.editor.redo') }}
      </button>
      <button
        type="button"
        :disabled="pending"
        @click="present"
      >
        {{ t('slides.present') }}
      </button>
      <button
        type="button"
        :disabled="pending"
        @click="emit('history')"
      >
        {{ t('documents.editor.history') }}
      </button>
    </div>
    <label class="document-field">
      {{ t('slides.deckTitle') }}
      <input
        v-model="draft.title"
        maxlength="160"
        required
        :disabled="pending"
        @input="changed()"
      >
    </label>
    <div class="slides-editor-grid">
      <aside
        class="slides-filmstrip"
        :aria-label="t('slides.pages')"
      >
        <button
          v-for="(item, index) in draft.blocks"
          :key="item.id ?? index"
          type="button"
          :class="{ active: index === pageIndex }"
          :aria-pressed="index === pageIndex"
          :aria-label="t('slides.pageNumber', { number: index + 1 })"
          @click="pageIndex = index; elementIndex = 0"
        >
          <SlideCanvas
            :page="item"
            :spec="spec"
          />
          <span>{{ t('slides.pageNumber', { number: index + 1 }) }}</span>
        </button>
        <button
          type="button"
          :disabled="pending || draft.blocks.length >= 12"
          @click="addPage"
        >
          {{ t('slides.addPage') }}
        </button>
      </aside>

      <div
        v-if="page"
        class="slides-stage"
      >
        <div class="document-actions">
          <span>{{ t('slides.pageCounter', { current: pageIndex + 1, total: draft.blocks.length }) }}</span>
          <button
            type="button"
            :disabled="pending || draft.blocks.length >= 12"
            @click="duplicatePage"
          >
            {{ t('slides.duplicatePage') }}
          </button>
          <button
            type="button"
            :disabled="pending || draft.blocks.length === 1"
            @click="removePage"
          >
            {{ t('slides.removePage') }}
          </button>
          <button
            type="button"
            :disabled="pending || pageIndex === 0"
            @click="movePage(-1)"
          >
            {{ t('slides.previousPosition') }}
          </button>
          <button
            type="button"
            :disabled="pending || pageIndex === draft.blocks.length - 1"
            @click="movePage(1)"
          >
            {{ t('slides.nextPosition') }}
          </button>
        </div>
        <p class="document-muted">
          {{ t('slides.selectionHint') }}
        </p>
        <div ref="canvasHost">
          <SlideCanvas
            :page="page"
            :spec="spec"
            :selected="selected"
            :interactive="!pending"
            @select="selectElement"
            @pointerdown="!pending && selection.beginRectangle($event)"
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
        <label class="document-field">
          {{ t('slides.notes') }}
          <textarea
            v-model="page.notes"
            rows="5"
            maxlength="2500"
            :disabled="pending"
            @input="changed(page)"
            @select="captureText(page, $event, 'notes')"
          />
        </label>
        <button
          v-if="page.id"
          type="button"
          :aria-pressed="notesSelected"
          :disabled="pending"
          @click="selection.toggleField(page.id, 'notes')"
        >
          {{ t('slides.selectNotes') }}
        </button>
      </div>

      <aside
        v-if="page"
        class="slides-inspector document-panel"
      >
        <fieldset
          class="document-fields"
          :disabled="pending"
        >
          <legend>{{ t('slides.elementSettings') }}</legend>
          <label class="document-field">
            {{ t('documents.template.layout') }}
            <select
              v-model="page.layout"
              @change="changed()"
            >
              <option
                v-for="layout in layouts"
                :key="layout"
                :value="layout"
              >{{ t('slides.layouts.' + layout) }}</option>
            </select>
          </label>
          <label class="document-field">
            {{ t('slides.element') }}
            <select v-model="elementIndex">
              <option
                v-for="(item, index) in elements"
                :key="item.id ?? index"
                :value="index"
              >{{ t('documents.blocks.' + item.kind) }} {{ index + 1 }}</option>
            </select>
          </label>
          <template v-if="element">
            <label class="document-field">
              {{ t('slides.elementText') }}
              <textarea
                v-model="element.text"
                rows="5"
                :maxlength="element.kind === 'heading' ? 120 : element.kind === 'image' ? 300 : 800"
                @input="changed(element)"
                @select="captureText(element, $event)"
              />
            </label>
            <template v-if="element.kind === 'list'">
              <label
                v-for="(_, index) in element.items"
                :key="index"
                class="document-field"
              >
                {{ t('documents.editor.item', { number: index + 1 }) }}
                <textarea
                  v-model="element.items![index]"
                  rows="2"
                  maxlength="400"
                  @input="changed(element)"
                  @select="captureText(element, $event, 'items.' + index)"
                />
              </label>
              <button
                type="button"
                :disabled="(element.items?.length ?? 0) >= 8"
                @click="element.items?.push(''); changed(element)"
              >
                {{ t('documents.editor.addItem') }}
              </button>
              <button
                type="button"
                :disabled="(element.items?.length ?? 0) <= 1"
                @click="element.items?.pop(); changed(element)"
              >
                {{ t('documents.editor.removeItem') }}
              </button>
            </template>
            <label
              v-if="element.kind === 'image'"
              class="document-field"
            >
              {{ t('documents.editor.imageURL') }}
              <input
                v-model="element.image_url"
                type="text"
                maxlength="2048"
                @input="changed(element)"
              >
            </label>
            <template v-if="element.kind === 'image'">
              <label class="document-field">
                {{ t('slides.uploadImage') }}
                <input type="file" accept="image/png,image/jpeg" @change="upload">
              </label>
              <p class="document-muted">{{ t('slides.uploadHint') }}</p>
              <p v-if="uploading" role="status">{{ t('slides.uploadingImage') }}</p>
              <p v-if="uploadError" role="alert">{{ t(uploadError) }}</p>
            </template>
            <div class="document-actions">
              <button
                type="button"
                :disabled="elementIndex === 0"
                @click="moveElement(-1)"
              >
                {{ t('documents.editor.up') }}
              </button>
              <button
                type="button"
                :disabled="elementIndex === elements.length - 1"
                @click="moveElement(1)"
              >
                {{ t('documents.editor.down') }}
              </button>
              <button
                type="button"
                :disabled="element.kind === 'heading' || elements.length <= 2"
                @click="removeElement"
              >
                {{ t('documents.editor.remove') }}
              </button>
            </div>
          </template>
          <select
            v-model="newKind"
            :aria-label="t('documents.editor.blockKind')"
          >
            <option
              v-for="kind in kinds"
              :key="kind"
              :value="kind"
            >
              {{ t('documents.blocks.' + kind) }}
            </option>
          </select>
          <button
            type="button"
            :disabled="elements.length >= 8"
            @click="addElement"
          >
            {{ t('documents.editor.add') }}
          </button>
        </fieldset>
      </aside>
    </div>

    <section class="document-panel document-fields">
      <h2>{{ t('slides.aiTitle') }}</h2>
      <p role="status">
        {{ t('slides.selectedCount', { count: anchors.length }) }}
      </p>
      <p
        v-if="dirty"
        class="document-muted"
      >
        {{ t('slides.saveBeforeAI') }}
      </p>
      <form
        class="document-fields"
        @submit.prevent="propose"
      >
        <label class="document-field">
          {{ t('slides.aiInstruction') }}
          <textarea
            v-model="command"
            rows="3"
            maxlength="1000"
            required
            :disabled="pending"
          />
        </label>
        <div class="document-actions">
          <button
            type="submit"
            :disabled="pending || dirty || !anchors.length || !command.trim()"
          >
            {{ t('slides.propose') }}
          </button>
          <button
            type="button"
            :disabled="pending || !anchors.length"
            @click="selection.clear"
          >
            {{ t('documents.editor.clearSelection', { count: anchors.length }) }}
          </button>
        </div>
      </form>
      <div
        v-if="proposal"
        class="slide-diff"
      >
        <h3>{{ t('slides.reviewChanges') }}</h3>
        <article
          v-for="(change, index) in proposal.changes"
          :key="index"
        >
          <h4>{{ t('slides.pageNumber', { number: changePageNumber(change.block_id) }) }} · {{ t(change.field === 'notes' ? 'slides.notes' : 'slides.elementText') }}</h4>
          <p>{{ t('slides.before') }}</p><del>{{ change.before }}</del>
          <p>{{ t('slides.after') }}</p><ins>{{ change.after }}</ins>
        </article>
        <div class="document-actions">
          <button
            type="button"
            class="document-primary"
            :disabled="pending || dirty || proposal.base_revision !== document.revision"
            @click="emit('accept')"
          >
            {{ t('slides.accept') }}
          </button>
          <button
            type="button"
            :disabled="pending"
            @click="emit('reject')"
          >
            {{ t('slides.reject') }}
          </button>
        </div>
      </div>
    </section>

    <section
      v-if="history.length"
      class="document-panel document-fields"
    >
      <label class="document-field">
        {{ t('documents.editor.chooseRevision') }}
        <select
          :value="previous?.revision ?? ''"
          :disabled="pending"
          @change="emit('viewRevision', Number(($event.target as HTMLSelectElement).value))"
        >
          <option
            value=""
            disabled
          >{{ t('documents.editor.chooseRevision') }}</option>
          <option
            v-for="item in history"
            :key="item.revision"
            :value="item.revision"
          >{{ t('documents.editor.revisionOption', { number: item.revision, title: item.title }) }}</option>
        </select>
      </label>
      <template v-if="previous && originalPage">
        <label class="document-field">
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
        <button
          type="button"
          :disabled="pending || dirty || previous.revision === document.revision"
          @click="emit('change', { action: 'restore', target_revision: previous.revision })"
        >
          {{ t('documents.editor.restore') }}
        </button>
      </template>
    </section>

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
