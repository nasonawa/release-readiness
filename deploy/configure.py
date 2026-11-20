#!/usr/bin/env python3
"""Prepare a private Kustomize overlay; never contacts the cluster."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import sys
import tempfile

ROOT = Path(__file__).resolve().parent
TOKENS = ('JIRA_TOKEN', 'GITLAB_TOKEN', 'GITHUB_TOKEN')


def dotenv(path):
    result = {}
    for number, line in enumerate(path.read_text().splitlines(), 1):
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        key, separator, value = line.partition('=')
        key, value = key.strip(), value.strip()
        if not separator or not re.fullmatch(r'[A-Z][A-Z0-9_]*', key):
            raise ValueError(f'{path.name}:{number}: expected KEY=value')
        if key in result:
            raise ValueError(f'{path.name}:{number}: duplicate {key}')
        if value.startswith(('"', "'")):
            if len(value) < 2 or value[-1] != value[0]:
                raise ValueError(f'{path.name}:{number}: unmatched quote')
            value = value[1:-1]
        result[key] = value
    return result


def read_file(value, env_path):
    path = Path(value).expanduser()
    if not path.is_absolute():
        path = env_path.parent / path
    return path.read_bytes()


def prepare(env_path):
    # The public template defines the complete configuration interface.
    defaults = dotenv(ROOT / '.env.example')
    supplied = dotenv(env_path)
    unknown = supplied.keys() - defaults.keys()
    if unknown:
        raise ValueError('Unknown settings: ' + ', '.join(sorted(unknown)))
    values = defaults | supplied
    settings = dotenv(ROOT / 'base' / 'app.env')
    if settings['PROW_JOB_DISCOVERY'] != 'true' or settings['JIRA_WRITE'] != 'false':
        raise ValueError('This deployment requires Prow discovery and read-only JIRA')
    namespace = values['NAMESPACE']
    if len(namespace) > 63 or not re.fullmatch(r'[a-z0-9](?:[a-z0-9-]*[a-z0-9])?', namespace):
        raise ValueError('NAMESPACE must be a DNS label')
    image = values.get('IMAGE', '')
    if not image or any(c.isspace() for c in image) or '/' not in image:
        raise ValueError('IMAGE must name a registry image built from the current branch')
    credentials = {'JIRA_EMAIL': values.get('JIRA_EMAIL', '')}
    credentials.update({key: values[key] for key in TOKENS})
    for key in ('JIRA_EMAIL', 'JIRA_TOKEN'):
        if not credentials[key]:
            raise ValueError(f'{key} is required for release discovery')
    if settings['GITLAB_URL'] and not credentials['GITLAB_TOKEN']:
        raise ValueError('GITLAB_TOKEN is required for shipment lookup')
    for key, value in (settings | credentials).items():
        if any(c in value for c in ('\n', '\r', '\x00')):
            raise ValueError(f'{key} must be a single line')
        # Env generator strips quotes; fail rather than silently alter a credential.
        if value.startswith(('"', "'")):
            raise ValueError(f'{key} cannot start with a quote')
    if not values.get('KONFLUX_KUBECONFIG'):
        raise ValueError('KONFLUX_KUBECONFIG must point to a self-contained reader kubeconfig')
    kubeconfig = read_file(values['KONFLUX_KUBECONFIG'], env_path)
    if not kubeconfig.strip():
        raise ValueError('KONFLUX_KUBECONFIG is empty')
    size = values['STORAGE_SIZE']
    if not re.fullmatch(r'[1-9][0-9]*(?:Mi|Gi|Ti)', size):
        raise ValueError('STORAGE_SIZE must be a positive quantity such as 5Gi')
    pvc = {'apiVersion': 'v1', 'kind': 'PersistentVolumeClaim',
           'metadata': {'name': 'release-readiness-data'},
           'spec': {'resources': {'requests': {'storage': size}}}}
    if values.get('STORAGE_CLASS'):
        pvc['spec']['storageClassName'] = values['STORAGE_CLASS']
    patches = [{'patch': json.dumps(pvc)}]
    if values.get('ROUTE_HOST'):
        host = values['ROUTE_HOST']
        if len(host) > 253 or not all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', part) for part in host.split('.')):
            raise ValueError('ROUTE_HOST must be a DNS hostname')
        patches.append({'patch': json.dumps({'apiVersion': 'route.openshift.io/v1',
            'kind': 'Route', 'metadata': {'name': 'release-readiness'}, 'spec': {'host': host}})})
    overlay = {'apiVersion': 'kustomize.config.k8s.io/v1beta1', 'kind': 'Kustomization',
        'namespace': namespace, 'resources': ['../base'],
        'images': [{'name': 'release-readiness', 'newName': image}],
        'secretGenerator': [
            {'name': 'release-readiness-credentials', 'envs': ['credentials.env']},
            {'name': 'release-readiness-konflux', 'files': ['kubeconfig=konflux.kubeconfig']}],
        'patches': patches}
    # Split tag/digest explicitly: otherwise Kustomize preserves :configured.
    image_spec = overlay['images'][0]
    if '@' in image:
        image_spec['newName'], image_spec['digest'] = image.rsplit('@', 1)
    elif ':' in image.rsplit('/', 1)[-1]:
        image_spec['newName'], image_spec['newTag'] = image.rsplit(':', 1)
        if not image_spec['newTag']:
            raise ValueError('IMAGE must include an explicit tag or digest')
    else:
        raise ValueError('IMAGE must include an explicit tag or digest')
    files = {'credentials.env': ''.join(f'{k}={v}\n' for k, v in credentials.items()).encode(),
             'konflux.kubeconfig': kubeconfig}
    if values.get('IMAGE_PULL_SECRET_FILE'):
        auth = read_file(values['IMAGE_PULL_SECRET_FILE'], env_path)
        try:
            valid_auth = isinstance(json.loads(auth).get('auths'), dict)
        except (ValueError, AttributeError):
            valid_auth = False
        if not valid_auth:
            raise ValueError('IMAGE_PULL_SECRET_FILE must contain Docker auth JSON with auths')
        files['pull.json'] = auth
        overlay['secretGenerator'].append({'name': 'release-readiness-pull',
            'type': 'kubernetes.io/dockerconfigjson', 'files': ['.dockerconfigjson=pull.json']})
        patches.append({'patch': json.dumps({'apiVersion': 'apps/v1', 'kind': 'Deployment',
            'metadata': {'name': 'release-readiness'}, 'spec': {'template': {'spec': {
                'imagePullSecrets': [{'name': 'release-readiness-pull'}]}}}})})
    files['kustomization.yaml'] = json.dumps(overlay, indent=2).encode() + b'\n'
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--env-file', type=Path, default=ROOT / '.env')
    args = parser.parse_args()
    try:
        files = prepare(args.env_file.resolve())
        # Validate everything before touching the previous usable overlay.
        os.umask(0o077)
        output = ROOT / '.generated'
        if output.is_symlink():
            raise ValueError('Refusing a symlink at .generated')
        with tempfile.TemporaryDirectory(prefix='.prepare-', dir=ROOT) as scratch:
            staging = Path(scratch)
            for name, data in files.items():
                (staging / name).write_bytes(data)
            if output.exists():
                shutil.rmtree(output)
            staging.rename(output)
        print('Prepared deploy/.generated (contains private credentials).')
        print('Deploy with: oc apply -k deploy/.generated')
    except (OSError, ValueError, UnicodeError):
        # OSError/parse failures can include file contents; never echo secrets.
        print('Configuration failed. Check paths, required values, and .env syntax; see deploy/README.md.', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
