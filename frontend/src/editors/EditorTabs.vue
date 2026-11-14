<script setup lang="ts">
import AppIcon from '../components/AppIcon.vue'
import type { IconName } from '../components/icons'

export interface EditorTab { key: string; label: string; icon: IconName }
defineProps<{ tabs: EditorTab[] }>()
const model = defineModel<string>({ required: true })
</script>

<template>
  <div
    class="subnav"
    role="tablist"
  >
    <button
      v-for="tab in tabs"
      :key="tab.key"
      type="button"
      role="tab"
      class="plain"
      :class="{ active: model === tab.key }"
      :aria-selected="model === tab.key"
      @click="model = tab.key"
    >
      <AppIcon :name="tab.icon" />{{ tab.label }}
    </button>
  </div>
</template>
