<script setup lang="ts">
import AppIcon from '../components/AppIcon.vue'
import { computed, onMounted, ref, watch } from 'vue'
import { deviceApi, type DeviceStats } from '../api/devices'
import { errorMessage } from '../api/client'
import { eventApi, type EventCount, type EventStats } from '../api/events'
import { countLabel, formatDate, severityLabel, statusLabel, behaviorLabel } from '../lib/format'

const range = ref<'today' | '7d' | '30d'>('7d')
const stats = ref<EventStats | null>(null)
const devices = ref<DeviceStats | null>(null)
const busy = ref(false)
const error = ref('')
const rangeLabel = computed(() => ({ today: '今日', '7d': '近 7 天', '30d': '近 30 天' })[range.value])
const trendMax = computed(() => Math.max(1, ...(stats.value?.trend.map((item) => item.count) ?? [])))

async function load() {
  busy.value = true
  error.value = ''
  const [eventResult, deviceResult] = await Promise.allSettled([eventApi.stats(range.value), deviceApi.stats()])
  if (eventResult.status === 'fulfilled') stats.value = eventResult.value
  if (deviceResult.status === 'fulfilled') devices.value = deviceResult.value
  const failed = [eventResult, deviceResult].find((item) => item.status === 'rejected')
  if (failed?.status === 'rejected') error.value = errorMessage(failed.reason)
  busy.value = false
}

function percent(value: number, maximum: number) { return `${Math.max(value > 0 ? 4 : 0, Math.round(value / Math.max(maximum, 1) * 100))}%` }
function maxCount(rows: EventCount[] = []) { return Math.max(1, ...rows.map((row) => row.count)) }
function translated(value: string, type: 'severity' | 'status' | 'behavior') {
  if (type === 'severity') return severityLabel(value)
  if (type === 'status') return statusLabel(value)
  return behaviorLabel(value)
}

watch(range, load)
onMounted(load)
</script>

<template>
  <section class="page-heading page-heading--split"><div><span class="eyebrow">ACCOUNT ANALYTICS</span><h1>数据分析</h1><p>统计只包含规则命中后生成的异常事件，误报仍计入事件历史。</p></div><div class="range-tabs range-tabs--large" role="group" aria-label="统计时间范围"><button v-for="item in [{ key: 'today', label: '今日' }, { key: '7d', label: '近 7 天' }, { key: '30d', label: '近 30 天' }]" :key="item.key" :class="{ 'range-tabs__active': range === item.key }" @click="range = item.key as 'today' | '7d' | '30d'">{{ item.label }}</button></div></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }} <button class="text-button" @click="load">重新加载</button></div>
  <section class="stat-grid stat-grid--analytics"><article class="stat-card"><div class="stat-card__top"><span>{{ rangeLabel }}异常事件</span><span class="stat-icon stat-icon--orange"><AppIcon name="events" /></span></div><strong>{{ busy ? '—' : countLabel(stats?.total_events ?? 0) }}</strong><div class="stat-foot"><span class="stat-muted">按事件发生时间统计</span><small>事件服务 · {{ formatDate(stats?.as_of, false) }}</small></div></article><article class="stat-card"><div class="stat-card__top"><span>当前待跟进</span><span class="stat-icon stat-icon--gold"><AppIcon name="arrowUp" /></span></div><strong>{{ busy ? '—' : countLabel(stats?.open_events ?? 0) }}</strong><div class="stat-foot"><span class="stat-muted">不受时间范围筛选</span><small>待处置与处理中</small></div></article><article class="stat-card"><div class="stat-card__top"><span>登记设备</span><span class="stat-icon stat-icon--blue"><AppIcon name="devices" /></span></div><strong>{{ busy ? '—' : countLabel(devices?.registered ?? 0) }}</strong><div class="stat-foot"><span class="stat-live"><i></i>{{ countLabel(devices?.online ?? 0) }} 台在线</span><small>设备服务 · {{ formatDate(devices?.as_of, false) }}</small></div></article></section>

  <section class="analytics-grid"><article class="panel analytics-trend"><div class="panel-heading"><div><span class="eyebrow">TIME SERIES</span><h2>{{ rangeLabel }}事件趋势</h2></div><span class="as-of-label">{{ stats?.trend.length ?? 0 }} 个统计时段</span></div><div v-if="busy" class="chart-skeleton"></div><div v-else-if="stats?.trend.length" class="bar-chart"><div v-for="item in stats.trend" :key="item.key" class="bar-chart__item" :title="`${item.key}: ${item.count}`"><strong>{{ item.count || '' }}</strong><i :style="{ height: percent(item.count, trendMax) }"></i><small>{{ range === 'today' ? item.key.slice(-5) : item.key.slice(-5) }}</small></div></div><div v-else class="empty-state empty-state--chart"><strong>当前范围没有统计数据</strong><p>事件产生后，趋势将在这里展示。</p></div><div v-if="stats" class="chart-foot"><span><i class="legend-dot"></i>按 Asia/Shanghai 日界线</span><small>{{ formatDate(stats.as_of) }} 更新</small></div></article>
    <article class="panel breakdown-panel"><div class="panel-heading"><div><span class="eyebrow">SEVERITY</span><h2>告警等级分布</h2></div></div><div v-if="stats?.by_severity.length" class="distribution-list"><div v-for="row in stats.by_severity" :key="row.key" class="distribution-row"><div class="distribution-row__label"><span><i :class="`severity-${row.key}`"></i>{{ translated(row.key, 'severity') }}</span><strong>{{ countLabel(row.count) }}</strong></div><div class="progress-track"><i :class="`bar-severity-${row.key}`" :style="{ width: percent(row.count, maxCount(stats.by_severity)) }"></i></div></div></div><div v-else class="empty-state empty-state--compact"><strong>暂无等级数据</strong><p>没有事件时这里保持为空。</p></div></article>
    <article class="panel breakdown-panel"><div class="panel-heading"><div><span class="eyebrow">BEHAVIOR TYPE</span><h2>行为类别</h2></div></div><div v-if="stats?.by_type.length" class="distribution-list"><div v-for="row in stats.by_type" :key="row.key" class="distribution-row"><div class="distribution-row__label"><span>{{ translated(row.key, 'behavior') }}</span><strong>{{ countLabel(row.count) }}</strong></div><div class="progress-track"><i class="bar-teal" :style="{ width: percent(row.count, maxCount(stats.by_type)) }"></i></div></div></div><div v-else class="empty-state empty-state--compact"><strong>暂无类别数据</strong><p>告警规则命中后显示行为类别。</p></div></article>
    <article class="panel breakdown-panel"><div class="panel-heading"><div><span class="eyebrow">DISPOSITION</span><h2>处置状态</h2></div></div><div v-if="stats?.by_status.length" class="distribution-list"><div v-for="row in stats.by_status" :key="row.key" class="distribution-row"><div class="distribution-row__label"><span>{{ translated(row.key, 'status') }}</span><strong>{{ countLabel(row.count) }}</strong></div><div class="progress-track"><i :class="row.key === 'resolved' ? 'bar-green' : row.key === 'false_positive' ? 'bar-gray' : 'bar-gold'" :style="{ width: percent(row.count, maxCount(stats.by_status)) }"></i></div></div></div><div v-else class="empty-state empty-state--compact"><strong>暂无处置数据</strong><p>事件处置后显示状态分布。</p></div></article>
    <article class="panel breakdown-panel"><div class="panel-heading"><div><span class="eyebrow">TOP LOCATIONS</span><h2>高发位置</h2></div></div><div v-if="stats?.by_location.length" class="location-list"><div v-for="(row, index) in stats.by_location" :key="row.key" class="location-row"><span class="location-rank">0{{ index + 1 }}</span><span class="location-name">{{ row.key || '未填写位置' }}</span><strong>{{ countLabel(row.count) }}</strong></div></div><div v-else class="empty-state empty-state--compact"><strong>暂无位置数据</strong><p>事件发生时的位置快照用于统计。</p></div></article>
  </section>
</template>
