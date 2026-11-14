<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { documentErrorKey, slides, type DocumentInput, type DocumentSummary } from '../api/documents'
import { usePreferencesStore } from '../stores/preferences'

const { t } = useI18n()
const preferences = usePreferencesStore()
const router = useRouter()
const form = ref<DocumentInput>({ topic: '', audience: '', kind: 'presentation', slide_count: 6, language: preferences.locale })
const languageOverridden = ref(false)
watch(() => preferences.locale, (locale) => {
  if (!languageOverridden.value) form.value.language = locale
})
const list = ref<DocumentSummary[]>([])
const loading = ref(true)
const submitting = ref(false)
const listError = ref('')
const formError = ref('')
const controller = new AbortController()

async function load() {
  loading.value = true
  listError.value = ''
  try {
    list.value = await slides.list(controller.signal)
  } catch (error) {
    if (!controller.signal.aborted) listError.value = documentErrorKey(error)
  } finally {
    loading.value = false
  }
}

async function create() {
  if (submitting.value) return
  submitting.value = true
  formError.value = ''
  try {
    const result = await slides.create(form.value, controller.signal)
    await router.push({ name: 'slide-deck', params: { id: result.id } })
  } catch (error) {
    if (!controller.signal.aborted) formError.value = documentErrorKey(error)
  } finally {
    submitting.value = false
  }
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
    <header class="page-heading">
      <div>
        <p class="eyebrow">
          {{ t('app.workspace') }}
        </p>
        <h1>{{ t('slides.title') }}</h1>
        <p class="lede">
          {{ t('slides.lede') }}
        </p>
      </div>
    </header>
    <div class="document-columns">
      <section class="document-panel">
        <h2>{{ t('slides.create') }}</h2>
        <form
          class="document-fields"
          @submit.prevent="create"
        >
          <label class="document-field">
            {{ t('documents.create.topic') }}
            <input
              v-model="form.topic"
              maxlength="160"
              required
              :placeholder="t('documents.create.topicExample')"
            >
          </label>
          <label class="document-field">
            {{ t('documents.create.audience') }}
            <input
              v-model="form.audience"
              maxlength="100"
              :placeholder="t('documents.create.audienceExample')"
            >
          </label>
          <label class="document-field">
            {{ t('slides.count') }}
            <input
              v-model.number="form.slide_count"
              type="number"
              min="1"
              max="12"
              required
            >
          </label>
          <label class="document-field">
            {{ t('documents.create.language') }}
            <select
              v-model="form.language"
              @change="languageOverridden = true"
            >
              <option value="zh-TW">{{ t('language.zhTW') }}</option>
              <option value="en">{{ t('language.en') }}</option>
            </select>
          </label>
          <p class="document-muted">
            {{ t('documents.create.privacy') }}
          </p>
          <p
            v-if="formError"
            class="document-error"
            role="alert"
          >
            {{ t(formError) }}
          </p>
          <button
            class="document-primary"
            type="submit"
            :disabled="submitting"
          >
            {{ t(submitting ? 'documents.working' : 'slides.createSubmit') }}
          </button>
        </form>
      </section>

      <section class="document-panel">
        <h2>{{ t('slides.library') }}</h2>
        <p
          v-if="loading"
          role="status"
        >
          {{ t('documents.working') }}
        </p>
        <template v-else-if="listError">
          <p
            class="document-error"
            role="alert"
          >
            {{ t(listError) }}
          </p>
          <button
            type="button"
            @click="load"
          >
            {{ t('documents.refresh') }}
          </button>
        </template>
        <p
          v-else-if="!list.length"
          class="document-muted"
        >
          {{ t('slides.empty') }}
        </p>
        <ul
          v-else
          class="document-list"
        >
          <li
            v-for="item in list"
            :key="item.id"
          >
            <RouterLink :to="{ name: 'slide-deck', params: { id: item.id } }">
              <span>{{ item.title }}</span><small>{{ t('documents.states.' + item.state) }}</small>
            </RouterLink>
          </li>
        </ul>
      </section>
    </div>
  </main>
</template>
