<script setup lang="ts">
import { watchEffect } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePreferencesStore } from './stores/preferences'

const preferences = usePreferencesStore()
const route = useRoute()
const { t, locale } = useI18n()

watchEffect(() => {
  locale.value = preferences.locale
  document.documentElement.lang = preferences.locale
  document.title = t('app.title')
})

function changeLocale(event: Event) {
  preferences.setLocale((event.target as HTMLSelectElement).value)
}
</script>

<template>
  <a
    class="skip-link"
    href="#main"
  >{{ t('app.skip') }}</a>
  <header class="site-header">
    <div class="header-inner">
      <RouterLink
        class="brand"
        to="/"
      >
        <svg
          class="brand-mark"
          viewBox="0 0 40 40"
          fill="none"
          aria-hidden="true"
        >
          <rect
            width="40"
            height="40"
            rx="12"
            fill="currentColor"
          />
          <path
            d="M11 13h8c4 0 7 3 7 7v10c-2-3-5-4-8-4h-7V13Z"
            stroke="#fff"
            stroke-width="2"
          />
          <path
            d="M29 10c-5 0-8 3-8 8 5 0 8-3 8-8Z"
            fill="#d9e6ad"
          />
        </svg>
        <span>{{ t('app.name') }}<small>{{ t('app.tagline') }}</small></span>
      </RouterLink>
      <nav
        class="header-actions"
        :aria-label="t('app.navigation')"
      >
        <RouterLink
          class="formats-link"
          :to="{ name: 'home', hash: '#formats' }"
        >{{ t('app.formats') }}</RouterLink>
        <RouterLink
          class="model-settings-link"
          :to="{ name: 'model-settings' }"
        >{{ t('models.title') }}</RouterLink>
        <RouterLink class="formats-link" :to="{ name: 'slides' }">{{ t('slides.title') }}</RouterLink>
        <label class="language-control">
          <span>{{ t('language.label') }}</span>
          <select
            :value="preferences.locale"
            @change="changeLocale"
          >
            <option value="zh-TW">{{ t('language.zhTW') }}</option>
            <option value="en">{{ t('language.en') }}</option>
          </select>
        </label>
      </nav>
    </div>
  </header>
  <RouterView :key="route.path" />
  <footer class="site-footer">
    <span>{{ t('footer.note') }}</span>
    <span>{{ t('footer.milestone') }}</span>
  </footer>
</template>

