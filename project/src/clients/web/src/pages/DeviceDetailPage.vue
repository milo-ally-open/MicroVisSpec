<script setup lang="ts">
import AppIcon from '../components/AppIcon.vue'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { deviceApi, type Device } from '../api/devices'
import { mediaApi } from '../api/media'
import { errorMessage } from '../api/client'
import { formatDate } from '../lib/format'
import { classroomClasses, createDefaultClassroomRules, validateClassroomRules, type ClassroomRule } from '../lib/classroomRules'

const route = useRoute()
const router = useRouter()
const device = ref<Device | null>(null)
const busy = ref(true)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const activationRequest = ref<{ request_id: string; status: string; expires_at: string } | null>(null)
let activationPollTimer: number | undefined
let activationPollGeneration = 0
const durationMode = ref<'unlimited' | 'custom'>('unlimited')
const durationValue = ref(30)
const durationUnit = ref<'minutes' | 'hours' | 'days'>('days')
const addCamera = ref(false)
const confirmDisable = ref(false)
const cameraForm = ref({ name: '', camera_key: '' })
const sourceForms = reactive<Record<string, { rtsp_url: string }>>({})
const sourceStates = reactive<Record<string, string>>({})
const edit = reactive({ name: '', ip: '', location: '' })
const rules = ref(createDefaultClassroomRules())
const classroom = computed(() => device.value?.algorithms.find((item) => item.algorithm_key === 'classroom_behavior_detection'))
const durationUnitSeconds = computed(() => ({ minutes: 60, hours: 3600, days: 86400 }[durationUnit.value]))
const maxDurationValue = computed(() => Math.floor(9_223_372_036 / durationUnitSeconds.value))
const selectedDurationSeconds = computed(() => durationMode.value === 'unlimited' ? null : durationValue.value * durationUnitSeconds.value)
const durationSummary = computed(() => {
  if (durationMode.value === 'unlimited') return '无限期'
  const unitName = { minutes: '分钟', hours: '小时', days: '天' }[durationUnit.value]
  return `${durationValue.value} ${unitName}`
})
function applyDevice(value: Device) {
  device.value = value
  edit.name = value.name
  edit.ip = value.ip
  edit.location = value.location
  const raw = classroom.value?.desired_parameters.classroom_rules as Record<string, Partial<ClassroomRule>> | undefined
  if (raw) {
    const next = createDefaultClassroomRules()
    for (const name of classroomClasses) next[name] = { ...next[name], ...raw[name] }
    rules.value = next
  } else rules.value = createDefaultClassroomRules()
}

async function load() {
  busy.value = true
  error.value = ''
  try {
    const current = await deviceApi.get(String(route.params.deviceId))
    applyDevice(current)
    await Promise.all(current.cameras.map(async (camera) => {
      const form = { rtsp_url: '' }
      sourceForms[camera.camera_id] = form
      if (!current.active) {
        sourceStates[camera.camera_id] = '设备授权到期；重新激活后可管理视频来源。'
        return
      }
      sourceStates[camera.camera_id] = '正在读取来源配置…'
      try {
        const status = await mediaApi.status(camera.camera_id)
        form.rtsp_url = status.rtsp_url ?? ''
        sourceStates[camera.camera_id] = describeSourceStatus(status)
      } catch (cause) {
        sourceStates[camera.camera_id] = errorMessage(cause)
      }
    }))
  } catch (cause) { error.value = errorMessage(cause) } finally { busy.value = false }
}

async function saveDevice() {
  saving.value = true
  error.value = ''
  notice.value = ''
  try { applyDevice(await deviceApi.update(String(route.params.deviceId), { name: edit.name, ip: edit.ip, location: edit.location })); notice.value = '设备资料已保存。' } catch (cause) { error.value = errorMessage(cause) } finally { saving.value = false }
}

async function disableDevice() {
  if (!device.value) return
  saving.value = true
  error.value = ''
  try {
    await deviceApi.disable(device.value.device_id)
    await router.replace('/devices')
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    saving.value = false
    confirmDisable.value = false
  }
}

function stopActivationPolling() {
  activationPollGeneration++
  if (activationPollTimer !== undefined) window.clearTimeout(activationPollTimer)
  activationPollTimer = undefined
}

async function pollActivationRequest(deviceId: string, requestId: string, generation: number) {
  if (generation !== activationPollGeneration) return
  try {
    const result = await deviceApi.getActivationRequest(deviceId, requestId)
    if (generation !== activationPollGeneration) return
    error.value = ''
    if (result.status === 'syncing') {
      notice.value = ''
      activationRequest.value = { request_id: requestId, status: 'syncing', expires_at: result.expires_at }
      activationPollTimer = window.setTimeout(() => void pollActivationRequest(deviceId, requestId, generation), 2500)
      return
    }
    if (result.status === 'pending' && Date.parse(result.expires_at) > Date.now()) {
      activationRequest.value = { request_id: requestId, status: 'pending', expires_at: result.expires_at }
      activationPollTimer = window.setTimeout(() => void pollActivationRequest(deviceId, requestId, generation), 2500)
      return
    }
    activationRequest.value = null
    if (result.status === 'activated') {
      notice.value = '设备已激活，新的授权期限已生效。'
      await load()
    } else if (result.status === 'expired') {
      error.value = '设备暂未领取激活请求，请确认设备在线后重新提交。'
    } else {
      error.value = '这条激活请求已被新的请求替代，请重新提交。'
    }
  } catch (cause) {
    if (generation !== activationPollGeneration) return
    error.value = errorMessage(cause)
    activationPollTimer = window.setTimeout(() => void pollActivationRequest(deviceId, requestId, generation), 5000)
  }
}

async function activateFromWeb() {
  error.value = ''
  notice.value = ''
  if (activationRequest.value) return
  if (durationMode.value === 'custom' && (!Number.isSafeInteger(durationValue.value) || durationValue.value < 1 || durationValue.value > maxDurationValue.value)) {
    error.value = `自定义期限需为 1 至 ${maxDurationValue.value} ${durationUnit.value === 'minutes' ? '分钟' : durationUnit.value === 'hours' ? '小时' : '天'}。`
    return
  }
  const deviceId = String(route.params.deviceId)
  saving.value = true
  try {
    const result = await deviceApi.createActivationRequest(deviceId, selectedDurationSeconds.value)
    activationRequest.value = { request_id: result.request_id, status: result.status, expires_at: result.expires_at }
    notice.value = '请求已提交，Atlas agent 会在后台安全领取凭据；激活进度会自动刷新。'
    const generation = ++activationPollGeneration
    activationPollTimer = window.setTimeout(() => void pollActivationRequest(deviceId, result.request_id, generation), 1500)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    saving.value = false
  }
}

async function registerCamera() {
  if (!device.value) return
  saving.value = true
  error.value = ''
  try { await deviceApi.createCamera(device.value.device_id, { name: cameraForm.value.name.trim(), camera_key: cameraForm.value.camera_key.trim() }); cameraForm.value = { name: '', camera_key: '' }; addCamera.value = false; notice.value = '摄像头已登记。可在下方配置 RTSP 地址。'; await load() } catch (cause) { error.value = errorMessage(cause) } finally { saving.value = false }
}

async function saveSource(cameraId: string) {
  const source = sourceForms[cameraId]
  const normalizedURL = source?.rtsp_url.trim() ?? ''
  let parsedURL: URL | undefined
  try { parsedURL = new URL(normalizedURL) } catch { /* Validation below presents the field error. */ }
  if (!source || normalizedURL.length > 2048 || !parsedURL || !['rtsp:', 'rtsps:'].includes(parsedURL.protocol) || !parsedURL.hostname || parsedURL.username || parsedURL.password || parsedURL.hash) { error.value = '请填写有效且不含账号信息的 RTSP 地址。'; return }
  error.value = ''
  sourceStates[cameraId] = '正在保存来源…'
  source.rtsp_url = normalizedURL
  try { const result = await mediaApi.putSource(cameraId, { rtsp_url: normalizedURL }); sourceStates[cameraId] = result.config_status === 'applied' ? 'RTSP 地址已应用' : 'RTSP 地址等待媒体服务确认'; notice.value = 'RTSP 地址已保存。' } catch (cause) { sourceStates[cameraId] = ''; error.value = errorMessage(cause) }
}

function describeSourceStatus(status: Awaited<ReturnType<typeof mediaApi.status>>) {
  if (!status.rtsp_url) return '尚未配置 RTSP 地址'
  if (status.config_status === 'pending') return 'RTSP 地址等待媒体服务确认'
  if (status.config_status === 'failed') return 'RTSP 地址应用失败'
  if (status.source_state === 'reachable') return 'RTSP 视频来源可达'
  return 'RTSP 地址已保存，等待视频连接'
}

async function saveRules() {
  if (!device.value || !classroom.value) return
  error.value = ''
  notice.value = ''
  const validationError = validateClassroomRules(rules.value)
  if (validationError) { error.value = validationError; return }
  saving.value = true
  try {
    const result = await deviceApi.updateRules(device.value.device_id, classroom.value.assignment_id, classroom.value.desired_version, { classroom_rules: rules.value })
    notice.value = `配置已发送，期望版本 ${result.desired_version}；设备实报 ${classroom.value.reported_version}，收到设备 ACK 前仍显示待应用。`
    await load()
  } catch (cause) { error.value = errorMessage(cause); if ((cause as { status?: number })?.status === 409) await load() } finally { saving.value = false }
}

watch(() => route.params.deviceId, () => { stopActivationPolling(); activationRequest.value = null; void load() })
onMounted(load)
onBeforeUnmount(stopActivationPolling)
</script>

<template>
  <section class="page-heading page-heading--split"><div><RouterLink class="back-link" to="/devices">← 返回设备</RouterLink><span class="eyebrow">DEVICE DETAIL</span><h1>{{ device?.name ?? '设备详情' }}</h1><p>{{ device?.location }} · {{ device?.ip }}</p></div><div v-if="device" class="detail-heading-actions"><span class="status-pill" :class="device.online ? 'status-pill--online' : 'status-pill--offline'"><i></i>{{ device.online ? '设备在线' : device.activation_state === 'expired' ? '授权已到期' : device.activation_state === 'active' ? '设备离线' : device.activation_state === 'disabled' ? '已停用' : '待激活' }}</span><button v-if="device.active" class="button button--quiet" @click="confirmDisable = true">停用设备</button></div></section>
  <div v-if="device?.activation_state === 'disabled'" class="notice notice--soft" role="status">此设备已由管理员停用，不能在网页续期；请联系管理员重新登记设备。</div><div v-if="error" class="notice notice--error" role="alert">{{ error }}</div><div v-if="notice" class="notice notice--success" role="status">{{ notice }}</div><div v-if="device?.activation_state === 'expired'" class="notice notice--soft" role="status">设备授权已到期，设备业务访问和 MQTT 权限已停用。选择新期限并在本页续期即可恢复；设备 agent 会在后台自动同步。</div>
  <div v-if="busy" class="panel loading-panel">正在加载设备服务数据…</div>
  <template v-else-if="device">
    <section class="detail-grid"><article class="panel"><div class="panel-heading"><div><span class="eyebrow">ASSET PROFILE</span><h2>设备资料</h2></div><span class="small-label">{{ device.activation_state === 'expired' ? '授权已到期' : device.active ? '有效设备' : '已停用' }}</span></div><form class="form-stack" @submit.prevent="saveDevice"><label class="field"><span>设备名称</span><input v-model="edit.name" required maxlength="120" :disabled="!device.active"></label><div class="field-row"><label class="field"><span>IP 地址</span><input v-model="edit.ip" required maxlength="45" :disabled="!device.active"></label><label class="field"><span>位置</span><input v-model="edit.location" required maxlength="256" :disabled="!device.active"></label></div><button class="button button--secondary" type="submit" :disabled="saving || !device.active">保存设备资料</button></form><div class="device-meta"><span>最近心跳<strong>{{ formatDate(device.last_seen_at) }}</strong></span><span>激活状态<strong>{{ device.activation_state }}</strong></span><span v-if="device.activation_expires_at">授权到期<strong>{{ formatDate(device.activation_expires_at) }}</strong></span></div></article>
      <article v-if="device.activation_state !== 'disabled'" class="panel activation-panel"><div class="panel-heading"><div><span class="eyebrow">SECURE ACTIVATION</span><h2>{{ device.activation_state === 'expired' ? '续期并重新激活' : '设备激活' }}</h2></div><span class="activation-icon"><AppIcon name="shield" /></span></div><p class="panel-intro">选择设备使用期限。到期后云端会停用设备业务访问、撤销 MQTT 权限；设备在线时会在后台自动领取续期，不需要操作 Atlas 终端。</p><label class="field"><span>使用期限</span><select v-model="durationMode" :disabled="Boolean(activationRequest)"><option value="unlimited">无限期</option><option value="custom">自定义期限</option></select></label><div v-if="durationMode === 'custom'" class="field-row activation-duration"><label class="field"><span>时长</span><input v-model.number="durationValue" type="number" min="1" :max="maxDurationValue" step="1" required :disabled="Boolean(activationRequest)"></label><label class="field"><span>单位</span><select v-model="durationUnit" :disabled="Boolean(activationRequest)"><option value="minutes">分钟</option><option value="hours">小时</option><option value="days">天</option></select></label></div><p class="activation-duration-note">本次设备授权：{{ durationSummary }}</p><button class="button button--primary" :disabled="saving || Boolean(activationRequest)" @click="activateFromWeb">{{ saving ? '正在提交…' : activationRequest ? (activationRequest.status === 'syncing' ? '正在同步到设备…' : '正在等待设备…') : device.activation_state === 'expired' ? '网页续期并激活' : '网页激活设备' }} <span aria-hidden="true">→</span></button><div v-if="activationRequest?.status === 'syncing'" class="activation-note" role="status"><span>…</span>云端已确认授权，正在等待 Atlas 保存新凭据。设备联网后会自动完成，请勿重复提交。</div><div v-else-if="activationRequest" class="activation-note" role="status"><span>…</span>请求已提交，Atlas agent 在线时会自动领取新凭据；请求等待期限至 {{ formatDate(activationRequest.expires_at) }}。</div><div v-else class="activation-note"><span>✓</span>用户只需在网页操作；设备需预先完成 agent 安装和可信接入。MQTT 凭据始终留在设备侧，不会显示在浏览器中。</div></article></section>

    <section class="panel camera-panel"><div class="panel-heading"><div><span class="eyebrow">CAMERA SOURCES</span><h2>摄像头与媒体来源 <span class="heading-count">{{ device.cameras.length }}</span></h2></div><button class="button button--secondary" @click="addCamera = !addCamera">{{ addCamera ? '取消' : '+ 添加摄像头' }}</button></div><form v-if="addCamera" class="camera-create-form" @submit.prevent="registerCamera"><label class="field"><span>摄像头名称</span><input v-model="cameraForm.name" required placeholder="前端摄像头"></label><label class="field"><span>摄像头标识</span><input v-model="cameraForm.camera_key" required placeholder="camera-01"></label><button class="button button--primary" type="submit" :disabled="saving">登记摄像头</button></form>
      <div v-if="!device.cameras.length" class="empty-state empty-state--compact"><span class="empty-state__icon"><AppIcon name="camera" /></span><strong>还没有摄像头</strong><p>登记摄像头后，可配置 RTSP 地址并在桌面监控页查看。</p></div>
      <article v-for="camera in device.cameras" :key="camera.camera_id" class="camera-config-row"><div class="camera-config-row__head"><span class="camera-thumb"><AppIcon name="camera" /></span><div><strong>{{ camera.name }}</strong><small>{{ camera.camera_key }}</small></div><span class="camera-status">设备侧：{{ camera.camera_state }} · 视频：{{ camera.stream_state }}</span><RouterLink class="inline-link" to="/monitor">打开监控 <span>→</span></RouterLink></div><div class="source-form"><label class="field"><span>RTSP 地址</span><input v-model="sourceForms[camera.camera_id].rtsp_url" placeholder="rtsp://192.168.0.2:8554/stream" autocomplete="off"></label><button class="button button--quiet" :disabled="sourceStates[camera.camera_id] === '正在保存来源…'" @click="saveSource(camera.camera_id)">保存地址</button></div><small v-if="sourceStates[camera.camera_id]" class="source-result">{{ sourceStates[camera.camera_id] }}</small></article>
    </section>

    <section class="panel rules-panel"><div class="panel-heading"><div><span class="eyebrow">ALGORITHM ALERT RULES</span><h2>算法告警规则</h2></div><div v-if="classroom" class="version-pair"><span>模型/NPU {{ classroom.runtime_state === 'ready' ? '运行中' : classroom.runtime_state }}</span><span>期望 v{{ classroom.desired_version }}</span><b>→</b><span>设备实报 v{{ classroom.reported_version }}</span><em :class="classroom.config_state === 'applied' ? 'text-good' : 'text-warn'">{{ classroom.config_state === 'applied' ? '已应用' : classroom.config_state }}</em></div></div>
      <p class="panel-intro">检测行为本身不代表异常。规则只在边缘端实报已应用后生效；新设备和新规则默认关闭。</p>
      <div v-if="!classroom" class="notice notice--soft">当前设备尚未报告此算法的运行配置。算法和模型状态由设备实报，规则暂不可编辑。</div>
      <template v-else><div class="rule-list"><article v-for="name in classroomClasses" :key="name" class="rule-row"><div class="rule-row__identity"><span class="rule-class-mark">{{ name === 'hand-raising' ? '举' : name === 'reading' ? '读' : '写' }}</span><div><strong>{{ name === 'hand-raising' ? '举手行为' : name === 'reading' ? '阅读行为' : '书写行为' }}</strong><small>{{ name }}</small></div></div><label class="toggle"><input v-model="rules[name].enabled" type="checkbox"><span></span><em>{{ rules[name].enabled ? '告警开启' : '已关闭' }}</em></label><div v-if="rules[name].enabled" class="rule-controls"><label class="field"><span>置信度阈值</span><input v-model.number="rules[name].confidence_threshold" type="number" min="0" max="1" step="0.05" required></label><label class="field"><span>持续时间 (ms)</span><input v-model.number="rules[name].minimum_duration_ms" type="number" min="1" step="100" required></label><label class="field"><span>冷却时间 (ms)</span><input v-model.number="rules[name].cooldown_ms" type="number" min="0" step="1000" required></label><label class="field"><span>告警等级</span><select v-model="rules[name].severity"><option value="low">低</option><option value="medium">中</option><option value="high">高</option><option value="critical">紧急</option></select></label></div><div v-else class="rule-disabled-copy">模型检测可用于预览，不产生事件、截图或通知。</div></article></div><div class="rules-footer"><span>规则配置版本 {{ classroom.desired_version }} · 实报版本 {{ classroom.reported_version }}</span><button class="button button--primary" :disabled="saving" @click="saveRules">{{ saving ? '正在发送…' : '保存规则配置' }} <span aria-hidden="true">→</span></button></div></template>
    </section>
  </template>
  <div v-if="confirmDisable" class="confirm-backdrop" role="presentation" @click.self="confirmDisable = false"><section class="panel confirm-panel" role="dialog" aria-modal="true" aria-labelledby="disable-device-title"><h2 id="disable-device-title">停用此设备？</h2><p>设备将不能再上报异常事件或接收配置，关联媒体流会停止。历史事件与处置记录保留。</p><div><button class="button button--secondary" :disabled="saving" @click="confirmDisable = false">取消</button><button class="button button--danger" :disabled="saving" @click="disableDevice">{{ saving ? '正在停用…' : '确认停用' }}</button></div></section></div>
</template>

<style scoped>
.detail-heading-actions{display:flex;align-items:center;gap:9px}.confirm-backdrop{position:fixed;z-index:100;inset:0;display:grid;place-items:center;padding:22px;background:#1c19177a;backdrop-filter:blur(3px)}.confirm-panel{width:min(100%,440px);padding:22px}.confirm-panel h2{margin:0;color:#1c1917;font-family:"Songti SC","STSong","SimSun",Georgia,serif;font-size:18px;font-weight:400}.confirm-panel p{margin:12px 0 20px;color:#6d655b;font-size:12px;line-height:1.7}.confirm-panel>div{display:flex;justify-content:flex-end;gap:8px}.button--danger{border-color:#a84f49;background:#a84f49;color:#fff}.button--danger:hover:not(:disabled){background:#873d38}
</style>
