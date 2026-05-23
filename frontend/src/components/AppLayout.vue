<template>
  <n-layout has-sider style="height:100vh">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="56"
      :width="200"
      :collapsed="collapsed"
      show-trigger
      @collapse="collapsed = true"
      @expand="collapsed = false"
    >
      <div class="sider-header" :class="{ 'sider-header--collapsed': collapsed }">
        <span v-if="!collapsed" class="sider-title">{{ appTitle }}</span>
      </div>

      <n-menu
        :collapsed="collapsed"
        :collapsed-width="56"
        :collapsed-icon-size="20"
        :options="menuOptions"
        :value="activeKey"
        @update:value="(key) => router.push('/' + key)"
      />

      <n-dropdown :options="userMenuOptions" placement="top-start" trigger="click" @select="handleUserMenuSelect">
        <div class="sider-footer" :class="{ 'sider-footer--collapsed': collapsed }">
          <n-icon size="16" style="flex-shrink:0"><person-icon /></n-icon>
          <span v-if="!collapsed" class="user-name">{{ auth.user?.username }}</span>
        </div>
      </n-dropdown>
    </n-layout-sider>

    <n-modal v-model:show="showPasswordModal" preset="card" title="修改密码" style="width:360px" :mask-closable="false">
      <n-form ref="pwFormRef" :model="pwForm" :rules="pwRules" label-placement="left" label-width="80">
        <n-form-item label="当前密码" path="oldPassword">
          <n-input v-model:value="pwForm.oldPassword" type="password" show-password-on="click" placeholder="当前密码" />
        </n-form-item>
        <n-form-item label="新密码" path="newPassword">
          <n-input v-model:value="pwForm.newPassword" type="password" show-password-on="click" placeholder="新密码" />
        </n-form-item>
        <n-form-item label="确认新密码" path="confirmPassword">
          <n-input v-model:value="pwForm.confirmPassword" type="password" show-password-on="click" placeholder="再次输入新密码" />
        </n-form-item>
      </n-form>
      <template #footer>
        <div style="display:flex;justify-content:flex-end;gap:8px">
          <n-button @click="cancelPasswordModal">取消</n-button>
          <n-button type="primary" :loading="pwLoading" @click="submitPasswordChange">确认修改</n-button>
        </div>
      </template>
    </n-modal>

    <n-layout>
      <n-layout-content class="main-content">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup>
import { computed, h, ref, reactive } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NIcon, useDialog, useMessage } from 'naive-ui'
import { DocumentOutline, SearchOutline, LogOutOutline as LogOutIcon, KeyOutline as KeyIcon, PersonCircleOutline as PersonIcon } from '@vicons/ionicons5'
import { useAuthStore } from '../stores/auth.js'
import { getConfig } from '../config/app-config.js'
import { authService } from '../services/auth.js'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const appTitle = getConfig().appTitle
const dialog = useDialog()
const message = useMessage()
const collapsed = ref(false)

const activeKey = computed(() => {
  const p = route.path
  if (p.startsWith('/documents')) return 'documents'
  if (p.startsWith('/search')) return 'search'
  return null
})

function icon(component) {
  return () => h(NIcon, null, { default: () => h(component) })
}

const menuOptions = [
  { label: '文档管理', key: 'documents', icon: icon(DocumentOutline) },
  { label: '知识库查询', key: 'search', icon: icon(SearchOutline) },
]

const userMenuOptions = [
  { label: '修改密码', key: 'change-password', icon: () => h(NIcon, null, { default: () => h(KeyIcon) }) },
  { type: 'divider', key: 'd1' },
  { label: '退出登录', key: 'logout', icon: () => h(NIcon, null, { default: () => h(LogOutIcon) }) },
]

function handleUserMenuSelect(key) {
  if (key === 'change-password') {
    showPasswordModal.value = true
  } else if (key === 'logout') {
    handleLogout()
  }
}

function handleLogout() {
  dialog.warning({
    title: '退出登录',
    content: '确认退出吗？',
    positiveText: '退出',
    negativeText: '取消',
    onPositiveClick: async () => {
      await auth.logout()
      router.push('/login')
    },
  })
}

const showPasswordModal = ref(false)
const pwLoading = ref(false)
const pwFormRef = ref(null)
const pwForm = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })

const pwRules = {
  oldPassword: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
  newPassword: [{ required: true, message: '请输入新密码', trigger: 'blur' }],
  confirmPassword: [
    { required: true, message: '请确认新密码', trigger: 'blur' },
    {
      validator: (_, value) => value === pwForm.newPassword,
      message: '两次输入的密码不一致',
      trigger: 'blur',
    },
  ],
}

function cancelPasswordModal() {
  showPasswordModal.value = false
  pwForm.oldPassword = ''
  pwForm.newPassword = ''
  pwForm.confirmPassword = ''
}

async function submitPasswordChange() {
  try {
    await pwFormRef.value?.validate()
  } catch {
    return
  }
  pwLoading.value = true
  try {
    await authService.changePassword(pwForm.oldPassword, pwForm.newPassword)
    message.success('密码修改成功')
    cancelPasswordModal()
  } catch (e) {
    message.error(e.message || '密码修改失败')
  } finally {
    pwLoading.value = false
  }
}
</script>

<style scoped>
.sider-header {
  height: 56px;
  display: flex;
  align-items: center;
  padding: 0 20px;
  border-bottom: 1px solid #efeff5;
}
.sider-header--collapsed {
  padding: 0;
  justify-content: center;
}
.sider-title {
  font-size: 15px;
  font-weight: 600;
  color: #333;
  white-space: nowrap;
  overflow: hidden;
}
.sider-footer {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 52px;
  display: flex;
  align-items: center;
  padding: 0 16px;
  border-top: 1px solid #efeff5;
  gap: 8px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}
.sider-footer:hover {
  background: #f5f5f5;
}
.sider-footer--collapsed {
  justify-content: center;
  padding: 0;
}
.user-name {
  font-size: 13px;
  color: #555;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.main-content {
  height: 100vh;
  overflow-y: auto;
}
</style>
