export type ApiProblem = {
  error?: { code?: string; message?: string }
  request_id?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message)
    this.status = status
    this.code = code
    this.requestId = requestId
    this.name = 'ApiError'
  }
}

type RequestOptions = RequestInit & { csrf?: boolean }

let csrfToken = ''

export function setCsrfToken(token: string) {
  csrfToken = token
}

export function clearCsrfToken() {
  csrfToken = ''
}

export function csrfHeaders(headers?: HeadersInit) {
  const result = new Headers(headers)
  if (csrfToken) result.set('X-CSRF-Token', csrfToken)
  return result
}

function isWrite(method: string) {
  return !['GET', 'HEAD', 'OPTIONS'].includes(method.toUpperCase())
}

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers = new Headers(options.headers)
  const hasBody = options.body !== undefined && options.body !== null
  if (hasBody && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  if (isWrite(method) && options.csrf !== false && csrfToken) {
    headers.set('X-CSRF-Token', csrfToken)
  }

  const response = await fetch(path, {
    ...options,
    method,
    headers,
    credentials: 'include',
    cache: 'no-store',
    redirect: 'error',
  })
  if (response.status === 401 && !path.endsWith('/auth/login') && !path.endsWith('/auth/register')) {
    window.dispatchEvent(new Event('auth:expired'))
  }
  if (!response.ok) {
    const problem = (await response.json().catch(() => ({}))) as ApiProblem
    const code = problem.error?.code ?? 'request_failed'
    const statusMessage: Record<number, string> = {
      401: path.endsWith('/auth/login') ? '用户名或密码不正确，请重新输入。' : '登录状态已失效，请重新登录。',
      403: '当前账号无权执行此操作。',
      404: '内容不存在或已不可访问。',
      409: '内容已发生变化，请刷新后重试。',
      410: '截图已过期。',
      422: '提交内容未通过校验。',
      429: '操作过于频繁，请稍后再试。',
      503: '服务暂不可用，请检查连接后重试。',
    }
    const message = code === 'capacity_full'
      ? '观看容量已满，请关闭其他监控画面后重试。'
      : code === 'device_not_provisioned'
        ? '此设备尚未完成首次接入，请在设备详情页使用网页接入向导。'
      : code === 'ssh_target_not_allowed'
        ? '设备地址不在平台允许的园区网段内，请联系平台管理员检查设备 IP 配置。'
      : code === 'ssh_host_key_unavailable'
        ? '暂时无法读取设备 SSH 指纹，请确认设备已开机并开启 SSH。'
      : code === 'ssh_host_key_changed'
        ? '设备 SSH 指纹与首次登记时不同，已停止连接；请联系管理员核对设备。'
      : code === 'device_already_provisioned'
        ? '设备已经完成首次接入，请直接选择使用期限并网页激活。'
      : code === 'provisioning_unavailable'
        ? '首次接入服务尚未就绪，请联系平台管理员检查部署配置。'
      : code === 'provisioning_in_progress'
        ? '设备正在进行首次接入，请等待当前进度完成。'
      : code === 'activation_sync_pending'
        ? '设备正在同步上一次激活，请稍候片刻再提交。'
        : statusMessage[response.status] ?? problem.error?.message ?? '请求失败，请稍后重试。'
    throw new ApiError(
      response.status,
      code,
      message,
      problem.request_id,
    )
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

export async function apiBlob(path: string): Promise<Blob> {
  const response = await fetch(path, { credentials: 'include', cache: 'no-store', redirect: 'error' })
  if (response.status === 401) window.dispatchEvent(new Event('auth:expired'))
  if (!response.ok) {
    const problem = (await response.json().catch(() => ({}))) as ApiProblem
    const message = response.status === 410 ? '截图已过期。' : response.status === 404 ? '截图尚未上传或不可访问。' : '截图暂时无法读取。'
    throw new ApiError(response.status, problem.error?.code ?? 'evidence_unavailable', message, problem.request_id)
  }
  return response.blob()
}

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试。'
}
