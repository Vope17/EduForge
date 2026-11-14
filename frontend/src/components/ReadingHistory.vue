<script setup lang="ts">
import { ref } from 'vue'
import { useWorkspace } from '../lib/workspace.ts'
const { state, t, api, act } = useWorkspace()
const title = ref(''), terms = ref('')
async function add() { const result = await act(() => api('/reading', { method: 'POST', body: { title: title.value, paper_id: '', terms: terms.value.split(',').map(x => x.trim()).filter(Boolean), completed_at: '' } })); if (result) { title.value = ''; terms.value = '' } }
async function remove(id: string) { if (confirm(t('deleteConfirm'))) await act(() => api(`/records/${id}`, { method: 'DELETE' })) }
</script>
<template>
  <div class="settings-section"><p class="lede">{{ t('readAssumption') }}</p><section class="panel"><h2>{{ t('manualReading') }}</h2><form @submit.prevent="add"><div class="form-grid"><label>{{ t('paperTitle') }}<input v-model="title" required></label><label>{{ t('understoodTerms') }}<input v-model="terms"></label></div><button class="primary" :disabled="!!state.busy">＋ {{ t('create') }}</button></form></section><section class="panel"><div v-for="item in (state.snapshot.reading || []).slice().reverse()" :key="item.id" class="source-row"><span class="read-check">✓</span><div class="grow"><strong>{{ item.data.title }}</strong><p class="caption">{{ (item.data.terms || []).join(' · ') }}</p><small class="muted">{{ new Date(item.data.completed_at).toLocaleString(state.language) }}</small></div><button class="text-button danger" @click="remove(item.id)">{{ t('unread') }}</button></div><p v-if="!state.snapshot.reading?.length" class="empty-inline">{{ t('empty') }}</p></section></div>
</template>
