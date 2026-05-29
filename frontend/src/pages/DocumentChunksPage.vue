
<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMessage, useDialog } from 'naive-ui'
import { documentsService } from '../services/documents.js'
import { chunksService } from '../services/chunks.js'
import { STATUS_TYPE, CHUNK_STATUS_TYPE } from '../utils/status.js'
import { useFormat } from '../utils/format.js'
import { useI18n } from '../i18n/index.js'
import { getConfig } from '../config/app-config.js'

const CHUNK_PAGE_SIZE = 50

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const { t } = useI18n()
const { formatDate, rfc3339 } = useFormat()

const documentId = route.params.documentId
const { staticBase } = getConfig()

function staticUrl(storagePath) {
  return staticBase.replace(/\/+$/, '') + '/' + storagePath.replace(/^static\//, '')
}

const doc = ref(null)
const chunks = ref([])
const loading = ref(false)
const loadingPage = ref(false)
const loadingNext = ref(false)
const loadingPrevious = ref(false)
const error = ref('')
const rechunking = ref(false)
const approving = ref(false)
const merging = ref(false)
const showDetailsModal = ref(false)
const chunkListRef = ref(null)

const pagination = ref({
  page: 1,
  page_size: CHUNK_PAGE_SIZE,
  total: 0,
  total_pages: 0,
  has_next: false,
  has_prev: false,
})
const loadedPages = ref(new Set())

// single-click selects for detail view; checkbox tracks multi-select for merge
const selectedId = ref(null)
const selectedIds = ref([])

const selectedChunk = computed(() => chunks.value.find(c => c.chunk_id === selectedId.value) || null)
const selectedResourceRefs = computed(() => selectedChunk.value?.resource_refs || [])
const sortedLoadedPages = computed(() => [...loadedPages.value].sort((a, b) => a - b))
const minLoadedPage = computed(() => sortedLoadedPages.value[0] || 0)
const maxLoadedPage = computed(() => sortedLoadedPages.value[sortedLoadedPages.value.length - 1] || 0)
const hasPreviousPage = computed(() => minLoadedPage.value > 1)
const hasNextPage = computed(() => maxLoadedPage.value > 0 && maxLoadedPage.value < pagination.value.total_pages)
const loadedRangeText = computed(() => {
  if (!pagination.value.total || !chunks.value.length) return t('chunks.count', { n: 0 })
  const start = chunks.value[0].chunk_index + 1
  const end = chunks.value[chunks.value.length - 1].chunk_index + 1
  return t('chunks.loadedRange', { start, end, total: pagination.value.total })
})

function resourceRefCount(chunk) {
  return Array.isArray(chunk?.resource_refs) ? chunk.resource_refs.length : 0
}

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
    const previousIndex = selectedChunk.value?.chunk_index
    await chunksService.edit(documentId, selectedChunk.value.chunk_id, editText.value)
    message.success(t('chunks.saved'))
    editing.value = false
    await refreshLoadedPages(previousIndex)
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

function rememberPage(page) {
  const next = new Set(loadedPages.value)
  next.add(page)
  loadedPages.value = next
}

function mergeChunkItems(existing, incoming) {
  const byId = new Map(existing.map(chunk => [chunk.chunk_id, chunk]))
  for (const chunk of incoming) byId.set(chunk.chunk_id, chunk)
  return [...byId.values()].sort((a, b) => a.chunk_index - b.chunk_index)
}

function selectNearestChunk(previousIndex, fallback = 'first') {
  if (!chunks.value.length) {
    selectedId.value = null
    return
  }
  if (selectedId.value && chunks.value.some(chunk => chunk.chunk_id === selectedId.value)) return
  if (typeof previousIndex === 'number') {
    let nearest = chunks.value[0]
    for (const chunk of chunks.value) {
      if (Math.abs(chunk.chunk_index - previousIndex) < Math.abs(nearest.chunk_index - previousIndex)) nearest = chunk
    }
    selectedId.value = nearest.chunk_id
    return
  }
  selectedId.value = fallback === 'last' ? chunks.value[chunks.value.length - 1].chunk_id : chunks.value[0].chunk_id
}

async function loadChunkPage(page, mode = 'replace', select = 'preserve') {
  if (loadingPage.value) return
  loadingPage.value = true
  if (mode === 'append') loadingNext.value = true
  if (mode === 'prepend') loadingPrevious.value = true
  const listEl = chunkListRef.value
  const previousScrollHeight = listEl?.scrollHeight || 0
  try {
    const chunkData = await chunksService.list(documentId, { page, pageSize: CHUNK_PAGE_SIZE })
    pagination.value = chunkData
    if (mode === 'replace') {
      chunks.value = chunkData.items || []
      loadedPages.value = new Set(chunkData.total_pages > 0 ? [chunkData.page] : [])
    } else {
      chunks.value = mergeChunkItems(chunks.value, chunkData.items || [])
      rememberPage(chunkData.page)
    }
    selectedIds.value = selectedIds.value.filter(id => chunks.value.some(chunk => chunk.chunk_id === id))
    if (select === 'first') selectedId.value = chunks.value[0]?.chunk_id || null
    else if (select === 'last') selectedId.value = chunks.value[chunks.value.length - 1]?.chunk_id || null
    else selectNearestChunk()
    await nextTick()
    if (mode === 'prepend' && listEl) {
      listEl.scrollTop += listEl.scrollHeight - previousScrollHeight
    }
    if (select === 'last' && listEl) {
      listEl.scrollTop = listEl.scrollHeight
    }
    if (mode !== 'prepend') fillScrollableList()
  } finally {
    loadingPage.value = false
    loadingNext.value = false
    loadingPrevious.value = false
  }
}

async function loadAll() {
  loading.value = true
  error.value = ''
  try {
    doc.value = await documentsService.get(documentId)
    selectedId.value = null
    selectedIds.value = []
    await loadChunkPage(1, 'replace', 'first')
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function refreshLoadedPages(previousIndex) {
  const pages = sortedLoadedPages.value.length ? sortedLoadedPages.value : [1]
  try {
    doc.value = await documentsService.get(documentId)
    const responses = await Promise.all(pages.map(page => chunksService.list(documentId, { page, pageSize: CHUNK_PAGE_SIZE })))
    const validResponses = responses.filter(response => response.total_pages === 0 || response.page <= response.total_pages)
    const nextChunks = []
    const nextPages = new Set()
    for (const response of validResponses) {
      nextChunks.push(...(response.items || []))
      if (response.total_pages > 0) nextPages.add(response.page)
    }
    pagination.value = validResponses[validResponses.length - 1] || pagination.value
    chunks.value = mergeChunkItems([], nextChunks)
    loadedPages.value = nextPages
    selectedIds.value = selectedIds.value.filter(id => chunks.value.some(chunk => chunk.chunk_id === id))
    selectNearestChunk(previousIndex)
  } catch (e) {
    error.value = e.message
  }
}

async function loadNextPage() {
  if (!hasNextPage.value || loadingPage.value) return
  await loadChunkPage(maxLoadedPage.value + 1, 'append')
}

function fillScrollableList() {
  nextTick(() => {
    const el = chunkListRef.value
    if (!el || loadingPage.value || !hasNextPage.value) return
    if (el.scrollHeight <= el.clientHeight + 24) loadNextPage()
  })
}

async function loadPreviousPage() {
  if (!hasPreviousPage.value || loadingPage.value) return
  await loadChunkPage(minLoadedPage.value - 1, 'prepend')
}

function handleChunkScroll(event) {
  const el = event.target
  if (!el) return
  if (el.scrollTop <= 24) loadPreviousPage()
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 24) loadNextPage()
}

async function jumpToFirst() {
  if (!pagination.value.total) return
  if (loadedPages.value.has(1)) {
    selectedId.value = chunks.value[0]?.chunk_id || null
    await nextTick()
    if (chunkListRef.value) chunkListRef.value.scrollTop = 0
    return
  }
  await loadChunkPage(1, 'replace', 'first')
}

async function jumpToLast() {
  if (!pagination.value.total) return
  const page = pagination.value.total_pages || Math.ceil(pagination.value.total / CHUNK_PAGE_SIZE)
  if (loadedPages.value.has(page)) {
    const lastChunk = chunks.value[chunks.value.length - 1]
    selectedId.value = lastChunk?.chunk_id || null
    await nextTick()
    if (chunkListRef.value) chunkListRef.value.scrollTop = chunkListRef.value.scrollHeight
    return
  }
  await loadChunkPage(page, 'replace', 'last')
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
    await refreshLoadedPages(chunk.chunk_index)
  } catch (e) {
    message.error(e.message)
  }
}

async function handleRestore(chunk) {
  try {
    await chunksService.restore(documentId, chunk.chunk_id)
    message.success(t('chunks.restored', { n: chunk.chunk_index + 1 }))
    await refreshLoadedPages(chunk.chunk_index)
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
    const previousIndex = selected[0]?.chunk_index
    await chunksService.merge(documentId, selectedIds.value)
    message.success(t('chunks.mergeSuccess'))
    selectedIds.value = []
    await refreshLoadedPages(previousIndex)
  } catch (e) {
    message.error(e.message)
  } finally {
    merging.value = false
  }
}

onMounted(loadAll)
</script>


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
          <n-button v-if="doc?.status === 'review_pending'" size="small" type="primary" :loading="approving" @click="handleApprove">
            {{ t('chunks.approve') }}
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
                <div style="display:flex;align-items:center;justify-content:space-between;gap:8px">
                  <div style="display:flex;flex-direction:column;gap:2px">
                    <n-text style="font-size:13px">{{ loadedRangeText }}</n-text>
                    <n-text v-if="loadingPage" depth="3" style="font-size:11px">{{ t('chunks.loadingPage') }}</n-text>
                  </div>
                  <n-space size="small" align="center">
                    <n-button text size="tiny" :disabled="!pagination.total || loadingPage" @click="jumpToFirst">{{ t('chunks.jumpToFirst') }}</n-button>
                    <n-button text size="tiny" :disabled="!pagination.total || loadingPage" @click="jumpToLast">{{ t('chunks.jumpToLast') }}</n-button>
                    <n-button text size="tiny" @click="selectedIds = []" v-if="selectedIds.length">{{ t('chunks.clearSelection') }}</n-button>
                  </n-space>
                </div>
              </template>
              <div ref="chunkListRef" class="chunk-list-scroll" @scroll="handleChunkScroll">
                <div v-if="hasPreviousPage" class="chunk-page-loader">{{ loadingPrevious ? t('chunks.loadingPage') : t('chunks.scrollForPrevious') }}</div>
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
                      <n-tag v-if="resourceRefCount(chunk) > 0" size="tiny" type="info" :bordered="false">
                        {{ t('chunks.resourceRefCount', { n: resourceRefCount(chunk) }) }}
                      </n-tag>
                      <n-tag size="tiny" :type="CHUNK_STATUS_TYPE[chunk.status] || 'default'">
                        {{ t(`chunkStatus.${chunk.status}`) || chunk.status }}
                      </n-tag>
                      <n-button
                        v-if="doc?.status !== 'indexed' && chunk.status !== 'rejected'"
                        text
                        size="tiny"
                        type="error"
                        :title="t('chunks.rejectTitle')"
                        @click.stop="handleReject(chunk)"
                      >✕</n-button>
                      <n-button
                        v-if="doc?.status !== 'indexed' && chunk.status === 'rejected'"
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
                <div v-if="hasNextPage" class="chunk-page-loader">{{ loadingNext ? t('chunks.loadingPage') : t('chunks.scrollForMore') }}</div>
              </div>
            </n-card>
          </div>

          <!-- Right: chunk detail -->
          <div class="split-view__right">
            <n-card v-if="selectedChunk" size="small" style="height:100%">
              <template #header>
                <div class="chunk-detail-header">
                  <n-space size="small" align="center" class="chunk-detail-title">
                    <n-text>Chunk #{{ selectedChunk.chunk_index + 1 }}</n-text>
                    <n-tag size="small" :type="CHUNK_STATUS_TYPE[selectedChunk.status] || 'default'">
                      {{ t(`chunkStatus.${selectedChunk.status}`) || selectedChunk.status }}
                    </n-tag>
                    <n-text v-if="selectedChunk.section_title" depth="3" class="chunk-detail-section">
                      {{ selectedChunk.section_title }}
                    </n-text>
                  </n-space>
                  <n-space size="small" align="center" class="chunk-detail-actions">
                    <n-button size="small" @click="showDetailsModal = true">
                      {{ t('chunks.detailsAndRefs', { n: selectedResourceRefs.length }) }}
                    </n-button>
                    <template v-if="!editing">
                      <n-button v-if="selectedChunk.status !== 'rejected'" size="small" type="primary" secondary @click="startEdit">{{ t('chunks.edit') }}</n-button>
                    </template>
                    <template v-else>
                      <n-button size="small" type="primary" :loading="saving" @click="saveEdit">{{ t('chunks.save') }}</n-button>
                      <n-button size="small" @click="cancelEdit">{{ t('chunks.cancel') }}</n-button>
                    </template>
                  </n-space>
                </div>
              </template>

              <n-scrollbar style="max-height:calc(100vh - 180px)">
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
              </n-scrollbar>
            </n-card>

            <n-empty v-else :description="t('chunks.emptySelection')" style="margin-top:80px" />
          </div>
        </div>
      </n-spin>

      <n-modal v-model:show="showDetailsModal" preset="card" :title="t('chunks.detailsAndRefsTitle')" style="max-width:820px">
        <n-scrollbar style="max-height:70vh">
          <section v-if="selectedChunk" class="chunk-modal-section">
            <div class="chunk-modal-section__title">{{ t('chunks.metadata') }}</div>
            <div class="chunk-meta-grid">
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.chunkId') }}</span><n-text code style="font-size:11px">{{ selectedChunk.chunk_id }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.documentId') }}</span><n-text code style="font-size:11px">{{ selectedChunk.document_id }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.version') }}</span><n-text>v{{ selectedChunk.chunk_version }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.source') }}</span><n-text>{{ selectedChunk.source || '—' }}</n-text></div>
              <div v-if="selectedChunk.filename" class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.filename') }}</span><n-text>{{ selectedChunk.filename }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.chunkIndex') }}</span><n-text>#{{ selectedChunk.chunk_index + 1 }}</n-text></div>
              <div v-if="selectedChunk.section_title" class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.sectionTitle') }}</span><n-text>{{ selectedChunk.section_title }}</n-text></div>
              <div v-if="selectedChunk.page_start" class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.page') }}</span><n-text>{{ selectedChunk.page_start }}{{ selectedChunk.page_end && selectedChunk.page_end !== selectedChunk.page_start ? '–' + selectedChunk.page_end : '' }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.currentVersion') }}</span><n-tag size="tiny" :type="selectedChunk.is_current ? 'success' : 'default'">{{ selectedChunk.is_current ? t('chunks.meta.yes') : t('chunks.meta.no') }}</n-tag></div>
              <div v-if="selectedChunk.embedding_model" class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.embeddingModel') }}</span><n-text>{{ selectedChunk.embedding_model }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.createdAt') }}</span><n-text :title="rfc3339(selectedChunk.created_at)">{{ formatDate(selectedChunk.created_at) }}</n-text></div>
              <div class="chunk-meta-item"><span class="chunk-meta-item__label">{{ t('chunks.meta.updatedAt') }}</span><n-text :title="rfc3339(selectedChunk.updated_at)">{{ formatDate(selectedChunk.updated_at) }}</n-text></div>
              <div v-if="selectedChunk.review_comment" class="chunk-meta-item chunk-meta-item--wide"><span class="chunk-meta-item__label">{{ t('chunks.meta.reviewComment') }}</span><n-text depth="3" style="display:block;font-size:12px;white-space:pre-wrap">{{ selectedChunk.review_comment }}</n-text></div>
            </div>
          </section>

          <section class="chunk-modal-section">
            <div class="chunk-modal-section__title">{{ t('chunks.resourceRefs', { n: selectedResourceRefs.length }) }}</div>
            <n-empty v-if="!selectedResourceRefs.length" :description="t('chunks.noResourceRefs')" />
            <div v-else class="resource-ref-list">
              <div v-for="ref in selectedResourceRefs" :key="ref.ref_id" class="resource-ref-item">
                <div class="resource-ref-item__header"><n-space size="small" align="center"><n-tag size="tiny">{{ ref.ref_type || 'ref' }}</n-tag><n-text style="font-size:13px;font-weight:600">{{ ref.label || ref.ref_id }}</n-text></n-space><n-text v-if="ref.page" depth="3" style="font-size:12px">{{ t('chunks.page') }} {{ ref.page }}</n-text></div>
                <n-text v-if="ref.caption" depth="3" style="font-size:12px;display:block">{{ ref.caption }}</n-text>
                <n-text v-if="ref.anchor_text" depth="3" style="font-size:12px;display:block">{{ t('chunks.anchorText') }}: {{ ref.anchor_text }}</n-text>
                <a v-if="ref.url" :href="ref.url" target="_blank" rel="noreferrer" style="font-size:12px">{{ ref.url }}</a>
                <img v-if="ref.ref_type === 'image' && ref.storage_path" :src="staticUrl(ref.storage_path)" :alt="ref.label || ref.ref_id" style="max-width:320px;max-height:240px;object-fit:contain;display:block;margin-top:6px;border-radius:4px" />
                <n-text v-if="ref.storage_path" code style="font-size:11px;display:block;margin-top:4px">{{ ref.storage_path }}</n-text>
                <n-tag v-if="ref.is_external" size="tiny" style="margin-top:6px">{{ t('chunks.externalResource') }}</n-tag>
              </div>
            </div>
          </section>
        </n-scrollbar>
      </n-modal>
    </div>
  </div>
</template>


<style scoped>
.chunk-detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-width: 0;
  flex-wrap: wrap;
}
.chunk-detail-title {
  min-width: 0;
  flex: 1 1 260px;
}
.chunk-detail-section {
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
.chunk-detail-actions {
  flex: 0 0 auto;
}
.chunk-modal-section + .chunk-modal-section {
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid #edf0f5;
}
.chunk-modal-section__title {
  margin-bottom: 10px;
  font-size: 13px;
  font-weight: 600;
}
.chunk-meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 10px 14px;
}
.chunk-meta-item {
  min-width: 0;
}
.chunk-meta-item--wide {
  grid-column: 1 / -1;
}
.chunk-meta-item__label {
  display: block;
  margin-bottom: 4px;
  font-size: 11px;
  color: #8c8f99;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.chunk-list-scroll {
  max-height: calc(100vh - 180px);
  overflow: auto;
}
.chunk-page-loader {
  padding: 8px 12px;
  text-align: center;
  font-size: 12px;
  color: #8c8f99;
}
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
.resource-ref-list { display: grid; gap: 10px; }
.resource-ref-item { padding: 10px 12px; border: 1px solid #e7e9ef; border-radius: 6px; background: #fff; }
.resource-ref-item__header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 6px; }
</style>
