<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { authApi, type DeviceGrant, type WorkspaceInfo, type WorkspaceInvitation, type WorkspaceMember } from '../api/auth'
import { deviceApi, type Device } from '../api/devices'
import { errorMessage } from '../api/client'

const capabilities = [
  { key: 'device.read', label: '查看设备' },
  { key: 'event.read', label: '查看事件和截图' },
  { key: 'event.handle', label: '处置事件' },
  { key: 'stream.view', label: '查看视频' },
  { key: 'alert.receive', label: '接收通知' },
] as const
const devices = ref<Device[]>([])
const workspace = ref<WorkspaceInfo | null>(null)
const workspaceName = ref('')
const members = ref<WorkspaceMember[]>([])
const invitations = ref<WorkspaceInvitation[]>([])
const busy = ref(true)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const editing = ref<WorkspaceMember | null>(null)
const grantState = ref<Record<string, Set<string>>>({})
const inviteState = ref<Record<string, Set<string>>>({})
const invitationUrl = ref('')

const activeInvitations = computed(() => invitations.value.filter((item) => !item.used_at && !item.revoked_at && Date.parse(item.expires_at) > Date.now()))

function grantsFromState(state: Record<string, Set<string>>): DeviceGrant[] {
  return Object.entries(state).filter(([, items]) => items.size > 0)
    .map(([device_id, items]) => ({ device_id, capabilities: capabilities.filter((capability) => items.has(capability.key)).map((item) => item.key) }))
}

function setGrant(state: Record<string, Set<string>>, deviceId: string, capability: string, checked: boolean) {
  const current = new Set(state[deviceId] ?? [])
  if (checked) current.add(capability)
  else current.delete(capability)
  state[deviceId] = current
}

function isGranted(state: Record<string, Set<string>>, deviceId: string, capability: string) {
  return state[deviceId]?.has(capability) ?? false
}

async function load() {
  busy.value = true
  error.value = ''
  try {
    const [workspaceInfo, workspaceDevices, memberPage, invitationPage] = await Promise.all([authApi.workspace(), deviceApi.listAll(), authApi.members(), authApi.invitations()])
    workspace.value = workspaceInfo
    workspaceName.value = workspaceInfo.name
    devices.value = workspaceDevices
    members.value = memberPage.items
    invitations.value = invitationPage.items
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    busy.value = false
  }
}

async function saveWorkspaceName() {
  saving.value = true
  error.value = ''
  try {
    workspace.value = await authApi.updateWorkspace(workspaceName.value)
    workspaceName.value = workspace.value.name
    notice.value = '工作空间名称已更新。'
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    saving.value = false
  }
}

function editMember(member: WorkspaceMember) {
  editing.value = member
  grantState.value = Object.fromEntries(member.device_grants.map((grant) => [grant.device_id, new Set(grant.capabilities)]))
  notice.value = ''
}

async function saveMemberGrants() {
  if (!editing.value) return
  saving.value = true
  error.value = ''
  try {
    await authApi.replaceMemberGrants(editing.value.member_id, grantsFromState(grantState.value))
    notice.value = `已更新 ${editing.value.username} 的设备权限。`
    editing.value = null
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    saving.value = false
  }
}

async function revokeMember(member: WorkspaceMember) {
  if (!window.confirm(`撤销 ${member.username} 的工作空间访问？`)) return
  try {
    await authApi.revokeMember(member.member_id)
    notice.value = `已撤销 ${member.username} 的访问权限。`
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

async function createInvite() {
  saving.value = true
  error.value = ''
  invitationUrl.value = ''
  try {
    const result = await authApi.createInvitation(grantsFromState(inviteState.value))
    invitationUrl.value = `${window.location.origin}/invite/redeem?token=${encodeURIComponent(result.invite_token)}`
    notice.value = '邀请已创建。请复制链接并通过可信渠道发送；链接 24 小时有效且只能使用一次。'
    inviteState.value = {}
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    saving.value = false
  }
}

async function copyInvite() {
  if (!invitationUrl.value) return
  await navigator.clipboard.writeText(invitationUrl.value)
  notice.value = '邀请链接已复制。'
}

async function revokeInvite(invitation: WorkspaceInvitation) {
  try {
    await authApi.revokeInvitation(invitation.invitation_id)
    notice.value = '邀请已撤销。'
    await load()
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

onMounted(load)
</script>

<template>
  <section class="page-heading"><div><span class="eyebrow">WORKSPACE ACCESS</span><h1>成员与设备授权</h1><p>按设备选择成员可以查看的信息和可执行的操作。视频权限覆盖所选设备下的摄像头。</p></div></section>
  <div v-if="error" class="notice notice--error" role="alert">{{ error }}</div>
  <div v-if="notice" class="notice notice--success" role="status">{{ notice }}</div>

  <section class="panel workspace-panel workspace-profile">
    <div class="panel-heading"><div><span class="eyebrow">PROFILE</span><h2>工作空间资料</h2></div></div>
    <form class="workspace-profile__form" @submit.prevent="saveWorkspaceName">
      <label for="workspace-name">名称</label>
      <input id="workspace-name" v-model="workspaceName" maxlength="100" required autocomplete="organization" :disabled="saving || workspace?.role !== 'owner'">
      <button class="button button--primary" :disabled="saving || !workspace || workspace.role !== 'owner'">{{ saving ? '正在保存…' : '保存名称' }}</button>
    </form>
  </section>

  <section class="panel workspace-panel">
    <div class="panel-heading"><div><span class="eyebrow">MEMBERS</span><h2>工作空间成员</h2></div><button class="button button--secondary" @click="load" :disabled="busy">刷新</button></div>
    <p v-if="busy">正在加载成员…</p>
    <div v-else-if="members.length === 0" class="empty-state">还没有移动成员。创建邀请后，成员可使用邀请链接建立账号。</div>
    <div v-else class="workspace-member-list">
      <article v-for="member in members" :key="member.member_id" class="workspace-member">
        <div><strong>{{ member.username }}</strong><small>{{ member.status === 'active' ? '有效成员' : '已撤销' }} · {{ member.device_grants.length }} 台设备已配置</small></div>
        <div class="workspace-member__actions"><button class="text-button" :disabled="member.status !== 'active'" @click="editMember(member)">编辑权限</button><button class="text-button text-button--danger" :disabled="member.status !== 'active'" @click="revokeMember(member)">撤销访问</button></div>
      </article>
    </div>
  </section>

  <section v-if="editing" class="panel workspace-panel">
    <div class="panel-heading"><div><span class="eyebrow">EDIT ACCESS</span><h2>{{ editing.username }} 的权限</h2></div><button class="text-button" @click="editing = null">取消</button></div>
    <p class="muted-copy">未勾选的能力立即从该成员权限中移除。</p>
    <div v-for="device in devices" :key="device.device_id" class="grant-device">
      <strong>{{ device.name }}</strong><small>{{ device.location }}</small>
      <label v-for="capability in capabilities" :key="capability.key" class="grant-option"><input type="checkbox" :checked="isGranted(grantState, device.device_id, capability.key)" @change="setGrant(grantState, device.device_id, capability.key, ($event.target as HTMLInputElement).checked)"><span>{{ capability.label }}</span><small v-if="capability.key === 'stream.view'">该设备的所有摄像头</small></label>
    </div>
    <button class="button button--primary" :disabled="saving" @click="saveMemberGrants">{{ saving ? '正在保存…' : '保存权限' }}</button>
  </section>

  <section class="panel workspace-panel">
    <div class="panel-heading"><div><span class="eyebrow">INVITATIONS</span><h2>创建移动成员邀请</h2></div></div>
    <p class="muted-copy">选择邀请生效的设备和能力。邀请有效期为 24 小时，只能兑换一次。</p>
    <div v-if="!devices.length" class="empty-state">请先登记设备，再创建成员邀请。</div>
    <div v-for="device in devices" :key="`invite-${device.device_id}`" class="grant-device">
      <strong>{{ device.name }}</strong><small>{{ device.location }}</small>
      <label v-for="capability in capabilities" :key="capability.key" class="grant-option"><input type="checkbox" :checked="isGranted(inviteState, device.device_id, capability.key)" @change="setGrant(inviteState, device.device_id, capability.key, ($event.target as HTMLInputElement).checked)"><span>{{ capability.label }}</span><small v-if="capability.key === 'stream.view'">该设备的所有摄像头</small></label>
    </div>
    <button class="button button--primary" :disabled="saving || grantsFromState(inviteState).length === 0" @click="createInvite">{{ saving ? '正在创建…' : '创建邀请链接' }}</button>
    <div v-if="invitationUrl" class="invite-link-box"><input :value="invitationUrl" readonly aria-label="新建邀请链接"><button class="button button--secondary" @click="copyInvite">复制链接</button></div>
  </section>

  <section class="panel workspace-panel">
    <div class="panel-heading"><div><span class="eyebrow">PENDING</span><h2>待处理邀请</h2></div></div>
    <p v-if="activeInvitations.length === 0" class="empty-state">没有待处理邀请。</p>
    <article v-for="invitation in activeInvitations" :key="invitation.invitation_id" class="workspace-member"><div><strong>待兑换邀请</strong><small>有效至 {{ new Date(invitation.expires_at).toLocaleString() }}</small></div><button class="text-button text-button--danger" @click="revokeInvite(invitation)">撤销邀请</button></article>
  </section>
</template>

<style scoped>
.workspace-panel { margin-top: 20px; padding: 24px; }
.workspace-profile__form { display: flex; align-items: center; gap: 12px; }
.workspace-profile__form input { min-width: 0; flex: 1; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; }
.muted-copy { color: var(--muted); margin: -6px 0 18px; }
.workspace-member-list { display: grid; gap: 10px; }
.workspace-member { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 14px 0; border-top: 1px solid var(--line); }
.workspace-member > div:first-child { display: grid; gap: 3px; }
.workspace-member small, .grant-device > small { display: block; color: var(--muted); }
.workspace-member__actions { display: flex; gap: 16px; }
.text-button--danger { color: var(--danger); }
.grant-device { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px 16px; padding: 16px 0; border-top: 1px solid var(--line); }
.grant-device > strong, .grant-device > small { grid-column: 1 / -1; }
.grant-option { display: flex; align-items: center; gap: 8px; color: var(--ink); }
.grant-option small { color: var(--muted); font-size: 11px; }
.invite-link-box { display: flex; gap: 10px; margin-top: 16px; }
.invite-link-box input { min-width: 0; flex: 1; padding: 10px 12px; border: 1px solid var(--line); border-radius: 10px; }
@media (max-width: 650px) { .workspace-member { align-items: flex-start; flex-direction: column; } .invite-link-box, .workspace-profile__form { align-items: stretch; flex-direction: column; } }
</style>
