import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import AppLayout from '../components/AppLayout.vue'
import LoginPage from '../pages/LoginPage.vue'
import DocumentsPage from '../pages/DocumentsPage.vue'
import DocumentDetailPage from '../pages/DocumentDetailPage.vue'
import DocumentChunksPage from '../pages/DocumentChunksPage.vue'
import SearchPage from '../pages/SearchPage.vue'

const routes = [
  { path: '/login', component: LoginPage },
  {
    path: '/',
    component: AppLayout,
    meta: { requiresAuth: true },
    children: [
      { path: '', redirect: 'documents' },
      { path: 'documents', component: DocumentsPage },
      { path: 'documents/:documentId', component: DocumentDetailPage },
      { path: 'documents/:documentId/chunks', component: DocumentChunksPage },
      { path: 'search', component: SearchPage },
    ],
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(async (to) => {
  if (!to.meta.requiresAuth) return true
  const auth = useAuthStore()
  if (!auth.user) await auth.fetchMe()
  if (!auth.user) return { path: '/login', query: { redirect: to.fullPath } }
  return true
})

export default router
