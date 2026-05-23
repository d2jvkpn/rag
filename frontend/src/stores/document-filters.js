import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useDocumentFiltersStore = defineStore('documentFilters', () => {
  const knowledgeBaseId = ref('')
  const statusFilter = ref('')

  function reset() {
    knowledgeBaseId.value = ''
    statusFilter.value = ''
  }

  return { knowledgeBaseId, statusFilter, reset }
})
