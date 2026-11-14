<script setup lang="ts">
import { ref, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { figureBlocks, joinBlocks } from '../../lib/markup.ts'
import RichText from './RichText.vue'
import type { Section } from '../../api/types.ts'
import type { FigureBlock } from '../../lib/markup.ts'

// A reader section's body with its figures in place: each figure right under
// the paragraph that mentions it, as in the exports. The body is split into
// editable blocks around the figures; typing only rejoins the blocks, so the
// caret stays put. The blocks are split again when the figures change or the
// body changes from outside (undo, AI edits).
const props = withDefaults(defineProps<{
  section: Section
  /** Caption per figure path. */
  captions: Record<string, string>
  url: (path: string) => string
  placeholder?: string
}>(), { placeholder: '' })
const emit = defineEmits<{ replace: [index: number] }>()
const { t } = useWorkspace()
const blocks = ref<FigureBlock[]>([])
let composed: string | null = null

function split() {
  composed = props.section.body || ''
  blocks.value = figureBlocks(composed, props.section.figures || [], props.captions)
}
watch(() => props.section.body, body => { if (body !== composed) split() }, { immediate: true })
watch(() => (props.section.figures || []).join('\n'), split)

function edit(index: number, text: string) {
  blocks.value[index]!.text = text
  composed = joinBlocks(blocks.value)
  props.section.body = composed
}

// Figures shown together under one paragraph can swap places.
function neighbour(block: FigureBlock, j: number, offset: number) { return block.figures[j + offset] }
function swap(block: FigureBlock, j: number, offset: number) {
  const list = [...(props.section.figures || [])]
  const a = list.indexOf(block.figures[j]!), b = list.indexOf(block.figures[j + offset]!)
  ;[list[a], list[b]] = [list[b]!, list[a]!]
  props.section.figures = list
}
function remove(path: string) { props.section.figures = (props.section.figures || []).filter(p => p !== path) }
</script>

<template>
  <div class="doc-body">
    <template v-for="(block, i) in blocks" :key="i + ':' + block.figures.join('|')">
      <RichText :model-value="block.text" :placeholder="i === 0 ? placeholder : ''" @update:model-value="edit(i, $event)" />
      <figure v-for="(path, j) in block.figures" :key="path" class="doc-figure">
        <img :src="url(path)" :alt="captions[path]">
        <figcaption v-if="captions[path]">{{ captions[path] }}</figcaption>
        <div class="doc-figure-tools">
          <button v-if="neighbour(block, j, -1)" class="icon-button" :title="t('moveUp')" @click="swap(block, j, -1)">↑</button>
          <button v-if="neighbour(block, j, 1)" class="icon-button" :title="t('moveDown')" @click="swap(block, j, 1)">↓</button>
          <button class="text-button" @click="emit('replace', (section.figures || []).indexOf(path))">{{ t('replaceImage') }}</button>
          <button class="text-button danger" @click="remove(path)">{{ t('remove') }}</button>
        </div>
      </figure>
    </template>
  </div>
</template>
