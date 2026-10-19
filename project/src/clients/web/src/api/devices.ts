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
  online: boolean
  last_seen_at?: string
  reported_state: Record<string, unknown>
  cameras: Camera[]
  algorithms: AlgorithmAssignment[]
}

export type DeviceStats = { registered: number; online: number; as_of: string }
export type DevicePage = { items: Device[]; next_cursor: string }

export const deviceApi = {
  list: () => apiRequest<DevicePage>('/api/v1/devices?limit=100'),
  stats: () => apiRequest<DeviceStats>('/api/v1/devices/stats'),
  get: (id: string) => apiRequest<Device>(`/api/v1/devices/${encodeURIComponent(id)}`),
  create: (input: { name: string; ip: string; location: string }) => apiRequest<{ device_id: string; activation_state: string }>('/api/v1/devices', { method: 'POST', body: JSON.stringify(input) }),
  update: (id: string, input: Partial<Pick<Device, 'name' | 'ip' | 'location'>>) => apiRequest<Device>(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(input) }),
  disable: (id: string) => apiRequest<void>(`/api/v1/devices/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  createCamera: (id: string, input: { name: string; camera_key: string }) => apiRequest<Camera>(`/api/v1/devices/${encodeURIComponent(id)}/cameras`, { method: 'POST', body: JSON.stringify(input) }),
  activationTicket: (id: string) => apiRequest<{ device_id: string; activation_ticket: string; expires_at: string }>(`/api/v1/devices/${encodeURIComponent(id)}/activation-tickets`, { method: 'POST', body: '{}' }),
  updateRules: (id: string, assignmentId: string, expectedVersion: number, parameters: Record<string, unknown>) => apiRequest<{ command_id: string; desired_version: number; status: string }>(`/api/v1/devices/${encodeURIComponent(id)}/algorithms/${encodeURIComponent(assignmentId)}/parameters`, { method: 'PUT', body: JSON.stringify({ expected_version: expectedVersion, parameters }) }),
}
