import { createHash, generateKeyPairSync, randomUUID, sign } from 'node:crypto'
import { createServer, type Server } from 'node:http'
import { expect, test, type Page } from '@playwright/test'

async function admin(page: Page) {
  await page.addInitScript(() => localStorage.setItem('cpa-helper-language', 'zh'))
  await page.request.post('/api/auth/setup', { data: { username: 'admin', password: 'test-password', nickname: 'Admin' } })
  expect((await page.request.post('/api/auth/login', { data: { username: 'admin', password: 'test-password' } })).ok()).toBeTruthy()
}

async function provider(username: string) {
  const { privateKey, publicKey } = generateKeyPairSync('rsa', { modulusLength: 2048 })
  const codes = new Map<string, { nonce: string, challenge: string }>()
  let origin = ''
  const server: Server = createServer(async (request, response) => {
    const url = new URL(request.url ?? '/', origin)
    const json = (data: unknown) => {
      response.setHeader('Content-Type', 'application/json')
      response.end(JSON.stringify(data))
    }
    if (url.pathname === '/.well-known/openid-configuration') {
      return json({ issuer: origin, authorization_endpoint: origin + '/authorize', token_endpoint: origin + '/token',
        jwks_uri: origin + '/keys', end_session_endpoint: origin + '/logout', id_token_signing_alg_values_supported: ['RS256'] })
    }
    if (url.pathname === '/keys') return json({ keys: [{ ...publicKey.export({ format: 'jwk' }), kid: 'test', alg: 'RS256', use: 'sig' }] })
    if (url.pathname === '/authorize') {
      const code = randomUUID()
      codes.set(code, { nonce: url.searchParams.get('nonce') ?? '', challenge: url.searchParams.get('code_challenge') ?? '' })
      const callback = new URL(url.searchParams.get('redirect_uri')!)
      callback.searchParams.set('code', code)
      callback.searchParams.set('state', url.searchParams.get('state')!)
      response.writeHead(302, { Location: callback.toString() }).end()
      return
    }
    if (url.pathname === '/token') {
      const chunks: Buffer[] = []
      for await (const chunk of request) chunks.push(Buffer.from(chunk))
      const form = new URLSearchParams(Buffer.concat(chunks).toString())
      const state = codes.get(form.get('code') ?? '')
      codes.delete(form.get('code') ?? '')
      if (!state || createHash('sha256').update(form.get('code_verifier') ?? '').digest('base64url') !== state.challenge) {
        response.writeHead(400).end()
        return
      }
      const head = Buffer.from(JSON.stringify({ alg: 'RS256', kid: 'test' })).toString('base64url')
      const payload = Buffer.from(JSON.stringify({
        iss: origin, aud: 'helper-e2e', sub: 'member-subject', preferred_username: username,
        name: 'SSO browser member', email: 'sso@example.test', picture: '',
        exp: Math.floor(Date.now() / 1000) + 300, nonce: state.nonce,
      })).toString('base64url')
      const unsigned = head + '.' + payload
      return json({ access_token: 'test-access', token_type: 'Bearer',
        id_token: unsigned + '.' + sign('RSA-SHA256', Buffer.from(unsigned), privateKey).toString('base64url') })
    }
    if (url.pathname === '/logout') {
      const callback = new URL(url.searchParams.get('post_logout_redirect_uri')!)
      callback.searchParams.set('state', url.searchParams.get('state')!)
      response.writeHead(302, { Location: callback.toString() }).end()
      return
    }
    response.writeHead(404).end()
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('No provider address')
  origin = 'http://127.0.0.1:' + address.port
  return { origin, close: () => new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())) }
}

test('OIDC sign-in, forced redirect, local bypass, profile and logout work in the browser', async ({ page, baseURL }) => {
  await admin(page)
  const original = await (await page.request.get('/api/settings')).json()
  const identityProvider = await provider('oidc-browser-' + Date.now())
  try {
    const settings = await page.request.put('/api/settings', { data: {
      cas_enabled: true, cas_base_url: 'https://cas.example.test/cas/app', cas_public_url: baseURL,
      sso_default_login: 'oidc', oidc_enabled: true, oidc_issuer: identityProvider.origin,
      oidc_client_id: 'helper-e2e', oidc_client_secret: 'fake-secret', oidc_public_url: baseURL,
      oidc_username_claim: 'preferred_username', oidc_auto_create_users: true,
    } })
    expect(settings.ok()).toBeTruthy()
    const settingsJSON = await settings.json()
    expect(settingsJSON.oidc_client_secret).toBeUndefined()
    expect(settingsJSON.oidc_client_secret_set).toBe(true)
    await page.goto('/admin/settings')
    await page.getByRole('tab', { name: '认证配置', exact: true }).click()
    await expect(page.getByLabel('强制登录方式')).toContainText('OIDC')
    await expect(page.getByLabel('Client Secret', { exact: true })).toHaveValue('')
    await expect(page.getByLabel('登录回调地址', { exact: true })).toHaveValue(baseURL + '/oidc/callback')
    await expect(page.getByText('默认使用 CAS 登录', { exact: true })).toHaveCount(0)
    await page.screenshot({ path: 'test-results/oidc-settings.png', fullPage: true })

    await page.context().clearCookies()
    await page.goto('/login?skipsso=true')
    await expect(page.getByLabel('账号', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: '使用 OIDC 登录' })).toBeVisible()
    await expect(page.getByRole('link', { name: '使用 CAS 登录' })).toBeVisible()
    await page.screenshot({ path: 'test-results/oidc-login.png', fullPage: true })

    await page.goto('/login?redirect=%2Faccount%2Fprofile')
    await expect(page).toHaveURL(/\/account\/profile$/)
    await expect(page.getByText('OIDC 已绑定', { exact: true })).toBeVisible()
    const me = await (await page.request.get('/api/auth/me')).json()
    expect(me.oidc_bound).toBe(true)
    expect(me.cas_bound).toBe(false)
    expect(me.can_change_password).toBe(false)
    expect(me.is_admin).toBe(false)
    // Exercise the same logout API/redirect followed by the sidebar.
    const logout = await (await page.request.post('/api/auth/logout')).json()
    expect(logout.logout_url).toContain(identityProvider.origin + '/logout')
    expect(logout.cas_logout_url).toBeUndefined()
    await page.goto(logout.logout_url)
    await expect(page).toHaveURL(/\/login\?skipsso=true$/)
    await expect(page.getByLabel('账号', { exact: true })).toBeVisible()
  } finally {
    await admin(page)
    expect((await page.request.put('/api/settings', { data: {
      ...original, sso_default_login: original.sso_default_login || '', oidc_enabled: false, oidc_client_secret: '',
    } })).ok()).toBeTruthy()
    await identityProvider.close()
  }
})

test('CAS automatic redirect still works without flashing the local login form', async ({ page }) => {
  await page.route('**/api/auth/setup', route => route.fulfill({ json: {
    setup_required: false, cas_enabled: true, cas_default_login: true, oidc_enabled: true, sso_default_login: 'cas',
  } }))
  await page.route('**/cas/login?**', route => route.fulfill({ contentType: 'text/html', body: '<h1>CAS redirect target</h1>' }))
  await page.goto('/login?redirect=%2Faccount%2Fquota')
  await expect(page).toHaveURL(/\/cas\/login\?returnTo=%2Faccount%2Fquota$/)
  await expect(page.getByRole('heading', { name: 'CAS redirect target' })).toBeVisible()
  await expect(page.getByLabel('账号', { exact: true })).toHaveCount(0)
})
