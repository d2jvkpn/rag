import { http } from './http.js'

export const chunksService = {
  list: (documentId, params = {}) => {
    const search = new URLSearchParams()
    if (params.page) search.set('page', String(params.page))
    if (params.pageSize) search.set('page_size', String(params.pageSize))
    const query = search.toString()
    return http.get(`/api/documents/${documentId}/chunks${query ? `?${query}` : ''}`)
  },
  rechunk: (documentId) => http.post(`/api/documents/${documentId}/chunks/rechunk`),
  approve: (documentId) => http.post(`/api/documents/${documentId}/chunks/approve`),
  reject: (documentId, chunkId) => http.post(`/api/documents/${documentId}/chunks/${chunkId}/reject`),
  restore: (documentId, chunkId) => http.post(`/api/documents/${documentId}/chunks/${chunkId}/restore`),
  edit: (documentId, chunkId, text) => http.put(`/api/documents/${documentId}/chunks/${chunkId}`, { text }),
  merge: (documentId, chunkIds) => http.post(`/api/documents/${documentId}/chunks/merge`, { chunk_ids: chunkIds }),
  index: (documentId) => http.post(`/api/documents/${documentId}/index`),
}
