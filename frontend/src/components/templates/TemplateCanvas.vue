<script setup lang="ts">
import { ref } from 'vue'

/** A draggable area: box is [x, y, w, h] as fractions of the slide. */
export interface CanvasBox {
  key: string
  label: string
  box: number[]
  role: string
  /** Sample text; several lines for the body area. */
  sample: string | string[]
}

// A template layout preview whose title/content areas can be dragged and
// resized. Boxes are fractions [x, y, w, h] of the slide.
const props = withDefaults(defineProps<{
  background?: string
  ratio?: string
  /** Slide width in points, for font sizes. */
  points?: number
  boxes: CanvasBox[]
  colors?: { bg?: string; title?: string; text?: string; accent?: string }
  /** CSS font-family lists. */
  fonts?: { heading?: string; body?: string }
  /** Keys highlighted for AI editing. */
  selected?: string[]
  label?: string
}>(), { background: '', ratio: '16 / 9', points: 960, colors: () => ({}), fonts: () => ({}), selected: () => [], label: '' })
const emit = defineEmits<{ update: [key: string, box: number[]]; pick: [key: string] }>()
const canvas = ref<HTMLElement | null>(null)
const pct = (v: number) => `${(v * 100).toFixed(3)}%`
const cqw = (pt: number) => `${(pt / props.points * 100).toFixed(3)}cqw`
const sizes: Record<string, number> = { title: 30, cover: 40, subtitle: 20, body: 18 }

function drag(event: PointerEvent, item: CanvasBox, mode: 'move' | 'resize') {
  if (event.shiftKey || event.ctrlKey || event.metaKey) { emit('pick', item.key); return }
  event.preventDefault()
  if (!canvas.value) return
  const rect = canvas.value.getBoundingClientRect()
  const [x = 0, y = 0, w = 0, h = 0] = item.box
  const start = { px: event.clientX, py: event.clientY }
  const move = (e: PointerEvent) => {
    const dx = (e.clientX - start.px) / rect.width, dy = (e.clientY - start.py) / rect.height
    const r = (v: number) => Math.round(v * 1000) / 1000
    const next = mode === 'move'
      ? [r(Math.min(Math.max(x + dx, 0), 1 - w)), r(Math.min(Math.max(y + dy, 0), 1 - h)), w, h]
      : [x, y, r(Math.min(Math.max(w + dx, 0.05), 1 - x)), r(Math.min(Math.max(h + dy, 0.05), 1 - y))]
    emit('update', item.key, next)
  }
  const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', stop)
}
</script>

<template>
  <figure class="template-canvas">
    <figcaption v-if="label">{{ label }}</figcaption>
    <div ref="canvas" class="slide-surface" :style="{ aspectRatio: ratio, backgroundColor: '#' + (colors.bg || 'FFFFFF'), backgroundImage: background ? `url(${background})` : 'none', backgroundSize: '100% 100%' }">
      <div v-for="item in boxes" :key="item.key" class="template-box" :class="[item.role, { 'ai-selected': selected.includes(item.key) }]"
        :style="{ left: pct(item.box[0]), top: pct(item.box[1]), width: pct(item.box[2]), height: pct(item.box[3]) }"
        @pointerdown="drag($event, item, 'move')">
        <span class="template-box-label">{{ item.label }}</span>
        <div class="template-box-text" :style="{
          fontFamily: item.role === 'body' ? fonts.body : fonts.heading,
          fontSize: cqw(sizes[item.role] || 18),
          fontWeight: item.role === 'body' || item.role === 'subtitle' ? 400 : 700,
          color: '#' + (item.role === 'body' || item.role === 'subtitle' ? colors.text : colors.title),
        }">
          <template v-if="item.role === 'body'">
            <div v-for="line in item.sample" :key="line" class="slide-bullet"><span class="slide-dot" :style="{ color: '#' + colors.accent }">•</span>{{ line }}</div>
          </template>
          <template v-else>{{ item.sample }}</template>
        </div>
        <span class="slide-handle" @pointerdown.stop="drag($event, item, 'resize')"></span>
      </div>
    </div>
  </figure>
</template>
