import { http } from './http.js'

export const usersService = {
  list: () => http.get('/api/users'),
  create: (data) => http.post('/api/users', data),
  disable: (userId) => http.post(`/api/users/${userId}/disable`),
  enable: (userId) => http.post(`/api/users/${userId}/enable`),
  setPermissions: (userId, permissions) => http.put(`/api/users/${userId}/permissions`, { permissions }),
  resetPassword: (userId, password) => http.post(`/api/users/${userId}/reset-password`, { password }),
}
