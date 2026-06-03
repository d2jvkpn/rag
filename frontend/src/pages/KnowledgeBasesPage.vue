
<script setup>
import { computed, h, onMounted, ref } from 'vue'
import { useMessage, useDialog, NButton, NTag, NText, NEllipsis } from 'naive-ui'
import { knowledgeBasesService } from '../services/knowledge-bases.js'
import { useAuthStore } from '../stores/auth.js'
import { useI18n } from '../i18n/index.js'
import { useFormat } from '../utils/format.js'

const { t } = useI18n()
const { rfc3339, fromNow } = useFormat()
const message = useMessage()
const dialog = useDialog()
const auth = useAuthStore()

const loading = ref(false)
const creating = ref(false)
const error = ref('')
const lastRefreshed = ref('')
const showCreate = ref(false)
const showDetail = ref(false)
const selectedKb = ref(null)
const knowledgeBases = ref([])
const formRef = ref(null)
const defaultDim = ref(1536)
const defaultModel = ref('')

const form = ref(defaultForm())
const canCreate = computed(() => auth.user?.permissions?.includes('manage_knowledge_bases'))
const canDelete = computed(() => auth.user?.permissions?.includes('manage_knowledge_bases'))

const analyzerOptions = computed(() => [
  { label: 'chinese', value: 'chinese' },
  { label: 'english', value: 'english' },
  { label: 'standard', value: 'standard' },
])

const rules = computed(() => ({
  knowledgeBaseId: [
    { required: true, message: t('knowledgeBases.rules.idRequired'), trigger: ['blur', 'input'] },
    {
      validator: (_, value) => /^[A-Za-z0-9_-]{1,63}$/.test(value || ''),
      message: t('knowledgeBases.rules.idFormat'),
      trigger: ['blur', 'input'],
    },
  ],
  analyzer: { required: true, message: t('knowledgeBases.rules.analyzerRequired'), trigger: 'change' },
  chunkSize: { type: 'number', required: true, min: 1, message: t('knowledgeBases.rules.positive'), trigger: ['blur', 'change'] },
  chunkOverlap: [
    { type: 'number', required: true, min: 0, message: t('knowledgeBases.rules.nonNegative'), trigger: ['blur', 'change'] },
    {
      validator: (_, value) => Number(value) < Number(form.value.chunkSize),
      message: t('knowledgeBases.rules.overlapLessThanSize'),
      trigger: ['blur', 'change'],
    },
  ],
  minChunks: { type: 'number', required: true, min: 1, message: t('knowledgeBases.rules.positive'), trigger: ['blur', 'change'] },
}))

const columns = computed(() => [
  {
    title: t('knowledgeBases.fields.id'),
    key: 'knowledge_base_id',
    width: 180,
    render: (row) => h(
      'a',
      { style: 'cursor:pointer', onClick: () => openDetail(row) },
      h(NEllipsis, { style: 'max-width:170px' }, { default: () => row.knowledge_base_id }),
    ),
  },
  {
    title: t('knowledgeBases.fields.documents'),
    key: 'document_count',
    width: 90,
    render: (row) => row.document_count || 0,
  },
  {
    title: t('knowledgeBases.fields.dim'),
    key: 'dim',
    width: 90,
    render: (row) => h(NTag, { size: 'small', bordered: false }, { default: () => row.dim }),
  },
  {
    title: t('knowledgeBases.fields.model'),
    key: 'model',
    width: 180,
    render: (row) => h(NEllipsis, { style: 'max-width:170px' }, { default: () => row.model || '-' }),
  },
  {
    title: t('knowledgeBases.fields.analyzer'),
    key: 'analyzer',
    width: 120,
    render: (row) => h(NTag, { size: 'small', bordered: false }, { default: () => row.analyzer || 'chinese' }),
  },
  {
    title: t('knowledgeBases.fields.chunking'),
    key: 'chunking',
    width: 240,
    render: (row) => h(NText, { depth: 3, style: 'font-size:12px' }, {
      default: () => `${row.chunk_size} / ${row.chunk_overlap} / ${row.min_chunks}`,
    }),
  },
  {
    title: t('knowledgeBases.fields.createdBy'),
    key: 'created_by',
    width: 120,
    render: (row) => row.created_by || '-',
  },
  {
    title: t('knowledgeBases.fields.createdAt'),
    key: 'created_at',
    width: 120,
    render: (row) => h('span', { title: rfc3339(row.created_at), style: 'font-size:12px;color:#999' }, fromNow(row.created_at)),
  },
  ...(canDelete.value ? [{
    title: t('knowledgeBases.fields.actions'),
    key: 'actions',
    width: 80,
    render: (row) => h(NButton, {
      size: 'tiny', type: 'error',
      onClick: () => handleDelete(row),
    }, { default: () => t('knowledgeBases.delete') }),
  }] : []),
])

function defaultForm() {
  return {
    knowledgeBaseId: '',
    analyzer: 'chinese',
    chunkSize: 800,
    chunkOverlap: 100,
    minChunks: 2,
  }
}

function openCreateModal() {
  form.value = defaultForm()
  formRef.value?.restoreValidation()
  showCreate.value = true
}

function openDetail(kb) {
  selectedKb.value = kb
  showDetail.value = true
}

async function loadKnowledgeBases() {
  loading.value = true
  error.value = ''
  try {
    const data = await knowledgeBasesService.list()
    knowledgeBases.value = data?.items || []
    defaultDim.value = data?.default_dim || knowledgeBases.value.find(kb => kb.dim)?.dim || defaultDim.value
    defaultModel.value = data?.default_model || knowledgeBases.value.find(kb => kb.model)?.model || defaultModel.value
    lastRefreshed.value = t('knowledgeBases.updatedAt', { time: new Date().toLocaleTimeString() })
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function handleCreate() {
  try { await formRef.value?.validate() } catch { return }
  creating.value = true
  try {
    const kb = await knowledgeBasesService.create({
      knowledge_base_id: form.value.knowledgeBaseId,
      analyzer: form.value.analyzer,
      chunk_size: form.value.chunkSize,
      chunk_overlap: form.value.chunkOverlap,
      min_chunks: form.value.minChunks,
    })
    defaultDim.value = kb?.dim || defaultDim.value
    message.success(t('knowledgeBases.modal.success'))
    showCreate.value = false
    await loadKnowledgeBases()
  } catch (e) {
    message.error(e.message || t('knowledgeBases.modal.failed'))
  } finally {
    creating.value = false
  }
}

function handleDelete(kb) {
  dialog.warning({
    title: t('knowledgeBases.deleteDialog.title'),
    content: t('knowledgeBases.deleteDialog.content', { id: kb.knowledge_base_id }),
    positiveText: t('knowledgeBases.deleteDialog.confirm'),
    negativeText: t('knowledgeBases.deleteDialog.cancel'),
    onPositiveClick: async () => {
      try {
        await knowledgeBasesService.delete(kb.knowledge_base_id)
        message.success(t('knowledgeBases.deleteDialog.deleted'))
        await loadKnowledgeBases()
      } catch (e) {
        message.error(e.message)
      }
    },
  })
}

onMounted(loadKnowledgeBases)
</script>


<template>
  <div class="page-layout">
    <div class="page-body">
      <div class="toolbar">
        <n-button v-if="canCreate" type="primary" @click="openCreateModal">{{ t('knowledgeBases.create') }}</n-button>
        <n-button :loading="loading" @click="loadKnowledgeBases">{{ t('knowledgeBases.refresh') }}</n-button>
        <n-text v-if="lastRefreshed" depth="3" style="font-size:12px">{{ lastRefreshed }}</n-text>
      </div>

      <n-alert v-if="error" type="error" style="margin-bottom:16px" closable @close="error = ''">
        {{ error }}
      </n-alert>

      <n-data-table
        :columns="columns"
        :data="knowledgeBases"
        :loading="loading"
        :pagination="false"
        :row-key="(row) => row.knowledge_base_id"
        :scroll-x="canDelete ? 1220 : 1140"
        size="small"
      />
    </div>

    <!-- Detail modal -->
    <n-modal
      v-model:show="showDetail"
      preset="card"
      :title="selectedKb?.knowledge_base_id"
      style="width:480px"
    >
      <n-descriptions
        v-if="selectedKb"
        label-placement="left"
        :column="1"
        label-style="width:140px;color:#999;font-size:13px"
        content-style="font-size:13px"
      >
        <n-descriptions-item :label="t('knowledgeBases.fields.id')">
          <n-text style="font-family:monospace">{{ selectedKb.knowledge_base_id }}</n-text>
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.documents')">
          {{ selectedKb.document_count || 0 }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.dim')">
          {{ selectedKb.dim }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.model')">
          {{ selectedKb.model || '-' }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.analyzer')">
          {{ selectedKb.analyzer || 'chinese' }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.chunkSize')">
          {{ selectedKb.chunk_size }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.chunkOverlap')">
          {{ selectedKb.chunk_overlap }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.minChunks')">
          {{ selectedKb.min_chunks }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.createdBy')">
          {{ selectedKb.created_by || '-' }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.createdAt')">
          {{ rfc3339(selectedKb.created_at) }}
        </n-descriptions-item>
        <n-descriptions-item :label="t('knowledgeBases.fields.updatedAt')">
          {{ rfc3339(selectedKb.updated_at) }}
        </n-descriptions-item>
      </n-descriptions>
    </n-modal>

    <!-- Create modal -->
    <n-modal v-model:show="showCreate" preset="card" :title="t('knowledgeBases.modal.title')" class="kb-modal" style="width:520px">
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="left" label-width="128px">
        <n-form-item :label="t('knowledgeBases.fields.id')" path="knowledgeBaseId">
          <n-input v-model:value="form.knowledgeBaseId" :placeholder="t('knowledgeBases.modal.idPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('knowledgeBases.fields.analyzer')" path="analyzer">
          <n-select v-model:value="form.analyzer" :options="analyzerOptions" />
        </n-form-item>
        <n-form-item :label="t('knowledgeBases.fields.dim')">
          <n-input-number :value="defaultDim" disabled style="width:100%" />
        </n-form-item>
        <n-form-item :label="t('knowledgeBases.fields.model')">
          <n-input :value="defaultModel" disabled style="width:100%" />
        </n-form-item>
        <n-form-item :label="t('knowledgeBases.fields.chunkSize')" path="chunkSize">
          <n-input-number v-model:value="form.chunkSize" :min="1" :step="100" style="width:100%" />
        </n-form-item>
        <n-form-item :label="t('knowledgeBases.fields.chunkOverlap')" path="chunkOverlap">
          <n-input-number v-model:value="form.chunkOverlap" :min="0" :step="25" style="width:100%" />
        </n-form-item>
        <n-form-item :label="t('knowledgeBases.fields.minChunks')" path="minChunks">
          <n-input-number v-model:value="form.minChunks" :min="1" :step="1" style="width:100%" />
        </n-form-item>
      </n-form>
      <template #footer>
        <div class="kb-modal__footer">
          <n-button @click="showCreate = false">{{ t('knowledgeBases.modal.cancel') }}</n-button>
          <n-button type="primary" :loading="creating" @click="handleCreate">{{ t('knowledgeBases.create') }}</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>


<style scoped>
.kb-modal__footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
