<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { documentErrorKey, documents, type Change, type DocumentDetails, type RevisionSummary, type SavedDocument } from '../api/documents'
import DocumentEditor from '../editors/document/DocumentEditor.vue'
import DocumentPreview from '../editors/document/DocumentPreview.vue'

const { t } = useI18n()
const id = String(useRoute().params.id)
const controller = new AbortController()
const details = ref<DocumentDetails>()
const busy = ref(false)
const errorKey = ref('')
const feedback = ref('')
const history = ref<RevisionSummary[]>([])
const previous = ref<SavedDocument>()
const fields = ['palette', 'font', 'layout', 'tone', 'density'] as const

async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  errorKey.value = ''
  try {
    await action()
  } catch (error) {
    if (!controller.signal.aborted) errorKey.value = documentErrorKey(error)
  } finally {
    busy.value = false
  }
}

function load() {
  return run(async () => {
    details.value = await documents.get(id, controller.signal)
  })
}

function preview() {
  const current = details.value
  if (!current) return
  return run(async () => {
    current.request = await documents.preview(id, current.request.template_version, feedback.value, controller.signal)
    feedback.value = ''
  })
}

function confirm() {
  const current = details.value
  if (!current) return
  return run(async () => {
    current.request = await documents.confirm(id, current.request.template_version, controller.signal)
  })
}

function generate() {
  const current = details.value
  if (!current) return
  return run(async () => {
    current.document = await documents.generate(id, current.request.confirmed_version, controller.signal)
    current.request.state = 'ready'
  })
}

function change(update: Change) {
  const current = details.value
  if (!current?.document) return
  const revision = current.document.revision
  return run(async () => {
    current.document = await documents.change(id, revision, update, controller.signal)
    history.value = []
    previous.value = undefined
  })
}

function loadHistory() {
  return run(async () => {
    history.value = await documents.history(id, controller.signal)
  })
}

function viewRevision(revision: number) {
  if (!revision) return
  return run(async () => {
    previous.value = await documents.revision(id, revision, controller.signal)
  })
}

onMounted(load)
onBeforeUnmount(() => controller.abort())
</script>

<template>
  <main
    id="main"
    class="workspace document-page"
    tabindex="-1"
  >
    <RouterLink
      class="document-back"
      :to="{ name: 'documents' }"
    >{{ t('documents.back') }}</RouterLink>
    <p
      v-if="busy"
      role="status"
    >
      {{ t('documents.working') }}
    </p>
    <p
      v-if="errorKey"
      class="document-error"
      role="alert"
    >
      {{ t(errorKey) }}
    </p>
    <RouterLink
      v-if="errorKey === 'documents.errors.provider_unavailable' || errorKey === 'documents.errors.plan_permission'"
      :to="{ name: 'model-settings' }"
    >
      {{ t('models.openSettings') }}
    </RouterLink>
    <button
      v-if="!details && errorKey"
      type="button"
      @click="load"
    >
      {{ t('documents.refresh') }}
    </button>

    <a
      v-if="errorKey === 'documents.errors.subscription_limit'"
      href="https://chatgpt.com/#settings/Usage"
      target="_blank"
      rel="noopener noreferrer"
    >{{ t('models.manageUsage') }}</a>

    <template v-if="details">
      <h1>{{ details.document?.content.title ?? details.request.topic }}</h1>
      <p class="document-muted">
        {{ t('documents.kinds.' + details.request.kind) }} ·
        {{ t(details.request.language === 'zh-TW' ? 'language.zhTW' : 'language.en') }} ·
        {{ t('documents.states.' + details.request.state) }}
      </p>

      <DocumentEditor
        v-if="details.document"
        :document="details.document"
        :spec="details.request.template"
        :pending="busy"
        :history="history"
        :previous="previous"
        @change="change"
        @history="loadHistory"
        @view-revision="viewRevision"
      />

      <section
        v-else
        class="document-panel document-fields"
      >
        <template v-if="details.request.template">
          <h2>{{ t('documents.template.version', { number: details.request.template_version }) }}</h2>
          <dl class="template-fields">
            <div
              v-for="field in fields"
              :key="field"
            >
              <dt>{{ t('documents.template.' + field) }}</dt>
              <dd>{{ t('documents.styles.' + details.request.template[field]) }}</dd>
            </div>
          </dl>
          <DocumentPreview
            :blocks="details.request.template.example"
            :spec="details.request.template"
          />
        </template>

        <template v-if="details.request.state === 'requested'">
          <h2>{{ t('documents.template.title') }}</h2>
          <p>{{ t('documents.template.description') }}</p>
          <button
            type="button"
            class="document-primary"
            :disabled="busy"
            @click="preview"
          >
            {{ t('documents.template.preview') }}
          </button>
        </template>

        <template v-if="details.request.state === 'awaiting_confirmation'">
          <button
            type="button"
            class="document-primary"
            :disabled="busy"
            @click="confirm"
          >
            {{ t('documents.template.confirm') }}
          </button>
          <form
            class="document-fields"
            @submit.prevent="preview"
          >
            <label class="document-field">
              {{ t('documents.template.feedback') }}
              <textarea
                v-model="feedback"
                maxlength="400"
                rows="3"
                required
                :disabled="busy"
              />
            </label>
            <button
              type="submit"
              :disabled="busy || !feedback.trim()"
            >
              {{ t('documents.template.adjust') }}
            </button>
          </form>
        </template>

        <button
          v-if="details.request.state === 'confirmed'"
          type="button"
          class="document-primary"
          :disabled="busy"
          @click="generate"
        >
          {{ t('documents.template.generate') }}
        </button>
        <button
          v-if="details.request.state === 'generating'"
          type="button"
          :disabled="busy"
          @click="load"
        >
          {{ t('documents.refresh') }}
        </button>
      </section>
    </template>
  </main>
</template>
