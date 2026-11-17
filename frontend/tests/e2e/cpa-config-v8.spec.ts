import { expect, test, type Page } from '@playwright/test'
import { parse } from 'yaml'
import { cpaConfigFields } from '../../src/features/settings/components/cpaConfigFields'
import { cpaConfigPath } from '../../src/features/settings/components/cpaConfigPaths'

const nativeYAML = `# retained comment
config-version: 8
server: {host: '', port: 18316}
access: {api-keys: []}
api-keys:
  codex:
    - name: production
      base-url: https://example.invalid/v1
      keys: [{api-key: upstream-secret, request-retry: null}]
observability: {logs: {debug: false}, usage: {usage-statistics-enabled: true}}
routing: {retry: {request-retry: 0}}
plugins: {enabled: false, configs: {custom: {untouched: true}}}
`

async function mockConfig(page: Page, version = 8, fail = false, initialContent?: string, language = 'zh') {
  let content = initialContent ?? (version === 8 ? nativeYAML : 'host: ""\nport: 18316\napi-keys: [legacy-client]\n')
  const writes: { content: string, api_version: number, revision: string }[] = []
  await page.addInitScript(language => localStorage.setItem('cpa-helper-language', language), language)
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/auth/me') return route.fulfill({ json: { id: 1, username: 'admin', nickname: 'Admin', email: '', avatar: '', created_at: '2026-11-13T00:00:00+08:00', is_admin: true, cas_bound: false, can_change_password: true, must_change_password: false, can_view_account_status: true, can_view_usage_history: true } })
    if (path === '/api/branding') return route.fulfill({ json: { brand_name_zh: 'Gateway', brand_name_en: 'Gateway', brand_subtitle_zh: '测试', brand_subtitle_en: 'Test' } })
    if (path === '/api/settings') return route.fulfill({ json: {
      cliaproxy_url: 'http://127.0.0.1:8317', management_key: 'synthetic-key', model_request_url: '', model_request_extra_endpoints: [],
      cpamc_url: '/cpamc', brand_name_zh: 'Gateway', brand_name_en: 'Gateway', brand_subtitle_zh: '测试', brand_subtitle_en: 'Test',
      collector_enabled: false, batch_size: 100, poll_interval_seconds: 2, retry_interval_seconds: 10, usage_detail_retention_days: 90,
      new_user_quota_unlimited: false, new_user_quota_daily_usd: 0, new_user_quota_weekly_usd: 0,
    } })
    if (path === '/api/codex-keeper/settings') return route.fulfill({ json: {
      schedule_cron: '*/30 * * * *', quota_threshold: 100, usage_timeout_seconds: 30, cpa_timeout_seconds: 30, max_retries: 2,
      worker_threads: 8, conditional_refresh_interval_seconds: 30, account_refresh_cache_minutes: 10, dry_run: true,
      enable_credential_websockets: false, auto_start_daemon: false, priority_rules: [], next_run_times: [],
    } })
    if (path === '/api/settings/cpa-config') {
      if (route.request().method() === 'PUT') {
        writes.push(route.request().postDataJSON())
        if (fail) return route.fulfill({ status: 409, json: { detail: { message: 'CPA 配置已被修改，请重新加载并核对后再保存' } } })
        content = writes.at(-1)!.content
      }
      return route.fulfill({ json: { content, api_version: version, revision: version === 8 ? `"${(writes.length ? 'b' : 'a').repeat(64)}"` : '' } })
    }
    return route.fulfill({ json: {} })
  })
  await page.goto('/admin/settings')
  await page.getByRole('tab', { name: language === 'zh' ? 'CPA 配置' : 'CPA Configuration', exact: true }).click()
  await expect(page.locator('#cpa-config-port')).toHaveValue('18316')
  return writes
}

test('Core 0.2 advanced settings preserve structured configuration and reject non-object policies', async ({ page }) => {
  const content = nativeYAML.replace('server: {host: \'\', port: 18316}', 'server: {host: \'\', port: 18316, response-compression: {enabled: true, future-option: keep}}')
  const writes = await mockConfig(page, 8, false, content)
  await page.getByRole('tab', { name: 'Core 高级配置', exact: true }).click()
  await page.locator('#cpa-config-kv-path').fill('/data/core-kv.db')
  await page.locator('#cpa-config-response-compression').fill('[invalid-object]')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  expect(writes).toHaveLength(0)
  await page.locator('#cpa-config-response-compression').fill('enabled: true\nfuture-option: keep\nmin-bytes: 512')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  const saved = parse(writes[0].content)
  expect(saved.server['kv-store'].path).toBe('/data/core-kv.db')
  expect(saved.server['response-compression']).toEqual({ enabled: true, 'future-option': 'keep', 'min-bytes': 512 })
  expect(saved.plugins).toEqual(parse(nativeYAML).plugins)
  expect(saved['api-keys']).toEqual(parse(nativeYAML)['api-keys'])
})

test('legacy Core does not expose native V8 advanced settings', async ({ page }) => {
  await mockConfig(page, 0)
  await expect(page.getByRole('tab', { name: 'Core 高级配置', exact: true })).toHaveCount(0)
})

test('V8 visual settings keep provider groups separate from client keys and preserve unknown fields', async ({ page }) => {
  const writes = await mockConfig(page)
  await expect(page.locator('#cpa-config-api-keys')).toHaveValue('')
  await page.locator('#cpa-config-api-keys').fill('new-client')
  await page.locator('#cpa-config-port').fill('18320')
  await page.getByRole('tab', { name: '运行与日志', exact: true }).click()
  await expect(page.locator('#cpa-config-request-retry')).toHaveValue('0')
  await expect(page.getByText('启用 Gemini CLI 入口', { exact: true })).toHaveCount(0)
  await page.locator('.cpa-config-switch').filter({ hasText: '调试模式' }).getByRole('switch').click()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  const saved = parse(writes[0].content)
  expect(writes[0]).toMatchObject({ api_version: 8, revision: `"${'a'.repeat(64)}"` })
  expect(saved.server.port).toBe(18320)
  expect(saved.access['api-keys']).toEqual(['new-client'])
  expect(saved['api-keys']).toEqual(parse(nativeYAML)['api-keys'])
  expect(saved.plugins).toEqual(parse(nativeYAML).plugins)
  expect(saved.observability.logs.debug).toBe(true)
  expect(saved).not.toHaveProperty('port')
  expect(saved).not.toHaveProperty('debug')
  expect(writes[0].content).toContain('# retained comment')
  await page.screenshot({ path: 'test-results/cpa-config-v8.png', fullPage: true })
})

test('V8 conflict keeps the draft and never replaces its revision automatically', async ({ page }) => {
  const writes = await mockConfig(page, 8, true)
  await page.locator('#cpa-config-port').fill('18320')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect(page.getByText('CPA 配置已被修改，请重新加载并核对后再保存', { exact: true })).toBeVisible()
  await expect(page.locator('#cpa-config-port')).toHaveValue('18320')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0].revision).toBe(`"${'a'.repeat(64)}"`)
})

test('legacy Core keeps its original field paths during staged upgrades', async ({ page }) => {
  const writes = await mockConfig(page, 0)
  await expect(page.locator('#cpa-config-api-keys')).toHaveValue('legacy-client')
  await page.locator('#cpa-config-port').fill('18320')
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await expect.poll(() => writes.length).toBe(1)
  const saved = parse(writes[0].content)
  expect(saved.port).toBe(18320)
  expect(saved['api-keys']).toEqual(['legacy-client'])
  expect(saved).not.toHaveProperty('server')
})

test('V8 configuration path mapping separates shared Codex, OAuth headers and payload', () => {
  for (const [key, expected] of Object.entries({
    'codex-identity-confuse': 'upstream.codex.identity-confuse',
    'codex-retry-action': 'upstream.codex.abnormal-reasoning-retry.action',
    'codex-user-agent': 'oauth.providers.codex.header-defaults.user-agent',
    'claude-user-agent': 'upstream.claude.header-defaults.user-agent',
    'quota-antigravity': 'oauth.providers.antigravity.antigravity-credits',
    'payload-override': 'requests.payload.override',
    'api-keys': 'access.api-keys',
    'rm-secret-key': 'management.secret-key',
  })) {
    const field = cpaConfigFields.find(item => item.key === key)!
    expect(cpaConfigPath(field, 8).join('.')).toBe(expected)
    expect(cpaConfigPath(field, 0)).toEqual(field.path)
  }
})

test('V8 upstream editing preserves untouched empty overrides and refreshes the revision after saving', async ({ page }) => {
  await mockConfig(page)
  let revision = `"${'a'.repeat(64)}"`
  let items = [{ 'api-key': 'synthetic-key', 'base-url': 'https://example.invalid/v1', priority: 3,
    websockets: false, 'disable-cooling': false, models: [], headers: {}, 'excluded-models': [],
    'connection-pool': { enabled: false, 'max-idle-conns-per-host': 2 },
    _cpa_v8: { group: 0, key: 0 },
  }]
  const writes: { items: typeof items, revision: string, api_version: number }[] = []
  await page.route('**/api/upstreams', route => route.fulfill({ json: {
    sections: { 'codex-api-key': items }, api_version: 8, revision,
  } }))
  await page.route('**/api/upstreams/codex-api-key', route => {
    writes.push(route.request().postDataJSON())
    items = writes.at(-1)!.items
    revision = `"${'b'.repeat(64)}"`
    return route.fulfill({ json: { status: 'updated' } })
  })
  await page.goto('/admin/upstreams')
  await page.getByRole('tab', { name: /^Codex/ }).click()
  await page.getByRole('button', { name: '打开 配置 #1 的操作菜单', exact: true }).click()
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  const editor = page.locator('[data-slot="sheet-content"]')
  await editor.locator('#upstream-priority').fill('8')
  expect(JSON.parse(await editor.locator('#upstream-connection-pool').inputValue())).toEqual({ enabled: false, 'max-idle-conns-per-host': 2 })
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor).toBeHidden()
  expect(writes).toHaveLength(1)
  expect(writes[0]).toMatchObject({ api_version: 8, revision: `"${'a'.repeat(64)}"` })
  expect(writes[0].items[0]).toMatchObject({
    priority: 8, websockets: false, 'disable-cooling': false, models: [], headers: {}, 'excluded-models': [],
    'connection-pool': { enabled: false, 'max-idle-conns-per-host': 2 },
    _cpa_v8: { group: 0, key: 0 },
  })
  await page.getByRole('switch', { name: '切换上游状态', exact: true }).click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1].revision).toBe(`"${'b'.repeat(64)}"`)
})

for (const language of ['zh', 'en']) {
  test(`all upstream provider tabs stay on one fully visible row at desktop and mobile widths (${language})`, async ({ page }) => {
    await mockConfig(page, 8, false, undefined, language)
    await page.route('**/api/upstreams', route => route.fulfill({ json: {
      sections: {}, api_version: 8, revision: `"${'a'.repeat(64)}"`,
    } }))
    await page.goto('/admin/upstreams')
    const tabs = page.getByRole('tablist', { name: language === 'zh' ? '提供商' : 'Providers', exact: true })
    const providers = tabs.getByRole('tab')
    await expect(providers).toHaveCount(8)

    for (const width of [2574, 1920, 1440, 1024, 768, 390]) {
      await page.setViewportSize({ width, height: 1000 })
      const layout = await tabs.evaluate(list => {
        const container = list.parentElement!
        const listBox = list.getBoundingClientRect()
        return {
          listHeight: listBox.height,
          containerHeight: container.clientHeight,
          contentHeight: container.scrollHeight,
          containerWidth: container.clientWidth,
          contentWidth: container.scrollWidth,
          pageWidth: document.documentElement.clientWidth,
          pageContentWidth: document.documentElement.scrollWidth,
          providers: [...list.querySelectorAll('[role="tab"]')].map(tab => {
            const box = tab.getBoundingClientRect()
            const label = tab.querySelector('.provider-tabs__label')!
            return { top: box.top - listBox.top, bottom: box.bottom - listBox.top,
              height: box.height, labelWidth: label.clientWidth, labelContentWidth: label.scrollWidth }
          }),
        }
      })
      expect(layout.contentHeight, `vertical clipping at ${width}px`).toBeLessThanOrEqual(layout.containerHeight + 1)
      expect(layout.pageContentWidth, `page overflow at ${width}px`).toBeLessThanOrEqual(layout.pageWidth + 1)
      for (const provider of layout.providers) {
        expect(provider.top, `second provider row at ${width}px`).toBeCloseTo(layout.providers[0]!.top, 0)
        expect(provider.height).toBeGreaterThanOrEqual(28)
        expect(provider.bottom).toBeLessThanOrEqual(layout.listHeight + 1)
        expect(provider.labelContentWidth).toBeLessThanOrEqual(provider.labelWidth + 1)
      }
      if (width >= 1920) expect(layout.contentWidth).toBeLessThanOrEqual(layout.containerWidth + 1)
      if (width === 390) expect(layout.contentWidth).toBeGreaterThan(layout.containerWidth)
      for (const provider of await providers.all()) {
        await provider.click()
        await expect(provider).toHaveAttribute('aria-selected', 'true')
      }
      await providers.first().focus()
      await providers.first().press('End')
      await expect(providers.last()).toBeFocused()
      await expect(providers.last()).toHaveAttribute('aria-selected', 'true')
      const lastVisible = await providers.last().evaluate(tab => {
        const box = tab.getBoundingClientRect()
        const viewport = tab.parentElement!.parentElement!.getBoundingClientRect()
        return box.left >= viewport.left - 1 && box.right <= viewport.right + 1
      })
      expect(lastVisible, `last provider unreachable at ${width}px`).toBe(true)
      if (width === 1920 || width === 390) {
        await page.screenshot({ path: `test-results/upstream-provider-tabs-${language}-${width}.png`, fullPage: true, animations: 'disabled' })
      }
    }
  })
}
