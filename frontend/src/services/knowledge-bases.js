import { http } from './http.js'

export const knowledgeBasesService = {
  list() {
    return http.get('/api/knowledge-bases')
  },
  create(payload) {
    return http.post('/api/knowledge-bases', payload)
  },
}
