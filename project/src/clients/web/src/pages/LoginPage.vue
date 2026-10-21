<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { authApi } from '../api/auth'
import { errorMessage } from '../api/client'
import { setSession } from '../state/auth'
import { safeInAppPath } from '../lib/navigation'
import AuthBrandPanel from '../components/AuthBrandPanel.vue'
import AppIcon from '../components/AppIcon.vue'
import BrandLockup from '../components/BrandLockup.vue'

const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const showPassword = ref(false)
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
    <AuthBrandPanel />
    <section class="auth-panel"><div class="auth-card"><div class="auth-mobile-brand"><BrandLockup compact /></div><span class="eyebrow">欢迎回来</span><h2>登录工作台</h2><p class="auth-description">输入你的账号信息，继续查看设备与异常事件。</p>
      <form class="form-stack" @submit.prevent="submit">
        <label class="field"><span>用户名</span><input v-model="username" name="username" autocomplete="username" required minlength="3" maxlength="32" placeholder="输入用户名"></label>
        <div class="field"><label for="login-password">密码</label><div class="password-field"><input id="login-password" v-model="password" name="password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" required minlength="12" placeholder="请输入密码"><button class="password-toggle" type="button" :aria-label="showPassword ? '隐藏密码' : '显示密码'" :aria-pressed="showPassword" @click="showPassword = !showPassword"><AppIcon :name="showPassword ? 'eyeOff' : 'eye'" /></button></div></div>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <button class="button button--primary button--wide" type="submit" :disabled="busy">{{ busy ? '正在登录…' : '登录' }} <span aria-hidden="true">→</span></button>
      </form>
      <p class="auth-switch">还没有账号？<RouterLink to="/register">创建账号</RouterLink></p><p class="auth-privacy">会话由安全 Cookie 管理，页面不会保存密码或访问令牌。</p>
    </div></section>
  </main>
</template>
