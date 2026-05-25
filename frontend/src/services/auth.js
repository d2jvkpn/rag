import { http } from './http.js'

export const authService = {
  login: (username, password, totpCode) =>
    http.post('/api/login', { username, password, ...(totpCode ? { totp_code: totpCode } : {}) }),
  logout: () => http.post('/api/logout'),
  me: () => http.get('/api/me'),
  changePassword: (oldPassword, newPassword) =>
    http.put('/api/me/password', { old_password: oldPassword, new_password: newPassword }),
  totpSetup: () => http.post('/api/me/totp/setup'),
  totpEnable: (code) => http.post('/api/me/totp/enable', { code }),
  totpDisable: (code) => http.post('/api/me/totp/disable', { code }),
}
