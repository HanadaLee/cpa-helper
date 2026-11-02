import { apiClient } from '@/shared/api/apiClient'
import type {
  PricingSyncProxySettings,
  PricingSyncProxySettingsPayload,
  ModelPrice,
  ModelPriceCatalogResponse,
  ModelPricePayload,
  ModelPriceSyncResponse,
  PricingHolidayCalendar,
} from '@/shared/types/api'

export function listModelPrices(): Promise<ModelPrice[]> {
  return apiClient.get<ModelPrice[]>('/model-prices')
}

export function listModelPriceCatalog(): Promise<ModelPriceCatalogResponse> {
  return apiClient.get<ModelPriceCatalogResponse>('/model-prices/catalog')
}

export function createModelPrice(payload: ModelPricePayload): Promise<ModelPrice> {
  return apiClient.post<ModelPrice>('/model-prices', payload)
}

export function updateModelPrice(id: number, payload: ModelPricePayload): Promise<ModelPrice> {
  return apiClient.put<ModelPrice>(`/model-prices/${id}`, payload)
}

export function deleteModelPrice(id: number): Promise<void> {
  return apiClient.delete(`/model-prices/${id}`)
}

export function syncModelsDevModelPrices(): Promise<ModelPriceSyncResponse> {
  return apiClient.post<ModelPriceSyncResponse>('/model-prices/sync/models-dev')
}

export function getPricingSyncProxySettings(): Promise<PricingSyncProxySettings> {
  return apiClient.get<PricingSyncProxySettings>('/model-prices/sync-proxy')
}

export function updatePricingSyncProxySettings(
  payload: PricingSyncProxySettingsPayload,
): Promise<PricingSyncProxySettings> {
  return apiClient.put<PricingSyncProxySettings>('/model-prices/sync-proxy', payload)
}

export function getPricingHolidayCalendar(year: number): Promise<PricingHolidayCalendar> {
  return apiClient.get<PricingHolidayCalendar>(`/model-prices/holiday-calendar?year=${year}`)
}

export function syncPricingHolidayCalendar(year: number): Promise<PricingHolidayCalendar> {
  return apiClient.post<PricingHolidayCalendar>('/model-prices/holiday-calendar/sync', { year })
}

export function updatePricingCalendarSettings(
  peakOnMakeupDays: boolean,
  peakPeriods: Array<{ start: string; end: string }>,
): Promise<{ peak_on_makeup_days: boolean; peak_periods: Array<{ start: string; end: string }> }> {
  return apiClient.put('/model-prices/holiday-calendar/settings', { peak_on_makeup_days: peakOnMakeupDays, peak_periods: peakPeriods })
}
