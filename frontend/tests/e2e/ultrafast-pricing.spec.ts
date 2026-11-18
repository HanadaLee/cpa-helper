import { expect, test } from '@playwright/test'

test('ULTRAFAST uses independent controls and appears in administrator and member rate tiers', async ({ page }) => {
  let price = { id: 1, provider: 'openai', model: 'gpt-ultra-test',
    input_usd_per_million: 2, output_usd_per_million: 4, cache_read_usd_per_million: 1, cache_creation_usd_per_million: 2,
    request_usd: null, fast_enabled: true, fast_multiplier: 2,
    ultrafast_enabled: true, ultrafast_multiplier: 6, long_context_ultrafast_unsupported: false,
    long_context_enabled: true, long_context_threshold_tokens: 100,
    long_context_input_usd_per_million: 4, long_context_output_usd_per_million: 8,
    long_context_cache_read_usd_per_million: 2, long_context_cache_creation_usd_per_million: 4,
    long_context_fast_unsupported: false, off_peak_enabled: true,
    peak_input_usd_per_million: 3, peak_output_usd_per_million: 6,
    peak_cache_read_usd_per_million: 1.5, peak_cache_creation_usd_per_million: 3, peak_request_usd: null,
    long_context_peak_input_usd_per_million: 6, long_context_peak_output_usd_per_million: 12,
    long_context_peak_cache_read_usd_per_million: 3, long_context_peak_cache_creation_usd_per_million: 6,
    billing_unit: 'token', source: 'manual', auto_synced: false, updated_at: '2026-11-18T00:00:00+08:00',
  }
  const writes: Record<string, unknown>[] = []
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/auth/me') return route.fulfill({ json: { id: 1, username: 'admin', nickname: 'Admin', is_admin: true,
      can_view_usage_history: true, can_change_password: true, must_change_password: false } })
    if (path === '/api/branding') return route.fulfill({ json: { brand_name_zh: 'Gateway', brand_name_en: 'Gateway' } })
    if (path === '/api/model-prices/1') {
      const payload = route.request().postDataJSON()
      writes.push(payload)
      price = { ...price, ...payload }
      return route.fulfill({ json: price })
    }
    if (path === '/api/model-prices') return route.fulfill({ json: [price] })
    if (path === '/api/model-prices/catalog') return route.fulfill({ json: { models: [], errors: [] } })
    if (path === '/api/account/models') return route.fulfill({ json: { has_api_keys: true, api_key_count: 1,
      queryable_api_key_count: 1, errors: [], models: [{ id: price.model, name: price.model, sources: [], metadata: {}, price }] } })
    return route.fulfill({ json: {} })
  })
  await page.goto('/admin/pricing')
  const row = page.getByRole('row').filter({ hasText: price.model })
  await expect(row.locator('.rate-tier-stack').first().locator('.rate-tier-line')).toHaveCount(12)
  await expect(row).toContainText('ULTRAFAST')
  await expect(row).not.toContainText('×6')
  await page.getByRole('button', { name: `打开 ${price.model} 的操作菜单`, exact: true }).click()
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑价格', exact: true })
  await expect(editor.locator('#price-ultrafast-multiplier')).toHaveValue('6')
  await editor.locator('#price-fast-enabled').uncheck()
  await expect(editor.locator('#price-ultrafast-multiplier')).toBeVisible()
  await editor.locator('#price-ultrafast-multiplier').fill('7')
  await editor.locator('#price-long-context-ultrafast-unsupported').uncheck()
  await page.screenshot({ path: 'test-results/ultrafast-price-editor.png', fullPage: true, animations: 'disabled' })
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor).toBeHidden()
  expect(writes[0]).toMatchObject({ ultrafast_enabled: true, ultrafast_multiplier: 7, fast_enabled: false,
    fast_multiplier: 1, long_context_ultrafast_unsupported: true })
  await expect(row.locator('.rate-tier-stack').first().locator('.rate-tier-line')).toHaveCount(6)
  await page.goto('/account/models')
  const member = page.getByRole('row').filter({ hasText: price.model })
  await expect(member.locator('.rate-tier-stack').first().locator('.rate-tier-line')).toHaveCount(6)
  await expect(member).toContainText('常规 · 空闲 · ULTRAFAST')
  await expect(member).toContainText('长上下文 · 空闲 · 无 ULTRAFAST')
  await page.screenshot({ path: 'test-results/ultrafast-model-tiers.png', fullPage: true, animations: 'disabled' })
})
