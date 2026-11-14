<script setup lang="ts">
import { computed, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { ACTIVE } from '../../lib/status.ts'
import PlanChat from './PlanChat.vue'
import TaskRow from '../tasks/TaskRow.vue'
import ArtifactEditor from '../ArtifactEditor.vue'
import Icon from '../Icon.vue'
import type { Artifact, Rec, Request } from '../../api/types.ts'

// The experiment step writes a downloadable reproduction plan (output C),
// either on the AI's own or after a saved conversation with it.
const { state, t, api, act, paper } = useWorkspace()
const method = ref('auto')
const language = ref(state.language === 'en' ? 'en' : 'zh-TW')
const chatOpen = ref(false), editingID = ref('')
const chat = computed(() => (state.snapshot.plan_chat || []).filter(x => x.paper_id === state.paperID).at(-1) || null)
const chatCount = computed(() => chat.value?.data.messages?.length || 0)
const plans = computed(() => (state.snapshot.artifact || []).filter(a => a.paper_id === state.paperID && a.data.type === 'C').slice().reverse())
const editing = computed(() => plans.value.find(x => x.id === editingID.value))
const running = computed(() => (state.snapshot.task || []).filter(x => x.paper_id === state.paperID && x.data.type === 'C' && ACTIVE.includes(x.data.status)))
const canGenerate = computed(() => !state.busy && !running.value.length && state.connection.mode !== 'none' && (method.value === 'auto' || chatCount.value > 0))
if (chat.value) method.value = 'chat'

async function generate() {
  await act(async () => {
    const request = await api<Rec<Request>>(`/projects/${state.projectID}/requests`, {
      method: 'POST',
      body: {
        selected: ['C'], combined: false, use_paper_settings: false, languages: { A: '', B: '', C: language.value }, profile_id: '',
        settings: { depth: 'standard', level: 'intermediate', audience: 'classmates', minutes: 10, template_id: '', plan_chat_id: method.value === 'chat' ? chat.value?.id ?? '' : '', languages: {} },
        overrides: { A: {}, B: {}, C: {} }, paper_ids: [state.paperID], ui_language: state.language,
      },
    })
    await api(`/requests/${request.id}/start`, { method: 'POST', body: { use_defaults: true } })
    return request
  })
}
async function clearChat() {
  const current = chat.value
  if (current && confirm(t('deleteConfirm'))) await act(() => api('/records/' + current.id, { method: 'DELETE' }))
}
async function removePlan(plan: Rec<Artifact>) {
  if (!confirm(t('deletePlanConfirm'))) return
  if (editingID.value === plan.id) editingID.value = ''
  await act(() => api(`/artifacts/${plan.id}`, { method: 'DELETE' }))
}
const when = (iso: string | undefined) => iso ? new Date(iso).toLocaleString(state.language) : ''
</script>

<template>
  <section v-if="paper" class="panel plan-panel">
    <div class="section-heading">
      <div>
        <h2>{{ t('planTitle') }}</h2>
        <p class="muted">{{ t('planIntro') }}</p>
      </div>
    </div>

    <div class="plan-methods" role="radiogroup" :aria-label="t('planMethod')">
      <label class="plan-method" :class="{ chosen: method === 'auto' }">
        <input v-model="method" type="radio" value="auto">
        <span class="plan-method-icon"><Icon name="brain" /></span>
        <strong>{{ t('planMethodAuto') }}</strong>
        <small>{{ t('planMethodAutoHint') }}</small>
      </label>
      <label class="plan-method" :class="{ chosen: method === 'chat' }">
        <input v-model="method" type="radio" value="chat">
        <span class="plan-method-icon"><Icon name="message" /></span>
        <strong>{{ t('planMethodChat') }}</strong>
        <small>{{ t('planMethodChatHint') }}</small>
      </label>
    </div>

    <div v-if="method === 'chat'" class="plan-chat-status">
      <p v-if="chatCount" class="caption">{{ t('chatSaved').replace('{n}', String(chatCount)).replace('{time}', when(chat!.data.saved_at)) }}<span v-if="chat!.data.grill"> · {{ t('grillMode') }}</span></p>
      <p v-else class="caption">{{ t('noChatYet') }}</p>
      <div class="button-row">
        <button class="secondary" @click="chatOpen = true"><Icon name="message" /> {{ t(chatCount ? 'continueChat' : 'openChat') }}</button>
        <button v-if="chat" class="text-button danger" @click="clearChat">{{ t('clearChat') }}</button>
      </div>
    </div>

    <div class="form-actions plan-generate">
      <label class="inline-field">{{ t('outputLanguage') }}
        <select v-model="language"><option value="zh-TW">繁體中文</option><option value="en">English</option></select>
      </label>
      <p v-if="method === 'chat' && !chatCount" class="caption">{{ t('planNeedsChat') }}</p>
      <p v-else-if="method === 'chat'" class="caption">{{ t('planUsesChat') }}</p>
      <button class="primary" :disabled="!canGenerate" @click="generate"><Icon name="file" /> {{ t('generatePlan') }}</button>
    </div>
  </section>

  <section v-if="running.length" class="panel running-panel">
    <h3>{{ t('stepGenerating') }}</h3>
    <TaskRow v-for="task in running" :key="task.id" :task="task" compact />
  </section>

  <section class="panel">
    <div class="section-heading"><h3>{{ t('planVersions') }}</h3></div>
    <p v-if="!plans.length" class="empty-inline">{{ t('noPlans') }}</p>
    <div class="artifact-grid">
      <article v-for="plan in plans" :key="plan.id" class="artifact-card">
        <div class="form-row">
          <span class="output-letter small-letter">C</span>
          <strong class="grow">{{ t('planTitle') }}</strong>
        </div>
        <p class="caption">{{ plan.data.language }} · v{{ plan.version }} · {{ when(plan.created_at) }}<span v-if="plan.data.settings?.plan_chat_id"> · {{ t('planFromChat') }}</span></p>
        <p v-if="plan.data.mode === 'source-extract'" class="draft-notice">{{ t('extractive') }}</p>
        <div class="button-row">
          <button class="primary small" @click="editingID = plan.id">{{ t('openReader') }}</button>
          <a v-for="f in [['pdf', 'PDF'], ['docx', 'Word'], ['html', 'HTML']]" :key="f[0]" class="secondary small" :href="`/api/artifacts/${plan.id}/download?format=${f[0]}`" download><Icon name="download" /> {{ f[1] }}</a>
          <button class="text-button danger" @click="removePlan(plan)">{{ t('delete') }}</button>
        </div>
      </article>
    </div>
  </section>

  <PlanChat v-if="chatOpen" :chat="chat" @close="chatOpen = false" />
  <ArtifactEditor v-if="editing" :key="editing.id" :record="editing" @close="editingID = ''" @saved="editingID = $event.id" />
</template>
