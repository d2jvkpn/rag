<template>
  <div class="login-wrapper">
    <n-card class="login-card" :title="appTitle">
      <n-form ref="formRef" :model="form" :rules="rules" @keydown.enter="submit">
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
        <n-alert v-if="errorMsg" type="error" style="margin-bottom:12px">{{ errorMsg }}</n-alert>
        <n-button type="primary" block :loading="loading" @click="submit">登录</n-button>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { ref } from 'vue'
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
const form = ref({ username: '', password: '' })

const rules = {
  username: { required: true, message: '请输入用户名', trigger: 'blur' },
  password: { required: true, message: '请输入密码', trigger: 'blur' },
}

async function submit() {
  try { await formRef.value?.validate() } catch { return }
  loading.value = true
  errorMsg.value = ''
  try {
    await auth.login(form.value.username, form.value.password)
    router.push(route.query.redirect || '/documents')
  } catch (e) {
    errorMsg.value = e.message || '登录失败'
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
