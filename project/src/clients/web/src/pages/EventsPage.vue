<script setup lang="ts">
import AppIcon from '../components/AppIcon.vue'
import { onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { deviceApi, type Device } from '../api/devices'
import { ApiError, errorMessage } from '../api/client'
import { eventApi, type EventRecord } from '../api/events'
import { behaviorLabel, formatDate, severityLabel, statusLabel } from '../lib/format'

const route = useRoute()
const router = useRouter()
const events = ref<EventRecord[]>([])
const devices = ref<Device[]>([])
const nextCursor = ref<string | null>(null)
const pageIndex = ref(0)
const busy = ref(true)
const error = ref('')
const filters = reactive({ startDate: '', endDate: '', eventType: '', severity: '', deviceId: '', location: '', status: '' })

function dateStart(value: string) { return value ? `${value}T00:00:00+08:00` : undefined }
function dateEnd(value: string) {
  if (!value) return undefined
  const [year, month, day] = value.split('-').map(Number)
  const next = new Date(Date.UTC(year, month - 1, day + 1))
  return `${next.toISOString().slice(0, 10)}T00:00:00+08:00`
}
function currentFilters() {
  return { start_at: dateStart(filters.startDate), end_at: dateEnd(filters.endDate), event_type: filters.eventType || undefined, severity: filters.severity || undefined, device_id: filters.deviceId || undefined, location: filters.location || undefined, status: filters.status || undefined }
}
function syncFromUrl() {
  filters.startDate = typeof route.query.start_date === 'string' ? route.query.start_date : ''
  filters.endDate = typeof route.query.end_date === 'string' ? route.query.end_date : ''
  filters.eventType = typeof route.query.event_type === 'string' ? route.query.event_type : ''
  filters.severity = typeof route.query.severity === 'string' ? route.query.severity : ''
  filters.deviceId = typeof route.query.device_id === 'string' ? route.query.device_id : ''
  filters.location = typeof route.query.location === 'string' ? route.query.location : ''
  filters.status = typeof route.query.status === 'string' ? route.query.status : ''
}

async function load(cursor?: string) {
  busy.value = true
  error.value = ''
  try {
    const result = await eventApi.list({ ...currentFilters(), cursor, limit: 20 })
    events.value = result.items
    nextCursor.value = result.next_cursor
    if (!cursor) pageIndex.value = 0
    else pageIndex.value += 1
  } catch (cause) {
    error.value = errorMessage(cause)
    if (cause instanceof ApiError && cause.status === 422 && cursor) { nextCursor.value = null; pageIndex.value = 0 }
  } finally { busy.value = false }
}

async function applyFilters() {
  const query: Record<string, string> = {}
  if (filters.startDate) query.start_date = filters.startDate
  if (filters.endDate) query.end_date = filters.endDate
  if (filters.eventType) query.event_type = filters.eventType
  if (filters.severity) query.severity = filters.severity
  if (filters.deviceId) query.device_id = filters.deviceId
  if (filters.location) query.location = filters.location
  if (filters.status) query.status = filters.status
  await router.replace({ name: 'events', query })
  await load()
}

function clearFilters() {
  Object.assign(filters, { startDate: '', endDate: '', eventType: '', severity: '', deviceId: '', location: '', status: '' })
  void applyFilters()
}

function loadNextPage() {
  void load(nextCursor.value ?? undefined)
}

function reload() {
  void load()
}

onMounted(async () => {
  syncFromUrl()
  const [deviceResult] = await Promise.allSettled([deviceApi.list()])
  if (deviceResult.status === 'fulfilled') devices.value = deviceResult.value.items
  await load()
})
</script>

<template>
  <section class="page-heading page-heading--split"><div><span class="eyebrow">EVENT INBOX</span><h1>异常事件</h1><p>集中查看、追溯和处理规则命中的课堂异常事件。</p></div><span class="event-source-note"><i class="status-dot status-dot--green"></i>事件由边缘规则触发</span></section>
  <section class="panel filters-panel"><div class="filters-heading"><div><strong>筛选事件</strong><small>筛选条件保存在地址中，可分享给同账号用户</small></div><button class="text-button" @click="clearFilters">清除筛选</button></div><form class="filters-grid" @submit.prevent="applyFilters"><label class="field"><span>开始日期</span><input v-model="filters.startDate" type="date"></label><label class="field"><span>结束日期</span><input v-model="filters.endDate" type="date"></label><label class="field"><span>行为类别</span><select v-model="filters.eventType"><option value="">全部类别</option><option value="classroom.behavior.hand-raising">举手行为</option><option value="classroom.behavior.reading">阅读行为</option><option value="classroom.behavior.writing">书写行为</option></select></label><label class="field"><span>告警等级</span><select v-model="filters.severity"><option value="">全部等级</option><option value="low">低</option><option value="medium">中</option><option value="high">高</option><option value="critical">紧急</option></select></label><label class="field"><span>设备</span><select v-model="filters.deviceId"><option value="">全部设备</option><option v-for="device in devices" :key="device.device_id" :value="device.device_id">{{ device.name }}</option></select></label><label class="field"><span>处置状态</span><select v-model="filters.status"><option value="">全部状态</option><option value="pending">待处置</option><option value="processing">处理中</option><option value="resolved">已解除</option><option value="false_positive">误报</option></select></label><label class="field filters-location"><span>发生位置</span><input v-model="filters.location" placeholder="搜索位置"></label><button class="button button--primary filters-submit" type="submit">应用筛选 <span aria-hidden="true">→</span></button></form></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }} <button class="text-button" @click="() => load()">重试</button></div>
  <section class="event-list-panel panel"><div class="list-toolbar"><div><h2>事件记录 <span>{{ events.length }}</span></h2><p>按发生时间从近到远排序 · 单页 20 条</p></div><button class="button button--quiet" @click="reload" :disabled="busy"><AppIcon name="refresh" :size="16" />刷新</button></div>
    <div class="event-table-wrap"><table class="event-table"><thead><tr><th>行为事件</th><th>位置 / 设备</th><th>告警等级</th><th>发生时间</th><th>处置状态</th><th></th></tr></thead><tbody><tr v-if="busy"><td colspan="6" class="table-message">正在从事件服务读取记录…</td></tr><tr v-else-if="!events.length"><td colspan="6"><div class="empty-state empty-state--table"><span class="empty-state__icon"><AppIcon name="events" /></span><strong>没有匹配的异常事件</strong><p>你可以修改筛选条件，或检查设备上的告警规则是否已启用并应用。</p></div></td></tr><tr v-for="event in events" v-else :key="event.event_id" class="event-table__row" @click="router.push(`/events/${event.event_id}`)"><td><span class="behavior-cell"><i :class="`severity-${event.severity}`"></i><span><strong>{{ behaviorLabel(event.event_type) }}</strong><small>规则版本 {{ event.rule_config_version }} · {{ event.camera_name }}</small></span></span></td><td><strong>{{ event.location }}</strong><small>{{ event.device_name }}</small></td><td><span class="severity-pill" :class="`severity-pill--${event.severity}`">{{ severityLabel(event.severity) }}</span></td><td><strong>{{ formatDate(event.occurred_at) }}</strong><small>接收于 {{ formatDate(event.received_at, false) }}</small></td><td><span class="status-pill status-pill--plain" :class="`status-pill--${event.status}`">{{ statusLabel(event.status) }}</span></td><td><RouterLink class="row-arrow" :to="`/events/${event.event_id}`" :aria-label="`查看 ${behaviorLabel(event.event_type)}详情`">→</RouterLink></td></tr></tbody></table></div>
    <div class="pagination"><span>第 {{ pageIndex + 1 }} 页</span><button class="button button--secondary" :disabled="busy || !nextCursor" @click="loadNextPage">下一页 <span aria-hidden="true">→</span></button></div>
  </section>
</template>
