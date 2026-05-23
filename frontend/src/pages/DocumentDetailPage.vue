<template>
  <div class="page-layout">
    <div class="page-body">
      <div class="page-nav">
        <n-button text size="small" @click="router.push('/documents')">← 返回列表</n-button>
        <n-text depth="3" style="font-size:13px"> / 文档详情</n-text>
      </div>
      <n-spin :show="loading && !doc">
        <div v-if="error && !doc">
          <n-alert type="error">{{ error }}</n-alert>
        </div>

        <template v-if="doc">
          <!-- Title + actions -->
          <div style="display:flex;align-items:flex-start;justify-content:space-between;margin-bottom:16px;gap:12px">
            <div>
              <h2 style="font-size:18px;margin-bottom:4px">{{ doc.title || doc.filename }}</h2>
              <n-space size="small">
                <n-tag :type="STATUS_TYPE[doc.status]" size="small">{{ STATUS_LABEL[doc.status] || doc.status }}</n-tag>
                <n-text depth="3" style="font-size:12px">阶段: {{ doc.stage }}</n-text>
              </n-space>
            </div>
            <n-space>
              <n-button @click="router.push(`/documents/${doc.document_id}/chunks`)">查看 Chunks</n-button>
              <n-button @click="handleRechunk" :loading="rechunking">重新切分</n-button>
              <n-button v-if="doc.human_review && doc.status === 'approved'" type="primary" @click="handleIndex" :loading="indexing">触发入库</n-button>
              <n-button type="error" @click="handleDelete">删除</n-button>
            </n-space>
          </div>

          <!-- Error message -->
          <n-alert v-if="doc.error_message" type="error" style="margin-bottom:16px">
            {{ doc.error_message }}
          </n-alert>

          <!-- Details -->
          <n-card style="margin-bottom:16px">
            <n-descriptions :column="2" bordered label-placement="left" size="small">
              <n-descriptions-item label="文档 ID">
                <n-text code style="font-size:12px">{{ doc.document_id }}</n-text>
              </n-descriptions-item>
              <n-descriptions-item label="知识库">{{ doc.knowledge_base_id }}</n-descriptions-item>
              <n-descriptions-item label="文件名">{{ doc.filename }}</n-descriptions-item>
              <n-descriptions-item label="类型">{{ SOURCE_TYPE_LABEL[doc.source_type] || doc.source_type }}</n-descriptions-item>
              <n-descriptions-item label="SHA256">
                <n-text code style="font-size:11px">{{ doc.sha256 }}</n-text>
              </n-descriptions-item>
              <n-descriptions-item label="页数">{{ doc.page_count || '—' }}</n-descriptions-item>
              <n-descriptions-item label="Chunk 数">{{ doc.chunk_count || '—' }}</n-descriptions-item>
              <n-descriptions-item label="Chunk 版本">{{ doc.chunk_version || '—' }}</n-descriptions-item>
              <n-descriptions-item label="标签" :span="2">
                <n-space v-if="doc.tags?.length" size="small">
                  <n-tag v-for="t in doc.tags" :key="t" size="small" bordered>{{ t }}</n-tag>
                </n-space>
                <n-text v-else depth="3">—</n-text>
              </n-descriptions-item>
              <n-descriptions-item label="创建时间">{{ formatDate(doc.created_at) }}</n-descriptions-item>
              <n-descriptions-item label="更新时间">{{ formatDate(doc.updated_at) }}</n-descriptions-item>
              <n-descriptions-item label="开始处理">{{ formatDate(doc.started_at) }}</n-descriptions-item>
              <n-descriptions-item label="处理完成">{{ formatDate(doc.finished_at) }}</n-descriptions-item>
            </n-descriptions>
          </n-card>

          <!-- Processing indicator -->
          <n-alert v-if="isPolling" type="info">
            正在处理中，每 {{ pollIntervalMs / 1000 }} 秒自动刷新…
          </n-alert>
        </template>
      </n-spin>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMessage, useDialog } from 'naive-ui'
import { documentsService } from '../services/documents.js'
import { chunksService } from '../services/chunks.js'

import { getConfig } from '../config/app-config.js'
import { STATUS_LABEL, STATUS_TYPE, SOURCE_TYPE_LABEL, isTerminal } from '../utils/status.js'
import { formatDate } from '../utils/format.js'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()

const { pollIntervalMs } = getConfig()
const documentId = route.params.documentId

const doc = ref(null)
const loading = ref(false)
const error = ref('')
const rechunking = ref(false)
const indexing = ref(false)
let pollTimer = null

const isPolling = computed(() => {
  if (!doc.value || isTerminal(doc.value.status)) return false
  if (!doc.value.human_review) return true
  return doc.value.status !== 'review_pending' && doc.value.status !== 'approved'
})

async function loadDoc() {
  loading.value = true
  try {
    doc.value = await documentsService.get(documentId)
    error.value = ''
    schedulePoll()
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function schedulePoll() {
  clearTimeout(pollTimer)
  if (isPolling.value) {
    pollTimer = setTimeout(async () => {
      try {
        doc.value = await documentsService.get(documentId)
        if (!doc.value.human_review && doc.value.status === 'review_pending') {
          await autoApproveAndIndex()
          return
        }
        schedulePoll()
      } catch { /* silently ignore poll errors */ }
    }, pollIntervalMs)
  }
}

async function autoApproveAndIndex() {
  try {
    await chunksService.approve(documentId)
    await chunksService.index(documentId)
    doc.value = await documentsService.get(documentId)
  } catch (e) {
    console.error('auto approve/index failed:', e)
  }
  schedulePoll()
}

async function handleRechunk() {
  dialog.warning({
    title: '重新切分',
    content: '将忽略当前快照，重新生成 Chunk 版本，确认吗？',
    positiveText: '确认',
    negativeText: '取消',
    onPositiveClick: async () => {
      rechunking.value = true
      try {
        await chunksService.rechunk(documentId)
        message.success('重新切分已提交')
        await loadDoc()
      } catch (e) {
        message.error(e.message)
      } finally {
        rechunking.value = false
      }
    },
  })
}

function handleIndex() {
  dialog.warning({
    title: '触发入库',
    content: '将对已批准的 Chunk 执行 Embedding 并写入向量库，确认吗？',
    positiveText: '确认入库',
    negativeText: '取消',
    onPositiveClick: async () => {
      indexing.value = true
      try {
        await chunksService.index(documentId)
        message.success('入库任务已提交')
        await loadDoc()
      } catch (e) {
        message.error(e.message)
      } finally {
        indexing.value = false
      }
    },
  })
}

function handleDelete() {
  dialog.warning({
    title: '删除文档',
    content: `确认删除「${doc.value?.title || doc.value?.filename}」？此操作不可撤销。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await documentsService.delete(documentId)
        message.success('已删除')
        router.push('/documents')
      } catch (e) {
        message.error(e.message)
      }
    },
  })
}

onMounted(loadDoc)
onUnmounted(() => clearTimeout(pollTimer))
</script>
