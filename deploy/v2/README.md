# OpenShift setup for v2.0

Image: `quay.io/nasonawa/release-readness:v2.0` (Linux amd64).

Run from the repository root. These are operator-run commands: preparing this guide does not create Secrets, deploy workloads, or change routes. The reviewed target project is `nasonawa`; it currently runs the static `quay-readiness-proto2` application. This manifest creates `release-readiness` with the backend and persistent database. An image-only update of the static prototype does not supply its required configuration.

## Secrets: commands to run yourself

```bash
# Prepare private local files. Never commit these files.
RR_SECRET_DIR=$(mktemp -d)
chmod 700 "$RR_SECRET_DIR"
cp deploy/v2/jira.env.example "$RR_SECRET_DIR/jira.env"
chmod 600 "$RR_SECRET_DIR/jira.env"
${EDITOR:-vi} "$RR_SECRET_DIR/jira.env"

# Required for release discovery and issue ingestion.
oc -n nasonawa create secret generic release-readiness-jira \
  --from-env-file="$RR_SECRET_DIR/jira.env" \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -

# Required for this manifest: a self-contained KONFLUX kubeconfig.
# Use a Konflux reader credential, not the target ROSA cluster kubeconfig.
# Its current context needs list snapshots/releases in art-quay-tenant;
# image scans also need get/list Tekton Results results/records and a bearer token.
oc --kubeconfig=/path/to/konflux-reader.kubeconfig \
  config view --minify --flatten --raw > "$RR_SECRET_DIR/konflux.kubeconfig"
chmod 600 "$RR_SECRET_DIR/konflux.kubeconfig"
oc -n nasonawa create secret generic release-readiness-konflux \
  --from-file=kubeconfig="$RR_SECRET_DIR/konflux.kubeconfig" \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -

# Optional: GitLab read_api token for private shipment MR data.
# /path/to/gitlab-token contains only the token, without a trailing newline.
oc -n nasonawa create secret generic release-readiness-gitlab \
  --from-file=token=/path/to/gitlab-token \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -

# Optional: read-only GitHub token for commit lookups / higher API rate limits.
# /path/to/github-token contains only the token, without a trailing newline.
oc -n nasonawa create secret generic release-readiness-github \
  --from-file=token=/path/to/github-token \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -

# Only if the image repository is private: use a Quay robot with pull access.
podman login --authfile "$RR_SECRET_DIR/quay-pull.json" quay.io
oc -n nasonawa create secret generic release-readiness-pull \
  --type=kubernetes.io/dockerconfigjson \
  --from-file=.dockerconfigjson="$RR_SECRET_DIR/quay-pull.json" \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -
oc -n nasonawa create serviceaccount release-readiness \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -
oc -n nasonawa secrets link release-readiness release-readiness-pull --for=pull
```

`jira.env` supplies `JIRA_EMAIL`, `JIRA_TOKEN`, `JIRA_URL`, `JIRA_API_URL`, and `JIRA_PROJECT`. Use the scoped-token API URL if your credential requires it. The Konflux kubeconfig must contain embedded CA data and a credential usable without a host-side login plugin or local file references. Rotate expiring bearer tokens and update the Secret. A reader RoleBinding on the target ROSA cluster cannot grant access to a separate Konflux cluster: have the Konflux namespace administrator authorize that identity there. The existing `deploy/rbac.yaml` documents the minimal resource permissions, but its service-account subject applies only when running on the same cluster.

There is no Prow, Sippy, ART-history, or catalog Secret in this setup. Their configured endpoints are read directly; image scans reuse the Konflux bearer token. JIRA writes remain off; a JIRA writer Secret is not required.

## First deployment

```bash
# Required non-secret validation plan; all planned jobs remain in the denominator.
oc -n nasonawa create configmap release-readiness-validation \
  --from-file=quay-validation-plan.json=config/quay-validation-plan.json \
  --dry-run=client -o yaml | oc -n nasonawa apply -f -

oc -n nasonawa apply -f deploy/v2/app.yaml
oc -n nasonawa rollout status deployment/release-readiness --timeout=300s
oc -n nasonawa get pvc release-readiness-data
oc -n nasonawa get route release-readiness
oc -n nasonawa logs deployment/release-readiness --tail=100
RR_ROUTE=$(oc -n nasonawa get route release-readiness -o jsonpath='{.spec.host}')
curl -fsS "https://${RR_ROUTE}/api/v1/health"
curl -fsS "https://${RR_ROUTE}/api/v1/sync-status"
```

The PVC uses the cluster default class (`gp3-csi` on the reviewed cluster), one replica, and Recreate updates for SQLite. A new PVC starts empty and populates through source synchronization; the local review database is not included in the image. Health confirms HTTP availability, not successful upstream ingestion. Inspect sync status before interpreting readiness.

The trusted CA ConfigMap is filled by OpenShift. If internal ART/GitLab endpoints use an additional corporate CA, the cluster's trusted bundle must include it. Do not disable TLS verification. The manifest runs under the normal OpenShift non-root security policy, without a hardcoded UID. The Route uses TLS but this app has no login layer; use your approved access controls for a dashboard containing internal release data.

## Keep the existing prototype URL (optional, after verifying v2)

```bash
oc -n nasonawa patch route quay-readiness-proto2 --type=merge \
  -p '{"spec":{"to":{"kind":"Service","name":"release-readiness"},"port":{"targetPort":"http"}}}'

# Reverse this route switch if needed; the prototype deployment is retained.
oc -n nasonawa patch route quay-readiness-proto2 --type=merge \
  -p '{"spec":{"to":{"kind":"Service","name":"quay-readiness-proto2"},"port":{"targetPort":"http"}}}'
```

Run only the first patch to switch to v2. The second is the rollback command.

## Later image and configuration updates

```bash
podman build -f deploy/Containerfile -t quay.io/nasonawa/release-readness:v2.0 .
podman push quay.io/nasonawa/release-readness:v2.0

oc -n nasonawa set image deployment/release-readiness \
  dashboard=quay.io/nasonawa/release-readness:v2.0
# Required when reusing the same tag; imagePullPolicy is Always.
oc -n nasonawa rollout restart deployment/release-readiness
oc -n nasonawa rollout status deployment/release-readiness --timeout=300s
```

Prefer pinning a verified `quay.io/nasonawa/release-readness@sha256:...` digest for reproducible updates. Changing a digest triggers a rollout without a separate restart. Reapplying `app.yaml` restores its tag unless you update that file too. Record the previous digest before changing it; rollback to that digest rather than trusting a reused tag. Back up the PVC before upgrades that migrate data.

After changing the validation ConfigMap or a Secret, restart the deployment: the plan and environment credentials are loaded at startup, and the Kubernetes client also needs to reload rotated credentials. `PROW_JOB_PATTERN` includes operator, legacy, upgrade, and standalone periodics; the validation plan itself does not enable ingestion. The checked-in plan is a revisioned snapshot; standalone identity matching and an OMR plan remain follow-up work.

Reference: [OpenShift 4.22 image pull secrets](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/images/managing-images#images-allow-pods-to-reference-images-from-secure-registries_using-image-pull-secrets).
