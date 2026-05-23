import { http } from './http.js'

export const chunksService = {
  list: (documentId) => http.get(`/api/documents/${documentId}/chunks`),
  rechunk: (documentId) => http.post(`/api/documents/${documentId}/chunks/rechunk`),
  approve: (documentId) => http.post(`/api/documents/${documentId}/chunks/approve`),
  reject: (documentId, chunkId) => http.post(`/api/documents/${documentId}/chunks/${chunkId}/reject`),
  merge: (documentId, chunkIds) => http.post(`/api/documents/${documentId}/chunks/merge`, { chunk_ids: chunkIds }),
  index: (documentId) => http.post(`/api/documents/${documentId}/index`),
}
