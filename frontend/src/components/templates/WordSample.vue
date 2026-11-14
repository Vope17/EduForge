<script setup lang="ts">
import { computed } from 'vue'
import type { TemplateSpec, WordStyleSpec } from '../../api/types.ts'

// A live A4 sample page drawn from a Word template spec: margins, header and
// footer, and each paragraph style. Sizes use container units so the page
// scales with its column (210 mm wide).
const props = withDefaults(defineProps<{
  spec: TemplateSpec
  /** Role keys highlighted for AI editing. */
  selected?: string[]
}>(), { selected: () => [] })
const emit = defineEmits<{ pick: [role: string] }>()
const mm = (v: number) => `${(v / 210 * 100).toFixed(3)}cqw`
const pt = (v: number | undefined) => mm((v || 0) * 0.3528)
const family = (st: Partial<WordStyleSpec> | undefined) => [st?.font, st?.font_ea].filter(Boolean).map(f => `'${f}'`).concat(["'Noto Sans CJK TC'", 'sans-serif']).join(', ')
function look(role: string) {
  const st: Partial<WordStyleSpec> = props.spec.styles?.[role] || {}
  return {
    fontFamily: family(st), fontSize: pt(st.size || 11), fontWeight: st.bold ? 700 : 400,
    color: st.color ? '#' + st.color : '#000', marginTop: pt(st.before), marginBottom: pt(st.after),
  }
}
const margins = computed(() => props.spec.margins || { top: 25.4, bottom: 25.4, left: 25.4, right: 25.4 })
const blocks: { role: string; text: string; bullet?: boolean }[] = [
  { role: 'subtitle', text: '論文導讀 · Lesson reading guide' },
  { role: 'title', text: '論文題目 Lesson title' },
  { role: 'h1', text: 'I.  研究動機與問題' },
  { role: 'normal', text: '這裡是讀本內文，套用模板的內文樣式。The reader text uses the body style.' },
  { role: 'normal', text: '• 重點一：標題與內文樣式', bullet: true },
  { role: 'h2', text: '小標題 Subheading' },
  { role: 'normal', text: '論文原圖置中插入，下方加上圖說。' },
  { role: 'caption', text: '圖 1：原圖說 Figure caption' },
  { role: 'h1', text: 'II.  方法' },
]
</script>

<template>
  <div class="word-sample">
    <div class="word-sample-page" :style="{ padding: `${mm(margins.top)} ${mm(margins.right)} ${mm(margins.bottom)} ${mm(margins.left)}` }">
      <div class="word-sample-header" :class="{ 'ai-selected': selected.includes('header') }" :style="{ top: mm(Math.max(margins.top / 2 - 3, 3)), left: mm(margins.left), right: mm(margins.right) }" @click.shift="emit('pick', 'header')">{{ spec.header?.text }}</div>
      <p v-for="(b, i) in blocks" :key="i" class="word-sample-block" :class="{ 'ai-selected': selected.includes(b.role), bullet: b.bullet }" :style="look(b.role)" @click.shift="emit('pick', b.role)">{{ b.text }}</p>
      <div class="word-sample-footer" :class="{ 'ai-selected': selected.includes('footer') }" :style="{ bottom: mm(Math.max(margins.bottom / 2 - 3, 3)), left: mm(margins.left), right: mm(margins.right) }" @click.shift="emit('pick', 'footer')">{{ spec.footer?.text }}</div>
    </div>
  </div>
</template>
