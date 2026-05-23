import { http } from './http.js'

export const authService = {
  login: (username, password) => http.post('/api/login', { username, password }),
  logout: () => http.post('/api/logout'),
  me: () => http.get('/api/me'),
  changePassword: (oldPassword, newPassword) =>
    http.put('/api/me/password', { old_password: oldPassword, new_password: newPassword }),
}
