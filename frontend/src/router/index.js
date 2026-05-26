import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'

const AppLayout = () => import('../components/AppLayout.vue')
const LoginPage = () => import('../pages/LoginPage.vue')
const DocumentsPage = () => import('../pages/DocumentsPage.vue')
const DocumentDetailPage = () => import('../pages/DocumentDetailPage.vue')
const DocumentChunksPage = () => import('../pages/DocumentChunksPage.vue')
const SearchPage = () => import('../pages/SearchPage.vue')
const UsersPage = () => import('../pages/UsersPage.vue')

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
      { path: 'users', component: UsersPage, meta: { requiresPermission: 'view_user_list' } },
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
  if (to.meta.requiresPermission && !auth.user.permissions?.includes(to.meta.requiresPermission)) {
    return { path: '/documents' }
  }
  return true
})

export default router
