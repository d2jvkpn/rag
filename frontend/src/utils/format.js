import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import zhCN from 'dayjs/locale/zh-cn'

dayjs.extend(relativeTime)
dayjs.locale(zhCN)

export function formatDate(ts) {
  if (!ts) return '—'
  return dayjs(ts).format('YYYY-MM-DD HH:mm:ss')
}

export function fromNow(ts) {
  if (!ts) return '—'
  return dayjs(ts).fromNow()
}
