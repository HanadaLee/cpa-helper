import type { CPAConfigFieldDefinition } from './cpaConfigFields'

// Canonical paths from CPA-Core-LTS v1-lts-0.1.2 internal/config/config_v8.go.
// Longest/specific prefixes must precede containers. Legacy is kept only for
// older Core instances during a staged rollout, never mixed into a V8 document.
const v8Paths: Record<string, string> = {
  host: 'server.host',
  port: 'server.port',
  tls: 'server.tls',
  'commercial-mode': 'server.commercial-mode',
  'remote-management': 'management',
  'api-keys': 'access.api-keys',
  'auth-dir': 'oauth.auth-dir',
  'auth-auto-refresh-workers': 'oauth.auth-auto-refresh-workers',
  'force-model-prefix': 'routing.force-model-prefix',
  'request-retry': 'routing.retry.request-retry',
  'max-retry-credentials': 'routing.retry.max-retry-credentials',
  'max-retry-interval': 'routing.retry.max-retry-interval',
  'disable-cooling': 'routing.cooldown.disable-cooling',
  'transient-error-cooldown-seconds': 'routing.cooldown.transient-error-cooldown-seconds',
  'proxy-url': 'requests.proxy-url',
  'passthrough-headers': 'requests.passthrough-headers',
  streaming: 'requests.streaming',
  'nonstream-keepalive-interval': 'requests.nonstream-keepalive-interval',
  payload: 'requests.payload',
  'disable-image-generation': 'multimedia.disable-image-generation',
  'gpt-image-2-base-model': 'multimedia.gpt-image-2-base-model',
  debug: 'observability.logs.debug',
  'request-log': 'observability.logs.request-log',
  'logging-to-file': 'observability.logs.logging-to-file',
  'logs-max-total-size-mb': 'observability.logs.logs-max-total-size-mb',
  'error-logs-max-files': 'observability.logs.error-logs-max-files',
  'usage-statistics-enabled': 'observability.usage.usage-statistics-enabled',
  'redis-usage-queue-retention-seconds': 'observability.usage.redis-usage-queue-retention-seconds',
  'ws-auth': 'oauth.providers.aistudio.ws-auth',
  'antigravity-signature-cache-enabled': 'oauth.providers.antigravity.signature-cache-enabled',
  'antigravity-signature-bypass-strict': 'oauth.providers.antigravity.signature-bypass-strict',
  'quota-exceeded.antigravity-credits': 'oauth.providers.antigravity.antigravity-credits',
  'claude-header-defaults': 'upstream.claude.header-defaults',
  'codex-header-defaults': 'oauth.providers.codex.header-defaults',
  'codex.identity-confuse': 'upstream.codex.identity-confuse',
  'codex.abnormal-reasoning-retry': 'upstream.codex.abnormal-reasoning-retry',
}

export function cpaConfigPath(field: CPAConfigFieldDefinition, apiVersion: number): string[] {
  if (apiVersion !== 8) return field.path
  const path = field.path.join('.')
  for (const [legacy, current] of Object.entries(v8Paths)) {
    if (path === legacy || path.startsWith(`${legacy}.`)) {
      return (current + path.slice(legacy.length)).split('.')
    }
  }
  return field.path
}
