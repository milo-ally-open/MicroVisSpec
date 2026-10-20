<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ApiError, errorMessage } from '../api/client'
import { eventApi, type EventDetail } from '../api/events'
import { behaviorLabel, formatDate, severityLabel, statusLabel } from '../lib/format'

const route = useRoute()
const detail = ref<EventDetail | null>(null)
const evidenceUrl = ref('')
const busy = ref(true)
const saving = ref(false)
const error = ref('')
const result = ref('')
const canStart = computed(() => detail.value?.event.status === 'pending')
const canClose = computed(() => detail.value?.event.status === 'processing')

function clearEvidence() { if (evidenceUrl.value) URL.revokeObjectURL(evidenceUrl.value); evidenceUrl.value = '' }
async function load() {
  busy.value = true
  error.value = ''
  clearEvidence()
  try {
    const loaded = await eventApi.detail(String(route.params.eventId))
    detail.value = loaded
    if (loaded.evidence.status === 'available') {
      try { evidenceUrl.value = URL.createObjectURL(await eventApi.evidence(loaded.event.event_id)) }
      catch (cause) { if (!(cause instanceof ApiError && cause.status === 410)) error.value = errorMessage(cause) }
    }
  } catch (cause) { error.value = errorMessage(cause) } finally { busy.value = false }
}

async function transition(toStatus: string) {
  const event = detail.value?.event
  if (!event) return
  if ((toStatus === 'resolved' || toStatus === 'false_positive') && !result.value.trim()) { error.value = '进入终态前请填写处理结果。'; return }
  saving.value = true
  error.value = ''
  try { await eventApi.transition(event.event_id, toStatus, result.value.trim(), event.version); result.value = ''; await load() }
  catch (cause) { error.value = errorMessage(cause); if (cause instanceof ApiError && cause.status === 409) await load() }
  finally { saving.value = false }
}

watch(() => route.params.eventId, load)
onMounted(load)
onBeforeUnmount(clearEvidence)
</script>

<template>
  <section class="page-heading"><RouterLink class="back-link" to="/events">← 返回异常事件</RouterLink><span class="eyebrow">EVENT DETAIL</span><h1>{{ detail ? behaviorLabel(detail.event.event_type) : '事件详情' }}</h1><p>查看现场证据与处理记录，跟进每一次异常。</p></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }} <button class="text-button" @click="load">刷新详情</button></div>
  <div v-if="busy" class="panel loading-panel">正在加载事件详情…</div>
  <section v-else-if="detail" class="event-detail-grid"><div class="event-detail-main"><article class="panel event-summary-card"><div class="event-summary-card__top"><span class="severity-pill" :class="`severity-pill--${detail.event.severity}`">{{ severityLabel(detail.event.severity) }}告警</span><span class="status-pill status-pill--plain" :class="`status-pill--${detail.event.status}`">{{ statusLabel(detail.event.status) }}</span></div><h2>{{ behaviorLabel(detail.event.event_type) }}</h2><p>{{ detail.event.location }} · {{ detail.event.device_name }} · {{ detail.event.camera_name }}</p><dl class="detail-facts"><div><dt>发生时间</dt><dd>{{ formatDate(detail.event.occurred_at) }}</dd></div><div><dt>平台接收</dt><dd>{{ formatDate(detail.event.received_at) }}</dd></div><div><dt>模型版本</dt><dd>{{ detail.event.algorithm_version }}</dd></div><div><dt>规则配置</dt><dd>v{{ detail.event.rule_config_version }}</dd></div><div><dt>事件 ID</dt><dd class="mono-value">{{ detail.event.event_id }}</dd></div><div><dt>设备 ID</dt><dd class="mono-value">{{ detail.event.device_id }}</dd></div></dl><div v-if="Object.keys(detail.event.attributes ?? {}).length" class="event-attributes"><span v-if="detail.event.attributes.confidence !== undefined">置信度 {{ Math.round(Number(detail.event.attributes.confidence) * 100) }}%</span><span v-if="detail.event.attributes.observed_duration_ms !== undefined">持续 {{ detail.event.attributes.observed_duration_ms }} ms</span></div></article>
      <article class="panel evidence-panel"><div class="panel-heading"><div><span class="eyebrow">PRIVATE EVIDENCE</span><h2>现场截图</h2></div><span class="evidence-expiry">上传后保留 24 小时</span></div><div v-if="evidenceUrl" class="evidence-image-wrap"><img :src="evidenceUrl" alt="异常事件现场截图"></div><div v-else class="evidence-placeholder"><span class="empty-state__icon">▧</span><strong>{{ detail.evidence.status === 'expired' ? '截图已过期' : detail.evidence.status === 'available' ? '截图不可用' : detail.evidence.status === 'unavailable' ? '截图文件不可用' : '截图等待上传' }}</strong><p>{{ detail.evidence.status === 'expired' ? '事件记录仍可查看，截图已按保留策略到期。' : detail.evidence.status === 'available' || detail.evidence.status === 'unavailable' ? '事件记录保留；请联系设备管理员检查截图存储。' : '边缘设备可能稍后补传截图。' }}</p></div></article>
    </div><aside class="event-detail-side"><article class="panel disposition-panel"><div class="panel-heading"><div><span class="eyebrow">DISPOSITION</span><h2>处置事件</h2></div></div><label class="field"><span>处理结果 / 备注</span><textarea v-model="result" rows="4" maxlength="2000" placeholder="记录核实情况或处理方式"></textarea></label><p class="disposition-hint">{{ detail.event.status === 'pending' ? '先将事件标记为处理中，或直接标记为误报。' : detail.event.status === 'processing' ? '完成核实后可解除事件，或标记为误报。' : '该事件已进入终态，处置记录只读。' }}</p><div class="disposition-actions"><button v-if="canStart" class="button button--secondary" :disabled="saving" @click="transition('processing')">开始处理</button><button v-if="canStart" class="button button--quiet" :disabled="saving" @click="transition('false_positive')">标记误报</button><button v-if="canClose" class="button button--primary" :disabled="saving" @click="transition('resolved')">解除事件 <span aria-hidden="true">→</span></button><button v-if="canClose" class="button button--quiet" :disabled="saving" @click="transition('false_positive')">标记误报</button></div><small class="version-note">提交时使用事件版本 {{ detail.event.version }}；如有并发更新，会刷新最新状态。</small></article>
      <article class="panel timeline-panel"><div class="panel-heading"><div><span class="eyebrow">AUDIT TRAIL</span><h2>处置记录</h2></div></div><div v-if="!detail.dispositions.length" class="timeline-empty">暂无处置记录</div><ol v-else class="timeline"><li v-for="item in detail.dispositions" :key="item.disposition_id"><span class="timeline-dot"></span><div><strong>{{ statusLabel(item.from_status) }} <span>→</span> {{ statusLabel(item.to_status) }}</strong><p>{{ item.result }}</p><small>{{ formatDate(item.acted_at) }}</small></div></li></ol></article></aside></section>
</template>
