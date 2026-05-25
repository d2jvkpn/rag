import { computed } from 'vue'
import { useLocaleStore } from '../stores/locale.js'
import zh from './zh.js'
import en from './en.js'

const messages = { zh, en }

function resolve(obj, path) {
  return path.split('.').reduce((o, k) => o?.[k], obj)
}

function interpolate(str, params) {
  if (!params || typeof str !== 'string') return str
  return Object.entries(params).reduce(
    (s, [k, v]) => s.replaceAll(`{${k}}`, v),
    str,
  )
}

export function useI18n() {
  const localeStore = useLocaleStore()

  function t(key, params) {
    const msg = resolve(messages[localeStore.locale] ?? messages.zh, key)
      ?? resolve(messages.zh, key)
      ?? key
    return interpolate(msg, params)
  }

  const locale = computed(() => localeStore.locale)

  function setLocale(lang) {
    localeStore.setLocale(lang)
  }

  return { t, locale, setLocale }
}
