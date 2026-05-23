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

  async function login(username, password) {
    const data = await authService.login(username, password)
    user.value = data
  }

  async function logout() {
    await authService.logout()
    user.value = null
  }

  return { user, fetchMe, login, logout }
})
