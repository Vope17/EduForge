<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMarquee } from '../../lib/marquee.ts'
import { useWorkspace } from '../../lib/workspace.ts'
import { move } from '../../lib/i18n.ts'
import { formulaChip, roman, sectionFigures, sectionTitle } from '../../lib/markup.ts'
import DocBody from './DocBody.vue'
import FigurePicker from './FigurePicker.vue'
import Icon from '../Icon.vue'
import type { Image, Section, Style } from '../../api/types.ts'

// Word-style editing of a reader: one page, editable headings and text,
// each figure under the paragraph that mentions it (as in the exports).
const props = withDefaults(defineProps<{
  sections: Section[]
  images: Image[]
  url: (path: string) => string
  references?: string[]
  title?: string
  language?: string
  /** A Word template's fonts and colours. */
  look?: Style
  /** AI editing: selected sec:<i> keys. */
  selection?: string[]
  selectable?: boolean
}>(), { references: () => [], title: '', language: 'zh-TW', look: () => ({}), selection: () => [], selectable: false })
const emit = defineEmits<{ 'update:selection': [keys: string[]] }>()
const isSelected = (i: number) => props.selection.includes(`sec:${i}`)
function toggleSelect(i: number) {
  const key = `sec:${i}`
  emit('update:selection', isSelected(i) ? props.selection.filter(k => k !== key) : [...props.selection, key])
}
const { t } = useWorkspace()
// Dragging over empty space selects the sections the box touches.
const root = ref<HTMLElement | null>(null)
const marquee = useMarquee({ container: root, items: '.doc-section', enabled: () => props.selectable, current: () => props.selection, commit: keys => emit('update:selection', keys), ignore: '.doc-toolbar, .doc-section-tools', listenOn: () => root.value?.closest<HTMLElement>('.editor-main') ?? root.value })
// The figure being chosen: index null appends one.
const picking = ref<{ section: number; index: number | null } | null>(null)
const hex = (v: unknown) => typeof v === 'string' && /^[0-9a-f]{6}$/i.test(v) ? '#' + v : null
const pageStyle = computed((): Record<string, string> => ({
  ...(props.look.doc_font ? { '--doc-font': `'${props.look.doc_font}', 'Noto Sans CJK TC', sans-serif` } : {}),
  ...(props.look.doc_heading_font ? { '--doc-heading-font': `'${props.look.doc_heading_font}', 'Noto Sans CJK TC', sans-serif` } : {}),
  ...(hex(props.look.doc_heading_color) ? { '--doc-heading-color': hex(props.look.doc_heading_color)! } : {}),
  ...(hex(props.look.doc_text_color) ? { '--doc-text-color': hex(props.look.doc_text_color)! } : {}),
}))
const captions = computed(() => Object.fromEntries(props.images.map(img => [img.path, img.caption || ''])))

// Older outputs choose figures automatically; make the choice explicit so it can be edited.
sectionFigures(props.sections, props.images).forEach((paths, i) => { props.sections[i]!.figures = paths })
// Sections are numbered I., II., … automatically; drop numbers typed into titles.
for (const sec of props.sections) sec.title = sectionTitle(sec.title)

function command(name: string, value?: string) { document.execCommand(name, false, value) }
// Inserts a TeX formula at the caret of the text being edited.
function insertFormula() {
  const target = document.activeElement
  if (!(target instanceof HTMLElement) || !target.classList.contains('rich-text')) { alert(t('formulaPlaceCaret')); return }
  const selection = document.getSelection()
  const range = selection?.rangeCount ? selection.getRangeAt(0).cloneRange() : null
  const tex = window.prompt(t('insertFormulaPrompt'), '')
  if (!tex?.trim()) return
  target.focus()
  if (selection && range) { selection.removeAllRanges(); selection.addRange(range) }
  command('insertHTML', formulaChip(tex.trim()) + '&nbsp;')
}
function toggleSubheading() {
  const block = String(document.queryCommandValue('formatBlock') || '').toLowerCase()
  command('formatBlock', block === 'h3' ? 'div' : 'h3')
}
function addSection(after: number) {
  props.sections.splice(after + 1, 0, { title: t('newSectionTitle'), body: '', notes: '', citations: [], figures: [] })
}
function removeSection(i: number) { if (confirm(t('deleteSectionConfirm'))) props.sections.splice(i, 1) }
function moveSection(i: number, offset: number) { props.sections.splice(0, props.sections.length, ...move(props.sections, i, offset)) }
function pick(path: string) {
  if (!picking.value) return
  const { section, index } = picking.value
  const figures = [...(props.sections[section]!.figures || [])]
  if (index === null) figures.push(path)
  else figures.splice(index, 1, path)
  props.sections[section]!.figures = figures
  picking.value = null
}
</script>

<template>
  <div ref="root" class="doc-editor" :class="{ marqueeing: marquee.active }">
    <div class="doc-toolbar" role="toolbar" :aria-label="t('formatting')">
      <button class="tool" :title="t('bold')" @mousedown.prevent="command('bold')"><b>B</b></button>
      <button class="tool" :title="t('subheading')" @mousedown.prevent="toggleSubheading">H</button>
      <button class="tool" :title="t('bulletList')" @mousedown.prevent="command('insertUnorderedList')">• ≡</button>
      <button class="tool" :title="t('insertFormula')" @mousedown.prevent="insertFormula">∑</button>
      <span class="tool-sep"></span>
      <button class="tool wide" @mousedown.prevent="addSection(sections.length - 1)"><Icon name="plus" /> {{ t('addSection') }}</button>
    </div>

    <article class="doc-page" :style="pageStyle">
      <header v-if="title" class="doc-title">
        <p class="doc-kicker">{{ language === 'zh-TW' ? '講義' : 'Handout' }}</p>
        <h1>{{ title }}</h1>
      </header>
      <section v-for="(sec, i) in sections" :key="i" class="doc-section" :data-select-key="`sec:${i}`" :class="{ 'ai-selected': selectable && isSelected(i) }">
        <label v-if="selectable" class="ai-select doc-select" :title="t('selectForAI')">
          <input type="checkbox" :checked="isSelected(i)" :aria-label="t('selectForAI') + ' · ' + roman(i + 1)" @change="toggleSelect(i)">
        </label>
        <div class="doc-section-tools">
          <button class="icon-button" :title="t('moveUp')" @click="moveSection(i, -1)">↑</button>
          <button class="icon-button" :title="t('moveDown')" @click="moveSection(i, 1)">↓</button>
          <button class="icon-button" :title="t('addSectionBelow')" @click="addSection(i)">＋</button>
          <button class="icon-button danger" :title="t('delete')" @click="removeSection(i)">×</button>
        </div>
        <div class="doc-heading-row">
          <span class="doc-number">{{ roman(i + 1) }}.</span>
          <input v-model="sec.title" class="doc-heading" :aria-label="t('title')">
        </div>
        <DocBody :section="sec" :captions="captions" :url="url" :placeholder="t('bodyPlaceholder')" @replace="j => picking = { section: i, index: j }" />
        <button class="doc-insert" @click="picking = { section: i, index: null }"><Icon name="plus" /> {{ t('insertImage') }}</button>
      </section>
      <section v-if="references.length" class="doc-references">
        <h2>{{ t('referencesTitle') }}</h2>
        <ol><li v-for="name in references" :key="name">{{ name }}</li></ol>
      </section>
    </article>
    <div v-if="marquee.active" class="marquee" :style="{ left: marquee.x + 'px', top: marquee.y + 'px', width: marquee.w + 'px', height: marquee.h + 'px' }"></div>
    <FigurePicker v-if="picking" :images="images" :url="url" @pick="pick" @close="picking = null" />
  </div>
</template>
