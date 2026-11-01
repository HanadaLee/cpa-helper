#!/usr/bin/env python3
"""Prepare a CPA-Core-LTS 0.1.2 config candidate without changing the running config.

Default: read-only validation and migration summary. --output creates a NEW file
with mode 0600; it never overwrites the source or an existing candidate. PyYAML
is required. This is a CPA-Helper deployment aid, not an upstream migration tool.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import sys

import yaml


# Reviewed against CPA-Core-LTS v1-lts-0.1.2 (460c177c), config_v8.go.
# Specific Codex paths are moved before the remaining OAuth-only container.
PATHS = {
    'host': 'server.host', 'port': 'server.port', 'tls': 'server.tls',
    'max-request-body-bytes': 'server.max-request-body-bytes',
    'trusted-proxies': 'server.trusted-proxies', 'github-token': 'server.github-token',
    'commercial-mode': 'server.commercial-mode', 'discovery': 'server.discovery',
    'response-compression': 'server.response-compression',
    'remote-management': 'management', 'api-keys': 'access.api-keys',
    'credential-concurrency': 'credentials.concurrency', 'credential-in-flight': 'credentials.in-flight',
    'force-model-prefix': 'routing.force-model-prefix',
    'request-retry': 'routing.retry.request-retry',
    'max-retry-credentials': 'routing.retry.max-retry-credentials',
    'max-retry-interval': 'routing.retry.max-retry-interval',
    'disable-cooling': 'routing.cooldown.disable-cooling',
    'save-cooldown-status': 'routing.cooldown.save-cooldown-status',
    'transient-error-cooldown-seconds': 'routing.cooldown.transient-error-cooldown-seconds',
    'proxy-url': 'requests.proxy-url', 'passthrough-headers': 'requests.passthrough-headers',
    'tls-handshake-timeout-seconds': 'requests.tls-handshake-timeout-seconds',
    'nonstream-keepalive-interval': 'requests.nonstream-keepalive-interval',
    'streaming': 'requests.streaming', 'payload': 'requests.payload',
    'reasoning-effort': 'requests.reasoning-effort',
    'auth-dir': 'oauth.auth-dir', 'auth-auto-refresh-workers': 'oauth.auth-auto-refresh-workers',
    'oauth-model-alias': 'oauth.model-alias', 'oauth-excluded-models': 'oauth.excluded-models',
    'oauth-model-policy': 'oauth.model-policy', 'oauth-request-scoped-errors': 'oauth.request-scoped-errors',
    'oauth-settings': 'oauth.settings', 'ws-auth': 'oauth.providers.aistudio.ws-auth',
    'codex.optimize-multi-agent-v2': 'client.codex.optimize-multi-agent-v2',
    **{f'codex.{field}': f'upstream.codex.{field}' for field in (
        'disable-codex-cloaking', 'stream-bootstrap-buffering', 'stream-bootstrap-timeout',
        'orphan-delegation-compatibility', 'model-level-cooling', 'response-steering',
        'cache-affinity', 'identity-confuse', 'outbound', 'zcode-context-usage',
        'client-metadata', 'desktop-tool-overlay', 'model-fallback', 'rate-limit-continuity',
        'abnormal-reasoning-retry', 'upstream-request-compression',
        'upstream-request-compression-min-bytes', 'websocket-compression')},
    'codex': 'oauth.providers.codex', 'codex-header-defaults': 'oauth.providers.codex.header-defaults',
    'claude-header-defaults': 'upstream.claude.header-defaults',
    'disable-claude-cloak-mode': 'upstream.claude.disable-claude-cloak-mode',
    'claude-code.disable-cloaking-model-list': 'upstream.claude.disable-cloaking-model-list',
    'xai': 'upstream.xai', 'devin': 'oauth.providers.devin',
    'antigravity': 'oauth.providers.antigravity',
    'antigravity-signature-cache-enabled': 'oauth.providers.antigravity.signature-cache-enabled',
    'antigravity-signature-bypass-strict': 'oauth.providers.antigravity.signature-bypass-strict',
    'quota-exceeded.antigravity-credits': 'oauth.providers.antigravity.antigravity-credits',
    'disable-image-generation': 'multimedia.disable-image-generation',
    'gpt-image-2-base-model': 'multimedia.gpt-image-2-base-model',
    'video-result-auth-cache-ttl': 'multimedia.video-result-auth-cache-ttl',
    'debug': 'observability.logs.debug', 'logging-to-file': 'observability.logs.logging-to-file',
    'logs-max-total-size-mb': 'observability.logs.logs-max-total-size-mb',
    'request-log': 'observability.logs.request-log', 'error-logs-max-files': 'observability.logs.error-logs-max-files',
    'usage-statistics-enabled': 'observability.usage.usage-statistics-enabled',
    'redis-usage-queue-retention-seconds': 'observability.usage.redis-usage-queue-retention-seconds',
    'pprof': 'observability.pprof',
}
FAMILIES = {f'{name}-api-key': name for name in ('gemini', 'interactions', 'vertex', 'codex', 'claude', 'xai', 'meta')}
FAMILIES['openai-compatibility'] = 'openai-compatibility'
SHARED_KEY_FIELDS = {'base-url', 'priority', 'prefix', 'proxy-url', 'headers', 'models',
                     'excluded-models', 'disable-cooling', 'request-retry', 'request-scoped-errors'}
NATIVE_ROOTS = {'config-version', 'server', 'management', 'access', 'credentials', 'oauth',
                'upstream', 'requests', 'multimedia', 'observability', 'models', 'client',
                'plugins', 'quota-exceeded', 'routing', 'flow-control', 'api-request-body-max-bytes', 'ampcode', 'api-keys'}


class ConfigError(ValueError):
    pass


class UniqueLoader(yaml.SafeLoader):
    """Refuse duplicate keys rather than silently losing a credential or setting."""


def unique_mapping(loader, node, deep=False):
    if any(key.value == '<<' for key, _ in node.value):
        raise ConfigError('YAML merge keys require manual review')
    result = {}
    for key_node, value_node in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if not isinstance(key, str) or key in result:
            raise ConfigError('Duplicate or non-string YAML mapping key; review source locally')
        result[key] = loader.construct_object(value_node, deep=deep)
    return result


UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, unique_mapping)


def take(root, path):
    parts = path.split('.')
    parents = []
    node = root
    for part in parts[:-1]:
        if not isinstance(node, dict) or part not in node:
            return False, None
        parents.append((node, part))
        node = node[part]
    if not isinstance(node, dict) or parts[-1] not in node:
        return False, None
    value = node.pop(parts[-1])
    for parent, part in reversed(parents):
        if parent[part] == {}:
            parent.pop(part)
        else:
            break
    return True, value


def put(root, path, value):
    parts = path.split('.')
    node = root
    for part in parts[:-1]:
        node = node.setdefault(part, {})
        if not isinstance(node, dict):
            raise ConfigError('Conflicting destination container: ' + path)
    if parts[-1] in node:
        raise ConfigError('Conflicting destination field: ' + path)
    node[parts[-1]] = value


def prepare_config(source):
    if not isinstance(source, dict):
        raise ConfigError('Configuration must be a mapping')
    if source.get('config-version') == 8:
        raise ConfigError('Source is already V8; no migration performed')
    for key in ('server', 'management', 'access', 'oauth', 'upstream', 'requests', 'observability', 'credentials'):
        if key in source:
            raise ConfigError('Mixed V7/V8 layout requires manual review: ' + key)
    if 'config-version' in source:
        raise ConfigError('Unexpected configuration version; manual review required')
    result = copy.deepcopy(source)
    moves = []
    # Moving api-keys must happen before constructing the new provider map.
    if 'api-keys' in result and not isinstance(result['api-keys'], list):
        raise ConfigError('Legacy client api-keys must be a list')
    for old, new in PATHS.items():
        exists, value = take(result, old)
        if exists:
            put(result, new, value)
            moves.append([old, new])
    for old, family in FAMILIES.items():
        if old not in result:
            continue
        entries = result.pop(old)
        if not isinstance(entries, list) or any(not isinstance(entry, dict) for entry in entries):
            raise ConfigError('Invalid provider list: ' + old)
        groups = []
        for index, entry in enumerate(entries):
            if family == 'openai-compatibility':
                group = copy.deepcopy(entry)
                group['keys'] = group.pop('api-key-entries', [])
            else:
                group = {'name': f'{family}-{index + 1}'}
                key = copy.deepcopy(entry)
                for field in SHARED_KEY_FIELDS:
                    if field in key:
                        group[field] = key.pop(field)
                group['keys'] = [key]
            groups.append(group)
        put(result, 'api-keys.' + family, groups)
        moves.append([old, 'api-keys.' + family])
    if any(key not in NATIVE_ROOTS for key in result):
        # Do not print arbitrary unknown keys: opaque plugin/config keys can themselves be secrets.
        raise ConfigError('Unmapped root fields remain; review source locally (no fields discarded)')
    result['config-version'] = 8
    return result, moves


def prepare_file(source, output=None):
    raw = source.read_bytes()
    if len(raw) > 8 * 1024 * 1024:
        raise ConfigError('Configuration exceeds 8 MiB')
    try:
        parsed = yaml.load(raw, Loader=UniqueLoader)
        candidate, moves = prepare_config(parsed)
        encoded = yaml.safe_dump(candidate, allow_unicode=True, sort_keys=False).encode('utf-8')
    except (yaml.YAMLError, RecursionError, UnicodeError, TypeError) as error:
        raise ConfigError('Unsupported YAML structure; review source locally') from error
    # Round-trip values, including false, zero, null and empty overrides.
    if yaml.safe_load(encoded) != candidate:
        raise ConfigError('Candidate round-trip validation failed')
    if output is not None:
        if output.resolve() == source.resolve():
            raise ConfigError('Output must not be the live source config')
        fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as handle:
            handle.write(encoded)
            handle.flush()
            os.fsync(handle.fileno())
    return {'status': 'candidate-created' if output else 'dry-run-passed',
            'source_sha256': hashlib.sha256(raw).hexdigest(),
            'candidate_sha256': hashlib.sha256(encoded).hexdigest(),
            'config_version': 8, 'moves': moves,
            'client_key_count': len(candidate.get('access', {}).get('api-keys', [])),
            'provider_groups': {name: len(groups) for name, groups in candidate.get('api-keys', {}).items()},
            'source_modified': False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--output', type=Path, help='Create a new private candidate file; never overwrite')
    args = parser.parse_args()
    try:
        print(json.dumps(prepare_file(args.source, args.output), ensure_ascii=False, indent=2))
        return 0
    except (OSError, ConfigError) as error:
        # Filesystem and YAML exceptions may contain private source fragments.
        message = str(error) if isinstance(error, ConfigError) else type(error).__name__
        print(json.dumps({'status': 'failed', 'error': message, 'source_modified': False}))
        return 1


if __name__ == '__main__':
    sys.exit(main())
