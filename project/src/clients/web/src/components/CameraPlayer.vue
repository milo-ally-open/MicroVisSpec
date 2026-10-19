<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { ApiError, csrfHeaders, errorMessage } from '../api/client'
import { mediaApi, type PlaybackLease } from '../api/media'

const props = defineProps<{ cameraId: string; cameraName: string }>()
const video = ref<HTMLVideoElement | null>(null)
const state = ref<'connecting' | 'playing' | 'fallback' | 'error'>('connecting')
const message = ref('正在申请授权播放…')
const lease = ref<PlaybackLease | null>(null)
let peer: RTCPeerConnection | null = null
let hls: import('hls.js').default | null = null
let renewTimer: number | undefined
let disposed = false

function safeMediaURL(value: string) {
  const url = new URL(value, window.location.origin)
  if (url.origin !== window.location.origin || !['https:', 'http:'].includes(url.protocol)) throw new Error('媒体播放地址不在当前安全站点内。')
  return url
}

function waitForIceGathering(connection: RTCPeerConnection) {
  if (connection.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise<void>((resolve) => {
    const timer = window.setTimeout(done, 3000)
    function done() {
      window.clearTimeout(timer)
      connection.removeEventListener('icegatheringstatechange', check)
      resolve()
    }
    function check() { if (connection.iceGatheringState === 'complete') done() }
    connection.addEventListener('icegatheringstatechange', check)
  })
}

function waitForVideoPlayback(element: HTMLVideoElement, connection: RTCPeerConnection) {
  if (element.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA && !element.paused && element.videoWidth > 0) return Promise.resolve()
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => finish(new Error('WebRTC 未收到可播放画面。')), 10000)
    function cleanup() {
      window.clearTimeout(timer)
      element.removeEventListener('playing', onPlaying)
      element.removeEventListener('error', onError)
      connection.removeEventListener('connectionstatechange', onConnectionState)
    }
    function finish(error?: Error) {
      cleanup()
      if (error) reject(error)
      else resolve()
    }
    function onPlaying() {
      if (element.videoWidth > 0) finish()
    }
    function onError() { finish(new Error('WebRTC 视频解码失败。')) }
    function onConnectionState() {
      if (connection.connectionState === 'failed') finish(new Error('WebRTC 网络连接失败。'))
    }
    element.addEventListener('playing', onPlaying)
    element.addEventListener('error', onError)
    connection.addEventListener('connectionstatechange', onConnectionState)
    onConnectionState()
  })
}

async function releasePlayback() {
  if (renewTimer) { window.clearInterval(renewTimer); renewTimer = undefined }
  if (peer) { peer.close(); peer = null }
  if (hls) { hls.destroy(); hls = null }
  if (video.value) {
    video.value.pause()
    video.value.removeAttribute('src')
    video.value.srcObject = null
    video.value.load()
  }
  const leaseId = lease.value?.lease_id
  lease.value = null
  if (leaseId) await mediaApi.release(leaseId).catch(() => undefined)
}

async function failPlayback(reason: string) {
  await releasePlayback()
  if (disposed) return
  state.value = 'error'
  message.value = reason
}

async function playWebRTC(url: URL) {
  peer = new RTCPeerConnection()
  const connection = peer
  connection.addTransceiver('video', { direction: 'recvonly' })
  connection.ontrack = (event) => {
    if (video.value) {
      video.value.srcObject = event.streams[0] ?? new MediaStream([event.track])
      void video.value.play().catch(() => undefined)
    }
  }
  connection.onconnectionstatechange = () => {
    if (connection.connectionState === 'failed' || (connection.connectionState === 'disconnected' && state.value === 'playing')) {
      void failPlayback('WebRTC 连接中断，可检查园区媒体网络后重新连接。')
    }
  }
  const offer = await connection.createOffer()
  await connection.setLocalDescription(offer)
  await waitForIceGathering(connection)
  const response = await fetch(url, { method: 'POST', headers: csrfHeaders({ 'Content-Type': 'application/sdp' }), body: connection.localDescription?.sdp, credentials: 'include', cache: 'no-store' })
  if (!response.ok) throw new ApiError(response.status, 'webrtc_unavailable', 'WebRTC 信令不可用。')
  const answer = await response.text()
  await connection.setRemoteDescription({ type: 'answer', sdp: answer })
  const element = video.value
  if (!element) throw new Error('视频播放器尚未准备好。')
  await waitForVideoPlayback(element, connection)
  state.value = 'playing'
  message.value = 'WebRTC 实时播放'
}

async function playHLS(url: URL) {
  const element = video.value
  if (!element) throw new Error('视频播放器尚未准备好。')
  if (peer) { peer.close(); peer = null }
  element.srcObject = null
  if (element.canPlayType('application/vnd.apple.mpegurl')) {
    element.src = url.href
    await element.play()
  } else {
    const { default: Hls } = await import('hls.js')
    if (!Hls.isSupported()) throw new Error('当前浏览器不支持 HLS 播放。')
    hls = new Hls({ xhrSetup: (xhr) => { xhr.withCredentials = true } })
    hls.loadSource(url.href)
    hls.attachMedia(element)
    await new Promise<void>((resolve, reject) => {
      hls?.on(Hls.Events.MANIFEST_PARSED, () => { void element.play().then(resolve).catch(reject) })
      hls?.on(Hls.Events.ERROR, (_event, data) => { if (data.fatal) reject(new Error('HLS 视频流无法播放。')) })
    })
  }
  state.value = 'fallback'
  message.value = 'WebRTC 不可用，已切换 HLS 回退播放'
}

async function start() {
  state.value = 'connecting'
  message.value = '正在申请授权播放…'
  try {
    const current = await mediaApi.playback(props.cameraId)
    if (disposed) {
      await mediaApi.release(current.lease_id).catch(() => undefined)
      return
    }
    lease.value = current
    renewTimer = window.setInterval(() => {
      const leaseId = lease.value?.lease_id
      if (leaseId) void mediaApi.renew(leaseId).catch(() => { void failPlayback('观看租约已失效，请重新连接。') })
    }, 15000)
    try {
      await playWebRTC(safeMediaURL(current.whep_url))
    } catch (cause) {
      if (disposed) return
      if (!lease.value) throw cause
      await playHLS(safeMediaURL(current.hls_url))
    }
  } catch (cause) {
    await failPlayback(errorMessage(cause))
  }
}

async function stop() {
  disposed = true
  await releasePlayback()
}

onMounted(() => { void start() })
onBeforeUnmount(() => { void stop() })
</script>

<template>
  <div class="camera-player">
    <video ref="video" autoplay muted playsinline controls :aria-label="`${cameraName} 实时视频`"></video>
    <div v-if="state !== 'playing' && state !== 'fallback'" class="camera-player__overlay"><span class="player-state-icon" :class="{ 'player-state-icon--loading': state === 'connecting' }">{{ state === 'connecting' ? '◌' : '!' }}</span><strong>{{ state === 'connecting' ? '正在连接视频' : '视频暂不可用' }}</strong><small>{{ message }}</small><button v-if="state === 'error'" class="button button--secondary" @click="disposed = false; void start()">重新连接</button></div>
    <div v-else class="camera-player__badge"><i></i>{{ state === 'fallback' ? 'HLS 回退' : 'WEBRTC' }}</div>
  </div>
</template>
