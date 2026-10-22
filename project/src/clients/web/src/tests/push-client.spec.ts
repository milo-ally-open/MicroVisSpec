import { afterEach, describe, expect, it, vi } from 'vitest'
import { pushApi } from '../api/push'

afterEach(() => vi.unstubAllGlobals())

describe('browser push subscription API boundary', () => {
  it('accepts browser JSON including nullable expiration without forwarding unsupported fields', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ subscription_id: 'test', active: true })))
    vi.stubGlobal('fetch', fetchMock)
    await pushApi.save({ endpoint: 'https://push.example.test/device', expirationTime: null, keys: { p256dh: 'public-key', auth: 'auth-key' } })
    const [, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(String(request.body))
    expect(body).toEqual({ endpoint: 'https://push.example.test/device', keys: { p256dh: 'public-key', auth: 'auth-key' } })
  })
})
