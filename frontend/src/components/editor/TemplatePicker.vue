<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { TITLE_BOX, templateStyle, themeOf } from '../../lib/slideTheme.ts'
import type { Element, Rec, Style, Template } from '../../api/types.ts'
import SlideView from './SlideView.vue'

// Choose the default look or an imported template: PowerPoint templates for
// slides (kind "pptx"), Word templates for readers (kind "docx").
const props = withDefaults(defineProps<{ modelValue?: string; kind?: 'pptx' | 'docx' }>(), { modelValue: '', kind: 'pptx' })
const emit = defineEmits<{ 'update:modelValue': [id: string] }>()
const { state, t, fileURL } = useWorkspace()

interface Option { id: string; name: string; style: Style; record: Rec<Template> | null }
const options = computed((): Option[] => [
  { id: '', name: t('defaultTemplate'), style: {}, record: null },
  ...(state.snapshot.template || []).filter(x => x.data.file && (x.data.kind || 'pptx') === props.kind)
    .map(x => ({ id: x.id, name: x.data.name, style: templateStyle(x.data), record: x })),
])
type Box = [number, number, number, number]
function sample(style: Style): { layout: Element[] } {
  const th = themeOf(style), scale = th.points / 960
  const [tx, ty, tw, th2] = (style.title_box as Box | undefined) || [TITLE_BOX.x, TITLE_BOX.y, TITLE_BOX.w, TITLE_BOX.h]
  const [bx, by, bw, bh] = (style.body_box as Box | undefined) || [0.06, 0.23, 0.88, 0.68]
  return {
    layout: [
      { id: 'title', kind: 'text', role: 'title', x: tx, y: ty, w: tw, h: th2, text: t('templateSampleTitle'), size: 28 * scale, bold: true, align: String(style.title_align || '') },
      { id: 'figure', kind: 'image', role: 'figure', x: bx, y: by, w: bw * 0.6, h: bh, path: '' },
      { id: 'points', kind: 'text', role: 'body', x: bx + bw * 0.65, y: by + bh * 0.05, w: bw * 0.35, h: bh * 0.9, text: t('templateSamplePoints'), size: 20 * scale },
    ],
  }
}
</script>

<template>
  <div class="template-picker" :class="{ word: kind === 'docx' }" role="radiogroup" :aria-label="t('template')">
    <button v-for="item in options" :key="item.id || 'default'" type="button" class="template-option" :class="{ selected: modelValue === item.id }"
      role="radio" :aria-checked="modelValue === item.id" @click="emit('update:modelValue', item.id)">
      <SlideView v-if="kind === 'pptx'" :slide="sample(item.style)" :theme="themeOf(item.style)" :background="item.record ? (path: string) => fileURL(item.record!, path) : undefined" :page="2" />
      <img v-else-if="item.record?.data.preview" class="word-preview" :src="fileURL(item.record, item.record.data.preview)" :alt="item.name">
      <div v-else class="word-preview placeholder"><strong>{{ t('defaultTemplate') }}</strong><span></span><span></span><span></span></div>
      <span class="template-option-name">{{ item.name }}</span>
    </button>
  </div>
</template>
