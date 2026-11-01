import copy
from pathlib import Path
import tempfile
import unittest

import yaml

from prepare_cpa_v8 import ConfigError, prepare_config, prepare_file


class PrepareCPAV8Tests(unittest.TestCase):
    def fixture(self):
        return {'host': '', 'port': 18316, 'api-keys': ['client-secret'],
                'remote-management': {'allow-remote': True, 'secret-key': 'management-secret'},
                'debug': False, 'request-retry': 0, 'auth-dir': '/root/.cli-proxy-api',
                'codex': {'identity-confuse': True, 'optimize-multi-agent-v2': False,
                          'live-media-relay': {'enabled': False}, 'abnormal-reasoning-retry': {'enabled': False}},
                'claude-code': {'disable-cloaking-model-list': False},
                'quota-exceeded': {'switch-project': True, 'antigravity-credits': True},
                'codex-api-key': [{'api-key': 'upstream-secret', 'base-url': 'https://example.invalid/v1',
                                   'websockets': True, 'disable-cooling': False, 'request-retry': 0, 'models': []}],
                'openai-compatibility': [{'name': 'compatible', 'disabled': True,
                    'api-key-entries': [{'api-key': 'compatible-secret', 'weight': 5, 'proxy-url': None}]}],
                'payload': {'override': [{'params': {'service_tier': 'fast'}}]},
                'plugins': {'enabled': False, 'configs': {'custom': {'opaque': {'secret': 'plugin-secret'}}}}}

    def test_layout_and_secrets_are_preserved_without_mutating_input(self):
        source = self.fixture()
        original = copy.deepcopy(source)
        candidate, _ = prepare_config(source)
        self.assertEqual(source, original)
        self.assertEqual(candidate['access']['api-keys'], ['client-secret'])
        self.assertEqual(candidate['management']['secret-key'], 'management-secret')
        self.assertEqual(candidate['upstream']['codex']['identity-confuse'], True)
        self.assertEqual(candidate['client']['codex']['optimize-multi-agent-v2'], False)
        self.assertEqual(candidate['oauth']['providers']['codex']['live-media-relay'], {'enabled': False})
        self.assertEqual(candidate['upstream']['claude']['disable-cloaking-model-list'], False)
        self.assertEqual(candidate['oauth']['providers']['antigravity']['antigravity-credits'], True)
        self.assertEqual(candidate['routing']['retry']['request-retry'], 0)
        group = candidate['api-keys']['codex'][0]
        self.assertEqual(group['keys'], [{'api-key': 'upstream-secret', 'websockets': True}])
        self.assertEqual(group['models'], [])
        self.assertFalse(group['disable-cooling'])
        self.assertEqual(group['request-retry'], 0)
        self.assertEqual(candidate['api-keys']['openai-compatibility'][0]['keys'][0]['proxy-url'], None)
        self.assertEqual(candidate['plugins'], source['plugins'])
        self.assertEqual(candidate['requests']['payload'], source['payload'])

    def test_ambiguous_or_unknown_layouts_fail_without_discarding_fields(self):
        for extra in ({'api-keys': {}}, {'server': {}}, {'config-version': 8}, {'forgotten-field': True}):
            with self.subTest(extra=extra), self.assertRaises(ConfigError):
                prepare_config(self.fixture() | extra)

    def test_default_is_read_only_and_output_is_private_exclusive(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'config.yml'
            output = Path(directory) / 'config.v8.yml'
            source.write_text(yaml.safe_dump(self.fixture()), encoding='utf-8')
            before = source.read_bytes()
            report = prepare_file(source)
            self.assertEqual(report['status'], 'dry-run-passed')
            self.assertFalse(output.exists())
            self.assertNotIn('client-secret', str(report))
            self.assertEqual(prepare_file(source, output)['status'], 'candidate-created')
            self.assertEqual(yaml.safe_load(output.read_text())['config-version'], 8)
            with self.assertRaises(FileExistsError):
                prepare_file(source, output)
            with self.assertRaises(ConfigError):
                prepare_file(source, source)
            self.assertEqual(source.read_bytes(), before)

    def test_duplicate_yaml_keys_are_not_silently_lost(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'config.yml'
            source.write_text('api-keys: [first-secret]\napi-keys: [second-secret]\n')
            with self.assertRaises(ConfigError):
                prepare_file(source)


if __name__ == '__main__':
    unittest.main()
