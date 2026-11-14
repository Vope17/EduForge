<script setup lang="ts">
import { computed } from 'vue'
import { backgroundStyle, boxStyle, decorations, rectStyle, slideKind, surfaceStyle, cqw, type Theme } from '../../lib/slideTheme.ts'
import type { Section } from '../../api/types.ts'
import SlideText from './SlideText.vue'
import MathView from './MathView.vue'
// A read-only slide: thumbnails and template previews.
const props = withDefaults(defineProps<{
  slide: Partial<Section>
  theme: Theme
  url?: (path: string) => string
  page?: number
  /** Template background URL. */
  background?: (path: string) => string
}>(), { page: 0 })
const kind = computed(() => slideKind(props.slide))
</script>

<template>
  <div class="slide-surface" :style="{ ...surfaceStyle(theme), ...backgroundStyle(theme, kind, background) }">
    <div v-for="(d, i) in decorations(theme, kind)" :key="'d' + i" class="slide-decor" :style="rectStyle(d)"></div>
    <div v-for="el in slide.layout" :key="el.id" class="slide-box" :style="boxStyle(el)">
      <template v-if="el.kind === 'image'">
        <img v-if="el.path && url" :src="url(el.path)" alt="">
        <div v-else class="slide-placeholder"></div>
      </template>
      <MathView v-else-if="el.kind === 'formula'" :tex="el.tex" :color="'#' + theme.fg" :font-size="cqw(24, theme)" />
      <SlideText v-else :el="el" :theme="theme" :kind="kind" />
    </div>
    <span v-if="page && kind === 'content' && !theme.template" class="slide-page" :style="{ fontSize: cqw(11, theme) }">{{ page }}</span>
  </div>
</template>
