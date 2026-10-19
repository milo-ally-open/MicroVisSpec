<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { authApi } from '../api/auth'
import { errorMessage } from '../api/client'
import { setSession } from '../state/auth'
import { safeInAppPath } from '../lib/navigation'

const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  error.value = ''
  busy.value = true
  try {
    setSession(await authApi.login(username.value.trim(), password.value))
    const destination = safeInAppPath(route.query.next) ?? '/'
    await router.replace(destination)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-aside"><RouterLink class="brand brand--light" to="/"><span class="brand-mark">Y</span><span><strong>云哨</strong><small>EDGE INTELLIGENCE</small></span></RouterLink><div class="auth-aside__copy"><span class="eyebrow eyebrow--light">CLOUD · EDGE · CAMPUS</span><h1>让课堂异常<br>清晰可见。</h1><p>从边缘设备到告警处置，在一个工作台里掌握现场状态。</p></div><div class="auth-aside__foot">云边协同 · 实时感知 · 有据可查</div></section>
    <section class="auth-panel"><div class="auth-card"><div class="auth-mobile-brand"><span class="brand-mark">Y</span><strong>云哨</strong></div><span class="eyebrow">欢迎回来</span><h2>登录工作台</h2><p class="auth-description">输入你的账号信息，继续查看设备与课堂事件。</p>
      <form class="form-stack" @submit.prevent="submit">
        <label class="field"><span>用户名</span><input v-model="username" name="username" autocomplete="username" required minlength="3" maxlength="32" placeholder="输入用户名"></label>
        <label class="field"><span>密码</span><input v-model="password" name="password" type="password" autocomplete="current-password" required minlength="12" placeholder="至少 12 位"></label>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <button class="button button--primary button--wide" type="submit" :disabled="busy">{{ busy ? '正在登录…' : '登录' }} <span aria-hidden="true">→</span></button>
      </form>
      <p class="auth-switch">还没有账号？<RouterLink to="/register">创建账号</RouterLink></p><p class="auth-privacy">会话由安全 Cookie 管理，页面不会保存密码或访问令牌。</p>
    </div></section>
  </main>
</template>
