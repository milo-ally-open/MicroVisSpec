import { apiRequest } from './client'

export type UserSession = {
  account_id: string
  username: string
  csrf_token: string
}

export const authApi = {
  me: () => apiRequest<UserSession>('/api/v1/auth/me'),
  login: (username: string, password: string) => apiRequest<UserSession>('/api/v1/auth/login', {
    method: 'POST', body: JSON.stringify({ username, password }), csrf: false,
  }),
  register: (username: string, password: string) => apiRequest<{ account_id: string; username: string }>('/api/v1/auth/register', {
    method: 'POST', body: JSON.stringify({ username, password }), csrf: false,
  }),
  logout: () => apiRequest<void>('/api/v1/auth/logout', { method: 'POST' }),
  changePassword: (oldPassword: string, newPassword: string, confirmation: string) => apiRequest<void>('/api/v1/auth/password', {
    method: 'PUT', body: JSON.stringify({ old_password: oldPassword, new_password: newPassword, new_password_confirmation: confirmation }),
  }),
}
