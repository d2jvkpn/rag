<template>
  <div class="page-layout">
    <div class="page-body">
      <div class="page-nav">
        <n-button text size="small" @click="router.push('/documents')">{{ t('documentDetail.backToList') }}</n-button>
        <n-text depth="3" style="font-size:13px"> {{ t('documentDetail.breadcrumb') }}</n-text>
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
                <n-tag :type="STATUS_TYPE[doc.status]" size="small">{{ t(`status.${doc.status}`) || doc.status }}</n-tag>
                <n-text depth="3" style="font-size:12px">{{ t('documentDetail.stage') }}: {{ doc.stage }}</n-text>
              </n-space>
            </div>
            <n-space>
              <n-button @click="router.push(`/documents/${doc.document_id}/chunks`)">{{ t('documentDetail.viewChunks') }}</n-button>
              <n-button @click="handleRechunk" :loading="rechunking">{{ t('documentDetail.rechunk') }}</n-button>
              <n-button v-if="doc.human_review && doc.status === 'approved'" type="primary" @click="handleIndex" :loading="indexing">{{ t('documentDetail.triggerIndex') }}</n-button>
              <n-button type="error" @click="handleDelete">{{ t('documentDetail.delete') }}</n-button>
            </n-space>
          </div>

          <!-- Error message -->
          <n-alert v-if="doc.error_message" type="error" style="margin-bottom:16px">
            {{ doc.error_message }}
          </n-alert>

          <!-- Details -->
          <n-card style="margin-bottom:16px">
            <n-descriptions :column="2" bordered label-placement="left" size="small">
              <n-descriptions-item :label="t('documentDetail.fields.id')">
                <n-text code style="font-size:12px">{{ doc.document_id }}</n-text>
              </n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.knowledgeBase')">{{ doc.knowledge_base_id }}</n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.filename')">{{ doc.filename }}</n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.type')">{{ t(`sourceType.${doc.source_type}`) || doc.source_type }}</n-descriptions-item>
              <n-descriptions-item label="SHA256">
                <n-text code style="font-size:11px">{{ doc.sha256 }}</n-text>
              </n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.pageCount')">{{ doc.page_count || '—' }}</n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.chunkCount')">{{ doc.chunk_count || '—' }}</n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.chunkVersion')">{{ doc.chunk_version || '—' }}</n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.tags')" :span="2">
                <n-space v-if="doc.tags?.length" size="small">
                  <n-tag v-for="tag in doc.tags" :key="tag" size="small" bordered>{{ tag }}</n-tag>
                </n-space>
                <n-text v-else depth="3">—</n-text>
              </n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.createdAt')"><span :title="rfc3339(doc.created_at)">{{ formatDate(doc.created_at) }}</span></n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.updatedAt')"><span :title="rfc3339(doc.updated_at)">{{ formatDate(doc.updated_at) }}</span></n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.startedAt')"><span :title="rfc3339(doc.started_at)">{{ formatDate(doc.started_at) }}</span></n-descriptions-item>
              <n-descriptions-item :label="t('documentDetail.fields.finishedAt')"><span :title="rfc3339(doc.finished_at)">{{ formatDate(doc.finished_at) }}</span></n-descriptions-item>
            </n-descriptions>
          </n-card>

          <!-- Processing indicator -->
          <n-alert v-if="isPolling" type="info">
            {{ t('documentDetail.polling', { interval: pollIntervalMs / 1000 }) }}
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
import { STATUS_TYPE, isTerminal } from '../utils/status.js'
import { useFormat } from '../utils/format.js'
import { useI18n } from '../i18n/index.js'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const { t } = useI18n()
const { formatDate, rfc3339 } = useFormat()

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
    title: t('documentDetail.rechunkDialog.title'),
    content: t('documentDetail.rechunkDialog.content'),
    positiveText: t('documentDetail.rechunkDialog.confirm'),
    negativeText: t('documentDetail.rechunkDialog.cancel'),
    onPositiveClick: async () => {
      rechunking.value = true
      try {
        await chunksService.rechunk(documentId)
        message.success(t('documentDetail.rechunkDialog.success'))
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
    title: t('documentDetail.indexDialog.title'),
    content: t('documentDetail.indexDialog.content'),
    positiveText: t('documentDetail.indexDialog.confirm'),
    negativeText: t('documentDetail.indexDialog.cancel'),
    onPositiveClick: async () => {
      indexing.value = true
      try {
        await chunksService.index(documentId)
        message.success(t('documentDetail.indexDialog.success'))
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
    title: t('documentDetail.deleteDialog.title'),
    content: t('documentDetail.deleteDialog.content', { name: doc.value?.title || doc.value?.filename }),
    positiveText: t('documentDetail.deleteDialog.confirm'),
    negativeText: t('documentDetail.deleteDialog.cancel'),
    onPositiveClick: async () => {
      try {
        await documentsService.delete(documentId)
        message.success(t('documentDetail.deleteDialog.deleted'))
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
