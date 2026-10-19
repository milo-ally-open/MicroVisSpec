<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { deviceApi, type Camera, type Device } from '../api/devices'
import { mediaApi, type CameraStatus } from '../api/media'
import { errorMessage } from '../api/client'
import CameraPlayer from '../components/CameraPlayer.vue'

const devices = ref<Device[]>([])
const cameraStatuses = ref<Record<string, CameraStatus>>({})
const busy = ref(true)
const error = ref('')
const selectedDevice = ref('all')
const cameras = computed(() => devices.value.flatMap((device) => device.cameras.filter((camera) => camera.active).map((camera) => ({ camera, device }))).filter(({ device }) => selectedDevice.value === 'all' || device.device_id === selectedDevice.value).slice(0, 12))

async function load() {
  busy.value = true
  error.value = ''
  try {
    devices.value = (await deviceApi.list()).items
    const entries = devices.value.flatMap((device) => device.cameras.map(async (camera: Camera) => {
      try { cameraStatuses.value[camera.camera_id] = await mediaApi.status(camera.camera_id) } catch { /* Tile shows unavailable without inventing a source state. */ }
    }))
    await Promise.all(entries)
  } catch (cause) { error.value = errorMessage(cause) } finally { busy.value = false }
}

onMounted(load)
</script>

<template>
  <section class="page-heading page-heading--split"><div><span class="eyebrow">LIVE CLASSROOM</span><h1>实时监控</h1><p>仅播放媒体服务授权的处理后视频。关闭页面或切换页面时自动释放观看租约。</p></div><div class="monitor-actions"><label class="select-field"><span class="sr-only">筛选设备</span><select v-model="selectedDevice"><option value="all">全部设备</option><option v-for="device in devices" :key="device.device_id" :value="device.device_id">{{ device.name }}</option></select></label><button class="button button--secondary" @click="load" :disabled="busy">↻ 刷新</button></div></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }}</div>
  <div class="monitor-summary"><span><i class="status-dot status-dot--green"></i>设备在线状态独立于视频播放状态</span><span>当前显示 {{ cameras.length }} / 最多 12 路</span><span class="monitor-summary__mobile">监控大屏请使用桌面浏览器</span></div>
  <section v-if="busy" class="monitor-grid"><div v-for="n in 4" :key="n" class="panel monitor-skeleton"></div></section>
  <section v-else-if="!cameras.length" class="panel empty-state empty-state--large"><span class="empty-state__icon">◉</span><strong>没有可播放的摄像头</strong><p>请先登记设备和摄像头，并配置 RTSP 来源。设备在线并不代表视频来源已就绪。</p><RouterLink class="button button--primary" to="/devices">配置摄像头来源 <span aria-hidden="true">→</span></RouterLink></section>
  <section v-else class="monitor-grid"><article v-for="item in cameras" :key="item.camera.camera_id" class="monitor-tile"><div class="monitor-tile__head"><div><strong>{{ item.camera.name }}</strong><small>{{ item.device.name }} · {{ item.device.location }}</small></div><span class="source-pill" :class="`source-${cameraStatuses[item.camera.camera_id]?.source_state ?? 'unknown'}`"><i></i>{{ cameraStatuses[item.camera.camera_id]?.media_state === 'streaming' ? '正在传输' : cameraStatuses[item.camera.camera_id]?.source_state === 'reachable' ? '来源可达' : cameraStatuses[item.camera.camera_id]?.source_state === 'not_configured' ? '未配置来源' : cameraStatuses[item.camera.camera_id]?.source_state === 'unknown' ? '状态未知' : '等待来源' }}</span></div><CameraPlayer :camera-id="item.camera.camera_id" :camera-name="item.camera.name" /><div class="monitor-tile__foot"><span>设备 {{ item.device.online ? '在线' : '离线' }}</span><span>摄像头 {{ item.camera.camera_state }}</span><span>来源 {{ cameraStatuses[item.camera.camera_id]?.source_state ?? '未检查' }}</span><span>媒体 {{ cameraStatuses[item.camera.camera_id]?.media_state ?? '未检查' }}</span><span>编码 {{ cameraStatuses[item.camera.camera_id]?.browser_compatibility ?? '待检测' }}</span></div><p v-if="cameraStatuses[item.camera.camera_id]?.last_error" class="monitor-tile__error" role="status">{{ cameraStatuses[item.camera.camera_id]?.last_error }}</p></article></section>
  <section class="monitor-note"><span>ⓘ</span><p>WebRTC 优先，连接失败后尝试 HLS。园区网络需允许 MediaMTX WebRTC 媒体端口；状态不明时请查看媒体服务与边缘推流日志。</p></section>
</template>
