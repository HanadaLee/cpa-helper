import { apiClient } from '@/shared/api/apiClient'
import { localize } from '@/shared/i18n'
import type {
  AuthUser,
  ChangeCredentialsPayload,
  FirstAdminSetupPayload,
  LoginPayload,
  SetupState,
} from '@/shared/types/api'

export function isAuthUser(value: unknown): value is AuthUser {
  if (!value || typeof value !== 'object') {
    return false
  }
  const record = value as Record<string, unknown>
  return (
    typeof record.id === 'number' &&
    typeof record.username === 'string' &&
    typeof record.nickname === 'string' &&
    typeof record.email === 'string' &&
    typeof record.avatar === 'string' &&
    typeof record.created_at === 'string' &&
    typeof record.is_admin === 'boolean' &&
    typeof record.cas_bound === 'boolean' &&
    typeof record.can_change_password === 'boolean' &&
    typeof record.must_change_password === 'boolean' &&
    typeof record.can_view_account_status === 'boolean' &&
    typeof record.can_view_usage_history === 'boolean'
  )
}

function toAuthUser(value: unknown): AuthUser {
  if (!isAuthUser(value)) {
    throw new Error(localize('登录状态缺少角色信息，请重启后端服务后重新登录', 'Your session is missing role information. Restart the backend and sign in again.'))
  }
  return value
}

export function login(payload: LoginPayload): Promise<AuthUser> {
  return apiClient.post<unknown>('/auth/login', payload).then(toAuthUser)
}

export function getMe(): Promise<AuthUser> {
  return apiClient.get<unknown>('/auth/me').then(toAuthUser)
}

export function getSetupState(): Promise<SetupState> {
  return apiClient.get<SetupState>('/auth/setup')
}

export function setupFirstAdmin(payload: FirstAdminSetupPayload): Promise<AuthUser> {
  return apiClient.post<unknown>('/auth/setup', payload).then(toAuthUser)
}

export function changeCredentials(payload: ChangeCredentialsPayload): Promise<AuthUser> {
  return apiClient.post<unknown>('/auth/change-credentials', payload).then(toAuthUser)
}

export function logout(): Promise<{ ok: boolean, logout_url?: string, cas_logout_url?: string }> {
  return apiClient.post<{ ok: boolean, logout_url?: string, cas_logout_url?: string }>('/auth/logout')
}
