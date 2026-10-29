import { apiRequest } from './client'

export type Camera = {
  camera_id: string
  device_id: string
  name: string
  camera_key: string
  active: boolean
  camera_state: string
  stream_state: string
}

export type AlgorithmAssignment = {
  assignment_id: string
  device_id: string
  algorithm_key: string
  algorithm_version: string
  runtime_state: string
  config_state: string
  capabilities: Record<string, unknown>
  reported_parameters: Record<string, unknown>
  desired_parameters: Record<string, unknown>
  desired_version: number
  reported_version: number
}

export type Device = {
  device_id: string
  name: string
  ip: string
  location: string
  activation_state: string
  active: boolean
  provisioned?: boolean
  activation_expires_at: string | null
  online: boolean
  last_seen_at?: string
  reported_state: Record<string, unknown>
  cameras: Camera[]
  algorithms: AlgorithmAssignment[]
}

export type DeviceStats = { registered: number; online: number; as_of: string }
export type DevicePage = { items: Device[]; next_cursor: string }
export type ActivationRequest = {
  request_id: string
  status: 'pending' | 'syncing' | 'activated' | 'expired' | 'superseded'
  expires_at: string
  activation_expires_at?: string | null
}
export type ProvisioningHostKey = { fingerprint: string; pinned: boolean }
export type ProvisioningJob = {
  job_id: string
  device_id: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'
  stage: string
  error_code?: string
  message: string
  created_at: string
  updated_at: string
  finished_at?: string
}

export const deviceApi = {
  list: (cursor = '', limit = 100) => apiRequest<DevicePage>(`/api/v1/devices?limit=${limit}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  listAll: async () => {
    const items: Device[] = []
    let cursor = ''
    do {
      const page = await deviceApi.list(cursor)
      items.push(...page.items)
      if (page.next_cursor && page.next_cursor === cursor) throw new Error('设备分页游标未前进。')
      cursor = page.next_cursor
    } while (cursor)
    return items
  },
  stats: () => apiRequest<DeviceStats>('/api/v1/devices/stats'),
  get: (id: string) => apiRequest<Device>(`/api/v1/devices/${encodeURIComponent(id)}`),
  create: (input: { name: string; ip: string; location: string }) => apiRequest<{ device_id: string; activation_state: string }>('/api/v1/devices', { method: 'POST', body: JSON.stringify(input) }),
  update: (id: string, input: Partial<Pick<Device, 'name' | 'ip' | 'location'>>) => apiRequest<Device>(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(input) }),
  disable: (id: string) => apiRequest<void>(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  createCamera: (id: string, input: { name: string; camera_key: string }) => apiRequest<Camera>(`/api/v1/devices/${encodeURIComponent(id)}/cameras`, { method: 'POST', body: JSON.stringify(input) }),
  createActivationRequest: (id: string, durationSeconds: number | null) => apiRequest<ActivationRequest>(`/api/v1/devices/${encodeURIComponent(id)}/activation-requests`, { method: 'POST', body: JSON.stringify({ duration_seconds: durationSeconds }) }),
  getActivationRequest: (id: string, requestId: string) => apiRequest<ActivationRequest>(`/api/v1/devices/${encodeURIComponent(id)}/activation-requests/${encodeURIComponent(requestId)}`),
  getProvisioningHostKey: (id: string) => apiRequest<ProvisioningHostKey>(`/api/v1/devices/${encodeURIComponent(id)}/provisioning/ssh-host-key`, { method: 'POST', body: '{}' }),
  startProvisioning: (id: string, input: { ssh_username: string; ssh_password: string; host_key_fingerprint: string; duration_seconds: number | null }) => apiRequest<ProvisioningJob>(`/api/v1/devices/${encodeURIComponent(id)}/provisioning`, { method: 'POST', body: JSON.stringify(input) }),
  getLatestProvisioningJob: async (id: string) => (await apiRequest<{ job: ProvisioningJob | null }>(`/api/v1/devices/${encodeURIComponent(id)}/provisioning`)).job,
  getProvisioningJob: (id: string, jobId: string) => apiRequest<ProvisioningJob>(`/api/v1/devices/${encodeURIComponent(id)}/provisioning/${encodeURIComponent(jobId)}`),
  updateRules: (id: string, assignmentId: string, expectedVersion: number, parameters: Record<string, unknown>) => apiRequest<{ command_id: string; desired_version: number; status: string }>(`/api/v1/devices/${encodeURIComponent(id)}/algorithms/${encodeURIComponent(assignmentId)}/parameters`, { method: 'PUT', body: JSON.stringify({ expected_version: expectedVersion, parameters }) }),
}
