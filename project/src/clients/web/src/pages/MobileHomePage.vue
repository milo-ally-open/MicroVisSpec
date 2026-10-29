<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { deviceApi, type Device } from '../api/devices'
import { authApi, type WorkspaceInfo } from '../api/auth'
import { eventApi, type EventRecord } from '../api/events'
import { errorMessage } from '../api/client'
import { formatDate, severityLabel, statusLabel } from '../lib/format'

const devices = ref<Device[]>([])
const events = ref<EventRecord[]>([])
const workspace = ref<WorkspaceInfo | null>(null)
const busy = ref(true)
const error = ref('')

async function load() {
  busy.value = true
  error.value = ''
  const results = await Promise.allSettled([deviceApi.listAll(), eventApi.list({ limit: 10 }), authApi.workspace()])
  if (results[0].status === 'fulfilled') devices.value = results[0].value
  if (results[1].status === 'fulfilled') events.value = results[1].value.items
  if (results[2].status === 'fulfilled') workspace.value = results[2].value
  const failed = results.find((result) => result.status === 'rejected')
  if (failed?.status === 'rejected') error.value = errorMessage(failed.reason)
  busy.value = false
}

function canViewCamera(deviceId: string) {
  return workspace.value?.device_grants?.some((grant) => grant.device_id === deviceId && grant.capabilities.includes('stream.view')) ?? false
}

onMounted(load)
</script>

<template>
  <section class="page-heading"><span class="eyebrow">MOBILE WORKSPACE</span><h1>我的授权设备</h1><p>查看成员已授权的设备、事件和实时视频。</p></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }} <button class="text-button" @click="load">重试</button></div>
  <section class="panel mobile-workspace-panel"><div class="panel-heading"><div><span class="eyebrow">DEVICES</span><h2>设备与摄像头</h2></div></div><p v-if="busy">正在读取授权设备…</p><p v-else-if="!devices.length" class="empty-state">当前没有可查看设备。如需访问，请联系工作空间管理员。</p><article v-for="device in devices" :key="device.device_id" class="mobile-device"><div><strong>{{ device.name }}</strong><small>{{ device.location }} · {{ device.online ? '在线' : '离线' }}</small></div><div v-if="device.cameras.length && canViewCamera(device.device_id)" class="mobile-camera-links"><RouterLink v-for="camera in device.cameras" :key="camera.camera_id" class="button button--secondary" :to="`/m/cameras/${camera.camera_id}`">查看 {{ camera.name }}</RouterLink></div><small v-else-if="!canViewCamera(device.device_id)" class="muted-copy">未授予此设备的视频权限</small><small v-else class="muted-copy">此设备暂无摄像头</small></article></section>
  <section class="panel mobile-workspace-panel"><div class="panel-heading"><div><span class="eyebrow">EVENTS</span><h2>最近事件</h2></div><RouterLink class="inline-link" to="/m/events">全部事件 →</RouterLink></div><p v-if="busy">正在读取事件…</p><p v-else-if="!events.length" class="empty-state">当前授权范围内没有事件。</p><RouterLink v-for="event in events" :key="event.event_id" class="mobile-event" :to="`/m/events/${event.event_id}`"><span class="severity-pill" :class="`severity-pill--${event.severity}`">{{ severityLabel(event.severity) }}</span><span class="mobile-event__copy"><strong>{{ event.device_name }} · {{ event.camera_name }}</strong><small>{{ event.location }} · {{ statusLabel(event.status) }}</small></span><time>{{ formatDate(event.occurred_at, false) }}</time></RouterLink></section>
</template>

<style scoped>
.mobile-workspace-panel { margin-top: 18px; padding: 20px; }
.mobile-device { display: grid; gap: 12px; padding: 16px 0; border-top: 1px solid var(--line); }
.mobile-device > div:first-child, .mobile-event__copy { display: grid; gap: 3px; }
.mobile-device small, .mobile-event small, .muted-copy { color: var(--muted); }
.mobile-camera-links { display: flex; gap: 8px; flex-wrap: wrap; }
.mobile-event { display: flex; align-items: center; gap: 12px; padding: 14px 0; border-top: 1px solid var(--line); }
.mobile-event__copy { min-width: 0; flex: 1; }
.mobile-event__copy strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mobile-event time { color: var(--muted); font-size: 12px; }
</style>
