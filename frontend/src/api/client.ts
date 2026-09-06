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
    super(payload?.error.message || `API request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.code = payload?.error.code || 'request_failed'
    this.field = payload?.error.field
    this.requestId = payload?.request_id
  }
}

function buildUrl(path: string, query?: Record<string, QueryValue>) {
  const normalizedBase = API_BASE_URL.endsWith('/') ? API_BASE_URL.slice(0, -1) : API_BASE_URL
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  const url = new URL(`${normalizedBase}${normalizedPath}`, window.location.origin)

  Object.entries(query || {}).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (Array.isArray(value)) value.forEach((item) => url.searchParams.append(key, String(item)))
    else url.searchParams.set(key, String(value))
  })

  return url.toString()
}

export async function apiRequest<T>(path: string, options: RequestOptions = {}): Promise<ApiEnvelope<T>> {
  const { body, query, headers, ...requestInit } = options
  const response = await fetch(buildUrl(path, query), {
    ...requestInit,
    credentials: 'include',
    headers: {
      Accept: 'application/json',
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (!response.ok) {
    const payload = await response.json().catch(() => undefined) as ApiErrorBody | undefined
    throw new ApiError(response.status, payload)
  }

  if (response.status === 204) {
    return {
      data: undefined as T,
      request_id: response.headers.get('x-request-id') || '',
    }
  }

  return response.json() as Promise<ApiEnvelope<T>>
}
