<template>
  <div class="page-layout">
    <div class="page-body">
      <div class="toolbar">
        <n-button :loading="loading" @click="loadUsers">{{ t('users.refresh') }}</n-button>
        <n-text v-if="lastRefreshed" depth="3" style="font-size:12px">{{ lastRefreshed }}</n-text>
      </div>

      <n-alert v-if="error" type="error" style="margin-bottom:16px" closable @close="error = ''">
        {{ error }}
      </n-alert>

      <n-data-table
        :columns="columns"
        :data="users"
        :loading="loading"
        :pagination="pagination"
        :row-key="(row) => row.user_id"
        size="small"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, h, onMounted, ref, watch } from 'vue'
import { NButton, NSpace, NTag, NText, useDialog, useMessage } from 'naive-ui'
import { usersService } from '../services/users.js'
import { useAuthStore } from '../stores/auth.js'
import { useI18n } from '../i18n/index.js'
import { useFormat } from '../utils/format.js'

const auth = useAuthStore()
const dialog = useDialog()
const message = useMessage()
const { t } = useI18n()
const { formatDate } = useFormat()
const pageSize = 20

const users = ref([])
const currentPage = ref(1)
const loading = ref(false)
const error = ref('')
const lastRefreshed = ref('')
const canDisableUsers = computed(() => auth.user?.permissions?.includes('disable_users'))
const totalPages = computed(() => Math.max(1, Math.ceil(users.value.length / pageSize)))
const displayPage = computed(() => Math.min(currentPage.value, totalPages.value))
const pageStart = computed(() => users.value.length === 0 ? 0 : (displayPage.value - 1) * pageSize + 1)
const pageEnd = computed(() => users.value.length === 0 ? 0 : Math.min(displayPage.value * pageSize, users.value.length))
const pagination = computed(() => ({
  page: displayPage.value,
  pageSize,
  prefix: () => t('users.pageSummary', { page: displayPage.value, pages: totalPages.value, start: pageStart.value, end: pageEnd.value, total: users.value.length }),
  onUpdatePage: (page) => { currentPage.value = page },
}))

const columns = computed(() => [
  {
    title: t('users.table.username'),
    key: 'username',
  },
  {
    title: t('users.table.status'),
    key: 'status',
    render: (row) => h(NTag, { type: row.status === 'active' ? 'success' : 'warning', size: 'small' }, { default: () => row.status }),
  },
  {
    title: t('users.table.permissions'),
    key: 'permissions',
    render: (row) => {
      const items = Array.isArray(row.permissions) ? row.permissions : []
      if (items.length === 0) return '—'
      return h(NSpace, { size: 4, wrapItem: true }, () =>
        items.map(permission =>
          h(NTag, { size: 'small', bordered: false }, { default: () => permission })
        )
      )
    },
  },
  {
    title: t('users.table.totp'),
    key: 'totp_enabled',
    render: (row) => h(NTag, { type: row.totp_enabled ? 'info' : 'default', size: 'small' }, { default: () => row.totp_enabled ? t('users.enabled') : t('users.disabled') }),
  },
  {
    title: t('users.table.lastLogin'),
    key: 'last_login_at',
    render: (row) => h(NText, null, { default: () => formatDate(row.last_login_at) }),
  },
  {
    title: t('users.table.createdAt'),
    key: 'created_at',
    render: (row) => h(NText, null, { default: () => formatDate(row.created_at) }),
  },
  {
    title: t('users.table.actions'),
    key: 'actions',
    render: (row) => {
      if (!canDisableUsers.value) return '—'
      const isSelf = row.user_id === auth.user?.user_id
      const action = row.status === 'disabled'
        ? h(NButton, {
          size: 'small',
          type: 'primary',
          secondary: true,
          disabled: isSelf,
          onClick: () => handleToggleStatus(row, 'active'),
        }, { default: () => t('users.actions.enable') })
        : h(NButton, {
          size: 'small',
          type: 'warning',
          secondary: true,
          disabled: isSelf,
          onClick: () => handleToggleStatus(row, 'disabled'),
        }, { default: () => t('users.actions.disable') })

      return h(NSpace, { size: 8 }, () => [action])
    },
  },
])

async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const data = await usersService.list()
    users.value = data.items || []
    currentPage.value = 1
    lastRefreshed.value = t('users.updatedAt', { time: new Date().toLocaleTimeString() })
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(totalPages, (pages) => {
  if (currentPage.value > pages) currentPage.value = pages
})

function handleToggleStatus(user, nextStatus) {
  const action = nextStatus === 'disabled' ? 'disable' : 'enable'
  dialog.warning({
    title: t(`users.dialog.${action}Title`),
    content: t(`users.dialog.${action}Content`, { name: user.username }),
    positiveText: t(`users.actions.${action}`),
    negativeText: t('users.actions.cancel'),
    onPositiveClick: async () => {
      try {
        if (nextStatus === 'disabled') {
          await usersService.disable(user.user_id)
        } else {
          await usersService.enable(user.user_id)
        }
        message.success(t(`users.messages.${action}Success`, { name: user.username }))
        await loadUsers()
      } catch (e) {
        message.error(e.message || t('users.messages.actionFailed'))
      }
    },
  })
}

onMounted(loadUsers)
</script>
