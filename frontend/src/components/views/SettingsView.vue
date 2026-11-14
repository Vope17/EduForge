<script setup lang="ts">
import { computed } from 'vue'
import { useWorkspace } from '../../lib/workspace.ts'
import { SETTINGS_TABS, type SettingsTab } from '../../lib/router.ts'
import type { IconName } from '../../lib/icons.ts'
import SubNav from '../SubNav.vue'
import Settings from '../Settings.vue'
import TemplateLibrary from '../TemplateLibrary.vue'

const { state, route, go, t } = useWorkspace()
const icons: Record<SettingsTab, IconName> = { providers: 'plug', templates: 'template' }
const counts = computed((): Partial<Record<SettingsTab, number>> => ({
  providers: (state.snapshot.provider || []).length,
  templates: (state.snapshot.template || []).filter(x => x.data.file).length,
}))
const tabs = computed(() => SETTINGS_TABS.map(key => ({ key, label: t('settingsTab_' + key), icon: icons[key], count: counts.value[key] })))
const tab = computed({ get: () => route.tab ?? 'providers', set: (value: string) => go({ name: 'settings', tab: value as SettingsTab }) })
</script>

<template>
  <section class="page-content">
    <div class="page-heading"><div><h1>{{ t('settings') }}</h1><p class="lede">{{ t('settingsIntro') }}</p></div></div>
    <SubNav v-model="tab" :tabs="tabs" />
    <div class="subpage" :key="tab">
      <Settings v-if="tab === 'providers'" section="providers" />
      <TemplateLibrary v-else-if="tab === 'templates'" />
    </div>
  </section>
</template>
