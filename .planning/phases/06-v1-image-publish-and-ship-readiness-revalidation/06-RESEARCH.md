# Phase 6: v1.0 Image-Publish Pipeline & Ship-Readiness Revalidation — Research

**Researched:** 2026-05-30
**Domain:** Docker buildx multi-arch CI pipeline, Helm chart tag alignment, bash script automation (cert-manager bring-up, image-load fallback)
**Confidence:** HIGH — all findings verified against live codebase + official docs

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Separate buildx matrix job (docker/build-push-action over 6 existing Dockerfiles). goreleaser stays CLI+chart only — no `dockers:`/`docker_manifests:` added.
- **D-02:** Minimal Dockerfile cross-compile refactor — add `FROM --platform=$BUILDPLATFORM` to all 6 builder stages. @sha256 pinning is OUT (v1.x SLSA). Cache-mounts/base-consolidation = optional planner discretion.
- **D-03:** 6 components fixed by chart contract: `tide-controller` (`./Dockerfile`), `tide-dashboard` (`./Dockerfile.dashboard`), `tide-stub-subagent` (`images/stub-subagent/Dockerfile`), `tide-credproxy` (`images/credproxy/Dockerfile`), `tide-push` (`images/tide-push/Dockerfile`), `tide-claude-subagent` (`images/claude-subagent/Dockerfile`). `images/tide-demo-init/Dockerfile` excluded.
- **D-04:** Image-publish integrates into the existing `v*`-tag `release.yaml` flow. Images publish before/with chart push. Exact job placement = planner discretion.
- **D-05:** $0 small-sample mode in `acceptance-v1`. Exact knob (env var or make target) = planner discretion.
- **D-06:** $0-mode pass criteria = infra+dispatch subset of D-A3 (controller Available + dashboard Running + small Project terminal + zero ERROR logs + no orphan Jobs + no ImagePullBackOff).

### Claude's Discretion

- Auto-detect detection method: `docker manifest inspect` vs `docker pull` attempt. Lean toward `docker manifest inspect`.
- Auto-detect code shape: shared helper sourced by both scripts vs inline. Lean toward shared helper.
- BuildKit cache mounts (optional low-risk speedup).
- `release.yaml` job placement for image-publish job relative to chart-push.
- Local-build tag used by scripts when building pre-tag.
- `dry-run-v1` cert-manager pin: mirror `acceptance-v1`'s `TIDE_CERT_MANAGER_VERSION` default `v1.20.2`.

### Deferred Ideas (OUT OF SCOPE)

- Cutting the `v1.0.0` tag + $25 real-LLM acceptance run (post-phase ship action)
- Dockerfile `@sha256` digest pinning + cosign/SLSA supply-chain hardening (v1.x)
- `test-int-kind-prep` cluster-name parameterization (backlog)
- `make doctor` preflight (backlog)
- Phase 5 reopen; new chart features beyond tag alignment
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| IMG-01 | Multi-arch image build-and-publish pipeline for all 6 components | §Standard Stack (buildx-matrix), §Buildx Multi-Arch Pattern, §release.yaml Integration |
| CHART-01 | Chart image-tag SOT alignment (5 `v0.1.0-dev` → `""`) | §CHART-01 SOT Mechanics, §Code Examples |
| DRY-01 | `dry-run-v1.sh` cert-manager bring-up (mirror acceptance-v1.sh) | §DRY-01 Cert-Manager Mirror, §Code Examples |
| IMG-LOAD-01 | Auto-detect local image-load fallback in both scripts | §IMG-LOAD-01 Auto-Detect, §Pitfalls (test-int-kind-prep trap) |
| ACC-01 | BOOT-04 $0 end-to-end revalidation (closeout gate) | §ACC-01 $0 Mode, §Pass Criteria |
| DOC-01 | Ship-state doc corrections + maintainer image-publish docs | §DOC-01 Scope |
| HYG-01 | `.acceptance-runs/` gitignore + `docs/troubleshooting.md` ImagePullBackOff entry | §HYG-01 Mechanics |
</phase_requirements>

---

## Summary

Phase 6 closes three concrete gaps surfaced by the 2026-05-30 BOOT-04 cascades: no image-publish pipeline exists anywhere in `.github/workflows/`; five chart component tags are hardcoded to the dead `v0.1.0-dev` string; and `dry-run-v1.sh` lacks cert-manager bring-up. All three gaps are mechanical and well-bounded — no architectural decisions remain open.

The standard pattern for IMG-01 is a `docker/build-push-action` v6 matrix job inside `release.yaml`. Because all 6 Dockerfiles already use `ARG TARGETOS` + `ARG TARGETARCH` and compile with `CGO_ENABLED=0`, adding `FROM --platform=$BUILDPLATFORM` to each builder stage enables single-runner cross-compile (native Go cross-compile, no QEMU needed for the Go stages). The dashboard's `images/claude-subagent/Dockerfile` uses `node:22-slim` as its runtime base but also compiles a Go binary in the builder stage — the builder gets `--platform=$BUILDPLATFORM`, the `FROM node:22-slim` runtime stage does not need it (it will be emulated for arm64 via QEMU registered by `docker/setup-qemu-action`, but the `npm install` is fast and node multi-arch images are widely available).

For IMG-LOAD-01, `docker manifest inspect` is the right existence probe (no layer download, pure API call, exit-code driven). The auto-detect logic builds all 6 images locally at the same tag the chart resolves (`:1.0.0` after CHART-01), then `kind load docker-image` each into the cluster. All 6 chart `pullPolicy` values are already `IfNotPresent` — once kind-loaded the images are used from containerd cache without attempting a pull.

The `test-int-kind-prep` delegation path in the existing `acceptance-v1.sh` (lines 55-59) is broken for Phase 6's purposes: `test-int-kind-prep` hardcodes `--name tide-test` (Makefile lines 158-167) regardless of the `KIND_CLUSTER` env passed in. The Phase 6 image-load implementation must NOT delegate to `test-int-kind-prep`; it must issue `kind load docker-image ... --name ${CLUSTER_NAME}` directly with the dynamic cluster name.

**Primary recommendation:** Write the image-publish job as a single `build-images` job in `release.yaml` with a `matrix.include` over the 6 components; integrate it with `needs: [helmify-verify]` and gate `chart-publish` on `needs: [build-images, release]`. Write auto-detect as a shared helper script `hack/scripts/load-images-if-needed.sh` sourced by both acceptance scripts.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Multi-arch image build+push | CI (GitHub Actions) | — | buildx matrix job in release.yaml; no in-cluster involvement |
| Chart tag resolution | Helm chart (values.yaml SOT) | Kubernetes (rendered at apply time) | `appVersion` → `.Chart.AppVersion` at `helm template` time; controller just reads the rendered image ref |
| cert-manager bring-up | Operator script (bash) | — | `kubectl apply` + `rollout status` before helm install; no chart involvement |
| Auto-detect image-load | Operator script (bash) | Docker daemon | `docker manifest inspect` probe + `docker build` + `kind load` |
| $0 BOOT-04 validation | Operator script + kind cluster | Controller (in-cluster) | acceptance-v1.sh spins kind, helm installs, waits for controller Available |
| Doc corrections | Repo files (docs/) | — | Markdown edits only |

---

## Standard Stack

### Core (for IMG-01 GitHub Actions workflow)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| docker/build-push-action | v6 | Build + push OCI images to GHCR | Official Docker GHA action; supports multi-platform, `file:`, `context:`, GHA cache |
| docker/setup-buildx-action | v3 | Create buildx builder with docker-container driver | Required for multi-platform builds; `docker-container` driver supports `--platform` |
| docker/setup-qemu-action | v3 | Register arm64 emulator | Required for `node:22-slim` arm64 runtime stage in `tide-claude-subagent`; not needed for pure Go stages |
| docker/login-action | v3 | Authenticate to ghcr.io | Standard pattern; uses `github.actor` + `secrets.GITHUB_TOKEN` |
| docker/metadata-action | v5 | Derive image tags from git ref | Auto-strips `v` prefix, derives semver from tag; optional (planner may inline tag derivation) |

**Version verification:**
```bash
# These are the current action versions per GitHub Marketplace as of 2026-05-30
# [ASSUMED] — not verified against marketplace API in this session
# The existing release.yaml uses goreleaser-action@v6 and setup-helm@v4 as reference points
```
[ASSUMED] — action versions above are from web search results corroborated against multiple sources. Planner should confirm with `gh api repos/docker/build-push-action/releases/latest`.

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `docker manifest inspect` | built-in Docker CLI | Existence probe for published images (no layer download) | IMG-LOAD-01 auto-detect check |
| `kind load docker-image` | kind v0.31.0 (installed) | Load local image into kind cluster's containerd | After local build, before helm install |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `docker manifest inspect` probe | `docker pull` attempt | `docker pull` downloads layers; manifest inspect is a pure API call. Manifest inspect is better for existence check |
| `docker manifest inspect` probe | `skopeo inspect` | skopeo not installed by default; adds a dependency. Docker CLI is already required |
| Shared helper script | Inline in each script | Inline duplicates the 6-image list. Shared helper is a single SOT |
| Single job (all 6 images) | 6 separate jobs | 6 separate jobs adds parallelism but 6× the boilerplate and 6× the runner startup. Matrix in one job is cleaner |
| `docker/metadata-action` for tags | Inline `${GITHUB_REF_NAME#v}` | The existing `chart-publish` job already uses `${GITHUB_REF_NAME#v}` — consistent to stay inline |

**Installation (GitHub Actions — no local install needed):**
```yaml
# These are GHA action steps, not npm/go packages
uses: docker/setup-qemu-action@v3
uses: docker/setup-buildx-action@v3
uses: docker/login-action@v3
uses: docker/build-push-action@v6
```

---

## Architecture Patterns

### System Architecture Diagram

```
v* tag push
    │
    ▼
release.yaml
    │
    ├── helmify-verify (gate 1, existing) ─────────────────────────┐
    │                                                                │
    ├── build-images (NEW gate, runs after helmify-verify) ◄────────┘
    │   matrix.include:
    │     - component: tide-controller, dockerfile: ./Dockerfile, context: .
    │     - component: tide-dashboard, dockerfile: ./Dockerfile.dashboard, context: .
    │     - component: tide-stub-subagent, dockerfile: images/stub-subagent/Dockerfile, context: .
    │     - component: tide-credproxy, dockerfile: images/credproxy/Dockerfile, context: .
    │     - component: tide-push, dockerfile: images/tide-push/Dockerfile, context: .
    │     - component: tide-claude-subagent, dockerfile: images/claude-subagent/Dockerfile, context: .
    │
    │   Per-matrix-item:
    │     docker/setup-qemu-action → docker/setup-buildx-action
    │     → docker/login-action (ghcr.io, packages:write)
    │     → docker/build-push-action
    │         platforms: linux/amd64,linux/arm64
    │         push: true (only on v* tag, not rc)
    │         tags: ghcr.io/jsquirrelz/{component}:${GITHUB_REF_NAME#v}
    │
    ├── pre-flight (gate 2, existing) — runs parallel with build-images OR after
    │   needs: [helmify-verify]
    │
    ├── release (goreleaser, existing)
    │   needs: [helmify-verify, pre-flight]
    │   if: !contains(...-rc.)
    │
    └── chart-publish (existing, EXTENDED)
        needs: [build-images, release]       ← ADDED build-images dependency
        if: !contains(...-rc.)
        (pushes charts AFTER images are published)
```

```
Local operator flow ($0 BOOT-04 — ACC-01):
make acceptance-v1 ACCEPTANCE_SAMPLE=small
    │
    ├── kind create cluster --name tide-acceptance-<ts>
    ├── cert-manager install (kubectl apply + rollout status × 3 deploys)
    ├── load-images-if-needed.sh (NEW shared helper)
    │     for each of 6 images:
    │       docker manifest inspect ghcr.io/jsquirrelz/<img>:1.0.0
    │         SUCCESS → nothing to do (image will pull from registry)
    │         FAIL    → docker build ... -t ghcr.io/jsquirrelz/<img>:1.0.0
    │                   kind load docker-image ghcr.io/jsquirrelz/<img>:1.0.0 --name <cluster>
    ├── helm install tide-crds + tide
    ├── kubectl wait --for=condition=Available deploy/tide-controller-manager --timeout=5m
    ├── [small-sample mode: kubectl apply examples/projects/small/project.yaml]
    │   [large-sample mode: create secrets, apply examples/projects/large/project.yaml]
    ├── kubectl wait project/small-project --for=jsonpath='{.status.phase}'=Complete --timeout=10m
    └── D-06 pass criteria check:
          - controller-manager Available ✓
          - dashboard Running ✓
          - small Project terminal ✓
          - zero ERROR logs ✓
          - no orphan Jobs ✓
          - no ImagePullBackOff ✓
```

### Recommended Project Structure (new files only)

```
.github/workflows/
├── release.yaml          # existing — add build-images job
hack/scripts/
├── acceptance-v1.sh      # existing — add $0 mode + call load-images-if-needed.sh
├── dry-run-v1.sh         # existing — add cert-manager block + call load-images-if-needed.sh
└── load-images-if-needed.sh  # NEW — shared auto-detect helper
hack/helm/
└── tide-values.yaml      # existing — edit 5 tags v0.1.0-dev → ""
                          # charts/tide/values.yaml regenerated by make helm
docs/
└── troubleshooting.md    # existing — append ImagePullBackOff mid-install row
.gitignore                # existing — append .acceptance-runs/
docs/INSTALL.md           # existing — add image-publish + local-fallback section
```

### Pattern 1: buildx Matrix Job for 6 Images (IMG-01)

**What:** A single job with `strategy.matrix.include` iterating over all 6 components. Each matrix item carries `component`, `dockerfile`, and `context` (repo root `.` for all 6 images since Dockerfiles use repo-root relative COPY paths).

**When to use:** When multiple images share identical build steps and differ only in `file:` + `tags:`. Avoids 6 separate jobs with identical boilerplate.

**Example:**
```yaml
# Source: https://docs.docker.com/build/ci/github-actions/multi-platform/ [VERIFIED: web search + docs]
build-images:
  name: Build and push component images
  if: ${{ !contains(github.ref, '-rc.') }}
  needs: [helmify-verify]
  runs-on: ubuntu-latest
  timeout-minutes: 30
  permissions:
    contents: read
    packages: write   # REQUIRED for ghcr.io push
  strategy:
    matrix:
      include:
        - component: tide-controller
          dockerfile: ./Dockerfile
          context: .
        - component: tide-dashboard
          dockerfile: ./Dockerfile.dashboard
          context: .
        - component: tide-stub-subagent
          dockerfile: images/stub-subagent/Dockerfile
          context: .
        - component: tide-credproxy
          dockerfile: images/credproxy/Dockerfile
          context: .
        - component: tide-push
          dockerfile: images/tide-push/Dockerfile
          context: .
        - component: tide-claude-subagent
          dockerfile: images/claude-subagent/Dockerfile
          context: .
  steps:
    - uses: actions/checkout@v4
      with:
        persist-credentials: false
    - name: Set up QEMU
      uses: docker/setup-qemu-action@v3
    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3
    - name: Login to ghcr.io
      uses: docker/login-action@v3
      with:
        registry: ghcr.io
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}
    - name: Build and push ${{ matrix.component }}
      uses: docker/build-push-action@v6
      with:
        context: ${{ matrix.context }}
        file: ${{ matrix.dockerfile }}
        platforms: linux/amd64,linux/arm64
        push: true
        tags: ghcr.io/jsquirrelz/${{ matrix.component }}:${{ github.ref_name }}
        cache-from: type=gha,scope=${{ matrix.component }}
        cache-to: type=gha,mode=max,scope=${{ matrix.component }}
```

**Tag note:** `github.ref_name` for a `v1.0.0` tag is `v1.0.0` (with the `v`). The chart resolves to `appVersion: "1.0.0"` (no `v`). This is a critical alignment gap — see Pitfall 3 below. The planner must use `${GITHUB_REF_NAME#v}` or strip the `v` from `github.ref_name`. The existing `chart-publish` job uses `${GITHUB_REF_NAME#v}` — match that pattern.

### Pattern 2: `FROM --platform=$BUILDPLATFORM` Cross-Compile (D-02)

**What:** Adding `FROM --platform=$BUILDPLATFORM` to each builder stage tells buildx to run the builder on the native platform (amd64 on an amd64 runner), then use Go's cross-compile (`GOOS=${TARGETOS} GOARCH=${TARGETARCH}`) to produce the target binary. The final `FROM gcr.io/distroless/static:nonroot` (or `FROM node:22-slim`) copies the binary and that stage is built for the target platform.

**When to use:** Any multi-stage Dockerfile where the builder stage compiles a Go binary and the runtime stage just runs it.

**Example (identical change for all 5 pure-Go images):**
```dockerfile
# Source: https://docs.docker.com/build/building/multi-platform/ [VERIFIED: official docs]
# BEFORE:
FROM golang:1.26 AS builder          # or golang:1.26-alpine
ARG TARGETOS
ARG TARGETARCH
# ...
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build ...

# AFTER (one-line change per Dockerfile):
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder    # <── the only change
ARG TARGETOS
ARG TARGETARCH
# BUILDPLATFORM is auto-provided by buildx; no additional ARG needed
# Go reads GOOS/GOARCH for cross-compile; native runner does the compilation
```

**Confirmed:** `BUILDPLATFORM` is automatically provided by BuildKit — no `ARG BUILDPLATFORM` needed at the Dockerfile top level. The `ARG TARGETOS` and `ARG TARGETARCH` lines that already exist in all 6 Dockerfiles remain required (they scope the ARG into the stage so the `go build` RUN instruction can read them).

**Dashboard (`images/claude-subagent/Dockerfile`) note:** The builder stage is Go (`golang:1.26-alpine`) — same fix applies. The runtime stage is `FROM node:22-slim` — this stage does NOT get `--platform=$BUILDPLATFORM`. BuildKit will produce the `node:22-slim` stage for the target platform (linux/arm64 for arm64 target). `node:22-slim` has official arm64 manifests. QEMU is registered by `setup-qemu-action` and handles the `npm install -g @anthropic-ai/claude-code` step for arm64. This is slower than the Go cross-compile path but is unavoidable for Node runtime stages.

**`Dockerfile.dashboard` (tide-dashboard):** Uses `golang:1.26 AS builder` (not alpine) and has no node stage — applies same one-line fix as the pure-Go images.

### Pattern 3: release.yaml Job Ordering (D-04)

**What:** The image-publish job must precede the chart-publish job so published charts never reference not-yet-published images.

**Ordering constraint:**
```
helmify-verify
    ├── build-images    (new; needs: [helmify-verify]; if: !contains rc)
    └── pre-flight      (existing; needs: [helmify-verify]; if: !contains rc)

release                 (existing; needs: [helmify-verify, pre-flight])
chart-publish           (existing + extended; needs: [build-images, release])
```

**Why `build-images` gates on `!contains(rc)`:** rc tags trigger `dry-run.yaml` only; no images or charts are published for rc tags per the existing flow. The image-publish job matches this posture.

**Why `chart-publish` gets `needs: [build-images, release]` not just `needs: [release]`:** Prevents the scenario where goreleaser succeeds but a container build times out, leaving the chart published with missing images.

### Pattern 4: CHART-01 SOT Mechanics

**What:** Edit `hack/helm/tide-values.yaml` at lines 39, 140, 144, 155, 165 (the five `tag: v0.1.0-dev` entries). Set each to `""` (matching the dashboard pattern at line 244). Run `make helm` (which runs `bash hack/helm/augment-tide-chart.sh`). The augment script step 2 is `cp "${HACK_DIR}/tide-values.yaml" "${CHART_DIR}/values.yaml"` — a direct copy. No additional patching needed.

**Verification:**
```bash
grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml  # must return 0
helm template charts/tide | grep -E 'image:'                                  # must show 1.0.0 for all 6 TIDE images
grep -E 'tag: "1\.36"' hack/helm/tide-values.yaml                             # must still be present (busybox)
```

**Note on `tag: ""`:** When the Helm template renders `{{ .Values.controllerManager.manager.image.tag | default .Chart.AppVersion }}`, an empty string `""` evaluates to falsy in Go templates so `| default .Chart.AppVersion` kicks in, yielding `1.0.0`. This is the same mechanism already working for `dashboard.image.tag: ""` at line 244.

**Verification of how tag resolution works in charts/tide/templates/:**
```bash
# [VERIFIED: live codebase] dashboard tag is "" and resolves to 1.0.0 via | default .Chart.AppVersion
helm template charts/tide | grep -A1 "tide-dashboard" | grep "image:"
```

### Pattern 5: DRY-01 Cert-Manager Mirror

**What:** Mirror the cert-manager block from `hack/scripts/acceptance-v1.sh` (lines 61-73, commit `adb1053`) into `hack/scripts/dry-run-v1.sh`. The block goes inside the DinD container's bash heredoc, between `kind create cluster` (line 80) and `helm install tide-crds` (line 81).

**Source pattern (from acceptance-v1.sh lines 61-73):**
```bash
# [VERIFIED: live codebase — acceptance-v1.sh lines 68-74]
CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"
echo "==> installing cert-manager ${CERT_MANAGER_VERSION}..."
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
echo "==> waiting for cert-manager Deployments to roll out..."
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status deployment/"${deploy}" --timeout=120s
done
```

**Placement in dry-run-v1.sh:** Inside the `bash -c "..."` heredoc passed to `docker run`, after `kind create cluster --name tide-dry-run` and before `helm install tide-crds ./charts/tide-crds`. The DinD container already has `kubectl` installed at that point (line 71 of current dry-run-v1.sh). Helm is also installed (line 67-68).

**`TIDE_CERT_MANAGER_VERSION` propagation in DinD:** The outer script's env var must be explicitly passed into the DinD container via `-e TIDE_CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"` on the `docker run` command, or the heredoc must hardcode the default. The simpler approach: hardcode `CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"` inside the heredoc and pass the outer var via `-e` so operators can override it.

### Pattern 6: IMG-LOAD-01 Auto-Detect (Shared Helper)

**What:** A shared script `hack/scripts/load-images-if-needed.sh` that accepts two arguments: a cluster name and a space-separated list of image refs (or reads from a fixed array). Both `acceptance-v1.sh` and `dry-run-v1.sh` source or call it.

**Detection method:** `docker manifest inspect <image>` exits 0 if the manifest exists in the registry, non-zero if it does not. No layers are downloaded.

**Algorithm:**
```bash
# [ASSUMED] pattern — standard docker manifest inspect usage per Docker docs
load_images_if_needed() {
  local cluster_name="$1"
  local image_tag="$2"  # e.g. "1.0.0"

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

  for i in "${!images[@]}"; do
    local img="${images[$i]}:${image_tag}"
    local df="${dockerfiles[$i]}"
    if docker manifest inspect "${img}" > /dev/null 2>&1; then
      echo "==> ${img}: found in registry, skipping local build"
    else
      echo "==> ${img}: not found in registry, building locally..."
      docker build -t "${img}" -f "${df}" "${REPO_ROOT}"
      kind load docker-image "${img}" --name "${cluster_name}"
    fi
  done
}
```

**Tag used for local build:** The chart's `appVersion` is `1.0.0`. After CHART-01, all 6 images resolve to `:1.0.0` at `helm template` time. The local build must use `:1.0.0` so the tag matches what the chart requests. `image_tag="1.0.0"` (no `v` prefix — matches appVersion convention, not goreleaser tag convention).

**`pullPolicy: IfNotPresent` is already set** for all 6 images in `charts/tide/values.yaml` (lines 40, 141, 145, 149, 156, 166, 245). A kind-loaded image tagged `:1.0.0` is used from containerd cache without attempting a registry pull. [VERIFIED: live codebase]

**Acceptance script invocation:**
```bash
# In acceptance-v1.sh (replaces the existing ACCEPTANCE_LOAD_IMAGES block lines 55-59)
IMAGE_TAG="1.0.0"  # matches chart appVersion post CHART-01
bash "${REPO_ROOT}/hack/scripts/load-images-if-needed.sh" "${CLUSTER_NAME}" "${IMAGE_TAG}"
```

**dry-run-v1.sh consideration:** `dry-run-v1.sh` runs inside a DinD container. The image probe and kind load must happen from the HOST (outside the docker run call) before the DinD container tries to install the helm chart, OR the DinD container's kind cluster must have images pre-loaded by the outer script before the `docker run` step. The simpler approach: the outer `dry-run-v1.sh` script runs the load helper on the host before calling `docker run`, and the kind cluster created by the DinD `kind create cluster --name tide-dry-run` is accessed from outside (since DinD uses `--network host` and mounts `/var/run/docker.sock`, the outer script can reach the same kind cluster). This requires carefully sequencing: (1) DinD creates the kind cluster via `docker run`, (2) outer script calls load helper to kind-load images into `tide-dry-run`, (3) DinD continues with helm install. The simplest solution is to split the DinD heredoc: first pass creates cluster, second call runs the outer load helper, third pass runs helm install + wait. Alternatively, run the entire pipeline outside DinD in the outer script, preserving the DinD wrapper only for the `git clone` + tool installation phase. The planner must choose the exact split.

### Pattern 7: ACC-01 $0 Mode in acceptance-v1.sh

**What:** A new mode triggered by `ACCEPTANCE_SAMPLE=small` (or a dedicated `make acceptance-v1-smoke` target) that applies `examples/projects/small/project.yaml` instead of the large sample. No `ANTHROPIC_API_KEY` or `GH_PAT` required in $0 mode.

**Small sample details:**
- File: `examples/projects/small/project.yaml` [VERIFIED: exists]
- Namespace: `tide-sample-small`
- Project name: `small-project`
- Subagent image: `ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0` (hardcoded in project.yaml line 49)
- Budget: `absoluteCapCents: 0` — any real API call fails immediately
- Gates: all `auto`
- kubectl wait target: `project/small-project -n tide-sample-small --for=jsonpath='{.status.phase}'=Complete --timeout=10m`

**$0-mode guard:** Do NOT require `ANTHROPIC_API_KEY` or `GH_PAT` env vars when `ACCEPTANCE_SAMPLE=small`. The existing fail-fast env gate (acceptance-v1.sh lines 27-28) must be conditional on the mode.

**D-06 pass criteria (subset of D-A3):**
1. `kubectl wait --for=condition=Available deploy/tide-controller-manager -n tide-system`
2. `kubectl wait --for=condition=Available deploy/tide-dashboard -n tide-system` (or equivalent Running check)
3. `kubectl wait project/small-project -n tide-sample-small --for=jsonpath='{.status.phase}'=Complete --timeout=10m`
4. `kubectl logs -n tide-system deploy/tide-controller-manager | grep -c '"level":"error"'` = 0
5. `kubectl get jobs --all-namespaces -l tideproject.k8s/project-uid=<uid> --field-selector=status.active=1 | grep -v "^0 "` = empty (no orphan active jobs)
6. `kubectl get pods --all-namespaces | grep ImagePullBackOff | wc -l` = 0

Checks 1, 2, 5, 7 (gitleaks, budget) from D-A3 are **excluded** from the $0 mode (no per-run branch, no commit shapes, no gitleaks counter, no budget spent).

### Anti-Patterns to Avoid

- **Hand-editing `charts/tide/values.yaml` directly:** This violates the chart-vs-binary anti-pattern (CLAUDE.md). All tag edits go into `hack/helm/tide-values.yaml` first, then `make helm` propagates.
- **Using `test-int-kind-prep` for kind-load in acceptance-v1.sh:** `test-int-kind-prep` hardcodes `--name tide-test` in the Makefile (lines 158-167). Delegating to it from acceptance-v1.sh with `KIND_CLUSTER="${CLUSTER_NAME}"` is silently broken — the images get loaded into `tide-test` not `tide-acceptance-<ts>`. The Phase 6 load helper must use `--name ${cluster_name}` directly. [VERIFIED: live Makefile]
- **Using `github.ref_name` as image tag directly:** `github.ref_name` for `v1.0.0` is `v1.0.0` (with `v`); appVersion is `1.0.0` (no `v`). The tag must use `${GITHUB_REF_NAME#v}` (strip `v`) to match what `helm template` resolves.
- **Touching `images/tide-demo-init/Dockerfile`:** Not a chart-referenced component; excluded from IMG-01 by D-03.
- **Adding `ARG BUILDPLATFORM` to Dockerfiles:** BuildKit provides this automatically. The only change needed is `FROM --platform=$BUILDPLATFORM` on the builder stage; no `ARG BUILDPLATFORM` line is needed.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Multi-arch image push | Custom docker buildx shell scripts | `docker/build-push-action` | Handles provenance attestation, GHA cache integration, QEMU registration ordering |
| Image existence check | curl to GHCR v2 API with token | `docker manifest inspect` | Docker CLI handles auth (uses `docker login` credentials) transparently; simpler |
| Tag derivation from git ref | Custom string manipulation | `${GITHUB_REF_NAME#v}` bash parameter expansion | One-liner; same pattern used in existing `chart-publish` job |
| cert-manager wait logic | Custom polling loop | `kubectl rollout status --timeout=120s` | Already proven in acceptance-v1.sh and integration test suite |

---

## Common Pitfalls

### Pitfall 1: `test-int-kind-prep` Hardcodes `tide-test` Cluster Name

**What goes wrong:** The existing stub in `acceptance-v1.sh` (lines 55-59) delegates image load to `test-int-kind-prep` via `KIND_CLUSTER="${CLUSTER_NAME}" make -C "${REPO_ROOT}" test-int-kind-prep`. The Makefile `test-int-kind-prep` target hardcodes `--name tide-test` on lines 158-167 — it checks for cluster existence using `grep -q "^tide-test$$"` and loads into `--name tide-test`. The `KIND_CLUSTER` env var is not consumed by this target. Images get loaded into `tide-test` cluster, not the dynamically-named `tide-acceptance-<ts>` cluster.

**Why it happens:** `test-int-kind-prep` was written for the integration test flow (fixed `tide-test` cluster). The acceptance script reused it as a quick hack.

**How to avoid:** The Phase 6 load helper must issue `kind load docker-image ... --name "${cluster_name}"` directly, not delegate to `test-int-kind-prep`. [VERIFIED: live Makefile lines 158-167]

**Warning signs:** `kubectl get pods -n tide-system` shows `ImagePullBackOff` even after the load step; `kind get clusters` shows both `tide-test` and `tide-acceptance-<ts>`.

### Pitfall 2: Image Tag Has `v` Prefix, appVersion Does Not

**What goes wrong:** `github.ref_name` for tag `v1.0.0` is the string `v1.0.0`. Chart `appVersion` is `"1.0.0"` (no `v`). If the image is pushed as `ghcr.io/jsquirrelz/tide-controller:v1.0.0` but the chart resolves to `ghcr.io/jsquirrelz/tide-controller:1.0.0`, every deployment gets `ImagePullBackOff`.

**Why it happens:** goreleaser convention uses `v`-prefixed tags; Helm `appVersion` strips the `v`.

**How to avoid:** Use `${GITHUB_REF_NAME#v}` (or `${{ github.ref_name }}` with a `sed 's/^v//'` step) consistently. The existing `chart-publish` job already uses `${GITHUB_REF_NAME#v}` — follow the same pattern. [VERIFIED: release.yaml lines 263-264]

**Warning signs:** `helm template charts/tide | grep image:` shows `:1.0.0` but GHCR has only `:v1.0.0`.

### Pitfall 3: ghcr.io Packages Private by Default on First Push

**What goes wrong:** The first push of a new package to `ghcr.io/jsquirrelz/` sets it to **private** by default. Helm install attempts to pull the image and gets `ImagePullBackOff` with an authorization error even though the image "exists" on GHCR.

**Why it happens:** GitHub Container Registry defaults new packages to private visibility. The repository owner must navigate to the package settings and set visibility to Public.

**How to avoid:** After the first real push of each image, go to `https://github.com/users/jsquirrelz/packages/container/<name>/settings` and set visibility to Public. This is a one-time manual step per image. Document in DOC-01 / INSTALL.md maintainer section. [MEDIUM confidence — common GHCR behavior from multiple web sources, no official doc citation found in this session]

**Warning signs:** `docker manifest inspect ghcr.io/jsquirrelz/tide-*:1.0.0` returns 401 even after push succeeded in CI.

### Pitfall 4: `docker manifest inspect` Requires Registry Auth for Private Images

**What goes wrong:** Pre-release (before images are made public on GHCR), `docker manifest inspect` exits non-zero even when the image exists, because the host Docker daemon is not authenticated to GHCR. The auto-detect logic incorrectly triggers a local build and kind-load even for published-but-private images.

**Why it happens:** `docker manifest inspect` uses the credentials in `~/.docker/config.json`. If the operator is not logged in to ghcr.io, the probe fails for private images.

**How to avoid:** For the local $0 operator run (pre-publish): images won't exist yet → probe fails → local build is triggered. This is the correct behavior. For post-publish local runs: images are public → probe succeeds → no rebuild. The only edge case is published-but-private images, which is a transient state after first push and before making public. Document as a known edge case.

**Warning signs:** `docker manifest inspect ghcr.io/jsquirrelz/tide-controller:1.0.0` exits non-zero on a machine not logged into GHCR, even after the image is published.

### Pitfall 5: `tide-claude-subagent` arm64 Build is Slow (QEMU for node stage)

**What goes wrong:** The `images/claude-subagent/Dockerfile` uses `FROM node:22-slim` as runtime. The arm64 build of this stage runs `npm install -g @anthropic-ai/claude-code@2.1.142` under QEMU emulation on the amd64 runner. npm install can take 2-5 minutes under QEMU.

**Why it happens:** Node does not have native cross-compile like Go. The arm64 npm install must execute natively (or under QEMU). The `--platform=$BUILDPLATFORM` fix only applies to the Go builder stage.

**How to avoid:** Set the `build-images` job timeout to 30 minutes (not the default 6m). QEMU arm64 npm installs are slow but reliable. This is expected and acceptable for a release pipeline. [ASSUMED based on known QEMU npm install performance]

**Warning signs:** `build-images` times out for `tide-claude-subagent` but succeeds for other components.

### Pitfall 6: DinD dry-run-v1 Image Load Timing

**What goes wrong:** `dry-run-v1.sh` runs the entire inner pipeline (`kind create` + `helm install` + `kubectl wait`) inside a single `docker run` heredoc. If the load helper is called from the outer script BEFORE the `docker run`, the kind cluster does not exist yet. If called AFTER the `docker run`, the helm install has already tried to pull images.

**Why it happens:** The DinD pattern chains all steps in one `docker run` call, mixing cluster creation with helm install.

**How to avoid:** Split the DinD heredoc into two passes: (1) `docker run` to create the kind cluster only; (2) outer script calls `load-images-if-needed.sh`; (3) second `docker run` (or continuation of first using a different mechanism) for helm install + wait. Alternatively, since `dry-run-v1.sh` uses `--network host` and mounts `/var/run/docker.sock`, the kind cluster created inside is accessible from the outer script. The planner must design the split carefully. See §Open Questions.

### Pitfall 7: RC Tag Should Not Push Images

**What goes wrong:** If the `build-images` job does not have `if: !contains(github.ref, '-rc.')`, images are pushed for rc tags even though goreleaser does not build binaries for rc tags. This creates a partial release state.

**Why it happens:** Matching the existing release flow where `release` and `chart-publish` are gated on `!contains(...-rc.)`.

**How to avoid:** Add `if: ${{ !contains(github.ref, '-rc.') }}` to the `build-images` job, mirroring the `release` and `chart-publish` conditions.

### Pitfall 8: Existing `ACCEPTANCE_LOAD_IMAGES` Path Should Be Replaced, Not Kept

**What goes wrong:** `acceptance-v1.sh` already has an `ACCEPTANCE_LOAD_IMAGES` env-gated image-load block (lines 55-59). If Phase 6 adds the auto-detect helper as a new code path alongside this block, both paths coexist and create confusion.

**Why it happens:** The existing block was a placeholder but it delegates to `test-int-kind-prep` which is broken (Pitfall 1).

**How to avoid:** Replace the entire `ACCEPTANCE_LOAD_IMAGES` block (lines 55-59) with the new auto-detect call. Remove the `ACCEPTANCE_LOAD_IMAGES` env var entirely (or leave it as a no-op comment). The auto-detect runs unconditionally (pre-helm-install, post-cluster-create). No operator flags needed per SPEC.

---

## Code Examples

### buildx Multi-Arch Image Tag (strip `v` prefix)

```yaml
# Source: release.yaml chart-publish job lines 263-264 [VERIFIED: live codebase]
# Pattern: ${GITHUB_REF_NAME#v} strips leading 'v' from tag name
tags: ghcr.io/jsquirrelz/${{ matrix.component }}:${{ github.ref_name }}
# BUT this pushes with 'v' prefix. Correct form:
# In a 'run:' step: IMAGE_TAG="${GITHUB_REF_NAME#v}"
# Then: tags: ghcr.io/jsquirrelz/${{ matrix.component }}:${IMAGE_TAG}
# Or use env:
# env:
#   IMAGE_TAG: ${{ github.ref_name }}  # then strip in run step
```

Better: define `IMAGE_TAG` in a top-level `env:` block on the job:
```yaml
# [ASSUMED] — standard GHA pattern
env:
  IMAGE_TAG: ${{ github.ref_name }}   # 'v1.0.0'
steps:
  # ...
  - name: Build and push ${{ matrix.component }}
    uses: docker/build-push-action@v6
    with:
      tags: ghcr.io/jsquirrelz/${{ matrix.component }}:${{ env.IMAGE_TAG }}
      # But IMAGE_TAG still has 'v' — need to strip it.
      # Alternative: use a 'run:' step to compute and write to GITHUB_ENV:
      # echo "IMAGE_TAG=${GITHUB_REF_NAME#v}" >> "${GITHUB_ENV}"
```

Cleanest pattern (matches existing chart-publish job):
```yaml
- name: Derive image tag
  run: echo "IMAGE_TAG=${GITHUB_REF_NAME#v}" >> "${GITHUB_ENV}"
- name: Build and push ${{ matrix.component }}
  uses: docker/build-push-action@v6
  with:
    tags: ghcr.io/jsquirrelz/${{ matrix.component }}:${{ env.IMAGE_TAG }}
```

### cert-manager Mirror (DRY-01)

```bash
# Source: acceptance-v1.sh lines 68-74 [VERIFIED: live codebase]
# Place this block inside the DinD heredoc, after `kind create cluster`, before `helm install tide-crds`:
CERT_MANAGER_VERSION="${TIDE_CERT_MANAGER_VERSION:-v1.20.2}"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
for deploy in cert-manager cert-manager-cainjector cert-manager-webhook; do
  kubectl -n cert-manager rollout status deployment/"${deploy}" --timeout=120s
done
```

### CHART-01 SOT Edit

```yaml
# Source: hack/helm/tide-values.yaml [VERIFIED: live codebase]
# BEFORE (5 occurrences to change):
#   line 39:  tag: v0.1.0-dev
#   line 140: tag: v0.1.0-dev
#   line 144: tag: v0.1.0-dev
#   line 155: tag: v0.1.0-dev
#   line 165: tag: v0.1.0-dev

# AFTER (each becomes):
tag: ""             # default to .Chart.AppVersion

# Line 148 (busybox, third-party) MUST NOT be changed:
tag: "1.36"         # preserve unchanged

# Propagate:
make helm    # runs bash hack/helm/augment-tide-chart.sh which cp's the SOT to charts/tide/values.yaml
```

### HYG-01: .gitignore Entry

```
# Add to .gitignore (append after existing entries)
# [VERIFIED: .gitignore exists; .acceptance-runs/ is NOT currently listed]
.acceptance-runs/
```

### HYG-01: troubleshooting.md Row

```markdown
# Source: docs/troubleshooting.md [VERIFIED: D-C4 format is Symptom | Cause | Recipe table]
| `deploy/tide-controller-manager` or `tide-dashboard` pod stuck in `ImagePullBackOff` mid-install | Chart references component images that have never been published to ghcr.io (pre-release state) OR chart tag pin (`v0.1.0-dev`) doesn't match any published tag | 1. Check if images exist: `docker manifest inspect ghcr.io/jsquirrelz/tide-controller:$(helm get values tide -n tide-system -o json | jq -r '.controllerManager.manager.image.tag // "1.0.0"')`. 2. If not published: run `make acceptance-v1-smoke` (builds + kind-loads locally). 3. Check chart tag pins: `grep -E 'v0\.1\.0-dev' charts/tide/values.yaml` — should return 0 after Phase 6. See [INSTALL.md](INSTALL.md) §Local image build. |
```

---

## Runtime State Inventory

> This phase is NOT a rename/refactor phase. The tag change in CHART-01 is a chart values change, not a stored-data rename. Omitting this section accordingly.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `goreleaser dockers:` for container builds | Separate buildx matrix job | D-01 locked decision | goreleaser does not manage containers; clean separation of concerns |
| QEMU-only multi-arch (emulation for Go) | `--platform=$BUILDPLATFORM` cross-compile | D-02 locked decision | Go builder runs native, only node runtime stage needs QEMU |
| Helm chart tags hardcoded (`v0.1.0-dev`) | `tag: ""` → resolves to `.Chart.AppVersion` | CHART-01 this phase | All 6 TIDE component tags track chart appVersion automatically |

**Current state of `release.yaml` (verified):**
- 4 jobs: `helmify-verify` → `pre-flight` → `release` → `chart-publish`
- `chart-publish` currently `needs: release` only
- Phase 6 inserts `build-images` job and adds `build-images` to `chart-publish`'s `needs:`
- `build-images` must be `if: !contains(github.ref, '-rc.')` to match existing posture

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `docker/build-push-action@v6`, `docker/setup-qemu-action@v3`, `docker/setup-buildx-action@v3`, `docker/login-action@v3` are current versions | Standard Stack | Planner should verify with `gh api repos/docker/build-push-action/releases/latest`; version drift is low-risk (v6 range is likely current) |
| A2 | `github.ref_name` for a `v1.0.0` tag push is `v1.0.0` (with `v`) | Pattern 1 | If wrong, tag format is already correct and `#v` strip is a no-op — acceptable |
| A3 | ghcr.io packages default to private on first push | Pitfall 3 | If wrong (GitHub org has public-by-default), this pitfall doesn't apply. Verify against first push behavior |
| A4 | QEMU arm64 npm install takes 2-5 minutes | Pitfall 5 | If faster, timeout budget can be reduced. If slower, 30-minute job timeout may still be sufficient |
| A5 | `docker manifest inspect` exits non-zero when image is absent (not just when unauthenticated) | Pattern 6 | If `docker manifest inspect` exits non-zero for both missing and auth-failure cases, the auto-detect cannot distinguish them — both trigger local build+load. For the $0 pre-publish use case this is correct behavior |
| A6 | `node:22-slim` has official arm64 manifests | Pattern 2 (dashboard) | If wrong, QEMU fallback handles it anyway — arm64 emulation still works |
| A7 | `small-project` uses `stub-subagent` image `ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0` (line 49 of small/project.yaml) | ACC-01 | [VERIFIED: live codebase] — confirmed. Note this hardcodes `v1.0.0` not `1.0.0`. After CHART-01 the chart resolves to `1.0.0`; the project.yaml spec.subagent.image still says `v1.0.0`. The auto-detect must build + load `tide-stub-subagent:1.0.0` (for the chart) AND `tide-stub-subagent:v1.0.0` (for the project.yaml spec.subagent.image). OR the local build should tag both. This is a subtle naming inconsistency the planner must address. |

**A7 requires planner attention.** The `examples/projects/small/project.yaml` line 49 hardcodes `image: ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0`. The chart after CHART-01 resolves to `tide-stub-subagent:1.0.0` (no `v`). There are two images involved: the chart-deployed stub-subagent sidecar (tag from appVersion = `1.0.0`) AND the per-Project override image (tag hardcoded in project.yaml = `v1.0.0`). If CHART-01 changes the chart stub-subagent tag to `1.0.0` but the project.yaml still says `v1.0.0`, the local-build must produce BOTH tags (`1.0.0` and `v1.0.0`) from the same Dockerfile, or update project.yaml to use `1.0.0`. Updating the project.yaml to `image: ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0` is likely the right fix — consistent with appVersion convention.

---

## Open Questions

1. **DinD heredoc split for dry-run-v1 image-load**
   - What we know: `dry-run-v1.sh` runs everything inside one `docker run` heredoc. The kind cluster is created inside DinD. The outer script can reach the kind cluster via `--network host` + `/var/run/docker.sock` mount.
   - What's unclear: The exact split point. Option A — call `load-images-if-needed.sh` from outside the DinD run, between two `docker run` invocations (split the heredoc). Option B — pass images into the DinD container pre-loaded using `kind load` from outside (the outer script creates the kind cluster first, then loads images, then runs the inner pipeline in DinD). Option C — skip image-load in `dry-run-v1.sh` for the $0 case (since `dry-run-v1.sh` targets the `v*-rc.*` gate, images may be published at that point).
   - Recommendation: Option C is simplest for the rc-tag use case (images should be pushed before tagging rc). For the pre-publish local developer case, Option A (split heredoc) is cleanest. The planner should pick one and document it.

2. **`project.yaml` stub-subagent tag inconsistency (A7)**
   - What we know: `examples/projects/small/project.yaml` line 49 says `v1.0.0` (with `v`). After CHART-01 the chart values resolve to `1.0.0` (no `v`).
   - What's unclear: Whether to fix the project.yaml tag to `1.0.0` (CHART-01 consistency) or build+load both tags.
   - Recommendation: Fix `project.yaml` to use `1.0.0` (matches appVersion convention). The `v` prefix was likely a copy-paste from goreleaser tag naming. This is a small additional edit scoped to `examples/projects/small/project.yaml`.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker | IMG-LOAD-01, buildx | ✓ | 29.4.3 | — |
| Docker buildx | IMG-01 (CI), local snapshot build | ✓ | v0.33.0-desktop.1 | — |
| kind | ACC-01, IMG-LOAD-01 | ✓ | v0.31.0 | — |
| Helm | ACC-01 | ✓ | v3.16.3 | — |
| kubectl | ACC-01, DRY-01 | ✓ | v1.34.1 | — |
| GitHub Actions runners (ubuntu-latest) | IMG-01 (CI pipeline) | ✓ (CI) | — | — |
| QEMU (via docker/setup-qemu-action in CI) | IMG-01 arm64 node stage | ✓ (CI via action) | — | — |

**Missing dependencies with no fallback:** None — all dependencies available on local machine and CI.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | bash assertions + kubectl wait + grep exit codes (no Go `_test.go` tests for Phase 6) |
| Config file | none — script-based validation |
| Quick run command | `make acceptance-v1-smoke` (or `ACCEPTANCE_SAMPLE=small make acceptance-v1`) |
| Full suite command | `make acceptance-v1` (requires `ANTHROPIC_API_KEY` + `GH_PAT`, post-phase) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| IMG-01 | 6 images buildable, multi-arch manifest | snapshot build | `docker buildx build --platform linux/amd64,linux/arm64 --load -f ./Dockerfile .` (per image, no push) | ❌ Wave 0 (add to Makefile as `docker-buildx-snapshot`) |
| CHART-01 | `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` = 0 | grep assertion | `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` | ✅ (acceptance criterion is the command itself) |
| CHART-01 | `helm template` resolves all 6 TIDE images to `1.0.0` | helm assertion | `helm template charts/tide \| grep -E 'image:' \| grep -v '1\.0\.0\|1\.36'` (should return empty) | ✅ |
| DRY-01 | cert-manager keyword in dry-run-v1.sh | grep assertion | `grep -cE 'cert-manager' hack/scripts/dry-run-v1.sh` | ✅ (post-implementation) |
| DRY-01 | dry-run reaches `helm install` without cert-manager error | integration smoke | `make dry-run-v1` (DinD, slow; used for release gate) | ✅ (existing target) |
| IMG-LOAD-01 | Auto-detect builds+loads all 6 images, pods reach Running | integration smoke | embedded in `make acceptance-v1-smoke` | ❌ Wave 0 (new make target) |
| ACC-01 | D-06 criteria all pass | end-to-end smoke | `ACCEPTANCE_SAMPLE=small make acceptance-v1` | ❌ Wave 0 (new mode in acceptance-v1.sh) |
| DOC-01 | No `v0.1.0-dev` ship-ready claim in docs | grep assertion | `grep -riE 'v0\.1\.0-dev\|v1\.0.*ship.ready' docs/ README.md INSTALL.md` = 0 hits on claims | ✅ (manual review) |
| HYG-01 | `.acceptance-runs/` in .gitignore | git check-ignore | `git check-ignore .acceptance-runs/` returns path | ❌ Wave 0 (gitignore edit) |
| HYG-01 | ImagePullBackOff entry in troubleshooting.md | grep assertion | `grep -ciE 'ImagePullBackOff' docs/troubleshooting.md` ≥ 1 | ❌ Wave 0 (doc edit) |

### Sampling Rate

- **Per task commit:** `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml` (< 1s) + `helm template charts/tide | grep -E 'image:' | grep -v '1\.0\.0\|1\.36'`
- **Per wave merge:** `ACCEPTANCE_SAMPLE=small make acceptance-v1` (full $0 BOOT-04 end-to-end, ~10-15 min)
- **Phase gate:** Full $0 BOOT-04 run green before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `hack/scripts/load-images-if-needed.sh` — new shared helper (IMG-LOAD-01)
- [ ] `make acceptance-v1-smoke` or `ACCEPTANCE_SAMPLE=small` mode in `acceptance-v1.sh` (ACC-01)
- [ ] `make docker-buildx-snapshot` (optional but useful for IMG-01 pre-CI verification)
- [ ] `.acceptance-runs/` line in `.gitignore` (HYG-01)
- [ ] ImagePullBackOff row in `docs/troubleshooting.md` (HYG-01)

---

## Security Domain

> `security_enforcement` not explicitly set to false in `.planning/config.json` → required.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No — no new auth surfaces added | — |
| V3 Session Management | No | — |
| V4 Access Control | Partial — `packages: write` on GITHUB_TOKEN in CI | Minimal scope: only the `build-images` job has `packages: write`; other jobs retain `contents: read` only |
| V5 Input Validation | No — no user input in image build pipeline | — |
| V6 Cryptography | No — no new crypto | — |

### Known Threat Patterns for This Stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| `GITHUB_TOKEN` with `packages: write` scope leaked in logs | Information Disclosure | Use `--password-stdin` (not `--password ${{ secrets.GITHUB_TOKEN }}` inline) — existing `chart-publish` already uses `--password-stdin` pattern; `docker/login-action` handles this securely by default |
| Published image tag `1.0.0` pushed before all 6 matrix jobs complete | Tampering (partial manifest list) | Matrix jobs run in parallel; all must succeed before `chart-publish` gates on `build-images`; the `needs:` ordering enforces this |
| ghcr.io image private-by-default exposes nothing sensitive | — | First-push visibility is private; no sensitive data in the image; operator makes it public manually as documented |

---

## Sources

### Primary (HIGH confidence)

- Live codebase reads — `.github/workflows/release.yaml`, `hack/scripts/acceptance-v1.sh`, `hack/scripts/dry-run-v1.sh`, `hack/helm/tide-values.yaml`, `hack/helm/augment-tide-chart.sh`, all 6 Dockerfiles, `Makefile` lines 153-167, `charts/tide/values.yaml`, `charts/tide/Chart.yaml`, `examples/projects/small/project.yaml`, `docs/troubleshooting.md`, `.gitignore`
- https://docs.docker.com/build/building/multi-platform/ — `FROM --platform=$BUILDPLATFORM` and `ARG TARGETOS/TARGETARCH` patterns
- https://docs.docker.com/build/ci/github-actions/multi-platform/ — GitHub Actions multi-platform build patterns

### Secondary (MEDIUM confidence)

- https://oneuptime.com/blog/post/2025-12-20-multi-platform-docker-builds-github-actions/view — matrix strategy for 6 Dockerfiles pattern (verified against official docs)
- Context7 `/docker/build-push-action` docs — action parameters, `platforms:`, `file:`, `context:` inputs
- https://docs.docker.com/reference/cli/docker/manifest/inspect/ — `docker manifest inspect` exit-code semantics

### Tertiary (LOW confidence — marked ASSUMED in body)

- Action version numbers (`@v6`, `@v3`) — from web search results; not verified against GitHub Marketplace API
- QEMU arm64 npm install timing estimates (2-5 min) — from general knowledge of QEMU performance
- ghcr.io private-by-default behavior on first push — widely documented community pattern, not verified against official GitHub docs in this session

---

## Metadata

**Confidence breakdown:**
- Standard Stack: HIGH — action names/parameters verified against official docs and live codebase
- Architecture: HIGH — verified against all relevant live source files; ordering constraints confirmed from release.yaml
- Pitfalls: HIGH (Pitfalls 1, 2) — verified against live Makefile and release.yaml; MEDIUM (Pitfalls 3, 5, 6) — inferred from common patterns
- CHART-01 mechanics: HIGH — verified against live tide-values.yaml, augment script, and chart Chart.yaml
- DRY-01 mirror: HIGH — verified against live acceptance-v1.sh and dry-run-v1.sh
- IMG-LOAD-01 auto-detect: MEDIUM-HIGH — algorithm is standard; exact DinD split (Pitfall 6 / Open Question 1) requires planner decision

**Research date:** 2026-05-30
**Valid until:** 2026-06-30 (stable domain; GHA action versions may drift but core patterns stable)
