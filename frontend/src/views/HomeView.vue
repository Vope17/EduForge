<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { checkHealth } from '../api/health'
import AppIcon from '../components/AppIcon.vue'

const { t } = useI18n()
const health = ref<'checking' | 'online' | 'offline'>('checking')
const formats = ['document', 'slides', 'pdf', 'video'] as const
const steps = ['template', 'confirm', 'edit'] as const
let activeRequest: AbortController | undefined

async function refreshHealth() {
  activeRequest?.abort()
  const controller = new AbortController()
  activeRequest = controller
  health.value = 'checking'
  const timeout = window.setTimeout(() => controller.abort(), 5000)
  try {
    await checkHealth(controller.signal)
    if (activeRequest === controller) health.value = 'online'
  } catch {
    if (activeRequest === controller) health.value = 'offline'
  } finally {
    window.clearTimeout(timeout)
  }
}

onMounted(refreshHealth)
onBeforeUnmount(() => {
  activeRequest?.abort()
  activeRequest = undefined
})
</script>

<template>
  <main
    id="main"
    class="workspace"
    tabindex="-1"
  >
    <header
      class="page-heading"
      aria-labelledby="welcome-heading"
    >
      <div>
        <p class="eyebrow">
          {{ t('home.eyebrow') }}
        </p>
        <h1 id="welcome-heading">
          {{ t('home.heading') }}<span class="heading-accent">{{ t('home.headingAccent') }}</span>
        </h1>
        <p class="lede">
          {{ t('home.description') }}
        </p>
      </div>
      <div class="home-actions">
        <RouterLink
          class="secondary"
          :to="{ name: 'slides' }"
        >
          <AppIcon name="template" />{{ t('slides.title') }}
        </RouterLink>
        <RouterLink
          class="primary"
          :to="{ name: 'documents' }"
        >
          <AppIcon name="file" />{{ t('documents.title') }}
        </RouterLink>
      </div>
    </header>

    <section
      class="panel onboarding"
      aria-labelledby="workflow-heading"
    >
      <div class="section-heading">
        <h2 id="workflow-heading">
          {{ t('workflow.title') }}
        </h2>
        <span class="pill">{{ t('workflow.planned') }}</span>
      </div>
      <ol class="onboard-list">
        <li
          v-for="(step, index) in steps"
          :key="step"
        >
          <span
            class="onboard-mark"
            aria-hidden="true"
          >{{ index + 1 }}</span>
          <div>
            <strong>{{ t(`workflow.${step}.title`) }}</strong>
            <span class="caption">{{ t(`workflow.${step}.description`) }}</span>
          </div>
        </li>
      </ol>
      <p class="caption workflow-note">
        {{ t('workflow.note') }} {{ t('home.stage') }}
      </p>
    </section>

    <section
      id="formats"
      aria-labelledby="formats-heading"
    >
      <div class="section-heading">
        <div>
          <p class="eyebrow">
            {{ t('formats.eyebrow') }}
          </p>
          <h2 id="formats-heading">
            {{ t('formats.heading') }}
          </h2>
        </div>
        <span class="caption">{{ t('formats.caption') }}</span>
      </div>
      <div class="format-grid">
        <article
          v-for="(format, index) in formats"
          :key="format"
          class="format-card"
          :class="`format-${format}`"
        >
          <div class="format-topline">
            <span
              class="format-icon"
              aria-hidden="true"
            >{{ ['Aa', '▤', 'PDF', '▷'][index] }}</span>
            <span
              v-if="format !== 'document' && format !== 'slides'"
              class="pill"
            >{{ t('formats.planned') }}</span>
          </div>
          <h3>{{ t(`formats.${format}.title`) }}</h3>
          <p>{{ t(`formats.${format}.description`) }}</p>
          <div class="format-footnote">
            <RouterLink
              v-if="format === 'document'"
              :to="{ name: 'documents' }"
            >
              {{ t('formats.document.detail') }} →
            </RouterLink>
            <RouterLink
              v-else-if="format === 'slides'"
              :to="{ name: 'slides' }"
            >
              {{ t('formats.slides.detail') }} →
            </RouterLink>
            <template v-else>
              {{ t(`formats.${format}.detail`) }}
            </template>
          </div>
        </article>
      </div>
    </section>

    <section
      class="panel health"
      :aria-label="t('health.label')"
    >
      <div class="health-info">
        <span
          class="health-dot"
          :class="health"
          aria-hidden="true"
        />
        <div>
          <h2>{{ t('health.label') }}</h2>
          <p
            role="status"
            aria-live="polite"
          >
            {{ t(`health.${health}`) }}
          </p>
        </div>
      </div>
      <button
        type="button"
        data-health-retry
        :disabled="health === 'checking'"
        @click="refreshHealth"
      >
        {{ t('health.retry') }}<span aria-hidden="true">↻</span>
      </button>
    </section>
  </main>
</template>
