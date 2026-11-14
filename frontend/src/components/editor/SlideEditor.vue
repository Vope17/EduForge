<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { move } from '../../lib/i18n.ts'
import { plainText } from '../../lib/markup.ts'
import { TITLE_BOX, syncSlideFields, backgroundStyle, boxStyle, decorations, defaultLayout, rectStyle, slideKind, surfaceStyle, textStyle, themeOf, cqw } from '../../lib/slideTheme.ts'
import { useMarquee } from '../../lib/marquee.ts'
import FigurePicker from './FigurePicker.vue'
import SlideText from './SlideText.vue'
import SlideView from './SlideView.vue'
import MathView from './MathView.vue'
import Icon from '../Icon.vue'
import type { Element, Image, Section, Style } from '../../api/types.ts'

// PowerPoint-style editing: each slide is a set of positioned text and image
// boxes (fractions of the slide) over the template's fixed decorations, with
// the speaker script underneath.
const props = withDefaults(defineProps<{
  slides: Section[]
  images: Image[]
  url: (path: string) => string
  theme?: Style
  /** Template background URL. */
  background?: (path: string) => string
  /** AI editing: slide:<i>, el:<i>:<id>, notes:<i>. */
  selection?: string[]
  selectable?: boolean
}>(), { theme: () => ({}), selection: () => [], selectable: false })
const emit = defineEmits<{ 'update:selection': [keys: string[]] }>()
// Every slide has a layout once the editor is open (see the loop below).
const boxes = (s: Section) => s.layout!
const has = (key: string) => props.selection.includes(key)
function toggleKey(key: string) { emit('update:selection', has(key) ? props.selection.filter(k => k !== key) : [...props.selection, key]) }
const boxSelected = (el: Element) => props.selectable && (has(`slide:${current.value}`) || has(`el:${current.value}:${el.id}`))
const notesSelected = computed(() => props.selectable && (has(`slide:${current.value}`) || has(`notes:${current.value}`)))
const { t } = useWorkspace()
// The figure picker: replacing an image box's picture, or adding a new one.
const current = ref(0), selected = ref(''), editing = ref(''), picking = ref<{ el: Element } | { add: true; el?: undefined } | null>(null)
const canvas = ref<HTMLElement | null>(null)
// Box selection: over the thumbnails it picks whole slides, over the slide it picks text boxes.
const rail = ref<HTMLElement | null>(null), stage = ref<HTMLElement | null>(null)
const railBox = useMarquee({ container: rail, items: '.slide-thumb-row', enabled: () => props.selectable, current: () => props.selection, commit: keys => emit('update:selection', keys), ignore: '.slide-rail-tools' })
const stageBox = useMarquee({ container: stage, items: '.slide-box[data-kind="text"]', enabled: () => props.selectable, current: () => props.selection, commit: keys => emit('update:selection', keys), ignore: '.slide-box' })
const slide = computed(() => props.slides[current.value])
// Undo can remove slides: stay on one that exists.
watch(() => props.slides.length, n => { if (current.value >= n) select(Math.max(0, n - 1)) })
const th = computed(() => themeOf(props.theme))
const kind = computed(() => slideKind(slide.value))
const ratioNumber = computed(() => { const [w = 16, h = 9] = th.value.ratio.split('/').map(Number); return w / h })
const captions = computed(() => Object.fromEntries(props.images.map(img => [img.path, img.caption || ''])))
let uid = 0
const newID = (k: string) => `${k}-${Date.now().toString(36)}-${uid++}`

// Slides made before box layouts existed get the default layout.
for (const s of props.slides) {
  if (!Array.isArray(s.layout) || !s.layout.length) s.layout = defaultLayout(s, plainText)
}

// Keep the section's own fields in step with its boxes for lists and exports.
function syncSlide(s: Section | undefined = slide.value) { if (s) syncSlideFields(s) }

onMounted(() => document.addEventListener('keydown', onKey))
onUnmounted(() => document.removeEventListener('keydown', onKey))
function onKey(event: KeyboardEvent) {
  if (editing.value || !selected.value || picking.value) return
  const active = document.activeElement as HTMLElement | null
  if (['INPUT', 'TEXTAREA', 'SELECT'].includes(active?.tagName ?? '') || active?.isContentEditable) return
  if (event.key === 'Delete' || event.key === 'Backspace') { event.preventDefault(); removeSelected() }
}

// Drag to move, drag the corner handle to resize.
function startDrag(event: PointerEvent, el: Element, mode: 'move' | 'resize') {
  if (editing.value === el.id) return
  // Shift/Ctrl/⌘-click picks text boxes for AI editing instead of dragging.
  if (props.selectable && mode === 'move' && el.kind === 'text' && (event.shiftKey || event.ctrlKey || event.metaKey)) {
    event.preventDefault()
    toggleKey(`el:${current.value}:${el.id}`)
    return
  }
  event.preventDefault()
  selected.value = el.id
  if (!canvas.value) return
  const box = canvas.value.getBoundingClientRect()
  const start = { px: event.clientX, py: event.clientY, x: el.x, y: el.y, w: el.w, h: el.h }
  const moveTo = (e: PointerEvent) => {
    const dx = (e.clientX - start.px) / box.width, dy = (e.clientY - start.py) / box.height
    if (mode === 'move') {
      el.x = Math.min(Math.max(start.x + dx, 0), 1 - el.w)
      el.y = Math.min(Math.max(start.y + dy, 0), 1 - el.h)
    } else {
      el.w = Math.min(Math.max(start.w + dx, 0.04), 1 - el.x)
      el.h = Math.min(Math.max(start.h + dy, 0.04), 1 - el.y)
    }
  }
  const stop = () => { window.removeEventListener('pointermove', moveTo); window.removeEventListener('pointerup', stop); syncSlide() }
  window.addEventListener('pointermove', moveTo)
  window.addEventListener('pointerup', stop)
}
async function edit(el: Element) {
  if (el.kind === 'image') { picking.value = { el }; return }
  if (el.kind === 'formula') { editFormula(el); return }
  editing.value = el.id
  await nextTick()
  const node = canvas.value?.querySelector<HTMLElement>(`[data-edit="${el.id}"]`)
  if (node) { node.innerText = el.text || ''; node.focus(); document.getSelection()?.selectAllChildren(node); document.getSelection()?.collapseToEnd() }
}
function finishEdit(el: Element, event: FocusEvent) { el.text = (event.target as HTMLElement).innerText.replace(/\n$/, ''); editing.value = ''; syncSlide() }

const selectedEl = computed(() => slide.value?.layout?.find(el => el.id === selected.value))
function addText() {
  const el: Element = { id: newID('text'), kind: 'text', role: 'body', x: 0.3, y: 0.4, w: 0.4, h: 0.15, text: t('newTextBox'), size: 20 }
  boxes(slide.value!).push(el); selected.value = el.id; syncSlide(); edit(el)
}
function addImage() { picking.value = { add: true } }
// A formula box holds TeX, drawn as a formula on the slide and in exports.
function editFormula(el?: Element) {
  const tex = window.prompt(t('editFormula'), el?.tex || '')
  if (tex === null) return
  if (!el) {
    if (!tex.trim()) return
    el = { id: newID('formula'), kind: 'formula', role: 'formula', x: 0.1, y: 0.25, w: 0.8, h: 0.16, tex: tex.trim() }
    boxes(slide.value!).push(el); selected.value = el.id
  } else el.tex = tex.trim()
  syncSlide()
}
function pick(path: string) {
  if (!picking.value) return
  if (picking.value.el) picking.value.el.path = path
  else {
    const el: Element = { id: newID('image'), kind: 'image', role: 'figure', x: 0.2, y: 0.22, w: 0.6, h: 0.6, path }
    boxes(slide.value!).push(el); selected.value = el.id
  }
  picking.value = null; syncSlide()
}
function removeSelected() {
  const s = slide.value!
  s.layout = boxes(s).filter(el => el.id !== selected.value)
  selected.value = ''; syncSlide()
}
function resize(step: number) { if (selectedEl.value) selectedEl.value.size = Math.min(Math.max((selectedEl.value.size || 18) + step, 8), 96) }
function setAlign(align: string) { if (selectedEl.value) selectedEl.value.align = align === 'left' ? '' : align }
const currentAlign = computed(() => selectedEl.value?.align || (selectedEl.value?.role === 'caption' ? 'center' : 'left'))
function layer(front: boolean) {
  const list = boxes(slide.value!), el = selectedEl.value
  if (!el) return
  list.splice(list.indexOf(el), 1)
  if (front) list.push(el)
  else list.unshift(el)
}
function select(i: number) { current.value = i; selected.value = ''; editing.value = '' }
function addSlide() {
  props.slides.splice(current.value + 1, 0, { title: t('newSlideTitle'), body: '', notes: '', citations: [], figures: [], layout: [
    { id: newID('title'), kind: 'text', role: 'title', ...TITLE_BOX, text: t('newSlideTitle'), size: 28, bold: true },
  ] })
  select(current.value + 1)
}
function duplicateSlide() {
  props.slides.splice(current.value + 1, 0, JSON.parse(JSON.stringify(slide.value)))
  select(current.value + 1)
}
function removeSlide() {
  if (props.slides.length <= 1 || !confirm(t('deleteSlideConfirm'))) return
  props.slides.splice(current.value, 1)
  select(Math.min(current.value, props.slides.length - 1))
}
function moveSlide(offset: number) {
  const next = current.value + offset
  if (next < 0 || next >= props.slides.length) return
  props.slides.splice(0, props.slides.length, ...move(props.slides, current.value, offset))
  select(next)
}

// About 260 Han characters or 130 English words per spoken minute.
const notesSeconds = computed(() => {
  const text = slide.value?.notes || ''
  const han = (text.match(/\p{Script=Han}/gu) || []).length
  const words = (text.replace(/\p{Script=Han}/gu, ' ').match(/[A-Za-z0-9]+/g) || []).length
  return Math.round((han / 260 + words / 130) * 60)
})
</script>

<template>
  <div class="slide-editor">
    <aside ref="rail" class="slide-rail" :aria-label="t('tabSlides')">
      <div v-for="(s, i) in slides" :key="i" class="slide-thumb-row" :data-select-key="`slide:${i}`">
        <button class="slide-thumb" :class="{ active: i === current, 'ai-selected': selectable && has(`slide:${i}`) }" @click="select(i)">
          <span class="slide-thumb-no">{{ i + 1 }}</span>
          <SlideView class="thumb" :slide="s" :theme="th" :url="url" :page="i + 1" :background="background" />
        </button>
        <label v-if="selectable" class="ai-select thumb-select" :title="t('selectSlideForAI')">
          <input type="checkbox" :checked="has(`slide:${i}`)" :aria-label="t('selectSlideForAI') + ' · ' + (i + 1)" @change="toggleKey(`slide:${i}`)">
        </label>
      </div>
      <div v-if="railBox.active" class="marquee" :style="{ left: railBox.x + 'px', top: railBox.y + 'px', width: railBox.w + 'px', height: railBox.h + 'px' }"></div>
      <div class="slide-rail-tools">
        <button class="secondary small" @click="addSlide"><Icon name="plus" /> {{ t('addSlide') }}</button>
      </div>
    </aside>

    <div class="slide-main">
      <div class="slide-toolbar" role="toolbar">
        <button class="tool wide" @click="addText"><Icon name="plus" /> {{ t('textBox') }}</button>
        <button class="tool wide" @click="addImage"><Icon name="plus" /> {{ t('imageBox') }}</button>
        <button class="tool wide" @click="editFormula()">∑ {{ t('formulaBox') }}</button>
        <span class="tool-sep"></span>
        <template v-if="selectedEl">
          <template v-if="selectedEl.kind === 'text'">
            <button class="tool" :title="t('smaller')" @click="resize(-2)">A−</button>
            <span class="tool-value">{{ selectedEl.size || 18 }}</span>
            <button class="tool" :title="t('larger')" @click="resize(2)">A+</button>
            <button class="tool" :class="{ on: selectedEl.bold }" :title="t('bold')" @click="selectedEl.bold = !selectedEl.bold"><b>B</b></button>
            <button class="tool" :class="{ on: currentAlign === 'left' }" :title="t('alignLeft')" @click="setAlign('left')">⇤</button>
            <button class="tool" :class="{ on: currentAlign === 'center' }" :title="t('alignCenter')" @click="setAlign('center')">↔</button>
          </template>
          <button v-else-if="selectedEl.kind === 'formula'" class="tool wide" @click="editFormula(selectedEl)">{{ t('editFormula') }}</button>
          <button v-else class="tool wide" @click="picking = { el: selectedEl }">{{ t('replaceImage') }}</button>
          <button class="tool wide" @click="layer(true)">{{ t('bringFront') }}</button>
          <button class="tool wide" @click="layer(false)">{{ t('sendBack') }}</button>
          <button class="tool wide danger" @click="removeSelected">{{ t('delete') }}</button>
        </template>
        <span v-else class="caption">{{ t('slideEditHint') }}</span>
        <span class="grow"></span>
        <button class="icon-button" :title="t('moveUp')" @click="moveSlide(-1)">↑</button>
        <button class="icon-button" :title="t('moveDown')" @click="moveSlide(1)">↓</button>
        <button class="text-button" @click="duplicateSlide">{{ t('duplicate') }}</button>
        <button class="text-button danger" @click="removeSlide">{{ t('deleteSlide') }}</button>
      </div>

      <div ref="stage" class="slide-stage" @pointerdown.self="selected = ''">
        <div v-if="stageBox.active" class="marquee" :style="{ left: stageBox.x + 'px', top: stageBox.y + 'px', width: stageBox.w + 'px', height: stageBox.h + 'px' }"></div>
        <div ref="canvas" class="slide-surface" :style="{ ...surfaceStyle(th), ...backgroundStyle(th, kind, background), '--ratio': ratioNumber }" @pointerdown.self="selected = ''">
          <div v-for="(d, i) in decorations(th, kind)" :key="'d' + i" class="slide-decor" :style="rectStyle(d)" @pointerdown="selected = ''"></div>
          <div v-for="el in slide.layout" :key="el.id" class="slide-box editable" :data-kind="el.kind" :data-select-key="`el:${current}:${el.id}`" :class="{ selected: selected === el.id, editing: editing === el.id, 'ai-selected': boxSelected(el) }"
            :style="boxStyle(el)" @pointerdown="startDrag($event, el, 'move')" @dblclick="edit(el)">
            <img v-if="el.kind === 'image'" :src="url(el.path ?? '')" :alt="captions[el.path ?? '']" draggable="false">
            <MathView v-else-if="el.kind === 'formula'" :tex="el.tex" :color="'#' + th.fg" :font-size="cqw(24, th)" />
            <div v-else-if="editing === el.id" class="slide-text" :data-edit="el.id" data-native-undo contenteditable="true" :style="textStyle(el, th, kind)"
              @blur="finishEdit(el, $event)" @keydown.esc.prevent="($event.target as HTMLElement).blur()" @pointerdown.stop></div>
            <SlideText v-else :el="el" :theme="th" :kind="kind" />
            <span v-if="selected === el.id && editing !== el.id" class="slide-handle" @pointerdown.stop="startDrag($event, el, 'resize')"></span>
          </div>
          <span v-if="kind === 'content' && !th.template" class="slide-page" :style="{ fontSize: cqw(11, th) }">{{ current + 1 }}</span>
        </div>
      </div>

      <section class="slide-notes" :class="{ 'ai-selected': notesSelected }" :aria-label="t('speakerNotes')">
        <div class="slide-notes-head">
          <strong>{{ t('speakerNotes') }}</strong>
          <span class="caption">{{ t('slideOf').replace('{n}', String(current + 1)).replace('{total}', String(slides.length)) }}</span>
          <span class="grow"></span>
          <span class="caption">{{ t('speakingTime').replace('{s}', String(notesSeconds)) }}</span>
          <label v-if="selectable" class="check ai-notes-select"><input type="checkbox" :checked="notesSelected" :disabled="has(`slide:${current}`)" @change="toggleKey(`notes:${current}`)">{{ t('selectNotesForAI') }}</label>
        </div>
        <textarea v-model="slide.notes" rows="6" :placeholder="t('notesPlaceholder')"></textarea>
      </section>
    </div>
    <FigurePicker v-if="picking" :images="images" :url="url" @pick="pick" @close="picking = null" />
  </div>
</template>
