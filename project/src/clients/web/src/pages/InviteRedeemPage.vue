<script setup lang="ts">
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { authApi } from '../api/auth'
import { errorMessage } from '../api/client'

const route = useRoute()
const token = ref(typeof route.query.token === 'string' ? route.query.token : '')
const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')
const completed = ref(false)

async function redeem() {
  error.value = ''
  busy.value = true
  try {
    await authApi.redeemInvitation(token.value.trim(), username.value.trim(), password.value)
    completed.value = true
    password.value = ''
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="invite-page">
    <section class="panel invite-card">
      <span class="eyebrow">WORKSPACE INVITATION</span>
      <h1>加入工作空间</h1>
      <p v-if="completed" class="form-success" role="status">账号已创建并绑定工作空间。请登录后查看已授权设备。</p>
      <form v-else class="form-stack" @submit.prevent="redeem">
        <label class="field"><span>邀请凭据</span><input v-model="token" type="password" autocomplete="one-time-code" required></label>
        <label class="field"><span>用户名</span><input v-model="username" autocomplete="username" minlength="3" maxlength="32" required></label>
        <label class="field"><span>设置密码</span><input v-model="password" type="password" autocomplete="new-password" minlength="12" required></label>
        <p class="settings-footnote">此邀请只能使用一次，并在 24 小时后失效。</p>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <button class="button button--primary" type="submit" :disabled="busy || !token">{{ busy ? '正在验证…' : '接受邀请并创建移动账号' }}</button>
      </form>
      <RouterLink v-if="completed" class="button button--primary" to="/login">前往登录</RouterLink>
      <RouterLink v-else class="text-button" to="/login">已有账号？登录</RouterLink>
    </section>
  </main>
</template>

<style scoped>
.invite-page { min-height: 100vh; display: grid; place-items: center; padding: 24px; background: var(--canvas); }
.invite-card { width: min(100%, 480px); padding: 34px; }
.invite-card h1 { margin: 6px 0 8px; }
.invite-card > .text-button, .invite-card > .button { margin-top: 16px; }
</style>
