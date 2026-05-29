
<script setup>
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import { getConfig } from '../config/app-config.js'
import { useI18n } from '../i18n/index.js'
import { LOCALES } from '../stores/locale.js'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const { t, locale, setLocale } = useI18n()

const appTitle = computed(() => t('appTitle'))

const langOptions = LOCALES.map(l => ({ label: l.label, key: l.value }))
const currentLangLabel = computed(() => LOCALES.find(l => l.value === locale.value)?.label ?? locale.value)

const formRef = ref(null)
const loading = ref(false)
const errorMsg = ref('')
const totpStep = ref(false)
const form = ref({ username: '', password: '', totpCode: '' })

const activeRules = computed(() => totpStep.value
  ? {
      totpCode: [
        { required: true, message: t('login.rules.totpCode'), trigger: 'blur' },
        { len: 6, message: t('login.rules.totpLen'), trigger: 'blur' },
      ],
    }
  : {
      username: { required: true, message: t('login.rules.username'), trigger: 'blur' },
      password: { required: true, message: t('login.rules.password'), trigger: 'blur' },
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
    errorMsg.value = e.message || t('login.failed')
    if (totpStep.value) form.value.totpCode = ''
  } finally {
    loading.value = false
  }
}
</script>


<template>
  <div class="login-wrapper">
    <div class="lang-switcher">
      <n-dropdown :options="langOptions" trigger="click" @select="setLocale">
        <n-button text size="small">{{ currentLangLabel }}</n-button>
      </n-dropdown>
    </div>
    <n-card class="login-card" :title="appTitle">
      <n-form ref="formRef" :model="form" :rules="activeRules" @keydown.enter="submit">
        <template v-if="!totpStep">
          <n-form-item :label="t('login.username')" path="username">
            <n-input v-model:value="form.username" :placeholder="t('login.username')" />
          </n-form-item>
          <n-form-item :label="t('login.password')" path="password">
            <n-input
              v-model:value="form.password"
              type="password"
              :placeholder="t('login.password')"
              show-password-on="click"
            />
          </n-form-item>
        </template>
        <template v-else>
          <n-form-item :label="t('login.totpCode')" path="totpCode">
            <n-input
              v-model:value="form.totpCode"
              :placeholder="t('login.totpPlaceholder')"
              maxlength="6"
              :allow-input="(v) => /^\d*$/.test(v)"
            />
          </n-form-item>
          <n-text depth="3" style="font-size:12px;display:block;margin-bottom:12px">
            {{ t('login.totpHint') }}
          </n-text>
        </template>
        <n-alert v-if="errorMsg" type="error" style="margin-bottom:12px">{{ errorMsg }}</n-alert>
        <div style="display:flex;gap:8px">
          <n-button v-if="totpStep" style="flex:1" @click="backToPassword">{{ t('login.back') }}</n-button>
          <n-button type="primary" :style="totpStep ? 'flex:2' : 'width:100%'" :loading="loading" @click="submit">
            {{ totpStep ? t('login.verify') : t('login.login') }}
          </n-button>
        </div>
      </n-form>
    </n-card>
  </div>
</template>


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
.lang-switcher {
  position: fixed;
  top: 16px;
  right: 20px;
}
</style>
