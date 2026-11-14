<script setup lang="ts">
import { icons, type IconName } from '../lib/icons.ts'

export interface Tab {
  key: string
  label: string
  icon: IconName
  count?: number
  disabled?: boolean
}

withDefaults(defineProps<{ tabs: Tab[]; modelValue: string; context?: string }>(), { context: '' })
const emit = defineEmits<{ 'update:modelValue': [key: string] }>()
function keyNav(event: KeyboardEvent, tabs: Tab[], index: number) {
  const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
  if (!step) return
  event.preventDefault()
  for (let i = 1; i <= tabs.length; i++) {
    const next = tabs[(index + step * i + tabs.length * i) % tabs.length]!
    if (!next.disabled) {
      emit('update:modelValue', next.key)
      ;(event.currentTarget as HTMLElement).parentElement?.querySelector<HTMLElement>(`[data-key="${next.key}"]`)?.focus()
      return
    }
  }
}
</script>

<template>
  <div class="subnav-wrap">
    <nav class="subnav" role="tablist">
      <button
        v-for="(tab, index) in tabs" :key="tab.key" type="button" role="tab" :data-key="tab.key"
        :aria-selected="modelValue === tab.key" :tabindex="modelValue === tab.key ? 0 : -1"
        :class="{ active: modelValue === tab.key }" :disabled="tab.disabled" :title="tab.label"
        @click="emit('update:modelValue', tab.key)" @keydown="keyNav($event, tabs, index)"
      >
        <svg class="subnav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="icons[tab.icon]" /></svg>
        <span class="subnav-label">{{ tab.label }}</span>
        <span v-if="tab.count !== undefined && tab.count !== null" class="subnav-count">{{ tab.count }}</span>
      </button>
    </nav>
    <span v-if="context" class="subnav-context" :title="context">{{ context }}</span>
  </div>
</template>
