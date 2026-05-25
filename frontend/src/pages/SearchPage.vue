<template>
  <div class="page-layout">
    <div class="page-body" style="padding:16px;max-width:960px">
      <n-card style="margin-bottom:16px">
        <n-space vertical size="medium">

          <!-- Row 1: KB + TopK + Mode -->
          <div style="display:flex;gap:10px;align-items:center;flex-wrap:wrap">
            <n-select
              v-model:value="knowledgeBaseId"
              :options="kbOptions"
              placeholder="知识库（留空搜全部）"
              clearable
              filterable
              style="width:200px;flex-shrink:0"
              @update:value="onKbChange"
            />
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
              {{ showAdvanced ? '收起参数 ▲' : '高级参数 ▼' }}
            </n-button>
          </div>

          <!-- Advanced params -->
          <div v-if="showAdvanced" style="display:flex;gap:16px;flex-wrap:wrap;align-items:flex-end;padding:8px 0 0;border-top:1px solid var(--n-border-color)">
            <n-form-item
              v-if="searchMode !== 'bm25'"
              label="EF（HNSW 搜索精度）"
              :show-feedback="false"
              style="margin-bottom:0;min-width:180px"
            >
              <n-select v-model:value="ef" :options="efOptions" style="width:140px" />
            </n-form-item>
            <n-form-item
              v-if="searchMode === 'bm25' || searchMode === 'hybrid'"
              label="Drop Ratio（BM25 剪枝）"
              :show-feedback="false"
              style="margin-bottom:0;min-width:180px"
            >
              <n-select v-model:value="dropRatio" :options="dropRatioOptions" style="width:140px" />
            </n-form-item>
            <n-form-item
              v-if="searchMode === 'hybrid'"
              label="RRF K"
              :show-feedback="false"
              style="margin-bottom:0;min-width:140px"
            >
              <n-select v-model:value="rrfK" :options="rrfKOptions" style="width:100px" />
            </n-form-item>
          </div>

          <!-- Row 2: Document filter -->
          <n-select
            v-if="knowledgeBaseId"
            v-model:value="selectedDocIds"
            :options="docOptions"
            :loading="docsLoading"
            multiple
            clearable
            filterable
            placeholder="筛选文档（留空则搜全部已入库文档）"
            max-tag-count="responsive"
          />

          <!-- Row 3: Textarea + Button -->
          <div style="display:flex;gap:10px;align-items:flex-end">
            <n-input
              v-model:value="queryText"
              type="textarea"
              placeholder="输入查询内容… (Ctrl+Enter 搜索)"
              :autosize="{ minRows: 2, maxRows: 8 }"
              style="flex:1"
              @keydown.ctrl.enter="handleSearch"
              @keydown.meta.enter="handleSearch"
            />
            <div style="display:flex;flex-direction:column;align-items:flex-end;gap:6px;flex-shrink:0">
              <n-button
                type="primary"
                :loading="loading"
                :disabled="!queryText.trim()"
                style="width:72px"
                @click="handleSearch"
              >
                搜索
              </n-button>
              <n-text v-if="searched && !loading" depth="3" style="font-size:11px;white-space:nowrap">
                {{ results.length > 0 ? `${results.length} 条` : '无结果' }}
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
          <n-text style="font-size:13px;font-weight:600">AI 回答</n-text>
        </template>
        <pre style="white-space:pre-wrap;font-size:14px;font-family:inherit;margin:0;line-height:1.7">{{ answer }}</pre>
      </n-card>

      <!-- No results -->
      <n-empty
        v-if="searched && results.length === 0 && !loading && !error"
        description="未找到相关内容，请尝试不同查询词或确认文档已完成入库"
      />

      <!-- Result cards -->
      <div v-for="(item, idx) in results" :key="item.chunk_id" style="margin-bottom:10px">
        <n-card size="small">
          <template #header>
            <div style="display:flex;align-items:center;justify-content:space-between;gap:8px;flex-wrap:wrap">
              <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap">
                <n-text style="font-size:13px;font-weight:600">{{ idx + 1 }}.</n-text>
                <n-text style="font-size:13px">{{ item.filename }}</n-text>
                <n-tag size="tiny">{{ SOURCE_TYPE_LABEL[item.source_type] || item.source_type }}</n-tag>
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
                查看文档
              </n-button>
            </div>
          </template>
        </n-card>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { searchService } from '../services/search.js'
import { documentsService } from '../services/documents.js'
import { SOURCE_TYPE_LABEL } from '../utils/status.js'

const router = useRouter()

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
const docOptions = ref([])

const topKOptions = [
  { label: 'Top 5', value: 5 },
  { label: 'Top 10', value: 10 },
  { label: 'Top 20', value: 20 },
]

const efOptions = [
  { label: '默认', value: 0 },
  { label: '64', value: 64 },
  { label: '128', value: 128 },
  { label: '256', value: 256 },
  { label: '512', value: 512 },
]

const dropRatioOptions = [
  { label: '默认 (0)', value: 0 },
  { label: '0.1', value: 0.1 },
  { label: '0.2', value: 0.2 },
  { label: '0.3', value: 0.3 },
  { label: '0.5', value: 0.5 },
]

const rrfKOptions = [
  { label: '默认 (60)', value: 0 },
  { label: '20', value: 20 },
  { label: '60', value: 60 },
  { label: '100', value: 100 },
]

function formatScore(score) {
  if (!searchMode.value) return (score * 100).toFixed(1) + '%'
  return score.toFixed(4)
}

async function loadKnowledgeBases() {
  try {
    const resp = await searchService.listAvailableKnowledgeBases()
    kbOptions.value = (resp?.items || []).map(kb => ({
      label: kb.knowledge_base_id,
      value: kb.knowledge_base_id,
    }))
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
      .map(d => ({ label: d.filename, value: d.document_id }))
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
