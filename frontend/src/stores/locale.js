import { defineStore } from 'pinia'
import { ref } from 'vue'

const STORAGE_KEY = 'app_locale'

export const LOCALES = [
  { value: 'zh', label: '中文' },
  { value: 'en', label: 'English' },
]

const LOCALE_VALUES = new Set(LOCALES.map(l => l.value))

function detectBrowserLocale() {
  const lang = (navigator.language || '').toLowerCase()
  if (lang.startsWith('zh')) return 'zh'
  return 'en'
}

export const useLocaleStore = defineStore('locale', () => {
  const stored = localStorage.getItem(STORAGE_KEY)
  const locale = ref(LOCALE_VALUES.has(stored) ? stored : detectBrowserLocale())

  function setLocale(lang) {
    if (!LOCALE_VALUES.has(lang)) return
    locale.value = lang
    localStorage.setItem(STORAGE_KEY, lang)
  }

  return { locale, setLocale }
})
