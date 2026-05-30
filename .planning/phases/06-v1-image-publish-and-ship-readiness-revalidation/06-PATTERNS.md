# Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation — Pattern Map

**Mapped:** 2026-05-30
**Files analyzed:** 15 (6 Dockerfiles, 1 CI workflow, 1 Helm SOT, 1 generated chart, 2 bash scripts, 1 new bash helper, 2 Makefile targets, 3 doc/config files)
**Analogs found:** 15 / 15 — every file has a concrete in-repo analog

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `.github/workflows/release.yaml` (MODIFY — add `build-images` job) | CI job | request-response (tag-triggered) | Existing `chart-publish` job in same file (lines 231-281) | exact |
| `./Dockerfile` (MODIFY — `--platform=$BUILDPLATFORM`) | Dockerfile | transform (cross-compile) | `./Dockerfile` itself (lines 1-32) — existing `ARG TARGETOS/TARGETARCH` pattern | exact |
| `./Dockerfile.dashboard` (MODIFY) | Dockerfile | transform (cross-compile) | `./Dockerfile` (same builder shape, same `golang:1.26 AS builder`) | exact |
| `images/stub-subagent/Dockerfile` (MODIFY) | Dockerfile | transform (cross-compile) | `images/credproxy/Dockerfile` (identical structure: alpine builder + distroless) | exact |
| `images/credproxy/Dockerfile` (MODIFY) | Dockerfile | transform (cross-compile) | `images/stub-subagent/Dockerfile` (identical structure) | exact |
| `images/tide-push/Dockerfile` (MODIFY) | Dockerfile | transform (cross-compile) | `images/credproxy/Dockerfile` (same alpine/distroless pattern) | exact |
| `images/claude-subagent/Dockerfile` (MODIFY — Go builder stage only) | Dockerfile | transform (cross-compile + npm runtime) | `images/stub-subagent/Dockerfile` for builder stage; self-referential for node runtime stage | role-match |
| `hack/helm/tide-values.yaml` (MODIFY — 5 `v0.1.0-dev` → `""`) | Helm SOT config | transform (tag resolution) | `hack/helm/tide-values.yaml` line 244 (`dashboard.image.tag: ""`) | exact |
| `charts/tide/values.yaml` (REGENERATED — not hand-edited) | generated config | — | `hack/helm/augment-tide-chart.sh` + `make helm` propagation | exact (anti-pattern guard) |
| `hack/scripts/load-images-if-needed.sh` (CREATE) | bash utility | file-I/O + event-driven (docker/kind) | `Makefile` lines 153-167 (`test-int-kind-prep` docker build + kind load pattern) | role-match |
| `hack/scripts/acceptance-v1.sh` (MODIFY — $0 mode + image-load call) | bash script | request-response (orchestration) | Self-referential: cert-manager block (lines 61-74); env-gate block (lines 26-28) | exact |
| `hack/scripts/dry-run-v1.sh` (MODIFY — cert-manager + image-load) | bash script | request-response (DinD orchestration) | `hack/scripts/acceptance-v1.sh` cert-manager block (lines 68-74) for DRY-01; self-referential heredoc (lines 55-87) for placement | exact |
| `Makefile` (MODIFY — add `acceptance-v1-smoke` and/or `docker-buildx-snapshot`) | Makefile targets | request-response (CLI) | `acceptance-v1` target (lines 566-573); `docker-buildx` target (lines 261-269) | exact |
| `.gitignore` (MODIFY — add `.acceptance-runs/`) | config | — | Existing `.gitignore` entries (lines 19-20: `/dist/` goreleaser output pattern) | exact |
| `docs/troubleshooting.md` (MODIFY — ImagePullBackOff row) | docs | — | Existing table rows (lines 27-28: ImagePullBackoff + tag-drift rows) | exact |
| `docs/INSTALL.md` (MODIFY — image-publish section) | docs | — | Existing INSTALL.md §Prerequisites + §First Project apply | role-match |
| `examples/projects/small/project.yaml` (MODIFY — `v1.0.0` → `1.0.0`) | sample config | — | Same file line 49 — the tag value to fix; dashboard SOT `tag: ""` pattern as the appVersion convention | exact |

---

## Pattern Assignments

### `.github/workflows/release.yaml` — add `build-images` job (IMG-01)

**Analog:** Existing `chart-publish` job in `.github/workflows/release.yaml` (lines 231-281)

**Key extracted patterns:**

**GHCR login pattern** (lines 254-256):
```yaml
- name: Helm registry login (ghcr.io)
  run: |
    echo "${{ secrets.GITHUB_TOKEN }}" | helm registry login ghcr.io -u ${{ github.actor }} --password-stdin
```
The `--password-stdin` pattern keeps the token off the command line. The `build-images` job uses `docker/login-action` which applies the same principle via the action's native secrets handling.

**`packages: write` permission block** (lines 237-240):
```yaml
permissions:
  contents: read
  packages: write   # Pitfall 3 — REQUIRED for ghcr.io OCI push
```
Copy this exact block onto the `build-images` job. Only this job needs `packages: write`; all other jobs in `release.yaml` retain `contents: read` only.

**`if: !contains(rc)` guard + `needs:` ordering** (lines 233-234):
```yaml
if: ${{ !contains(github.ref, '-rc.') }}
needs: release
```
The `build-images` job uses the same `if:` guard. Its `needs:` is `[helmify-verify]` (not `release`) because images should build in parallel with goreleaser, not after. The `chart-publish` job's `needs:` must be extended to `[build-images, release]`.

**Tag strip pattern** (lines 263-264):
```yaml
helm package charts/tide-crds --version "${GITHUB_REF_NAME#v}" --app-version "${GITHUB_REF_NAME#v}"
helm push tide-crds-${GITHUB_REF_NAME#v}.tgz oci://ghcr.io/jsquirrelz/tide-charts
```
The `${GITHUB_REF_NAME#v}` bash parameter expansion strips the leading `v` from a `v1.0.0` tag to get `1.0.0`. The `build-images` job's image tag derivation must use this same expansion — NOT `${{ github.ref_name }}` raw — to produce `1.0.0` (no `v`) matching chart `appVersion`.

**GHA cache pattern** (absent in chart-publish but standard for buildx):
```yaml
cache-from: type=gha,scope=${{ matrix.component }}
cache-to: type=gha,mode=max,scope=${{ matrix.component }}
```
Use per-component GHA cache scope so 6 matrix jobs do not clobber each other's cache layers.

**`chart-publish` `needs:` extension** — add `build-images` to the existing `needs: release` (line 234):
```yaml
# BEFORE (line 234):
needs: release
# AFTER:
needs: [build-images, release]
```

---

### `./Dockerfile` (MODIFY — D-02 `--platform=$BUILDPLATFORM`) (tide-controller)

**Analog:** Self — existing file (lines 1-32)

**Current pattern** (lines 1-4):
```dockerfile
FROM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH
```

**Target pattern** (one-line change):
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH
```

**What stays unchanged:** Everything else — `WORKDIR`, `COPY go.mod/go.sum`, `RUN go mod download`, `COPY .`, `RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager ./cmd/manager`, and the entire runtime `FROM gcr.io/distroless/static:nonroot` stage.

**No `ARG BUILDPLATFORM` needed:** BuildKit provides `BUILDPLATFORM` automatically when `--platform=$BUILDPLATFORM` is used. The existing `ARG TARGETOS` and `ARG TARGETARCH` lines remain required to scope those ARGs into the builder stage.

---

### `./Dockerfile.dashboard` (MODIFY — D-02) (tide-dashboard)

**Analog:** `./Dockerfile` (identical Go builder structure)

**Current pattern** (line 16):
```dockerfile
FROM golang:1.26 AS builder
```

**Target pattern**:
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
```

`Dockerfile.dashboard` already has `ARG TARGETOS` and `ARG TARGETARCH` (lines 17-18). The runtime stage `FROM gcr.io/distroless/static:nonroot` is unchanged. No node stage — same one-line fix as `./Dockerfile`.

---

### `images/stub-subagent/Dockerfile` (MODIFY — D-02) (tide-stub-subagent)

**Analog:** `images/credproxy/Dockerfile` (structurally identical: `golang:1.26-alpine AS builder` + `gcr.io/distroless/static:nonroot`)

**Current pattern** (line 7):
```dockerfile
FROM golang:1.26-alpine AS builder
```

**Target pattern**:
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
```

Note: the `RUN CGO_ENABLED=0 GOOS=linux go build` line (line 20) does NOT pass `GOARCH` — it will need `ARG TARGETARCH` added and `GOARCH=${TARGETARCH}` in the `go build` command to actually cross-compile. Current form only cross-compiles GOOS; add:
```dockerfile
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/stub-subagent \
      ./cmd/stub-subagent
```

---

### `images/credproxy/Dockerfile` (MODIFY — D-02) (tide-credproxy)

**Analog:** `images/stub-subagent/Dockerfile` (same structure)

**Current pattern** (line 7):
```dockerfile
FROM golang:1.26-alpine AS builder
```

**Target pattern**:
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
```

Same `ARG TARGETOS / ARG TARGETARCH` + `GOARCH=${TARGETARCH}` addition as stub-subagent. The `RUN CGO_ENABLED=0 GOOS=linux go build` line (line 19) needs the same TARGETARCH pass-through.

---

### `images/tide-push/Dockerfile` (MODIFY — D-02) (tide-push)

**Analog:** `images/credproxy/Dockerfile` (identical alpine/distroless pattern)

**Current pattern** (line 7):
```dockerfile
FROM golang:1.26-alpine AS builder
```

**Target pattern**:
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
```

Same `ARG TARGETOS / ARG TARGETARCH` + `GOARCH=${TARGETARCH}` in the `go build` RUN (line 26).

---

### `images/claude-subagent/Dockerfile` (MODIFY — D-02, Go builder stage only) (tide-claude-subagent)

**Analog:** `images/stub-subagent/Dockerfile` for the builder stage; self-referential for the `FROM node:22-slim` runtime stage.

**Current builder pattern** (line 7):
```dockerfile
FROM golang:1.26-alpine AS builder
```

**Target builder pattern** (one line):
```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
```

Same `ARG TARGETOS / ARG TARGETARCH` + `GOARCH=${TARGETARCH}` addition as credproxy/stub-subagent (line 22 `go build`).

**Runtime stage — unchanged** (lines 35-43):
```dockerfile
FROM node:22-slim

RUN npm install -g @anthropic-ai/claude-code@2.1.142
```
The `FROM node:22-slim` runtime stage does NOT get `--platform=$BUILDPLATFORM`. BuildKit + QEMU (registered by `docker/setup-qemu-action` in CI) handles arm64 for this stage. QEMU is slower but unavoidable for the Node runtime. Job timeout must be 30 minutes to accommodate QEMU arm64 `npm install`.

---

### `hack/helm/tide-values.yaml` (MODIFY — CHART-01 SOT tag alignment)

**Analog:** Same file — line 244 (`dashboard.image.tag: ""`) is the proven pattern that resolves to `.Chart.AppVersion`.

**Five lines to change** (current → target):

| Line | Key | Current | Target |
|------|-----|---------|--------|
| 39 | `controllerManager.manager.image.tag` | `v0.1.0-dev` | `""` |
| 140 | `images.stubSubagent.tag` | `v0.1.0-dev` | `""` |
| 144 | `images.credProxy.tag` | `v0.1.0-dev` | `""` |
| 155 | `images.tidePush.tag` | `v0.1.0-dev` | `""` |
| 165 | `images.claudeSubagent.tag` | `v0.1.0-dev` | `""` |

**Line 148 — DO NOT TOUCH** (busybox, third-party):
```yaml
tag: "1.36"   # preserve — busybox, not a TIDE component
```

**Proven working pattern** (line 244, dashboard):
```yaml
dashboard:
  image:
    repository: ghcr.io/jsquirrelz/tide-dashboard
    tag: ""             # default to .Chart.AppVersion
    pullPolicy: IfNotPresent
```

**Propagation command** (run after SOT edit):
```bash
make helm   # runs bash hack/helm/augment-tide-chart.sh → cp tide-values.yaml → charts/tide/values.yaml
```

**Verification after propagation:**
```bash
grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml  # must return 0
helm template charts/tide | grep -E 'image:'    # all 6 TIDE images must show :1.0.0
grep -E 'tag: "1\.36"' hack/helm/tide-values.yaml  # must still be present
```

---

### `charts/tide/values.yaml` (GENERATED — do not hand-edit)

**Anti-pattern guard:** This file is generated output (`bash hack/helm/augment-tide-chart.sh` step 2 does `cp "${HACK_DIR}/tide-values.yaml" "${CHART_DIR}/values.yaml"`). The only valid mutation is running `make helm` after editing the SOT. Direct edits violate the chart-vs-binary anti-pattern in CLAUDE.md and will be clobbered on the next `make helm`.

---

### `hack/scripts/load-images-if-needed.sh` (CREATE — IMG-LOAD-01)

**Analog:** `Makefile` lines 153-167 (`test-int-kind-prep` target) for the `docker build` + `kind load docker-image --name` pattern.

**Existing analog pattern** (Makefile lines 153-167):
```makefile
test-int-kind-prep:
    $(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-stub-subagent:test -f images/stub-subagent/Dockerfile .
    $(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-credproxy:test -f images/credproxy/Dockerfile .
    $(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-push:test -f images/tide-push/Dockerfile .
    $(CONTAINER_TOOL) build -t controller:test -f Dockerfile .
    @if ! $(KIND) get clusters 2>/dev/null | grep -q "^tide-test$$"; then \
        $(KIND) create cluster --name tide-test --config test/integration/kind/cluster.yaml; \
    fi
    $(KIND) load docker-image ghcr.io/jsquirrelz/tide-stub-subagent:test --name tide-test
    $(KIND) load docker-image ghcr.io/jsquirrelz/tide-credproxy:test --name tide-test
    $(KIND) load docker-image ghcr.io/jsquirrelz/tide-push:test --name tide-test
    $(KIND) load docker-image controller:test --name tide-test
```

**Critical divergence from analog:** `test-int-kind-prep` hardcodes `--name tide-test`. The new helper must use a dynamic `${cluster_name}` argument — NOT delegate to `test-int-kind-prep`. Images loaded into `tide-test` do not reach the dynamically-named `tide-acceptance-<ts>` cluster.

**Script structure to copy** (from `acceptance-v1.sh` header pattern, lines 1-23):
```bash
#!/usr/bin/env bash
# [description]
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
```

**Detection method:** `docker manifest inspect` (no layer download; exit code 0 = image exists in registry):
```bash
if docker manifest inspect "${img}" > /dev/null 2>&1; then
    echo "==> ${img}: found in registry, skipping local build"
else
    echo "==> ${img}: not found in registry, building locally..."
    docker build -t "${img}" -f "${df}" "${REPO_ROOT}"
    kind load docker-image "${img}" --name "${cluster_name}"
fi
```

**6-image list with dockerfiles** (mirroring D-03 fixed list):
```bash
local images=(
    "ghcr.io/jsquirrelz/tide-controller"
    "ghcr.io/jsquirrelz/tide-dashboard"
    "ghcr.io/jsquirrelz/tide-stub-subagent"
    "ghcr.io/jsquirrelz/tide-credproxy"
    "ghcr.io/jsquirrelz/tide-push"
    "ghcr.io/jsquirrelz/tide-claude-subagent"
)

local dockerfiles=(
    "./Dockerfile"
    "./Dockerfile.dashboard"
    "images/stub-subagent/Dockerfile"
    "images/credproxy/Dockerfile"
    "images/tide-push/Dockerfile"
    "images/claude-subagent/Dockerfile"
)
```

**Image tag:** `1.0.0` (no `v` prefix — matches chart `appVersion` after CHART-01). Pre-tag the pull fails → local build at `:1.0.0` matches what `helm template` resolves.

**A7 handling** (RESEARCH open question): `examples/projects/small/project.yaml` line 49 says `v1.0.0` (with `v`). The planner must either:
1. Fix `project.yaml` to `1.0.0` (recommended — matches appVersion convention), OR
2. Have the helper also `docker tag` the built image as both `:1.0.0` and `:v1.0.0`.

---

### `hack/scripts/acceptance-v1.sh` (MODIFY — $0 mode + image-load call)

**Analog:** Self — existing cert-manager block (lines 61-74) for the pattern; existing env-gate block (lines 26-28) for conditional guard shape.

**$0 mode env-gate shape** — mirrors existing fail-fast pattern (lines 26-28):
```bash
# Existing env-gate (lines 26-28) — copy this conditional shape:
: "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required...}"
: "${GH_PAT:?GH_PAT required...}"
```
In $0 mode (`ACCEPTANCE_SAMPLE=small`), skip these two gates. Pattern:
```bash
ACCEPTANCE_SAMPLE="${ACCEPTANCE_SAMPLE:-large}"

if [ "${ACCEPTANCE_SAMPLE}" != "small" ]; then
  : "${ANTHROPIC_API_KEY:?ANTHROPIC_API_KEY required for acceptance-v1 — see docs/INSTALL.md}"
  : "${GH_PAT:?GH_PAT required for git creds Secret — see docs/INSTALL.md}"
fi
```

**Image-load call placement** — replaces the existing broken `ACCEPTANCE_LOAD_IMAGES` block (lines 55-59):
```bash
# BEFORE (lines 55-59) — broken; delegates to test-int-kind-prep which hardcodes --name tide-test:
if [ -n "${ACCEPTANCE_LOAD_IMAGES:-}" ]; then
  KIND_CLUSTER="${CLUSTER_NAME}" make -C "${REPO_ROOT}" test-int-kind-prep || true
fi

# AFTER — replace entirely with:
IMAGE_TAG="1.0.0"  # matches chart appVersion post CHART-01
bash "${REPO_ROOT}/hack/scripts/load-images-if-needed.sh" "${CLUSTER_NAME}" "${IMAGE_TAG}"
```

**$0 sample branch shape** — mirrors existing large-sample apply block (lines 82-102). In $0 mode, skip Secret creation and large-project apply; substitute:
```bash
if [ "${ACCEPTANCE_SAMPLE}" = "small" ]; then
  echo "==> applying examples/projects/small/project.yaml ($0 stub mode)..."
  kubectl apply -f "${REPO_ROOT}/examples/projects/small/project.yaml"
  kubectl wait --for=jsonpath="{.status.phase}"=Complete \
    "project/small-project" \
    -n "tide-sample-small" \
    --timeout=10m
else
  # existing large-sample block unchanged
  ...
fi
```

---

### `hack/scripts/dry-run-v1.sh` (MODIFY — DRY-01 cert-manager + IMG-LOAD-01)

**Analog:** `hack/scripts/acceptance-v1.sh` lines 68-74 (the cert-manager block to mirror verbatim).

**DRY-01 cert-manager block to insert** — source (acceptance-v1.sh lines 68-74):
```bash
CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"
echo "==> installing cert-manager ${CERT_MANAGER_VERSION}..."
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
echo "==> waiting for cert-manager Deployments to roll out..."
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status deployment/"${deploy}" --timeout=120s
done
```

**Placement in DinD heredoc** — between `kind create cluster --name tide-dry-run` (line 80) and `helm install tide-crds` (line 81):
```bash
kind create cluster --name tide-dry-run
# DRY-01: cert-manager block goes here (inside heredoc)
CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status deployment/"${deploy}" --timeout=120s
done
helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
```

**`TIDE_CERT_MANAGER_VERSION` propagation in DinD:** Pass the env var explicitly into the `docker run` call via `-e TIDE_CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"`, or hardcode the default inside the heredoc. The heredoc pattern with `${TIDE_CERT_MANAGER_VERSION:-v1.20.2}` directly is simpler (bash variable expansion works inside double-quoted heredocs).

**IMG-LOAD-01 in dry-run-v1.sh** — the planner must decide the DinD split (RESEARCH Open Question 1). Recommended: call `load-images-if-needed.sh` from the OUTER script (before `docker run`) since `--network host` + `/var/run/docker.sock` mount means the outer script can reach the kind cluster created inside DinD. Requires splitting the heredoc into two `docker run` passes: first creates the cluster, then the outer script loads images, then the second pass runs helm install + wait.

---

### `Makefile` (MODIFY — add `acceptance-v1-smoke` + `docker-buildx-snapshot`)

**Analog:** Existing `acceptance-v1` target (lines 562-573) for `acceptance-v1-smoke`; existing `docker-buildx` target (lines 261-269) for `docker-buildx-snapshot`.

**`acceptance-v1` pattern to extend** (lines 562-573):
```makefile
.PHONY: acceptance-v1
acceptance-v1: ## Phase 5 D-A4 — maintainer ritual ($25 hard cap; requires ANTHROPIC_API_KEY).
    @if [ -z "$$ANTHROPIC_API_KEY" ]; then \
      echo "ERROR: ANTHROPIC_API_KEY env not set — refusing to run acceptance-v1"; \
      echo "       See docs/INSTALL.md for Secret setup."; \
      exit 1; \
    fi
    @hack/scripts/acceptance-v1.sh
```

**New `acceptance-v1-smoke` target shape** (copy pattern, remove env guard, add ACCEPTANCE_SAMPLE):
```makefile
.PHONY: acceptance-v1-smoke
acceptance-v1-smoke: ## Phase 6 ACC-01 — $0 BOOT-04 revalidation (stub-subagent, no API key required).
    @ACCEPTANCE_SAMPLE=small hack/scripts/acceptance-v1.sh
```

**`docker-buildx` pattern for reference** (lines 261-269):
```makefile
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
    sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
    - $(CONTAINER_TOOL) buildx create --name tide-builder
    $(CONTAINER_TOOL) buildx use tide-builder
    - $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
    - $(CONTAINER_TOOL) buildx rm tide-builder
    rm Dockerfile.cross
```

**New `docker-buildx-snapshot` target shape** (load only, no push, all 6 images):
```makefile
IMAGE_TAG ?= 1.0.0

.PHONY: docker-buildx-snapshot
docker-buildx-snapshot: ## Phase 6 IMG-01 — local multi-arch snapshot build of all 6 component images (no push).
    @echo "Building all 6 component images for linux/amd64,linux/arm64 (no push)..."
    $(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
        -t ghcr.io/jsquirrelz/tide-controller:$(IMAGE_TAG) -f ./Dockerfile .
    $(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
        -t ghcr.io/jsquirrelz/tide-dashboard:$(IMAGE_TAG) -f ./Dockerfile.dashboard .
    $(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
        -t ghcr.io/jsquirrelz/tide-stub-subagent:$(IMAGE_TAG) -f images/stub-subagent/Dockerfile .
    $(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
        -t ghcr.io/jsquirrelz/tide-credproxy:$(IMAGE_TAG) -f images/credproxy/Dockerfile .
    $(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
        -t ghcr.io/jsquirrelz/tide-push:$(IMAGE_TAG) -f images/tide-push/Dockerfile .
    $(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
        -t ghcr.io/jsquirrelz/tide-claude-subagent:$(IMAGE_TAG) -f images/claude-subagent/Dockerfile .
```

---

### `.gitignore` (MODIFY — HYG-01)

**Analog:** Existing entries using the `/dist/` pattern (line 20) — goreleaser output.

**Pattern shape** (lines 19-20):
```
# goreleaser snapshot/release build output (plan 04-09 — D-C2)
/dist/
```

**New entry to append:**
```
# Phase 6 ACC-01 — acceptance run evidence archives (maintainer-local, never committed)
.acceptance-runs/
```

Note: `.acceptance-runs/` without leading `/` matches at any depth; using the bare path (not `/.acceptance-runs/`) is correct since the directory is repo-root-level.

---

### `docs/troubleshooting.md` (MODIFY — HYG-01 ImagePullBackOff row)

**Analog:** Existing row for controller `ImagePullBackoff` (line 27):
```markdown
| `Pod tide-controller-manager-…` stuck in `ImagePullBackoff` | Image pull secret missing for a private registry, OR `appVersion` in the chart doesn't match an available tag | `kubectl describe pod -n tide-system <pod>` shows the reason. If credentials: ... If tag drift: confirm `helm get values tide -n tide-system \| grep appVersion` matches a published image tag. |
```

**New row to append** (same `Symptom | Cause | Recipe` shape, Phase 5 D-C4):
```markdown
| `deploy/tide-controller-manager` or `tide-dashboard` pod stuck in `ImagePullBackOff` mid-install (immediately after `helm install`) | Chart references component images that have never been published to ghcr.io (pre-release state), OR chart tag pin (`v0.1.0-dev`) doesn't match any published tag | 1. Check if images exist: `docker manifest inspect ghcr.io/jsquirrelz/tide-controller:1.0.0`. 2. If not published: run `make acceptance-v1-smoke` (builds + kind-loads all 6 images locally — Phase 6 ACC-01). 3. Check chart tag pins: `grep -E 'v0\.1\.0-dev' charts/tide/values.yaml` — should return 0 after Phase 6. See [INSTALL.md](INSTALL.md) §Local image build. |
```

---

### `docs/INSTALL.md` (MODIFY — DOC-01 image-publish section)

**Analog:** Existing INSTALL.md §Prerequisites table (lines 19-25) and §First Project apply section for style; `release.yaml` for the factual pipeline description.

**Style pattern** (lines 19-25 — use same table format):
```markdown
| Tool      | Minimum version | Purpose |
| --------- | --------------- | ------- |
| Docker    | 24.x            | ...     |
```

**Content to add** — a new §Maintainer: image-publish section documenting:
1. How the 6 images publish (the `build-images` matrix job on `v*` tag push)
2. The `make acceptance-v1-smoke` local fallback path (pre-tag pre-publish)
3. GHCR visibility note (first push is private; set to Public at `github.com/users/jsquirrelz/packages/container/<name>/settings`)
4. Removal of premature "v1.0 ship-ready" language if present

---

### `examples/projects/small/project.yaml` (MODIFY — tag fix per RESEARCH A7)

**Analog:** `hack/helm/tide-values.yaml` line 244 (`tag: ""` pattern) as the appVersion convention.

**Current line 49:**
```yaml
image: ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0
```

**Target line 49** (fix `v` prefix — match chart appVersion `1.0.0` not goreleaser tag `v1.0.0`):
```yaml
image: ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0
```

**Why:** After CHART-01 the chart resolves `tide-stub-subagent` to tag `1.0.0`. If `project.yaml` still requests `v1.0.0`, the acceptance run must build and kind-load BOTH tags. Fixing `project.yaml` to `1.0.0` keeps both the chart and the sample consistently using appVersion convention (no `v` prefix).

---

## Shared Patterns

### cert-manager install + rollout-wait
**Source:** `hack/scripts/acceptance-v1.sh` lines 68-74
**Apply to:** `hack/scripts/dry-run-v1.sh` (DRY-01) — mirror verbatim into the DinD heredoc
```bash
CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"
echo "==> installing cert-manager ${CERT_MANAGER_VERSION}..."
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
echo "==> waiting for cert-manager Deployments to roll out..."
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status deployment/"${deploy}" --timeout=120s
done
```

### Tag stripping (`${GITHUB_REF_NAME#v}`)
**Source:** `.github/workflows/release.yaml` lines 263-264
**Apply to:** `build-images` job image tag derivation (produce `1.0.0` from `v1.0.0`)
```bash
echo "IMAGE_TAG=${GITHUB_REF_NAME#v}" >> "${GITHUB_ENV}"
```

### GHCR authentication (`--password-stdin`)
**Source:** `.github/workflows/release.yaml` lines 254-256
**Apply to:** `build-images` job login step — use `docker/login-action@v3` which handles `--password-stdin` internally; never pass `${{ secrets.GITHUB_TOKEN }}` inline as a positional argument

### `packages: write` permission scope
**Source:** `.github/workflows/release.yaml` lines 237-240
**Apply to:** `build-images` job only — job-level `permissions:` block; other jobs in `release.yaml` are unchanged
```yaml
permissions:
  contents: read
  packages: write
```

### `set -euo pipefail` + `REPO_ROOT` derivation
**Source:** `hack/scripts/acceptance-v1.sh` lines 22-24 and `hack/scripts/dry-run-v1.sh` lines 23-25
**Apply to:** `hack/scripts/load-images-if-needed.sh` — same header pattern
```bash
#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
```

### `kind load docker-image --name <dynamic-cluster-name>`
**Source:** `Makefile` lines 161-167 (the `kind load` command shape)
**Apply to:** `hack/scripts/load-images-if-needed.sh` — use dynamic cluster name arg, NOT `--name tide-test`
```bash
kind load docker-image "${img}" --name "${cluster_name}"
```

---

## No Analog Found

No files in Phase 6 are truly without an analog. All 15 files map to concrete in-repo patterns.

---

## Critical Anti-Patterns (binding constraints from CLAUDE.md + RESEARCH.md)

| Anti-Pattern | Risk | Correct Approach |
|--------------|------|-----------------|
| Hand-editing `charts/tide/values.yaml` directly | Silently clobbered on next `make helm` | Edit SOT `hack/helm/tide-values.yaml` → run `make helm` |
| `kind load` delegating to `test-int-kind-prep` | Images load into `tide-test`, not `tide-acceptance-<ts>` (Pitfall 1) | Issue `kind load --name ${cluster_name}` directly |
| Image tag `v1.0.0` (with `v`) in CI push | Chart resolves `:1.0.0` (no `v`) → ImagePullBackOff (Pitfall 2) | `${GITHUB_REF_NAME#v}` to strip `v` prefix |
| Adding `ARG BUILDPLATFORM` to Dockerfiles | BuildKit auto-provides it; the ARG is redundant and noise | Only add `FROM --platform=$BUILDPLATFORM`; no extra ARG |
| Touching `images/tide-demo-init/Dockerfile` | Not a chart-referenced component (D-03) | Excluded from IMG-01 pipeline |

---

## Metadata

**Analog search scope:** `.github/workflows/`, `hack/scripts/`, `hack/helm/`, `Makefile`, `./Dockerfile`, `./Dockerfile.dashboard`, `images/*/Dockerfile`, `docs/`, `.gitignore`, `examples/projects/small/`
**Files read:** 14 source files across 7 directories
**Pattern extraction date:** 2026-05-30
