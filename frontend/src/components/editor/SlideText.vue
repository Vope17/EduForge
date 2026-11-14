<script setup lang="ts">
import { computed } from 'vue'
import { textLines, textStyle, type SlideKind, type Theme } from '../../lib/slideTheme.ts'
import type { Element } from '../../api/types.ts'
// A text box's content: bullet lines with a hanging accent bullet.
const props = withDefaults(defineProps<{ el: Element; theme: Theme; kind?: SlideKind }>(), { kind: 'content' })
const lines = computed(() => textLines(props.el.text))
</script>

<template>
  <div class="slide-text" :style="textStyle(el, theme, kind)">
    <template v-for="(line, i) in lines" :key="i">
      <div v-if="line.bullet" class="slide-bullet"><span class="slide-dot">•</span>{{ line.text }}</div>
      <div v-else class="slide-line">{{ line.text || ' ' }}</div>
    </template>
  </div>
</template>
