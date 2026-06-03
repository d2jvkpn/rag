
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
const canManageUsers = computed(() => auth.user?.permissions?.includes('manage_users'))
const totalPages = computed(() => Math.max(1, Math.ceil(users.value.length / pageSize)))
const displayPage = computed(() => Math.min(currentPage.value, totalPages.value))
const pageStart = computed(() => users.value.length === 0 ? 0 : (displayPage.value - 1) * pageSize + 1)
const pageEnd = computed(() => users.value.length === 0 ? 0 : Math.min(displayPage.value * pageSize, users.value.length))
const pagedUsers = computed(() => users.value.slice(pageStart.value - 1, pageEnd.value))

// --- permission tag styling ---
const permissionTagType = {
  manage_users: 'error',
  manage_knowledge_bases: 'success',
  manage_documents: 'warning',
}
const allPermissions = ['manage_users', 'manage_knowledge_bases', 'manage_documents']

function permissionType(p) {
  return permissionTagType[p] ?? 'default'
}

function permissionLabel(p) {
  const key = `users.permissions.${p}`
  const translated = t(key)
  return translated === key ? p : translated
}

// --- create user dialog ---
const showCreateDialog = ref(false)
const createForm = ref({ username: '', password: '', confirmPassword: '', permissions: [] })
const createLoading = ref(false)

function openCreateDialog() {
  createForm.value = { username: '', password: '', confirmPassword: '', permissions: [] }
  showCreateDialog.value = true
}

async function submitCreate() {
  if (!createForm.value.username.trim()) {
    message.error(t('users.rules.usernameRequired'))
    return
  }
  if (!createForm.value.password.trim()) {
    message.error(t('users.rules.passwordRequired'))
    return
  }
  if (createForm.value.password !== createForm.value.confirmPassword) {
    message.error(t('users.rules.passwordMismatch'))
    return
  }
  createLoading.value = true
  try {
    await usersService.create({
      username: createForm.value.username,
      password: createForm.value.password,
      permissions: createForm.value.permissions,
    })
    message.success(t('users.messages.createSuccess', { name: createForm.value.username }))
    showCreateDialog.value = false
    await loadUsers()
  } catch (e) {
    message.error(e.message || t('users.messages.actionFailed'))
  } finally {
    createLoading.value = false
  }
}

// --- edit permissions dialog ---
const showPermissionsDialog = ref(false)
const permissionsTarget = ref(null)
const permissionsSelected = ref([])
const permissionsLoading = ref(false)

function openPermissionsDialog(user) {
  permissionsTarget.value = user
  permissionsSelected.value = (user.permissions || []).filter(p => allPermissions.includes(p))
  showPermissionsDialog.value = true
}

async function submitPermissions() {
  permissionsLoading.value = true
  try {
    await usersService.setPermissions(permissionsTarget.value.user_id, permissionsSelected.value)
    message.success(t('users.messages.permissionsSuccess'))
    showPermissionsDialog.value = false
    await loadUsers()
  } catch (e) {
    message.error(e.message || t('users.messages.actionFailed'))
  } finally {
    permissionsLoading.value = false
  }
}

// --- reset password dialog ---
const showResetPasswordDialog = ref(false)
const resetPasswordTarget = ref(null)
const resetPasswordForm = ref({ password: '', confirmPassword: '' })
const resetPasswordLoading = ref(false)

function openResetPasswordDialog(user) {
  resetPasswordTarget.value = user
  resetPasswordForm.value = { password: '', confirmPassword: '' }
  showResetPasswordDialog.value = true
}

async function submitResetPassword() {
  if (!resetPasswordForm.value.password.trim()) {
    message.error(t('users.rules.passwordRequired'))
    return
  }
  if (resetPasswordForm.value.password !== resetPasswordForm.value.confirmPassword) {
    message.error(t('users.rules.passwordMismatch'))
    return
  }
  resetPasswordLoading.value = true
  try {
    await usersService.resetPassword(resetPasswordTarget.value.user_id, resetPasswordForm.value.password)
    message.success(t('users.messages.resetPasswordSuccess', { name: resetPasswordTarget.value.username }))
    showResetPasswordDialog.value = false
  } catch (e) {
    message.error(e.message || t('users.messages.actionFailed'))
  } finally {
    resetPasswordLoading.value = false
  }
}

// --- table columns ---
const columns = computed(() => [
  {
    title: t('users.table.id'),
    key: 'user_id',
    render: (row) => h(NText, { depth: 3, style: 'font-size:12px;font-family:monospace' }, { default: () => row.user_id }),
  },
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
      if (items.length === 0) return h(NText, { depth: 3 }, { default: () => '—' })
      return h(NSpace, { size: [4, 4], wrap: true }, () =>
        items.map(p => h('span', { title: p }, [
          h(NTag, {
            size: 'small',
            bordered: false,
            type: permissionType(p),
          }, { default: () => permissionLabel(p) }),
        ]))
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
      if (!canManageUsers.value) return '—'
      const isSelf = row.user_id === auth.user?.user_id
      const toggleBtn = row.status === 'disabled'
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

      const permBtn = h(NButton, {
        size: 'small',
        secondary: true,
        onClick: () => openPermissionsDialog(row),
      }, { default: () => t('users.actions.editPermissions') })

      const resetBtn = h(NButton, {
        size: 'small',
        secondary: true,
        disabled: isSelf,
        onClick: () => openResetPasswordDialog(row),
      }, { default: () => t('users.actions.resetPassword') })

      return h(NSpace, { size: 8 }, () => [toggleBtn, permBtn, resetBtn])
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

<template>
  <div class="page-layout">
    <div class="page-body">
      <div class="toolbar">
        <n-button :loading="loading" @click="loadUsers">{{ t('users.refresh') }}</n-button>
        <n-button v-if="canManageUsers" type="primary" @click="openCreateDialog">
          {{ t('users.createUser') }}
        </n-button>
        <n-text v-if="lastRefreshed" depth="3" style="font-size:12px">{{ lastRefreshed }}</n-text>
      </div>

      <n-alert v-if="error" type="error" style="margin-bottom:16px" closable @close="error = ''">
        {{ error }}
      </n-alert>

      <n-data-table
        :columns="columns"
        :data="pagedUsers"
        :loading="loading"
        :pagination="false"
        :row-key="(row) => row.user_id"
        size="small"
      />
      <div style="display:flex;justify-content:flex-end;align-items:center;gap:12px;margin-top:12px">
        <n-text depth="3" style="font-size:12px">
          {{ t('users.pageSummary', { page: displayPage, pages: totalPages, start: pageStart, end: pageEnd, total: users.length }) }}
        </n-text>
        <n-button size="small" :disabled="displayPage <= 1" @click="currentPage = displayPage - 1">Prev</n-button>
        <n-button size="small" :disabled="displayPage >= totalPages" @click="currentPage = displayPage + 1">Next</n-button>
      </div>
    </div>
  </div>

  <!-- Create User Dialog -->
  <n-modal v-model:show="showCreateDialog" preset="dialog" :title="t('users.dialog.createTitle')" style="width:420px">
    <n-form style="margin-top:12px">
      <n-form-item :label="t('users.form.username')">
        <n-input v-model:value="createForm.username" :placeholder="t('users.form.username')" />
      </n-form-item>
      <n-form-item :label="t('users.form.password')">
        <n-input v-model:value="createForm.password" type="password" show-password-on="click" :placeholder="t('users.form.password')" />
      </n-form-item>
      <n-form-item :label="t('users.form.confirmPassword')">
        <n-input v-model:value="createForm.confirmPassword" type="password" show-password-on="click" :placeholder="t('users.form.confirmPassword')" />
      </n-form-item>
      <n-form-item :label="t('users.table.permissions')">
        <n-checkbox-group v-model:value="createForm.permissions">
          <n-space vertical>
            <n-checkbox v-for="p in allPermissions" :key="p" :value="p">
              <n-tag :type="permissionType(p)" size="small" :bordered="false">{{ permissionLabel(p) }}</n-tag>
            </n-checkbox>
          </n-space>
        </n-checkbox-group>
      </n-form-item>
    </n-form>
    <template #action>
      <n-button @click="showCreateDialog = false">{{ t('users.actions.cancel') }}</n-button>
      <n-button type="primary" :loading="createLoading" @click="submitCreate">{{ t('users.createUser') }}</n-button>
    </template>
  </n-modal>

  <!-- Edit Permissions Dialog -->
  <n-modal
    v-model:show="showPermissionsDialog"
    preset="dialog"
    :title="permissionsTarget ? t('users.dialog.permissionsTitle', { name: permissionsTarget.username }) : ''"
    style="width:380px"
  >
    <n-checkbox-group v-model:value="permissionsSelected" style="margin-top:12px">
      <n-space vertical>
        <n-checkbox v-for="p in allPermissions" :key="p" :value="p">
          <n-tag :type="permissionType(p)" size="small" :bordered="false">{{ permissionLabel(p) }}</n-tag>
        </n-checkbox>
      </n-space>
    </n-checkbox-group>
    <template #action>
      <n-button @click="showPermissionsDialog = false">{{ t('users.actions.cancel') }}</n-button>
      <n-button type="primary" :loading="permissionsLoading" @click="submitPermissions">Save</n-button>
    </template>
  </n-modal>

  <!-- Reset Password Dialog -->
  <n-modal
    v-model:show="showResetPasswordDialog"
    preset="dialog"
    :title="resetPasswordTarget ? t('users.dialog.resetPasswordTitle', { name: resetPasswordTarget.username }) : ''"
    style="width:380px"
  >
    <n-form style="margin-top:12px">
      <n-form-item :label="t('users.form.password')">
        <n-input v-model:value="resetPasswordForm.password" type="password" show-password-on="click" :placeholder="t('users.form.password')" />
      </n-form-item>
      <n-form-item :label="t('users.form.confirmPassword')">
        <n-input v-model:value="resetPasswordForm.confirmPassword" type="password" show-password-on="click" :placeholder="t('users.form.confirmPassword')" />
      </n-form-item>
    </n-form>
    <template #action>
      <n-button @click="showResetPasswordDialog = false">{{ t('users.actions.cancel') }}</n-button>
      <n-button type="primary" :loading="resetPasswordLoading" @click="submitResetPassword">{{ t('users.actions.resetPassword') }}</n-button>
    </template>
  </n-modal>
</template>
