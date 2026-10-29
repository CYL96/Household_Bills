import { createRouter, createWebHistory } from 'vue-router'
import { authState } from '../stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
      meta: { title: '登录 · 家庭账本' },
    },
    {
      path: '/',
      component: () => import('../layouts/AppLayout.vue'),
      meta: { requiresAuth: true },
      children: [
        {
          path: '',
          name: 'dashboard',
          component: () => import('../views/DashboardView.vue'),
          meta: { title: '总览', requiresAuth: true },
        },
        {
          path: 'transactions',
          name: 'transactions',
          component: () => import('../views/TransactionsView.vue'),
          meta: { title: '收支明细', requiresAuth: true },
        },
        {
          path: 'accounts',
          name: 'accounts',
          component: () => import('../views/AccountsView.vue'),
          meta: { title: '资金账户', requiresAuth: true },
        },
        {
          path: 'members',
          name: 'members',
          component: () => import('../views/MembersView.vue'),
          meta: { title: '家庭成员', requiresAuth: true },
        },
        {
          path: 'users',
          name: 'users',
          component: () => import('../views/UsersView.vue'),
          meta: {
            title: '用户管理',
            requiresAuth: true,
            requiresAdmin: true,
          },
        },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      redirect: '/',
    },
  ],
  scrollBehavior() {
    return { top: 0 }
  },
})

router.beforeEach((to) => {
  const requiresAuth = Boolean(to.meta.requiresAuth)
  if (requiresAuth && !authState.user) {
    return {
      name: 'login',
      query: { redirect: to.fullPath },
    }
  }
  if (to.meta.requiresAdmin && !authState.user?.is_admin) {
    return { name: 'dashboard' }
  }
  if (to.name === 'login' && authState.user) {
    return { name: 'dashboard' }
  }
  return true
})

router.afterEach((to) => {
  const title = typeof to.meta.title === 'string' ? to.meta.title : ''
  document.title =
    title.length === 0
      ? '家庭账本'
      : title.includes('家庭账本')
        ? title
        : `${title} · 家庭账本`
})

export default router
