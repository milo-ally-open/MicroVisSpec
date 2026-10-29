import { createRouter, createWebHistory } from 'vue-router'
import { authState, ensureSession } from '../state/auth'
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
      path: '/invite/redeem',
      name: 'invite-redeem',
      component: () => import('../pages/InviteRedeemPage.vue'),
      meta: { public: true },
    },
    {
      path: '/admin',
      name: 'admin-home',
      component: () => import('../pages/AdminPage.vue'),
      meta: { requiresAuth: true, roles: ['admin'] },
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
        { path: 'workspace', name: 'workspace', component: () => import('../pages/WorkspacePage.vue') },
      ],
    },
    {
      path: '/m',
      component: () => import('../layouts/AppShell.vue'),
      meta: { requiresAuth: true, roles: ['mobile'] },
      children: [
        { path: '', name: 'mobile-home', component: () => import('../pages/MobileHomePage.vue') },
        { path: 'events', name: 'mobile-events', component: () => import('../pages/MobileEventsPage.vue') },
        { path: 'events/:eventId', name: 'mobile-event-detail', component: () => import('../pages/EventDetailPage.vue') },
        { path: 'cameras/:cameraId', name: 'mobile-camera', component: () => import('../pages/MobileCameraPage.vue') },
        { path: 'settings', name: 'mobile-settings', component: () => import('../pages/SettingsPage.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('../pages/NotFoundPage.vue') },
  ],
})

router.beforeEach(async (to) => {
  const authenticated = await ensureSession()
  if (to.meta.public) {
    if (to.name === 'invite-redeem') return true
    if (authenticated) return safeInAppPath(to.query.next) ?? '/'
    return true
  }
  if (!authenticated) return { name: 'login', query: { next: to.fullPath } }
  const role = authState.user?.user_type
  if (role === 'mobile' && !to.path.startsWith('/m')) return { path: '/m' }
  if (role === 'pc' && to.path.startsWith('/m')) return { path: '/' }
  if (role === 'admin' && !to.path.startsWith('/admin')) return { path: '/admin' }
  if (role !== 'admin' && to.path.startsWith('/admin')) return role === 'mobile' ? { path: '/m' } : { path: '/' }
  return true
})

export default router
