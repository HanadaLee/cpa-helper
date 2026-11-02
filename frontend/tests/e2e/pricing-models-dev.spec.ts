import { createServer } from 'node:http'
import { expect, test } from '@playwright/test'

test('models.dev synchronization imports native prices and preserves manual pricing', async ({ page }) => {
  const suffix = Date.now()
  const syncedModel = `gpt-models-dev-${suffix}`
  const manualModel = `gpt-manual-${suffix}`
  const catalog = {
    openai: { models: {
      [syncedModel]: { cost: { input: 2.5, output: 15, cache_read: 0.25, tiers: [
        { input: 5, output: 22.5, cache_read: 0.5, tier: { type: 'context', size: 272000 } },
      ] } },
      [manualModel]: { cost: { input: 1, output: 2 } },
    } },
    reseller: { models: { [syncedModel]: { cost: { input: 0.1, output: 0.2 } } } },
  }
  const source = createServer((_, response) => {
    response.setHeader('Content-Type', 'application/json')
    response.end(JSON.stringify(catalog))
  })
  await new Promise<void>(resolve => source.listen(0, '127.0.0.1', resolve))
  const address = source.address()
  if (!address || typeof address === 'string') throw new Error('No fixture server address')
  try {
    await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
    await page.request.post('/api/auth/setup', { data: { username: 'admin', password: 'test-password', nickname: 'Admin' } })
    expect((await page.request.post('/api/auth/login', { data: { username: 'admin', password: 'test-password' } })).ok()).toBeTruthy()
    expect((await page.request.post('/api/model-prices', { data: {
      provider: 'custom', model: manualModel, input_usd_per_million: 9, output_usd_per_million: 18,
    } })).ok()).toBeTruthy()
    await page.route('**/api/model-prices/sync/models-dev', route => route.continue({
      postData: JSON.stringify({ source_url: `http://127.0.0.1:${address.port}/api.json` }),
      headers: { ...route.request().headers(), 'content-type': 'application/json' },
    }))
    await page.goto('/admin/pricing')
    await expect(page.getByRole('button', { name: '同步 models.dev', exact: true })).toBeVisible()
    await expect(page.getByText(/LiteLLM/)).toHaveCount(0)
    await page.getByRole('button', { name: '代理配置', exact: true }).click()
    const proxy = page.getByRole('dialog', { name: 'models.dev 代理配置', exact: true })
    await expect(proxy).toContainText('价格数据来自 models.dev')
    await proxy.getByRole('button', { name: '保存', exact: true }).click()
    const synced = page.waitForResponse(response => response.url().endsWith('/api/model-prices/sync/models-dev'))
    await page.getByRole('button', { name: '同步 models.dev', exact: true }).click()
    const response = await synced
    expect(response.ok()).toBeTruthy()
    expect(await response.json()).toMatchObject({ imported: 1, skipped_manual: 1, skipped_duplicates: 1, total_entries: 3 })
    await page.getByPlaceholder('搜索模型或服务商').fill(syncedModel)
    const row = page.getByRole('row').filter({ hasText: syncedModel })
    await expect(row).toHaveCount(1)
    await expect(row).toContainText('models.dev')
    await expect(row).toContainText('openai')
    await expect(row).toContainText('长上下文')
    const prices = await (await page.request.get('/api/model-prices')).json()
    expect(prices.find((price: { model: string }) => price.model === syncedModel)).toMatchObject({
      source: 'models.dev', source_model: `openai/${syncedModel}`, input_usd_per_million: 2.5,
      output_usd_per_million: 15, cache_read_usd_per_million: 0.25, long_context_threshold_tokens: 272000,
    })
    expect(prices.find((price: { model: string }) => price.model === manualModel)).toMatchObject({ source: 'manual', input_usd_per_million: 9 })
    await page.screenshot({ path: 'test-results/models-dev-pricing.png', fullPage: true })
  } finally {
    await new Promise<void>((resolve, reject) => source.close(error => error ? reject(error) : resolve()))
  }
})
