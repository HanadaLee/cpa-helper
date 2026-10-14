import { expect, type Locator, type Page } from '@playwright/test'

export async function setTime(field: Locator, time: string) {
  const [hour, minute] = time.split(':')
  for (const [part, value] of [['hour', hour!], ['minute', minute!]]) {
    const segment = field.locator(`[data-reka-time-field-segment="${part}"]`)
    await segment.click()
    await segment.pressSequentially(value!)
    await expect(segment).toHaveAttribute('aria-valuenow', String(Number(value)))
  }
}

export function nextMonthDate() {
  const beijing = new Date(Date.now() + 8 * 60 * 60 * 1000)
  return new Date(Date.UTC(beijing.getUTCFullYear(), beijing.getUTCMonth() + 1, 15)).toISOString().slice(0, 10)
}

export async function selectExpiry(page: Page, trigger: Locator, time = '00:00') {
  await trigger.click()
  const popup = page.locator('[data-slot="date-time-content"]')
  await expect(popup).toBeVisible()
  await popup.locator('[data-slot="calendar-next"]').click()
  const date = nextMonthDate()
  await popup.locator(`[data-value="${date}"]`).click()
  await setTime(popup.locator('[data-slot="time-picker"]'), time)
  await popup.getByRole('button', { name: /^(应用|Apply)$/ }).click()
  await expect(popup).toHaveCount(0)
  await expect(trigger).toHaveText(`${date} ${time}`)
  return `${date}T${time}:00+08:00`
}
