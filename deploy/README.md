# OpenShift deployment with Kustomize (v2.2)

This setup deploys the backend and embedded UI, a persistent SQLite volume,
Service, HTTPS Route, service account, injected trusted CA bundle, configuration,
and credentials. It uses dynamic Prow periodic discovery; no static validation
plan or generator matrix is required. JIRA writes are disabled.

v2.2 is the current v2 release. The default image is
`quay.io/nasonawa/release-readness:v2.2`. Rebuild this tag from the **current branch**
before deployment: the previously published image predates the worker-budget and
ART-group flags. Preparing these files does not build an image or deploy anything.

## Prepare configuration

From the repository root:

```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
```

Edit `deploy/.env`:

- `IMAGE`: full image reference with an explicit tag or digest.
- `NAMESPACE`: target OpenShift project (default `nasonawa`).
- `JIRA_EMAIL` and `JIRA_TOKEN` (or `JIRA_TOKEN_FILE`): read credentials.
- `GITLAB_TOKEN` (or `GITLAB_TOKEN_FILE`): read access to shipment MRs.
  Set `GITLAB_URL=` to disable this integration instead.
- `KONFLUX_KUBECONFIG`: path to a self-contained Konflux reader kubeconfig.
- Optional `GITHUB_TOKEN` / `GITHUB_TOKEN_FILE` for GitHub rate limits.
- Optional `IMAGE_PULL_SECRET_FILE`: Docker/Podman auth JSON for a private image.
- Optional `ROUTE_HOST`, `STORAGE_CLASS`, `STORAGE_SIZE` (default `5Gi`).

Every setting in [base/app.env](base/app.env) can also be overridden in `deploy/.env`:
source URLs, ART groups, project, worker counts, poll intervals, lookback, and
history budgets. `PROW_JOB_DISCOVERY=true` and `JIRA_WRITE=false` are enforced.
Use a dedicated deployment env file rather than passing an unrelated Compose
`.env`: unknown keys are rejected. Existing credentials can be copied into this
file or referenced via token files; files contain only the token.

The parser accepts `KEY=value`, comments on separate lines, and optional matching
quotes. It does **not** execute shell code, expand variables, or interpret escapes.
Relative credential paths resolve against the env file's directory; `~` is
supported. Never put credentials in `base/app.env`, which is tracked in Git.

Export only the required Konflux context (replace the context below):

```bash
umask 077
oc --context=YOUR_KONFLUX_READER_CONTEXT config view --minify --flatten --raw \
  > deploy/konflux-reader.kubeconfig
# In deploy/.env: KONFLUX_KUBECONFIG=konflux-reader.kubeconfig
```

The exported context must select the intended Konflux cluster and contain an
embedded credential and CA, with no host file references or exec/login plugins.
The app mounts it read-only. Its identity needs list access to Snapshots and
Releases in `KONFLUX_NAMESPACE`; scans also need a bearer token and get/list
permissions on Tekton Results `results` and `records`.

[rbac.yaml](rbac.yaml) documents those resource permissions, but is intentionally
excluded from this Kustomize base: Konflux and the target app can be on different
clusters. A Konflux administrator must authorize the reader identity there.
No target-cluster API permission or mounted service account token is needed.

## Generate and deploy

Requires Python 3.9+ and `oc` with built-in Kustomize support. Generation is local
and does not contact any service:

```bash
python3 deploy/configure.py --env-file deploy/.env
# Validate rendering without printing the Secret contents:
oc kustomize deploy/.generated > /dev/null

# Use the context for your application cluster, not the Konflux reader context.
oc config use-context YOUR_APP_CLUSTER_CONTEXT
oc project nasonawa
# If the project does not exist and you have permission: oc new-project nasonawa
oc apply -k deploy/.generated
oc -n nasonawa rollout status deployment/release-readiness --timeout=300s
oc -n nasonawa get route release-readiness -o jsonpath='https://{.spec.host}{"\n"}'
oc -n nasonawa logs deployment/release-readiness --tail=100
```

If you change `NAMESPACE`, use that project in the commands. The overlay explicitly
sets namespaces; it does not create a Project. Apply **the generated overlay**,
not the base directory. `deploy/.generated/` is ignored by Git and contains
plaintext credentials in owner-only files; rendered YAML also contains Secrets.

The app uses a single replica and Recreate updates for SQLite. An existing
`release-readiness-data` PVC is reused; back it up before upgrading. Its storage
class cannot be changed in place, and resizing depends on the storage provider.
Leaving `STORAGE_CLASS` blank selects the cluster default for a new PVC.

The injected CA bundle must trust your internal endpoints. Ensure the target
cluster can reach JIRA, GitLab, Konflux, ART, Sippy, GitHub, GCS, and the registries.
HTTP health means the server is running; `/api/v1/sync-status` shows ingestion
failures separately. The Route provides TLS, but the app has no login layer;
apply your organization's access controls before exposing internal release data.

## Update or rotate credentials

Edit `deploy/.env` or its referenced files, rerun `configure.py`, and apply the
overlay again. ConfigMap/Secret name hashes change their Deployment references,
triggering a rollout. Prefer a new image digest/tag for each release; if reusing
a tag with unchanged configuration, explicitly restart the Deployment:

```bash
python3 deploy/configure.py
oc apply -k deploy/.generated
oc -n nasonawa rollout restart deployment/release-readiness
oc -n nasonawa rollout status deployment/release-readiness --timeout=300s
```

Older generated Secrets and ConfigMaps are retained by `oc apply`; remove them
only after confirming they are no longer referenced by workloads or needed for
rollback. Do not use broad pruning that could remove the database PVC.

To stop the app while preserving its database and configuration:

```bash
oc -n nasonawa scale deployment/release-readiness --replicas=0
```

`oc delete -k deploy/.generated` also deletes the PVC and can destroy its data.
Use explicit resource deletion if you want to preserve the database.

## Local verification

```bash
python3 -m unittest discover -s deploy -p 'test_*.py'
```

Tests use dummy credentials and local Kustomize rendering; no cluster is contacted.
This directory replaces the former version-specific deployment snapshots.
The base lives in `deploy/base/`; the generated overlay adds namespace, image,
settings, and Secrets. Render tests additionally require PyYAML.
