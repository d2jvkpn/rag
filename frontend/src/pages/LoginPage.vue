<template>
  <div class="login-wrapper">
    <n-card class="login-card" :title="appTitle">
      <n-form ref="formRef" :model="form" :rules="activeRules" @keydown.enter="submit">
        <template v-if="!totpStep">
          <n-form-item label="用户名" path="username">
            <n-input v-model:value="form.username" placeholder="用户名" />
          </n-form-item>
          <n-form-item label="密码" path="password">
            <n-input
              v-model:value="form.password"
              type="password"
              placeholder="密码"
              show-password-on="click"
            />
          </n-form-item>
        </template>
        <template v-else>
          <n-form-item label="验证码" path="totpCode">
            <n-input
              v-model:value="form.totpCode"
              placeholder="请输入 6 位动态验证码"
              maxlength="6"
              :allow-input="(v) => /^\d*$/.test(v)"
            />
          </n-form-item>
          <n-text depth="3" style="font-size:12px;display:block;margin-bottom:12px">
            请打开您的身份验证器 App，输入当前显示的 6 位验证码
          </n-text>
        </template>
        <n-alert v-if="errorMsg" type="error" style="margin-bottom:12px">{{ errorMsg }}</n-alert>
        <div style="display:flex;gap:8px">
          <n-button v-if="totpStep" style="flex:1" @click="backToPassword">返回</n-button>
          <n-button type="primary" :style="totpStep ? 'flex:2' : 'width:100%'" :loading="loading" @click="submit">
            {{ totpStep ? '验证' : '登录' }}
          </n-button>
        </div>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import { getConfig } from '../config/app-config.js'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const appTitle = getConfig().appTitle

const formRef = ref(null)
const loading = ref(false)
const errorMsg = ref('')
const totpStep = ref(false)
const form = ref({ username: '', password: '', totpCode: '' })

const activeRules = computed(() => totpStep.value
  ? { totpCode: [{ required: true, message: '请输入验证码', trigger: 'blur' }, { len: 6, message: '验证码为 6 位数字', trigger: 'blur' }] }
  : {
      username: { required: true, message: '请输入用户名', trigger: 'blur' },
      password: { required: true, message: '请输入密码', trigger: 'blur' },
    }
)

function backToPassword() {
  totpStep.value = false
  form.value.totpCode = ''
  errorMsg.value = ''
}

async function submit() {
  try { await formRef.value?.validate() } catch { return }
  loading.value = true
  errorMsg.value = ''
  try {
    const result = await auth.login(
      form.value.username,
      form.value.password,
      totpStep.value ? form.value.totpCode : undefined,
    )
    if (result?.totp_required) {
      totpStep.value = true
      return
    }
    router.push(route.query.redirect || '/documents')
  } catch (e) {
    errorMsg.value = e.message || '登录失败'
    if (totpStep.value) form.value.totpCode = ''
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrapper {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--bg-color);
}
.login-card {
  width: 360px;
}
</style>
