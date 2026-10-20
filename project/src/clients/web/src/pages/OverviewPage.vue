<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { deviceApi, type DeviceStats } from '../api/devices'
import { eventApi, type EventRecord, type EventStats } from '../api/events'
import { errorMessage } from '../api/client'
import { behaviorLabel, countLabel, formatDate, statusLabel } from '../lib/format'
import { authState } from '../state/auth'
import AppIcon from '../components/AppIcon.vue'

const deviceStats = ref<DeviceStats | null>(null)
const eventStats = ref<EventStats | null>(null)
const recentEvents = ref<EventRecord[]>([])
const busy = ref(true)
const error = ref('')
const range = ref<'today' | '7d' | '30d'>('today')
const ranges = [{ key: 'today', label: '今日' }, { key: '7d', label: '近 7 天' }, { key: '30d', label: '近 30 天' }] as const
const rangeCaption = computed(() => ranges.find((item) => item.key === range.value)?.label ?? '今日')
const clock = ref(new Date())
let clockTimer: number | undefined
const currentDate = computed(() => new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: 'long', day: 'numeric', weekday: 'long' }).format(clock.value))
const greeting = computed(() => {
  const hour = Number(new Intl.DateTimeFormat('en-GB', { timeZone: 'Asia/Shanghai', hour: 'numeric', hour12: false }).format(clock.value))
  return hour < 6 || hour >= 18 ? '晚上好' : hour < 12 ? '早上好' : '下午好'
})
const maxTrend = computed(() => Math.max(1, ...(eventStats.value?.trend.map((item) => item.count) ?? [])))
const trendPoints = computed(() => {
  const items = eventStats.value?.trend ?? []
  if (items.length < 2) return ''
  return items.map((item, index) => `${(index / (items.length - 1)) * 100},${92 - (item.count / maxTrend.value) * 76}`).join(' ')
})

async function load() {
  busy.value = true
  error.value = ''
  const results = await Promise.allSettled([deviceApi.stats(), eventApi.stats(range.value), eventApi.list({ limit: 5 })])
  if (results[0].status === 'fulfilled') deviceStats.value = results[0].value
  if (results[1].status === 'fulfilled') eventStats.value = results[1].value
  if (results[2].status === 'fulfilled') recentEvents.value = results[2].value.items
  const failure = results.find((result) => result.status === 'rejected')
  if (failure?.status === 'rejected') error.value = errorMessage(failure.reason)
  busy.value = false
}

async function setRange(value: typeof range.value) {
  range.value = value
  await load()
}

onMounted(() => {
  void load()
  clockTimer = window.setInterval(() => { clock.value = new Date() }, 60000)
})
onBeforeUnmount(() => { if (clockTimer !== undefined) window.clearInterval(clockTimer) })
</script>

<template>
  <section class="overview-hero">
    <div class="overview-hero__copy">
      <span class="hero-date"><AppIcon name="calendar" :size="16" />{{ currentDate }}<span class="hero-date__divider"></span>边缘智能运行总览</span>
      <h1>{{ greeting }}，{{ authState.user?.username }}<span class="greeting-dot">。</span></h1>
      <p v-if="busy">正在为你汇总设备状态和异常事件。</p>
      <p v-else-if="eventStats">{{ eventStats.open_events > 0 ? `还有 ${countLabel(eventStats.open_events)} 条异常事件需要跟进，现场状态尽在掌握。` : '当前没有待跟进事件，随时掌握设备与运行动态。' }}</p>
      <p v-else>在这里查看设备状态，跟进每一条异常事件。</p>
      <div class="hero-actions"><RouterLink class="button button--primary" to="/events">查看异常事件 <AppIcon name="arrow" :size="17" /></RouterLink><RouterLink class="button button--hero" to="/devices"><AppIcon name="devices" :size="17" />管理设备</RouterLink></div>
    </div>
    <div class="hero-scene" aria-hidden="true"><div class="hero-scene__orbit"></div><div class="hero-scene__core"><AppIcon name="monitor" :size="44" /></div><span class="hero-scene__label">边缘 AI 感知</span><div class="hero-behaviors"><span><AppIcon name="devices" :size="16" />设备状态</span><span><AppIcon name="monitor" :size="16" />实时视频</span><span><AppIcon name="events" :size="16" />智能告警</span></div></div>
  </section>
  <div v-if="error" class="notice notice--error" role="alert"><span>服务连接暂不可用</span><p>{{ error }}</p><button class="text-button" @click="load">重新加载</button></div>

  <section class="stat-grid" aria-label="运行指标">
    <article class="stat-card"><div class="stat-card__top"><span>已登记设备</span><span class="stat-icon stat-icon--blue"><AppIcon name="devices" /></span></div><strong>{{ busy ? '—' : countLabel(deviceStats?.registered ?? 0) }}<small>台</small></strong><div class="stat-foot"><span class="stat-live" :class="{ 'stat-live--idle': !deviceStats?.online }"><i></i>{{ deviceStats ? `${countLabel(deviceStats.online)} 台在线` : '等待设备数据' }}</span><small>{{ formatDate(deviceStats?.as_of, false) }} 更新</small></div></article>
    <article class="stat-card"><div class="stat-card__top"><span>{{ rangeCaption }}异常事件</span><span class="stat-icon stat-icon--orange"><AppIcon name="events" /></span></div><strong>{{ busy ? '—' : countLabel(eventStats?.total_events ?? 0) }}<small>条</small></strong><div class="stat-foot"><span class="stat-muted">已命中规则的事件</span><small>{{ formatDate(eventStats?.as_of, false) }} 更新</small></div></article>
    <article class="stat-card stat-card--attention"><div class="stat-card__top"><span>待跟进事件</span><span class="stat-icon stat-icon--gold"><AppIcon name="arrowUp" /></span></div><strong>{{ busy ? '—' : countLabel(eventStats?.open_events ?? 0) }}<small>条</small></strong><div class="stat-foot"><span class="stat-muted">待处置与处理中</span><RouterLink class="inline-link" to="/events?status=pending">进入处置 <AppIcon name="arrow" :size="14" /></RouterLink></div></article>
    <article class="stat-card stat-card--model"><div class="stat-card__top"><span>智能告警规则</span><span class="stat-icon stat-icon--green"><AppIcon name="shield" /></span></div><strong>按设备配置</strong><div class="stat-foot"><span class="stat-muted">按算法与场景设置</span><RouterLink class="inline-link" to="/devices">管理规则 <AppIcon name="arrow" :size="14" /></RouterLink></div></article>
  </section>

  <section class="overview-grid">
    <article class="panel trend-panel"><div class="panel-heading"><div><span class="eyebrow">EVENT ACTIVITY</span><h2>异常事件趋势</h2></div><div class="range-tabs" role="group" aria-label="统计时间范围"><button v-for="item in ranges" :key="item.key" :class="{ 'range-tabs__active': range === item.key }" @click="setRange(item.key)">{{ item.label }}</button></div></div>
      <div v-if="busy" class="chart-skeleton"></div>
      <div v-else-if="eventStats?.trend.length" class="chart-wrap"><div class="chart-y-labels"><span>{{ maxTrend }}</span><span>{{ Math.round(maxTrend / 2) }}</span><span>0</span></div><svg class="trend-chart" viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="按时间统计的异常事件趋势"><defs><linearGradient id="trend-fill" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stop-color="#48a79a" stop-opacity=".2"/><stop offset="1" stop-color="#48a79a" stop-opacity="0"/></linearGradient></defs><path v-if="trendPoints" :d="`M ${trendPoints.replaceAll(' ', ' L ')} L 100 100 L 0 100 Z`" fill="url(#trend-fill)"/><polyline :points="trendPoints" fill="none" stroke="#399d91" stroke-width="1.4" vector-effect="non-scaling-stroke"/><line x1="0" y1="92" x2="100" y2="92" stroke="#e6eceb" stroke-width=".5" vector-effect="non-scaling-stroke"/></svg><div class="chart-x-labels"><span>{{ eventStats.trend[0]?.key }}</span><span>{{ eventStats.trend[Math.floor(eventStats.trend.length / 2)]?.key }}</span><span>{{ eventStats.trend.at(-1)?.key }}</span></div></div>
      <div v-else class="empty-state empty-state--chart"><span class="empty-state__icon">⌁</span><strong>当前范围没有异常事件</strong><p>启用的规则命中后，事件趋势会显示在这里。</p></div>
      <div class="chart-foot"><span><i class="legend-dot"></i>异常事件</span><small>数据按 Asia/Shanghai 统计 · {{ formatDate(eventStats?.as_of, false) }} 更新</small></div>
    </article>

    <article class="panel recent-panel"><div class="panel-heading"><div><span class="eyebrow">FOLLOW UP</span><h2>最近异常</h2></div><RouterLink class="inline-link" to="/events">全部事件 <span aria-hidden="true">→</span></RouterLink></div>
      <div v-if="busy" class="loading-row">正在读取事件服务…</div>
      <div v-else-if="!recentEvents.length" class="empty-state empty-state--compact"><span class="empty-state__icon"><AppIcon name="events" /></span><strong>暂时没有异常事件</strong><p>规则命中后，新的事件会显示在这里。</p></div>
      <RouterLink v-for="item in recentEvents" :key="item.event_id" class="event-row" :to="`/events/${item.event_id}`"><span class="event-row__severity" :class="`severity-${item.severity}`"></span><span class="event-row__main"><strong>{{ behaviorLabel(item.event_type) }}</strong><small>{{ item.location }} · {{ item.device_name }}</small></span><span class="event-row__side"><small>{{ formatDate(item.occurred_at, false) }}</small><em :class="`status-${item.status}`">{{ statusLabel(item.status) }}</em></span></RouterLink>
    </article>
  </section>

  <section class="bottom-banner"><div><span class="bottom-banner__mark"><AppIcon name="shield" :size="23" /></span><div><strong>从设备连接，到每一次现场响应</strong><p>设备在线、视频连接、模型运行与规则应用状态，均可在设备详情中分别查看。</p></div></div><RouterLink to="/devices">管理边缘设备 <AppIcon name="arrow" :size="16" /></RouterLink></section>
</template>
