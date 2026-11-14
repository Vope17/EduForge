<script setup lang="ts">
import { useWorkspace } from '../../lib/workspace.ts'
import type { Image } from '../../api/types.ts'
// Chooses one of the lesson's original figures.
defineProps<{ images: Image[]; url: (path: string) => string }>()
const emit = defineEmits<{ pick: [path: string]; close: [] }>()
const { t } = useWorkspace()
</script>

<template>
  <div class="picker-backdrop" @click.self="emit('close')">
    <section class="picker" role="dialog" aria-modal="true" :aria-label="t('insertImage')">
      <div class="section-heading">
        <h3>{{ t('insertImage') }}</h3>
        <button class="icon-button" :aria-label="t('close')" @click="emit('close')">×</button>
      </div>
      <p v-if="!images.length" class="empty-inline">{{ t('noFigures') }}</p>
      <div class="picker-grid">
        <button v-for="img in images" :key="img.path" class="picker-item" @click="emit('pick', img.path)">
          <img :src="url(img.path)" :alt="img.caption || ''" loading="lazy">
          <span>{{ img.caption || img.citation }}</span>
        </button>
      </div>
    </section>
  </div>
</template>
