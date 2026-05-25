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
        :pagination="{ pageSize: 20 }"
        :row-key="(row) => row.document_id"
        size="small"
      />
    </div>

    <!-- Upload Modal -->
    <n-modal v-model:show="showUpload" preset="card" :title="t('documents.uploadModal.title')" style="width:480px" @after-enter="loadKbOptions">
      <n-form ref="uploadFormRef" :model="uploadForm" :rules="uploadRules">
        <n-form-item :label="t('documents.uploadModal.file')" path="file">
          <n-upload
            :max="1"
            accept=".pdf,.docx,.pptx,.md,.markdown"
            :default-upload="false"
            @change="onFileChange"
          >
            <n-upload-dragger>
              <n-icon size="32"><upload-icon /></n-icon>
              <n-text>{{ t('documents.uploadModal.dragHint') }}</n-text>
              <n-text depth="3" style="font-size:12px">{{ t('documents.uploadModal.supportedFormats') }}</n-text>
            </n-upload-dragger>
          </n-upload>
        </n-form-item>
        <n-form-item :label="t('documents.uploadModal.knowledgeBase')" path="knowledgeBaseId">
          <div style="width:100%;display:flex;flex-direction:column;gap:6px">
            <n-select
              v-model:value="uploadForm.knowledgeBaseId"
              :options="kbOptions"
              :placeholder="t('documents.uploadModal.selectKb')"
              style="width:100%"
            />
            <div v-if="currentUploadKbConfig" style="display:flex;gap:10px;flex-wrap:wrap;align-items:center">
              <n-text depth="3" style="font-size:12px">
                dim <n-tag size="tiny" :bordered="false">{{ currentUploadKbConfig.dim }}</n-tag>
              </n-text>
              <n-text depth="3" style="font-size:12px">
                analyzer <n-tag size="tiny" :bordered="false">{{ currentUploadKbConfig.analyzer || 'chinese' }}</n-tag>
              </n-text>
              <n-text depth="3" style="font-size:12px">
                chunk <n-tag size="tiny" :bordered="false">{{ currentUploadKbConfig.chunk_size }}</n-tag>
                overlap <n-tag size="tiny" :bordered="false">{{ currentUploadKbConfig.chunk_overlap }}</n-tag>
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
          <n-switch v-model:value="uploadForm.humanReview" />
          <n-text depth="3" style="font-size:12px;margin-left:8px">{{ t('documents.uploadModal.humanReviewHint') }}</n-text>
        </n-form-item>
      </n-form>
      <template #footer>
        <div style="display:flex;gap:8px;justify-content:flex-end">
          <n-button @click="showUpload = false">{{ t('documents.uploadModal.cancel') }}</n-button>
          <n-button type="primary" :loading="uploading" @click="handleUpload">{{ t('documents.upload') }}</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted } from 'vue'
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

const documents = ref([])
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
    lastRefreshed.value = t('documents.updatedAt', { time: new Date().toLocaleTimeString() })
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

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
