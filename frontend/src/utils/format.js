import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'
import 'dayjs/locale/en'
import { useLocaleStore } from '../stores/locale.js'

dayjs.extend(relativeTime)

export function useFormat() {
  const localeStore = useLocaleStore()

  function dayjsLocale() {
    return localeStore.locale === 'zh' ? 'zh-cn' : 'en'
  }

  function fromNow(ts) {
    if (!ts) return '—'
    return dayjs(ts).locale(dayjsLocale()).fromNow()
  }

  function formatDate(ts) {
    if (!ts) return '—'
    return dayjs(ts).locale(dayjsLocale()).format('YYYY-MM-DD HH:mm:ss')
  }

  function rfc3339(ts) {
    if (!ts) return ''
    return dayjs(ts).toISOString()
  }

  return { fromNow, formatDate, rfc3339 }
}
