<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import CameraPlayer from '../components/CameraPlayer.vue'
import { deviceApi } from '../api/devices'
import type { StreamCamera } from '../api/devices'
import { errorMessage } from '../api/client'

const route = useRoute()
const camera = ref<StreamCamera | null>(null)
const deviceName = ref('')
const error = ref('')

onMounted(async () => {
  try {
    const availableDevices = await deviceApi.listAllStreamCameras()
    for (const device of availableDevices) {
      const match = device.cameras.find((item) => item.camera_id === route.params.cameraId)
      if (match) {
        camera.value = match
        deviceName.value = device.name
        return
      }
    }
    error.value = '此摄像头不在当前设备授权范围内。'
  } catch (cause) { error.value = errorMessage(cause) }
})
</script>

<template>
  <section class="page-heading"><RouterLink class="back-link" to="/m">← 返回授权设备</RouterLink><span class="eyebrow">LIVE CAMERA</span><h1>{{ camera?.name ?? '实时视频' }}</h1><p>{{ deviceName }} · 单路实时视频</p></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }}</div>
  <div v-else-if="camera" class="mobile-player-wrap"><CameraPlayer :camera-id="camera.camera_id" :camera-name="camera.name" /></div>
  <div v-else class="panel loading-panel">正在验证摄像头授权…</div>
</template>

<style scoped>
.mobile-player-wrap { width: min(100%, 960px); margin: 0 auto; }
</style>
