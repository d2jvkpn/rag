<template>
  <div class="page-layout">
    <div class="page-body">
      <!-- Toolbar -->
      <div class="toolbar">
        <n-button type="primary" @click="showUpload = true">{{ t('documents.upload') }}</n-button>
        <n-select
          v-model:value="filters.knowledgeBaseId"
          :options="kbOptions"
          :placeholder="t('documents.kbFilter')"
          clearable
          style="width:200px"
          @update:value="loadDocuments"
        />
        <n-select
          v-model:value="filters.statusFilter"
          :placeholder="t('documents.statusFilter')"
          clearable
          :options="statusOptions"
          style="width:160px"
          @update:value="loadDocuments"
        />
        <n-button :loading="loading" @click="loadDocuments">{{ t('documents.refresh') }}</n-button>
        <n-text v-if="lastRefreshed" depth="3" style="font-size:12px">{{ lastRefreshed }}</n-text>
      </div>

      <!-- Error -->
      <n-alert v-if="error" type="error" style="margin-bottom:16px" closable @close="error = ''">
        {{ error }}
      </n-alert>

      <!-- Table -->
      <n-data-table
        :columns="columns"
        :data="filteredDocuments"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row) => row.document_id"
        size="small"
      />
    </div>

    <!-- Upload Modal -->
    <n-modal v-model:show="showUpload" preset="card" :title="t('documents.uploadModal.title')" class="upload-modal" style="width:560px" @after-enter="loadKbOptions">
      <n-form ref="uploadFormRef" :model="uploadForm" :rules="uploadRules">
        <n-form-item :label="t('documents.uploadModal.file')" path="file">
          <n-upload
            :max="1"
            accept=".pdf,.docx,.pptx,.md,.markdown"
            :default-upload="false"
            @change="onFileChange"
          >
            <n-upload-dragger class="upload-modal__dragger">
              <n-icon size="32"><upload-icon /></n-icon>
              <div class="upload-modal__dragger-copy">
                <n-text class="upload-modal__dragger-title">{{ t('documents.uploadModal.dragHint') }}</n-text>
                <n-text depth="3" class="upload-modal__dragger-subtitle">{{ t('documents.uploadModal.supportedFormats') }}</n-text>
              </div>
            </n-upload-dragger>
          </n-upload>
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.knowledgeBase')" path="knowledgeBaseId">
          <div class="upload-modal__kb">
            <n-select
              v-model:value="uploadForm.knowledgeBaseId"
              :options="kbOptions"
              :placeholder="t('documents.uploadModal.selectKb')"
              style="width:100%"
            />
            <div v-if="currentUploadKbConfig" class="upload-modal__kb-meta">
              <n-text depth="3" class="upload-modal__kb-meta-item">
                dim <n-tag size="tiny" :bordered="false" round>{{ currentUploadKbConfig.dim }}</n-tag>
              </n-text>
              <n-text depth="3" class="upload-modal__kb-meta-item">
                analyzer <n-tag size="tiny" :bordered="false" round>{{ currentUploadKbConfig.analyzer || 'chinese' }}</n-tag>
              </n-text>
              <n-text depth="3" class="upload-modal__kb-meta-item">
                chunk <n-tag size="tiny" :bordered="false" round>{{ currentUploadKbConfig.chunk_size }}</n-tag>
                overlap <n-tag size="tiny" :bordered="false" round>{{ currentUploadKbConfig.chunk_overlap }}</n-tag>
              </n-text>
              <n-text depth="3" class="upload-modal__kb-meta-item upload-modal__kb-meta-item--full">
                min chunks <n-tag size="tiny" :bordered="false" round>{{ currentUploadKbConfig.min_chunks }}</n-tag>
              </n-text>
            </div>
          </div>
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.titleLabel')">
          <n-input v-model:value="uploadForm.title" :placeholder="t('documents.uploadModal.titlePlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.tags')">
          <n-dynamic-tags v-model:value="uploadForm.tags" />
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.humanReview')">
          <div class="upload-modal__review">
            <n-switch v-model:value="uploadForm.humanReview" />
            <n-text depth="3" class="upload-modal__review-hint">{{ t('documents.uploadModal.humanReviewHint') }}</n-text>
          </div>
        </n-form-item>
      </n-form>
      <template #footer>
        <div class="upload-modal__footer">
          <n-button @click="showUpload = false">{{ t('documents.uploadModal.cancel') }}</n-button>
          <n-button type="primary" :loading="uploading" @click="handleUpload">{{ t('documents.upload') }}</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage, useDialog, NButton, NTag, NSpace, NText, NEllipsis } from 'naive-ui'
import { CloudUploadOutline as UploadIcon } from '@vicons/ionicons5'
import { useDocumentFiltersStore } from '../stores/document-filters.js'
import { documentsService } from '../services/documents.js'
import { chunksService } from '../services/chunks.js'
import { searchService } from '../services/search.js'
import { STATUS_TYPE } from '../utils/status.js'
import { useFormat } from '../utils/format.js'
import { useI18n } from '../i18n/index.js'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const filters = useDocumentFiltersStore()
const { t } = useI18n()
const { fromNow, rfc3339 } = useFormat()
const pageSize = 20

const documents = ref([])
const currentPage = ref(1)
const loading = ref(false)
const error = ref('')
const lastRefreshed = ref('')
const showUpload = ref(false)
const uploading = ref(false)
const kbOptions = ref([])
const kbConfigs = ref({})
const uploadFormRef = ref(null)
const selectedFile = ref(null)
const uploadForm = ref({ knowledgeBaseId: '', title: '', tags: [], humanReview: true })

const uploadRules = computed(() => ({
  knowledgeBaseId: { required: true, message: t('documents.uploadModal.kbRequired'), trigger: 'blur' },
}))

const statusOptions = computed(() =>
  Object.keys(STATUS_TYPE).map(v => ({ value: v, label: t(`status.${v}`) }))
)

const filteredDocuments = computed(() => {
  if (!filters.statusFilter) return documents.value
  return documents.value.filter(d => d.status === filters.statusFilter)
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredDocuments.value.length / pageSize)))
const displayPage = computed(() => Math.min(currentPage.value, totalPages.value))
const pageStart = computed(() => filteredDocuments.value.length === 0 ? 0 : (displayPage.value - 1) * pageSize + 1)
const pageEnd = computed(() => filteredDocuments.value.length === 0 ? 0 : Math.min(displayPage.value * pageSize, filteredDocuments.value.length))
const pagination = computed(() => ({
  page: displayPage.value,
  pageSize,
  prefix: () => t('documents.pageSummary', { page: displayPage.value, pages: totalPages.value, start: pageStart.value, end: pageEnd.value, total: filteredDocuments.value.length }),
  onUpdatePage: (page) => { currentPage.value = page },
}))

const currentUploadKbConfig = computed(() => kbConfigs.value[uploadForm.value.knowledgeBaseId] || null)

async function loadKbOptions() {
  try {
    const data = await searchService.listAvailableKnowledgeBases()
    const items = data?.items || []
    kbOptions.value = items.map(kb => ({ label: kb.knowledge_base_id, value: kb.knowledge_base_id }))
    const map = {}
    for (const kb of items) map[kb.knowledge_base_id] = kb
    kbConfigs.value = map
  } catch { /* non-critical */ }
}

async function loadDocuments() {
  loading.value = true
  error.value = ''
  try {
    const data = await documentsService.list(filters.knowledgeBaseId)
    documents.value = data.items || []
    currentPage.value = 1
    lastRefreshed.value = t('documents.updatedAt', { time: new Date().toLocaleTimeString() })
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(totalPages, (pages) => {
  if (currentPage.value > pages) currentPage.value = pages
})

function onFileChange({ fileList }) {
  selectedFile.value = fileList[0]?.file || null
}

async function handleUpload() {
  try { await uploadFormRef.value?.validate() } catch { return }
  if (!selectedFile.value) { message.warning(t('documents.uploadModal.fileRequired')); return }

  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', selectedFile.value)
    fd.append('knowledge_base_id', uploadForm.value.knowledgeBaseId)
    if (uploadForm.value.title) fd.append('title', uploadForm.value.title)
    uploadForm.value.tags.forEach(tag => fd.append('tags', tag))
    fd.append('human_review', String(uploadForm.value.humanReview))

    await documentsService.upload(fd)
    message.success(t('documents.uploadModal.success'))
    showUpload.value = false
    uploadForm.value = { knowledgeBaseId: '', title: '', tags: [], humanReview: true }
    selectedFile.value = null
    await loadDocuments()
  } catch (e) {
    message.error(e.message || t('documents.uploadModal.title'))
  } finally {
    uploading.value = false
  }
}

async function handleRetry(doc) {
  try {
    await documentsService.index(doc.document_id)
    message.success(t('documents.retrySubmitted'))
    await loadDocuments()
  } catch (e) {
    message.error(e.message)
  }
}

function handleDelete(doc) {
  dialog.warning({
    title: t('documents.deleteDialog.title'),
    content: t('documents.deleteDialog.content', { name: doc.title || doc.filename }),
    positiveText: t('documents.deleteDialog.confirm'),
    negativeText: t('documents.deleteDialog.cancel'),
    onPositiveClick: async () => {
      try {
        await documentsService.delete(doc.document_id)
        message.success(t('documents.deleteDialog.deleted'))
        await loadDocuments()
      } catch (e) {
        message.error(e.message)
      }
    },
  })
}

async function handleRechunk(doc) {
  try {
    await chunksService.rechunk(doc.document_id)
    message.success(t('documents.rechunkSubmitted'))
    await loadDocuments()
  } catch (e) {
    message.error(e.message)
  }
}

const columns = computed(() => [
  {
    title: t('documents.table.filename'),
    key: 'filename',
    width: 240,
    render: (row) => h(
      'a',
      { style: 'cursor:pointer;color:inherit', onClick: () => router.push(`/documents/${row.document_id}`) },
      h(NEllipsis, { style: 'max-width:220px' }, { default: () => row.title || row.filename })
    ),
  },
  {
    title: t('documents.table.type'),
    key: 'source_type',
    width: 90,
    render: (row) => h(NTag, { size: 'small', bordered: false }, { default: () => t(`sourceType.${row.source_type}`) || row.source_type }),
  },
  {
    title: t('documents.table.knowledgeBase'),
    key: 'knowledge_base_id',
    width: 140,
    render: (row) => h(NEllipsis, { style: 'max-width:130px' }, { default: () => row.knowledge_base_id }),
  },
  {
    title: t('documents.table.status'),
    key: 'status',
    width: 120,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        h(NTag, { size: 'small', type: STATUS_TYPE[row.status] || 'default' }, { default: () => t(`status.${row.status}`) || row.status }),
        row.error_message ? h(NText, { type: 'error', style: 'font-size:11px', depth: 3 }, { default: () => '!' }) : null,
      ],
    }),
  },
  {
    title: t('documents.table.stage'),
    key: 'stage',
    width: 80,
    render: (row) => h(NText, { depth: 3, style: 'font-size:12px' }, { default: () => row.stage }),
  },
  {
    title: t('documents.table.chunks'),
    key: 'chunk_count',
    width: 70,
    render: (row) => row.chunk_count || '—',
  },
  {
    title: t('documents.table.uploader'),
    key: 'uploader_name',
    width: 90,
    render: (row) => h(NText, { depth: 3, style: 'font-size:12px' }, { default: () => row.uploader_name || '—' }),
  },
  {
    title: t('documents.table.updatedAt'),
    key: 'updated_at',
    width: 100,
    render: (row) => h('span', { title: rfc3339(row.updated_at), style: 'font-size:12px;color:#999' }, fromNow(row.updated_at)),
  },
  {
    title: t('documents.table.actions'),
    key: 'actions',
    width: 200,
    render: (row) => h(NSpace, { size: 6 }, {
      default: () => [
        h(NButton, { size: 'tiny', onClick: () => router.push(`/documents/${row.document_id}`) }, { default: () => t('documents.actions.detail') }),
        h(NButton, { size: 'tiny', onClick: () => router.push(`/documents/${row.document_id}/chunks`) }, { default: () => 'Chunks' }),
        row.status !== 'indexed' && h(NButton, { size: 'tiny', onClick: () => handleRechunk(row) }, { default: () => t('documents.actions.rechunk') }),
        row.status === 'failed' && h(NButton, { size: 'tiny', type: 'warning', onClick: () => handleRetry(row) }, { default: () => t('documents.actions.retry') }),
        h(NButton, { size: 'tiny', type: 'error', onClick: () => handleDelete(row) }, { default: () => t('documents.actions.delete') }),
      ],
    }),
  },
])

onMounted(() => { loadDocuments(); loadKbOptions() })
</script>

<style scoped>
.upload-modal :deep(.n-card-header) {
  padding-bottom: 8px;
}

.upload-modal :deep(.n-card__content) {
  padding-top: 4px;
}

.upload-modal__dragger :deep(.n-upload-dragger__content) {
  min-height: 132px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
}

.upload-modal__dragger-copy {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 4px;
  text-align: center;
}

.upload-modal__dragger-title {
  font-size: 15px;
}

.upload-modal__dragger-subtitle {
  font-size: 12px;
}

.upload-modal__kb {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.upload-modal__kb-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 12px;
  align-items: center;
}

.upload-modal__kb-meta-item {
  font-size: 12px;
}

.upload-modal__kb-meta-item :deep(.n-tag) {
  margin-left: 4px;
}

.upload-modal__kb-meta-item--full {
  flex-basis: 100%;
}

.upload-modal__review {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.upload-modal__review-hint {
  font-size: 12px;
  line-height: 1.5;
  max-width: 420px;
}

.upload-modal__footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
