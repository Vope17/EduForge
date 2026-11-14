<script setup lang="ts">
import { ref, watch } from 'vue'
import { renderFormula } from '../../lib/math.ts'
import { useWorkspace } from '../../lib/workspace.ts'

// A slide's formula box: the TeX rendered as SVG at the slides' formula type
// size (fontSize), shrunk only to fit the box.
const props = withDefaults(defineProps<{ tex?: string; color?: string; fontSize?: string }>(), { tex: '', color: '', fontSize: '' })
const { api } = useWorkspace()
const svg = ref(''), error = ref('')
watch(() => props.tex, async tex => {
  svg.value = ''; error.value = ''
  if (!tex?.trim()) return
  const result = await renderFormula(api, tex, true)
  if (tex !== props.tex) return
  svg.value = result.svg || ''
  error.value = result.error || ''
}, { immediate: true })
</script>

<template>
  <div class="slide-formula" :class="{ 'math-error': error }" :style="{ color: color || undefined, fontSize: fontSize || undefined }" :title="error || tex">
    <span v-if="svg" class="slide-formula-svg" v-html="svg"></span>
    <code v-else>{{ tex }}</code>
  </div>
</template>
