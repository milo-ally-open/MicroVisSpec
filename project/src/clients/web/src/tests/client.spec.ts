// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiRequest, clearCsrfToken, csrfHeaders, setCsrfToken } from '../api/client'
import { createDefaultClassroomRules, validateClassroomRules } from '../lib/classroomRules'
import { safeInAppPath } from '../lib/navigation'
import { eventNotificationRoute } from '../lib/pushRouting'
import { pwaNavigationDenylist } from '../lib/pwaPolicy'
import { authState, clearSession, setSession } from '../state/auth'

afterEach(() => {
  clearCsrfToken()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('same-origin API client', () => {
  it('sends the in-memory CSRF token on cookie-authenticated writes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{"saved":true}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    setCsrfToken('csrf-test-token')

    await expect(apiRequest<{ saved: boolean }>('/api/v1/devices', {
      method: 'POST',
      body: JSON.stringify({ name: 'Room 1' }),
    })).resolves.toEqual({ saved: true })

    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = new Headers(init.headers)
    expect(init.credentials).toBe('include')
    expect(init.cache).toBe('no-store')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-test-token')
    expect(headers.get('Content-Type')).toContain('application/json')
  })

  it('adds the CSRF token to raw media request headers', () => {
    setCsrfToken('csrf-test-token')
    const headers = csrfHeaders({ 'Content-Type': 'application/sdp' })
    expect(headers.get('Content-Type')).toBe('application/sdp')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-test-token')
  })

  it('signals expired authentication and maps conflicts to a safe message', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'version_conflict', message: 'private backend detail' },
      request_id: 'req-test',
    }), { status: 409, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const expired = vi.fn()
    window.addEventListener('auth:expired', expired)

    await expect(apiRequest('/api/v1/events/123')).rejects.toMatchObject({
      status: 409,
      code: 'version_conflict',
      requestId: 'req-test',
      message: '内容已发生变化，请刷新后重试。',
    })
    expect(expired).not.toHaveBeenCalled()

    fetchMock.mockResolvedValueOnce(new Response('{}', { status: 401 }))
    await expect(apiRequest('/api/v1/devices')).rejects.toMatchObject({ status: 401 })
    expect(expired).toHaveBeenCalledTimes(1)
    window.removeEventListener('auth:expired', expired)
  })

  it('reports media capacity errors as capacity errors', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'capacity_full', message: 'internal quota detail' },
    }), { status: 429, headers: { 'Content-Type': 'application/json' } })))
    await expect(apiRequest('/api/v1/media/cameras/camera-test/playback')).rejects.toMatchObject({
      code: 'capacity_full',
      message: '观看容量已满，请关闭其他监控画面后重试。',
    })
  })
})

describe('session state', () => {
  it('clears the in-memory user and CSRF token on expiry', async () => {
    setSession({ account_id: 'account-test', username: 'teacher', csrf_token: 'csrf-test-token' })
    clearSession()
    expect(authState.user).toBeNull()

    const fetchMock = vi.fn().mockResolvedValue(new Response('{"ok":true}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    await apiRequest('/api/v1/test', { method: 'POST', body: '{}' })
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false)
  })
})

describe('classroom alert rules', () => {
  it('starts all supported alerts disabled and accepts valid enabled rules', () => {
    const rules = createDefaultClassroomRules()
    expect(Object.values(rules).every((rule) => !rule.enabled)).toBe(true)
    expect(validateClassroomRules(rules)).toBeNull()
    rules.reading.enabled = true
    expect(validateClassroomRules(rules)).toBeNull()
  })

  it('rejects non-finite thresholds for an enabled alert', () => {
    const rules = createDefaultClassroomRules()
    rules.writing.enabled = true
    rules.writing.confidence_threshold = Number.NaN
    expect(validateClassroomRules(rules)).toContain('0–1 置信度')
  })
})

describe('navigation and PWA boundaries', () => {
  it('accepts safe in-app paths and rejects external or backslash redirects', () => {
    expect(safeInAppPath('/events?status=pending')).toBe('/events?status=pending')
    expect(safeInAppPath('//example.com')).toBeNull()
    expect(safeInAppPath('/\\example.com')).toBeNull()
  })

  it('limits notification routes to event UUIDs and excludes sensitive navigation paths', () => {
    expect(eventNotificationRoute('123e4567-e89b-42d3-a456-426614174000')).toBe('/events/123e4567-e89b-42d3-a456-426614174000')
    expect(eventNotificationRoute('https://example.com')).toBeNull()
    expect(pwaNavigationDenylist.some((pattern) => pattern.test('/api/v1/events'))).toBe(true)
    expect(pwaNavigationDenylist.some((pattern) => pattern.test('/evidence/123'))).toBe(true)
    expect(pwaNavigationDenylist.some((pattern) => pattern.test('/media/camera-1'))).toBe(true)
    expect(pwaNavigationDenylist.some((pattern) => pattern.test('/push/subscriptions'))).toBe(true)
    expect(pwaNavigationDenylist.some((pattern) => pattern.test('/events'))).toBe(false)
  })
})
