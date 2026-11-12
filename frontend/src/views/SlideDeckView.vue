<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { DocumentError, documentErrorKey, generationJobs, slides, type Block, type Change, type DocumentDetails, type PatchProposal, type RevisionSummary, type SavedDocument, type SlideLayout } from '../api/documents'
import type { Anchor } from '../selection/useSelection'
import SlideCanvas from '../editors/slides/SlideCanvas.vue'
import SlidesEditor from '../editors/slides/SlidesEditor.vue'

const { t } = useI18n()
const id = String(useRoute().params.id)
const controller = new AbortController()
const details = ref<DocumentDetails>()
const busy = ref(false)
const errorKey = ref('')
const feedback = ref('')
const preferredLayout = ref<SlideLayout>('title_body')
const history = ref<RevisionSummary[]>([])
const previous = ref<SavedDocument>()
const proposal = ref<PatchProposal>()
const fields = ['palette', 'font', 'tone', 'density'] as const
const layouts: SlideLayout[] = ['title_body', 'two_columns', 'image_text']
const activeJob = computed(() => !!details.value?.job && ['queued', 'running'].includes(details.value.job.state))
let timer: ReturnType<typeof setTimeout> | undefined

function sample(layout: SlideLayout): Block {
  return {
    kind: 'slide', text: '', layout,
    children: [
      { kind: 'heading', text: t('slides.sampleTitle') },
      { kind: 'paragraph', text: t('slides.sampleText') },
      ...(layout === 'image_text' ? [{ kind: 'image' as const, text: t('slides.sampleImage'), image_url: '' }] :
        layout === 'two_columns' ? [{ kind: 'paragraph' as const, text: t('slides.sampleQuestion') }] : []),
    ],
  }
}

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

function schedulePoll() {
  clearTimeout(timer)
  if (activeJob.value && !controller.signal.aborted) timer = setTimeout(poll, 1200)
}

async function refresh() {
  details.value = await slides.get(id, controller.signal)
  schedulePoll()
}

async function poll() {
  const current = details.value
  if (!current?.job || controller.signal.aborted) return
  try {
    current.job = await generationJobs.get(current.job.job_id, controller.signal)
    if (['queued', 'running'].includes(current.job.state)) schedulePoll()
    else await refresh()
  } catch (error) {
    if (!controller.signal.aborted) errorKey.value = documentErrorKey(error)
    // Keep retrying transient status failures; the durable job continues on the server.
    if (!controller.signal.aborted) timer = setTimeout(poll, 4000)
  }
}

function preview() {
  const current = details.value
  if (!current || activeJob.value) return
  return run(async () => {
    const preference = t('slides.layoutPreference', { layout: t('slides.layouts.' + preferredLayout.value) })
    current.request = await slides.preview(id, current.request.template_version, [preference, feedback.value].filter(Boolean).join('\n'), controller.signal)
    feedback.value = ''
  })
}

function confirm() {
  const current = details.value
  if (!current) return
  return run(async () => { current.request = await slides.confirm(id, current.request.template_version, controller.signal) })
}

function generate() {
  const current = details.value
  if (!current || activeJob.value) return
  return run(async () => {
    current.job = await slides.generate(id, current.request.confirmed_version, controller.signal)
    schedulePoll()
  })
}

function cancel() {
  const current = details.value
  if (!current?.job) return
  return run(async () => {
    current.job = await generationJobs.cancel(current.job!.job_id, controller.signal)
    if (activeJob.value) schedulePoll()
    else await refresh()
  })
}

function change(update: Change) {
  const current = details.value
  if (!current?.document) return
  const revision = current.document.revision
  return run(async () => {
    current.document = await slides.change(id, revision, update, controller.signal)
    history.value = []
    previous.value = undefined
    proposal.value = undefined
  })
}

function propose(instruction: string, selection: Anchor[]) {
  const revision = details.value?.document?.revision
  if (!revision) return
  return run(async () => { proposal.value = await slides.propose(id, revision, instruction, selection, controller.signal) })
}

function accept() {
  const current = details.value
  const pending = proposal.value
  if (!current || !pending) return
  return run(async () => {
    current.document = await slides.accept(id, pending.proposal_id, controller.signal)
    proposal.value = undefined
    previous.value = undefined
    history.value = []
  })
}

function reject() {
  const pending = proposal.value
  if (!pending) return
  return run(async () => {
    await slides.reject(id, pending.proposal_id, controller.signal)
    proposal.value = undefined
  })
}

function loadHistory() {
  return run(async () => { history.value = await slides.history(id, controller.signal) })
}

function viewRevision(revision: number) {
  if (revision) return run(async () => { previous.value = await slides.revision(id, revision, controller.signal) })
}

onMounted(() => run(refresh))
onBeforeUnmount(() => { clearTimeout(timer); controller.abort() })
</script>

<template>
  <main
    id="main"
    class="workspace document-page slides-page"
    tabindex="-1"
  >
    <RouterLink
      class="document-back"
      :to="{ name: 'slides' }"
    >{{ t('slides.back') }}</RouterLink>
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
      v-if="errorKey === 'documents.errors.provider_unavailable' || errorKey === 'documents.errors.plan_permission' || details?.job?.error_code === 'provider_unavailable' || details?.job?.error_code === 'plan_permission'"
      :to="{ name: 'model-settings' }"
    >{{ t('models.openSettings') }}</RouterLink>
    <a
      v-if="errorKey === 'documents.errors.subscription_limit' || details?.job?.error_code === 'subscription_limit'"
      href="https://chatgpt.com/#settings/Usage"
      target="_blank"
      rel="noopener noreferrer"
    >{{ t('models.manageUsage') }}</a>
    <button
      v-if="!details && errorKey"
      type="button"
      @click="run(refresh)"
    >
      {{ t('documents.refresh') }}
    </button>
    <template v-if="details">
      <h1>{{ details.document?.content.title ?? details.request.topic }}</h1>
      <p class="document-muted">
        {{ t('slides.title') }} · {{ t(details.request.language === 'zh-TW' ? 'language.zhTW' : 'language.en') }} · {{ t('documents.states.' + details.request.state) }}
      </p>
      <p class="document-muted">
        {{ t('documents.editor.aiNotice') }}
      </p>

      <SlidesEditor
        v-if="details.document"
        :document="details.document"
        :spec="details.request.template"
        :pending="busy"
        :history="history"
        :previous="previous"
        :proposal="proposal"
        @change="change"
        @history="loadHistory"
        @view-revision="viewRevision"
        @propose="propose"
        @accept="accept"
        @reject="reject"
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
              <dt>{{ t('documents.template.' + field) }}</dt><dd>{{ t('documents.styles.' + details.request.template[field]) }}</dd>
            </div>
            <div><dt>{{ t('documents.template.layout') }}</dt><dd>{{ t('slides.layouts.' + details.request.template.layout) }}</dd></div>
          </dl>
          <SlideCanvas
            :page="details.request.template.example[0]"
            :spec="details.request.template"
          />
          <p class="slide-history-notes">
            {{ details.request.template.example[0].notes }}
          </p>
        </template>

        <template v-if="['requested', 'awaiting_confirmation'].includes(details.request.state) && !activeJob">
          <h2>{{ t('slides.templateTitle') }}</h2>
          <p>{{ t('slides.templateDescription') }}</p>
          <div
            class="slide-layout-choices"
            role="group"
            :aria-label="t('documents.template.layout')"
          >
            <button
              v-for="layout in layouts"
              :key="layout"
              type="button"
              :aria-pressed="preferredLayout === layout"
              :disabled="busy"
              @click="preferredLayout = layout"
            >
              <SlideCanvas :page="sample(layout)" /><span>{{ t('slides.layouts.' + layout) }}</span>
            </button>
          </div>
          <label class="document-field">{{ t('documents.template.feedback') }}<textarea
            v-model="feedback"
            rows="3"
            maxlength="300"
            :disabled="busy"
          /></label>
          <button
            type="button"
            :disabled="busy"
            @click="preview"
          >
            {{ t(details.request.template ? 'documents.template.adjust' : 'documents.template.preview') }}
          </button>
          <button
            v-if="details.request.state === 'awaiting_confirmation'"
            type="button"
            class="document-primary"
            :disabled="busy"
            @click="confirm"
          >
            {{ t('documents.template.confirm') }}
          </button>
        </template>

        <button
          v-if="details.request.state === 'confirmed' && !activeJob"
          type="button"
          class="document-primary"
          :disabled="busy"
          @click="generate"
        >
          {{ t('slides.generate') }}
        </button>

        <section
          v-if="details.job"
          class="slides-job"
          aria-live="polite"
        >
          <p>{{ t('slides.jobs.' + details.job.state) }}</p>
          <progress
            :value="details.job.progress"
            max="100"
            :aria-label="t('slides.generationProgress')"
          />
          <p
            v-if="details.job.state === 'failed'"
            class="document-error"
          >
            {{ t(documentErrorKey(new DocumentError(details.job.error_code))) }}
          </p>
          <p
            v-if="activeJob"
            class="document-muted"
          >
            {{ t('slides.backgroundNote') }}
          </p>
          <button
            v-if="activeJob"
            type="button"
            :disabled="busy || details.job.cancel_requested"
            @click="cancel"
          >
            {{ t(details.job.cancel_requested ? 'slides.cancelling' : 'slides.cancel') }}
          </button>
          <button
            type="button"
            :disabled="busy"
            @click="run(refresh)"
          >
            {{ t('documents.refresh') }}
          </button>
        </section>
      </section>
    </template>
  </main>
</template>
