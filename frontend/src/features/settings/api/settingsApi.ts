import { apiClient } from '@/shared/api/apiClient'
import type { BrandingResponse, CollectorStatus, SettingsResponse, SettingsUpdatePayload } from '@/shared/types/api'

export interface CPAConfigResponse {
  content: string
  api_version?: number
  revision?: string
}

export function getBranding(): Promise<BrandingResponse> {
  return apiClient.get<BrandingResponse>('/branding')
}

export function getSettings(): Promise<SettingsResponse> {
  return apiClient.get<SettingsResponse>('/settings')
}

export function updateSettings(payload: SettingsUpdatePayload): Promise<SettingsResponse> {
  return apiClient.put<SettingsResponse>('/settings', payload)
}

export function getCollectorStatus(): Promise<CollectorStatus> {
  return apiClient.get<CollectorStatus>('/collector/status')
}

export function getCPAConfig(): Promise<CPAConfigResponse> {
  return apiClient.get<CPAConfigResponse>('/settings/cpa-config')
}

export function updateCPAConfig(payload: CPAConfigResponse): Promise<CPAConfigResponse> {
  return apiClient.put<CPAConfigResponse>('/settings/cpa-config', payload)
}
