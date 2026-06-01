
<script setup>
import { computed, h, ref, reactive } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NIcon, useDialog, useMessage } from 'naive-ui'
import { DocumentOutline, SearchOutline, PeopleOutline, LogOutOutline as LogOutIcon, KeyOutline as KeyIcon, PersonCircleOutline as PersonIcon, ShieldCheckmarkOutline as ShieldIcon, LanguageOutline as LangIcon, LogoGithub as LogoGithubIcon } from '@vicons/ionicons5'
import { useAuthStore } from '../stores/auth.js'
import { getConfig } from '../config/app-config.js'
import { authService } from '../services/auth.js'
import { useI18n } from '../i18n/index.js'
import { LOCALES } from '../stores/locale.js'
import { toDataURL } from 'qrcode'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const appTitle = computed(() => t('appTitle'))
const dialog = useDialog()
const message = useMessage()
const collapsed = ref(false)

const SIDER_MIN = 150
const SIDER_MAX = 480

const siderWidth = ref(parseInt(localStorage.getItem('siderWidth') || '200', 10))

function startResize(e) {
  e.preventDefault()
  const startX = e.clientX
  const startWidth = siderWidth.value

  function onMouseMove(ev) {
    siderWidth.value = Math.min(SIDER_MAX, Math.max(SIDER_MIN, startWidth + ev.clientX - startX))
  }
  function onMouseUp() {
    localStorage.setItem('siderWidth', siderWidth.value)
    window.removeEventListener('mousemove', onMouseMove)
    window.removeEventListener('mouseup', onMouseUp)
  }
  window.addEventListener('mousemove', onMouseMove)
  window.addEventListener('mouseup', onMouseUp)
}
const { t, locale, setLocale } = useI18n()

const activeKey = computed(() => {
  const p = route.path
  if (p.startsWith('/documents')) return 'documents'
  if (p.startsWith('/search')) return 'search'
  if (p.startsWith('/knowledge-bases')) return 'knowledge-bases'
  if (p.startsWith('/users')) return 'users'
  return null
})

function icon(component) {
  return () => h(NIcon, null, { default: () => h(component) })
}

const menuOptions = computed(() => {
  const items = [
    { label: t('nav.documents'), key: 'documents', icon: icon(DocumentOutline) },
    { label: t('nav.search'), key: 'search', icon: icon(SearchOutline) },
  ]
  items.push({ label: t('nav.knowledgeBases'), key: 'knowledge-bases', icon: icon(DocumentOutline) })
  if (auth.user?.permissions?.includes('view_user_list')) {
    items.push({ label: t('nav.users'), key: 'users', icon: icon(PeopleOutline) })
  }
  return items
})

const userMenuOptions = computed(() => [
  { label: t('user.changePassword'), key: 'change-password', icon: () => h(NIcon, null, { default: () => h(KeyIcon) }) },
  {
    label: auth.user?.totp_enabled ? t('user.disableTOTP') : t('user.enableTOTP'),
    key: 'totp',
    icon: () => h(NIcon, null, { default: () => h(ShieldIcon) }),
  },
  {
    label: t('user.language'),
    key: 'language',
    icon: () => h(NIcon, null, { default: () => h(LangIcon) }),
    children: LOCALES.map(l => ({ label: l.label, key: `lang-${l.value}` })),
  },
  { type: 'divider', key: 'd1' },
  { label: t('user.logout'), key: 'logout', icon: () => h(NIcon, null, { default: () => h(LogOutIcon) }) },
])

function handleUserMenuSelect(key) {
  if (key === 'change-password') {
    showPasswordModal.value = true
  } else if (key === 'totp') {
    openTOTPModal()
  } else if (key.startsWith('lang-')) {
    setLocale(key.slice(5))
  } else if (key === 'logout') {
    handleLogout()
  }
}

function handleLogout() {
  dialog.warning({
    title: t('logout.title'),
    content: t('logout.message'),
    positiveText: t('logout.confirm'),
    negativeText: t('logout.cancel'),
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

const pwRules = computed(() => ({
  oldPassword: [{ required: true, message: t('password.rules.old'), trigger: 'blur' }],
  newPassword: [{ required: true, message: t('password.rules.new'), trigger: 'blur' }],
  confirmPassword: [
    { required: true, message: t('password.rules.confirm'), trigger: 'blur' },
    {
      validator: (_, value) => value === pwForm.newPassword,
      message: t('password.rules.mismatch'),
      trigger: 'blur',
    },
  ],
}))

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
    message.success(t('password.success'))
    cancelPasswordModal()
  } catch (e) {
    message.error(e.message || t('password.failed'))
  } finally {
    pwLoading.value = false
  }
}

// TOTP modal
const showTOTPModal = ref(false)
const totpStep = ref('setup') // setup | disable
const totpSecret = ref('')
const totpQRDataUrl = ref('')
const totpCode = ref('')
const totpLoading = ref(false)
const totpError = ref('')

async function openTOTPModal() {
  totpCode.value = ''
  totpError.value = ''
  totpSecret.value = ''
  totpQRDataUrl.value = ''
  showTOTPModal.value = true
  if (auth.user?.totp_enabled) {
    totpStep.value = 'disable'
  } else {
    totpStep.value = 'setup'
    startTOTPSetup()
  }
}

async function startTOTPSetup() {
  totpLoading.value = true
  totpError.value = ''
  try {
    const data = await authService.totpSetup()
    totpSecret.value = data.secret
    totpQRDataUrl.value = await toDataURL(data.qr_url, { width: 200, margin: 1 })
    totpStep.value = 'setup'
  } catch (e) {
    totpError.value = e.message || t('totp.initFailed')
  } finally {
    totpLoading.value = false
  }
}

async function confirmTOTPEnable() {
  if (!totpCode.value) { totpError.value = t('totp.codeRequired'); return }
  totpLoading.value = true
  totpError.value = ''
  try {
    await authService.totpEnable(totpCode.value)
    auth.user = { ...auth.user, totp_enabled: true }
    message.success(t('totp.enableSuccess'))
    showTOTPModal.value = false
  } catch (e) {
    totpError.value = e.message || t('totp.verifyFailed')
    totpCode.value = ''
  } finally {
    totpLoading.value = false
  }
}

async function confirmTOTPDisable() {
  if (!totpCode.value) { totpError.value = t('totp.codeRequired'); return }
  totpLoading.value = true
  totpError.value = ''
  try {
    await authService.totpDisable(totpCode.value)
    auth.user = { ...auth.user, totp_enabled: false }
    message.success(t('totp.disableSuccess'))
    showTOTPModal.value = false
  } catch (e) {
    totpError.value = e.message || t('totp.verifyFailed')
    totpCode.value = ''
  } finally {
    totpLoading.value = false
  }
}
</script>

<template>
  <n-layout has-sider style="height:100vh">
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="56"
      :width="siderWidth"
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

      <div class="sider-source" :class="{ 'sider-source--collapsed': collapsed }">
        <a href="https://github.com/d2jvkpn/rag" target="_blank" rel="noopener">
          <n-icon size="14"><logo-github-icon /></n-icon>
          <span v-if="!collapsed">{{ t('app.sourceLink') }}</span>
        </a>
      </div>

      <div v-if="!collapsed" class="sider-resize-handle" @mousedown="startResize" />
    </n-layout-sider>

    <n-modal v-model:show="showPasswordModal" preset="card" :title="t('password.title')" style="width:360px" :mask-closable="false">
      <n-form ref="pwFormRef" :model="pwForm" :rules="pwRules" label-placement="left" label-width="auto">
        <n-form-item :label="t('password.oldPassword')" path="oldPassword">
          <n-input v-model:value="pwForm.oldPassword" type="password" show-password-on="click" :placeholder="t('password.oldPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('password.newPassword')" path="newPassword">
          <n-input v-model:value="pwForm.newPassword" type="password" show-password-on="click" :placeholder="t('password.newPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('password.confirmPassword')" path="confirmPassword">
          <n-input v-model:value="pwForm.confirmPassword" type="password" show-password-on="click" :placeholder="t('password.confirmPlaceholder')" />
        </n-form-item>
      </n-form>
      <template #footer>
        <div style="display:flex;justify-content:flex-end;gap:8px">
          <n-button @click="cancelPasswordModal">{{ t('password.cancel') }}</n-button>
          <n-button type="primary" :loading="pwLoading" @click="submitPasswordChange">{{ t('password.submit') }}</n-button>
        </div>
      </template>
    </n-modal>

    <n-modal v-model:show="showTOTPModal" preset="card" :title="t('totp.title')" style="width:400px" :mask-closable="false">
      <div v-if="totpStep === 'setup'">
        <div style="text-align:center;margin-bottom:12px">
          <img v-if="totpQRDataUrl" :src="totpQRDataUrl" alt="QR Code" style="width:200px;height:200px" />
        </div>
        <n-text depth="3" style="font-size:12px;display:block;margin-bottom:4px">{{ t('totp.setupHint') }}</n-text>
        <n-text code style="font-size:13px;word-break:break-all;display:block;margin-bottom:16px">{{ totpSecret }}</n-text>
        <n-input
          v-model:value="totpCode"
          placeholder="000000"
          maxlength="6"
          :allow-input="(v) => /^\d*$/.test(v)"
          style="margin-bottom:8px"
        />
        <n-text v-if="totpError" type="error" style="font-size:12px;display:block;margin-bottom:8px">{{ totpError }}</n-text>
      </div>

      <div v-else-if="totpStep === 'disable'">
        <n-text depth="3" style="display:block;margin-bottom:16px">{{ t('totp.disableHint') }}</n-text>
        <n-input
          v-model:value="totpCode"
          placeholder="000000"
          maxlength="6"
          :allow-input="(v) => /^\d*$/.test(v)"
          style="margin-bottom:8px"
        />
        <n-text v-if="totpError" type="error" style="font-size:12px;display:block;margin-bottom:8px">{{ totpError }}</n-text>
      </div>

      <template #footer>
        <div style="display:flex;justify-content:flex-end;gap:8px">
          <n-button @click="showTOTPModal = false">{{ t('totp.cancel') }}</n-button>
          <n-button
            v-if="totpStep === 'setup'"
            type="primary"
            :loading="totpLoading"
            @click="confirmTOTPEnable"
          >{{ t('totp.confirmEnable') }}</n-button>
          <n-button
            v-else-if="totpStep === 'disable'"
            type="error"
            :loading="totpLoading"
            @click="confirmTOTPDisable"
          >{{ t('totp.confirmDisable') }}</n-button>
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
.sider-source {
  position: absolute;
  bottom: 52px;
  left: 0;
  right: 0;
  padding: 4px 16px;
  display: flex;
}
.sider-source a {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--n-text-color-disabled, #aaa);
  text-decoration: none;
  white-space: nowrap;
  overflow: hidden;
}
.sider-source a:hover {
  color: var(--n-text-color, #333);
}
.sider-source--collapsed {
  justify-content: center;
  padding: 4px 0;
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
.sider-resize-handle {
  position: absolute;
  top: 0;
  right: -3px;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
  z-index: 10;
}
.sider-resize-handle:hover,
.sider-resize-handle:active {
  background: rgba(99, 125, 255, 0.3);
}
</style>
