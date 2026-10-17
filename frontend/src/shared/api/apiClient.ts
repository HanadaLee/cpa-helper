import { localizedApiErrorMessage } from '@/shared/i18n'

interface ApiErrorPayload {
  detail?: {
    code?: string
    message?: string
  }
}

export class ApiRequestError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiRequestError'
    this.status = status
  }
}

export function isUnauthorizedApiError(error: unknown): boolean {
  return error instanceof ApiRequestError && error.status === 401
}

function isApiErrorPayload(value: unknown): value is ApiErrorPayload {
  if (!value || typeof value !== 'object') {
    return false
  }
  const detail = (value as Record<string, unknown>).detail
  return typeof detail === 'object' && detail !== null
}

async function parseError(response: Response): Promise<string> {
  try {
    const data: unknown = await response.json()
    if (isApiErrorPayload(data)) {
      return localizedApiErrorMessage(data.detail?.code, data.detail?.message)
    }
  } catch {
    return localizedApiErrorMessage(null, null)
  }
  return localizedApiErrorMessage(null, null)
}

async function requestResponse(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers)
  if (!(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(`/api${path}`, {
    credentials: 'include',
    ...init,
    headers,
  })

  if (!response.ok) {
    throw new ApiRequestError(response.status, await parseError(response))
  }

  return response
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await requestResponse(path, init)

  if (response.status === 204) {
    return undefined as T
  }

  return (await response.json()) as T
}

function toQuery(params: Record<string, string | number | boolean | undefined>): string {
  const query = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      query.set(key, String(value))
    }
  })
  const text = query.toString()
  return text ? `?${text}` : ''
}

export const apiClient = {
  async getBlob(path: string, params: Record<string, string | number | boolean | undefined> = {}) {
    const response = await requestResponse(`${path}${toQuery(params)}`)
    return response.blob()
  },
  get<T>(path: string, params: Record<string, string | number | boolean | undefined> = {}) {
    return request<T>(`${path}${toQuery(params)}`)
  },
  post<T>(path: string, body?: unknown) {
    const init: RequestInit = { method: 'POST' }
    if (body !== undefined) {
      init.body = JSON.stringify(body)
    }
    return request<T>(path, init)
  },
  put<T>(path: string, body?: unknown) {
    const init: RequestInit = { method: 'PUT' }
    if (body !== undefined) {
      init.body = JSON.stringify(body)
    }
    return request<T>(path, init)
  },
  patch<T>(path: string, body?: unknown) {
    const init: RequestInit = { method: 'PATCH' }
    if (body !== undefined) {
      init.body = JSON.stringify(body)
    }
    return request<T>(path, init)
  },
  postForm<T>(path: string, body: FormData) {
    return request<T>(path, { method: 'POST', body })
  },
  delete(path: string) {
    return request<void>(path, { method: 'DELETE' })
  },
}
