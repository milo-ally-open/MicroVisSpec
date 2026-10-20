<script setup lang="ts">
import { ref } from 'vue'
import { RouterLink } from 'vue-router'
import { authApi } from '../api/auth'
import { errorMessage } from '../api/client'
import AuthBrandPanel from '../components/AuthBrandPanel.vue'

const username = ref('')
const password = ref('')
const confirmation = ref('')
const busy = ref(false)
const error = ref('')
const created = ref(false)

async function submit() {
  error.value = ''
  if (!/^[A-Za-z0-9_]{3,32}$/.test(username.value)) {
    error.value = '用户名需为 3–32 位英文字母、数字或下划线。'
    return
  }
  if (password.value !== confirmation.value) {
    error.value = '两次输入的密码不一致。'
    return
  }
  busy.value = true
  try {
    await authApi.register(username.value.trim(), password.value)
    created.value = true
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-page"><AuthBrandPanel /><section class="auth-panel"><div class="auth-card"><div class="auth-mobile-brand"><span class="brand-mark">Y</span><strong>云哨</strong></div><span class="eyebrow">创建个人账号</span><h2>{{ created ? '账号已创建' : '开始使用云哨' }}</h2><p class="auth-description">{{ created ? '请返回登录页，使用刚创建的账号进入工作台。' : '首发仅支持个人账号，设备与事件仅对所属账号可见。' }}</p>
      <form v-if="!created" class="form-stack" @submit.prevent="submit"><label class="field"><span>用户名</span><input v-model="username" autocomplete="username" required minlength="3" maxlength="32" pattern="[A-Za-z0-9_]+" placeholder="3–32 位字母、数字或下划线"></label><label class="field"><span>密码</span><input v-model="password" type="password" autocomplete="new-password" required minlength="12" placeholder="至少 12 位"></label><label class="field"><span>再次输入密码</span><input v-model="confirmation" type="password" autocomplete="new-password" required minlength="12" placeholder="确认密码"></label><p v-if="error" class="form-error" role="alert">{{ error }}</p><button class="button button--primary button--wide" type="submit" :disabled="busy">{{ busy ? '正在创建…' : '创建账号' }} <span aria-hidden="true">→</span></button></form>
      <RouterLink v-else class="button button--primary button--wide auth-created" to="/login">前往登录 <span aria-hidden="true">→</span></RouterLink>
      <p class="auth-switch">已有账号？<RouterLink to="/login">返回登录</RouterLink></p><p class="auth-privacy">密码使用安全哈希保存，页面不会持久化会话凭据。</p>
    </div></section></main>
</template>
