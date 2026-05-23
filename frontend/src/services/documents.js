import { http } from './http.js'

export const documentsService = {
  list(knowledgeBaseId) {
    const qs = knowledgeBaseId ? `?knowledge_base_id=${encodeURIComponent(knowledgeBaseId)}` : ''
    return http.get('/api/documents' + qs)
  },
  get: (id) => http.get(`/api/documents/${id}`),
  upload: (formData) => http.postForm('/api/documents', formData),
  delete: (id) => http.delete(`/api/documents/${id}`),
}
