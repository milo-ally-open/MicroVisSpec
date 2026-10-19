import { apiBlob, apiRequest } from './client'

export type EventRecord = {
  event_id: string
  device_id: string
  camera_id: string
  event_type: string
  severity: string
  occurred_at: string
  received_at: string
  device_name: string
  camera_name: string
  location: string
  algorithm_key: string
  algorithm_version: string
  rule_config_version: number
  attributes: Record<string, unknown>
  status: string
  version: number
}

export type EventPage = { items: EventRecord[]; next_cursor: string | null }
export type EventDetail = {
  event: EventRecord
  evidence: { status: 'missing' | 'available' | 'expired' | 'unavailable' }
  dispositions: Array<{ disposition_id: string; from_status: string; to_status: string; result: string; acted_at: string }>
}
export type EventCount = { key: string; count: number }
export type EventStats = {
  range: string
  from: string
  to: string
  as_of: string
  total_events: number
  open_events: number
  trend: EventCount[]
  by_severity: EventCount[]
  by_type: EventCount[]
  by_location: EventCount[]
  by_status: EventCount[]
}

export type EventFilters = {
  start_at?: string
  end_at?: string
  event_type?: string
  severity?: string
  device_id?: string
  camera_id?: string
  location?: string
  status?: string
  cursor?: string
  limit?: number
}

export const eventApi = {
  list: (filters: EventFilters = {}) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(filters)) if (value !== undefined && value !== '') query.set(key, String(value))
    return apiRequest<EventPage>(`/api/v1/events${query.size ? `?${query}` : ''}`)
  },
  detail: (id: string) => apiRequest<EventDetail>(`/api/v1/events/${encodeURIComponent(id)}`),
  transition: (id: string, toStatus: string, result: string, expectedVersion: number) => apiRequest<{ event: EventRecord }>(`/api/v1/events/${encodeURIComponent(id)}/transitions`, { method: 'POST', body: JSON.stringify({ to_status: toStatus, result, expected_version: expectedVersion }) }),
  stats: (range: 'today' | '7d' | '30d') => apiRequest<EventStats>(`/api/v1/events/stats?range=${range}`),
  evidence: (id: string) => apiBlob(`/api/v1/events/${encodeURIComponent(id)}/evidence`),
}
