import { createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'
import { eventNotificationRoute } from './lib/pushRouting'
import { pwaNavigationDenylist } from './lib/pwaPolicy'

const serviceWorker = self as unknown as ServiceWorkerGlobalScope

// @ts-expect-error Workbox replaces this static token with the generated asset list.
precacheAndRoute(self.__WB_MANIFEST)
registerRoute(new NavigationRoute(createHandlerBoundToURL('/index.html'), {
  denylist: pwaNavigationDenylist,
}))

serviceWorker.addEventListener('push', (event: PushEvent) => {
  if (!event.data) return
  let payload: Record<string, unknown>
  try {
    payload = event.data.json() as Record<string, unknown>
  } catch {
    return
  }
  const eventId = typeof payload.event_id === 'string' ? payload.event_id : ''
  const route = eventNotificationRoute(eventId)
  if (!route) return
  const location = typeof payload.location === 'string' ? payload.location.slice(0, 80) : '教室'
  const severity = typeof payload.severity === 'string' ? payload.severity : '异常'
  const occurredAt = typeof payload.occurred_at === 'string' ? payload.occurred_at : ''
  const timeLabel = occurredAt && !Number.isNaN(Date.parse(occurredAt))
    ? new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit' }).format(new Date(occurredAt))
    : ''
  event.waitUntil(serviceWorker.registration.showNotification('课堂异常提醒', {
    body: [location, severity, timeLabel].filter(Boolean).join(' · '),
    tag: `yunshao-event-${eventId}`,
    data: { event_id: eventId },
    icon: '/pwa-192x192.png',
    badge: '/pwa-192x192.png',
  }))
})

serviceWorker.addEventListener('notificationclick', (event: NotificationEvent) => {
  event.notification.close()
  const eventId = event.notification.data?.event_id
  const route = eventNotificationRoute(eventId)
  if (!route) return
  const destination = new URL(route, serviceWorker.location.origin).href
  event.waitUntil((async () => {
    const windows = await serviceWorker.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const client of windows) {
      if (new URL(client.url).origin !== serviceWorker.location.origin) continue
      await client.navigate(destination)
      await client.focus()
      return
    }
    await serviceWorker.clients.openWindow(destination)
  })())
})
