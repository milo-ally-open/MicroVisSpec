import { apiRequest } from './client'

export type UserSession = {
  account_id: string
  username: string
  user_type: 'admin' | 'pc' | 'mobile'
  csrf_token: string
}

export type DeviceGrant = { device_id: string; capabilities: string[] }
export type WorkspaceInfo = { workspace_id: string; name: string; status: string; role: 'owner' | 'member'; member_id?: string; device_grants?: DeviceGrant[] }
export type WorkspaceMember = { member_id: string; account_id: string; username: string; status: string; joined_at: string; device_grants: DeviceGrant[] }
export type WorkspaceInvitation = { invitation_id: string; workspace_id: string; created_by: string; expires_at: string; used_at?: string | null; revoked_at?: string | null; created_at: string }
export type AdminAccount = { account_id: string; username: string; user_type: 'admin' | 'pc' | 'mobile'; status: string; created_at: string; updated_at: string }
export type AdminHealth = { services: Array<{ name: string; status: string; checked_at: string; dependencies?: Record<string, string> }>; dependencies: Record<string, string>; checked_at: string }

export const authApi = {
  me: () => apiRequest<UserSession>('/api/v1/auth/me'),
  login: (username: string, password: string) => apiRequest<UserSession>('/api/v1/auth/login', {
    method: 'POST', body: JSON.stringify({ username, password }), csrf: false,
  }),
  register: (username: string, password: string) => apiRequest<{ account_id: string; username: string; user_type: 'pc' }>('/api/v1/auth/register', {
    method: 'POST', body: JSON.stringify({ username, password }), csrf: false,
  }),
  logout: () => apiRequest<void>('/api/v1/auth/logout', { method: 'POST' }),
  changePassword: (oldPassword: string, newPassword: string, confirmation: string) => apiRequest<void>('/api/v1/auth/password', {
    method: 'PUT', body: JSON.stringify({ old_password: oldPassword, new_password: newPassword, new_password_confirmation: confirmation }),
  }),
  workspace: () => apiRequest<WorkspaceInfo>('/api/v1/auth/workspace'),
  members: () => apiRequest<{ workspace_id: string; items: WorkspaceMember[] }>('/api/v1/auth/workspace/members'),
  replaceMemberGrants: (memberId: string, deviceGrants: DeviceGrant[]) => apiRequest<void>(`/api/v1/auth/workspace/members/${encodeURIComponent(memberId)}/grants`, {
    method: 'PUT', body: JSON.stringify({ device_grants: deviceGrants }),
  }),
  revokeMember: (memberId: string) => apiRequest<void>(`/api/v1/auth/workspace/members/${encodeURIComponent(memberId)}`, { method: 'DELETE' }),
  invitations: () => apiRequest<{ items: WorkspaceInvitation[] }>('/api/v1/auth/workspace/invitations'),
  createInvitation: (deviceGrants: DeviceGrant[]) => apiRequest<{ invitation_id: string; invite_token: string; expires_at: string; uses_remaining: number }>('/api/v1/auth/workspace/invitations', {
    method: 'POST', body: JSON.stringify({ device_grants: deviceGrants }),
  }),
  revokeInvitation: (invitationId: string) => apiRequest<void>(`/api/v1/auth/workspace/invitations/${encodeURIComponent(invitationId)}`, { method: 'DELETE' }),
  redeemInvitation: (token: string, username: string, password: string) => apiRequest<{ account_id: string; username: string; user_type: 'mobile'; workspace_id: string; member_id: string }>('/api/v1/invitations/redeem', {
    method: 'POST', body: JSON.stringify({ token, username, password }), csrf: false,
  }),
  adminAccounts: () => apiRequest<{ items: AdminAccount[]; truncated: boolean }>('/api/v1/admin/accounts'),
  adminHealth: () => apiRequest<AdminHealth>('/api/v1/admin/health'),
  adminAudit: () => apiRequest<{ items: Array<{ audit_id: string; actor_account_id: string; workspace_id: string; action: string; resource_id: string; result: string; request_id: string; created_at: string }>; truncated: boolean }>('/api/v1/admin/audit'),
  adminCreatePC: (username: string, password: string) => apiRequest<{ account_id: string; username: string; user_type: 'pc'; status: string }>('/api/v1/admin/accounts', {
    method: 'POST', body: JSON.stringify({ username, password }),
  }),
  adminSetStatus: (accountId: string, status: 'active' | 'suspended') => apiRequest<void>(`/api/v1/admin/accounts/${encodeURIComponent(accountId)}/status`, {
    method: 'PATCH', body: JSON.stringify({ status }),
  }),
}
