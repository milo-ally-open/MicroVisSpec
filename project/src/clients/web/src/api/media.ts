import { apiRequest } from './client'

export type CameraStatus = {
  camera_id: string
  device_id: string
  stream_id?: string
  source_state: string
  media_state: string
  browser_compatibility?: string
  config_status: string
  last_error?: string
}

export type PlaybackLease = {
  stream_id: string
  lease_id: string
  expires_at: string
  status: string
  whep_url: string
  hls_url: string
}

export const mediaApi = {
  putSource: (cameraId: string, source: { rtsp_url: string; source_username?: string; source_password?: string }) => apiRequest<{ stream_id: string; config_status: string; config_version: number }>(`/api/v1/media/cameras/${encodeURIComponent(cameraId)}/source`, { method: 'PUT', body: JSON.stringify(source) }),
  status: (cameraId: string) => apiRequest<CameraStatus>(`/api/v1/media/cameras/${encodeURIComponent(cameraId)}/status`),
  playback: (cameraId: string) => apiRequest<PlaybackLease>(`/api/v1/media/cameras/${encodeURIComponent(cameraId)}/playback`),
  renew: (leaseId: string) => apiRequest<{ lease_id: string; expires_at: string }>(`/api/v1/media/viewer-leases/${encodeURIComponent(leaseId)}/renew`, { method: 'POST', body: '{}' }),
  release: (leaseId: string) => apiRequest<void>(`/api/v1/media/viewer-leases/${encodeURIComponent(leaseId)}`, { method: 'DELETE' }),
}
