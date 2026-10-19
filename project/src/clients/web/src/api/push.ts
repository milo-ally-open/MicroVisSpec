import { apiRequest } from './client'

export type PushSubscriptionItem = { subscription_id: string; active: boolean; created_at: string }

export const pushApi = {
  vapidKey: () => apiRequest<{ vapid_public_key: string }>('/api/v1/push/vapid-public-key'),
  list: () => apiRequest<{ items: PushSubscriptionItem[] }>('/api/v1/push/subscriptions'),
  save: (subscription: PushSubscriptionJSON) => apiRequest<{ subscription_id: string; active: boolean }>('/api/v1/push/subscriptions', { method: 'PUT', body: JSON.stringify(subscription) }),
  remove: (id: string) => apiRequest<void>(`/api/v1/push/subscriptions/${encodeURIComponent(id)}`, { method: 'DELETE' }),
}
