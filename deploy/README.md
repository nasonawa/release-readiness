# OpenShift deployment with Kustomize

This setup deploys the backend and embedded UI, a persistent SQLite volume,
Service, HTTPS Route, service account, injected trusted CA bundle, configuration,
and credentials. It uses dynamic Prow periodic discovery; no static validation
plan or generator matrix is required. JIRA writes are disabled.

The example image is `quay.io/your-org/release-readiness:v2.2`. Replace
`your-org` with your registry organization and build and publish the image from
the current code before deployment. Replace `your-namespace` with your target
OpenShift project throughout the configuration and commands. Preparing these
files does not build an image or deploy anything.

## Plain apply (single manifest)

[app.yaml](app.yaml) is a standalone manifest with the same Deployment
setup the app runs in production — dynamic Prow discovery with Sippy,
explicit ART groups, accelerated history backfill, read-only JIRA, a
mounted Konflux reader kubeconfig and the injected trusted CA. It holds
no credentials: the Secrets it references are created separately.

```bash
# Build and push the image, then point deploy/app.yaml's image at it.
oc create secret generic release-readiness-jira \
  --from-literal=JIRA_EMAIL=<email> --from-literal=JIRA_TOKEN=<token>
oc create secret generic release-readiness-konflux-kubeconfig \
  --from-file=kubeconfig=<minified reader kubeconfig>
# Optional: release-readiness-gitlab, release-readiness-github (key: token).
oc apply -n <namespace> -f deploy/app.yaml
```

The Kustomize flow below generates the same Deployment from `base/` with
per-environment values and generated Secrets; use whichever fits. Keep the
two in sync when the Deployment changes.

## Prepare configuration

From the repository root:

```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
```

Edit `deploy/.env`:

- `IMAGE`: full image reference with an explicit tag or digest.
- `NAMESPACE`: target OpenShift project (replace the `your-namespace` placeholder).
- `JIRA_EMAIL` and `JIRA_TOKEN`: read credentials.
- `GITLAB_TOKEN`: required read access to shipment MRs.
- `KONFLUX_KUBECONFIG`: path to a self-contained Konflux reader kubeconfig.
- Optional `GITHUB_TOKEN` for GitHub rate limits.
- Optional `IMAGE_PULL_SECRET_FILE`: Docker/Podman auth JSON for a private image.
- Optional `ROUTE_HOST`, `STORAGE_CLASS`, `STORAGE_SIZE` (default `5Gi`).

[.env.example](.env.example) is the complete set of supported deployment inputs.
The script rejects keys absent from this template, including token-file variables
and source/worker overrides. Omitted keys use the template defaults; required
credentials and the Konflux kubeconfig path must be filled in. API tokens are
supplied inline, while the kubeconfig and optional image-pull auth use file paths.

Source URLs, ART groups, project, worker counts, poll intervals, lookback, and
history budgets remain internal defaults in [base/app.env](base/app.env).
`PROW_JOB_DISCOVERY=true` and `JIRA_WRITE=false` remain enforced. No second
application env file needs to be prepared. Use a dedicated deployment `.env`
rather than passing an unrelated Compose env file with additional keys.

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
oc project your-namespace
# If the project does not exist and you have permission: oc new-project your-namespace
oc apply -k deploy/.generated
oc -n your-namespace rollout status deployment/release-readiness --timeout=300s
oc -n your-namespace get route release-readiness -o jsonpath='https://{.spec.host}{"\n"}'
oc -n your-namespace logs deployment/release-readiness --tail=100
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
oc -n your-namespace rollout restart deployment/release-readiness
oc -n your-namespace rollout status deployment/release-readiness --timeout=300s
```

Older generated Secrets and ConfigMaps are retained by `oc apply`; remove them
only after confirming they are no longer referenced by workloads or needed for
rollback. Do not use broad pruning that could remove the database PVC.

To stop the app while preserving its database and configuration:

```bash
oc -n your-namespace scale deployment/release-readiness --replicas=0
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
