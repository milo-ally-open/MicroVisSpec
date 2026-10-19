import { reactive } from 'vue'
import { authApi, type UserSession } from '../api/auth'
import { clearCsrfToken, setCsrfToken } from '../api/client'
import { syncCurrentBrowserPushSubscription } from '../lib/pushSubscription'

export const authState = reactive<{ user: UserSession | null }>({ user: null })

export function setSession(user: UserSession) {
  authState.user = user
  setCsrfToken(user.csrf_token)
  void syncCurrentBrowserPushSubscription().catch(() => undefined)
}

export function clearSession() {
  authState.user = null
  clearCsrfToken()
}

let sessionRequest: Promise<boolean> | null = null

export function ensureSession(): Promise<boolean> {
  if (authState.user) return Promise.resolve(true)
  if (sessionRequest) return sessionRequest

  sessionRequest = authApi.me()
    .then((user) => {
      setSession(user)
      return true
    })
    .catch(() => {
      clearSession()
      return false
    })
    .finally(() => {
      sessionRequest = null
    })

  return sessionRequest
}
