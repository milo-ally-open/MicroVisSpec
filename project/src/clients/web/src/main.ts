import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { clearSession } from './state/auth'
import './style.css'

createApp(App).use(router).mount('#app')

let redirectingExpiredSession = false
window.addEventListener('auth:expired', () => {
  clearSession()
  if (redirectingExpiredSession || !router.currentRoute.value.meta.requiresAuth) return

  redirectingExpiredSession = true
  void router.replace({ name: 'login', query: { next: router.currentRoute.value.fullPath } })
    .finally(() => { redirectingExpiredSession = false })
})
