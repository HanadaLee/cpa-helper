import { expect, test, type Page } from '@playwright/test'
import { selectExpiry } from './helpers/date-time'

async function login(page: Page, username = 'admin', password = 'test-password') {
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
  await page.request.post('/api/auth/setup', {
    data: { username: 'admin', password: 'test-password', nickname: 'Admin' },
  })
  const response = await page.request.post('/api/auth/login', { data: { username, password } })
  expect(response.ok()).toBeTruthy()
}

async function member(page: Page, suffix: string) {
  const username = `quota-${suffix}-${Date.now()}`
  const response = await page.request.post('/api/users', {
    data: { username, password: 'member-password', nickname: username },
  })
  expect(response.ok()).toBeTruthy()
  const user = await response.json()
  expect(
    (
      await page.request.put(`/api/users/${user.id}/quota`, {
        data: { daily_quota_usd: 2, weekly_quota_usd: 5 },
      })
    ).ok(),
  ).toBeTruthy()
  return { id: user.id as number, username }
}

test('new users stay blocked by an enabled zero weekly quota and can be changed to weekly-only limits', async ({ page }) => {
  await login(page)
  await page.goto('/admin/users')
  await page.getByRole('button', { name: '增加用户', exact: true }).click()
  const creator = page.getByRole('dialog', { name: '增加用户', exact: true })
  await expect(creator.getByRole('switch', { name: '启用周配额', exact: true })).toBeChecked()
  await expect(creator.getByLabel('周配额 USD', { exact: true })).toHaveValue('0')
  await creator.getByRole('button', { name: '取消', exact: true }).click()
  const username = `weekly-switch-${Date.now()}`
  const created = await page.request.post('/api/users', { data: { username, password: 'member-password', nickname: username } })
  expect(created.ok()).toBeTruthy()
  const user = await created.json()
  expect(user.quota).toMatchObject({ unlimited: false, weekly_quota_usd: 0, available_usd: 0, can_create_keys: false })
  await page.reload()
  await page.getByRole('combobox', { name: '每页数量', exact: true }).click()
  await page.getByRole('option', { name: '100', exact: true }).click()
  await page.getByRole('button', { name: `编辑 ${username}`, exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑用户', exact: true })
  await editor.getByRole('switch', { name: '启用日配额', exact: true }).uncheck()
  await expect(editor.getByLabel('日配额 USD', { exact: true })).toBeDisabled()
  await editor.getByLabel('周配额 USD', { exact: true }).fill('12')
  await page.screenshot({ path: 'test-results/quota-weekly-only-editor.png', fullPage: true, animations: 'disabled' })
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor).toBeHidden()
  const updated = (await (await page.request.get('/api/users')).json()).find((item: { id: number }) => item.id === user.id)
  expect(updated.quota).toMatchObject({ unlimited: false, daily_quota_usd: null, weekly_quota_usd: 12, available_usd: 12, can_create_keys: true })
  expect((await page.request.post('/api/quota/cards/issue', { data: { user_ids: [user.id], kind: 'credit', amount_usd: 3, name: 'Weekly credit' } })).ok()).toBeTruthy()
  await login(page, username, 'member-password')
  await page.goto('/account/usage')
  const badge = page.getByTestId('usage-balance')
  await expect(badge).toHaveText('可用余额 $15.00')
  await expect(badge).toHaveAttribute('title', /日配额 不限制 \/ 周配额 \$12.00 \/ 额度卡 \$3.00/)
  await page.goto('/account/quota')
  await expect(page.getByRole('progressbar', { name: '日配额', exact: true })).toHaveAttribute('aria-valuetext', '剩余 无限制')
  await expect(page.getByRole('progressbar', { name: '周配额', exact: true })).toHaveAttribute('aria-valuetext', '剩余 $12.00 / $12.00')
})

test('quota period switches round trip independently and unlimited requires both off', async ({ page }) => {
  await login(page)
  const user = await member(page, 'daily-switch')
  expect((await page.request.put(`/api/users/${user.id}/quota`, { data: { daily_quota_usd: 5, weekly_quota_usd: null } })).ok()).toBeTruthy()
  await page.goto('/admin/users')
  await page.getByRole('combobox', { name: '每页数量', exact: true }).click()
  await page.getByRole('option', { name: '100', exact: true }).click()
  await page.getByRole('button', { name: `编辑 ${user.username}`, exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑用户', exact: true })
  await expect(editor.getByRole('switch', { name: '启用日配额', exact: true })).toBeChecked()
  await expect(editor.getByRole('switch', { name: '启用周配额', exact: true })).not.toBeChecked()
  await expect(editor.getByLabel('周配额 USD', { exact: true })).toBeDisabled()
  await editor.getByRole('switch', { name: '启用日配额', exact: true }).uncheck()
  await expect(editor.getByText('日配额与周配额均已关闭，此用户不限制用量且不消耗额度卡。', { exact: true })).toBeVisible()
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor).toBeHidden()
  const updated = (await (await page.request.get('/api/users')).json()).find((item: { id: number }) => item.id === user.id)
  expect(updated.quota).toMatchObject({ unlimited: true, daily_quota_usd: null, weekly_quota_usd: null })
})

test('my usage shares its balance icon and status styling across administrator and member roles', async ({ page }) => {
  await login(page)
  const user = await member(page, 'balance-style')
  const quota = (await (await page.request.get('/api/users')).json()).find((item: { id: number }) => item.id === user.id).quota
  let warning = false
  await page.route('**/api/account/quota', route => route.fulfill({ json: { ...quota, unpriced_records: warning ? 1 : 0 } }))
  await page.goto('/account/usage')
  const balance = page.getByTestId('usage-balance')
  await expect(balance).toHaveText('可用余额 $2.00')
  const adminIcon = await balance.locator('svg').innerHTML()
  const adminColor = await balance.evaluate(element => getComputedStyle(element).color)
  await expect(balance).not.toHaveClass(/is-warning/)
  await login(page, user.username, 'member-password')
  await page.goto('/account/usage')
  await expect(balance).toHaveText('可用余额 $2.00')
  expect(await balance.locator('svg').innerHTML()).toBe(adminIcon)
  expect(await balance.evaluate(element => getComputedStyle(element).color)).toBe(adminColor)
  warning = true
  await page.reload()
  await expect(balance).toHaveClass(/is-warning/)
  await expect(balance).toHaveAttribute('title', /历史未定价 1 条/)
  expect(await balance.locator('svg').innerHTML()).toBe(adminIcon)
  expect(await balance.evaluate(element => getComputedStyle(element).color)).not.toBe(adminColor)
})

test('administrator issues, edits and revokes cards in bulk and resets quotas', async ({
  page,
}) => {
  await login(page)
  const first = await member(page, 'first')
  const second = await member(page, 'second')
  await page.goto('/admin/users')
  await page.getByRole('combobox', { name: '每页数量', exact: true }).click()
  await page.getByRole('option', { name: '100', exact: true }).click()
  await page.getByRole('checkbox', { name: `选择 ${first.username}`, exact: true }).check()
  await page.getByRole('checkbox', { name: `选择 ${second.username}`, exact: true }).check()
  await page.getByRole('button', { name: '发放卡片', exact: true }).click()
  const manager = page.getByRole('dialog', { name: '配额卡管理', exact: true })
  await manager.getByRole('button', { name: '发放卡片', exact: true }).click()
  const issuer = page.getByRole('dialog', { name: '发放卡片', exact: true })
  await expect(issuer.getByText('已选择 2 位用户')).toBeVisible()
  await issuer.getByLabel('卡片名称').fill('Bulk credit')
  await issuer.getByLabel('总额度 USD').fill('25')
  await issuer.getByRole('switch', { name: '永久有效', exact: true }).click()
  const expiresAt = await selectExpiry(page, issuer.getByLabel('到期时间（北京时间）'), '08:30')
  await issuer.getByRole('button', { name: '发放', exact: true }).click()
  await expect(page.getByText('已发放 2 张卡片')).toBeVisible()
  const result = await page.request.get(`/api/quota/cards?user_id=${first.id}`)
  const card = (await result.json()).items[0]
  expect(new Date(card.expires_at).toISOString()).toBe(new Date(expiresAt).toISOString())
  await manager.getByRole('button', { name: `编辑卡片 ${card.id}`, exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑卡片', exact: true })
  await expect(editor.getByLabel('到期时间（北京时间）')).toHaveText(expiresAt.slice(0, 16).replace('T', ' '))
  await editor.getByLabel('总额度 USD').fill('30')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('卡片已保存', { exact: true })).toBeVisible()
  await manager.getByRole('button', { name: 'Close', exact: true }).click()
  await page.getByRole('button', { name: '吊销额度卡', exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: '吊销', exact: true }).click()
  await expect(page.getByText('额度卡已吊销', { exact: true })).toBeVisible()
  for (const user of [first, second]) {
    const cards = await (await page.request.get(`/api/quota/cards?user_id=${user.id}`)).json()
    expect(cards.items[0].status).toBe('revoked')
  }
  await page.getByRole('button', { name: '重置额度', exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: '重置', exact: true }).click()
  await expect(page.getByText('额度已重置', { exact: true })).toBeVisible()
  await page.screenshot({ path: 'test-results/quota-admin.png', fullPage: true })
})

test('card revocation confirmation stays above the user management dialog', async ({ page }) => {
  await login(page)
  const user = await member(page, 'nested-dialog')
  for (const kind of ['reset', 'credit']) {
    const issued = await page.request.post('/api/quota/cards/issue', {
      data: { user_ids: [user.id], kind, name: `Nested ${kind}`, amount_usd: 10 },
    })
    expect(issued.ok()).toBeTruthy()
  }
  const cards = (await (await page.request.get(`/api/quota/cards?user_id=${user.id}`)).json()).items as Array<{ id: number; kind: string; status: string }>
  await page.goto('/admin/users')
  await page.getByRole('combobox', { name: '每页数量', exact: true }).click()
  await page.getByRole('option', { name: '100', exact: true }).click()
  await page.getByRole('button', { name: `管理 ${user.username} 的额度卡`, exact: true }).click()
  const manager = page.getByRole('dialog', { name: '配额卡管理', exact: true })
  for (const kind of ['reset', 'credit']) {
    const card = cards.find((item) => item.kind === kind)!
    await manager.getByRole('tab', { name: kind === 'reset' ? '重置卡' : '额度卡', exact: true }).click()
    await manager.getByRole('button', { name: `吊销卡片 ${card.id}`, exact: true }).click()
    const confirmation = page.getByRole('alertdialog', { name: '吊销卡片', exact: true })
    const revokeButton = confirmation.getByRole('button', { name: '吊销', exact: true })
    await expect(revokeButton).toBeVisible()
    await expect.poll(() => revokeButton.evaluate((element) => {
      const box = element.getBoundingClientRect()
      return element.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2))
    })).toBe(true)
    await page.screenshot({ path: `test-results/quota-revoke-${kind}.png`, fullPage: true })
    await confirmation.getByRole('button', { name: '取消', exact: true }).click()
    await expect(confirmation).toHaveCount(0)
    await expect(manager).toBeVisible()
    await manager.getByRole('button', { name: `吊销卡片 ${card.id}`, exact: true }).click()
    await revokeButton.click()
    await expect(manager.getByText('已失效', { exact: true })).toBeVisible()
    const updated = (await (await page.request.get(`/api/quota/cards?user_id=${user.id}`)).json()).items as typeof cards
    expect(updated.find((item) => item.id === card.id)?.status).toBe('revoked')
  }
  await manager.getByRole('button', { name: 'Close', exact: true }).click()
  await expect(manager).toHaveCount(0)
})

test('my quota exposes card credit and single-use reset cards without admin operations', async ({
  page,
}) => {
  await login(page)
  const user = await member(page, 'account')
  expect(
    (
      await page.request.post('/api/quota/cards/issue', {
        data: { user_ids: [user.id], kind: 'credit', name: 'Personal credit', amount_usd: 20 },
      })
    ).ok(),
  ).toBeTruthy()
  expect(
    (
      await page.request.post('/api/quota/cards/issue', {
        data: { user_ids: [user.id], kind: 'reset', name: 'Personal reset' },
      })
    ).ok(),
  ).toBeTruthy()
  await login(page, user.username, 'member-password')
  const chargeHistoryRequests: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/account/quota/history') {
      chargeHistoryRequests.push(request.url())
    }
  })
  await page.goto('/account/quota')
  await expect(page.getByRole('heading', { name: '我的配额', exact: true })).toBeVisible()
  await expect(page.getByText('额度管理', { exact: true })).toBeVisible()
  await expect(page.getByText('我的卡片', { exact: true })).toHaveCount(0)
  await expect(page.getByText(/用量同时计入日配额和周配额|取日、周剩余配额中的较小值|额度卡与可用配额按最早到期顺序抵扣|每日 0 点、每周一 0 点按北京时间重置/)).toHaveCount(0)
  await expect(page.getByRole('tab', { name: '扣款记录', exact: true })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: '未覆盖', exact: true })).toHaveCount(0)
  const limitsPanel = page.getByTestId('quota-limits')
  const resetHeading = limitsPanel.getByRole('heading', { name: '重置记录', exact: true })
  await expect(resetHeading).toBeVisible()
  expect(await resetHeading.evaluate((element) => element.closest('[data-slot="card"]')?.getAttribute('data-testid'))).toBe('quota-limits')
  await expect(limitsPanel.getByRole('columnheader', { name: '恢复日配额', exact: true })).toBeVisible()
  await expect(limitsPanel.locator('[data-slot="table-pagination-footer"]')).toBeVisible()
  await expect(page.getByText('$22.00', { exact: true })).toBeVisible()
  await expect(page.getByTestId('quota-limits').getByRole('progressbar')).toHaveCount(2)
  await expect(page.getByTestId('quota-limits').getByRole('progressbar', { name: '日配额', exact: true })).toHaveAttribute('aria-valuenow', '100')
  await expect(page.getByText(/Personal credit #/)).toBeVisible()
  await expect(page.getByRole('progressbar', { name: '额度卡', exact: true })).toHaveAttribute('aria-valuenow', '100')
  await expect(page.getByText('生效中', { exact: true })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '操作', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '发放卡片', exact: true })).toHaveCount(0)
  const before = await (await page.request.get('/api/account/quota')).json()
  await page.getByRole('tab', { name: '重置卡', exact: true }).click()
  await expect(page.getByRole('columnheader', { name: '操作', exact: true })).toBeVisible()
  await expect(page.getByText('未使用', { exact: true })).toBeVisible()
  await expect(page.getByText('生效中', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '使用', exact: true }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: '使用', exact: true }).click()
  await expect(page.getByText('日配额与周配额已重置', { exact: true })).toBeVisible()
  await expect(page.getByText('已使用', { exact: true })).toBeVisible()
  const after = await (await page.request.get('/api/account/quota')).json()
  expect(after.daily_resets_at).toBe(before.daily_resets_at)
  expect(after.weekly_resets_at).toBe(before.weekly_resets_at)
  expect(after.cards_remaining_usd).toBe(20)
  await expect(limitsPanel.getByText(/重置卡 #/)).toBeVisible()
  expect(chargeHistoryRequests).toHaveLength(0)
  await page.screenshot({ path: 'test-results/quota-account.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: '我的配额', exact: true })).toBeVisible()
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
  ).toBeTruthy()
  await page.screenshot({ path: 'test-results/quota-mobile.png', fullPage: true })
})
