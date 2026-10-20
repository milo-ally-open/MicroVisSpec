<script setup lang="ts">
import AppIcon from '../components/AppIcon.vue'
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { deviceApi, type Device } from '../api/devices'
import { errorMessage } from '../api/client'
import { formatDate } from '../lib/format'

const devices = ref<Device[]>([])
const router = useRouter()
const busy = ref(true)
const saving = ref(false)
const error = ref('')
const success = ref('')
const showForm = ref(false)
const form = ref({ name: '', ip: '', location: '' })
const onlineCount = computed(() => devices.value.filter((item) => item.online).length)

async function load() {
  busy.value = true
  error.value = ''
  try { devices.value = (await deviceApi.list()).items } catch (cause) { error.value = errorMessage(cause) } finally { busy.value = false }
}

async function createDevice() {
  error.value = ''
  success.value = ''
  saving.value = true
  try {
    const result = await deviceApi.create({ name: form.value.name.trim(), ip: form.value.ip.trim(), location: form.value.location.trim() })
    success.value = '设备已登记。下一步可进入详情创建摄像头，并签发一次性激活票据。'
    form.value = { name: '', ip: '', location: '' }
    showForm.value = false
    await load()
    await router.replace(`/devices/${result.device_id}`)
  } catch (cause) { error.value = errorMessage(cause) } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <section class="page-heading page-heading--split"><div><span class="eyebrow">EDGE FLEET</span><h1>边缘设备</h1><p>管理设备与摄像头，查看连接和算法运行状态。</p></div><button class="button button--primary" @click="showForm = !showForm">{{ showForm ? '收起表单' : '+ 登记设备' }}</button></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }} <button class="text-button" @click="load">重试</button></div>
  <div v-if="success" class="notice notice--success" role="status">{{ success }}</div>
  <section v-if="showForm" class="panel device-create-panel"><div class="panel-heading"><div><span class="eyebrow">REGISTER DEVICE</span><h2>登记一台边缘设备</h2></div><span class="step-number">01</span></div><p class="panel-intro">登记后设备处于待激活状态。设备 IP 用于管理记录，不作为设备身份认证。</p><form class="device-form-grid" @submit.prevent="createDevice"><label class="field"><span>设备名称</span><input v-model="form.name" required maxlength="120" placeholder="例如：教学楼 A 区边缘设备"></label><label class="field"><span>设备 IP</span><input v-model="form.ip" required maxlength="45" placeholder="例如：192.168.0.2"></label><label class="field"><span>位置</span><input v-model="form.location" required maxlength="256" placeholder="例如：教学楼 A · 101 教室"></label><button class="button button--primary form-submit" type="submit" :disabled="saving">{{ saving ? '正在登记…' : '确认登记' }} <span aria-hidden="true">→</span></button></form></section>
  <section class="device-list-header"><div><h2>设备列表 <span>{{ devices.length }}</span></h2><p>在线设备 {{ onlineCount }} 台 · 状态更新时间由心跳决定</p></div><button class="button button--quiet" @click="load" :disabled="busy"><AppIcon name="refresh" :size="16" />刷新状态</button></section>
  <section v-if="busy" class="device-grid"><div v-for="n in 3" :key="n" class="panel skeleton-card"></div></section>
  <section v-else-if="!devices.length" class="panel empty-state empty-state--large"><span class="empty-state__icon"><AppIcon name="devices" /></span><strong>还没有登记设备</strong><p>登记 Atlas 设备后，工作台会显示真实心跳、摄像头和算法状态。</p><button class="button button--primary" @click="showForm = true">登记第一台设备 <span aria-hidden="true">→</span></button></section>
  <section v-else class="device-grid"><article v-for="device in devices" :key="device.device_id" class="panel device-card"><div class="device-card__top"><div class="device-illustration"><AppIcon name="devices" :size="28" /><i v-if="device.online"></i></div><span class="status-pill" :class="device.online ? 'status-pill--online' : 'status-pill--offline'"><i></i>{{ device.online ? '设备在线' : device.activation_state === 'active' ? '设备离线' : '待激活' }}</span></div><RouterLink class="device-card__title" :to="`/devices/${device.device_id}`">{{ device.name }} <span aria-hidden="true">↗</span></RouterLink><p class="device-card__location">{{ device.location || '未填写位置' }}</p><div class="device-card__facts"><span><small>设备 IP</small><strong>{{ device.ip }}</strong></span><span><small>摄像头</small><strong>{{ device.cameras?.length ?? 0 }} 路</strong></span><span><small>算法运行</small><strong :class="device.algorithms?.some((algorithm) => algorithm.runtime_state === 'ready') ? 'text-good' : 'text-muted'">{{ device.algorithms?.some((algorithm) => algorithm.runtime_state === 'ready') ? '运行中' : '未就绪' }}</strong></span></div><div class="device-card__bottom"><small>最近心跳 {{ formatDate(device.last_seen_at, false) }}</small><RouterLink :to="`/devices/${device.device_id}`">查看详情 <span aria-hidden="true">→</span></RouterLink></div></article></section>
</template>
