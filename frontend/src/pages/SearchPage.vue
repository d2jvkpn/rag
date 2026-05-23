<template>
  <div class="page-layout">
    <div class="page-body" style="padding:16px;max-width:960px">
      <!-- Query form -->
      <n-card style="margin-bottom:16px">
        <n-space vertical size="medium">
          <div style="display:flex;gap:12px;align-items:center;flex-wrap:wrap">
            <n-select
              v-model:value="knowledgeBaseId"
              :options="kbOptions"
              placeholder="知识库（留空则搜索全部）"
              clearable
              filterable
              style="width:240px;flex-shrink:0"
            />
            <n-select
              v-model:value="topK"
              :options="topKOptions"
              style="width:100px;flex-shrink:0"
            />
          </div>
          <n-input
            v-model:value="queryText"
            type="textarea"
            placeholder="输入查询内容… (Ctrl+Enter 搜索)"
            :autosize="{ minRows: 2, maxRows: 6 }"
            @keydown.ctrl.enter="handleSearch"
            @keydown.meta.enter="handleSearch"
          />
          <div style="display:flex;align-items:center;gap:12px">
            <n-button type="primary" :loading="loading" :disabled="!queryText.trim()" @click="handleSearch">
              搜索
            </n-button>
            <n-text v-if="searched && !loading" depth="3" style="font-size:12px">
              {{ results.length > 0 ? `返回 ${results.length} 条结果` : '未找到相关内容' }}
            </n-text>
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
      <div v-if="searched && results.length === 0 && !loading && !error">
        <n-empty description="未找到相关内容，请尝试不同查询词或确认文档已完成入库" />
      </div>

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
                {{ (item.score * 100).toFixed(1) }}%
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
import { SOURCE_TYPE_LABEL } from '../utils/status.js'

const router = useRouter()

const knowledgeBaseId = ref(null)
const queryText = ref('')
const topK = ref(5)
const loading = ref(false)
const error = ref('')
const results = ref([])
const answer = ref('')
const searched = ref(false)
const kbOptions = ref([])

const topKOptions = [
  { label: 'Top 5', value: 5 },
  { label: 'Top 10', value: 10 },
  { label: 'Top 20', value: 20 },
]

async function loadKnowledgeBases() {
  try {
    const resp = await searchService.listKnowledgeBases()
    kbOptions.value = (resp?.items || []).map(kb => ({
      label: `${kb.knowledge_base_id} (${kb.document_count})`,
      value: kb.knowledge_base_id,
    }))
  } catch { /* non-critical */ }
}

async function handleSearch() {
  if (!queryText.value.trim()) return
  loading.value = true
  error.value = ''
  answer.value = ''
  searched.value = false
  try {
    const resp = await searchService.query(knowledgeBaseId.value || '', queryText.value.trim(), topK.value)
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
