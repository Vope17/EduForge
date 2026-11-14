<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { htmlToMarkup, markupToHtml } from '../../lib/markup.ts'
import { hydrate } from '../../lib/math.ts'
import { useWorkspace } from '../../lib/workspace.ts'

// A contenteditable block that edits stored markup (**bold**, "- " bullets,
// **subheading** lines, $TeX$ formulas) as formatted text, like a word
// processor. Formulas show rendered; clicking one edits its TeX.
const props = withDefaults(defineProps<{ modelValue?: string; placeholder?: string }>(), { modelValue: '', placeholder: '' })
const emit = defineEmits<{ 'update:modelValue': [markup: string]; focus: [event: FocusEvent] }>()
const { api, t } = useWorkspace()
const el = ref<HTMLElement | null>(null)
let last: string | null = null

function render(value: string) {
  if (!el.value) return
  el.value.innerHTML = markupToHtml(value) || '<div><br></div>'; last = value; hydrate(api, el.value)
}
onMounted(() => render(props.modelValue))
// Re-render only for outside changes, so typing does not move the caret.
watch(() => props.modelValue, value => { if (el.value && value !== last) render(value) })
function input() {
  if (!el.value) return
  last = htmlToMarkup(el.value.innerHTML)
  emit('update:modelValue', last)
  hydrate(api, el.value) // formulas inserted from the toolbar
}
function paste(event: ClipboardEvent) {
  event.preventDefault()
  document.execCommand('insertText', false, event.clipboardData?.getData('text/plain') ?? '')
}
function editFormula(event: MouseEvent) {
  const chip = (event.target as Element | null)?.closest<HTMLElement>('[data-tex]')
  if (!chip || !el.value?.contains(chip)) return
  const tex = window.prompt(t('editFormula'), chip.dataset.tex)
  if (tex === null) return
  if (tex.trim()) {
    chip.dataset.tex = tex.trim()
    chip.textContent = tex.trim()
  } else chip.remove()
  input()
}
</script>

<template>
  <div ref="el" class="rich-text" contenteditable="true" spellcheck="false" :data-placeholder="placeholder"
    @input="input" @paste="paste" @click="editFormula" @focus="emit('focus', el)"></div>
</template>
