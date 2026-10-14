import { expect, test, type Locator, type Page } from '@playwright/test'

const adminRoutes = [
  ['/admin/usage', /用量分析|Usage Analytics/i],
  ['/admin/records', /请求明细|Request Records/i],
  ['/admin/users', /用户管理|Users/i],
  ['/admin/pricing', /模型价格|Model Prices/i],
  ['/admin/upstreams', /上游管理|Upstreams/i],
  ['/admin/credential-mgmt', /凭证管理|Credential Management/i],
  ['/admin/cpamc', 'CPAMC'],
  ['/admin/settings', /系统设置|System Settings/i],
] as const

const accountRoutes = [
  ['/account/usage', /我的用量|My Usage/i],
  ['/account/records', /我的明细|My Records/i],
  ['/account/keys', /API 密钥|API Keys/i],
  ['/account/models', /可用模型|Available Models/i],
] as const

async function setupOrLogin(page: Page) {
  await page.goto('/login')
  const inputs = page.locator('input')
  await expect(inputs.first()).toBeVisible()
  await inputs.nth(0).fill('admin')
  await inputs.nth(1).fill('test-password')
  if ((await inputs.count()) >= 3) {
    await inputs.nth(2).fill('管理员')
  }
  await page.locator('button[type="submit"]').click()
  await page.waitForURL(/\/admin\/usage|\/change-credentials/)
  if (page.url().includes('/change-credentials')) {
    const passwordInputs = page.locator('input[type="password"]')
    await passwordInputs.nth(0).fill('test-password')
    await passwordInputs.nth(1).fill('new-test-password')
    await page.locator('button').filter({ hasText: /保存|Save/ }).click()
    await page.waitForURL(/\/admin\/usage/)
  }
}

async function expectTableInsidePanel(page: Page, tableSelector: string, panelSelector: string) {
  const tableBox = await page.locator(tableSelector).boundingBox()
  const panelBox = await page.locator(panelSelector).boundingBox()
  expect(tableBox).not.toBeNull()
  expect(panelBox).not.toBeNull()
  expect((tableBox?.y ?? 0) + (tableBox?.height ?? 0)).toBeLessThanOrEqual(
    (panelBox?.y ?? 0) + (panelBox?.height ?? 0) + 1,
  )
  await expect.poll(() =>
    page.locator('.content-scroll').evaluate((element) => element.scrollHeight <= element.clientHeight + 1),
  ).toBe(true)
}

async function expectPlainDialogFooter(dialog: Locator) {
  const footer = dialog.locator('[data-slot="dialog-footer"]')
  await expect(footer).toHaveCSS('border-top-width', '0px')
  await expect(footer).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)')
  await expect(footer).toHaveCSS('box-shadow', 'none')
}

test('initial navigation uses shell and dashboard skeletons instead of a blank screen', async ({ page }) => {
  await setupOrLogin(page)
  await expect(page.locator('[data-page-title]')).toBeVisible()

  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Network.setCacheDisabled', { cacheDisabled: true })

  let releaseAuthRequest!: () => void
  let releaseUsageRequest!: () => void
  const authRequestGate = new Promise<void>((resolve) => {
    releaseAuthRequest = resolve
  })
  const usageRequestGate = new Promise<void>((resolve) => {
    releaseUsageRequest = resolve
  })
  let holdAuthRequest = true
  let holdUsageRequest = true

  await page.route('**/api/auth/me', async (route) => {
    if (holdAuthRequest) {
      holdAuthRequest = false
      await authRequestGate
    }
    await route.continue()
  })
  await page.route('**/api/usage/overview**', async (route) => {
    if (holdUsageRequest) {
      holdUsageRequest = false
      await usageRequestGate
    }
    await route.continue()
  })

  await page.goto('/admin/usage', { waitUntil: 'domcontentloaded' })

  const appSkeleton = page.locator('[data-app-loading="true"]')
  await expect(appSkeleton).toBeVisible()
  await expect.poll(() => appSkeleton.locator('[data-slot="skeleton"]').count()).toBeGreaterThan(12)
  const startupPanels = appSkeleton.locator('.startup-panels > .startup-panel')
  const startupTrendBox = await startupPanels.nth(0).boundingBox()
  const startupTokenBox = await startupPanels.nth(1).boundingBox()
  expect(startupTrendBox).not.toBeNull()
  expect(startupTokenBox).not.toBeNull()
  expect(
    Math.abs((startupTrendBox?.width ?? 0) - ((startupTokenBox?.width ?? 0) * 2 + 16)),
  ).toBeLessThanOrEqual(1)
  await expect(appSkeleton.locator('.startup-token-legend [data-slot="skeleton"]')).toHaveCount(4)

  releaseAuthRequest()
  await expect(appSkeleton).toBeHidden()
  await expect(page.locator('[data-page-title]')).toBeVisible()
  await expect(page.locator('.filter-panel')).toBeVisible()

  const dashboardSkeleton = page.locator('[data-usage-loading="true"]')
  await expect(dashboardSkeleton).toBeVisible()
  await expect.poll(() => dashboardSkeleton.locator('[data-slot="skeleton"]').count()).toBeGreaterThan(20)
  const skeletonTopPanels = dashboardSkeleton.locator('.usage-skeleton-top-grid > .usage-skeleton-panel')
  const skeletonBottomColumns = dashboardSkeleton.locator('.usage-skeleton-bottom-grid > .usage-skeleton-column')
  const skeletonTokenBox = await skeletonTopPanels.nth(1).boundingBox()
  const skeletonRankingBox = await skeletonBottomColumns.nth(2).boundingBox()
  expect(skeletonTokenBox).not.toBeNull()
  expect(skeletonRankingBox).not.toBeNull()
  expect(Math.abs((skeletonTokenBox?.x ?? 0) - (skeletonRankingBox?.x ?? 0))).toBeLessThanOrEqual(1)
  expect(Math.abs((skeletonTokenBox?.width ?? 0) - (skeletonRankingBox?.width ?? 0))).toBeLessThanOrEqual(1)

  releaseUsageRequest()
  await expect(dashboardSkeleton).toBeHidden()
  await expect(page.locator('.dashboard-metric-grid')).toBeVisible()
  const tokenPanelBox = await page.locator('.area-token').boundingBox()
  const rankingColumnBox = await page.locator('.dashboard-column-right').boundingBox()
  expect(tokenPanelBox).not.toBeNull()
  expect(rankingColumnBox).not.toBeNull()
  expect(Math.abs((tokenPanelBox?.x ?? 0) - (rankingColumnBox?.x ?? 0))).toBeLessThanOrEqual(1)
  expect(Math.abs((tokenPanelBox?.width ?? 0) - (rankingColumnBox?.width ?? 0))).toBeLessThanOrEqual(1)
})

test('sidebar navigation switches immediately and lets the destination render its loading state', async ({ page }) => {
  await setupOrLogin(page)
  await page.goto('/admin/usage')
  await expect(page.locator('[data-page-title]')).toContainText(/用量分析|Usage Analytics/i)

  let releaseUpstreamRequest!: () => void
  const upstreamRequestGate = new Promise<void>((resolve) => {
    releaseUpstreamRequest = resolve
  })
  let releaseAuthRequest!: () => void
  const authRequestGate = new Promise<void>((resolve) => {
    releaseAuthRequest = resolve
  })
  let authRequests = 0
  await page.route('**/api/auth/me', async (route) => {
    authRequests += 1
    await authRequestGate
    await route.continue()
  })
  await page.route('**/api/upstreams', async (route) => {
    await upstreamRequestGate
    await route.continue()
  })

  await page.getByRole('button', { name: /上游管理|Upstreams/ }).click()
  await expect(page).toHaveURL(/\/admin\/upstreams/)
  await expect(page.locator('[data-page-title]')).toContainText(/上游管理|Upstream Management/i)
  await expect(page.locator('[data-page-title]').filter({ hasText: /用量分析|Usage Analytics/i })).toHaveCount(0)
  await expect(page.locator('.provider-table-shell [data-slot="skeleton"]').first()).toBeVisible()
  await expect.poll(() => authRequests).toBeGreaterThan(0)

  releaseAuthRequest()
  releaseUpstreamRequest()
  await expect(page.locator('.provider-table-shell [data-slot="skeleton"]')).toHaveCount(0)
})

test('a transient auth me gateway failure does not sign out a valid session', async ({ page }) => {
  await setupOrLogin(page)
  await page.goto('/admin/usage')

  let failedMeRequests = 0
  await page.route('**/api/auth/me', async (route) => {
    if (failedMeRequests === 0) {
      failedMeRequests += 1
      await route.fulfill({ status: 504, contentType: 'text/plain', body: 'Gateway Timeout' })
      return
    }
    await route.continue()
  })

  await page.reload()
  await expect.poll(() => failedMeRequests).toBe(1)
  await expect(page).toHaveURL(/\/admin\/usage$/)
  await expect(page.locator('[data-page-title]')).toContainText(/用量分析|Usage Analytics/i)
  await expect(page).not.toHaveURL(/\/login/)
})

test('upstream model discovery merges selected models into the editor', async ({ page }) => {
  await setupOrLogin(page)
  await page.route('**/api/upstreams', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        sections: {
          'gemini-api-key': [],
          'codex-api-key': [],
          'xai-api-key': [],
          'claude-api-key': [],
          'vertex-api-key': [],
          'openai-compatibility': [],
        },
      }),
    })
  })
  let discoveryPayload: Record<string, unknown> | null = null
  await page.route('**/api/upstreams/models', async (route) => {
    discoveryPayload = route.request().postDataJSON() as Record<string, unknown>
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        models: [
          { name: 'gemini-2.5-flash', display_name: 'Gemini 2.5 Flash' },
          { name: 'gemini-2.5-pro', display_name: 'Gemini 2.5 Pro' },
        ],
      }),
    })
  })

  await page.goto('/admin/upstreams')
  await page.getByRole('button', { name: /新建|New/ }).click()
  const upstreamDrawer = page.locator('[data-slot="sheet-content"]')
  await upstreamDrawer.locator('#upstream-api-key').fill('gemini-test-key')
  await upstreamDrawer.getByRole('button', { name: /获取模型|Fetch models/ }).click()

  const discoveryDialog = page.getByRole('dialog', { name: /获取模型|Fetch models/ })
  await expect(discoveryDialog).toBeVisible()
  await expect.poll(() => discoveryPayload?.section).toBe('gemini-api-key')
  await expect.poll(() => discoveryPayload?.api_key).toBe('gemini-test-key')
  const modelRow = discoveryDialog.locator('.model-discovery-row').filter({ hasText: 'gemini-2.5-pro' })
  await modelRow.locator('[role="checkbox"]').click()
  await discoveryDialog.getByRole('button', { name: /添加所选模型|Add selected models/ }).click()

  await expect(discoveryDialog).toBeHidden()
  await expect(upstreamDrawer.getByPlaceholder(/上游模型名称|Upstream model name/).first()).toHaveValue('gemini-2.5-pro')
  await expect(upstreamDrawer.getByPlaceholder(/显示名称（可选）|Display name \(optional\)/).first()).toHaveValue('Gemini 2.5 Pro')
})

test('standard users open the renamed credential status route in read-only mode', async ({ page }) => {
  await page.route('**/api/auth/me', async (route) => {
    await route.fulfill({
      json: {
        id: 2,
        username: 'member',
        nickname: 'Member',
        email: 'member@example.com',
        avatar: '',
        created_at: '2026-09-03T00:00:00Z',
        is_admin: false,
        cas_bound: false,
        can_change_password: true,
        must_change_password: false,
        can_view_account_status: true,
        can_view_usage_history: false,
      },
    })
  })
  await page.route('**/api/codex-keeper/accounts', async (route) => {
    await route.fulfill({ json: { items: [], priority_rules: [] } })
  })
  await page.route('**/api/codex-keeper/status', async (route) => {
    await route.fulfill({
      json: {
        running: false,
        running_modes: [],
        daemon_running: false,
        state: 'idle',
        detail: '尚未运行',
        mode: null,
        last_started_at: null,
        last_finished_at: null,
        stats: {
          total: 0,
          healthy: 0,
          status_disabled: 0,
          status_enabled: 0,
          priority_degraded: 0,
          priority_restored: 0,
          skipped: 0,
          network_error: 0,
        },
        logs: [],
      },
    })
  })

  await page.goto('/account/credential-status')

  await expect(page).toHaveURL(/\/account\/credential-status$/)
  await expect(page.locator('[data-page-title]')).toHaveText(/凭证状态|Credential Status/)
  await expect(page.getByRole('button', { name: /凭证状态|Credential Status/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /上传文件|Upload Files/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Codex 巡检|Inspect Codex/ })).toHaveCount(0)
})

test('API key actions can temporarily disable and re-enable one key', async ({ page }) => {
  await setupOrLogin(page)

  let disabled = false
  const keySummary = () => ({
    api_key_hash: 'fixture-key',
    api_key: 'sk-fixture-key',
    description: 'Temporary key',
    disabled,
    user_id: 1,
    user_name: 'admin',
    created_at: '2026-09-03T00:00:00Z',
    updated_at: '2026-09-03T00:00:00Z',
    records: 0,
    success_records: 0,
    failed_records: 0,
    total_tokens: 0,
    today_records: 0,
    today_success_records: 0,
    today_failed_records: 0,
    today_input_tokens: 0,
    today_output_tokens: 0,
    today_cached_tokens: 0,
    today_reasoning_tokens: 0,
    today_total_tokens: 0,
    today_estimated_cost_usd: 0,
    today_unpriced_records: 0,
    first_seen_at: null,
    last_seen_at: null,
    last_provider: null,
    last_model: null,
    providers: [],
    models: [],
  })

  await page.route('**/api/api-keys', async (route) => {
    await route.fulfill({ json: [keySummary()] })
  })
  await page.route('**/api/api-keys/fixture-key/disable', async (route) => {
    disabled = true
    await route.fulfill({ json: keySummary() })
  })
  await page.route('**/api/api-keys/fixture-key/enable', async (route) => {
    disabled = false
    await route.fulfill({ json: keySummary() })
  })

  await page.goto('/account/keys')
  const actionsButton = page.getByRole('button', { name: /打开 Temporary key 的操作菜单|Open actions for Temporary key/ })
  await actionsButton.click()
  await page.getByRole('menuitem', { name: /禁用|Disable/ }).click()
  const disableDialog = page.getByRole('alertdialog', { name: /禁用 API 密钥|Disable API key/ })
  await expect(disableDialog).toBeVisible()
  await disableDialog.getByRole('button', { name: /确认禁用|Confirm disable/ }).click()
  await expect(page.getByText(/已禁用|Disabled/).last()).toBeVisible()

  await actionsButton.click()
  await page.getByRole('menuitem', { name: /启用|Enable/ }).click()
  const enableDialog = page.getByRole('alertdialog', { name: /启用 API 密钥|Enable API key/ })
  await enableDialog.getByRole('button', { name: /确认启用|Confirm enable/ }).click()
  await expect(page.getByText(/已禁用|Disabled/)).toHaveCount(0)
})

test('request test uses the selected configured endpoint and usage analytics omits key-description filtering', async ({ page }) => {
  await setupOrLogin(page)
  await page.route('**/api/api-keys', async (route) => {
    await route.fulfill({ json: [{
      api_key_hash: 'fixture-key', api_key: 'sk-fixture-key', description: 'Test key', disabled: false,
      user_id: 1, user_name: 'admin', created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z',
      records: 0, success_records: 0, failed_records: 0, total_tokens: 0, today_records: 0,
      today_success_records: 0, today_failed_records: 0, today_input_tokens: 0, today_output_tokens: 0,
      today_cached_tokens: 0, today_reasoning_tokens: 0, today_total_tokens: 0, today_estimated_cost_usd: 0,
      today_unpriced_records: 0, first_seen_at: null, last_seen_at: null, last_provider: null, last_model: null,
      providers: [], models: ['gpt-test'],
    }] })
  })
  await page.route('**/api/account/model-request', async (route) => {
    await route.fulfill({ json: {
      model_request_url: 'https://primary.example.test',
      openai_base_url: 'https://primary.example.test/v1',
      chat_completions_url: 'https://primary.example.test/v1/chat/completions',
      extra_endpoints: [{ url: 'https://backup.example.test/v1', description: 'Backup endpoint' }],
    } })
  })
  await page.route('**/api/account/models', async (route) => {
    await route.fulfill({ json: {
      has_api_keys: true, api_key_count: 1, queryable_api_key_count: 1, quota_paused: false, errors: [],
      models: [{ id: 'gpt-test', name: 'GPT Test', sources: [{ api_key_hash: 'fixture-key' }] }],
    } })
  })
  let testPayload: Record<string, unknown> | null = null
  await page.route('**/api/account/model-request/test', async (route) => {
    testPayload = route.request().postDataJSON() as Record<string, unknown>
    await route.fulfill({ json: {
      endpoint: 'chat_completions', model: 'gpt-test', reply: 'pong', status_code: 200, duration_ms: 12,
    } })
  })

  await page.goto('/account/keys')
  await page.getByRole('button', { name: /打开 Test key 的操作菜单|Open actions for Test key/ }).click()
  await page.getByRole('menuitem', { name: /请求测试|Request test/ }).click()
  const dialog = page.getByRole('dialog', { name: /请求测试|Request test/ })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText(/请求测试使用默认 Endpoint|Request tests use the default endpoint/)).toHaveCount(0)
  await dialog.getByRole('combobox', { name: /请求 Endpoint|Request endpoint/ }).click()
  await page.getByRole('option', { name: 'Backup endpoint' }).click()
  await expect(dialog).toContainText('https://backup.example.test/v1/chat/completions')
  await dialog.getByRole('button', { name: /发送测试|Send test/ }).click()
  await expect(dialog).toContainText('pong')
  expect(testPayload?.base_url).toBe('https://backup.example.test/v1')

  await page.goto('/admin/usage')
  await expect(page.locator('.field-row .filter-combobox')).toHaveCount(4)
  await expect(page.locator('.field-row')).not.toContainText(/KEY 描述|Key description/)
  await page.goto('/account/usage')
  await expect(page.locator('.field-row')).toContainText(/KEY 描述|Key description/)
})

test('all migrated routes render and core controls remain interactive', async ({ page }) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error' && !message.text().startsWith('Failed to load resource:')) {
      consoleErrors.push(message.text())
    }
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))

  await setupOrLogin(page)
  await expect(page.getByText(/管理中心|Admin Center/, { exact: true })).toBeVisible()

  for (const [route, title] of adminRoutes) {
    await page.goto(route)
    await expect(page.locator('.desktop-location')).toContainText(title)
    await expect(page.locator('[data-page-title]')).toBeVisible()
    await expect(page.locator('.desktop-location')).toHaveCSS('visibility', 'visible')
  }

  for (const [route, title] of accountRoutes) {
    await page.goto(route)
    await expect(page.locator('.desktop-location')).toContainText(title)
    await expect(page.locator('[data-page-title]')).toBeVisible()
  }
  await expect(page.getByRole('button', { name: /账户设置|Account Settings/ })).toHaveCount(0)

  await page.goto('/admin/settings')
  const generalSettingsCard = page.locator('[data-slot="card"]').filter({
    has: page.getByText(/通用配置|General Settings/i, { exact: true }),
  })
  const collectionSettingsCard = page.locator('[data-slot="card"]').filter({
    has: page.getByText(/采集与保留设置|Collection and Retention Settings/i, { exact: true }),
  })
  await expect(generalSettingsCard.getByText(/^(采集与保留参数|Collection and retention)$/i)).toHaveCount(0)
  await page.getByRole('tab', { name: /采集与保留|Collection & Retention/i }).click()
  await expect(collectionSettingsCard.getByText(/^(采集与保留参数|Collection and retention)$/i)).toBeVisible()
  await expect(collectionSettingsCard.getByLabel(/批量读取数|Batch size/i)).toBeVisible()

  await page.getByRole('tab', { name: /认证配置|Authentication/i }).click()
  await expect(page.getByRole('tab', { name: /CAS 登录|CAS Login/i })).toHaveCount(0)
  const newUserQuotaCard = page.locator('[data-slot="card"]').filter({
    has: page.getByText(/新用户默认配额|Default quotas for new users/i, { exact: true }),
  })
  await expect(newUserQuotaCard.getByText(/不限制余额|Unlimited balance/i, { exact: true })).toBeVisible()
  await expect(newUserQuotaCard.getByLabel(/日配额|Daily quota/i)).toHaveValue('0')
  await expect(newUserQuotaCard.getByLabel(/周配额|Weekly quota/i)).toHaveValue('0')
  await expect(newUserQuotaCard.getByLabel(/每月|Monthly/i)).toHaveCount(0)
  await expect(newUserQuotaCard.getByLabel(/不限时|Lifetime/i)).toHaveCount(0)

  await page.goto('/admin/usage')
  const headerLayout = await page.locator('.app-header').evaluate((header) => {
    const headerBox = header.getBoundingClientRect()
    const title = header.querySelector<HTMLElement>('.desktop-location')
    if (!title) return null
    const titleBox = title.getBoundingClientRect()
    return {
      titleInset: titleBox.x - headerBox.x,
      titleWeight: getComputedStyle(title).fontWeight,
    }
  })
  expect(headerLayout).not.toBeNull()
  expect(headerLayout?.titleInset).toBeCloseTo(77, 0)
  expect(headerLayout?.titleWeight).toBe('500')
  const analyticsQuickRangeTabs = page.locator('.quick-ranges[data-slot="tabs"]')
  await expect(analyticsQuickRangeTabs.getByRole('tablist')).toHaveAccessibleName(/快捷时间范围|Quick time ranges/)
  await expect(analyticsQuickRangeTabs.getByRole('tab')).toHaveCount(6)
  await expect(page.locator('body')).toHaveCSS('font-family', '"Geist Variable", sans-serif')
  expect(await page.locator('body').evaluate((element) => getComputedStyle(element).fontSynthesis)).toContain('weight')
  const analyticsRangeTrigger = page.locator('[data-slot="date-time-range-trigger"]')
  const analyticsRangeBox = await analyticsRangeTrigger.boundingBox()
  expect(analyticsRangeBox?.width ?? 0).toBeGreaterThanOrEqual(420)
  expect(analyticsRangeBox?.height).toBe(32)
  expect(await analyticsRangeTrigger.innerText()).not.toMatch(/\d{2}:\d{2}:\d{2}/)
  await expect(analyticsQuickRangeTabs.getByRole('tablist')).toHaveCSS('height', '32px')
  await expect(analyticsRangeTrigger).toHaveCSS('font-size', '14px')
  expect(await analyticsRangeTrigger.locator('[data-slot="date-time-range-start"], [data-slot="date-time-range-end"]').evaluateAll(
    (elements) => elements.every((element) => element.scrollWidth <= element.clientWidth),
  )).toBe(true)
  const analyticsFilterRow = page.locator('.field-row')
  const analyticsFilterLayout = await analyticsFilterRow.evaluate((row) => {
    const children = Array.from(row.children) as HTMLElement[]
    const rowBox = row.getBoundingClientRect()
    const boxes = children.map((child) => child.getBoundingClientRect())
    return {
      yDifference: Math.max(...boxes.map((box) => box.y)) - Math.min(...boxes.map((box) => box.y)),
      leftInset: boxes[0]!.left - rowBox.left,
      rightInset: rowBox.right - boxes.at(-1)!.right,
    }
  })
  expect(analyticsFilterLayout.yDifference).toBeLessThanOrEqual(1)
  expect(Math.abs(analyticsFilterLayout.leftInset)).toBeLessThanOrEqual(1)
  expect(Math.abs(analyticsFilterLayout.rightInset)).toBeLessThanOrEqual(1)
  const analyticsSearchableFilter = analyticsFilterRow.locator('.filter-combobox-trigger').first()
  await expect(analyticsSearchableFilter).toHaveCSS('font-weight', '400')
  await expect(analyticsSearchableFilter).toHaveCSS('height', '32px')
  await expect(analyticsSearchableFilter).toHaveCSS('font-size', '14px')
  await analyticsSearchableFilter.click()
  const analyticsFilterPopup = page.locator('[data-slot="combobox-content"]')
  const analyticsFilterTriggerBox = await analyticsSearchableFilter.boundingBox()
  const analyticsFilterPopupBox = await analyticsFilterPopup.boundingBox()
  expect(analyticsFilterTriggerBox).not.toBeNull()
  expect(analyticsFilterPopupBox).not.toBeNull()
  expect(analyticsFilterPopupBox?.y ?? 0).toBeGreaterThanOrEqual(
    (analyticsFilterTriggerBox?.y ?? 0) + (analyticsFilterTriggerBox?.height ?? 0),
  )
  await expect(analyticsFilterPopup.getByRole('option').first()).toHaveText(/清除筛选|Clear filter/)
  await expect(page.getByPlaceholder(/搜索用户|Search users/)).toHaveCount(0)
  await page.keyboard.press('Escape')

  await page.goto('/account/settings')
  await expect(page).toHaveURL(/\/account\/settings$/)
  await expect(page.locator('[data-page-title]')).toHaveCount(0)

  await page.route('**/api/codex-keeper/accounts', async (route) => {
    await route.fulfill({
      json: {
        items: [
          {
            name: 'menu-test.json',
            provider: 'codex',
            email: 'menu-test@example.com',
            account_type: 'oauth',
            plan_type: 'pro_20x',
            disabled: false,
            unavailable: true,
            runtime_only: false,
            priority: -1,
            primary_used_percent: 20,
            secondary_used_percent: null,
            primary_reset_at: null,
            secondary_reset_at: null,
            primary_window_seconds: null,
            secondary_window_seconds: null,
            primary_window_usage: null,
            secondary_window_usage: null,
            quota_threshold: 90,
            last_status_code: 200,
            last_error: null,
            latest_action: null,
            status: 'error',
            status_message: 'quota exhausted',
            last_checked_at: null,
            last_healthy_at: null,
          },
          {
            name: 'claude-test.json',
            provider: 'anthropic',
            email: 'claude@example.com',
            label: 'Claude Team',
            disabled: false,
            unavailable: false,
            runtime_only: false,
            priority: 5,
            weight: 2,
            request_retry: 1,
            status: 'active',
            status_message: null,
            last_refresh_at: '2026-09-04T12:00:00Z',
            modified_at: '2026-09-04T12:00:00Z',
          },
          {
            name: 'overloaded-test.json',
            provider: 'codex',
            email: 'overloaded@example.com',
            account_type: 'oauth',
            plan_type: 'pro_20x',
            disabled: false,
            unavailable: true,
            runtime_only: false,
            priority: 0,
            last_status_code: 503,
            last_error: null,
            latest_action: null,
            status: 'error',
            status_message: '{"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later.","param":null}}',
            last_checked_at: null,
            last_healthy_at: null,
          },
        ],
        priority_rules: [],
      },
    })
  })
  await page.route('**/api/codex-keeper/auth-files/menu-test.json', async (route) => {
    await route.fulfill({
      json: {
        json: {
          note: 'E2E auth file note',
          priority: 0,
          websockets: false,
        },
      },
    })
  })
  await page.goto('/admin/credential-mgmt')
  const providerFilter = page.getByRole('combobox', { name: /提供商|Provider/ })
  await providerFilter.click()
  const providerPopup = page.locator('[data-slot="combobox-content"]')
  await expect(providerPopup.getByRole('option').first()).toHaveText(/清除筛选|Clear filter/)
  await expect(page.getByPlaceholder(/搜索提供商|Search providers/)).toHaveCount(0)
  await page.keyboard.press('Escape')
  const credentialTable = page.locator('.account-table')
  await expect(credentialTable.getByText('Claude', { exact: true })).toBeVisible()
  const codexCredentialRow = credentialTable.locator('tbody tr').filter({ hasText: 'menu-test.json' })
  await expect(codexCredentialRow.getByText('Codex', { exact: true })).toBeVisible()
  await expect(codexCredentialRow.getByText(/启用中|Enabled/, { exact: true })).toBeVisible()
  await expect(codexCredentialRow.getByText(/额度耗尽|Quota Exhausted/, { exact: true })).toBeVisible()
  await expect(codexCredentialRow.getByText(/周配额|Weekly Quota/, { exact: true })).toBeVisible()
  await expect(codexCredentialRow.getByText('OAuth', { exact: true })).toBeVisible()
  await expect(codexCredentialRow.getByText(/不可用|Unavailable/, { exact: true })).toHaveCount(0)
  await expect(codexCredentialRow.getByText('error', { exact: true })).toHaveCount(0)
  await expect(codexCredentialRow.getByText(/状态警告|Status Warning/, { exact: true })).toHaveCount(0)
  const overloadedCredentialRow = credentialTable.locator('tbody tr').filter({ hasText: 'overloaded-test.json' })
  await expect(overloadedCredentialRow.getByText(/状态警告|Status Warning/, { exact: true })).toBeVisible()
  await expect(overloadedCredentialRow.getByText(/不可用|Unavailable/, { exact: true })).toHaveCount(0)
  const accountPagination = page.locator('[data-slot="table-pagination-footer"]')
  await expect(accountPagination.locator('[data-slot="pagination"]')).toBeVisible()
  await expect(accountPagination.getByRole('combobox', { name: /每页数量|Rows per page/ })).toBeVisible()
  await expect(accountPagination.locator('[data-slot="pagination-first"]')).toBeVisible()
  await expect(accountPagination.locator('[data-slot="pagination-last"]')).toBeVisible()
  const accountTableBox = await page.locator('.account-table-shell').boundingBox()
  const accountPaginationBox = await accountPagination.boundingBox()
  expect(accountTableBox).not.toBeNull()
  expect(accountPaginationBox).not.toBeNull()
  expect(Math.abs((accountTableBox?.x ?? 0) - (accountPaginationBox?.x ?? 0))).toBeLessThanOrEqual(1)
  expect(Math.abs((accountTableBox?.width ?? 0) - (accountPaginationBox?.width ?? 0))).toBeLessThanOrEqual(1)
  const latestActionHeaderBox = await page.getByRole('columnheader', { name: /最近操作|Latest Action/ }).boundingBox()
  expect(latestActionHeaderBox).not.toBeNull()
  expect(latestActionHeaderBox?.width).toBeLessThanOrEqual(170)
  const accountActionHeaderBox = await page.locator('.account-table thead th').last().boundingBox()
  expect(accountActionHeaderBox).not.toBeNull()
  expect(accountActionHeaderBox?.width).toBeLessThanOrEqual(64)
  const accountRow = page.locator('.account-table tbody tr').first()
  const accountFirstCell = accountRow.locator('td').first()
  const accountFixedCell = accountRow.locator('td').last()
  await accountRow.hover()
  await expect.poll(async () => {
    const [firstCellColor, fixedCellColor] = await Promise.all([
      accountFirstCell.evaluate((element) => getComputedStyle(element).backgroundColor),
      accountFixedCell.evaluate((element) => getComputedStyle(element).backgroundColor),
    ])
    return fixedCellColor === firstCellColor
  }).toBe(true)
  await page.getByRole('button', { name: /打开 claude@example\.com 的操作菜单|Open actions for claude@example\.com/ }).click()
  await expect(page.getByRole('menuitem', { name: /详情|Details/ })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /禁用|Disable/ })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /巡检|Inspect/ })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: /删除|Delete/ })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /打开 menu-test@example\.com 的操作菜单|Open actions for menu-test@example\.com/ }).click()
  await expect(page.getByRole('menuitem', { name: /详情|Details/ })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /禁用|Disable/ })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /巡检|Inspect/ })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /删除|Delete/ })).toBeVisible()
  await page.getByRole('menuitem', { name: /删除|Delete/ }).click()
  const accountDeleteDialog = page.getByRole('dialog', { name: /删除凭证|Delete Credential/ })
  await expectPlainDialogFooter(accountDeleteDialog)
  await accountDeleteDialog.getByRole('button', { name: /取消|Cancel/ }).click()
  await page.getByRole('button', { name: /打开 menu-test@example\.com 的操作菜单|Open actions for menu-test@example\.com/ }).click()
  await page.getByRole('menuitem', { name: /详情|Details/ }).click()
  await page.getByRole('button', { name: /认证文件管理|Auth File Management/ }).click()
  const authFileDialog = page.getByRole('dialog', { name: /认证文件管理|Auth File Management/ })
  await expect(authFileDialog.getByRole('textbox').last()).toHaveValue('E2E auth file note')
  await expectPlainDialogFooter(authFileDialog)
  await authFileDialog.locator('[data-slot="dialog-footer"]').getByRole('button', { name: /关闭|Close/ }).click()

  await page.goto('/admin/pricing')
  const priceMetricCards = page.locator('.price-metric-card')
  await expect(priceMetricCards.first()).toHaveCSS('border-left-width', '1px')
  await expect(priceMetricCards.last()).toHaveCSS('border-right-width', '1px')
  const statusFilter = page.getByRole('combobox').nth(1)
  await expect(statusFilter).toHaveAccessibleName(/状态|Status/)
  await expect(statusFilter).toContainText(/状态|Status/)
  await statusFilter.click()
  const statusPopup = page.locator('[data-slot="combobox-content"]')
  await expect(statusPopup.getByRole('option').first()).toHaveText(/清除筛选|Clear filter/)
  const selectedStatusLabel = await statusPopup.getByRole('option').nth(1).innerText()
  await statusPopup.getByRole('option').nth(1).click()
  await expect(statusFilter).toContainText(selectedStatusLabel)
  await statusFilter.click()
  await page.locator('[data-slot="combobox-content"]').getByRole('option').first().click()
  await expect(statusFilter).toContainText(/状态|Status/)
  await expectTableInsidePanel(page, '.price-table', '.price-table-panel')
  const pricePagination = page.locator('[data-slot="table-pagination-footer"]')
  await expect(pricePagination.getByRole('combobox', { name: /每页数量|Rows per page/ })).toBeVisible()
  await expect(pricePagination.locator('[data-slot="pagination-first"]')).toBeVisible()
  await expect(pricePagination.locator('[data-slot="pagination-last"]')).toBeVisible()
  await page.getByRole('button', { name: /新增价格|Add price/ }).click()
  const priceDialog = page.getByRole('dialog', { name: /新增价格|Add price/ })
  const fastMultiplierInput = priceDialog.locator('#price-fast-multiplier')
  await expect(fastMultiplierInput).toHaveCount(0)
  await priceDialog.getByRole('switch', { name: /支持 FAST 模式|Supports FAST mode/ }).click()
  await fastMultiplierInput.fill('2')
  await expect(fastMultiplierInput).toHaveValue('2')
  await expect(fastMultiplierInput).toHaveAttribute('step', '0.01')
  expect(await fastMultiplierInput.evaluate((input: HTMLInputElement) => ({
    valid: input.validity.valid,
    stepMismatch: input.validity.stepMismatch,
  }))).toEqual({ valid: true, stepMismatch: false })
  const longContextFastSupport = priceDialog.getByRole('switch', {
    name: /长上下文支持 FAST 模式|FAST mode available for long context/,
  })
  await expect(longContextFastSupport).toHaveCount(0)
  const longContextSwitch = priceDialog.getByRole('switch', { name: /启用长上下文费率|Enable long-context rates/ })
  await expect(longContextSwitch).toBeVisible()
  await longContextSwitch.click()
  await expect(longContextFastSupport).toBeVisible()
  await fastMultiplierInput.fill('1')
  await expect(longContextFastSupport).toBeVisible()
  await fastMultiplierInput.fill('2')
  await expect(longContextFastSupport).toBeVisible()
  await expect(priceDialog.locator('#price-long-context-threshold')).toBeVisible()
  await expect(priceDialog.locator('#price-long-context-input')).toBeVisible()
  await expect(priceDialog.locator('#price-long-context-output')).toBeVisible()
  await expect(priceDialog.locator('#price-long-context-cache-read')).toBeVisible()
  await expect(priceDialog.locator('#price-long-context-cache-write')).toBeVisible()
  const priceCancelButton = priceDialog.getByRole('button', { name: /取消|Cancel/ })
  await expect(priceCancelButton).toHaveCSS('cursor', 'pointer')
  await expect(priceCancelButton).toHaveCSS('border-top-width', '1px')
  await expectPlainDialogFooter(priceDialog)
  await priceCancelButton.click()

  await page.goto('/admin/upstreams')
  await expect(page.getByText(/上游配置|Upstream configuration/, { exact: true })).toHaveCount(0)
  await expect(page.getByText(/按提供商类型管理 API 密钥、路由和模型映射。|Manage API keys, routes, and model mappings by provider family\./, { exact: true })).toHaveCount(0)
  const providerTabs = page.locator('.provider-tabs[data-slot="tabs"]')
  await expect(providerTabs.getByRole('tablist')).toHaveAccessibleName(/提供商|Providers/)
  await expect(providerTabs.getByRole('tab')).toHaveCount(6)
  await expect(page.locator('.provider-nav')).toHaveCount(0)
  const upstreamWorkbenchLayout = await page.locator('.upstream-workbench').evaluate((workbench) => {
    const switcher = workbench.querySelector<HTMLElement>('.provider-switcher')
    const table = workbench.querySelector<HTMLElement>('.provider-table-shell')
    if (!switcher || !table) return null
    const switcherBox = switcher.getBoundingClientRect()
    const tableBox = table.getBoundingClientRect()
    return {
      leftDifference: Math.abs(switcherBox.x - tableBox.x),
      widthDifference: Math.abs(switcherBox.width - tableBox.width),
      tableBelowSwitcher: tableBox.y >= switcherBox.bottom,
    }
  })
  expect(upstreamWorkbenchLayout).not.toBeNull()
  expect(upstreamWorkbenchLayout?.leftDifference).toBeLessThanOrEqual(1)
  expect(upstreamWorkbenchLayout?.widthDifference).toBeLessThanOrEqual(1)
  expect(upstreamWorkbenchLayout?.tableBelowSwitcher).toBe(true)
  const upstreamSearch = page.getByPlaceholder(/搜索密钥、地址或前缀|Search keys, URLs, or prefixes/)
  await expect(upstreamSearch.locator('..')).toHaveAttribute('data-slot', 'input-group')
  await expect(upstreamSearch.locator('..').locator('svg.lucide-search')).toBeVisible()
  const upstreamCreateButton = page.getByRole('button', { name: /新建|New/ })
  const upstreamAlignment = await page.locator('.provider-panel__toolbar').evaluate((toolbar) => {
    const search = toolbar.querySelector<HTMLElement>('[data-slot="input-group"]')
    const create = toolbar.querySelector<HTMLElement>('.provider-create-button')
    if (!search || !create) return null
    const searchBox = search.getBoundingClientRect()
    const createBox = create.getBoundingClientRect()
    return {
      yDifference: Math.abs(searchBox.y - createBox.y),
      heightDifference: Math.abs(searchBox.height - createBox.height),
    }
  })
  expect(upstreamAlignment).not.toBeNull()
  expect(upstreamAlignment?.yDifference).toBeLessThanOrEqual(1)
  expect(upstreamAlignment?.heightDifference).toBeLessThanOrEqual(1)
  await expect(upstreamCreateButton).toHaveCSS('height', '32px')
  await expect(upstreamCreateButton).toHaveCSS('cursor', 'pointer')
  await upstreamCreateButton.click()
  const upstreamDrawer = page.locator('[data-slot="sheet-content"]')
  await expect(upstreamDrawer).toBeVisible()
  await expect(upstreamDrawer).toHaveCSS('width', '720px')
  await page.getByRole('button', { name: /取消|Cancel/ }).click()

  await page.goto('/admin/users')
  await expect(page.getByRole('button', { name: /^编辑 |^Edit / }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: /打开 .*操作菜单|Open actions for/ })).toHaveCount(0)
  await page.getByRole('button', { name: /增加用户|Add user/ }).click()
  await expect(page.getByText(/增加用户|Add user/, { exact: true }).last()).toBeVisible()
  const adminSwitch = page.locator('button[role="switch"]').first()
  await adminSwitch.click()
  await expect(adminSwitch).toHaveAttribute('data-state', 'checked')
  await expect(adminSwitch.locator('[data-slot="switch-thumb"]')).toHaveCSS('border-top-width', '1px')
  await page.getByRole('button', { name: /取消|Cancel/ }).click()

  const usageRecordFixture = {
    id: 101,
    timestamp: '2026-08-31T12:00:00+08:00',
    api_key_description: 'E2E key',
    user_id: 1,
    user_label: 'admin',
    provider: 'openai',
    model: 'gpt-5.6-terra',
    reasoning_effort: 'xhigh',
    request_service_tier: 'fast',
    endpoint: '/v1/responses',
    source: 'e2e',
    request_id: 'request-detail-width-e2e',
    auth_index: null,
    auth: 'api_key',
    latency_ms: 1200,
    ttft_ms: 180,
    failed: false,
    input_tokens: 1200,
    output_tokens: 400,
    cached_tokens: 200,
    cache_read_tokens: 200,
    cache_creation_tokens: 0,
    reasoning_tokens: 120,
    total_tokens: 1600,
    estimated_cost_usd: 0.012,
    unpriced: false,
  }
  await page.route('**/api/usage/records?*', async (route) => {
    await route.fulfill({
      json: {
        items: [usageRecordFixture],
        total: 1,
        page: 1,
        page_size: 50,
        start: '2026-08-31T00:00:00+08:00',
        end: '2026-09-01T00:00:00+08:00',
      },
    })
  })
  await page.route('**/api/usage/records/101*', async (route) => {
    await route.fulfill({
      json: {
        ...usageRecordFixture,
        raw_json: {
          request: {
            model: 'gpt-5.6-terra',
            input: 'A deliberately long raw payload value used to verify that the wider request detail drawer remains readable.',
          },
          response: { id: 'request-detail-width-e2e' },
        },
      },
    })
  })
  await page.goto('/admin/records')
  await expect(page.locator('.field-row .filter-combobox')).toHaveCount(5)
  await expect(page.locator('.field-row')).not.toContainText(/KEY 描述|Key description/)
  const quickRangeTabs = page.locator('.quick-ranges[data-slot="tabs"]')
  await expect(quickRangeTabs.getByRole('tablist')).toHaveAccessibleName(/快捷时间范围|Quick time ranges/)
  await expect(quickRangeTabs.getByRole('tab')).toHaveCount(6)
  await expect(page.locator('input[type="datetime-local"]')).toHaveCount(0)
  await expect(page.locator('[data-slot="date-time-range-start"]')).toBeVisible()
  await expect(page.locator('[data-slot="date-time-range-end"]')).toBeVisible()
  const recordsRangeTrigger = page.locator('[data-slot="date-time-range-trigger"]')
  const recordsRangeBox = await recordsRangeTrigger.boundingBox()
  expect(recordsRangeBox?.width ?? 0).toBeGreaterThanOrEqual(420)
  expect(await recordsRangeTrigger.innerText()).not.toMatch(/\d{2}:\d{2}:\d{2}/)
  expect(await recordsRangeTrigger.locator('[data-slot="date-time-range-start"], [data-slot="date-time-range-end"]').evaluateAll(
    (elements) => elements.every((element) => element.scrollWidth <= element.clientWidth),
  )).toBe(true)
  const recordsFilterLayout = await page.locator('.field-row').evaluate((row) => {
    const boxes = Array.from(row.children, (child) => (child as HTMLElement).getBoundingClientRect())
    const rowBox = row.getBoundingClientRect()
    return {
      yDifference: Math.max(...boxes.map((box) => box.y)) - Math.min(...boxes.map((box) => box.y)),
      leftInset: boxes[0]!.left - rowBox.left,
      rightInset: rowBox.right - boxes.at(-1)!.right,
    }
  })
  expect(recordsFilterLayout.yDifference).toBeLessThanOrEqual(1)
  expect(Math.abs(recordsFilterLayout.leftInset)).toBeLessThanOrEqual(1)
  expect(Math.abs(recordsFilterLayout.rightInset)).toBeLessThanOrEqual(1)
  await page.locator('[data-slot="date-time-range-trigger"]').click()
  await expect(page.locator('[data-slot="range-calendar"]')).toBeVisible()
  const rangeTimeInputs = page.locator('[data-slot="time-picker"]')
  await expect(rangeTimeInputs).toHaveCount(2)
  await expect(page.locator('input[type="time"]')).toHaveCount(0)
  for (const field of await rangeTimeInputs.all()) {
    await expect(field.getByRole('spinbutton')).toHaveCount(2)
    await expect(field.locator('[data-reka-time-field-segment="hour"]')).toHaveAttribute('aria-valuenow', /\d+/)
    await expect(field.locator('[data-reka-time-field-segment="minute"]')).toHaveAttribute('aria-valuenow', /\d+/)
  }
  await page.keyboard.press('Escape')
  await expect(page.locator('table')).toBeVisible()
  const adminTimeHeaderBox = await page.getByRole('columnheader', { name: /^(时间|Time)$/ }).boundingBox()
  const adminNicknameHeaderBox = await page.getByRole('columnheader', { name: /^(用户昵称|User nickname)$/ }).boundingBox()
  expect(adminTimeHeaderBox?.width ?? 0).toBeGreaterThanOrEqual(160)
  expect(adminNicknameHeaderBox?.width ?? Number.POSITIVE_INFINITY).toBeLessThanOrEqual(110)
  await expect(page.getByRole('columnheader', { name: /^(来源|Source)$/ })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: /^(思考|Reasoning)$/ })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: /^(接口|Endpoint)$/ })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: /^(费用|Cost)$/ })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: /^(服务商|Provider)$/ })).toHaveCount(0)
  await expect(page.getByRole('cell', { name: '$0.012', exact: true })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: /^(KEY 描述|Key description)$/ })).toHaveCount(0)
  await expect(page.locator('.records-table')).toHaveCSS('border-top-left-radius', /[1-9]/)
  await expectTableInsidePanel(page, '.records-table', '.records-table-panel')
  const recordsPagination = page.locator('[data-slot="table-pagination-footer"]')
  await expect(recordsPagination.getByRole('combobox', { name: /每页数量|Rows per page/ })).toBeVisible()
  await expect(recordsPagination.locator('[data-slot="pagination-first"]')).toBeVisible()
  await expect(recordsPagination.locator('[data-slot="pagination-last"]')).toBeVisible()
  await page.getByRole('button', { name: /详情|Details/ }).first().click()
  const requestDetailSheet = page.getByRole('dialog', { name: /请求事件详情|Request event details/ })
  const requestDetailSheetBox = await requestDetailSheet.boundingBox()
  expect(requestDetailSheetBox).not.toBeNull()
  expect(requestDetailSheetBox?.width).toBeGreaterThanOrEqual(900)
  await expect(requestDetailSheet).toHaveCSS('border-left-width', '0px')
  await expect(requestDetailSheet.locator('.mono-json')).toHaveCSS('overflow-x', 'auto')
  await expect(requestDetailSheet).toContainText(/API KEY 描述|API key description/)
  await expect(requestDetailSheet).toContainText('E2E key')
  await expect(requestDetailSheet).toContainText('openai')
  await requestDetailSheet.getByRole('button', { name: /Close/ }).click()

  await page.goto('/account/records')
  await expect(page.locator('.field-row')).toContainText(/KEY 描述|Key description/)
  await expect(page.getByRole('columnheader', { name: /^(KEY 描述|Key description)$/ })).toBeVisible()
  const accountTimeHeaderBox = await page.getByRole('columnheader', { name: /^(时间|Time)$/ }).boundingBox()
  expect(accountTimeHeaderBox?.width ?? 0).toBeGreaterThanOrEqual(160)
  await expect(page.getByRole('columnheader', { name: /^(用户昵称|User nickname)$/ })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: /^(费用|Cost)$/ })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: /^(服务商|Provider)$/ })).toHaveCount(0)
  await expect(page.getByRole('cell', { name: '$0.012', exact: true })).toBeVisible()
  await page.getByRole('cell', { name: '$0.012', exact: true }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: 'test-results/account-records-cost.png', fullPage: true })

  usageRecordFixture.estimated_cost_usd = 0
  for (const unpriced of [true, false]) {
    usageRecordFixture.unpriced = unpriced
    for (const path of ['/admin/records', '/account/records']) {
      await page.goto(path)
      await expect(page.getByRole('cell', { name: unpriced ? /^(未定价|Unpriced)$/ : '$0.00', exact: true })).toBeVisible()
    }
  }

  await page.goto('/admin/settings')
  const brandingSection = page.locator('.settings-section').filter({ hasText: /界面品牌|Interface branding/ })
  const accessSection = page.locator('.settings-section').filter({ hasText: /访问配置|Access control/ })
  const collectionSection = page.locator('.settings-section').filter({ hasText: /采集与保留参数|Collection and retention/ })
  await expect(accessSection).toBeVisible()
  await expect(accessSection.getByText(/开启本地采集|Enable local collection/)).toHaveCount(0)
  await page.getByRole('tab', { name: /采集与保留|Collection & Retention/i }).click()
  await expect(collectionSection.getByText(/开启本地采集|Enable local collection/)).toBeVisible()
  await expect(collectionSection.getByText(/用量明细保留天数|Usage detail retention days/)).toBeVisible()
  await page.getByRole('tab', { name: /通用配置|General/i }).click()
  const brandingInputs = brandingSection.locator('input')
  await expect(brandingInputs).toHaveCount(4)
  await expect(brandingInputs.nth(0)).toHaveValue('CPA-Helper')
  await brandingInputs.nth(0).fill('测试边缘网关')
  await brandingInputs.nth(1).fill('Test Edge Gateway')
  await page.getByRole('button', { name: /保存设置|Save settings/ }).click()
  await expect(page.locator('.brand-copy strong')).toHaveText(/测试边缘网关|Test Edge Gateway/)
  await brandingInputs.nth(0).fill('CPA-Helper')
  await brandingInputs.nth(1).fill('CPA-Helper')
  await page.getByRole('button', { name: /保存设置|Save settings/ }).click()
  await expect(page.locator('.brand-copy strong')).toHaveText('CPA-Helper')
  await page.getByRole('button', { name: /追加 Endpoint|Add endpoint/ }).click()
  const extraEndpointGroups = page.locator('.extra-endpoint-row [data-slot="input-group"]')
  const extraEndpointURLBox = await extraEndpointGroups.nth(0).boundingBox()
  const extraEndpointDescriptionBox = await extraEndpointGroups.nth(1).boundingBox()
  const cliProxyBox = await page.locator('#cliaproxy-url').boundingBox()
  const modelRequestBox = await page.locator('#model-request-url').boundingBox()
  expect(extraEndpointURLBox).not.toBeNull()
  expect(extraEndpointDescriptionBox).not.toBeNull()
  expect(cliProxyBox).not.toBeNull()
  expect(modelRequestBox).not.toBeNull()
  expect(Math.abs((extraEndpointURLBox?.width ?? 0) - (extraEndpointDescriptionBox?.width ?? 0))).toBeLessThanOrEqual(1)
  expect(Math.abs((extraEndpointURLBox?.x ?? 0) - (cliProxyBox?.x ?? 0))).toBeLessThanOrEqual(1)
  expect(Math.abs((extraEndpointDescriptionBox?.x ?? 0) - (modelRequestBox?.x ?? 0))).toBeLessThanOrEqual(1)
  const managementKeyGroup = page.locator('#management-key').locator('..')
  await expect(extraEndpointGroups.nth(0)).toHaveCSS(
    'background-color',
    await managementKeyGroup.evaluate((element) => getComputedStyle(element).backgroundColor),
  )
  const switches = page.locator('button[role="switch"]')
  await expect(switches.first()).toBeVisible()
  await page.locator('.content-scroll').evaluate((element) => element.scrollTo({ top: element.scrollHeight }))
  await expect(page.locator('.desktop-location')).toHaveCSS('visibility', 'visible')

  await page.goto('/account/keys')
  await expect(page.locator('.desktop-location')).toContainText(/API 密钥|API Keys/i)
  await page.getByRole('button', { name: /新建|New API key/i }).click()
  const apiKeyDialog = page.getByRole('dialog', { name: /新建 API 密钥|New API key/i })
  await expect(apiKeyDialog).toBeVisible()
  const editorInputBox = await apiKeyDialog.getByRole('textbox').boundingBox()
  const editorSaveBox = await apiKeyDialog.getByRole('button', { name: /创建|Create/ }).boundingBox()
  expect(editorInputBox).not.toBeNull()
  expect(editorSaveBox).not.toBeNull()
  expect((editorSaveBox?.y ?? 0) - ((editorInputBox?.y ?? 0) + (editorInputBox?.height ?? 0))).toBeGreaterThanOrEqual(8)
  await page.getByRole('button', { name: /取消|Cancel/ }).click()
  const endpointTypeTabs = page.locator('.api-endpoint-type-tabs[data-slot="tabs"]')
  const endpointTypeGroup = endpointTypeTabs.locator('[data-slot="tabs-list"]')
  const endpointTypeButtons = endpointTypeGroup.locator('[data-slot="tabs-trigger"]')
  await expect(endpointTypeGroup).toHaveAccessibleName(/URL 类型|URL type/)
  await expect(page.getByText(/^(URL 类型|URL type)$/)).toHaveCount(0)
  await expect(endpointTypeButtons).toHaveCount(4)
  await expect(endpointTypeButtons.first()).toHaveCSS('cursor', 'pointer')
  await expect(endpointTypeButtons.first()).toHaveAttribute('data-state', 'active')
  const endpointTypeLayout = await endpointTypeButtons.evaluateAll((elements) => {
    const boxes = elements.map((element) => element.getBoundingClientRect())
    const yPositions = boxes.map((box) => box.y)
    return {
      yDifference: Math.max(...yPositions) - Math.min(...yPositions),
    }
  })
  expect(endpointTypeLayout.yDifference).toBeLessThanOrEqual(1)
  const endpointGroupBox = await endpointTypeGroup.boundingBox()
  const endpointFirstButtonBox = await endpointTypeButtons.first().boundingBox()
  const endpointLastButtonBox = await endpointTypeButtons.last().boundingBox()
  expect(endpointGroupBox).not.toBeNull()
  expect(endpointFirstButtonBox).not.toBeNull()
  expect(endpointLastButtonBox).not.toBeNull()
  const endpointLeftInset = (endpointFirstButtonBox?.x ?? 0) - (endpointGroupBox?.x ?? 0)
  const endpointRightInset =
    ((endpointGroupBox?.x ?? 0) + (endpointGroupBox?.width ?? 0)) -
    ((endpointLastButtonBox?.x ?? 0) + (endpointLastButtonBox?.width ?? 0))
  expect(endpointLeftInset).toBeGreaterThanOrEqual(2)
  expect(endpointRightInset).toBeGreaterThanOrEqual(2)
  expect(
    Math.abs(endpointLeftInset - endpointRightInset),
  ).toBeLessThanOrEqual(1)
  await endpointTypeButtons.nth(1).click()
  await expect(endpointTypeButtons.nth(1)).toHaveAttribute('data-state', 'active')
  await expect(endpointTypeButtons.first()).toHaveAttribute('data-state', 'inactive')

  expect(consoleErrors).toEqual([])
})

test('available models shows the regular, peak, long-context and FAST rate tiers', async ({ page }) => {
  await setupOrLogin(page)
  await page.route('**/api/account/models', async (route) => {
    await route.fulfill({
      json: {
        has_api_keys: true,
        api_key_count: 1,
        queryable_api_key_count: 1,
        errors: [],
        models: [
          {
            id: 'gpt-5.6-terra',
            name: 'GPT 5.6 Terra',
            object: 'model',
            owner: 'openai',
            created: null,
            metadata: {},
            sources: [
              {
                api_key_hash: 'fixture-key',
                api_key_preview: 'sk-…test',
                description: 'Test key',
              },
            ],
            price: {
              provider: 'openai',
              model: 'gpt-5.6-terra',
              input_usd_per_million: 2,
              output_usd_per_million: 12,
              cache_read_usd_per_million: 0.2,
              cache_creation_usd_per_million: 2.5,
              request_usd: null,
              fast_enabled: true,
              fast_multiplier: 1.8,
              long_context_enabled: true,
              long_context_threshold_tokens: 200000,
              long_context_input_usd_per_million: 4,
              long_context_output_usd_per_million: 16,
              long_context_cache_read_usd_per_million: 0.4,
              long_context_cache_creation_usd_per_million: 4,
              long_context_fast_unsupported: false,
              off_peak_enabled: true,
              peak_input_usd_per_million: 3,
              peak_output_usd_per_million: 18,
              peak_cache_read_usd_per_million: 0.3,
              peak_cache_creation_usd_per_million: 3.5,
              peak_request_usd: null,
              long_context_peak_input_usd_per_million: 6,
              long_context_peak_output_usd_per_million: 24,
              long_context_peak_cache_read_usd_per_million: 0.6,
              long_context_peak_cache_creation_usd_per_million: 6,
              billing_unit: 'token',
            },
          },
        ],
      },
    })
  })

  await page.goto('/account/models')
  await expect(page.getByRole('columnheader', { name: /费率档位|Rate tier/ })).toBeVisible()
  const tiers = page.getByRole('row').filter({ hasText: 'gpt-5.6-terra' }).getByRole('cell').nth(3)
  await expect(tiers.locator('.rate-tier-line')).toHaveCount(8)
  await expect(tiers).toContainText('FAST')
  await expect(tiers).not.toContainText('×1.8')
  await expect(page.getByRole('row').filter({ hasText: 'gpt-5.6-terra' }).getByRole('cell').nth(5).locator('.rate-tier-line')).toHaveText(['2', '3.6', '3', '5.4', '4', '7.2', '6', '10.8'])
  await expect(page.locator('.models-page .metric-card')).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: /所有者|Owner|来源 Key|Source Key/ })).toHaveCount(0)
  await page.setViewportSize({ width: 1024, height: 1000 })
  const modelTableContainer = page.locator('.available-models-table [data-slot="table-container"]')
  expect(await modelTableContainer.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(false)
})

test('theme and mobile navigation survive the migration', async ({ page }) => {
  await setupOrLogin(page)
  await expect(page.locator('[data-slot="sidebar"][data-variant="inset"]')).toBeVisible()
  await expect.poll(() =>
    page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--cpa-primary').trim()),
  ).toMatch(/^oklch\((?:0\.55|55%) (?:0\.19|\.19) 257\)$/)
  await expect(page.locator('[data-slot="sidebar-container"]')).toHaveCSS('border-right-width', '0px')
  await expect(page.locator('[data-slot="sidebar-container"]')).toHaveCSS('width', '256px')
  const usageMenuButton = page.getByRole('button', { name: /用量分析|Usage Analytics/ })
  await expect(usageMenuButton).toHaveCSS('cursor', 'pointer')
  await expect(usageMenuButton).toHaveCSS('font-size', '14px')
  await expect(usageMenuButton).toHaveCSS('height', '32px')
  await expect(page.locator('a[href*="github.com/walkingddd/CPA-Helper"]')).toHaveCount(0)
  const desktopUserMenuButton = page.getByRole('button', { name: /管理员.*admin|Admin.*admin/i })
  await expect(desktopUserMenuButton.locator('.lucide-ellipsis-vertical')).toBeVisible()
  const desktopUserButtonBox = await desktopUserMenuButton.boundingBox()
  await desktopUserMenuButton.click()
  const desktopUserMenu = page.getByRole('menu')
  await expect(page.getByRole('menuitem', { name: /退出登录|Sign out/ })).toBeVisible()
  const desktopUserMenuBox = await desktopUserMenu.boundingBox()
  expect(desktopUserButtonBox).not.toBeNull()
  expect(desktopUserMenuBox).not.toBeNull()
  expect(desktopUserMenuBox?.x ?? 0).toBeGreaterThanOrEqual(
    (desktopUserButtonBox?.x ?? 0) + (desktopUserButtonBox?.width ?? 0) - 1,
  )
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: /打开导航|Open navigation/ }).click()
  await expect(page.locator('div[data-state="collapsed"][data-side="left"]')).toBeVisible()
  await expect.poll(() => page.locator('[data-slot="sidebar-container"]').evaluate((sidebar) => {
    const mark = sidebar.querySelector<HTMLElement>('.brand-mark')
    if (!mark) return Number.POSITIVE_INFINITY
    const sidebarBox = sidebar.getBoundingClientRect()
    const markBox = mark.getBoundingClientRect()
    return Math.abs(
      (sidebarBox.left + sidebarBox.width / 2) - (markBox.left + markBox.width / 2),
    )
  })).toBeLessThanOrEqual(1)
  const collapsedBrandHeader = page.locator('[data-slot="sidebar-container"] [data-sidebar="header"]')
  await expect(collapsedBrandHeader).toHaveCSS('overflow', 'visible')
  await expect(collapsedBrandHeader.locator('.sidebar-brand-button')).toHaveCSS('overflow', 'visible')
  expect(await page.locator('[data-slot="sidebar-container"]').evaluate((sidebar) => {
    const mark = sidebar.querySelector<HTMLElement>('.brand-mark')
    if (!mark) return false
    const sidebarBox = sidebar.getBoundingClientRect()
    const markBox = mark.getBoundingClientRect()
    return markBox.left > sidebarBox.left && markBox.right < sidebarBox.right
  })).toBe(true)
  await page.getByRole('button', { name: /打开导航|Open navigation/ }).click()
  await page.getByRole('button', { name: /切换主题|Switch theme/ }).first().click()
  await page.getByRole('button', { name: /切换主题|Switch theme/ }).first().click()
  await expect(page.locator('html')).toHaveClass(/dark/)
  await expect.poll(() =>
    page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--cpa-primary').trim()),
  ).toMatch(/^oklch\((?:0\.68|68%) (?:0\.16|\.16) 257\)$/)
  await expect(page.locator('[data-slot="sidebar-container"]')).toHaveCSS('border-right-width', '0px')

  await page.setViewportSize({ width: 390, height: 844 })
  await page.reload()
  await page.getByRole('button', { name: /打开导航|Open navigation/ }).click()
  await expect(page.locator('[data-mobile="true"]')).toHaveCSS('border-right-color', 'oklch(1 0 0 / 0.1)')
  await expect(page.locator('[data-mobile="true"]')).toHaveCSS('width', '288px')
  await expect(page.locator('.mobile-brand-copy strong')).toBeVisible()
  await expect(page.locator('.mobile-version-badge')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /用量分析|Usage Analytics/ }).first()).toBeVisible()
  const mobileUserMenuButton = page.getByRole('button', { name: /管理员.*admin|Admin.*admin/i })
  await expect(mobileUserMenuButton.locator('.lucide-ellipsis-vertical')).toBeVisible()
  const mobileUserButtonBox = await mobileUserMenuButton.boundingBox()
  await mobileUserMenuButton.click()
  const mobileUserMenuBox = await page.getByRole('menu').boundingBox()
  expect(mobileUserButtonBox).not.toBeNull()
  expect(mobileUserMenuBox).not.toBeNull()
  expect((mobileUserMenuBox?.y ?? 0) + (mobileUserMenuBox?.height ?? 0)).toBeLessThanOrEqual(
    (mobileUserButtonBox?.y ?? 0) + 1,
  )
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /用户管理|Users/ }).click()
  await expect(page).toHaveURL(/\/admin\/users/)
  await expect(page.locator('[data-mobile="true"]')).toBeHidden()
})
