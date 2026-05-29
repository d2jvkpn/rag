
<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { searchService } from '../services/search.js'
import { documentsService } from '../services/documents.js'
import { useI18n } from '../i18n/index.js'

const router = useRouter()
const { t } = useI18n()

const knowledgeBaseId = ref(null)
const queryText = ref('')
const topK = ref(5)
const searchMode = ref('')
const selectedDocIds = ref([])
const showAdvanced = ref(false)
const ef = ref(0)
const dropRatio = ref(0)
const rrfK = ref(0)

const loading = ref(false)
const docsLoading = ref(false)
const error = ref('')
const results = ref([])
const answer = ref('')
const searched = ref(false)
const kbOptions = ref([])
const kbConfigs = ref({})
const docOptions = ref([])

const currentKbConfig = computed(() => kbConfigs.value[knowledgeBaseId.value] || null)

// Drawer state
const drawerVisible = ref(false)
const docSearchText = ref('')
const tempSelectedDocIds = ref([])

const filteredDocs = computed(() => {
  const q = docSearchText.value.toLowerCase()
  if (!q) return docOptions.value
  return docOptions.value.filter(d => d.label.toLowerCase().includes(q))
})

const allSelected = computed(() =>
  filteredDocs.value.length > 0 &&
  filteredDocs.value.every(d => tempSelectedDocIds.value.includes(d.value))
)

const someSelected = computed(() =>
  filteredDocs.value.some(d => tempSelectedDocIds.value.includes(d.value)) && !allSelected.value
)

const docButtonLabel = computed(() => {
  if (selectedDocIds.value.length === 0) return t('search.allDocs')
  return t('search.selectedDocs', { n: selectedDocIds.value.length })
})

function openDocDrawer() {
  tempSelectedDocIds.value = [...selectedDocIds.value]
  docSearchText.value = ''
  drawerVisible.value = true
}

function confirmDocSelection() {
  selectedDocIds.value = [...tempSelectedDocIds.value]
  drawerVisible.value = false
}

function resetDocSelection() {
  tempSelectedDocIds.value = []
}

function toggleSelectAll() {
  if (allSelected.value) {
    const filteredIds = new Set(filteredDocs.value.map(d => d.value))
    tempSelectedDocIds.value = tempSelectedDocIds.value.filter(id => !filteredIds.has(id))
  } else {
    const filteredIds = filteredDocs.value.map(d => d.value)
    const existing = new Set(tempSelectedDocIds.value)
    tempSelectedDocIds.value = [
      ...tempSelectedDocIds.value,
      ...filteredIds.filter(id => !existing.has(id)),
    ]
  }
}

const topKOptions = [
  { label: 'Top 5', value: 5 },
  { label: 'Top 10', value: 10 },
  { label: 'Top 20', value: 20 },
]

const efOptions = computed(() => [
  { label: t('search.efDefault'), value: 0 },
  { label: '64', value: 64 },
  { label: '128', value: 128 },
  { label: '256', value: 256 },
  { label: '512', value: 512 },
])

const dropRatioOptions = computed(() => [
  { label: t('search.dropRatioDefault'), value: 0 },
  { label: '0.1', value: 0.1 },
  { label: '0.2', value: 0.2 },
  { label: '0.3', value: 0.3 },
  { label: '0.5', value: 0.5 },
])

const rrfKOptions = computed(() => [
  { label: t('search.rrfKDefault'), value: 0 },
  { label: '20', value: 20 },
  { label: '60', value: 60 },
  { label: '100', value: 100 },
])

function formatScore(score) {
  if (!searchMode.value) return (score * 100).toFixed(1) + '%'
  return score.toFixed(4)
}

async function loadKnowledgeBases() {
  try {
    const resp = await searchService.listAvailableKnowledgeBases()
    const items = resp?.items || []
    kbOptions.value = items.map(kb => ({
      label: kb.knowledge_base_id,
      value: kb.knowledge_base_id,
    }))
    const map = {}
    for (const kb of items) map[kb.knowledge_base_id] = kb
    kbConfigs.value = map
  } catch { /* non-critical */ }
}

async function loadDocuments(kbId) {
  docOptions.value = []
  selectedDocIds.value = []
  if (!kbId) return
  docsLoading.value = true
  try {
    const resp = await documentsService.list(kbId)
    docOptions.value = (resp?.items || [])
      .filter(d => d.status === 'indexed')
      .map(d => ({
        label: d.filename,
        value: d.document_id,
        sourceType: d.source_type,
        uploaderName: d.uploader_name,
      }))
  } catch { /* non-critical */ } finally {
    docsLoading.value = false
  }
}

function onKbChange(val) {
  loadDocuments(val)
}

async function handleSearch() {
  if (!queryText.value.trim()) return
  loading.value = true
  error.value = ''
  answer.value = ''
  searched.value = false
  try {
    const resp = await searchService.query(
      knowledgeBaseId.value || '',
      queryText.value.trim(),
      topK.value,
      {
        searchMode: searchMode.value,
        documentIds: selectedDocIds.value,
        ef: ef.value,
        dropRatio: dropRatio.value,
        rrfK: rrfK.value,
      },
    )
    results.value = resp?.items || []
    answer.value = resp?.answer || ''
    searched.value = true
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

onMounted(loadKnowledgeBases)
</script>


<template>
  <div class="page-layout">
    <div class="page-body" style="padding:16px;max-width:960px">
      <n-card style="margin-bottom:16px">
        <n-space vertical size="medium">

          <!-- Row 1: KB + Doc filter + TopK + Mode -->
          <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
            <n-select
              v-model:value="knowledgeBaseId"
              :options="kbOptions"
              :placeholder="t('search.selectKb')"
              filterable
              style="width:200px;flex-shrink:0"
              @update:value="onKbChange"
            />
            <n-button
              :disabled="!knowledgeBaseId"
              :loading="docsLoading"
              size="small"
              style="flex-shrink:0"
              @click="openDocDrawer"
            >
              {{ docButtonLabel }}
              <template v-if="selectedDocIds.length > 0">
                <n-badge :value="selectedDocIds.length" style="margin-left:6px" />
              </template>
            </n-button>
            <n-select
              v-model:value="topK"
              :options="topKOptions"
              style="width:96px;flex-shrink:0"
            />
            <n-radio-group v-model:value="searchMode" size="small">
              <n-radio-button value="">Dense</n-radio-button>
              <n-radio-button value="bm25">BM25</n-radio-button>
              <n-radio-button value="hybrid">Hybrid</n-radio-button>
            </n-radio-group>
            <n-button
              text
              size="small"
              style="margin-left:auto;color:var(--n-text-color-disabled)"
              @click="showAdvanced = !showAdvanced"
            >
              {{ showAdvanced ? t('search.collapseParams') : t('search.advancedParams') }}
            </n-button>
          </div>

          <!-- KB config info -->
          <div
            v-if="currentKbConfig"
            style="display:flex;gap:12px;flex-wrap:wrap;align-items:center;padding:4px 2px 0"
          >
            <n-text depth="3" style="font-size:12px">
              dim <n-tag size="tiny" :bordered="false">{{ currentKbConfig.dim }}</n-tag>
            </n-text>
            <n-text depth="3" style="font-size:12px">
              analyzer <n-tag size="tiny" :bordered="false">{{ currentKbConfig.analyzer || 'chinese' }}</n-tag>
            </n-text>
            <n-text depth="3" style="font-size:12px">
              chunk <n-tag size="tiny" :bordered="false">{{ currentKbConfig.chunk_size }}</n-tag>
              overlap <n-tag size="tiny" :bordered="false">{{ currentKbConfig.chunk_overlap }}</n-tag>
            </n-text>
            <n-text depth="3" style="font-size:12px">
              min chunks <n-tag size="tiny" :bordered="false">{{ currentKbConfig.min_chunks }}</n-tag>
            </n-text>
          </div>

          <!-- Advanced params -->
          <div v-if="showAdvanced" style="display:flex;gap:16px;flex-wrap:wrap;align-items:flex-end;padding:8px 0 0;border-top:1px solid var(--n-border-color)">
            <n-form-item
              v-if="searchMode !== 'bm25'"
              :label="t('search.ef')"
              :show-feedback="false"
              style="margin-bottom:0;min-width:180px"
            >
              <n-select v-model:value="ef" :options="efOptions" style="width:140px" />
            </n-form-item>
            <n-form-item
              v-if="searchMode === 'bm25' || searchMode === 'hybrid'"
              :label="t('search.dropRatio')"
              :show-feedback="false"
              style="margin-bottom:0;min-width:180px"
            >
              <n-select v-model:value="dropRatio" :options="dropRatioOptions" style="width:140px" />
            </n-form-item>
            <n-form-item
              v-if="searchMode === 'hybrid'"
              :label="t('search.rrfK')"
              :show-feedback="false"
              style="margin-bottom:0;min-width:140px"
            >
              <n-select v-model:value="rrfK" :options="rrfKOptions" style="width:100px" />
            </n-form-item>
          </div>

          <!-- Row 2: Textarea + Button -->
          <div style="display:flex;gap:10px;align-items:flex-end">
            <n-input
              v-model:value="queryText"
              type="textarea"
              :placeholder="t('search.searchPlaceholder')"
              :autosize="{ minRows: 2, maxRows: 8 }"
              style="flex:1"
              @keydown.ctrl.enter="handleSearch"
              @keydown.meta.enter="handleSearch"
            />
            <div style="display:flex;flex-direction:column;align-items:flex-end;gap:6px;flex-shrink:0">
              <n-button
                type="primary"
                :loading="loading"
                :disabled="!knowledgeBaseId || !queryText.trim()"
                style="width:72px"
                @click="handleSearch"
              >
                {{ t('search.search') }}
              </n-button>
              <n-text v-if="searched && !loading" depth="3" style="font-size:11px;white-space:nowrap">
                {{ results.length > 0 ? t('search.results', { n: results.length }) : t('search.noResults') }}
              </n-text>
            </div>
          </div>

        </n-space>
      </n-card>

      <!-- Error -->
      <n-alert v-if="error" type="error" style="margin-bottom:12px" closable @close="error=''">{{ error }}</n-alert>

      <!-- LLM answer -->
      <n-card
        v-if="answer"
        style="margin-bottom:16px;background:#f8fbff;border-color:#c8dfff"
        size="small"
      >
        <template #header>
          <n-text style="font-size:13px;font-weight:600">{{ t('search.aiAnswer') }}</n-text>
        </template>
        <pre style="white-space:pre-wrap;font-size:14px;font-family:inherit;margin:0;line-height:1.7">{{ answer }}</pre>
      </n-card>

      <!-- No results -->
      <n-empty
        v-if="searched && results.length === 0 && !loading && !error"
        :description="t('search.emptyResult')"
      />

      <!-- Result cards -->
      <div v-for="(item, idx) in results" :key="item.chunk_id" style="margin-bottom:10px">
        <n-card size="small">
          <template #header>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:8px;flex-wrap:wrap">
              <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">
                <n-text style="font-size:13px;font-weight:600">{{ idx + 1 }}.</n-text>
                <n-text style="font-size:13px">{{ item.filename }}</n-text>
                <n-tag size="tiny">{{ t(`sourceType.${item.source_type}`) || item.source_type }}</n-tag>
                <n-text v-if="item.section_title" depth="3" style="font-size:12px">{{ item.section_title }}</n-text>
                <n-text v-if="item.page_start" depth="3" style="font-size:12px">
                  p{{ item.page_start }}<span v-if="item.page_end && item.page_end !== item.page_start">–{{ item.page_end }}</span>
                </n-text>
              </div>
              <n-tag type="success" size="small" style="flex-shrink:0">
                {{ formatScore(item.score) }}
              </n-tag>
            </div>
          </template>
          <pre style="white-space:pre-wrap;font-size:13px;font-family:inherit;margin:0;line-height:1.6">{{ item.text }}</pre>
          <template #footer>
            <div style="display:flex;align-items:center;gap:8px">
              <n-text depth="3" style="font-size:11px">{{ item.knowledge_base_id }}</n-text>
              <n-button text size="tiny" @click="router.push(`/documents/${item.document_id}`)">
                {{ t('search.viewDocument') }}
              </n-button>
            </div>
          </template>
        </n-card>
      </div>
    </div>
  </div>

  <!-- Document selection drawer -->
  <n-drawer v-model:show="drawerVisible" width="460" placement="right">
    <n-drawer-content :title="t('search.drawer.title')" closable>
      <template #default>
        <div style="display:flex;flex-direction:column;height:100%;gap:12px">
          <!-- Search input -->
          <n-input
            v-model:value="docSearchText"
            :placeholder="t('search.drawer.searchPlaceholder')"
            clearable
            size="small"
          />

          <!-- Select all -->
          <div style="display:flex;align-items:center;justify-content:space-between;padding-bottom:8px;border-bottom:1px solid var(--n-border-color)">
            <n-checkbox
              :checked="allSelected"
              :indeterminate="someSelected"
              @update:checked="toggleSelectAll"
            >
              {{ t('search.drawer.selectAll', { n: filteredDocs.length }) }}
            </n-checkbox>
            <n-text depth="3" style="font-size:12px">
              {{ t('search.drawer.selectedCount', { selected: tempSelectedDocIds.length, total: docOptions.length }) }}
            </n-text>
          </div>

          <!-- Document list -->
          <div style="flex:1;overflow-y:auto">
            <n-empty
              v-if="filteredDocs.length === 0"
              :description="t('search.drawer.empty')"
              style="padding:32px 0"
            />
            <n-checkbox-group v-model:value="tempSelectedDocIds">
              <div
                v-for="doc in filteredDocs"
                :key="doc.value"
                style="display:flex;align-items:center;gap:8px;padding:7px 4px;border-bottom:1px solid var(--n-border-color)"
              >
                <n-checkbox :value="doc.value" style="flex-shrink:0" />
                <n-text style="flex:1;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" :title="doc.label">
                  {{ doc.label }}
                </n-text>
                <n-tag size="tiny" style="flex-shrink:0">{{ t(`sourceType.${doc.sourceType}`) || doc.sourceType }}</n-tag>
                <n-text v-if="doc.uploaderName" depth="3" style="font-size:11px;flex-shrink:0;max-width:72px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">
                  {{ doc.uploaderName }}
                </n-text>
              </div>
            </n-checkbox-group>
          </div>
        </div>
      </template>

      <template #footer>
        <div style="display:flex;gap:8px;justify-content:flex-end">
          <n-button size="small" @click="resetDocSelection">{{ t('search.drawer.reset') }}</n-button>
          <n-button type="primary" size="small" @click="confirmDocSelection">{{ t('search.drawer.confirm') }}</n-button>
        </div>
      </template>
    </n-drawer-content>
  </n-drawer>
</template>
