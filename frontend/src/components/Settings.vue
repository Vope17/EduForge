<script setup lang="ts">
import { computed, reactive, ref, watch, onMounted, onUnmounted } from 'vue'
import { useWorkspace } from '../lib/workspace.ts'
import { move } from '../lib/i18n.ts'
import type { LoginState, ModelOption, Provider, Rec } from '../api/types.ts'
const props = withDefaults(defineProps<{ section?: string }>(), { section: 'providers' })
const { state, t, api, act, explain, providers, checkConnections } = useWorkspace()
const sub = computed(() => props.section)
// Per provider id: the editable copy, an entered key, its models, sign-in state, last error, loading.
const form = reactive<Record<string, Provider>>({}), keys = reactive<Record<string, string>>({}), models = reactive<Record<string, ModelOption[]>>({})
const login = reactive<Record<string, LoginState>>({}), connectionErrors = reactive<Record<string, string>>({}), loading = reactive<Record<string, boolean>>({})
const sshConfig = ref(''), sshKnown = ref(''), sshKey = ref('')
// Model providers are ordered by the user; the offline test mode sits apart.
const modelProviders = computed(() => providers.value.filter(p => p.data.kind !== 'source-extract'))
const offlineProviders = computed(() => providers.value.filter(p => p.data.kind === 'source-extract'))
watch(() => state.snapshot.provider, () => {
  for (const p of providers.value) if (!form[p.id]) form[p.id] = { ...p.data }
}, { immediate: true })
async function loadModels(p: Rec<Provider>) {
  if (loading[p.id]) return
  loading[p.id] = true
  connectionErrors[p.id] = ''
  try {
    const result = await api<ModelOption[]>('/providers/' + p.id + '/models')
    models[p.id] = result
    const provider = form[p.id]
    if (provider && result[0] && !result.some(model => model.id === provider.model)) {
      provider.model = result[0].id
      await api('/providers/' + p.id, { method: 'PUT', body: { provider: form[p.id] } })
    }
  } catch (error) { models[p.id] = []; connectionErrors[p.id] = (error as Error).message }
  finally { loading[p.id] = false }
}
async function save(p: Rec<Provider>, refreshModels = true) {
  const result = await act(() => api<Rec<Provider>>('/providers/' + p.id, {
    method: 'PUT', body: { provider: form[p.id], key: keys[p.id] || '' },
  }))
  if (result) {
    keys[p.id] = ''
    form[p.id] = { ...result.data }
    if (refreshModels && p.data.kind !== 'source-extract') await loadModels(p)
    void checkConnections()
  }
}
async function signIn(p: Rec<Provider>) {
  const result = await act(() => api<LoginState>('/providers/' + p.id + '/login', { method: 'POST' }), { success: false })
  if (result) login[p.id] = result
}
async function pollLogins() {
  for (const p of providers.value.filter(p => login[p.id]?.status === 'pending')) {
    try {
      const next = await api<LoginState>('/providers/' + p.id + '/login')
      login[p.id] = next
      if (next.status === 'connected') { await loadModels(p); void checkConnections() }
    } catch (error) { connectionErrors[p.id] = (error as Error).message }
  }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(async () => {
  for (const p of providers.value) {
    if (p.data.kind === 'codex') {
      try { login[p.id] = await api<LoginState>('/providers/' + p.id + '/login') } catch {}
    }
    if (p.data.has_credential || ['codex', 'claude-cli'].includes(p.data.kind)) void loadModels(p)
  }
  timer = setInterval(pollLogins, 2000)
})
onUnmounted(() => clearInterval(timer))
async function reorder(index: number, offset: number) {
  const ids = move(modelProviders.value.map(p => p.id), index, offset).concat(offlineProviders.value.map(p => p.id))
  await act(() => api('/providers/order', { method: 'PUT', body: { ids } }))
  void checkConnections()
}
async function saveSSH() {
  const result = await act(() => api('/connections/ssh', { method: 'PUT', body: { config: sshConfig.value, known_hosts: sshKnown.value, private_key: sshKey.value } }))
  if (result) { sshConfig.value = ''; sshKnown.value = ''; sshKey.value = '' }
}
</script>

<template>
  <div class="settings-section">
    <section v-show="sub === 'providers'" class="panel subpage">
      <h2>{{ t('providerOrder') }}</h2>
      <p class="caption">{{ t('connectionFlow') }}</p>
      <form v-for="(p, index) in modelProviders" :key="p.id" class="provider-form" @submit.prevent="save(p)">
        <template v-if="form[p.id]">
          <div class="section-heading">
            <h3>{{ p.data.name }}</h3>
            <div class="button-row">
              <button type="button" class="icon-button" :aria-label="t('moveUp')" @click="reorder(index,-1)">↑</button>
              <button type="button" class="icon-button" :aria-label="t('moveDown')" @click="reorder(index,1)">↓</button>
              <label class="check"><input v-model="form[p.id].enabled" type="checkbox" @change="save(p, false)">{{ t('enabled') }}</label>
            </div>
          </div>
          <template v-if="p.data.kind === 'codex'">
            <button type="button" class="primary" :disabled="!!state.busy || login[p.id]?.status === 'pending'" @click="signIn(p)">Sign in with OpenAI</button>
            <div v-if="login[p.id]?.status === 'pending'" class="login-code">
              <p>{{ t('enterLoginCode') }}</p>
              <p><code>{{ login[p.id].user_code }}</code></p>
              <a class="text-button" :href="login[p.id].verification_url" target="_blank" rel="noopener noreferrer">{{ t('openOfficialLogin') }} ↗</a>
              <p class="caption">{{ t('waitingForLogin') }}</p>
            </div>
            <p v-if="login[p.id]?.status === 'connected'" class="pill success">{{ t('subscriptionConnected') }}</p>
            <p v-if="login[p.id]?.error" class="issue-text">{{ explain(login[p.id]!.error!) }}</p>
          </template>
          <p v-if="p.data.kind === 'claude-cli'" class="caption">{{ t('claudeLoginHint') }}</p>
          <label v-if="['openai','anthropic'].includes(p.data.kind)">{{ t('apiKey') }}<input v-model="keys[p.id]" type="password" autocomplete="new-password"></label>
          <button v-if="['openai','anthropic'].includes(p.data.kind)" class="secondary" :disabled="!!state.busy">{{ t('saveAndConnect') }}</button>
          <label>{{ t('model') }}
            <select v-model="form[p.id].model" :disabled="loading[p.id] || !models[p.id]?.length" @change="save(p, false)">
              <option v-if="!models[p.id]?.length" :value="form[p.id].model">{{ t('connectToChooseModel') }}</option>
              <option v-for="model in models[p.id] || []" :key="model.id" :value="model.id">{{ model.name }} · {{ model.id }}</option>
            </select>
          </label>
          <button type="button" class="text-button" :disabled="loading[p.id]" @click="loadModels(p)">{{ t(loading[p.id] ? 'loading' : 'refreshModels') }}</button>
          <p v-if="connectionErrors[p.id]" class="caption">{{ explain(connectionErrors[p.id]) }}</p>
        </template>
      </form>
      <details v-if="offlineProviders.length" class="advanced-settings" :open="offlineProviders.some(p => p.data.enabled)">
        <summary>{{ t('advancedSettings') }}</summary>
        <form v-for="p in offlineProviders" :key="p.id" class="provider-form" @submit.prevent>
          <template v-if="form[p.id]">
            <div class="section-heading">
              <h3>{{ t('offlineMode') }}</h3>
              <label class="check"><input v-model="form[p.id].enabled" type="checkbox" @change="save(p, false)">{{ t('enabled') }}</label>
            </div>
            <p class="caption">{{ t('offlineModeHint') }}</p>
          </template>
        </form>
      </details>
    </section>
    <section v-show="sub === 'ssh'" class="panel subpage"><div class="section-heading"><h2>{{ t('ssh') }}</h2></div><p class="caption">{{ t('sshHint') }}</p><form @submit.prevent="saveSSH"><label>SSH config<textarea v-model="sshConfig" rows="5" spellcheck="false" placeholder="Host lab&#10;  HostName …&#10;  User …"></textarea></label><label>known_hosts<textarea v-model="sshKnown" rows="3" spellcheck="false"></textarea></label><label>{{ t('sshPrivateKey') }}<textarea v-model="sshKey" rows="3" spellcheck="false" autocomplete="off"></textarea></label><button class="secondary" :disabled="!!state.busy">{{ t('save') }}</button></form></section>
  </div>
</template>
