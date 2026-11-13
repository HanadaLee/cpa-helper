import { expect, test, type Page } from '@playwright/test'

const account = {
  name: 'fixture.json', provider: 'codex', email: 'fixture@example.invalid', auth_index: 'fixture-index',
  v8_supported: true, requires_sign_in: true, renewal_state: 'sign_in_required', recovery_stage: 'sign_in_required',
  siwc_bind_supported: true, siwc_status: 'off', disabled: false, unavailable: false, runtime_only: false,
  priority: 3, status: 'active', success: 0, failed: 0,
  primary_used_percent: null, secondary_used_percent: null, primary_window_seconds: null, secondary_window_seconds: null,
  primary_reset_at: null, secondary_reset_at: null, primary_window_usage: null, secondary_window_usage: null,
}

async function mockCore(page: Page) {
  const actions: string[] = []
  const oauthRequests: Record<string, unknown>[] = []
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/auth/me') return route.fulfill({ json: { id: 1, username: 'admin', nickname: 'Admin', email: '', avatar: '', created_at: '2026-11-13T00:00:00+08:00', is_admin: true, cas_bound: false, can_change_password: true, must_change_password: false, can_view_account_status: true, can_view_usage_history: true } })
    if (path === '/api/branding') return route.fulfill({ json: { brand_name_zh: 'Gateway', brand_name_en: 'Gateway', brand_subtitle_zh: '测试', brand_subtitle_en: 'Test' } })
    if (path === '/api/codex-keeper/accounts') return route.fulfill({ json: { items: [account], priority_rules: [], degraded: false } })
    if (path === '/api/codex-keeper/status') return route.fulfill({ json: { running: false, running_modes: [], daemon_running: false, state: 'idle', detail: '', mode: null, stats: {}, logs: [] } })
    if (/\/accounts\/fixture.json\/(core-refresh|core-verify|siwc-bind)$/.test(path)) {
      actions.push(path.split('/').at(-1)!)
      return route.fulfill({ json: path.endsWith('siwc-bind') ? { url: 'https://auth.example.invalid/authorize', state: 'fixture-state' } : { ok: true } })
    }
    if (path === '/api/codex-keeper/oauth/start') {
      oauthRequests.push(route.request().postDataJSON())
      return route.fulfill({ json: { url: 'https://auth.example.invalid/authorize', state: 'fixture-state' } })
    }
    if (path === '/api/codex-keeper/oauth/status') return route.fulfill({ json: { status: 'wait' } })
    if (path === '/api/codex-keeper/oauth/session') return route.fulfill({ status: 204 })
    return route.fulfill({ json: {} })
  })
  return { actions, oauthRequests }
}

async function openActions(page: Page) {
  await page.getByRole('button', { name: '打开 fixture@example.invalid 的操作菜单', exact: true }).click()
}

test('native token refresh and inference verification remain distinct and verification requires confirmation', async ({ page }) => {
  const { actions } = await mockCore(page)
  await page.goto('/admin/credential-mgmt')
  await openActions(page)
  await page.getByRole('menuitem', { name: 'Core 刷新令牌', exact: true }).click()
  await expect.poll(() => actions).toEqual(['core-refresh'])
  await openActions(page)
  await page.getByRole('menuitem', { name: '推理验证', exact: true }).click()
  const confirmation = page.getByRole('dialog', { name: '推理验证', exact: true })
  await expect(confirmation.getByText(/最小真实请求/)).toBeVisible()
  expect(actions).toEqual(['core-refresh'])
  await confirmation.getByRole('button', { name: '验证', exact: true }).click()
  await expect.poll(() => actions).toEqual(['core-refresh', 'core-verify'])
})

test('credential reauthorization is targeted and never requests automatic inference verification', async ({ page }) => {
  const { actions, oauthRequests } = await mockCore(page)
  await page.goto('/admin/credential-mgmt')
  await openActions(page)
  await page.getByRole('menuitem', { name: '重新授权此凭证', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'OAuth 登录', exact: true })
  await expect(dialog.getByText(/fixture.json/)).toBeVisible()
  await expect(dialog.getByRole('combobox')).toBeDisabled()
  await dialog.getByRole('button', { name: '开始 Codex 登录', exact: true }).click()
  await expect.poll(() => oauthRequests.length).toBe(1)
  expect(oauthRequests[0]).toMatchObject({ provider: 'codex', name: 'fixture.json', auth_index: 'fixture-index' })
  expect(oauthRequests[0]).not.toHaveProperty('verify')
  expect(actions).toEqual([])
})

test('SIWC binding uses its native operation on the selected credential', async ({ page }) => {
  const { actions, oauthRequests } = await mockCore(page)
  await page.goto('/admin/credential-mgmt')
  await openActions(page)
  await page.getByRole('menuitem', { name: '绑定 SIWC', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'OAuth 登录', exact: true })
  await dialog.getByRole('button', { name: '开始 SIWC 绑定', exact: true }).click()
  await expect.poll(() => actions).toEqual(['siwc-bind'])
  expect(oauthRequests).toEqual([])
})

test('request details render actual tier and canonical accounting without confusing reasoning with extra output', async ({ page }) => {
  await mockCore(page)
  const record = {
    id: 1, timestamp: '2026-11-13T09:00:00+08:00', provider: 'claude', model: 'fixture-model', failed: false,
    input_tokens: 10, cached_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, output_tokens: 5,
    reasoning_tokens: 15, total_tokens: 30, estimated_cost_usd: 0.00005, unpriced: false,
    request_service_tier: 'priority', effective_service_tier: 'default', service_tier_evidence: 'response',
    cpa: { upstream_model: 'upstream-fixture', executor_type: 'claude', execution_id: 'execution-fixture',
      ttfb_ms: 5, ttfa_ms: 10, token_breakdown: { schema_version: 2, quality: 'complete', output: { total_tokens: 20 } } },
    raw_json: {},
  }
  await page.route('**/api/usage/options*', route => route.fulfill({ json: { users: [], api_key_descriptions: [], providers: [], models: [], sources: [], endpoints: [] } }))
  await page.route('**/api/usage/records?*', route => route.fulfill({ json: { items: [record], total: 1, page: 1, page_size: 50, start: '2026-11-13T00:00:00+08:00', end: '2026-11-14T00:00:00+08:00' } }))
  await page.route('**/api/usage/records/1*', route => route.fulfill({ json: record }))
  await page.goto('/admin/records')
  await page.getByRole('button', { name: '详情', exact: true }).click()
  const detail = page.locator('[data-slot="sheet-content"]')
  await expect(detail.getByText('default (response)', { exact: true })).toBeVisible()
  await expect(detail.getByText('execution-fixture', { exact: true })).toBeVisible()
  await expect(detail.getByText('计费输出 Token（含推理）', { exact: true })).toBeVisible()
  await expect(detail.getByText('20', { exact: true })).toBeVisible()
  await page.screenshot({ path: 'test-results/cpa-core-020-request-details.png', fullPage: true })
})
