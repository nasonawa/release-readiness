"""Offline checks of credential handling and rendered deployment wiring."""
import base64
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import configure


class ConfigureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'deploy'
        shutil.copytree(configure.ROOT, self.root,
                        ignore=shutil.ignore_patterns('.generated', '.env', '*.kubeconfig', '__pycache__', '.prepare-*'))
        self.env = self.root / '.env'
        (self.root / 'reader.yaml').write_text('apiVersion: v1\nkind: Config\n')
        self.base = ('NAMESPACE=nasonawa\nIMAGE=quay.io/example/readiness:test\n'
                     'JIRA_EMAIL=test@example.invalid\nJIRA_TOKEN=fake$jira#token\n'
                     'GITLAB_TOKEN=fake-gitlab\nKONFLUX_KUBECONFIG=reader.yaml\n')
        self.env.write_text(self.base)
        self.root_patch = patch.object(configure, 'ROOT', self.root)
        self.root_patch.start()
        self.addCleanup(self.root_patch.stop)

    def test_inline_tokens_are_literal(self):
        self.env.write_text(self.base.replace('fake$jira#token', 'fake$(touch /tmp/not-executed)'))
        files = configure.prepare(self.env)
        self.assertIn(b'JIRA_TOKEN=fake$(touch /tmp/not-executed)\n', files['credentials.env'])
        self.assertNotIn('app.env', files)
        self.assertNotIn('configMapGenerator', json.loads(files['kustomization.yaml']))

    def test_invalid_inputs_rejected(self):
        for change in ('JIRA_TOKEN_FILE=reader.yaml\n', 'UNRECOGNIZED=value\n',
                       'JIRA_WRITE=true\n', 'PROW_JOB_DISCOVERY=false\n',
                       'STORAGE_SIZE=-1Gi\n', 'ROUTE_HOST=https://invalid\n',
                       'CI_WORKERS=8\n', 'JIRA_API_URL=https://example.invalid\n',
                       'GITLAB_URL=\n', 'GITHUB_TOKEN_FILE=token\n',
                       'GITLAB_TOKEN_FILE=token\n', 'GITHUB_TOKEN="unterminated\n'):
            with self.subTest(change=change):
                self.env.write_text(self.base + change)
                with self.assertRaises(ValueError):
                    configure.prepare(self.env)
        self.env.write_text(self.base.replace('JIRA_TOKEN=fake$jira#token', 'JIRA_TOKEN='))
        with self.assertRaises(ValueError):
            configure.prepare(self.env)

    def test_digest_image(self):
        digest = 'sha256:' + 'a' * 64
        self.env.write_text(self.base.replace('readiness:test', 'readiness@' + digest))
        overlay = json.loads(configure.prepare(self.env)['kustomization.yaml'])
        self.assertEqual(overlay['images'][0]['digest'], digest)
        self.assertEqual(overlay['images'][0]['newName'], 'quay.io/example/readiness')

    def test_required_credentials_and_optional_github(self):
        self.assertIn(b'GITHUB_TOKEN=\n', configure.prepare(self.env)['credentials.env'])
        for key in ('JIRA_EMAIL', 'JIRA_TOKEN', 'GITLAB_TOKEN', 'KONFLUX_KUBECONFIG'):
            with self.subTest(key=key):
                self.env.write_text('\n'.join(line for line in self.base.splitlines()
                                              if not line.startswith(key + '=')))
                with self.assertRaises(ValueError):
                    configure.prepare(self.env)

    def test_template_defaults(self):
        template = configure.dotenv(self.root / '.env.example')
        self.env.write_text('\n'.join(line for line in self.base.splitlines()
                                      if not line.startswith(('NAMESPACE=', 'IMAGE='))))
        overlay = json.loads(configure.prepare(self.env)['kustomization.yaml'])
        self.assertEqual(overlay['namespace'], template['NAMESPACE'])
        image = overlay['images'][0]
        self.assertEqual(image['newName'] + ':' + image['newTag'], template['IMAGE'])


    def test_render_and_rotation(self):
        if not shutil.which('oc'):
            self.skipTest('oc is required for offline Kustomize checks')
        self.env.write_text(self.base + 'ROUTE_HOST=readiness.example.com\nSTORAGE_CLASS=example\n'
                            'IMAGE_PULL_SECRET_FILE=pull-auth.json\n')
        (self.root / 'pull-auth.json').write_text('{"auths":{"quay.io":{"auth":"ZmFrZQ=="}}}')

        def generate():
            result = subprocess.run(['python3', str(self.root / 'configure.py')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            output = self.root / '.generated'
            self.assertEqual(output.stat().st_mode & 0o777, 0o700)
            for file in output.iterdir():
                self.assertEqual(file.stat().st_mode & 0o777, 0o600)
            result = subprocess.run(['oc', 'kustomize', str(output), '-o', str(output / 'rendered.yaml')],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            # Use local Python YAML only for assertions, never for the generator.
            import yaml
            docs = list(yaml.safe_load_all((output / 'rendered.yaml').read_text()))
            return docs

        try:
            import yaml  # noqa: F401
        except ImportError:
            self.skipTest('PyYAML needed only for rendered manifest assertions')
        docs = generate()
        deployment = next(d for d in docs if d['kind'] == 'Deployment')
        pod = deployment['spec']['template']['spec']
        container = pod['containers'][0]
        self.assertEqual(container['image'], 'quay.io/example/readiness:test')
        secrets = {d['metadata']['name']: d for d in docs if d['kind'] == 'Secret'}
        credential_name = container['envFrom'][1]['secretRef']['name']
        self.assertIn(credential_name, secrets)
        self.assertEqual(base64.b64decode(secrets[credential_name]['data']['JIRA_TOKEN']), b'fake$jira#token')
        self.assertIn(pod['volumes'][1]['secret']['secretName'], secrets)
        self.assertIn(pod['imagePullSecrets'][0]['name'], secrets)
        maps = {d['metadata']['name'] for d in docs if d['kind'] == 'ConfigMap'}
        self.assertIn(container['envFrom'][0]['configMapRef']['name'], maps)
        self.assertTrue(all(d['metadata']['namespace'] == 'nasonawa' for d in docs))
        self.assertFalse(pod['automountServiceAccountToken'])
        self.env.write_text(self.env.read_text().replace('fake$jira#token', 'rotated-fake-token'))
        rotated = generate()
        new_deploy = next(d for d in rotated if d['kind'] == 'Deployment')
        new_name = new_deploy['spec']['template']['spec']['containers'][0]['envFrom'][1]['secretRef']['name']
        self.assertNotEqual(credential_name, new_name)
        # Failed regeneration must preserve the previous overlay.
        before = (self.root / '.generated/credentials.env').read_bytes()
        self.env.write_text(self.base.replace('JIRA_TOKEN=fake$jira#token', 'JIRA_TOKEN='))
        result = subprocess.run(['python3', str(self.root / 'configure.py')], capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(before, (self.root / '.generated/credentials.env').read_bytes())


if __name__ == '__main__':
    unittest.main()
