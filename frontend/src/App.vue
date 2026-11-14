<script setup lang="ts">
import { computed, ref, watch, watchEffect } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { usePreferencesStore } from './stores/preferences'
import AppIcon from './components/AppIcon.vue'
import type { IconName } from './components/icons'

const preferences = usePreferencesStore()
const route = useRoute()
const { t, locale } = useI18n()
// The sidebar slides in over the page on narrow screens.
const navOpen = ref(false)

watchEffect(() => {
  locale.value = preferences.locale
  document.documentElement.lang = preferences.locale
  document.title = t('app.title')
})
watch(() => route.fullPath, () => { navOpen.value = false })

// Sidebar sections; `match` lists the routes that belong to each.
const sections: { name: string; label: string; icon: IconName; match: string[] }[] = [
  { name: 'home', label: 'app.home', icon: 'home', match: ['home'] },
  { name: 'documents', label: 'documents.title', icon: 'file', match: ['documents', 'document'] },
  { name: 'slides', label: 'slides.title', icon: 'template', match: ['slides', 'slide-deck'] },
]
const current = computed(() => sections.find((section) => section.match.includes(String(route.name))))
const crumbs = computed(() => {
  if (route.name === 'model-settings') return [t('models.title')]
  if (!current.value || current.value.name === 'home') return [t('app.name')]
  const list = [t(current.value.label)]
  if (route.name === 'document' || route.name === 'slide-deck') list.push(t('app.editing'))
  return list
})
</script>

<template>
  <a
    class="skip-link"
    href="#main"
  >{{ t('app.skip') }}</a>
  <div class="app-shell">
    <div
      v-if="navOpen"
      class="nav-backdrop"
      @click="navOpen = false"
    />
    <aside
      class="sidebar"
      :class="{ open: navOpen }"
    >
      <RouterLink
        class="brand"
        to="/"
      >
        <span
          class="brand-symbol"
          aria-hidden="true"
        >E<span>F</span></span>
        <span>Edu<span class="brand-light">Forge</span><small>{{ t('app.tagline') }}</small></span>
      </RouterLink>

      <p class="sidebar-label">
        {{ t('app.navigation') }}
      </p>
      <nav
        class="side-nav"
        :aria-label="t('app.navigation')"
      >
        <RouterLink
          v-for="section in sections"
          :key="section.name"
          class="side-link"
          :class="{ active: current?.name === section.name }"
          :to="{ name: section.name }"
        >
          <AppIcon :name="section.icon" />{{ t(section.label) }}
        </RouterLink>
      </nav>

      <div class="sidebar-bottom">
        <RouterLink
          class="side-link"
          :class="{ active: route.name === 'model-settings' }"
          :to="{ name: 'model-settings' }"
        >
          <AppIcon name="plug" />{{ t('models.title') }}
        </RouterLink>
        <div
          class="language-switch"
          role="group"
          :aria-label="t('language.label')"
        >
          <button
            type="button"
            :class="{ active: preferences.locale === 'zh-TW' }"
            :aria-pressed="preferences.locale === 'zh-TW'"
            @click="preferences.setLocale('zh-TW')"
          >
            繁中
          </button>
          <button
            type="button"
            :class="{ active: preferences.locale === 'en' }"
            :aria-pressed="preferences.locale === 'en'"
            @click="preferences.setLocale('en')"
          >
            EN
          </button>
        </div>
      </div>
    </aside>

    <div class="workspace-column">
      <header class="topbar">
        <button
          type="button"
          class="menu-button"
          :aria-label="t('app.menu')"
          :aria-expanded="navOpen"
          @click="navOpen = true"
        >
          <AppIcon name="menu" />
        </button>
        <nav
          class="crumbs"
          :aria-label="t('app.breadcrumb')"
        >
          <template
            v-for="(crumb, index) in crumbs"
            :key="index"
          >
            <span
              v-if="index"
              class="crumb-sep"
            >/</span>
            <span :class="{ 'crumb-current': index === crumbs.length - 1 }">{{ crumb }}</span>
          </template>
        </nav>
        <div class="topbar-actions">
          <RouterLink
            class="conn-chip"
            :to="{ name: 'model-settings' }"
          >
            <span class="conn-dot" /><span class="conn-label">{{ t('models.title') }}</span>
          </RouterLink>
        </div>
      </header>
      <RouterView :key="route.path" />
      <footer class="site-footer">
        <span>{{ t('footer.note') }}</span>
        <span>{{ t('footer.milestone') }}</span>
      </footer>
    </div>
  </div>
</template>
