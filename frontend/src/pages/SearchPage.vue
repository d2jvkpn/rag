<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { DocumentTextOutline as DocumentTextIcon, DownloadOutline as DownloadIcon, OpenOutline as OpenIcon } from '@vicons/ionicons5'
import { searchService } from '../services/search.js'
import { documentsService } from '../services/documents.js'
import { useI18n } from '../i18n/index.js'

const router = useRouter()
const { t } = useI18n()

const searchMode = ref('dense')
const knowledgeBaseId = ref(null)
const queryText = ref('')
const topK = ref(5)
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
const lastSearchSnapshot = ref(null)
const kbOptions = ref([])
const kbConfigs = ref({})
const docOptions = ref([])

const currentKbConfig = computed(() => kbConfigs.value[knowledgeBaseId.value] || null)
const canDownloadResults = computed(() => Boolean(searched.value && lastSearchSnapshot.value))

const searchCache = new Map()

function buildCacheKey(kbId, query, topK, mode, docIds, ef, dropRatio, rrfK) {
  return JSON.stringify({ kbId, query, topK, mode, docIds: [...docIds].sort(), ef, dropRatio, rrfK })
}

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

const docButtonLabel = computed(() => t('search.docCount', {
  selected: selectedDocIds.value.length,
  total: docOptions.value.length,
}))

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
  if (searchMode.value === 'dense') return (score * 100).toFixed(1) + '%'
  return score.toFixed(4)
}

function shortId(id) {
  if (!id) return ''
  return id.length > 12 ? `${id.slice(0, 8)}...${id.slice(-4)}` : id
}

function openDocument(documentId) {
  window.open(`/documents/${documentId}`, '_blank')
}

function selectedDocumentSnapshots() {
  const docMap = new Map(docOptions.value.map(doc => [doc.value, doc]))
  return selectedDocIds.value.map(id => {
    const doc = docMap.get(id)
    return {
      document_id: id,
      filename: doc?.label || '',
      source_type: doc?.sourceType || '',
      uploader_name: doc?.uploaderName || '',
    }
  })
}

function buildSearchSnapshot(query, items, aiAnswer) {
  return {
    generated_at: new Date().toISOString(),
    site: window.location.href,
    query: {
      knowledge_base_id: knowledgeBaseId.value || '',
      text: query,
      top_k: topK.value,
      mode: searchMode.value,
      document_ids: [...selectedDocIds.value],
      documents: selectedDocumentSnapshots(),
      advanced: {
        ef: ef.value,
        drop_ratio: dropRatio.value,
        rrf_k: rrfK.value,
      },
      knowledge_base_config: currentKbConfig.value ? { ...currentKbConfig.value } : null,
    },
    output: {
      answer: aiAnswer || '',
      result_count: items.length,
      results: items.map((item, idx) => ({
        rank: idx + 1,
        score: item.score,
        knowledge_base_id: item.knowledge_base_id,
        document_id: item.document_id,
        chunk_id: item.chunk_id,
        chunk_index: item.chunk_index,
        filename: item.filename,
        source_type: item.source_type,
        section_title: item.section_title || '',
        page_start: item.page_start || 0,
        page_end: item.page_end || 0,
        text: item.text || '',
      })),
    },
  }
}

function yamlScalar(value, indent) {
  if (value === null || value === undefined) return 'null'
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  const str = String(value)
  if (!str) return "''"
  if (str.includes('\n')) {
    const pad = ' '.repeat(indent + 2)
    return `|\n${str.split('\n').map(line => `${pad}${line}`).join('\n')}`
  }
  if (/^[A-Za-z0-9_./@-]+$/.test(str)) return str
  return `'${str.replace(/'/g, "''")}'`
}

function isEmptyCollection(value) {
  return value && typeof value === 'object' && Object.keys(value).length === 0
}

function toYaml(value, indent = 0) {
  const pad = ' '.repeat(indent)
  if (Array.isArray(value)) {
    if (!value.length) return '[]'
    return value.map(item => {
      if (item && typeof item === 'object') {
        const nested = toYaml(item, indent + 2)
        return `${pad}- ${nested.trimStart()}`
      }
      return `${pad}- ${yamlScalar(item, indent)}`
    }).join('\n')
  }
  if (value && typeof value === 'object') {
    const entries = Object.entries(value)
    if (!entries.length) return '{}'
    return entries.map(([key, item]) => {
      if (Array.isArray(item) && !item.length) {
        return `${pad}${key}: []`
      }
      if (item && typeof item === 'object') {
        if (isEmptyCollection(item)) return `${pad}${key}: {}`
        return `${pad}${key}:\n${toYaml(item, indent + 2)}`
      }
      return `${pad}${key}: ${yamlScalar(item, indent)}`
    }).join('\n')
  }
  return `${pad}${yamlScalar(value, indent)}`
}

function pad2(value) {
  return String(value).padStart(2, '0')
}

function formatDownloadFilename(generatedAt) {
  const date = new Date(generatedAt)
  const yyyy = date.getFullYear()
  const dd = pad2(date.getDate())
  const mm = pad2(date.getMonth() + 1)
  const timestamp = `${pad2(date.getHours())}${pad2(date.getMinutes())}${pad2(date.getSeconds())}`
  return `rag_query--test_01.${yyyy}-${dd}-${mm}-${timestamp}.yaml`
}

function downloadSearchResults() {
  if (!lastSearchSnapshot.value) return
  const yaml = `${toYaml(lastSearchSnapshot.value)}\n`
  const blob = new Blob([yaml], { type: 'application/x-yaml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = formatDownloadFilename(lastSearchSnapshot.value.generated_at)
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function handleSearchModeChange(nextMode) {
  searchMode.value = nextMode
  error.value = ''
  const key = buildCacheKey(
    knowledgeBaseId.value, queryText.value.trim(), topK.value,
    nextMode, selectedDocIds.value, ef.value, dropRatio.value, rrfK.value,
  )
  const cached = searchCache.get(key)
  if (cached) {
    results.value = cached.results
    answer.value = cached.answer
    lastSearchSnapshot.value = cached.snapshot
    searched.value = true
  } else {
    results.value = []
    answer.value = ''
    searched.value = false
    lastSearchSnapshot.value = null
  }
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
  searchCache.clear()
  loadDocuments(val)
}

async function handleSearch() {
  if (!queryText.value.trim()) return
  const query = queryText.value.trim()
  const key = buildCacheKey(
    knowledgeBaseId.value, query, topK.value,
    searchMode.value, selectedDocIds.value, ef.value, dropRatio.value, rrfK.value,
  )
  const cached = searchCache.get(key)
  if (cached) {
    results.value = cached.results
    answer.value = cached.answer
    lastSearchSnapshot.value = cached.snapshot
    searched.value = true
    error.value = ''
    return
  }
  loading.value = true
  error.value = ''
  answer.value = ''
  searched.value = false
  try {
    const resp = await searchService.query(
      knowledgeBaseId.value || '',
      query,
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
    lastSearchSnapshot.value = buildSearchSnapshot(query, results.value, answer.value)
    searched.value = true
    searchCache.set(key, {
      results: results.value,
      answer: answer.value,
      snapshot: lastSearchSnapshot.value,
    })
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
            </n-button>
            <n-select
              v-model:value="topK"
              :options="topKOptions"
              style="width:96px;flex-shrink:0"
            />
            <n-radio-group :value="searchMode" size="small" @update:value="handleSearchModeChange">
              <n-radio-button value="dense">Dense</n-radio-button>
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

          <!-- Row 2: Textarea + Search/Download -->
          <div class="search-input-row">
            <n-input
              v-model:value="queryText"
              type="textarea"
              :placeholder="t('search.searchPlaceholder')"
              :autosize="{ minRows: 2, maxRows: 8 }"
              class="search-input"
              @keydown.ctrl.enter="handleSearch"
              @keydown.meta.enter="handleSearch"
            />
            <div class="search-actions">
              <n-button
                type="primary"
                :loading="loading"
                :disabled="!knowledgeBaseId || !queryText.trim()"
                class="search-button"
                @click="handleSearch"
              >
                {{ t('search.search') }}
              </n-button>
              <n-button
                size="small"
                secondary
                class="search-download-button"
                :disabled="!canDownloadResults"
                @click="downloadSearchResults"
              >
                <template #icon>
                  <n-icon><DownloadIcon /></n-icon>
                </template>
                {{ t('search.downloadResults') }}
              </n-button>
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
                <n-text style="font-size:13px;font-weight:600">{{ idx + 1 }}/{{ results.length }}. {{ item.filename }}</n-text>
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
          <pre class="search-result-text">{{ item.text }}</pre>
          <template #footer>
            <div class="search-result-footer">
              <div class="search-result-meta">
                <span class="meta-kv"><span class="meta-k">knowledge_base_id</span><span class="meta-sep">=</span><span class="meta-v">{{ item.knowledge_base_id }}</span></span>
                <span class="meta-kv"><span class="meta-k">chunk_id</span><span class="meta-sep">=</span><span class="meta-v">{{ item.chunk_id }}</span></span>
                <span class="meta-kv"><span class="meta-k">chunk_index</span><span class="meta-sep">=</span><span class="meta-v">{{ item.chunk_index }}</span></span>
              </div>
              <n-button secondary size="tiny" class="search-result-action" @click="openDocument(item.document_id)">
                <template #icon>
                  <n-icon><OpenIcon /></n-icon>
                </template>
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

<style scoped>
.search-input-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.search-input {
  flex: 1;
  min-width: 0;
}

.search-actions {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 6px;
  flex-shrink: 0;
}

.search-button {
  width: 72px;
}

.search-result-text {
  margin: 0;
  color: #2f3437;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.68;
  white-space: pre-wrap;
}

.search-download-button {
  --n-font-size: 12px;
  --n-padding: 0 10px;
  font-weight: 500;
}

.search-result-footer {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding-top: 2px;
}

.search-result-meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.meta-kv {
  display: flex;
  align-items: baseline;
  font-size: 11px;
  line-height: 1.5;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.meta-k {
  color: #a8adb8;
  font-weight: 400;
  flex-shrink: 0;
}

.meta-sep {
  color: #c8ccd4;
  margin: 0 2px;
  flex-shrink: 0;
}

.meta-v {
  color: #3d4146;
  font-weight: 500;
  word-break: break-all;
}

.search-result-action {
  --n-font-size: 12px;
  --n-padding: 0 10px;
  font-weight: 500;
  flex-shrink: 0;
}
</style>
