import { apiClient } from '@/shared/api/apiClient'
import type {
  CodexKeeperAuthFileDetail,
  CodexKeeperAuthFileFields,
  CodexKeeperAuthFileModelsResponse,
  CodexKeeperAuthFileUploadResponse,
  CodexKeeperBulkDeletePayload,
  CodexKeeperBulkDeleteResponse,
  CodexKeeperCronPreviewPayload,
  CodexKeeperCronPreviewResponse,
  CodexKeeperAccountsResponse,
  CodexKeeperOAuthStartResponse,
  CodexKeeperOAuthStatusResponse,
  CodexKeeperRefreshPayload,
  CodexKeeperResetCredits,
  CodexKeeperSettings,
  CodexKeeperSettingsUpdatePayload,
  CodexKeeperStatus,
  CredentialOAuthProvider,
} from '@/shared/types/api'

export function uploadCodexKeeperAuthFiles(files: File[]): Promise<CodexKeeperAuthFileUploadResponse> {
  const form = new FormData()
  files.forEach((file) => form.append('file', file, file.name))
  return apiClient.postForm<CodexKeeperAuthFileUploadResponse>('/codex-keeper/auth-files', form)
}

export function startCodexKeeperOAuth(
  provider: CredentialOAuthProvider,
  projectId?: string,
  target?: { name: string; auth_index?: string | null },
): Promise<CodexKeeperOAuthStartResponse> {
  return apiClient.post<CodexKeeperOAuthStartResponse>('/codex-keeper/oauth/start', {
    provider,
    ...target,
    project_id: projectId?.trim() || undefined,
  })
}

export function getCodexKeeperOAuthStatus(state: string): Promise<CodexKeeperOAuthStatusResponse> {
  return apiClient.get<CodexKeeperOAuthStatusResponse>('/codex-keeper/oauth/status', { state })
}

export function cancelCodexKeeperOAuth(state: string): Promise<void> {
  return apiClient.delete(`/codex-keeper/oauth/session?state=${encodeURIComponent(state)}`)
}

export function coreCredentialAction(name: string, action: 'core-refresh' | 'core-verify'): Promise<void> {
  return apiClient.post<void>(`/codex-keeper/accounts/${encodeURIComponent(name)}/${action}`)
}

export function bindCredentialSIWC(name: string): Promise<CodexKeeperOAuthStartResponse> {
  return apiClient.post<CodexKeeperOAuthStartResponse>(`/codex-keeper/accounts/${encodeURIComponent(name)}/siwc-bind`)
}

export function submitCodexKeeperOAuthCallback(
  provider: CredentialOAuthProvider,
  redirectUrl: string,
): Promise<void> {
  return apiClient.post<void>('/codex-keeper/oauth/callback', { provider, redirect_url: redirectUrl })
}

export function getCodexKeeperAuthFile(name: string): Promise<CodexKeeperAuthFileDetail> {
  return apiClient.get<CodexKeeperAuthFileDetail>(
    `/codex-keeper/auth-files/${encodeURIComponent(name)}`,
  )
}

export function getCodexKeeperAuthFileModels(name: string): Promise<CodexKeeperAuthFileModelsResponse> {
  return apiClient.get<CodexKeeperAuthFileModelsResponse>(
    `/codex-keeper/auth-files/${encodeURIComponent(name)}/models`,
  )
}

export function updateCodexKeeperAuthFile(
  name: string,
  fields: CodexKeeperAuthFileFields,
): Promise<void> {
  return apiClient.patch<void>(
    `/codex-keeper/auth-files/${encodeURIComponent(name)}`,
    fields,
  )
}

export function getCodexKeeperSettings(): Promise<CodexKeeperSettings> {
  return apiClient.get<CodexKeeperSettings>('/codex-keeper/settings')
}

export function updateCodexKeeperSettings(
  payload: CodexKeeperSettingsUpdatePayload,
): Promise<CodexKeeperSettings> {
  return apiClient.put<CodexKeeperSettings>('/codex-keeper/settings', payload)
}

export function previewCodexKeeperSchedule(
  payload: CodexKeeperCronPreviewPayload,
): Promise<CodexKeeperCronPreviewResponse> {
  return apiClient.post<CodexKeeperCronPreviewResponse>('/codex-keeper/schedule/preview', payload)
}

export function getCodexKeeperStatus(): Promise<CodexKeeperStatus> {
  return apiClient.get<CodexKeeperStatus>('/codex-keeper/status')
}

export function listCodexKeeperAccounts(): Promise<CodexKeeperAccountsResponse> {
  return apiClient.get<CodexKeeperAccountsResponse>('/codex-keeper/accounts')
}

export function runCodexKeeperOnce(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/run-once')
}

export function startCodexKeeper(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/start')
}

export function stopCodexKeeper(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/stop')
}

export function clearCodexKeeperLogs(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/logs/clear')
}

export function enableCodexKeeperAccount(authName: string): Promise<void> {
  return apiClient.post<void>(`/codex-keeper/accounts/${encodeURIComponent(authName)}/enable`)
}

export function disableCodexKeeperAccount(authName: string): Promise<void> {
  return apiClient.post<void>(`/codex-keeper/accounts/${encodeURIComponent(authName)}/disable`)
}

export function deleteCodexKeeperAccount(authName: string): Promise<void> {
  return apiClient.delete(`/codex-keeper/accounts/${encodeURIComponent(authName)}`)
}

export function bulkDeleteCodexKeeperAccounts(
  payload: CodexKeeperBulkDeletePayload,
): Promise<CodexKeeperBulkDeleteResponse> {
  return apiClient.post<CodexKeeperBulkDeleteResponse>(
    '/codex-keeper/accounts/bulk-delete',
    payload,
  )
}

export function refreshCodexKeeperAccounts(payload: CodexKeeperRefreshPayload): Promise<void> {
  return apiClient.post<void>('/codex-keeper/accounts/refresh', payload)
}

export function syncCodexKeeperAccountList(): Promise<void> {
  return apiClient.post<void>('/codex-keeper/accounts/sync')
}

export function updateCodexKeeperPriority(authName: string, priority: number): Promise<void> {
  return apiClient.patch<void>(`/codex-keeper/accounts/${encodeURIComponent(authName)}/priority`, {
    priority,
  })
}

export function queryCodexKeeperResetCredits(authName: string): Promise<CodexKeeperResetCredits> {
  return apiClient.post<CodexKeeperResetCredits>(
    `/codex-keeper/accounts/${encodeURIComponent(authName)}/reset-credits/query`,
  )
}

export function consumeCodexKeeperResetCredit(authName: string): Promise<CodexKeeperResetCredits> {
  return apiClient.post<CodexKeeperResetCredits>(
    `/codex-keeper/accounts/${encodeURIComponent(authName)}/reset-credits/consume`,
  )
}
