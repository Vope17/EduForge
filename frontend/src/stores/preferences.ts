import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Locale } from '../i18n'

export const usePreferencesStore = defineStore('preferences', () => {
  const locale = ref<Locale>('zh-TW')

  function setLocale(value: string) {
    if (value === 'zh-TW' || value === 'en') locale.value = value
  }

  return { locale, setLocale }
})

