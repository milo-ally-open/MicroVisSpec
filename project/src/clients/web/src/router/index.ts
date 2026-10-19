import { createRouter, createWebHistory } from 'vue-router'
import { ensureSession } from '../state/auth'
import { safeInAppPath } from '../lib/navigation'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../pages/LoginPage.vue'),
      meta: { public: true },
    },
    {
      path: '/register',
      name: 'register',
      component: () => import('../pages/RegisterPage.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: () => import('../layouts/AppShell.vue'),
      meta: { requiresAuth: true },
      children: [
        { path: '', name: 'overview', component: () => import('../pages/OverviewPage.vue') },
        { path: 'devices', name: 'devices', component: () => import('../pages/DevicesPage.vue') },
        { path: 'devices/:deviceId', name: 'device-detail', component: () => import('../pages/DeviceDetailPage.vue') },
        { path: 'monitor', name: 'monitor', component: () => import('../pages/MonitorPage.vue') },
        { path: 'events', name: 'events', component: () => import('../pages/EventsPage.vue') },
        { path: 'events/:eventId', name: 'event-detail', component: () => import('../pages/EventDetailPage.vue') },
        { path: 'analytics', name: 'analytics', component: () => import('../pages/AnalyticsPage.vue') },
        { path: 'settings', name: 'settings', component: () => import('../pages/SettingsPage.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('../pages/NotFoundPage.vue') },
  ],
})

router.beforeEach(async (to) => {
  const authenticated = await ensureSession()
  if (to.meta.public) {
    if (authenticated) return safeInAppPath(to.query.next) ?? '/'
    return true
  }
  if (!authenticated) return { name: 'login', query: { next: to.fullPath } }
  if (to.name === 'monitor' && window.matchMedia('(max-width: 760px)').matches) return { name: 'devices' }
  return true
})

export default router
