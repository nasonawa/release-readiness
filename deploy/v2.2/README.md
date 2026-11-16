# OpenShift deployment: v2.2

Target server: `https://api.quaydev-hcp.28db.p3.openshiftapps.com:443`  
Project: `nasonawa`  
Image: `quay.io/nasonawa/release-readness:v2.2@sha256:e4a1fee6e680fbfcfb0c53ecef2c674ec7eeece5ffa0e6f4ed7814c14cfaacd6`

The manifest uses the existing `release-readiness-data` PVC, dynamic periodic job discovery, Sippy, and two-minute Prow polling. It does not mount the prototype's static validation plan. JIRA writes remain disabled. Shipment MR ingestion uses the configured GitLab reader token.

Required Secrets: `release-readiness-jira` (JIRA read environment variables) and `release-readiness-konflux` (a self-contained Konflux reader kubeconfig under key `kubeconfig`). Optional `release-readiness-gitlab` supplies key `token` for private shipment MR access. No credential values belong in this directory. GitHub access uses anonymous requests unless a separately supplied optional Secret exists.

Apply and verify with the explicit target context:

```bash
RR_CONTEXT=nasonawa/api-quaydev-hcp-28db-p3-openshiftapps-com:443/nasonawa
oc --context="$RR_CONTEXT" -n nasonawa apply -f deploy/v2.2/app.yaml
oc --context="$RR_CONTEXT" -n nasonawa rollout status deployment/release-readiness --timeout=300s
oc --context="$RR_CONTEXT" -n nasonawa get route release-readiness
oc --context="$RR_CONTEXT" -n nasonawa logs deployment/release-readiness --tail=100
```

Health checks confirm the app is serving; per-source ingestion health is reported by `/api/v1/sync-status`. Existing database content is retained and schema migrations run in place. This manifest creates its own TLS Route and does not change the prototype's route.

## Verified route

https://release-readiness-nasonawa.apps.rosa.quaydev-hcp.28db.p3.openshiftapps.com

November 16 verification: one ready replica, correct registry digest and app version v2.2, healthy HTTPS API, and 13 retained release records. JIRA and periodic inventory synchronized. The cluster cannot resolve internal GitLab and its connection to the private Konflux API address times out; those sources require appropriate cluster network/DNS access before their data can refresh.
