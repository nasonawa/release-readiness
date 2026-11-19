# Release Readiness Dashboard

A release-readiness dashboard for Red Hat Quay and OMR. It brings JIRA release
scope, ART builds, Konflux promotion state, Prow CI evidence, and shipment merge
requests into one place for engineering, product, and leadership teams.

The portfolio groups minor-version streams into cards and shows concrete patch
releases in stages. Each release has a briefing, validation workbench, and issue
scope. Validation distinguishes a job's outcome from evidence that it tested the
selected FBC. The dashboard observes existing CI runs; it does not launch jobs
or approve shipments.

One Go binary serves the embedded React/PatternFly UI and API, with SQLite for
persistence. Background collectors prioritize current releases over history.

## Prerequisites

- Docker Engine with the Docker Compose plugin for local containers.
- Node.js 24 and the Go version specified in `go.mod` for source builds/tests.
- JIRA read credentials for release discovery and a Konflux reader kubeconfig
  for build/promotion data. GitLab read access enables private shipment MR lookup.
- `oc` and Python 3.9+ for preparing the OpenShift Kustomize deployment.

## Run locally

Create a repository-root `.env` containing `JIRA_EMAIL`, `JIRA_TOKEN`, and, for
private shipment MRs, `GITLAB_TOKEN`. GitHub credentials are optional. The Compose
setup mounts `~/.kube/config` read-only; its selected context must be the Konflux
reader context. The app needs network access to its configured source services.

```bash
touch .env
chmod 600 .env
# Fill in the credentials before starting.
docker compose up -d --build
docker compose logs -f
```

Open [http://localhost:8088](http://localhost:8088). Use `RR_PORT=8089` before the
Compose command to choose another host port. The named volume preserves SQLite
data; JIRA writes are disabled. An empty database without working JIRA reads
will not yet have releases to display.

```bash
docker compose down
```

This removes containers and the network while preserving the database volume.
Do not add `--volumes` unless you intend to delete its data.

## Deploy to OpenShift

Use an image built from the current code. Replace `your-org` in the example
`quay.io/your-org/release-readiness:v2.2` with your registry organization, and
`your-namespace` with your target OpenShift project. Follow the
[deployment guide](deploy/README.md) for credentials, cluster selection, storage,
and access controls. In the intended application-cluster context:

```bash
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
# Fill the supported settings and credential paths in deploy/.env.
python3 deploy/configure.py
oc project your-namespace  # Match NAMESPACE in deploy/.env.
oc apply -k deploy/.generated
oc -n your-namespace rollout status deployment/release-readiness --timeout=300s
oc -n your-namespace get route release-readiness -o jsonpath='https://{.spec.host}{"\n"}'
```

The deployment includes the app, persistent storage, Service, TLS Route, and
Secrets. The app has no built-in login layer; use your organization's access
controls. Back up the PVC before upgrades; supported database migrations run
in place.

## Build

Build the frontend first: Go embeds `web/dist/`.

```bash
npm --prefix web ci
npm --prefix web run build
go build -o release-readiness ./cmd/release-readiness/
```

To build a container image:

```bash
docker build -f deploy/Containerfile --build-arg APP_VERSION=v2.2 \
  -t quay.io/your-org/release-readiness:v2.2 .
```

Building does not publish the image. Use a unique tag or digest for reproducible
production rollouts.

## Test

After installing dependencies and building the frontend:

```bash
go test ./...
go vet ./...
golangci-lint run
npm --prefix web test
npm --prefix web run lint
npm --prefix web run typecheck:e2e
(cd web && npx playwright install chromium && npm run test:e2e)
python3 -m unittest discover -s deploy -p 'test_*.py'
```

Use a Go-compatible `golangci-lint` version. Deployment render tests need `oc` and
PyYAML. Browser tests use mocked APIs and a temporary frontend, without backend
credentials. See the [Playwright guide](web/e2e/README.md) for screenshots,
accessibility checks, and comparison tools.

## Documentation

- [Architecture](docs/architecture.md): components, workflow diagram, persistence.
- [How readiness works](docs/how-it-works.md): portfolio, releases, FBC matching,
  validation, shipment status, and API semantics.
- [Synchronization](docs/syncing.md): sources, scheduling, caching, history,
  retention, and freshness.
- [CLI and configuration reference](docs/cli-reference.md): flags, environment
  mappings, defaults, and deployment configuration boundaries.
- [OpenShift deployment](deploy/README.md): Kustomize, credentials, upgrades.
- [Frontend tests and review tools](web/e2e/README.md): browser testing and visuals.
