const CSRF_COOKIE_NAME = 'csrftoken'
const CSRF_HEADER_NAME = 'X-CSRFToken'

export class ApiError extends Error {
  readonly status: number
  readonly fields: Record<string, string>

  constructor(status: number, message: string, fields: Record<string, string> = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.fields = fields
  }
}

type RequestMethod = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'

interface RequestOptions {
  method?: RequestMethod
  body?: unknown
}

type UnauthorizedHandler = () => void

let unauthorizedHandler: UnauthorizedHandler | null = null

export function setUnauthorizedHandler(handler: UnauthorizedHandler | null): void {
  unauthorizedHandler = handler
}

function notifyUnauthorized(): void {
  unauthorizedHandler?.()
}

const FIELD_LABELS: Record<string, string> = {
  username: '用户名',
  nickname: '昵称',
  password: '密码',
  name: '名称',
  owner: '账户归属',
  balance: '当前余额',
  account: '资金账户',
  transfer_account: '转入账户',
  kind: '收支类型',
  amount: '金额',
  category: '分类',
  date: '记账时间',
  note: '备注',
  created_by: '录入人',
  month: '月份',
  avatar: '头像',
  current_password: '当前密码',
  q: '搜索',
}

type MessageTranslation = [RegExp, string | ((match: RegExpMatchArray) => string)]

const SERVER_MESSAGE_TRANSLATIONS: MessageTranslation[] = [
  [/CSRF/i, '安全校验失败，请刷新页面后重试。'],
  [/^Authentication credentials were not provided\.?$/i, '登录状态已失效，请重新登录。'],
  [/^No active account found with the given credentials\.?$/i, '用户名或密码不正确。'],
  [/^Unable to log in with provided credentials\.?$/i, '用户名或密码不正确。'],
  [/^Invalid username or password\.?$/i, '用户名或密码不正确。'],
  [/^A user with that username already exists\.?$/i, '该用户名已被使用。'],
  [/^This password is too common\.?$/i, '密码过于常见，请换一个更安全的密码。'],
  [/^This password is entirely numeric\.?$/i, '密码不能全部为数字。'],
  [/^This password is too short\b/i, '密码太短，至少需要 8 位字符。'],
  [/^The password is too similar to the username\.?$/i, '密码与用户名过于相似。'],
  [
    /^Enter a valid username\.?/i,
    '用户名只能包含字母、数字以及 @ . + - _ 字符。',
  ],
  [
    /^Ensure this field has no more than (\d+) characters\.?$/i,
    (match) => `最多 ${match[1] ?? ''} 个字符。`,
  ],
  [
    /^Ensure this field has at least (\d+) characters\.?$/i,
    (match) => `至少需要 ${match[1] ?? ''} 个字符。`,
  ],
  [/^This field is required\.?$/i, '此项为必填项。'],
  [/^This field may not be blank\.?$/i, '此项不能为空。'],
  [/^This field may not be null\.?$/i, '此项不能为空。'],
  [/^Invalid credentials\.?$/i, '用户名或密码不正确。'],
  [/^Not found\.?$/i, '请求的内容不存在。'],
  [/^Method "[^"]+" not allowed\.?$/i, '当前操作不被允许。'],
]

function translateServerMessage(value: string): string {
  const text = value.trim()
  for (const [pattern, replacement] of SERVER_MESSAGE_TRANSLATIONS) {
    const match = text.match(pattern)
    if (match) {
      return typeof replacement === 'string' ? replacement : replacement(match)
    }
  }
  if (/^</.test(text) || text.length > 400) {
    return '服务器返回了异常响应，请稍后重试。'
  }
  return text
}

function defaultMessage(status: number): string {
  if (status === 400) return '提交的信息有误，请检查后重试。'
  if (status === 401) return '登录状态已失效，请重新登录。'
  if (status === 403) return '没有权限执行此操作。'
  if (status === 404) return '请求的内容不存在。'
  if (status === 409) return '数据发生冲突，请刷新后重试。'
  if (status === 429) return '请求过于频繁，请稍后再试。'
  if (status >= 500) return '服务器异常，请稍后重试。'
  return '请求失败，请稍后重试。'
}

function buildApiError(status: number, payload: unknown): ApiError {
  const fieldMessages: Record<string, string[]> = {}
  const generalMessages: string[] = []

  const collect = (value: unknown, field: string | null): void => {
    if (value === null || value === undefined) return
    if (Array.isArray(value)) {
      for (const entry of value) collect(entry, field)
      return
    }
    if (typeof value === 'object') {
      for (const [key, entry] of Object.entries(value as Record<string, unknown>)) {
        const nextField =
          key === 'detail' || key === 'non_field_errors' || key === 'message'
            ? null
            : key
        collect(entry, nextField)
      }
      return
    }
    const text = String(value).trim()
    if (!text) return
    const translated = translateServerMessage(text)
    if (field) {
      const label = FIELD_LABELS[field] ?? field
      ;(fieldMessages[field] ??= []).push(`${label}：${translated}`)
    } else {
      generalMessages.push(translated)
    }
  }

  collect(payload, null)

  const fields: Record<string, string> = {}
  for (const [key, messages] of Object.entries(fieldMessages)) {
    fields[key] = messages.join('；')
  }

  const messages = [...new Set([...generalMessages, ...Object.values(fields)])]
  return new ApiError(status, messages.length > 0 ? messages.join('；') : defaultMessage(status), fields)
}

export function getErrorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error && error.message) return error.message
  return '发生未知错误，请稍后重试。'
}

function readCookie(name: string): string | null {
  const prefix = `${encodeURIComponent(name)}=`
  for (const part of document.cookie.split(';')) {
    const cookie = part.trim()
    if (cookie.startsWith(prefix)) {
      return decodeURIComponent(cookie.slice(prefix.length))
    }
  }
  return null
}

async function fetchFreshCsrfCookie(): Promise<void> {
  let response: Response
  try {
    response = await fetch('/api/auth/me/', {
      method: 'GET',
      credentials: 'same-origin',
      headers: { Accept: 'application/json' },
    })
  } catch {
    throw new ApiError(0, '网络连接失败，请检查网络或确认后端服务已启动。')
  }
  try {
    await response.text()
  } catch {
    // The cookie is set by the response headers even if the body cannot be read.
  }
  if (!response.ok) {
    if (response.status === 401) notifyUnauthorized()
    throw new ApiError(response.status, '无法初始化安全会话，请刷新页面后重试。')
  }
}

let csrfRequest: Promise<void> | null = null

async function ensureCsrfCookie(): Promise<void> {
  if (readCookie(CSRF_COOKIE_NAME)) return
  if (!csrfRequest) {
    csrfRequest = fetchFreshCsrfCookie().finally(() => {
      csrfRequest = null
    })
  }
  await csrfRequest
}

async function readResponseBody(response: Response): Promise<unknown> {
  if (response.status === 204 || response.status === 205) return null
  let text: string
  try {
    text = await response.text()
  } catch {
    return null
  }
  if (!text) return null
  const contentType = response.headers.get('content-type') ?? ''
  if (contentType.includes('application/json')) {
    try {
      return JSON.parse(text)
    } catch {
      return text
    }
  }
  const trimmed = text.trim()
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      return JSON.parse(trimmed)
    } catch {
      return text
    }
  }
  return text
}

async function performRequest(
  url: string,
  method: RequestMethod,
  body: unknown,
): Promise<Response> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  let requestBody: BodyInit | undefined

  if (method !== 'GET') {
    const token = readCookie(CSRF_COOKIE_NAME)
    if (token) headers[CSRF_HEADER_NAME] = token
    if (body instanceof Blob) {
      requestBody = body
      if (body.type) headers['Content-Type'] = body.type
    } else {
      headers['Content-Type'] = 'application/json'
      if (body !== undefined) requestBody = JSON.stringify(body)
    }
  }

  try {
    return await fetch(url, {
      method,
      credentials: 'same-origin',
      headers,
      body: requestBody,
    })
  } catch {
    throw new ApiError(0, '网络连接失败，请检查网络或确认后端服务已启动。')
  }
}

export async function request<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const isMutation = method !== 'GET'

  if (isMutation) await ensureCsrfCookie()

  const response = await performRequest(url, method, options.body)
  const payload = await readResponseBody(response)

  if (!response.ok) {
    if (response.status === 401) notifyUnauthorized()
    throw buildApiError(response.status, payload)
  }

  return payload as T
}
