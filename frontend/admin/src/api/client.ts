import type { ApiEnvelope, ApiErrorBody } from './contracts'

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1'

type QueryValue = string | number | boolean | null | undefined | readonly (string | number)[]

type RequestOptions = Omit<RequestInit, 'body'> & {
  body?: unknown
  query?: Record<string, QueryValue>
}

export class ApiError extends Error {
  status: number
  code: string
  field?: string
  requestId?: string

  constructor(status: number, payload?: ApiErrorBody) {
    super(payload?.error.message || `请求失败（HTTP ${status}）`)
    this.name = 'ApiError'
    this.status = status
    this.code = payload?.error.code || 'request_failed'
    this.field = payload?.error.field
    this.requestId = payload?.request_id
  }
}

function buildUrl(path: string, query?: Record<string, QueryValue>) {
  const base = API_BASE_URL.endsWith('/') ? API_BASE_URL.slice(0, -1) : API_BASE_URL
  const url = new URL(
    `${base}${path.startsWith('/') ? path : `/${path}`}`,
    window.location.origin,
  )
  Object.entries(query ?? {}).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (Array.isArray(value)) value.forEach((item) => url.searchParams.append(key, String(item)))
    else url.searchParams.set(key, String(value))
  })
  return url.toString()
}

/**
 * 管理端请求封装。与 web 端共用同一套信封与错误码约定
 * （唯一真相源是仓库级的 openapi.yaml）。
 */
export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, query, headers, ...init } = options
  const response = await fetch(buildUrl(path, query), {
    ...init,
    credentials: 'include',
    headers: {
      Accept: 'application/json',
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!response.ok) {
    const payload = (await response.json().catch(() => undefined)) as ApiErrorBody | undefined
    throw new ApiError(response.status, payload)
  }

  if (response.status === 204) return undefined as T

  const envelope = (await response.json()) as ApiEnvelope<T>
  return envelope.data
}
