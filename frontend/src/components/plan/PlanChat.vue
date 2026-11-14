<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { markupToHtml } from '../../lib/markup.ts'
import type { ChatMessage, PlanChat, Rec } from '../../api/types.ts'

// A chat with the AI about how to reproduce the paper's experiments. Saved
// conversations are taken into account when the reproduction plan is written.
const props = withDefaults(defineProps<{ chat?: Rec<PlanChat> | null }>(), { chat: null })
const emit = defineEmits<{ close: []; saved: [chat: Rec<PlanChat>] }>()
const { state, t, api, act } = useWorkspace()
const messages = ref<ChatMessage[]>(JSON.parse(JSON.stringify(props.chat?.data.messages || [])))
const grill = ref(!!props.chat?.data.grill)
const input = ref(''), waiting = ref(false), dirty = ref(false)
const list = ref<HTMLElement | null>(null), box = ref<HTMLTextAreaElement | null>(null)
const canSend = computed(() => input.value.trim() && !waiting.value)

async function scrollDown() { await nextTick(); if (list.value) list.value.scrollTop = list.value.scrollHeight }
async function send(text = input.value) {
  text = text.trim()
  if (!text || waiting.value) return
  messages.value.push({ role: 'user', text, at: new Date().toISOString() })
  input.value = ''
  dirty.value = true
  waiting.value = true
  scrollDown()
  const result = await act(() => api<{ reply: string }>(`/papers/${state.paperID}/plan-chat/reply`, {
    method: 'POST', body: { messages: messages.value.map(({ role, text }) => ({ role, text })), grill: grill.value, language: state.language },
  }))
  waiting.value = false
  if (result?.reply) messages.value.push({ role: 'assistant', text: result.reply, at: new Date().toISOString() })
  scrollDown()
  box.value?.focus()
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); send() }
}
async function save() {
  const result = await act(() => api<Rec<PlanChat>>(`/papers/${state.paperID}/plan-chat`, { method: 'PUT', body: { messages: messages.value, grill: grill.value } }))
  if (result) { dirty.value = false; emit('saved', result) }
}
function leave() {
  if (dirty.value && !confirm(t('discardChat'))) return
  emit('close')
}
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') leave() }
onMounted(() => { document.addEventListener('keydown', onKey); document.body.classList.add('no-scroll'); scrollDown(); box.value?.focus() })
onUnmounted(() => { document.removeEventListener('keydown', onKey); document.body.classList.remove('no-scroll') })
</script>

<template>
  <Teleport to="body">
    <div class="sheet-backdrop" @click.self="leave">
      <section class="sheet plan-chat" role="dialog" aria-modal="true" :aria-label="t('chatTitle')">
        <header class="sheet-head">
          <div class="grow">
            <span class="eyebrow">{{ t('planTitle') }}</span>
            <h2>{{ t('chatTitle') }}</h2>
          </div>
          <label class="switch" :title="t('grillHint')">
            <input v-model="grill" type="checkbox" @change="dirty = true">
            <span>{{ t('grillMode') }}</span>
          </label>
          <span v-if="!dirty && messages.length" class="caption">{{ t('chatIsSaved') }}</span>
          <button class="secondary" @click="leave">{{ t('chatLeave') }}</button>
          <button class="primary" :disabled="!!state.busy && !waiting" @click="save">{{ t('chatSave') }}</button>
        </header>
        <div ref="list" class="chat-list">
          <div v-if="!messages.length" class="chat-empty">
            <p>{{ t(grill ? 'grillHint' : 'chatHint') }}</p>
            <button v-if="grill" class="secondary" :disabled="waiting" @click="send(t('chatStartPrompt'))">{{ t('chatStart') }}</button>
          </div>
          <div v-for="(m, i) in messages" :key="i" class="chat-message" :class="m.role">
            <div v-if="m.role === 'assistant'" class="chat-bubble" v-html="markupToHtml(m.text)"></div>
            <div v-else class="chat-bubble">{{ m.text }}</div>
          </div>
          <div v-if="waiting" class="chat-message assistant"><div class="chat-bubble thinking">{{ t('chatThinking') }}</div></div>
        </div>
        <footer class="chat-input">
          <textarea ref="box" v-model="input" rows="2" :placeholder="t('chatPlaceholder')" @keydown="keydown"></textarea>
          <button class="primary" :disabled="!canSend" @click="send()">{{ t('send') }}</button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
