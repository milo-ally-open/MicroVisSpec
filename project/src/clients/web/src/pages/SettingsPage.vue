<script setup lang="ts">
import AppIcon from '../components/AppIcon.vue'
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { authApi } from '../api/auth'
import { errorMessage } from '../api/client'
import { pushApi } from '../api/push'
import { clearSession, authState } from '../state/auth'
import { syncCurrentBrowserPushSubscription } from '../lib/pushSubscription'

const router = useRouter()
const available = computed(() => typeof window !== 'undefined' && window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window)
const permission = ref(typeof Notification === 'undefined' ? 'unsupported' : Notification.permission)
const subscriptionId = ref('')
const pushBusy = ref(false)
const passwordBusy = ref(false)
const pushMessage = ref('')
const pushError = ref('')
const passwordMessage = ref('')
const passwordError = ref('')
const password = ref({ old: '', next: '', confirmation: '' })
const isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent)
const isStandalone = window.matchMedia('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true

async function refreshSubscription() {
  pushMessage.value = ''
  if (!available.value) return
  try {
    subscriptionId.value = await syncCurrentBrowserPushSubscription() ?? ''
    if (subscriptionId.value) {
      pushMessage.value = '此浏览器的通知订阅已与当前账号同步。'
    }
  } catch (cause) { pushError.value = errorMessage(cause) }
}

function decodeVapidKey(value: string) {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - value.length % 4) % 4)
  return Uint8Array.from(atob(padded), (character) => character.charCodeAt(0))
}

async function enablePush() {
  pushBusy.value = true
  pushError.value = ''
  pushMessage.value = ''
  try {
    if (!available.value) throw new Error('当前浏览器或连接环境不支持 Web Push。请使用 HTTPS 和支持的浏览器。')
    const granted = await Notification.requestPermission()
    permission.value = granted
    if (granted !== 'granted') throw new Error(granted === 'denied' ? '通知权限已被浏览器拒绝，请在浏览器设置中重新允许。' : '尚未授予通知权限。')
    const registration = await navigator.serviceWorker.ready
    let browserSubscription = await registration.pushManager.getSubscription()
    if (!browserSubscription) {
      const { vapid_public_key: vapidKey } = await pushApi.vapidKey()
      browserSubscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: decodeVapidKey(vapidKey) })
    }
    const saved = await pushApi.save(browserSubscription.toJSON())
    subscriptionId.value = saved.subscription_id
    pushMessage.value = '本浏览器已订阅通知。点击通知后仍会重新验证登录状态和事件权限。'
  } catch (cause) { pushError.value = errorMessage(cause) } finally { pushBusy.value = false }
}

async function disablePush() {
  pushBusy.value = true
  pushError.value = ''
  pushMessage.value = ''
  try {
    const registration = await navigator.serviceWorker.ready
    const browserSubscription = await registration.pushManager.getSubscription()
    const server = await pushApi.list()
    await Promise.all(server.items.map((item) => pushApi.remove(item.subscription_id)))
    if (browserSubscription) await browserSubscription.unsubscribe()
    subscriptionId.value = ''
    pushMessage.value = '本浏览器的通知订阅已关闭。'
  } catch (cause) { pushError.value = errorMessage(cause) } finally { pushBusy.value = false }
}

async function changePassword() {
  passwordError.value = ''
  passwordMessage.value = ''
  if (password.value.next.length < 12) { passwordError.value = '新密码至少需要 12 个字符。'; return }
  if (password.value.next !== password.value.confirmation) { passwordError.value = '两次输入的新密码不一致。'; return }
  passwordBusy.value = true
  try {
    await authApi.changePassword(password.value.old, password.value.next, password.value.confirmation)
    clearSession()
    passwordMessage.value = '密码已更新，旧会话已撤销。请使用新密码重新登录。'
    password.value = { old: '', next: '', confirmation: '' }
    await router.replace('/login')
  } catch (cause) { passwordError.value = errorMessage(cause) } finally { passwordBusy.value = false }
}

onMounted(refreshSubscription)
</script>

<template>
  <section class="page-heading"><span class="eyebrow">ACCOUNT & NOTIFICATIONS</span><h1>账号与通知</h1><p>管理当前个人账号、安全会话和这台设备的后台告警通知。</p></section>
  <section class="settings-grid"><article class="panel settings-profile"><div class="settings-profile__identity"><span class="avatar avatar--large">{{ authState.user?.username.slice(0, 1).toUpperCase() ?? 'U' }}</span><div><span class="eyebrow">PERSONAL WORKSPACE</span><h2>{{ authState.user?.username }}</h2><small>设备、事件和通知订阅按当前账号隔离</small></div></div><div class="settings-info-row"><span>登录方式</span><strong>安全会话 Cookie</strong></div><div class="settings-info-row"><span>账号 ID</span><code>{{ authState.user?.account_id }}</code></div><p class="privacy-note">会话标识仅在当前页面内存中用于请求。平台不把访问令牌写入浏览器持久存储。</p></article>
    <article class="panel push-settings"><div class="panel-heading"><div><span class="eyebrow">WEB PUSH</span><h2>手机告警通知</h2></div><span class="push-icon"><AppIcon name="events" /></span></div><p class="panel-intro">只有已启用且命中的告警规则会触发通知。推送内容不包含截图，点击后需由登录态重新读取事件。</p><div class="push-status"><span class="push-status__indicator" :class="subscriptionId ? 'push-status__indicator--on' : ''"><i></i></span><div><strong>{{ !available ? '当前浏览器不可用' : permission === 'denied' ? '通知权限已拒绝' : subscriptionId ? '通知已开启' : '通知尚未开启' }}</strong><small>{{ !available ? '请在 HTTPS 环境使用支持 Web Push 的浏览器。' : subscriptionId ? '此浏览器的订阅已绑定当前账号。' : '你可以选择为当前浏览器开启通知。' }}</small></div></div><p v-if="isIOS && !isStandalone" class="notice notice--soft">iPhone / iPad 需先将云哨添加到主屏幕，再开启通知。</p><div class="push-actions"><button class="button button--primary" :disabled="!available || pushBusy || Boolean(subscriptionId) || permission === 'denied'" @click="enablePush">{{ pushBusy ? '处理中…' : subscriptionId ? '通知已订阅' : '开启此设备通知' }}</button><button v-if="subscriptionId" class="button button--secondary" :disabled="pushBusy" @click="disablePush">关闭通知</button><button v-if="!subscriptionId && available" class="text-button" @click="refreshSubscription">同步订阅状态</button></div><p v-if="pushMessage" class="form-success" role="status">{{ pushMessage }}</p><p v-if="pushError" class="form-error" role="alert">{{ pushError }}</p><small v-if="permission === 'default'" class="settings-footnote">点击上方按钮后，浏览器会询问通知权限。权限请求不会在页面加载时自动弹出。</small></article>
    <article class="panel password-settings"><div class="panel-heading"><div><span class="eyebrow">SECURITY</span><h2>修改密码</h2></div><span class="security-icon"><AppIcon name="lock" /></span></div><p class="panel-intro">需要验证旧密码。修改后现有会话会被撤销，并要求重新登录。</p><form class="form-stack" @submit.prevent="changePassword"><label class="field"><span>当前密码</span><input v-model="password.old" type="password" autocomplete="current-password" required></label><div class="field-row"><label class="field"><span>新密码</span><input v-model="password.next" type="password" autocomplete="new-password" minlength="12" required></label><label class="field"><span>确认新密码</span><input v-model="password.confirmation" type="password" autocomplete="new-password" minlength="12" required></label></div><p v-if="passwordError" class="form-error" role="alert">{{ passwordError }}</p><p v-if="passwordMessage" class="form-success" role="status">{{ passwordMessage }}</p><button class="button button--secondary" type="submit" :disabled="passwordBusy">{{ passwordBusy ? '正在更新…' : '更新密码' }}</button></form></article>
    <article class="panel pwa-settings"><div class="panel-heading"><div><span class="eyebrow">PWA LIFECYCLE</span><h2>应用与隐私</h2></div><span class="pwa-icon">⌂</span></div><ul class="privacy-list"><li><span>✓</span>离线时只提供静态应用壳，不显示过期账号数据。</li><li><span>✓</span>API、截图和视频流始终走网络，不进入离线缓存。</li><li><span>✓</span>通知点击打开站内事件地址，并重新检查当前登录状态。</li></ul><p class="settings-footnote">新版本可通过页面提示安全刷新。设备配置、处置和账号状态均从服务端重新载入。</p></article></section>
</template>
