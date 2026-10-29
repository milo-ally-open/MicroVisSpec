<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { eventApi, type EventRecord } from '../api/events'
import { errorMessage } from '../api/client'
import { behaviorLabel, formatDate, severityLabel, statusLabel } from '../lib/format'

const events = ref<EventRecord[]>([])
const busy = ref(true)
const error = ref('')
async function load() {
  busy.value = true
  try { events.value = (await eventApi.list({ limit: 100 })).items }
  catch (cause) { error.value = errorMessage(cause) }
  finally { busy.value = false }
}
onMounted(load)
</script>

<template>
  <section class="page-heading"><span class="eyebrow">AUTHORIZED EVENTS</span><h1>授权范围内的事件</h1><p>只能查看已获设备权限的事件和证据。</p></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }} <button class="text-button" @click="load">重试</button></div>
  <section class="panel mobile-event-list"><p v-if="busy">正在读取事件…</p><p v-else-if="!events.length" class="empty-state">没有授权范围内的事件。</p><RouterLink v-for="event in events" :key="event.event_id" :to="`/m/events/${event.event_id}`" class="mobile-event"><span class="severity-pill" :class="`severity-pill--${event.severity}`">{{ severityLabel(event.severity) }}</span><span class="mobile-event__copy"><strong>{{ behaviorLabel(event.event_type) }}</strong><small>{{ event.device_name }} · {{ event.camera_name }} · {{ event.location }}</small><small>{{ statusLabel(event.status) }} · {{ formatDate(event.occurred_at) }}</small></span></RouterLink></section>
</template>

<style scoped>
.mobile-event-list { padding: 20px; }
.mobile-event { display: flex; align-items: flex-start; gap: 12px; padding: 15px 0; border-bottom: 1px solid var(--line); }
.mobile-event:last-child { border-bottom: 0; }
.mobile-event__copy { display: grid; min-width: 0; gap: 4px; }
.mobile-event small { color: var(--muted); }
</style>
