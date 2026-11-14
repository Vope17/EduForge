<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import type { SettingsOverride } from '../../lib/settings.ts'

// Audience, level, lesson length and handout depth. In "default" mode every
// field has a value; in "override" mode an empty field inherits from the level
// above and shows what it would inherit.
const props = defineProps<{ modelValue: SettingsOverride; inherited?: SettingsOverride; mode: 'default' | 'override'; busy?: boolean }>()
const emit = defineEmits<{ save: [value: SettingsOverride] }>()
const { t } = useWorkspace()
const form = reactive({ audience: '', level: '', minutes: '' as number | string, depth: '' })
function load() {
  form.audience = props.modelValue.audience || ''
  form.level = props.modelValue.level || ''
  form.minutes = props.modelValue.minutes || ''
  form.depth = props.modelValue.depth || ''
}
watch(() => props.modelValue, load, { immediate: true, deep: true })
const override = computed(() => props.mode === 'override')
const levels = ['beginner', 'intermediate', 'advanced']
const depths = ['brief', 'standard', 'deep']
const hint = (key: 'audience' | 'level' | 'minutes' | 'depth') => {
  const value = props.inherited?.[key]
  if (!override.value || !value) return ''
  const shown = key === 'level' ? t('level_' + value) : key === 'depth' ? t('depth_' + value) : String(value)
  return t('inheritedFrom').replace('{value}', shown)
}
// Empty fields are zero values, which mean "inherit" on the server.
function save() {
  emit('save', { audience: form.audience.trim(), level: form.level, minutes: Number(form.minutes) || 0, depth: form.depth })
}
</script>

<template>
  <form class="settings-fields" @submit.prevent="save">
    <label>{{ t('settingsAudience') }}
      <input v-model="form.audience" :placeholder="hint('audience') || t('settingsAudiencePlaceholder')">
    </label>
    <label>{{ t('settingsLevel') }}
      <select v-model="form.level">
        <option v-if="override" value="">{{ hint('level') || t('inherit') }}</option>
        <option v-for="level in levels" :key="level" :value="level">{{ t('level_' + level) }}</option>
      </select>
    </label>
    <label>{{ t('settingsMinutes') }}
      <input v-model="form.minutes" type="number" min="1" max="180" :placeholder="hint('minutes')" :required="!override">
    </label>
    <label>{{ t('settingsDepth') }}
      <select v-model="form.depth">
        <option v-if="override" value="">{{ hint('depth') || t('inherit') }}</option>
        <option v-for="depth in depths" :key="depth" :value="depth">{{ t('depth_' + depth) }}</option>
      </select>
    </label>
    <button class="secondary" :disabled="busy">{{ t('saveSettings') }}</button>
  </form>
</template>
