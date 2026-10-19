import { pushApi } from '../api/push'

export async function syncCurrentBrowserPushSubscription(): Promise<string | null> {
  if (typeof navigator === 'undefined' || typeof window === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) return null

  const serverSubscriptions = await pushApi.list()
  const registration = await navigator.serviceWorker.getRegistration()
  const browserSubscription = await registration?.pushManager.getSubscription()
  if (browserSubscription) {
    const saved = await pushApi.save(browserSubscription.toJSON())
    return saved.subscription_id
  }

  await Promise.all(serverSubscriptions.items.map((item) => pushApi.remove(item.subscription_id)))
  return null
}
