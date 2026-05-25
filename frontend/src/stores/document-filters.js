import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useDocumentFiltersStore = defineStore('documentFilters', () => {
  const knowledgeBaseId = ref(null)
  const statusFilter = ref(null)
  const tagFilter = ref(null)

  function reset() {
    knowledgeBaseId.value = null
    statusFilter.value = null
    tagFilter.value = null
  }

  return { knowledgeBaseId, statusFilter, tagFilter, reset }
})
