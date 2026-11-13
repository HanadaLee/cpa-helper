export type CPAConfigFieldKind = 'boolean' | 'string' | 'number' | 'select' | 'string-list' | 'integer-list' | 'yaml' | 'yaml-object'

export interface CPAConfigOption {
  value: string
  labelZH: string
  labelEN: string
}

export interface CPAConfigFieldDefinition {
  key: string
  path: string[]
  kind: CPAConfigFieldKind
  labelZH: string
  labelEN: string
  descriptionZH?: string
  descriptionEN?: string
  placeholder?: string
  defaultValue?: string | boolean
  options?: CPAConfigOption[]
  wide?: boolean
  secret?: boolean
}

export interface CPAConfigSectionDefinition {
  v8Only?: boolean
  key: string
  labelZH: string
  labelEN: string
  descriptionZH: string
  descriptionEN: string
  fields: CPAConfigFieldDefinition[]
}

const booleanField = (
  key: string,
  path: string[],
  labelZH: string,
  labelEN: string,
  descriptionZH: string,
  descriptionEN: string,
  defaultValue = false,
): CPAConfigFieldDefinition => ({
  key,
  path,
  kind: 'boolean',
  labelZH,
  labelEN,
  descriptionZH,
  descriptionEN,
  defaultValue,
  wide: true,
})

const routingOptions: CPAConfigOption[] = [
  { value: 'round-robin', labelZH: '轮询', labelEN: 'Round robin' },
  { value: 'weighted-round-robin', labelZH: '加权轮询', labelEN: 'Weighted round robin' },
  { value: 'fill-first', labelZH: '优先填满', labelEN: 'Fill first' },
]

export const cpaConfigSections: CPAConfigSectionDefinition[] = [
  {
    key: 'server',
    labelZH: '服务与访问',
    labelEN: 'Server & Access',
    descriptionZH: '管理监听地址、TLS、认证目录、管理面板与客户端 API 密钥。',
    descriptionEN: 'Manage listener, TLS, auth storage, remote management, and client API keys.',
    fields: [
      { key: 'host', path: ['host'], kind: 'string', labelZH: '监听地址', labelEN: 'Listen host', placeholder: '0.0.0.0' },
      { key: 'port', path: ['port'], kind: 'number', labelZH: '监听端口', labelEN: 'Listen port', placeholder: '8317' },
      booleanField('tls-enable', ['tls', 'enable'], '启用 TLS', 'Enable TLS', '由 CPA 直接提供 HTTPS。', 'Serve HTTPS directly from CPA.'),
      { key: 'tls-cert', path: ['tls', 'cert'], kind: 'string', labelZH: 'TLS 证书文件', labelEN: 'TLS certificate file' },
      { key: 'tls-key', path: ['tls', 'key'], kind: 'string', labelZH: 'TLS 私钥文件', labelEN: 'TLS private key file' },
      { key: 'auth-dir', path: ['auth-dir'], kind: 'string', labelZH: '认证文件目录', labelEN: 'Auth directory' },
      booleanField('rm-allow-remote', ['remote-management', 'allow-remote'], '允许远程管理', 'Allow remote management', '允许非本机地址访问管理 API。', 'Allow non-loopback access to the management API.'),
      { key: 'rm-secret-key', path: ['remote-management', 'secret-key'], kind: 'string', labelZH: '远程管理密钥', labelEN: 'Remote management secret', secret: true },
      booleanField('rm-disable-panel', ['remote-management', 'disable-control-panel'], '禁用控制面板', 'Disable control panel', '关闭 CPA 自带的管理面板。', 'Disable the built-in CPA management panel.'),
      booleanField('rm-disable-auto-update', ['remote-management', 'disable-auto-update-panel'], '禁用面板自动更新', 'Disable panel auto-update', '阻止 CPA 自动更新面板资源。', 'Prevent automatic panel asset updates.'),
      { key: 'rm-panel-repo', path: ['remote-management', 'panel-github-repository'], kind: 'string', labelZH: '面板仓库', labelEN: 'Panel repository', placeholder: 'owner/repository' },
      { key: 'api-keys', path: ['api-keys'], kind: 'string-list', labelZH: '客户端 API 密钥', labelEN: 'Client API keys', descriptionZH: '每行填写一个密钥。', descriptionEN: 'Enter one key per line.', wide: true, secret: true },
      booleanField('plugins-enabled', ['plugins', 'enabled'], '启用插件', 'Enable plugins', '启用 CPA 插件运行时。', 'Enable the CPA plugin runtime.'),
      { key: 'plugin-store-sources', path: ['plugins', 'store-sources'], kind: 'string-list', labelZH: '插件源', labelEN: 'Plugin store sources', descriptionZH: '每行填写一个插件源地址。', descriptionEN: 'Enter one plugin source URL per line.', wide: true },
      { key: 'plugin-store-auth', path: ['plugins', 'store-auth'], kind: 'yaml', labelZH: '插件源认证规则', labelEN: 'Plugin store authentication', descriptionZH: '使用 YAML 编辑认证规则数组。', descriptionEN: 'Edit the authentication rule array as YAML.', wide: true },
    ],
  },
  {
    key: 'runtime',
    labelZH: '运行与日志',
    labelEN: 'Runtime & Logging',
    descriptionZH: '配置运行模式、日志、代理、重试、冷却和协议入口。',
    descriptionEN: 'Configure runtime behavior, logging, proxying, retries, cooling, and protocol endpoints.',
    fields: [
      booleanField('debug', ['debug'], '调试模式', 'Debug mode', '输出更详细的调试日志。', 'Emit more detailed debug logs.'),
      booleanField('commercial-mode', ['commercial-mode'], '商业模式', 'Commercial mode', '启用商业版本运行模式。', 'Enable commercial runtime mode.'),
      booleanField('request-log', ['request-log'], '记录请求日志', 'Request logging', '记录代理请求的概要日志。', 'Record summary logs for proxied requests.'),
      booleanField('logging-to-file', ['logging-to-file'], '写入日志文件', 'Log to files', '将运行日志写入文件。', 'Write runtime logs to files.'),
      { key: 'logs-max-total-size', path: ['logs-max-total-size-mb'], kind: 'number', labelZH: '日志总大小上限（MB）', labelEN: 'Maximum total log size (MB)' },
      { key: 'error-logs-max-files', path: ['error-logs-max-files'], kind: 'number', labelZH: '错误日志文件上限', labelEN: 'Maximum error log files' },
      booleanField('usage-statistics', ['usage-statistics-enabled'], '启用用量统计', 'Enable usage statistics', '由 CPA 记录用量统计。', 'Let CPA collect usage statistics.'),
      { key: 'redis-queue-retention', path: ['redis-usage-queue-retention-seconds'], kind: 'number', labelZH: 'Redis 用量队列保留秒数', labelEN: 'Redis usage queue retention (seconds)' },
      { key: 'proxy-url', path: ['proxy-url'], kind: 'string', labelZH: '全局代理地址', labelEN: 'Global proxy URL', placeholder: 'http://127.0.0.1:7890' },
      booleanField('force-model-prefix', ['force-model-prefix'], '强制模型前缀', 'Force model prefix', '要求请求模型名带提供商前缀。', 'Require provider prefixes in request model names.'),
      booleanField('passthrough-headers', ['passthrough-headers'], '透传响应头', 'Pass through response headers', '将上游响应头透传给客户端。', 'Pass upstream response headers to clients.'),
      { key: 'request-retry', path: ['request-retry'], kind: 'number', labelZH: '请求重试次数', labelEN: 'Request retries' },
      { key: 'max-retry-credentials', path: ['max-retry-credentials'], kind: 'number', labelZH: '最大重试凭证数', labelEN: 'Maximum retry credentials' },
      { key: 'max-retry-interval', path: ['max-retry-interval'], kind: 'number', labelZH: '最大重试间隔（秒）', labelEN: 'Maximum retry interval (seconds)' },
      { key: 'transient-cooldown', path: ['transient-error-cooldown-seconds'], kind: 'number', labelZH: '瞬时错误冷却（秒）', labelEN: 'Transient error cooldown (seconds)' },
      booleanField('disable-cooling', ['disable-cooling'], '禁用凭证冷却', 'Disable credential cooling', '发生错误时不自动冷却凭证。', 'Do not cool credentials automatically after errors.'),
      {
        key: 'disable-image-generation',
        path: ['disable-image-generation'],
        kind: 'select',
        labelZH: '图像生成限制',
        labelEN: 'Image generation restriction',
        defaultValue: 'false',
        options: [
          { value: 'false', labelZH: '不限制', labelEN: 'Disabled' },
          { value: 'true', labelZH: '全部禁用', labelEN: 'Disable all' },
          { value: 'chat', labelZH: '仅禁用聊天入口', labelEN: 'Disable chat only' },
          { value: 'passthrough', labelZH: '仅禁用透传入口', labelEN: 'Disable passthrough only' },
        ],
      },
      { key: 'gpt-image-base-model', path: ['gpt-image-2-base-model'], kind: 'string', labelZH: 'GPT Image 2 基础模型', labelEN: 'GPT Image 2 base model' },
      { key: 'auth-auto-refresh-workers', path: ['auth-auto-refresh-workers'], kind: 'number', labelZH: '认证自动刷新并发数', labelEN: 'Auth auto-refresh workers' },
      booleanField('ws-auth', ['ws-auth'], 'WebSocket 鉴权', 'WebSocket authentication', '对 WebSocket 请求执行 API 密钥校验。', 'Require API-key authentication for WebSocket requests.'),
      booleanField('gemini-cli-endpoint', ['enable-gemini-cli-endpoint'], '启用 Gemini CLI 入口', 'Enable Gemini CLI endpoint', '开放 Gemini CLI 兼容端点。', 'Expose the Gemini CLI compatible endpoint.'),
      booleanField('antigravity-signature-cache', ['antigravity-signature-cache-enabled'], 'Antigravity 签名缓存', 'Antigravity signature cache', '缓存 Antigravity 请求签名。', 'Cache Antigravity request signatures.', true),
      booleanField('antigravity-signature-bypass', ['antigravity-signature-bypass-strict'], 'Antigravity 严格绕过', 'Antigravity strict bypass', '启用严格签名绕过策略。', 'Enable strict signature bypass behavior.'),
    ],
  },
  {
    key: 'routing',
    labelZH: '配额、路由与流式',
    labelEN: 'Quota, Routing & Streaming',
    descriptionZH: '配置配额耗尽回退、凭证路由、会话亲和与流式保活。',
    descriptionEN: 'Configure quota fallback, credential routing, session affinity, and streaming keepalive.',
    fields: [
      booleanField('quota-switch-project', ['quota-exceeded', 'switch-project'], '配额耗尽时切换项目', 'Switch project on quota exhaustion', '当前项目耗尽时尝试其他项目。', 'Try another project when the current one is exhausted.', true),
      booleanField('quota-switch-preview', ['quota-exceeded', 'switch-preview-model'], '配额耗尽时切换预览模型', 'Switch preview model on quota exhaustion', '预览模型耗尽时尝试其他模型。', 'Try another preview model after quota exhaustion.', true),
      booleanField('quota-antigravity', ['quota-exceeded', 'antigravity-credits'], '使用 Antigravity Credits', 'Use Antigravity credits', '允许回退到 Antigravity Credits。', 'Allow fallback to Antigravity credits.'),
      { key: 'routing-strategy', path: ['routing', 'strategy'], kind: 'select', labelZH: '路由策略', labelEN: 'Routing strategy', defaultValue: 'round-robin', options: routingOptions },
      booleanField('routing-session-affinity', ['routing', 'session-affinity'], '会话亲和', 'Session affinity', '相同会话优先使用同一凭证。', 'Prefer the same credential for the same session.'),
      { key: 'routing-session-ttl', path: ['routing', 'session-affinity-ttl'], kind: 'string', labelZH: '会话亲和有效期', labelEN: 'Session affinity TTL', placeholder: '1h' },
      { key: 'streaming-keepalive', path: ['streaming', 'keepalive-seconds'], kind: 'number', labelZH: '流式保活间隔（秒）', labelEN: 'Streaming keepalive (seconds)' },
      { key: 'streaming-bootstrap-retries', path: ['streaming', 'bootstrap-retries'], kind: 'number', labelZH: '流式启动重试次数', labelEN: 'Streaming bootstrap retries' },
      { key: 'nonstream-keepalive', path: ['nonstream-keepalive-interval'], kind: 'number', labelZH: '非流式保活间隔（秒）', labelEN: 'Non-stream keepalive interval (seconds)' },
    ],
  },
  {
    key: 'headers',
    labelZH: '请求头默认值',
    labelEN: 'Header Defaults',
    descriptionZH: '覆盖 Claude 与 Codex 上游请求使用的客户端标识和运行环境信息。',
    descriptionEN: 'Override client identity and runtime headers sent to Claude and Codex upstreams.',
    fields: [
      { key: 'claude-user-agent', path: ['claude-header-defaults', 'user-agent'], kind: 'string', labelZH: 'Claude User-Agent', labelEN: 'Claude User-Agent' },
      { key: 'claude-package-version', path: ['claude-header-defaults', 'package-version'], kind: 'string', labelZH: 'Claude 包版本', labelEN: 'Claude package version' },
      { key: 'claude-runtime-version', path: ['claude-header-defaults', 'runtime-version'], kind: 'string', labelZH: 'Claude 运行时版本', labelEN: 'Claude runtime version' },
      { key: 'claude-os', path: ['claude-header-defaults', 'os'], kind: 'string', labelZH: 'Claude 操作系统', labelEN: 'Claude OS' },
      { key: 'claude-arch', path: ['claude-header-defaults', 'arch'], kind: 'string', labelZH: 'Claude 架构', labelEN: 'Claude architecture' },
      { key: 'claude-timeout', path: ['claude-header-defaults', 'timeout'], kind: 'string', labelZH: 'Claude 超时标记', labelEN: 'Claude timeout header' },
      booleanField('claude-stabilize-profile', ['claude-header-defaults', 'stabilize-device-profile'], '固定 Claude 设备画像', 'Stabilize Claude device profile', '保持设备相关请求头稳定。', 'Keep device-related request headers stable.'),
      { key: 'codex-user-agent', path: ['codex-header-defaults', 'user-agent'], kind: 'string', labelZH: 'Codex User-Agent', labelEN: 'Codex User-Agent' },
      { key: 'codex-beta-features', path: ['codex-header-defaults', 'beta-features'], kind: 'string', labelZH: 'Codex Beta Features', labelEN: 'Codex beta features' },
    ],
  },
  {
    key: 'codex',
    labelZH: 'Codex 重试',
    labelEN: 'Codex Retry',
    descriptionZH: '配置 Codex 身份混淆、异常思考重试、结果选择和竞速请求。',
    descriptionEN: 'Configure Codex identity masking, abnormal reasoning retries, result selection, and hedging.',
    fields: [
      booleanField('codex-identity-confuse', ['codex', 'identity-confuse'], 'Codex 身份混淆', 'Codex identity masking', '混淆发送给上游的身份信息。', 'Mask identity information sent upstream.'),
      {
        key: 'codex-retry-action', path: ['codex', 'abnormal-reasoning-retry', 'action'], kind: 'select', labelZH: '异常思考处理方式', labelEN: 'Abnormal reasoning action', defaultValue: 'disabled', options: [
          { value: 'disabled', labelZH: '关闭', labelEN: 'Disabled' },
          { value: 'observe-only', labelZH: '仅观察', labelEN: 'Observe only' },
          { value: 'retry', labelZH: '自动重试', labelEN: 'Retry' },
        ],
      },
      booleanField('codex-retry-enabled', ['codex', 'abnormal-reasoning-retry', 'enabled'], '启用异常思考重试', 'Enable abnormal reasoning retry', '兼容旧版 enabled 配置。', 'Maintain compatibility with the legacy enabled flag.'),
      { key: 'codex-retry-models', path: ['codex', 'abnormal-reasoning-retry', 'model-contains'], kind: 'string-list', labelZH: '匹配模型', labelEN: 'Model substrings', descriptionZH: '每行一个模型名片段。', descriptionEN: 'Enter one model substring per line.', wide: true },
      { key: 'codex-retry-efforts', path: ['codex', 'abnormal-reasoning-retry', 'reasoning-efforts'], kind: 'string-list', labelZH: '匹配思考等级', labelEN: 'Reasoning efforts', descriptionZH: '每行一个 reasoning effort。', descriptionEN: 'Enter one reasoning effort per line.', wide: true },
      { key: 'codex-retry-tokens', path: ['codex', 'abnormal-reasoning-retry', 'reasoning-tokens'], kind: 'integer-list', labelZH: '异常思考 Token', labelEN: 'Abnormal reasoning tokens', descriptionZH: '每行一个非负整数。', descriptionEN: 'Enter one non-negative integer per line.', wide: true },
      { key: 'codex-retry-auth-kinds', path: ['codex', 'abnormal-reasoning-retry', 'auth-kinds'], kind: 'string-list', labelZH: '匹配认证类型', labelEN: 'Authentication kinds', descriptionZH: '每行一个认证类型。', descriptionEN: 'Enter one authentication kind per line.', wide: true },
      { key: 'codex-retry-auth-ids', path: ['codex', 'abnormal-reasoning-retry', 'auth-ids'], kind: 'string-list', labelZH: '匹配认证 ID', labelEN: 'Authentication IDs', descriptionZH: '每行一个认证 ID；留空表示不限定。', descriptionEN: 'Enter one auth ID per line; leave empty for all.', wide: true },
      booleanField('codex-stream-buffer', ['codex', 'abnormal-reasoning-retry', 'stream-buffer'], '启用流缓冲', 'Enable stream buffer', '缓冲流式结果后判断是否需要重试。', 'Buffer streaming results before deciding whether to retry.', true),
      { key: 'codex-stream-buffer-max', path: ['codex', 'abnormal-reasoning-retry', 'stream-buffer-max-bytes'], kind: 'number', labelZH: '流缓冲上限（字节）', labelEN: 'Stream buffer limit (bytes)' },
      { key: 'codex-max-retries', path: ['codex', 'abnormal-reasoning-retry', 'max-retries'], kind: 'number', labelZH: '最大重试次数', labelEN: 'Maximum retries' },
      {
        key: 'codex-exhausted', path: ['codex', 'abnormal-reasoning-retry', 'exhausted-behavior'], kind: 'select', labelZH: '重试耗尽行为', labelEN: 'Exhausted behavior', defaultValue: 'error', options: [
          { value: 'error', labelZH: '返回错误', labelEN: 'Return error' },
          { value: 'pass-through', labelZH: '透传最佳结果', labelEN: 'Pass through result' },
        ],
      },
      {
        key: 'codex-usage-aggregation', path: ['codex', 'abnormal-reasoning-retry', 'client-usage-aggregation'], kind: 'select', labelZH: '客户端用量汇总', labelEN: 'Client usage aggregation', defaultValue: 'delivered-only', options: [
          { value: 'delivered-only', labelZH: '仅最终结果', labelEN: 'Delivered only' },
          { value: 'sum', labelZH: '全部求和', labelEN: 'Sum all' },
          { value: 'sum-with-delivered-total', labelZH: '求和并保留最终总量', labelEN: 'Sum with delivered total' },
        ],
      },
      {
        key: 'codex-delivery-policy', path: ['codex', 'abnormal-reasoning-retry', 'delivery-policy'], kind: 'select', labelZH: '结果交付策略', labelEN: 'Delivery policy', defaultValue: 'best-non-special', options: [
          { value: 'best-non-special', labelZH: '最佳普通结果', labelEN: 'Best non-special' },
          { value: 'first-non-special', labelZH: '首个普通结果', labelEN: 'First non-special' },
          { value: 'max-output', labelZH: '最大输出', labelEN: 'Maximum output' },
          { value: 'latest', labelZH: '最新结果', labelEN: 'Latest' },
        ],
      },
      {
        key: 'codex-fallback-policy', path: ['codex', 'abnormal-reasoning-retry', 'fallback-policy'], kind: 'select', labelZH: '异常结果回退策略', labelEN: 'Fallback policy', defaultValue: 'best-special', options: [
          { value: 'best-special', labelZH: '最佳异常结果', labelEN: 'Best special' },
          { value: 'max-output-special', labelZH: '最大异常输出', labelEN: 'Maximum special output' },
          { value: 'latest-special', labelZH: '最新异常结果', labelEN: 'Latest special' },
        ],
      },
      booleanField('codex-hedged-enabled', ['codex', 'abnormal-reasoning-retry', 'hedged-retry', 'enabled'], '启用竞速重试', 'Enable hedged retry', '延迟后并发发起另一条请求。', 'Start another request concurrently after a delay.'),
      {
        key: 'codex-hedged-mode', path: ['codex', 'abnormal-reasoning-retry', 'hedged-retry', 'mode'], kind: 'select', labelZH: '竞速模式', labelEN: 'Hedged retry mode', defaultValue: 'quality', options: [
          { value: 'quality', labelZH: '质量优先', labelEN: 'Quality' },
          { value: 'speed', labelZH: '速度优先', labelEN: 'Speed' },
        ],
      },
      { key: 'codex-hedge-delay', path: ['codex', 'abnormal-reasoning-retry', 'hedged-retry', 'hedge-delay-ms'], kind: 'number', labelZH: '竞速延迟（毫秒）', labelEN: 'Hedge delay (ms)' },
      booleanField('codex-hedge-distinct-auth', ['codex', 'abnormal-reasoning-retry', 'hedged-retry', 'require-distinct-auth'], '要求不同认证', 'Require distinct auth', '竞速请求必须使用不同凭证。', 'Require hedged requests to use a different credential.', true),
    ],
  },
  {
    key: 'payload',
    labelZH: '请求体规则',
    labelEN: 'Payload Rules',
    descriptionZH: '编辑默认参数、原始参数、覆盖和过滤规则；每项使用 YAML 数组格式。',
    descriptionEN: 'Edit default, raw, override, and filter rules. Each field uses a YAML array.',
    fields: [
      { key: 'payload-default', path: ['payload', 'default'], kind: 'yaml', labelZH: '默认参数规则', labelEN: 'Default rules', wide: true },
      { key: 'payload-default-raw', path: ['payload', 'default-raw'], kind: 'yaml', labelZH: '默认原始参数规则', labelEN: 'Default raw rules', wide: true },
      { key: 'payload-override', path: ['payload', 'override'], kind: 'yaml', labelZH: '覆盖参数规则', labelEN: 'Override rules', wide: true },
      { key: 'payload-override-raw', path: ['payload', 'override-raw'], kind: 'yaml', labelZH: '覆盖原始参数规则', labelEN: 'Override raw rules', wide: true },
      { key: 'payload-filter', path: ['payload', 'filter'], kind: 'yaml', labelZH: '参数过滤规则', labelEN: 'Filter rules', wide: true },
    ],
  },
]

cpaConfigSections.push({
  key: 'core-v8',
  labelZH: 'Core 高级配置', labelEN: 'Core Advanced',
  descriptionZH: '管理新版 Core 的持久化、会话、客户端适配与上游行为。对象字段使用 YAML，留空恢复默认。',
  descriptionEN: 'Manage current Core persistence, sessions, client compatibility, and upstream behavior. Object fields use YAML; leave empty to restore defaults.',
  v8Only: true,
  fields: [
    booleanField('kv-disabled', ['server', 'kv-store', 'disabled'], '禁用本地 KV', 'Disable local KV', '关闭本地 KV 持久化。', 'Disable local KV persistence.'),
    { key: 'kv-path', path: ['server', 'kv-store', 'path'], kind: 'string', labelZH: 'KV 文件路径', labelEN: 'KV file path', descriptionZH: '留空时使用认证目录中的 kv.db。', descriptionEN: 'Defaults to kv.db in the authentication directory.' },
    booleanField('save-cooldown', ['routing', 'cooldown', 'save-cooldown-status'], '持久保存冷却状态', 'Persist cooldown state', '在凭证文件旁保存冷却状态。', 'Save cooldown state next to credential files.'),
    booleanField('session-subagents', ['routing', 'session-affinity-subagents'], '子代理继承会话亲和', 'Inherit session affinity for subagents', '启用会话亲和时，子代理优先继承父会话凭证。', 'When session affinity is enabled, subagents prefer their parent session credential.', true),
    { key: 'session-rebind', path: ['routing', 'session-affinity-rebind-on'], kind: 'select', labelZH: '会话重新绑定策略', labelEN: 'Session rebind policy', defaultValue: 'any', options: [
      { value: 'any', labelZH: '任意凭证故障', labelEN: 'Any credential failure' },
      { value: 'quota', labelZH: '仅配额故障', labelEN: 'Quota failures only' },
    ] },
    { key: 'reasoning-policy', path: ['requests', 'reasoning-effort'], kind: 'yaml', labelZH: '推理强度策略', labelEN: 'Reasoning effort policy', wide: true },
    booleanField('multi-agent-v2', ['client', 'codex', 'optimize-multi-agent-v2'], 'Codex 多代理适配', 'Codex multi-agent compatibility', '适配 Codex 多代理请求与工具行为。', 'Adapt Codex multi-agent requests and tool behavior.'),
    booleanField('apply-patch', ['client', 'codex', 'enable-apply-patch'], 'Codex apply_patch 适配', 'Codex apply_patch compatibility', '启用 Core 的 apply_patch 客户端适配。', 'Enable Core apply_patch client compatibility.'),
    booleanField('show-commentary', ['client', 'claude', 'show-codex-commentary'], '保留 Codex commentary', 'Preserve Codex commentary', '向 Claude 客户端呈现 Codex commentary。', 'Expose Codex commentary to Claude clients.'),
    { key: 'search-result-cache', path: ['client', 'codex', 'web-search-result-cache'], kind: 'yaml-object', labelZH: '搜索结果回放缓存', labelEN: 'Search result replay cache', wide: true },
    { key: 'response-compression', path: ['server', 'response-compression'], kind: 'yaml-object', labelZH: '响应压缩配置', labelEN: 'Response compression', wide: true },
    { key: 'credential-concurrency', path: ['credentials', 'concurrency'], kind: 'yaml-object', labelZH: '凭证并发策略', labelEN: 'Credential concurrency policy', wide: true },
    { key: 'credential-in-flight', path: ['credentials', 'in-flight'], kind: 'yaml-object', labelZH: '在途请求策略', labelEN: 'In-flight request policy', wide: true },
    { key: 'codex-outbound', path: ['upstream', 'codex', 'outbound'], kind: 'yaml-object', labelZH: 'Codex 出站策略', labelEN: 'Codex outbound policy', wide: true },
    { key: 'codex-cache-affinity', path: ['upstream', 'codex', 'cache-affinity'], kind: 'yaml-object', labelZH: 'Codex 缓存亲和', labelEN: 'Codex cache affinity', wide: true },
    { key: 'codex-model-fallback', path: ['upstream', 'codex', 'model-fallback'], kind: 'yaml-object', labelZH: 'Codex 模型回退', labelEN: 'Codex model fallback', wide: true },
    { key: 'codex-continuity', path: ['upstream', 'codex', 'rate-limit-continuity'], kind: 'yaml-object', labelZH: 'Codex 限流连续性', labelEN: 'Codex rate-limit continuity', wide: true },
    { key: 'claude-timezone', path: ['upstream', 'claude', 'header-defaults', 'timezone'], kind: 'string', labelZH: 'Claude 时区', labelEN: 'Claude timezone' },
  ],
})

export const cpaConfigFields = cpaConfigSections.flatMap(section => section.fields)
