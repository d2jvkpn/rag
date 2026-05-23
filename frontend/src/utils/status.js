export const STATUS_LABEL = {
  uploaded: '已上传',
  processing: '处理中',
  review_pending: '待审核',
  reviewing: '审核中',
  approved: '已审核',
  indexed: '已入库',
  failed: '失败',
}

export const STATUS_TYPE = {
  uploaded: 'info',
  processing: 'warning',
  review_pending: 'warning',
  reviewing: 'warning',
  approved: 'success',
  indexed: 'success',
  failed: 'error',
}

export const STAGE_LABEL = {
  upload: '上传',
  parse: '解析',
  chunk: '切分',
  embed: 'Embedding',
  index: '写入向量库',
  done: '完成',
  delete: '删除',
}

export const SOURCE_TYPE_LABEL = {
  pdf: 'PDF',
  docx: 'Word',
  pptx: 'PowerPoint',
  markdown: 'Markdown',
}

export const CHUNK_STATUS_LABEL = {
  draft: '草稿',
  approved: '已批准',
  rejected: '已拒绝',
}

export const CHUNK_STATUS_TYPE = {
  draft: 'default',
  approved: 'success',
  rejected: 'error',
}

export const TERMINAL_STATUSES = new Set(['indexed', 'failed'])

export function isTerminal(status) {
  return TERMINAL_STATUSES.has(status)
}
