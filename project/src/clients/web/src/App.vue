<template>
  <main v-if="!online" class="offline-screen" role="status" aria-live="polite">
    <span class="offline-screen__mark">Y</span>
    <h1>当前处于离线状态</h1>
    <p>应用页面可离线打开。账号数据、视频与事件证据需要重新联网后读取。</p>
  </main>
  <RouterView v-else />
  <PwaUpdatePrompt />
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import PwaUpdatePrompt from './components/PwaUpdatePrompt.vue'

const online = ref(navigator.onLine)
function updateNetworkState() { online.value = navigator.onLine }
onMounted(() => {
  window.addEventListener('online', updateNetworkState)
  window.addEventListener('offline', updateNetworkState)
})
onBeforeUnmount(() => {
  window.removeEventListener('online', updateNetworkState)
  window.removeEventListener('offline', updateNetworkState)
})
</script>

<style scoped>
.offline-screen{display:grid;min-height:100vh;align-content:center;justify-items:center;padding:28px;background:#f4f7f6;text-align:center}.offline-screen__mark{display:grid;width:45px;height:45px;place-items:center;border-radius:14px;background:#176a63;color:#fff;font-size:22px;font-weight:800}.offline-screen h1{margin:20px 0 0;color:#203536;font-size:22px}.offline-screen p{max-width:430px;margin:10px 0 0;color:#748183;font-size:13px;line-height:1.7}
</style>
