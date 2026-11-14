<script setup lang="ts">
import { useWorkspace } from '../../lib/workspace.ts'
const { state, t, go } = useWorkspace()
</script>

<template>
  <div v-if="!state.connection.checking && state.connection.mode !== 'model'" class="connection-notice" :class="state.connection.mode">
    <span>{{ t(state.connection.mode === 'extract' ? 'noticeExtractOnly' : 'noticeNoProvider') }}</span>
    <button type="button" class="text-button" @click="go({ name: 'settings', tab: 'providers' })">{{ t('tabProviders') }} →</button>
  </div>
</template>
