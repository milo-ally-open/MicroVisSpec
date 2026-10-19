<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { authApi } from '../api/auth'
import { ApiError } from '../api/client'
import { clearSession, authState } from '../state/auth'

const route = useRoute()
const router = useRouter()
const signingOut = ref(false)
const signOutError = ref('')
const title = computed(() => {
  if (route.name === 'device-detail') return '设备详情'
  if (route.name === 'event-detail') return '事件详情'
  return ({
    overview: '运行总览', devices: '边缘设备', monitor: '实时监控', events: '异常事件', analytics: '数据分析', settings: '账号与通知',
  } as Record<string, string>)[String(route.name)] ?? '云哨工作台'
})

async function signOut() {
  signOutError.value = ''
  signingOut.value = true
  let loggedOut = false
  try {
    try {
      const registration = await navigator.serviceWorker?.getRegistration()
      const subscription = await registration?.pushManager.getSubscription()
      if (subscription) {
        const { pushApi } = await import('../api/push')
        const serverSubscriptions = await pushApi.list()
        await Promise.all(serverSubscriptions.items.map((item) => pushApi.remove(item.subscription_id)))
        await subscription.unsubscribe()
      }
    } catch {
      // Session revocation is the server-side compensation for an unavailable browser request.
    }
    await authApi.logout()
    loggedOut = true
  } catch (cause) {
    if (cause instanceof ApiError && cause.status === 401) {
      loggedOut = true
    } else {
      signOutError.value = '退出请求未能确认。请保持页面在线后重试。'
    }
  } finally {
    signingOut.value = false
  }
  if (loggedOut) {
    clearSession()
    await router.replace({ name: 'login' })
  }
}

const primaryLinks = [
  { name: 'overview', label: '运行总览', icon: '◫', desktop: true },
  { name: 'monitor', label: '实时监控', icon: '◉', desktop: true },
  { name: 'events', label: '异常事件', icon: '◎', desktop: true },
  { name: 'analytics', label: '数据分析', icon: '▥', desktop: true },
  { name: 'devices', label: '边缘设备', icon: '⌘', desktop: true },
]
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <RouterLink class="brand" :to="{ name: 'overview' }" aria-label="云哨首页">
        <span class="brand-mark">Y</span>
        <span><strong>云哨</strong><small>EDGE INTELLIGENCE</small></span>
      </RouterLink>

      <div class="workspace-label">工作空间</div>
      <nav class="side-nav" aria-label="主导航">
        <RouterLink v-for="item in primaryLinks" :key="item.name" :to="{ name: item.name }" class="nav-link" :class="{ 'nav-link--active': route.name === item.name || (item.name === 'events' && route.name === 'event-detail') || (item.name === 'devices' && route.name === 'device-detail') }">
          <span class="nav-icon" aria-hidden="true">{{ item.icon }}</span><span>{{ item.label }}</span>
          <span v-if="item.name === 'events'" class="nav-dot" aria-hidden="true"></span>
        </RouterLink>
      </nav>

      <div class="sidebar-bottom">
        <div class="connection-card"><span class="connection-dot"></span><div><strong>云边连接</strong><small>由设备状态持续更新</small></div></div>
        <RouterLink class="nav-link" :to="{ name: 'settings' }" :class="{ 'nav-link--active': route.name === 'settings' }"><span class="nav-icon">⚙</span><span>账号与通知</span></RouterLink>
        <button class="profile-button" type="button" @click="signOut" :disabled="signingOut">
          <span class="avatar">{{ authState.user?.username.slice(0, 1).toUpperCase() || 'U' }}</span>
          <span class="profile-copy"><strong>{{ authState.user?.username || '当前账号' }}</strong><small>{{ signingOut ? '正在退出…' : '个人工作空间' }}</small></span>
          <span class="profile-exit" aria-hidden="true">↗</span>
        </button>
      </div>
    </aside>

    <div class="main-column">
      <header class="topbar">
        <div class="breadcrumb"><span>云哨平台</span><span class="crumb-separator">/</span><strong>{{ title }}</strong></div>
        <div class="topbar-right"><span class="topbar-status"><span class="status-dot status-dot--muted"></span>数据来自实时服务</span><span class="topbar-divider"></span><RouterLink class="topbar-settings" :to="{ name: 'settings' }">{{ authState.user?.username }}</RouterLink></div>
      </header>
      <main class="page-content"><div v-if="signOutError" class="notice notice--error" role="alert">{{ signOutError }}</div><RouterView /></main>
    </div>

    <nav class="mobile-nav" aria-label="手机导航">
      <RouterLink v-for="item in [{ name: 'overview', label: '总览', icon: '◫' }, { name: 'events', label: '事件', icon: '◎' }, { name: 'devices', label: '设备', icon: '⌘' }, { name: 'settings', label: '我的', icon: '○' }]" :key="item.name" :to="{ name: item.name }" :class="{ 'mobile-nav__active': route.name === item.name || (item.name === 'events' && route.name === 'event-detail') || (item.name === 'devices' && route.name === 'device-detail') }">
        <span>{{ item.icon }}</span><small>{{ item.label }}</small>
      </RouterLink>
    </nav>
  </div>
</template>
