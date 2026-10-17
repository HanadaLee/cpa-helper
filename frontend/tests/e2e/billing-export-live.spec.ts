import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'

// Run against a newly started local backend with an empty, disposable data dir:
// CPA_HELPER_E2E_LIVE_BILLING=1 (the Vite API proxy must point to that backend).
test('billing downloads real Excel exports for all pages and selected filters', async ({ page, baseURL }) => {
  test.skip(process.env.CPA_HELPER_E2E_LIVE_BILLING !== '1', 'Requires an isolated empty local backend')
  test.setTimeout(90_000)
  expect(['127.0.0.1', 'localhost']).toContain(new URL(baseURL!).hostname)
  const setup = await page.request.post('/api/auth/setup', { data: {
    username: 'export-admin', password: 'export-test-password', nickname: '导出管理员',
  } })
  expect(setup.ok(), 'Use a fresh disposable database for this test').toBeTruthy()
  for (let index = 1; index <= 55; index++) {
    const created = await page.request.post('/api/users', { data: {
      username: `export-user-${index}`, password: 'export-test-password', nickname: `导出用户 ${index}`, is_admin: false,
    } })
    expect(created.ok()).toBeTruthy()
  }
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
  await page.goto('/admin/billing?quick_range=yesterday')
  await expect(page.locator('[data-billing-row]')).toHaveCount(50)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.locator('[data-billing-row]')).toHaveCount(6)

  const download = page.waitForEvent('download')
  const exported = page.waitForResponse(res => new URL(res.url()).pathname === '/api/usage/billing/export')
  await page.getByRole('button', { name: '导出 Excel', exact: true }).click()
  const [file, response] = await Promise.all([download, exported])
  expect(response.status()).toBe(200)
  expect(response.headers()['content-type']).toContain('spreadsheetml.sheet')
  expect(file.suggestedFilename()).toMatch(/^费用核算_.*\.xlsx$/)
  const params = new URL(response.url()).searchParams
  expect(params.get('page')).toBeNull()
  expect(params.get('page_size')).toBeNull()
  expect(params.get('start')).toContain('T00:00:00')
  expect(params.get('lang')).toBe('zh')
  const bytes = await readFile((await file.path())!)
  expect(bytes.subarray(0, 2).toString()).toBe('PK')
  expect(bytes.length).toBeGreaterThan(1000)
  // Chromium may omit attachment bodies from DevTools; inspect the downloaded
  // file instead. Workbook cells and totals are parsed in the backend tests.
  await file.saveAs(test.info().outputPath('billing.xlsx'))
  await expect(page.getByText('费用核算表格已导出', { exact: true })).toBeVisible()

  const beijingDay = (daysAgo: number) => new Date(Date.now() + 8 * 3600_000 - daysAgo * 86400_000).toISOString().slice(0, 10)
  const customStart = `${beijingDay(2)}T00:15:00+08:00`
  const customEnd = `${beijingDay(1)}T12:30:00+08:00`
  await page.goto(`/admin/billing?user_id=1&provider=openai&model=test-model&endpoint=%2Fv1%2Fresponses&failed=false&start=${encodeURIComponent(customStart)}&end=${encodeURIComponent(customEnd)}`)
  await expect(page.locator('[data-billing-row]')).toHaveCount(1)
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'en'))
  await page.reload()
  await expect(page.getByRole('button', { name: 'Export Excel', exact: true })).toBeEnabled()
  const filteredDownload = page.waitForEvent('download')
  const filteredResponse = page.waitForResponse(res => new URL(res.url()).pathname === '/api/usage/billing/export')
  await page.getByRole('button', { name: 'Export Excel', exact: true }).click()
  const filtered = await filteredResponse
  expect(filtered.status()).toBe(200)
  const selected = new URL(filtered.url()).searchParams
  for (const [key, value] of Object.entries({ user_id: '1', provider: 'openai', model: 'test-model', endpoint: '/v1/responses', failed: 'false', lang: 'en' })) {
    expect(selected.get(key)).toBe(value)
  }
  expect(selected.get('start')).toBe(customStart)
  expect(selected.get('end')).toBe(customEnd)
  expect((await filteredDownload).suggestedFilename()).toMatch(/^Cost-Accounting_.*\.xlsx$/)

  await page.route('**/api/usage/billing/export**', route => route.fulfill({ status: 503, json: { detail: { message: 'export unavailable' } } }))
  await page.getByRole('button', { name: 'Export Excel', exact: true }).click()
  await expect(page.getByText('export unavailable', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Export Excel', exact: true })).toBeEnabled()
})
