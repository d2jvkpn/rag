import { http } from './http.js'

export const usersService = {
  list: () => http.get('/api/users'),
  disable: (userId) => http.post(`/api/users/${userId}/disable`),
  enable: (userId) => http.post(`/api/users/${userId}/enable`),
}
