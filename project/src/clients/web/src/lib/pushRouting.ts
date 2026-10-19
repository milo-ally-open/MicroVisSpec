const EVENT_ID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i

export function eventNotificationRoute(eventId: unknown): string | null {
  return typeof eventId === 'string' && EVENT_ID_PATTERN.test(eventId)
    ? `/events/${eventId}`
    : null
}
