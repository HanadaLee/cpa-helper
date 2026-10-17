import { expect, test, type Page } from '@playwright/test'
import { setTime } from './helpers/date-time'

test.use({ timezoneId: 'America/Los_Angeles' })

const users = Array.from({ length: 55 }, (_, index) => ({
  user_id: index + 1, username: `user-${index + 1}`, label: `用户 ${index + 1}`,
}))
const items = users.map((user, index) => ({
  ...user, cost_usd: index === 54 ? 0 : 100 - index, records: index === 54 ? 0 : 1000,
  failed_records: 0, total_tokens: index === 54 ? 0 : 1200000, unpriced_records: index === 0 ? 1 : 0,
}))

async function mockAPI(page: Page, admin = true, language = 'zh') {
  await page.addInitScript(lang => localStorage.setItem('cpa-helper-language', lang), language)
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/auth/me') return route.fulfill({ json: {
      id: 1, username: 'admin', nickname: 'Admin', email: '', avatar: '', created_at: '2026-10-01T00:00:00+08:00',
      is_admin: admin, cas_bound: false, oidc_bound: false, can_change_password: true,
      must_change_password: false, can_view_account_status: false, can_view_usage_history: true,
    } })
    if (url.pathname === '/api/branding') return route.fulfill({ json: {
      brand_name_zh: 'AI Gateway', brand_name_en: 'AI Gateway', brand_subtitle_zh: '边缘 AI 平台', brand_subtitle_en: 'Edge AI Platform',
    } })
    if (url.pathname === '/api/usage/options') return route.fulfill({ json: {
      users: [], api_key_descriptions: [], providers: ['openai'], models: ['gpt-model'], sources: [], endpoints: ['/v1/responses'],
    } })
    if (url.pathname === '/api/usage/billing') {
      const selected = url.searchParams.get('user_id')
      const filtered = selected ? items.filter(item => String(item.user_id) === selected) : items
      return route.fulfill({ json: {
        start: url.searchParams.get('start'), end: url.searchParams.get('end'), items: filtered, users,
        active_users: filtered.filter(item => item.records > 0).length,
        summary: {
          cost_usd: filtered.reduce((sum, item) => sum + item.cost_usd, 0), records: filtered.reduce((sum, item) => sum + item.records, 0),
          total_tokens: filtered.reduce((sum, item) => sum + item.total_tokens, 0), failed_records: 0,
          unpriced_records: filtered.reduce((sum, item) => sum + item.unpriced_records, 0),
        },
      } })
    }
    return route.fulfill({ status: 200, json: {} })
  })
}

test('billing shows all users with pagination, zero costs, filters, ranges and detail links', async ({ page }) => {
  await mockAPI(page)
  await page.goto('/admin/billing')
  await expect(page.getByRole('heading', { name: '费用核算', exact: true })).toBeVisible()
  await expect(page.locator('[data-billing-row]')).toHaveCount(50)
  await expect(page.getByText('存在未定价用量', { exact: true })).toBeVisible()
  await expect(page.locator('[data-billing-row]').first()).toContainText('$100.00')
  await expect(page.locator('[data-billing-row]').first()).toContainText('1.2M')
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.locator('[data-billing-row]')).toHaveCount(5)
  await expect(page.locator('[data-billing-row]').last()).toContainText('$0.00')
  await page.getByRole('combobox', { name: '用户昵称', exact: true }).click()
  await page.getByPlaceholder('搜索用户昵称').fill('user-55')
  await page.getByRole('option', { name: '用户 55 (user-55)', exact: true }).click()
  await expect(page.locator('[data-billing-row]')).toHaveCount(1)
  await expect(page).toHaveURL(/user_id=55/)
  await page.getByRole('combobox', { name: '用户昵称', exact: true }).click()
  await page.getByRole('option', { name: '清除筛选', exact: true }).click()
  await expect(page.locator('[data-billing-row]')).toHaveCount(50)
  for (const [name, key] of [['昨日', 'yesterday'], ['近 24 小时', 'last24h'], ['近 3 日', 'last3d'], ['近 7 日', 'last7d'], ['近 30 日', 'last30d'], ['今日', 'today']]) {
    const request = page.waitForRequest(req => new URL(req.url()).pathname === '/api/usage/billing')
    await page.getByRole('tab', { name, exact: true }).click()
    const params = new URL((await request).url()).searchParams
    const start = params.get('start')!
    const end = params.get('end')!
    if (key === 'yesterday' || key === 'today') {
      expect(start).toContain('T00:00:00')
      expect(end).toContain('T00:00:00')
    } else {
      const days = { last24h: 1, last3d: 3, last7d: 7, last30d: 30 }[key]!
      expect(new Date(end).getTime() - new Date(start).getTime()).toBe(days * 86400000)
    }
    await expect(page).toHaveURL(new RegExp(`quick_range=${key}`))
    await expect(page.locator('.billing-page')).toHaveAttribute('aria-busy', 'false')
  }
  await page.locator('[data-slot="date-time-range-trigger"]').click()
  await setTime(page.getByRole('group', { name: '开始时间', exact: true }), '00:15')
  await page.getByRole('button', { name: '应用', exact: true }).click()
  await expect(page).not.toHaveURL(/quick_range/)
  await expect(page.locator('[data-slot="date-time-range-trigger"]')).toContainText('00:15')
  await page.getByRole('combobox', { name: '模型', exact: true }).click()
  await page.getByRole('option', { name: 'gpt-model', exact: true }).click()
  await expect(page).toHaveURL(/model=gpt-model/)
  await expect(page.locator('.billing-page')).toHaveAttribute('aria-busy', 'false')
  await page.screenshot({ path: 'test-results/billing-desktop.png', fullPage: true })
  await page.locator('[data-billing-row]').first().getByRole('button', { name: '明细', exact: true }).click()
  await expect(page).toHaveURL(/\/admin\/records\?/)
  const detail = new URL(page.url())
  expect(detail.searchParams.get('user_id')).toBe('1')
  expect(detail.searchParams.get('model')).toBe('gpt-model')
  expect(detail.searchParams.get('start')).toContain('00:15:00')
})

test('billing has English labels and an empty state', async ({ page }) => {
  await mockAPI(page, true, 'en')
  await page.route('**/api/usage/billing**', route => route.fulfill({ json: {
    items: [], users: [], active_users: 0, start: '', end: '',
    summary: { cost_usd: 0, records: 0, total_tokens: 0, failed_records: 0, unpriced_records: 0 },
  } }))
  await page.goto('/admin/billing')
  await expect(page.getByRole('heading', { name: 'Cost Accounting', exact: true })).toBeVisible()
  await expect(page.getByText('No matching users', { exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Last 30 days', exact: true })).toBeVisible()
})

test('billing uses skeletons, rejects stale responses and surfaces load failure', async ({ page }) => {
  await mockAPI(page)
  let release!: () => void
  const gate = new Promise<void>(resolve => { release = resolve })
  let held = true
  await page.route('**/api/usage/billing**', async route => {
    if (held) { held = false; await gate }
    await route.fallback()
  })
  await page.goto('/admin/billing')
  await expect(page.locator('.billing-page [data-slot="skeleton"]').first()).toBeVisible()
  await expect(page.getByRole('button', { name: '导出', exact: true })).toBeDisabled()
  await page.getByRole('tab', { name: '昨日', exact: true }).click()
  await expect(page.locator('[data-billing-row]')).toHaveCount(50)
  release()
  await expect(page.getByRole('tab', { name: '昨日', exact: true })).toHaveAttribute('data-state', 'active')
  await page.route('**/api/usage/billing**', route => route.fulfill({ status: 503, json: { detail: { message: '暂时不可用' } } }))
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByText('加载失败', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '导出', exact: true })).toBeDisabled()
  await expect(page.locator('[data-billing-row]')).toHaveCount(0)
})

test('billing is responsive and regular users cannot navigate to it', async ({ page }) => {
  await mockAPI(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/billing')
  await expect(page.locator('[data-billing-row]')).toHaveCount(50)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  await page.screenshot({ path: 'test-results/billing-mobile.png', fullPage: true })
  await page.unrouteAll()
  await mockAPI(page, false)
  await page.goto('/admin/billing')
  await expect(page).toHaveURL(/\/account\/usage/)
  await expect(page.getByRole('button', { name: '费用核算', exact: true })).toHaveCount(0)
})
