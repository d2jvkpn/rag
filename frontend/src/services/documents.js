import { http } from './http.js'

export const documentsService = {
  list({ knowledgeBaseId, tag, status, page = 1, pageSize = 20 } = {}) {
    const params = new URLSearchParams()
    if (knowledgeBaseId) params.set('knowledge_base_id', knowledgeBaseId)
    if (tag) params.set('tag', tag)
    if (status) params.set('status', status)
    params.set('page', String(page))
    params.set('page_size', String(pageSize))
    return http.get(`/api/documents?${params.toString()}`)
  },
  listTags(knowledgeBaseId) {
    const qs = knowledgeBaseId ? `?knowledge_base_id=${encodeURIComponent(knowledgeBaseId)}` : ''
    return http.get('/api/document-tags' + qs)
  },
  get: (id) => http.get(`/api/documents/${id}`),
  upload: (formData) => http.postForm('/api/documents', formData),
  delete: (id) => http.delete(`/api/documents/${id}`),
  index: (id) => http.post(`/api/documents/${id}/index`),
}
