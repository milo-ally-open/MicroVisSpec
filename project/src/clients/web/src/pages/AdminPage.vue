<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { authApi, type AdminAccount, type AdminHealth } from '../api/auth'
import { errorMessage } from '../api/client'
import { clearSession, authState } from '../state/auth'

const router = useRouter()
const accounts = ref<AdminAccount[]>([])
const audit = ref<Array<{ audit_id: string; actor_account_id: string; workspace_id: string; action: string; resource_id: string; result: string; request_id: string; created_at: string }>>([])
const health = ref<AdminHealth | null>(null)
const busy = ref(true)
const saving = ref(false)
const error = ref('')
const message = ref('')
const accountForm = ref({ username: '', password: '' })

async function load() {
  busy.value = true
  error.value = ''
  const [accountResult, auditResult, healthResult] = await Promise.allSettled([authApi.adminAccounts(), authApi.adminAudit(), authApi.adminHealth()])
  if (accountResult.status === 'fulfilled') accounts.value = accountResult.value.items
  if (auditResult.status === 'fulfilled') audit.value = auditResult.value.items
  if (healthResult.status === 'fulfilled') health.value = healthResult.value
  const failed = [accountResult, auditResult, healthResult].find((item) => item.status === 'rejected')
  if (failed?.status === 'rejected') error.value = errorMessage(failed.reason)
  busy.value = false
}

async function createPC() {
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    await authApi.adminCreatePC(accountForm.value.username.trim(), accountForm.value.password)
    accountForm.value = { username: '', password: '' }
    message.value = 'PC 账号及其工作空间已创建。请通过受控渠道传递初始密码。'
    await load()
  } catch (cause) { error.value = errorMessage(cause) }
  finally { saving.value = false }
}

async function setStatus(account: AdminAccount) {
  const next = account.status === 'active' ? 'suspended' : 'active'
  const action = next === 'active' ? '恢复' : '停用'
  if (!window.confirm(`${action}账号 ${account.username}？`)) return
  saving.value = true
  error.value = ''
  try {
    await authApi.adminSetStatus(account.account_id, next)
    message.value = next === 'active' ? `已恢复 ${account.username}。` : `已停用 ${account.username}，其现有会话已撤销。`
    await load()
  } catch (cause) { error.value = errorMessage(cause) }
  finally { saving.value = false }
}

async function signOut() {
  try { await authApi.logout() } finally { clearSession(); await router.replace('/login') }
}

const roleLabel = (value: string) => ({ admin: '管理员', pc: 'PC 所有者', mobile: '移动成员' }[value] ?? value)
const statusLabel = (value: string) => value === 'active' ? '正常' : '已停用'
const healthLabel = (value: string) => value === 'ready' ? '正常' : value === 'unavailable' ? '不可用' : '未知'
const timeLabel = (value: string) => new Date(value).toLocaleString()

onMounted(load)
</script>

<template>
  <main class="admin-shell">
    <header class="admin-header"><div><span class="eyebrow">MICROVISSPEC PLATFORM</span><h1>平台管理</h1><p>账号、服务健康与安全审计</p></div><div><span class="admin-identity">{{ authState.user?.username }}</span><button class="button button--secondary" @click="signOut">退出登录</button></div></header>
    <div v-if="error" class="notice notice--error" role="alert">{{ error }}</div>
    <div v-if="message" class="notice notice--success" role="status">{{ message }}</div>

    <section class="panel admin-panel"><div class="panel-heading"><div><span class="eyebrow">PLATFORM HEALTH</span><h2>服务状态</h2></div><button class="text-button" :disabled="busy" @click="load">{{ busy ? '检查中…' : '重新检查' }}</button></div><p v-if="!health && busy">正在读取健康状态…</p><div v-else-if="health" class="health-grid"><article v-for="service in health.services" :key="service.name" class="health-card"><span>{{ service.name }}</span><strong :class="`health-${service.status}`">{{ healthLabel(service.status) }}</strong><small>检查于 {{ timeLabel(service.checked_at) }}</small></article><article v-for="(status, name) in health.dependencies" :key="`dep-${name}`" class="health-card health-card--dependency"><span>{{ name }}</span><strong :class="`health-${status}`">{{ healthLabel(status) }}</strong><small>由服务就绪探针汇总</small></article></div><p class="admin-note">仅显示服务就绪探针与依赖状态；此页面不提供 shell、配置修改或服务重启。</p></section>

    <section class="admin-grid">
      <article class="panel admin-panel"><div class="panel-heading"><div><span class="eyebrow">ACCOUNTS</span><h2>账号管理</h2></div></div><form class="admin-create-form" @submit.prevent="createPC"><label class="field"><span>创建 PC 用户名</span><input v-model="accountForm.username" minlength="3" maxlength="32" required></label><label class="field"><span>初始密码</span><input v-model="accountForm.password" type="password" minlength="12" required></label><button class="button button--primary" :disabled="saving">创建 PC 账号</button><small class="admin-note">移动成员只能经工作空间邀请建立。管理员不在平台中查看或恢复明文密码。</small></form><div class="admin-account-list"><article v-for="account in accounts" :key="account.account_id" class="admin-account"><div><strong>{{ account.username }}</strong><small>{{ roleLabel(account.user_type) }} · {{ statusLabel(account.status) }}</small></div><button v-if="account.user_type !== 'admin'" class="text-button" :disabled="saving" @click="setStatus(account)">{{ account.status === 'active' ? '停用' : '恢复' }}</button></article><p v-if="!busy && !accounts.length" class="empty-state">没有账号记录。</p></div></article>

      <article class="panel admin-panel"><div class="panel-heading"><div><span class="eyebrow">AUDIT</span><h2>操作审计</h2></div></div><p v-if="busy">正在读取审计记录…</p><div v-else-if="!audit.length" class="empty-state">暂无管理操作记录。</div><div v-else class="audit-list"><article v-for="entry in audit" :key="entry.audit_id" class="audit-row"><strong>{{ entry.action }}</strong><small>操作者 {{ entry.actor_account_id }} · 资源 {{ entry.resource_id }}</small><small>{{ timeLabel(entry.created_at) }} · 请求 {{ entry.request_id }}</small></article></div></article>
    </section>
  </main>
</template>

<style scoped>
.admin-shell { min-height: 100vh; padding: 32px clamp(18px, 5vw, 72px); background: var(--canvas); }
.admin-header { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin: 0 auto 26px; max-width: 1440px; }
.admin-header > div:last-child { display: flex; align-items: center; gap: 14px; }
.admin-header h1 { margin: 4px 0 0; font-size: 32px; }
.admin-header p, .admin-note, .health-card small, .admin-account small, .audit-row small { color: var(--muted); }
.admin-panel { padding: 22px; }
.health-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(175px, 1fr)); gap: 10px; }
.health-card { display: grid; gap: 5px; padding: 14px; border: 1px solid var(--line); border-radius: 12px; }
.health-ready { color: var(--green); }.health-unavailable { color: var(--danger); }.health-unknown { color: var(--gold); }
.health-card--dependency { background: #faf8f4; }
.admin-note { display: block; margin: 12px 0 0; font-size: 12px; }
.admin-grid { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, .9fr); gap: 18px; max-width: 1440px; margin: 18px auto; }
.admin-create-form { display: grid; grid-template-columns: 1fr 1fr auto; align-items: end; gap: 12px; padding-bottom: 18px; border-bottom: 1px solid var(--line); }
.admin-create-form .admin-note { grid-column: 1/-1; margin: 0; }
.admin-account-list, .audit-list { max-height: 580px; overflow: auto; }
.admin-account, .audit-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 13px 0; border-bottom: 1px solid var(--line); }
.admin-account > div, .audit-row { display: grid; gap: 3px; }.audit-row { align-items: start; }
@media (max-width: 760px) { .admin-grid { grid-template-columns: 1fr; }.admin-create-form { grid-template-columns: 1fr; }.admin-header { align-items: flex-start; flex-direction: column; } }
</style>
