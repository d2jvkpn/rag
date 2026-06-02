
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
const total = ref(0)
const totalPagesValue = ref(1)
const loading = ref(false)
const error = ref('')
const lastRefreshed = ref('')
const showUpload = ref(false)
const uploading = ref(false)
const kbOptions = ref([])
const tagOptions = ref([])
const uploadFormRef = ref(null)
const selectedFile = ref(null)
const uploadForm = ref({ knowledgeBaseId: '', title: '', tags: [] })

const uploadRules = computed(() => ({
  knowledgeBaseId: { required: true, message: t('documents.uploadModal.kbRequired'), trigger: ['blur', 'change'] },
}))

const statusOptions = computed(() =>
  Object.keys(STATUS_TYPE).map(v => ({ value: v, label: t(`status.${v}`) }))
)

const totalPages = computed(() => Math.max(1, totalPagesValue.value))
const displayPage = computed(() => Math.min(currentPage.value, totalPages.value))
const pageStart = computed(() => total.value === 0 ? 0 : (displayPage.value - 1) * pageSize + 1)
const pageEnd = computed(() => total.value === 0 ? 0 : pageStart.value + documents.value.length - 1)

async function loadKbOptions() {
  try {
    const data = await searchService.listAvailableKnowledgeBases()
    const items = data?.items || []
    kbOptions.value = items.map(kb => ({ label: kb.knowledge_base_id, value: kb.knowledge_base_id }))
  } catch { /* non-critical */ }
}

async function loadTagOptions() {
  try {
    const data = await documentsService.listTags(filters.knowledgeBaseId)
    const items = data?.items || []
    tagOptions.value = items.map(item => ({
      label: `${item.tag} (${item.count})`,
      value: item.tag,
    }))
    if (filters.tagFilter && !tagOptions.value.some(item => item.value === filters.tagFilter)) {
      filters.tagFilter = ''
      await loadDocuments()
    }
  } catch {
    tagOptions.value = []
  }
}

async function loadDocuments() {
  loading.value = true
  error.value = ''
  try {
    const data = await documentsService.list({
      knowledgeBaseId: filters.knowledgeBaseId,
      tag: filters.tagFilter,
      status: filters.statusFilter,
      page: currentPage.value,
      pageSize,
    })
    documents.value = data.items || []
    currentPage.value = data.page || currentPage.value
    total.value = data.total || 0
    totalPagesValue.value = data.total_pages || 1
    lastRefreshed.value = t('documents.updatedAt', { time: new Date().toLocaleTimeString() })
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function loadFirstPage() {
  currentPage.value = 1
  await loadDocuments()
}

async function goToPage(page) {
  currentPage.value = page
  await loadDocuments()
}


watch(() => filters.knowledgeBaseId, async () => {
  await loadTagOptions()
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
    fd.append('human_review', 'true')
    if (uploadForm.value.title) fd.append('title', uploadForm.value.title)
    uploadForm.value.tags.forEach(tag => fd.append('tags', tag))
    await documentsService.upload(fd)
    message.success(t('documents.uploadModal.success'))
    showUpload.value = false
    uploadForm.value = { knowledgeBaseId: '', title: '', tags: [] }
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
    key: 'file_type',
    width: 90,
    render: (row) => h(NTag, { size: 'small', bordered: false }, { default: () => row.file_type }),
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
    title: t('documents.table.tags'),
    key: 'tags',
    width: 180,
    render: (row) => {
      const tags = Array.isArray(row.tags) ? row.tags : []
      if (tags.length === 0) return '—'
      return h(NSpace, { size: 4, wrapItem: true }, () =>
        tags.map(tag => h(NTag, { size: 'small', bordered: false }, { default: () => tag }))
      )
    },
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

onMounted(async () => {
  await loadDocuments()
  await loadKbOptions()
  await loadTagOptions()
})
</script>

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
          @update:value="loadFirstPage"
        />
        <n-select
          v-model:value="filters.statusFilter"
          :placeholder="t('documents.statusFilter')"
          clearable
          :options="statusOptions"
          style="width:160px"
          @update:value="loadFirstPage"
        />
        <n-select
          v-model:value="filters.tagFilter"
          :options="tagOptions"
          :placeholder="t('documents.tagFilter')"
          clearable
          filterable
          style="width:220px"
          @update:value="loadFirstPage"
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
        :data="documents"
        :loading="loading"
        :pagination="false"
        :row-key="(row) => row.document_id"
        :scroll-x="1310"
        size="small"
      />
      <div style="display:flex;justify-content:flex-end;align-items:center;gap:12px;margin-top:12px">
        <n-text depth="3" style="font-size:12px">
          {{ t('documents.pageSummary', { page: displayPage, pages: totalPages, start: pageStart, end: pageEnd, total }) }}
        </n-text>
        <n-button size="small" :disabled="displayPage <= 1" @click="goToPage(displayPage - 1)">Prev</n-button>
        <n-button size="small" :disabled="displayPage >= totalPages" @click="goToPage(displayPage + 1)">Next</n-button>
      </div>
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
          <n-select
            v-model:value="uploadForm.knowledgeBaseId"
            :options="kbOptions"
            :placeholder="t('documents.uploadModal.selectKb')"
            style="width:100%"
          />
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.titleLabel')">
          <n-input v-model:value="uploadForm.title" :placeholder="t('documents.uploadModal.titlePlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.tags')">
          <n-dynamic-tags v-model:value="uploadForm.tags" />
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
