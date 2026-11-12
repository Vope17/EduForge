import { createI18n } from 'vue-i18n'
import zhTW from './zh-TW.json'
import en from './en.json'

export type Locale = 'zh-TW' | 'en'

export const messages = { 'zh-TW': zhTW, en }

export function createAppI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh-TW',
    fallbackLocale: 'zh-TW',
    messages,
  })
}

