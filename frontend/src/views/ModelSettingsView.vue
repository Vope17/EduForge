<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  connectionErrorKey, modelConnection, type Connection, type LoginState, type ModelChoice,
} from '../api/connections'

const POLL_MS = 3000

const { t } = useI18n()
const controller = new AbortController()
const connection = ref<Connection>()
const method = ref<'chatgpt' | 'api_key'>('chatgpt')
const endpoint = ref('https://api.openai.com/v1/chat/completions')
const model = ref('')
const apiKey = ref('')
const choices = ref<ModelChoice[]>([])
const selectedModel = ref('')
const busy = ref(false)
const errorKey = ref('')
const messageKey = ref('')
const signIn = ref<LoginState>({ status: 'idle' })
let pollTimer: ReturnType<typeof setTimeout> | undefined

const statusKey = computed(() => {
  const current = connection.value
  if (!current?.connected) return 'models.disconnected'
  return current.verified ? 'models.tested' : 'models.configured'
})
const expiresAt = computed(() => {
  const value = signIn.value.expires_at
  return value ? new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''
})

function applyConnection(value: Connection) {
  connection.value = value
  if (value.mode) method.value = value.mode
  if (value.mode === 'api_key') {
    endpoint.value = value.endpoint
    model.value = value.model
  }
  selectedModel.value = value.model
}

async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  errorKey.value = ''
  messageKey.value = ''
  try {
    await action()
  } catch (error) {
    if (!controller.signal.aborted) errorKey.value = connectionErrorKey(error)
  } finally {
    busy.value = false
  }
}

async function fetchModels() {
  choices.value = await modelConnection.models(controller.signal)
}

async function refresh() {
  applyConnection(await modelConnection.get(controller.signal))
  if (connection.value?.mode === 'chatgpt') await fetchModels()
}

function load() {
  return run(async () => {
    await refresh()
    const state = await modelConnection.loginStatus(controller.signal)
    if (state.status === 'pending') waitForSignIn(state)
  })
}

function save() {
  if (busy.value) return
  const current = connection.value
  if (!current) return
  const input = { endpoint: endpoint.value, model: model.value, api_key: apiKey.value, version: current.version }
  apiKey.value = ''
  return run(async () => {
    applyConnection(await modelConnection.save(input, current.csrf_token, controller.signal))
    messageKey.value = 'models.saved'
  })
}

function test() {
  const current = connection.value
  if (!current) return
  return run(async () => {
    applyConnection(await modelConnection.test(current.version, current.csrf_token, controller.signal))
    messageKey.value = 'models.testSuccess'
  })
}

function disconnect() {
  const current = connection.value
  if (!current) return
  return run(async () => {
    const result = await modelConnection.disconnect(current.version, current.csrf_token, controller.signal)
    stopWaiting()
    signIn.value = { status: 'idle' }
    applyConnection(result.connection)
    apiKey.value = ''
    choices.value = []
    selectedModel.value = ''
    messageKey.value = result.signed_out ? 'models.disconnected' : 'models.signOutPending'
  })
}

function stopWaiting() {
  clearTimeout(pollTimer)
  pollTimer = undefined
}

// Shows the device code and polls until the sign-in finishes in the other tab.
function waitForSignIn(state: LoginState) {
  signIn.value = state
  stopWaiting()
  if (state.status !== 'pending') return
  pollTimer = setTimeout(async () => {
    try {
      const next = await modelConnection.loginStatus(controller.signal)
      if (next.status === 'connected') {
        signIn.value = next
        await run(async () => {
          await refresh()
          messageKey.value = 'models.loginSuccess'
        })
      } else if (next.status === 'failed' || next.status === 'idle') {
        signIn.value = next
        errorKey.value = 'models.loginFailed'
      } else waitForSignIn(next)
    } catch {
      if (!controller.signal.aborted) waitForSignIn(signIn.value)
    }
  }, POLL_MS)
}

function login() {
  const current = connection.value
  if (!current) return
  return run(async () => {
    const state = await modelConnection.login(current.csrf_token, controller.signal)
    const target = new URL(state.verification_url ?? '')
    if (target.protocol !== 'https:' || target.host !== 'auth.openai.com') throw new Error('invalid_login_url')
    waitForSignIn(state)
  })
}

function selectModel() {
  const current = connection.value
  if (!current) return
  return run(async () => {
    applyConnection(await modelConnection.selectModel(
      selectedModel.value, current.version, current.csrf_token, controller.signal,
    ))
    messageKey.value = 'models.saved'
  })
}

onMounted(load)
onBeforeUnmount(() => {
  stopWaiting()
  controller.abort()
  apiKey.value = ''
})
</script>

<template>
  <main
    id="main"
    class="workspace document-page model-settings"
    tabindex="-1"
  >
    <h1>{{ t('models.title') }}</h1>
    <p>{{ t('models.description') }}</p>
    <p
      v-if="busy"
      role="status"
    >
      {{ t('models.working') }}
    </p>
    <p
      v-if="errorKey"
      class="document-error"
      role="alert"
    >
      {{ t(errorKey) }}
    </p>
    <p
      v-if="messageKey"
      class="connection-message"
      role="status"
    >
      {{ t(messageKey) }}
    </p>
    <button
      v-if="!connection"
      type="button"
      :disabled="busy"
      @click="load"
    >
      {{ t('models.reload') }}
    </button>

    <template v-if="connection">
      <section
        class="document-panel document-fields"
        :aria-label="t('models.current')"
      >
        <div class="document-heading">
          <h2>{{ t('models.current') }}</h2>
          <span>{{ t(statusKey) }}</span>
        </div>
        <dl
          v-if="connection.connected"
          class="template-fields"
        >
          <div>
            <dt>{{ t('models.method') }}</dt>
            <dd>{{ t(connection.mode === 'chatgpt' ? 'models.subscription' : 'models.apiKey') }}</dd>
          </div>
          <div v-if="connection.account">
            <dt>{{ t('models.account') }}</dt><dd>{{ connection.account }}</dd>
          </div>
          <div v-if="connection.model || connection.mode === 'chatgpt'">
            <dt>{{ t('models.model') }}</dt><dd>{{ connection.model || t('models.defaultModel') }}</dd>
          </div>
          <div v-if="connection.source === 'environment'">
            <dt>{{ t('models.source') }}</dt><dd>{{ t('models.serverSettings') }}</dd>
          </div>
        </dl>
        <div
          v-if="connection.connected"
          class="document-actions"
        >
          <button
            type="button"
            :disabled="busy"
            @click="test"
          >
            {{ t('models.test') }}
          </button>
          <button
            type="button"
            :disabled="busy"
            @click="disconnect"
          >
            {{ t('models.disconnect') }}
          </button>
          <RouterLink :to="{ name: 'documents' }">{{ t('models.documents') }}</RouterLink>
        </div>
      </section>

      <fieldset
        class="connection-methods"
        :disabled="busy"
      >
        <legend>{{ t('models.method') }}</legend>
        <label><input
          v-model="method"
          type="radio"
          value="chatgpt"
        >{{ t('models.subscription') }}</label>
        <label><input
          v-model="method"
          type="radio"
          value="api_key"
        >{{ t('models.apiKey') }}</label>
      </fieldset>

      <section
        v-if="method === 'chatgpt'"
        class="document-panel document-fields"
      >
        <h2>{{ t('models.subscription') }}</h2>
        <p>{{ t('models.subscriptionDescription') }}</p>
        <p
          v-if="!connection.chatgpt_available"
          class="document-error"
        >
          {{ t('models.codexMissing') }}
        </p>
        <div
          v-if="signIn.status === 'pending'"
          class="sign-in-code"
          role="status"
        >
          <p>{{ t('models.codeStep1') }}</p>
          <a
            class="document-primary"
            :href="signIn.verification_url"
            target="_blank"
            rel="noopener noreferrer"
          >{{ t('models.openSignIn') }}</a>
          <p>{{ t('models.codeStep2') }}</p>
          <output
            class="sign-in-code-value"
            data-sign-in-code
          >{{ signIn.user_code }}</output>
          <p class="document-muted">
            {{ t('models.codeWaiting', { time: expiresAt }) }}
          </p>
        </div>
        <div
          v-else
          class="document-actions"
        >
          <button
            v-if="connection.mode !== 'chatgpt'"
            type="button"
            class="document-primary"
            data-sign-in
            :disabled="busy || !connection.chatgpt_available"
            @click="login"
          >
            {{ t('models.continueChatGPT') }}
          </button>
          <a
            href="https://chatgpt.com/#settings/Usage"
            target="_blank"
            rel="noopener noreferrer"
          >{{ t('models.manageUsage') }}</a>
        </div>
        <p
          v-if="connection.mode === 'chatgpt'"
          class="document-muted"
        >
          {{ t('models.switchAccount') }}
        </p>

        <form
          v-if="connection.mode === 'chatgpt'"
          class="document-fields"
          @submit.prevent="selectModel"
        >
          <label class="document-field">
            {{ t('models.model') }}
            <select
              v-model="selectedModel"
              :disabled="busy"
            >
              <option value="">{{ t('models.defaultModel') }}</option>
              <option
                v-for="choice in choices"
                :key="choice.id"
                :value="choice.id"
              >{{ choice.name || choice.id }}</option>
            </select>
          </label>
          <p
            v-if="!choices.length"
            class="document-muted"
          >
            {{ t('models.noModels') }}
          </p>
          <div class="document-actions">
            <button
              type="submit"
              :disabled="busy || selectedModel === connection.model"
            >
              {{ t('models.applyModel') }}
            </button>
            <button
              type="button"
              :disabled="busy"
              @click="run(fetchModels)"
            >
              {{ t('models.refreshModels') }}
            </button>
          </div>
        </form>
      </section>

      <section
        v-else
        class="document-panel document-fields"
      >
        <h2>{{ t('models.apiKey') }}</h2>
        <p class="document-muted">
          {{ t('models.apiDescription') }}
        </p>
        <form
          class="document-fields"
          @submit.prevent="save"
        >
          <fieldset
            class="document-fields"
            :disabled="busy"
          >
            <label class="document-field">
              {{ t('models.endpoint') }}
              <input
                v-model="endpoint"
                type="url"
                required
                pattern="https://.+"
                maxlength="2048"
                autocomplete="off"
              >
            </label>
            <label class="document-field">
              {{ t('models.model') }}
              <input
                v-model="model"
                required
                maxlength="200"
                :placeholder="t('models.modelPlaceholder')"
                autocomplete="off"
              >
            </label>
            <label class="document-field">
              {{ t('models.apiKey') }}
              <input
                v-model="apiKey"
                name="api-key"
                type="password"
                required
                maxlength="8192"
                autocomplete="off"
                :spellcheck="false"
                autocapitalize="none"
              >
            </label>
            <button
              type="submit"
              class="document-primary"
            >
              {{ t('models.save') }}
            </button>
          </fieldset>
        </form>
      </section>
      <p class="document-muted">
        {{ t('models.sessionNote') }}
      </p>
    </template>
  </main>
</template>
