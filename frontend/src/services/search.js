import { http } from './http.js'

export const searchService = {
  query(knowledgeBaseId, query, topK = 5) {
    return http.post('/api/query', {
      knowledge_base_id: knowledgeBaseId,
      query,
      top_k: topK,
    })
  },
  listKnowledgeBases() {
    return http.get('/api/knowledge-bases')
  },
  listAvailableKnowledgeBases() {
    return http.get('/api/knowledge-bases/available')
  },
}
