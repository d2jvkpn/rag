<template>
  <div class="page-layout">
    <div class="page-body">
      <!-- Toolbar -->
      <div class="toolbar">
        <n-button type="primary" @click="showUpload = true">上传文档</n-button>
        <n-input
          v-model:value="filters.knowledgeBaseId"
          placeholder="知识库 ID 筛选"
          clearable
          style="width:200px"
          @update:value="loadDocuments"
        />
        <n-select
          v-model:value="filters.statusFilter"
          placeholder="状态筛选"
          clearable
          :options="statusOptions"
          style="width:160px"
          @update:value="loadDocuments"
        />
        <n-button :loading="loading" @click="loadDocuments">刷新</n-button>
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
    <n-modal v-model:show="showUpload" preset="card" title="上传文档" style="width:480px" @after-enter="loadKbOptions">
      <n-form ref="uploadFormRef" :model="uploadForm" :rules="uploadRules">
        <n-form-item label="文件" path="file">
          <n-upload
            :max="1"
            accept=".pdf,.docx,.pptx,.md,.markdown"
            :default-upload="false"
            @change="onFileChange"
          >
            <n-upload-dragger>
              <n-icon size="32"><upload-icon /></n-icon>
              <n-text>点击或拖拽文件到此处</n-text>
              <n-text depth="3" style="font-size:12px">支持 PDF、Word、PowerPoint、Markdown</n-text>
            </n-upload-dragger>
          </n-upload>
        </n-form-item>
        <n-form-item label="知识库" path="knowledgeBaseId">
          <n-select
            v-model:value="uploadForm.knowledgeBaseId"
            :options="kbOptions"
            filterable
            tag
            placeholder="选择已有知识库或输入新 ID"
            style="width:100%"
          />
        </n-form-item>
        <n-form-item label="标题（可选）">
          <n-input v-model:value="uploadForm.title" placeholder="留空则使用文件名" />
        </n-form-item>
        <n-form-item label="标签（可选）">
          <n-dynamic-tags v-model:value="uploadForm.tags" />
        </n-form-item>
        <n-form-item label="人工审核">
          <n-switch v-model:value="uploadForm.humanReview" />
          <n-text depth="3" style="font-size:12px;margin-left:8px">开启后切分完成不自动入库，需人工审核 Chunk</n-text>
        </n-form-item>
      </n-form>
      <template #footer>
        <div style="display:flex;gap:8px;justify-content:flex-end">
          <n-button @click="showUpload = false">取消</n-button>
          <n-button type="primary" :loading="uploading" @click="handleUpload">上传</n-button>
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
import { STATUS_LABEL, STATUS_TYPE, SOURCE_TYPE_LABEL } from '../utils/status.js'
import { fromNow } from '../utils/format.js'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const filters = useDocumentFiltersStore()

const documents = ref([])
const loading = ref(false)
const error = ref('')
const lastRefreshed = ref('')
const showUpload = ref(false)
const uploading = ref(false)
const kbOptions = ref([])
const uploadFormRef = ref(null)
const selectedFile = ref(null)
const uploadForm = ref({ knowledgeBaseId: '', title: '', tags: [], humanReview: false })

const uploadRules = {
  knowledgeBaseId: { required: true, message: '请填写知识库 ID', trigger: 'blur' },
}

const statusOptions = Object.entries(STATUS_LABEL).map(([v, l]) => ({ value: v, label: l }))

const filteredDocuments = computed(() => {
  if (!filters.statusFilter) return documents.value
  return documents.value.filter(d => d.status === filters.statusFilter)
})

async function loadKbOptions() {
  try {
    const data = await searchService.listKnowledgeBases()
    kbOptions.value = (data?.items || []).map(kb => ({
      label: `${kb.knowledge_base_id} (${kb.document_count})`,
      value: kb.knowledge_base_id,
    }))
  } catch { /* non-critical */ }
}

async function loadDocuments() {
  loading.value = true
  error.value = ''
  try {
    const data = await documentsService.list(filters.knowledgeBaseId)
    documents.value = data.items || []
    lastRefreshed.value = '更新于 ' + new Date().toLocaleTimeString()
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
  if (!selectedFile.value) { message.warning('请选择文件'); return }

  uploading.value = true
  try {
    const fd = new FormData()
    fd.append('file', selectedFile.value)
    fd.append('knowledge_base_id', uploadForm.value.knowledgeBaseId)
    if (uploadForm.value.title) fd.append('title', uploadForm.value.title)
    uploadForm.value.tags.forEach(t => fd.append('tags', t))
    fd.append('human_review', String(uploadForm.value.humanReview))

    await documentsService.upload(fd)
    message.success('上传成功，正在处理')
    showUpload.value = false
    uploadForm.value = { knowledgeBaseId: '', title: '', tags: [], humanReview: false }
    selectedFile.value = null
    await loadDocuments()
  } catch (e) {
    message.error(e.message || '上传失败')
  } finally {
    uploading.value = false
  }
}

function handleDelete(doc) {
  dialog.warning({
    title: '删除文档',
    content: `确认删除「${doc.title || doc.filename}」？此操作不可撤销。`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await documentsService.delete(doc.document_id)
        message.success('已删除')
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
    message.success('重新切分已提交')
    await loadDocuments()
  } catch (e) {
    message.error(e.message)
  }
}


const columns = [
  {
    title: '文档',
    key: 'filename',
    width: 240,
    render: (row) => h(
      'a',
      { style: 'cursor:pointer;color:inherit', onClick: () => router.push(`/documents/${row.document_id}`) },
      h(NEllipsis, { style: 'max-width:220px' }, { default: () => row.title || row.filename })
    ),
  },
  {
    title: '类型',
    key: 'source_type',
    width: 90,
    render: (row) => h(NTag, { size: 'small', bordered: false }, { default: () => SOURCE_TYPE_LABEL[row.source_type] || row.source_type }),
  },
  {
    title: '知识库',
    key: 'knowledge_base_id',
    width: 140,
    render: (row) => h(NEllipsis, { style: 'max-width:130px' }, { default: () => row.knowledge_base_id }),
  },
  {
    title: '状态',
    key: 'status',
    width: 120,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        h(NTag, { size: 'small', type: STATUS_TYPE[row.status] || 'default' }, { default: () => STATUS_LABEL[row.status] || row.status }),
        row.error_message ? h(NText, { type: 'error', style: 'font-size:11px', depth: 3 }, { default: () => '!' }) : null,
      ],
    }),
  },
  {
    title: '阶段',
    key: 'stage',
    width: 80,
    render: (row) => h(NText, { depth: 3, style: 'font-size:12px' }, { default: () => row.stage }),
  },
  {
    title: 'Chunks',
    key: 'chunk_count',
    width: 70,
    render: (row) => row.chunk_count || '—',
  },
  {
    title: '更新',
    key: 'updated_at',
    width: 100,
    render: (row) => h(NText, { depth: 3, style: 'font-size:12px' }, { default: () => fromNow(row.updated_at) }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 200,
    render: (row) => h(NSpace, { size: 6 }, {
      default: () => [
        h(NButton, { size: 'tiny', onClick: () => router.push(`/documents/${row.document_id}`) }, { default: () => '详情' }),
        h(NButton, { size: 'tiny', onClick: () => router.push(`/documents/${row.document_id}/chunks`) }, { default: () => 'Chunks' }),
        h(NButton, { size: 'tiny', onClick: () => handleRechunk(row) }, { default: () => '重切分' }),
        h(NButton, { size: 'tiny', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删除' }),
      ],
    }),
  },
]

onMounted(() => { loadDocuments(); loadKbOptions() })
</script>
