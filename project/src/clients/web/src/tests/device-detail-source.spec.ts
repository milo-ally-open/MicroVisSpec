// @vitest-environment jsdom
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  deviceGet: vi.fn(),
  deviceUpdate: vi.fn(),
  deviceDisable: vi.fn(),
  createCamera: vi.fn(),
  activationTicket: vi.fn(),
  updateRules: vi.fn(),
  mediaStatus: vi.fn(),
  putSource: vi.fn(),
}))

vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRoute: () => ({ params: { deviceId: 'device-1' } }),
    useRouter: () => ({ replace: vi.fn() }),
  }
})

vi.mock('../api/devices', () => ({
  deviceApi: {
    get: mocks.deviceGet,
    update: mocks.deviceUpdate,
    disable: mocks.deviceDisable,
    createCamera: mocks.createCamera,
    activationTicket: mocks.activationTicket,
    updateRules: mocks.updateRules,
  },
}))

vi.mock('../api/media', () => ({
  mediaApi: {
    status: mocks.mediaStatus,
    putSource: mocks.putSource,
  },
}))

import DeviceDetailPage from '../pages/DeviceDetailPage.vue'

const configuredURL = 'rtsp://192.168.0.2:8554/classroom-demo'
const deviceFixture = {
  device_id: 'device-1', name: 'Atlas', ip: '192.168.0.2', location: 'Room 1',
  activation_state: 'active', active: true, online: true, reported_state: {},
  cameras: [{ camera_id: 'camera-1', device_id: 'device-1', name: 'Camera 1', camera_key: 'front', active: true, camera_state: 'online', stream_state: 'ready' }],
  algorithms: [],
}

describe('device detail RTSP source configuration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.deviceGet.mockResolvedValue(deviceFixture)
    mocks.mediaStatus.mockResolvedValue({ camera_id: 'camera-1', device_id: 'device-1', rtsp_url: configuredURL, source_state: 'reachable', media_state: 'streaming', config_status: 'applied' })
    mocks.putSource.mockResolvedValue({ stream_id: 'stream-1', config_status: 'applied', config_version: 2 })
  })

  it('loads one saved RTSP URL field and submits only that URL', async () => {
    const wrapper = mount(DeviceDetailPage, { global: { stubs: { RouterLink: true, AppIcon: true } } })
    await flushPromises()

    const fields = wrapper.findAll('.source-form input')
    expect(fields).toHaveLength(1)
    expect((fields[0].element as HTMLInputElement).value).toBe(configuredURL)
    await fields[0].setValue(configuredURL)
    await wrapper.find('.source-form button').trigger('click')
    await flushPromises()

    expect(mocks.putSource).toHaveBeenCalledWith('camera-1', { rtsp_url: configuredURL })
    expect(wrapper.text()).toContain('RTSP 地址已应用')
    wrapper.unmount()
  })

  it('rejects an RTSP URL that embeds user information', async () => {
    const wrapper = mount(DeviceDetailPage, { global: { stubs: { RouterLink: true, AppIcon: true } } })
    await flushPromises()
    await wrapper.find('.source-form input').setValue('rtsp://user:password@192.168.0.2:8554/stream')
    await wrapper.find('.source-form button').trigger('click')
    await flushPromises()

    expect(mocks.putSource).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('不含账号信息')
    wrapper.unmount()
  })
})
