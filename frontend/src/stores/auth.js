import { defineStore } from 'pinia'
import { ref } from 'vue'
import { authService } from '../services/auth.js'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)

  async function fetchMe() {
    try {
      user.value = await authService.me()
    } catch {
      user.value = null
    }
  }

  async function login(username, password, totpCode) {
    const data = await authService.login(username, password, totpCode)
    if (data?.totp_required) return { totp_required: true }
    user.value = data
    return {}
  }

  async function logout() {
    await authService.logout()
    user.value = null
  }

  return { user, fetchMe, login, logout }
})
