<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { connectionErrorKey, modelConnection, type Connection, type ModelChoice } from '../api/connections'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
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
const requestConsent = ref(false)

const statusKey = computed(() => {
  const current = connection.value
  if (!current?.connected) return 'models.disconnected'
  if (current.mode === 'chatgpt' && !current.model) return 'models.chooseModelStatus'
  return current.verified ? 'models.tested' : 'models.configured'
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

function load() {
  return run(async () => {
    applyConnection(await modelConnection.get(controller.signal))
    if (connection.value?.mode === 'chatgpt') await fetchModels()
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
    applyConnection(result.connection)
    apiKey.value = ''
    choices.value = []
    selectedModel.value = ''
    messageKey.value = result.remote_revoked ? 'models.disconnected' : 'models.revocationPending'
  })
}

function login(newAccount = false) {
  const current = connection.value
  if (!current) return
  if (window.location.origin !== current.chatgpt_origin) {
    const target = new URL('/settings/models', current.chatgpt_origin)
    target.searchParams.set('connect', newAccount ? 'new-chatgpt' : 'chatgpt')
    window.location.assign(target.toString())
    return
  }
  return run(async () => {
    const result = await modelConnection.login(newAccount, requestConsent.value, current.csrf_token, controller.signal)
    const target = new URL(result.authorization_url)
    if (target.protocol !== 'https:' || target.host !== 'auth.openai.com') throw new Error('invalid_authorization_url')
    window.location.assign(target.toString())
  })
}

function selectModel() {
  const current = connection.value
  if (!current || !selectedModel.value) return
  return run(async () => {
    applyConnection(await modelConnection.selectModel(
      selectedModel.value, current.version, current.csrf_token, controller.signal,
    ))
    messageKey.value = 'models.saved'
  })
}

onMounted(async () => {
  await load()
  const oauth = String(route.query.oauth ?? '')
  const resultKeys: Record<string, string> = {
    success: 'models.loginSuccess', denied: 'models.oauthDenied', permission: 'models.oauthPermission',
    expired: 'models.oauthExpired', failed: 'models.oauthFailed',
  }
  requestConsent.value = oauth === 'permission'
  if (oauth && resultKeys[oauth]) {
    messageKey.value = resultKeys[oauth]
    await router.replace({ name: 'model-settings' })
  }
  const connect = route.query.connect
  if ((connect === 'chatgpt' || connect === 'new-chatgpt') && connection.value) {
    await router.replace({ name: 'model-settings' })
    await login(connect === 'new-chatgpt')
  }
})
onBeforeUnmount(() => {
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
          <div v-if="connection.model">
            <dt>{{ t('models.model') }}</dt><dd>{{ connection.model }}</dd>
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
            :disabled="busy || !connection.model"
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
        <p class="document-muted">
          {{ t('models.loopbackNote') }}
        </p>
        <div class="document-actions">
          <button
            type="button"
            class="document-primary"
            data-oai-siwc-button
            :disabled="busy"
            @click="login()"
          >
            {{ t('models.continueChatGPT') }}
          </button>
          <button
            v-if="connection.mode === 'chatgpt'"
            type="button"
            :disabled="busy"
            @click="login(true)"
          >
            {{ t('models.otherAccount') }}
          </button>
          <a
            href="https://chatgpt.com/#settings/Usage"
            target="_blank"
            rel="noopener noreferrer"
          >{{ t('models.manageUsage') }}</a>
        </div>

        <form
          v-if="connection.mode === 'chatgpt'"
          class="document-fields"
          @submit.prevent="selectModel"
        >
          <label class="document-field">
            {{ t('models.model') }}
            <select
              v-model="selectedModel"
              required
              :disabled="busy || !choices.length"
            >
              <option
                value=""
                disabled
              >{{ t('models.chooseModel') }}</option>
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
              :disabled="busy || !selectedModel"
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
