import { http } from './http.js'

export const searchService = {
  query(knowledgeBaseId, query, topK = 5, { searchMode = '', documentIds = [], ef = 0, dropRatio = 0, rrfK = 0 } = {}) {
    const body = {
      knowledge_base_id: knowledgeBaseId,
      query,
      top_k: topK,
    }
    if (searchMode) body.search_mode = searchMode
    if (documentIds.length) body.document_ids = documentIds
    if (ef > 0) body.ef = ef
    if (dropRatio > 0) body.drop_ratio = dropRatio
    if (rrfK > 0) body.rrf_k = rrfK
    return http.post('/api/query', body)
  },
  listKnowledgeBases() {
    return http.get('/api/knowledge-bases')
  },
  listAvailableKnowledgeBases() {
    return http.get('/api/knowledge-bases/available')
  },
}
