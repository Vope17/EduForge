<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '../components/AppIcon.vue'
import type { RevisionSummary } from '../api/documents'

// Saved versions: step back or forward one version, or view and restore any
// earlier one (each restore creates a new version).
const props = defineProps<{
  history: RevisionSummary[]
  current: number
  selected?: number
  pending: boolean
  dirty: boolean
  canUndo: boolean
  canRedo: boolean
}>()
const emit = defineEmits<{ load: []; view: [revision: number]; restore: [revision: number]; undo: []; redo: [] }>()
const { t } = useI18n()
onMounted(() => { if (!props.history.length) emit('load') })
</script>

<template>
  <div class="revision-panel">
    <aside class="revision-list">
      <div class="section-heading">
        <h3>{{ t('revisions.title') }}</h3>
        <span class="caption">{{ t('revisions.current', { number: current }) }}</span>
      </div>
      <p
        v-if="dirty"
        class="notice warning"
      >
        {{ t('revisions.saveFirst') }}
      </p>
      <div class="button-row">
        <button
          type="button"
          class="small"
          :disabled="pending || dirty || !canUndo"
          @click="emit('undo')"
        >
          <AppIcon name="undo" />{{ t('revisions.undo') }}
        </button>
        <button
          type="button"
          class="small"
          :disabled="pending || dirty || !canRedo"
          @click="emit('redo')"
        >
          <AppIcon name="redo" />{{ t('revisions.redo') }}
        </button>
      </div>
      <ol>
        <li
          v-for="item in history"
          :key="item.revision"
        >
          <button
            type="button"
            class="plain revision-row"
            :class="{ active: item.revision === selected, current: item.revision === current }"
            :aria-pressed="item.revision === selected"
            :disabled="pending"
            @click="emit('view', item.revision)"
          >
            <strong>{{ t('revisions.number', { number: item.revision }) }}</strong>
            <span>{{ item.title }}</span>
            <small>{{ item.created_at }}</small>
          </button>
        </li>
      </ol>
    </aside>
    <section class="revision-preview">
      <template v-if="selected">
        <div class="section-heading">
          <h3>{{ t('revisions.number', { number: selected }) }}</h3>
          <button
            type="button"
            class="document-primary small"
            :disabled="pending || dirty || selected === current"
            @click="emit('restore', selected)"
          >
            {{ t('revisions.restore') }}
          </button>
        </div>
        <slot />
      </template>
      <p
        v-else
        class="empty-inline"
      >
        {{ t('revisions.choose') }}
      </p>
    </section>
  </div>
</template>
