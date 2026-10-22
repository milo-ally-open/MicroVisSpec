const form = document.querySelector('#event-form')
const sendButton = document.querySelector('#send')
const refreshButton = document.querySelector('#refresh')
const error = document.querySelector('#error')
let csrf = '', jobId = '', pending = null, polling = false

function localTime() {
  const date = new Date()
  date.setMinutes(date.getMinutes() - date.getTimezoneOffset())
  form.elements.occurred_at.value = date.toISOString().slice(0, 16)
}

async function request(path, options = {}) {
  const response = await fetch(path, { cache: 'no-store', ...options })
  const result = await response.json()
  if (!response.ok) throw new Error(result.error ?? `请求失败：${response.status}`)
  return result
}

function render(job) {
  const steps = []
  const add = (text, state = '') => steps.push({ text, state })
  add('测试任务已建立', 'passed')
  if (job.stage === 'queued') add('正在登录、准备模拟设备与规则…')
  if (job.mqtt_ack) add(`设备事件确认：${job.mqtt_ack}`, ['accepted', 'duplicate'].includes(job.mqtt_ack) ? 'passed' : 'warning')
  else if (job.stage === 'rules_applied') add('规则已应用，等待事件确认…')
  if (job.event_id) {
    add('事件已入库，详情可查询', 'passed')
    if (['upload_failed', 'too_large'].includes(job.evidence)) add('模拟证据上传未完成；事件详情和推送状态仍可查询', 'warning')
    const delivery = job.delivery
    if (delivery?.state === 'observed') {
      add(`目标账号的有效订阅数：${delivery.account_active_subscriptions}`, delivery.account_active_subscriptions ? '' : 'warning')
      add(delivery.inbox_received ? 'push-service 已接收事件' : '等待 push-service 接收', delivery.inbox_received ? 'passed' : '')
      if (!delivery.deliveries.length) add('尚未创建投递任务；请核查有效订阅', 'warning')
      for (const [index, item] of delivery.deliveries.entries()) {
        const labels = { accepted: '推送平台已受理', pending: '排队或重试中', expired: '已过期', permanent_failure: '永久失败', cancelled: '已取消' }
        add(`投递 ${index + 1}：${labels[item.status] ?? item.status}，尝试 ${item.attempts} 次${item.last_class ? `（${item.last_class}）` : ''}`, item.status === 'accepted' ? 'passed' : item.status === 'pending' ? '' : 'warning')
      }
    } else add('未观察到实际投递状态，请在接收端核实', 'warning')
    add('浏览器/手机实际弹出与点击：待接收端确认')
  }
  if (job.error) add(job.error, 'warning')
  const list = document.querySelector('#stages')
  list.replaceChildren(...steps.map(({ text, state }) => {
    const li = document.createElement('li'); li.textContent = text; li.className = state; return li
  }))
  const linkBox = document.querySelector('#event-link')
  linkBox.replaceChildren()
  if (job.detail_url) {
    const link = document.createElement('a')
    link.href = job.detail_url; link.target = '_blank'; link.rel = 'noopener noreferrer'; link.textContent = '打开对应事件详情 ↗'
    linkBox.append(link)
  }
  document.querySelector('#raw').textContent = JSON.stringify(job, null, 2)
}

async function refresh() {
  if (!jobId) return null
  const job = await request(`/api/jobs/${encodeURIComponent(jobId)}`)
  render(job)
  return job
}

async function poll() {
  if (polling) return
  polling = true
  try {
    for (let index = 0; index < 90; index++) {
      const job = await refresh()
      if (['failed', 'interrupted'].includes(job.stage)) break
      const items = job.delivery?.deliveries
      if (job.stage === 'event_created' && items?.length && items.every(item => item.status !== 'pending')) break
      if (job.stage === 'event_created' && ((job.delivery?.inbox_received && job.delivery?.account_active_subscriptions === 0) || job.delivery?.state !== 'observed')) break
      await new Promise(resolve => setTimeout(resolve, 1500))
    }
  } catch (cause) { error.textContent = cause.message }
  finally { polling = false; sendButton.disabled = true; refreshButton.disabled = false }
}

form.addEventListener('submit', async event => {
  event.preventDefault(); error.textContent = ''; sendButton.disabled = true
  try {
    if (!pending) {
      const { time_mode, ...value } = Object.fromEntries(new FormData(form))
      pending = { ...value, occurred_at: time_mode === 'now' ? 'now' : new Date(value.occurred_at).toISOString(), request_id: crypto.randomUUID() }
    }
    jobId = pending.request_id
    location.hash = jobId
    const job = await request('/api/jobs', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify(pending) })
    render(job); refreshButton.disabled = false; void poll()
  } catch (cause) { error.textContent = cause.message; sendButton.disabled = false }
})

document.querySelector('#new-event').addEventListener('click', () => {
  if (polling) { error.textContent = '当前任务仍在查询，请等待结果后准备新事件。'; return }
  pending = null; jobId = ''; location.hash = ''; localTime(); sendButton.disabled = !csrf; refreshButton.disabled = true
  document.querySelector('#stages').replaceChildren(Object.assign(document.createElement('li'), { textContent: '已准备新事件，等待提交' }))
  document.querySelector('#event-link').replaceChildren(); document.querySelector('#raw').textContent = '尚未创建事件'; error.textContent = ''
})
refreshButton.addEventListener('click', () => { error.textContent = ''; void refresh().catch(cause => { error.textContent = cause.message }) })
form.elements.time_mode.addEventListener('change', () => {
  form.elements.occurred_at.disabled = form.elements.time_mode.value === 'now'
  form.elements.occurred_at.required = form.elements.time_mode.value === 'custom'
})

localTime()
try {
  const context = await request('/api/context')
  csrf = context.csrf_token
  const profiles = document.querySelector('#profile')
  profiles.replaceChildren(...context.profiles.map(profile => Object.assign(document.createElement('option'), { value: profile.id, textContent: profile.label })))
  profiles.disabled = !context.profiles.length; sendButton.disabled = !context.profiles.length
  const previous = location.hash.slice(1)
  if (/^[a-f0-9-]{36}$/.test(previous)) { jobId = previous; sendButton.disabled = true; refreshButton.disabled = false; void poll() }
} catch (cause) { error.textContent = '无法加载本机配置：' + cause.message }
