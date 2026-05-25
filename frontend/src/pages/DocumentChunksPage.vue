<template>
  <div class="page-layout">
    <div class="page-body" style="padding:16px">
      <div class="page-nav" style="margin-bottom:12px">
        <div style="display:flex;align-items:center;gap:8px">
          <n-button text size="small" @click="router.push(`/documents/${documentId}`)">{{ t('chunks.backToDetail') }}</n-button>
          <n-text v-if="doc" depth="3" style="font-size:13px">{{ t('chunks.breadcrumb') }} {{ doc.title || doc.filename }}</n-text>
        </div>
        <n-space>
          <n-tag v-if="doc" :type="STATUS_TYPE[doc.status]" size="small">
            {{ t(`status.${doc.status}`) || doc.status }}
          </n-tag>
          <n-button v-if="doc?.human_review && (doc?.status === 'review_pending' || doc?.status === 'approved')" size="small" type="primary" :loading="approving" @click="handleApprove">
            {{ t('chunks.approve') }}
          </n-button>
          <n-button v-if="doc?.human_review && doc?.status === 'approved'" size="small" type="info" :loading="indexing" @click="handleIndex">
            {{ t('chunks.triggerIndex') }}
          </n-button>
          <n-button v-if="selectedIds.length >= 2" size="small" @click="handleMerge" :loading="merging">
            {{ t('chunks.mergeSelected', { n: selectedIds.length }) }}
          </n-button>
          <n-button v-if="doc?.status !== 'indexed'" size="small" :loading="rechunking" @click="handleRechunk">{{ t('chunks.rechunk') }}</n-button>
        </n-space>
      </div>
      <n-alert v-if="error" type="error" style="margin-bottom:12px" closable @close="error=''">{{ error }}</n-alert>

      <n-spin :show="loading && !chunks.length">
        <n-empty v-if="!loading && !chunks.length" :description="t('chunks.empty')" />

        <div v-else class="split-view">
          <!-- Left: chunk list -->
          <div class="split-view__left">
            <n-card size="small" style="height:100%">
              <template #header>
                <div style="display:flex;align-items:center;justify-content:space-between">
                  <n-text style="font-size:13px">{{ t('chunks.count', { n: chunks.length }) }}</n-text>
                  <n-button text size="tiny" @click="selectedIds = []" v-if="selectedIds.length">{{ t('chunks.clearSelection') }}</n-button>
                </div>
              </template>
              <n-scrollbar style="max-height:calc(100vh - 180px)">
                <div
                  v-for="chunk in chunks"
                  :key="chunk.chunk_id"
                  class="chunk-item"
                  :class="{
                    'chunk-item--active': selectedId === chunk.chunk_id,
                    'chunk-item--selected': selectedIds.includes(chunk.chunk_id),
                    'chunk-item--rejected': chunk.status === 'rejected',
                  }"
                  @click="toggleSelect(chunk)"
                >
                  <div class="chunk-item__header">
                    <n-space size="small" align="center">
                      <n-checkbox
                        :checked="selectedIds.includes(chunk.chunk_id)"
                        @update:checked="(v) => toggleCheckbox(chunk.chunk_id, v)"
                        @click.stop
                        size="small"
                      />
                      <n-text style="font-size:12px;font-weight:600">#{{ chunk.chunk_index + 1 }}</n-text>
                    </n-space>
                    <n-space size="small" align="center">
                      <n-tag size="tiny" :type="CHUNK_STATUS_TYPE[chunk.status] || 'default'">
                        {{ t(`chunkStatus.${chunk.status}`) || chunk.status }}
                      </n-tag>
                      <n-button
                        v-if="doc?.human_review && chunk.status !== 'rejected'"
                        text
                        size="tiny"
                        type="error"
                        :title="t('chunks.rejectTitle')"
                        @click.stop="handleReject(chunk)"
                      >✕</n-button>
                      <n-button
                        v-if="doc?.human_review && chunk.status === 'rejected'"
                        text
                        size="tiny"
                        type="success"
                        :title="t('chunks.restoreTitle')"
                        @click.stop="handleRestore(chunk)"
                      >↩</n-button>
                    </n-space>
                  </div>
                  <div v-if="chunk.section_title" class="chunk-item__section">{{ chunk.section_title }}</div>
                  <div v-if="chunk.page_start" class="chunk-item__pages">
                    p{{ chunk.page_start }}<span v-if="chunk.page_end && chunk.page_end !== chunk.page_start">–{{ chunk.page_end }}</span>
                  </div>
                  <n-text depth="3" style="font-size:12px;display:block;margin-top:4px">
                    {{ chunk.text.slice(0, 80) }}{{ chunk.text.length > 80 ? '…' : '' }}
                  </n-text>
                </div>
              </n-scrollbar>
            </n-card>
          </div>

          <!-- Right: chunk detail -->
          <div class="split-view__right">
            <n-card v-if="selectedChunk" size="small" style="height:100%">
              <template #header>
                <div style="display:flex;align-items:center;gap:8px;flex:1">
                  <n-text>Chunk #{{ selectedChunk.chunk_index + 1 }}</n-text>
                  <n-tag size="small" :type="CHUNK_STATUS_TYPE[selectedChunk.status] || 'default'">
                    {{ t(`chunkStatus.${selectedChunk.status}`) || selectedChunk.status }}
                  </n-tag>
                  <n-text v-if="selectedChunk.section_title" depth="3" style="font-size:12px">
                    {{ selectedChunk.section_title }}
                  </n-text>
                  <div style="margin-left:auto">
                    <template v-if="!editing">
                      <n-button v-if="selectedChunk.status !== 'rejected'" text size="tiny" @click="startEdit">{{ t('chunks.edit') }}</n-button>
                    </template>
                    <template v-else>
                      <n-button size="tiny" type="primary" :loading="saving" @click="saveEdit" style="margin-right:4px">{{ t('chunks.save') }}</n-button>
                      <n-button size="tiny" @click="cancelEdit">{{ t('chunks.cancel') }}</n-button>
                    </template>
                  </div>
                </div>
              </template>

              <n-scrollbar style="max-height:calc(100vh - 180px)">
                <div style="display:flex;gap:16px;margin-bottom:12px;font-size:12px;color:#666">
                  <span>{{ t('chunks.version') }}: <strong>v{{ selectedChunk.chunk_version }}</strong></span>
                  <span>{{ t('chunks.source') }}: <strong>{{ selectedChunk.source }}</strong></span>
                  <span v-if="selectedChunk.page_start">{{ t('chunks.page') }}: <strong>{{ selectedChunk.page_start }}{{ selectedChunk.page_end && selectedChunk.page_end !== selectedChunk.page_start ? '–' + selectedChunk.page_end : '' }}</strong></span>
                </div>

                <n-input
                  v-if="editing"
                  v-model:value="editText"
                  type="textarea"
                  :autosize="{ minRows: 6 }"
                  style="font-size:13px;margin-bottom:12px;font-family:inherit"
                />
                <n-card v-else size="small" style="background:#fafafa;margin-bottom:12px">
                  <pre style="white-space:pre-wrap;font-size:13px;font-family:inherit;margin:0">{{ selectedChunk.text }}</pre>
                </n-card>

                <template v-if="selectedChunk.normalized_text && selectedChunk.normalized_text !== selectedChunk.text">
                  <n-text style="font-size:12px;font-weight:600;display:block;margin-bottom:6px">{{ t('chunks.normalizedText') }}</n-text>
                  <n-card size="small" style="background:#fafafa;margin-bottom:12px">
                    <pre style="white-space:pre-wrap;font-size:12px;font-family:inherit;margin:0;color:#666">{{ selectedChunk.normalized_text }}</pre>
                  </n-card>
                </template>

                <template v-if="selectedChunk.resource_refs?.length">
                  <n-text style="font-size:12px;font-weight:600;display:block;margin-bottom:6px">
                    {{ t('chunks.resourceRefs', { n: selectedChunk.resource_refs.length }) }}
                  </n-text>
                  <n-card
                    v-for="ref in selectedChunk.resource_refs"
                    :key="ref.ref_id"
                    size="small"
                    style="margin-bottom:8px"
                  >
                    <div style="display:flex;align-items:center;gap:8px;margin-bottom:4px">
                      <n-tag size="tiny">{{ ref.ref_type }}</n-tag>
                      <n-text style="font-size:12px;font-weight:600">{{ ref.label }}</n-text>
                    </div>
                    <n-text v-if="ref.caption" depth="3" style="font-size:12px;display:block">{{ ref.caption }}</n-text>
                    <n-text v-if="ref.anchor_text" depth="3" style="font-size:12px;display:block">{{ t('chunks.anchorText') }}: {{ ref.anchor_text }}</n-text>
                    <a v-if="ref.url" :href="ref.url" target="_blank" style="font-size:12px">{{ ref.label || ref.url }}</a>
                    <n-text v-if="ref.storage_path" code style="font-size:11px;display:block;margin-top:2px">{{ ref.storage_path }}</n-text>
                  </n-card>
                </template>
              </n-scrollbar>
            </n-card>

            <n-empty v-else :description="t('chunks.emptySelection')" style="margin-top:80px" />
          </div>
        </div>
      </n-spin>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMessage, useDialog } from 'naive-ui'
import { documentsService } from '../services/documents.js'
import { chunksService } from '../services/chunks.js'
import { STATUS_TYPE, CHUNK_STATUS_TYPE } from '../utils/status.js'
import { useI18n } from '../i18n/index.js'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const { t } = useI18n()

const documentId = route.params.documentId

const doc = ref(null)
const chunks = ref([])
const loading = ref(false)
const error = ref('')
const rechunking = ref(false)
const approving = ref(false)
const indexing = ref(false)
const merging = ref(false)

// single-click selects for detail view; checkbox tracks multi-select for merge
const selectedId = ref(null)
const selectedIds = ref([])

const selectedChunk = computed(() => chunks.value.find(c => c.chunk_id === selectedId.value) || null)

const editing = ref(false)
const editText = ref('')
const saving = ref(false)

function startEdit() {
  editText.value = selectedChunk.value.text
  editing.value = true
}

function cancelEdit() {
  editing.value = false
  editText.value = ''
}

async function saveEdit() {
  saving.value = true
  try {
    await chunksService.edit(documentId, selectedChunk.value.chunk_id, editText.value)
    message.success(t('chunks.saved'))
    editing.value = false
    await loadAll()
  } catch (e) {
    message.error(e.message)
  } finally {
    saving.value = false
  }
}

function toggleSelect(chunk) {
  selectedId.value = chunk.chunk_id
  editing.value = false
  editText.value = ''
}

function toggleCheckbox(chunkId, checked) {
  if (checked) {
    if (!selectedIds.value.includes(chunkId)) selectedIds.value.push(chunkId)
  } else {
    selectedIds.value = selectedIds.value.filter(id => id !== chunkId)
  }
}

async function loadAll() {
  loading.value = true
  error.value = ''
  try {
    const [docData, chunkData] = await Promise.all([
      documentsService.get(documentId),
      chunksService.list(documentId),
    ])
    doc.value = docData
    chunks.value = chunkData.items || []
    if (chunks.value.length && !selectedId.value) {
      selectedId.value = chunks.value[0].chunk_id
    }
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

function handleRechunk() {
  dialog.warning({
    title: t('chunks.rechunkDialog.title'),
    content: t('chunks.rechunkDialog.content'),
    positiveText: t('chunks.rechunkDialog.confirm'),
    negativeText: t('chunks.rechunkDialog.cancel'),
    onPositiveClick: async () => {
      rechunking.value = true
      try {
        await chunksService.rechunk(documentId)
        message.success(t('chunks.rechunkDialog.success'))
        selectedIds.value = []
        await loadAll()
      } catch (e) {
        message.error(e.message)
      } finally {
        rechunking.value = false
      }
    },
  })
}

async function handleReject(chunk) {
  try {
    await chunksService.reject(documentId, chunk.chunk_id)
    message.success(t('chunks.rejected', { n: chunk.chunk_index + 1 }))
    await loadAll()
  } catch (e) {
    message.error(e.message)
  }
}

async function handleRestore(chunk) {
  try {
    await chunksService.restore(documentId, chunk.chunk_id)
    message.success(t('chunks.restored', { n: chunk.chunk_index + 1 }))
    await loadAll()
  } catch (e) {
    message.error(e.message)
  }
}

function handleApprove() {
  dialog.warning({
    title: t('chunks.approveDialog.title'),
    content: t('chunks.approveDialog.content'),
    positiveText: t('chunks.approveDialog.confirm'),
    negativeText: t('chunks.approveDialog.cancel'),
    onPositiveClick: async () => {
      approving.value = true
      try {
        await chunksService.approve(documentId)
        message.success(t('chunks.approveDialog.success'))
        await loadAll()
      } catch (e) {
        message.error(e.message)
      } finally {
        approving.value = false
      }
    },
  })
}

function handleIndex() {
  dialog.warning({
    title: t('chunks.indexDialog.title'),
    content: t('chunks.indexDialog.content'),
    positiveText: t('chunks.indexDialog.confirm'),
    negativeText: t('chunks.indexDialog.cancel'),
    onPositiveClick: async () => {
      indexing.value = true
      try {
        await chunksService.index(documentId)
        message.success(t('chunks.indexDialog.success'))
        router.push(`/documents/${documentId}`)
      } catch (e) {
        message.error(e.message)
      } finally {
        indexing.value = false
      }
    },
  })
}

async function handleMerge() {
  if (selectedIds.value.length < 2) { message.warning(t('chunks.mergeError')); return }

  const selected = chunks.value.filter(c => selectedIds.value.includes(c.chunk_id))
  selected.sort((a, b) => a.chunk_index - b.chunk_index)
  for (let i = 1; i < selected.length; i++) {
    if (selected[i].chunk_index !== selected[i - 1].chunk_index + 1) {
      message.warning(t('chunks.mergeAdjacentError'))
      return
    }
  }

  merging.value = true
  try {
    await chunksService.merge(documentId, selectedIds.value)
    message.success(t('chunks.mergeSuccess'))
    selectedIds.value = []
    await loadAll()
  } catch (e) {
    message.error(e.message)
  } finally {
    merging.value = false
  }
}

onMounted(loadAll)
</script>

<style scoped>
.chunk-item {
  padding: 10px 12px;
  border-radius: 4px;
  cursor: pointer;
  margin-bottom: 4px;
  border: 1px solid #eee;
  transition: background 0.1s;
}
.chunk-item:hover { background: #f5f5f5; }
.chunk-item--active { background: #e8f4ff; border-color: #99caff; }
.chunk-item--selected { background: #f0f8ff; border-color: #b3d9ff; }
.chunk-item--rejected { opacity: 0.45; }
.chunk-item__header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 2px; }
.chunk-item__section { font-size: 12px; color: #555; margin-bottom: 2px; }
.chunk-item__pages { font-size: 11px; color: #999; }
</style>
