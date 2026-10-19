<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
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
const ticket = ref('')
const ticketExpires = ref('')
const addCamera = ref(false)
const confirmDisable = ref(false)
const cameraForm = ref({ name: '', camera_key: '' })
const sourceForms = reactive<Record<string, { rtsp_url: string; source_username: string; source_password: string }>>({})
const sourceStates = reactive<Record<string, string>>({})
const edit = reactive({ name: '', ip: '', location: '' })
const rules = ref(createDefaultClassroomRules())
const classroom = computed(() => device.value?.algorithms.find((item) => item.algorithm_key === 'classroom_behavior_detection'))

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
  try { applyDevice(await deviceApi.get(String(route.params.deviceId))) } catch (cause) { error.value = errorMessage(cause) } finally { busy.value = false }
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

async function issueTicket() {
  error.value = ''
  ticket.value = ''
  try { const result = await deviceApi.activationTicket(String(route.params.deviceId)); ticket.value = result.activation_ticket; ticketExpires.value = result.expires_at } catch (cause) { error.value = errorMessage(cause) }
}

async function registerCamera() {
  if (!device.value) return
  saving.value = true
  error.value = ''
  try { await deviceApi.createCamera(device.value.device_id, { name: cameraForm.value.name.trim(), camera_key: cameraForm.value.camera_key.trim() }); cameraForm.value = { name: '', camera_key: '' }; addCamera.value = false; notice.value = '摄像头已登记。可在下方添加私有 RTSP 来源。'; await load() } catch (cause) { error.value = errorMessage(cause) } finally { saving.value = false }
}

async function saveSource(cameraId: string) {
  const source = sourceForms[cameraId]
  if (!source || !/^rtsps?:\/\//i.test(source.rtsp_url) || /:\/\/[^/]*@/.test(source.rtsp_url)) { error.value = '请填写不含嵌入式凭据的 RTSP 地址，并将账号密码填入独立字段。'; return }
  error.value = ''
  sourceStates[cameraId] = '正在保存来源…'
  try { const result = await mediaApi.putSource(cameraId, source); sourceStates[cameraId] = result.config_status === 'applied' ? '来源配置已应用' : '来源配置等待 MediaMTX 确认'; notice.value = 'RTSP 来源已提交，配置状态以媒体服务对账结果为准。' } catch (cause) { sourceStates[cameraId] = ''; error.value = errorMessage(cause) }
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

function copyTicket() { void navigator.clipboard?.writeText(ticket.value) }
function initializeSources() {
  for (const camera of device.value?.cameras ?? []) sourceForms[camera.camera_id] ??= { rtsp_url: '', source_username: '', source_password: '' }
}
watch(device, initializeSources)
watch(() => route.params.deviceId, load)
onMounted(load)
</script>

<template>
  <section class="page-heading page-heading--split"><div><RouterLink class="back-link" to="/devices">← 返回设备</RouterLink><span class="eyebrow">DEVICE DETAIL</span><h1>{{ device?.name ?? '设备详情' }}</h1><p>{{ device?.location }} · {{ device?.ip }}</p></div><div v-if="device" class="detail-heading-actions"><span class="status-pill" :class="device.online ? 'status-pill--online' : 'status-pill--offline'"><i></i>{{ device.online ? '设备在线' : device.activation_state === 'active' ? '设备离线' : '待激活' }}</span><button v-if="device.active" class="button button--quiet" @click="confirmDisable = true">停用设备</button></div></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }}</div><div v-if="notice" class="notice notice--success" role="status">{{ notice }}</div>
  <div v-if="busy" class="panel loading-panel">正在加载设备服务数据…</div>
  <template v-else-if="device">
    <section class="detail-grid"><article class="panel"><div class="panel-heading"><div><span class="eyebrow">ASSET PROFILE</span><h2>设备资料</h2></div><span class="small-label">{{ device.active ? '有效设备' : '已停用' }}</span></div><form class="form-stack" @submit.prevent="saveDevice"><label class="field"><span>设备名称</span><input v-model="edit.name" required maxlength="120"></label><div class="field-row"><label class="field"><span>IP 地址</span><input v-model="edit.ip" required maxlength="45"></label><label class="field"><span>位置</span><input v-model="edit.location" required maxlength="256"></label></div><button class="button button--secondary" type="submit" :disabled="saving">保存设备资料</button></form><div class="device-meta"><span>最近心跳<strong>{{ formatDate(device.last_seen_at) }}</strong></span><span>激活状态<strong>{{ device.activation_state }}</strong></span></div></article>
      <article class="panel activation-panel"><div class="panel-heading"><div><span class="eyebrow">SECURE ACTIVATION</span><h2>设备激活</h2></div><span class="activation-icon">⌑</span></div><p class="panel-intro">签发 10 分钟有效的一次性票据，在 Atlas 设备上完成绑定。票据只显示一次。</p><button class="button button--primary" @click="issueTicket">签发激活票据 <span aria-hidden="true">→</span></button><div v-if="ticket" class="ticket-box"><div><small>一次性激活票据</small><code>{{ ticket }}</code></div><button class="icon-button" title="复制票据" @click="copyTicket">复制</button><small>有效期至 {{ formatDate(ticketExpires) }}</small></div><div v-else class="activation-note"><span>!</span>请在安全的设备终端上使用，不要分享给他人。</div></article></section>

    <section class="panel camera-panel"><div class="panel-heading"><div><span class="eyebrow">CAMERA SOURCES</span><h2>摄像头与媒体来源 <span class="heading-count">{{ device.cameras.length }}</span></h2></div><button class="button button--secondary" @click="addCamera = !addCamera">{{ addCamera ? '取消' : '+ 添加摄像头' }}</button></div><form v-if="addCamera" class="camera-create-form" @submit.prevent="registerCamera"><label class="field"><span>摄像头名称</span><input v-model="cameraForm.name" required placeholder="101 教室前摄"></label><label class="field"><span>摄像头标识</span><input v-model="cameraForm.camera_key" required placeholder="classroom-101-front"></label><button class="button button--primary" type="submit" :disabled="saving">登记摄像头</button></form>
      <div v-if="!device.cameras.length" class="empty-state empty-state--compact"><span class="empty-state__icon">◉</span><strong>还没有摄像头</strong><p>登记摄像头后，可分别配置私有 RTSP 来源并在桌面监控页查看。</p></div>
      <article v-for="camera in device.cameras" :key="camera.camera_id" class="camera-config-row"><div class="camera-config-row__head"><span class="camera-thumb">◉</span><div><strong>{{ camera.name }}</strong><small>{{ camera.camera_key }}</small></div><span class="camera-status">设备侧：{{ camera.camera_state }} · 视频：{{ camera.stream_state }}</span><RouterLink class="inline-link" to="/monitor">打开监控 <span>→</span></RouterLink></div><div class="source-form"><label class="field"><span>RTSP 地址</span><input v-model="sourceForms[camera.camera_id].rtsp_url" placeholder="rtsp://设备内网地址/stream" autocomplete="off"></label><label class="field"><span>来源用户名</span><input v-model="sourceForms[camera.camera_id].source_username" autocomplete="off"></label><label class="field"><span>来源密码</span><input v-model="sourceForms[camera.camera_id].source_password" type="password" autocomplete="new-password"></label><button class="button button--quiet" @click="saveSource(camera.camera_id)">保存来源</button></div><small v-if="sourceStates[camera.camera_id]" class="source-result">{{ sourceStates[camera.camera_id] }}</small></article>
    </section>

    <section class="panel rules-panel"><div class="panel-heading"><div><span class="eyebrow">CLASSROOM ALERT RULES</span><h2>课堂行为告警规则</h2></div><div v-if="classroom" class="version-pair"><span>模型/NPU {{ classroom.runtime_state === 'ready' ? '运行中' : classroom.runtime_state }}</span><span>期望 v{{ classroom.desired_version }}</span><b>→</b><span>设备实报 v{{ classroom.reported_version }}</span><em :class="classroom.config_state === 'applied' ? 'text-good' : 'text-warn'">{{ classroom.config_state === 'applied' ? '已应用' : classroom.config_state }}</em></div></div>
      <p class="panel-intro">检测行为本身不代表异常。规则只在边缘端实报已应用后生效；新设备和新规则默认关闭。</p>
      <div v-if="!classroom" class="notice notice--soft">当前设备尚未报告课堂行为算法 assignment。算法和模型状态由设备实报，规则暂不可编辑。</div>
      <template v-else><div class="rule-list"><article v-for="name in classroomClasses" :key="name" class="rule-row"><div class="rule-row__identity"><span class="rule-class-mark">{{ name === 'hand-raising' ? '举' : name === 'reading' ? '读' : '写' }}</span><div><strong>{{ name === 'hand-raising' ? '举手行为' : name === 'reading' ? '阅读行为' : '书写行为' }}</strong><small>{{ name }}</small></div></div><label class="toggle"><input v-model="rules[name].enabled" type="checkbox"><span></span><em>{{ rules[name].enabled ? '告警开启' : '已关闭' }}</em></label><div v-if="rules[name].enabled" class="rule-controls"><label class="field"><span>置信度阈值</span><input v-model.number="rules[name].confidence_threshold" type="number" min="0" max="1" step="0.05" required></label><label class="field"><span>持续时间 (ms)</span><input v-model.number="rules[name].minimum_duration_ms" type="number" min="1" step="100" required></label><label class="field"><span>冷却时间 (ms)</span><input v-model.number="rules[name].cooldown_ms" type="number" min="0" step="1000" required></label><label class="field"><span>告警等级</span><select v-model="rules[name].severity"><option value="low">低</option><option value="medium">中</option><option value="high">高</option><option value="critical">紧急</option></select></label></div><div v-else class="rule-disabled-copy">模型检测可用于预览，不产生事件、截图或通知。</div></article></div><div class="rules-footer"><span>规则配置版本 {{ classroom.desired_version }} · 实报版本 {{ classroom.reported_version }}</span><button class="button button--primary" :disabled="saving" @click="saveRules">{{ saving ? '正在发送…' : '保存规则配置' }} <span aria-hidden="true">→</span></button></div></template>
    </section>
  </template>
  <div v-if="confirmDisable" class="confirm-backdrop" role="presentation" @click.self="confirmDisable = false"><section class="panel confirm-panel" role="dialog" aria-modal="true" aria-labelledby="disable-device-title"><h2 id="disable-device-title">停用此设备？</h2><p>设备将不能再上报课堂事件或接收配置，关联媒体流会停止。历史事件与处置记录保留。</p><div><button class="button button--secondary" :disabled="saving" @click="confirmDisable = false">取消</button><button class="button button--danger" :disabled="saving" @click="disableDevice">{{ saving ? '正在停用…' : '确认停用' }}</button></div></section></div>
</template>

<style scoped>
.detail-heading-actions{display:flex;align-items:center;gap:9px}.confirm-backdrop{position:fixed;z-index:100;inset:0;display:grid;place-items:center;padding:22px;background:#14242378;backdrop-filter:blur(3px)}.confirm-panel{width:min(100%,440px);padding:22px}.confirm-panel h2{margin:0;color:#263b39;font-size:17px}.confirm-panel p{margin:12px 0 20px;color:#71817e;font-size:11px;line-height:1.7}.confirm-panel>div{display:flex;justify-content:flex-end;gap:8px}.button--danger{border-color:#a84f49;background:#a84f49;color:#fff}.button--danger:hover:not(:disabled){background:#873d38}
</style>
