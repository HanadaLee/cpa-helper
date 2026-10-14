import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { expect, test } from '@playwright/test'
import { nextMonthDate, setTime } from './helpers/date-time'

test.use({ timezoneId: 'America/Los_Angeles' })

test('no native browser date or time pickers remain in application templates', () => {
  const root = fileURLToPath(new URL('../../src/', import.meta.url))
  for (const file of readdirSync(root, { recursive: true }).filter(file => /\.(vue|tsx?|html)$/.test(String(file)))) {
    const source = readFileSync(`${root}/${file}`, 'utf8')
    expect(source, String(file)).not.toMatch(/\btype\s*=\s*["'](?:datetime-local|date|time|month|week)["']/i)
    expect(source, String(file)).not.toMatch(/\.showPicker\s*\(/)
  }
})

for (const language of ['zh', 'en'] as const) {
  test(`card expiration works above nested dialogs in ${language}, including mobile and Beijing time`, async ({ page }) => {
    await page.addInitScript((lang) => {
      localStorage.setItem('cpa-helper-language', lang)
    }, language)
    await page.request.post('/api/auth/setup', { data: { username: 'admin', password: 'test-password', nickname: 'Admin' } })
    expect((await page.request.post('/api/auth/login', { data: { username: 'admin', password: 'test-password' } })).ok()).toBeTruthy()
    const username = `picker-${language}-${Date.now()}`
    const user = await (await page.request.post('/api/users', { data: { username, password: 'test-password', nickname: username } })).json()
    await page.goto('/admin/users')
    await page.getByRole('combobox', { name: /^(每页数量|Rows per page)$/ }).click()
    await page.getByRole('option', { name: '100', exact: true }).click()
    await page.getByRole('button', { name: new RegExp(`(管理 ${username} 的额度卡|Manage cards for ${username})`) }).click()
    const manager = page.getByRole('dialog', { name: /^(配额卡管理|Quota cards)$/ })
    await manager.getByRole('tab', { name: /^(重置卡|Reset cards)$/ }).click()
    await manager.getByRole('button', { name: /^(发放卡片|Issue cards)$/ }).click()
    const editor = page.getByRole('dialog', { name: /^(发放卡片|Issue cards)$/ })
    await editor.getByRole('switch', { name: /^(永久有效|Never expires)$/ }).click()
    await editor.getByRole('button', { name: /^(发放|Issue)$/ }).click()
    await expect(page.getByText(/请选择未来的到期时间|Choose a future expiration time/)).toBeVisible()
    const trigger = editor.getByLabel(/到期时间（北京时间）|Expiration \(Beijing time\)/)
    await trigger.click()
    const popup = page.locator('[data-slot="date-time-content"]')
    const apply = popup.getByRole('button', { name: /^(应用|Apply)$/ })
    await expect(apply).toBeDisabled()
    await popup.locator('[data-slot="calendar-next"]').click()
    await popup.locator(`[data-value="${nextMonthDate()}"]`).click()
    await setTime(popup.locator('[data-slot="time-picker"]'), '23:59')
    await popup.locator('[data-reka-time-field-segment="minute"]').press('Backspace')
    await popup.locator('[data-reka-time-field-segment="minute"]').press('Backspace')
    await expect(apply).toBeDisabled()
    await setTime(popup.locator('[data-slot="time-picker"]'), '23:59')
    await popup.getByRole('button', { name: /^(取消|Cancel)$/ }).click()
    await expect(trigger).toHaveText(/选择日期与时间|Select date and time/)
    await trigger.click()
    await expect(apply).toBeDisabled()
    await popup.locator('[data-slot="calendar-next"]').click()
    await popup.locator(`[data-value="${nextMonthDate()}"]`).click()
    await setTime(popup.locator('[data-slot="time-picker"]'), '23:59')
    await expect.poll(() => apply.evaluate((element) => {
      const box = element.getBoundingClientRect()
      return element.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2))
    })).toBe(true)
    await page.screenshot({ path: `test-results/date-time-${language}-desktop.png` })
    await apply.click()
    await editor.getByRole('button', { name: /^(发放|Issue)$/ }).click()
    await expect(editor).toHaveCount(0)
    const cards = await (await page.request.get(`/api/quota/cards?user_id=${user.id}`)).json()
    const card = cards.items[0]
    expect(new Date(card.expires_at).toISOString()).toBe(`${nextMonthDate()}T15:59:00.000Z`)
    await manager.getByRole('button', { name: new RegExp(`^(编辑卡片 ${card.id}|Edit card ${card.id})$`) }).click()
    const edit = page.getByRole('dialog', { name: /^(编辑卡片|Edit card)$/ })
    await expect(edit.getByLabel(/到期时间（北京时间）|Expiration \(Beijing time\)/)).toHaveText(`${nextMonthDate()} 23:59`)
    await page.setViewportSize({ width: 390, height: 844 })
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(page.locator('html')).toHaveClass(/dark/)
    await edit.getByLabel(/到期时间（北京时间）|Expiration \(Beijing time\)/).click()
    await expect(popup).toBeVisible()
    const box = await popup.boundingBox()
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(390)
    expect(box!.y).toBeGreaterThanOrEqual(0)
    expect(box!.y + box!.height).toBeLessThanOrEqual(844)
    await expect(popup.locator(`[data-value="${nextMonthDate()}"]`)).toHaveAttribute('data-selected', 'true')
    await page.screenshot({ path: `test-results/date-time-${language}-mobile.png` })
    await page.keyboard.press('Escape')
    await expect(popup).toHaveCount(0)
    await expect(edit).toBeVisible()
    await edit.getByRole('switch', { name: /^(永久有效|Never expires)$/ }).click()
    await expect(edit.locator('[data-slot="date-time-trigger"]')).toHaveCount(0)
    await edit.getByRole('button', { name: /^(保存|Save)$/ }).click()
    await expect(edit).toHaveCount(0)
    const updated = await (await page.request.get(`/api/quota/cards?user_id=${user.id}`)).json()
    expect(updated.items[0].expires_at).toBeNull()
  })
}

test('all four usage and detail filters share styled time inputs with apply and cancel', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
  await page.request.post('/api/auth/login', { data: { username: 'admin', password: 'test-password' } })
  for (const route of ['/admin/usage', '/admin/records', '/account/usage', '/account/records']) {
    await page.goto(route)
    const trigger = page.locator('[data-slot="date-time-range-trigger"]')
    const original = (await trigger.textContent())!
    await trigger.click()
    const popup = page.locator('[data-slot="popover-content"]')
    const start = popup.getByRole('group', { name: '开始时间', exact: true })
    await setTime(start, '00:15')
    await popup.getByRole('button', { name: '取消', exact: true }).click()
    await expect(trigger).toHaveText(original)
    await trigger.click()
    await setTime(start, '00:15')
    await popup.getByRole('button', { name: '应用', exact: true }).click()
    await expect(trigger).toContainText('00:15')
    await expect(page.locator('input[type="time"], input[type="datetime-local"]')).toHaveCount(0)
  }
})
