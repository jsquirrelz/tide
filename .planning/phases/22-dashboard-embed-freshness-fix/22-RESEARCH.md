# Phase 22: Dashboard Embed Freshness Fix — Research

**Researched:** 2026-06-16
**Domain:** Docker multi-stage builds, GitHub Actions CI gating, Go `//go:embed`, Vite SPA builds
**Confidence:** HIGH — all findings verified by direct inspection of the repo's own files

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| FIX-01 | The dashboard image build embeds the current SPA (regenerate `cmd/dashboard/embed/dist` in the image/release path, or gate staleness in CI) so published images can never ship a bundle older than source — verified against the Telemetry tab rendering. | Multi-stage Dockerfile pattern, CI staleness gate pattern, telemetry marker token all confirmed below. |
</phase_requirements>

---

## Summary

The bug is structural: `Dockerfile.dashboard` copies the repo and runs `go build ./cmd/dashboard` but never runs `npm run build`. The `//go:embed all:dist` directive in `cmd/dashboard/embed/embed.go` bakes whatever is committed at `cmd/dashboard/embed/dist/` into the binary. Nothing in `release.yaml`'s `build-images` job runs `make dashboard-frontend` before building the dashboard image, so the published image embeds whatever was last hand-committed to `dist/`. The release.yaml comment at line 16 even states this explicitly: "The SPA must be pre-built into cmd/dashboard/embed/dist/ before `docker build`."

The current committed bundle (commit `3499011`, Phase 21-02) does actually contain the telemetry content — `cmd/dashboard/embed/dist/assets/index-BEfeN1Kf.js` contains `panel-cache-efficiency` and `telemetry-view` strings confirmed by grep. The memory's `6d7a28f` claim reflects an older state; the committed dist is now current as of Phase 21-02. However, the bug is structural and will recur: any future `dashboard/web/src` change that is not followed by a manual `make dashboard-frontend + git commit dist/` will silently ship a stale bundle. The fix must make freshness automatic, not manual.

The fix has two independent layers: (1) make the image build regenerate the SPA from source automatically — solved with a multi-stage Dockerfile; (2) catch stale `dist/` in PR CI before it reaches the release path — solved with a `make verify-dashboard-freshness` target modeled on the existing `helmify-verify` git-diff gate. A third criterion (Telemetry tab render proof) is addressed by a `grep`-based test on the embedded bundle in the image.

**Primary recommendation:** Multi-stage `Dockerfile.dashboard` with a `--platform=$BUILDPLATFORM` node build stage, plus a `verify-dashboard-freshness` CI gate. Decision on whether to gitignore `cmd/dashboard/embed/dist/` is discussed at length below — recommendation is to KEEP it tracked (Option A) and gate its freshness.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SPA bundle generation | CI/Build | Docker (multi-stage) | Vite runs at build time; the output is a static artifact |
| Embed freshness enforcement | Dockerfile (image layer) | CI gate (pre-merge) | Dockerfile owns self-contained correctness; CI gate catches drift earlier |
| Staleness detection | CI (`git diff` after rebuild) | — | Same pattern as `helmify-verify`; authoritative for PR-time catches |
| Telemetry tab proof | Image smoke test | kind_e2e suite | Grep on embedded JS is cheapest deterministic check |
| Bundle size enforcement | `vitest run` (npm test) | — | Already implemented in `src/__tests__/bundle-size.test.ts` |

---

## Root Cause — Verified Facts

**`Dockerfile.dashboard` (lines 1-37) — VERIFIED by direct read:**
- Has no Node/npm stage. Line 17: `FROM golang:1.26@sha256:...` — Go builder only.
- Line 29: `go build -o dashboard ./cmd/dashboard` — only step; reads `embed/dist` as-is.
- Line 15 comment: "The SPA must be pre-built into cmd/dashboard/embed/dist/ before `docker build`."
- `.dockerignore` re-includes `!cmd/dashboard/embed/dist/**` so the committed dist is available to the Go builder. [VERIFIED: direct file read]

**`release.yaml` `build-images` job — VERIFIED:**
- Steps: `checkout`, `derive-image-tag`, `setup-qemu`, `setup-buildx`, `login-ghcr`, `build-push`. No `setup-node` step, no `npm ci`, no `make dashboard-frontend`. The dashboard matrix entry (`component: tide-dashboard`, `dockerfile: ./Dockerfile.dashboard`) fires with a committed `dist/` from the checkout. [VERIFIED: direct file read, lines 265-327]

**`cmd/dashboard/embed/embed.go` — VERIFIED:**
- Line 39: `//go:embed all:dist` — embeds the entire `dist/` directory tree as `embed.FS`.
- The `all:` prefix retains dotfiles/underscore-prefixed files; not relevant here but future-proof.

**`cmd/dashboard/embed/dist/` — VERIFIED tracked by git:**
- `git check-ignore -v cmd/dashboard/embed/dist/index.html` → "NOT ignored (tracked)". Root `.gitignore`'s `/dist/` is root-relative and does NOT match `cmd/dashboard/embed/dist/`. Three files tracked: `index.html`, `assets/index-BEfeN1Kf.js`, `assets/index-BJNoTuKK.css`. [VERIFIED: git ls-files]
- Most recent commit touching this path: `3499011` ("feat(21-02): add CacheEfficiencyPanel and BreakdownKind level selector to TelemetryView") — Phase 21-02. [VERIFIED: git log]
- Local `dashboard/web/dist/` matches committed `cmd/dashboard/embed/dist/` byte-for-byte (same filenames, same MD5: `a6d51b97e2f4d79013288dccc8ab9bfd` for the JS bundle). [VERIFIED: md5 comparison]

**Telemetry bundle content — VERIFIED:**
- `grep -c "panel-cache-efficiency\|telemetry-view\|view-tab-telemetry" cmd/dashboard/embed/dist/assets/index-BEfeN1Kf.js` → 2 matches. The committed bundle IS the post-telemetry SPA. [VERIFIED: grep]
- Source: `TelemetryView.tsx` contains `data-testid="telemetry-view"` (line 1205) and `data-testid="panel-cache-efficiency"` (line 764); `App.tsx` contains `data-testid="view-tab-telemetry"` (line 162). These testids survive Vite minification because they are string literals. [VERIFIED: grep of source]

---

## Standard Stack

This phase adds no new dependencies. All work is in existing infrastructure files.

| File | Change type | Purpose |
|------|------------|---------|
| `Dockerfile.dashboard` | Add node build stage | Regenerate SPA from source at image build time |
| `.dockerignore` | Add `!dashboard/web/**` (minus node_modules) | Expose SPA source to the node build stage |
| `Makefile` | Add `verify-dashboard-freshness` target | PR/CI staleness gate |
| `.github/workflows/ci.yaml` | Add gate step | Run `verify-dashboard-freshness` per push/PR |
| `.github/workflows/release.yaml` | Add gate step in `helmify-verify` or new job | Fail the release on stale dist |

No new npm packages, no new Go dependencies, no new Go modules.

---

## Package Legitimacy Audit

> No new packages are introduced by this phase. Skipped.

---

## Architecture Patterns

### System Architecture Diagram

```
dashboard/web/src/    ->   [npm ci + npm run build]   ->   dashboard/web/dist/
      (source)               (Dockerfile node stage           (Vite output)
                              --platform=$BUILDPLATFORM)
                                        |
                                        v
                              cmd/dashboard/embed/dist/
                              (COPY from node stage)
                                        |
                                        v
                              [go build ./cmd/dashboard]   ->   /dashboard binary
                                (embed.FS bakes dist in)         (embeds fresh SPA)
```

For the CI staleness gate:

```
PR push  ->  [make dashboard-frontend]  ->  [git diff --quiet cmd/dashboard/embed/dist/]
             (rebuild from src)               PASS: dist/ is current
                                              FAIL: dist/ is stale — commit it before merging
```

### Recommended Project Structure

No structural changes to the project. Files modified in-place:

```
.
├── Dockerfile.dashboard          # Add node build stage (Stage 0)
├── .dockerignore                 # Re-include dashboard/web source (excl. node_modules)
├── Makefile                      # Add verify-dashboard-freshness
└── .github/workflows/
    ├── ci.yaml                   # Add verify-dashboard-freshness step
    └── release.yaml              # Add verify-dashboard-freshness in helmify-verify job
                                    (or a new dedicated job)
```

### Pattern 1: Multi-Stage Dockerfile with --platform=$BUILDPLATFORM Node Stage

**What:** Add a `FROM node:22-alpine@sha256:<pin> AS spa-builder --platform=$BUILDPLATFORM` stage before the Go builder. Run `npm ci && npm run build` inside the container. COPY the output into the Go build stage instead of relying on the committed `dist/`.

**When to use:** When the image build must be self-contained and not depend on pre-built artifacts on the host or in the repo.

**Why `--platform=$BUILDPLATFORM`:** The `build-images` job builds for `linux/amd64,linux/arm64` via QEMU. Adding `--platform=$BUILDPLATFORM` on the node stage means npm runs natively on the builder's architecture (amd64 in CI), not under QEMU emulation. This avoids the 2–5 min QEMU `npm install` cost documented in `release.yaml` line 249–251 for `tide-claude-subagent`. The Go builder stage already uses `--platform=$BUILDPLATFORM` (line 17 of current `Dockerfile.dashboard`).

**Node version:** `.nvmrc` pins `22`. `claude-subagent/Dockerfile` uses `node:22-slim@sha256:7af03b14a13c8cdd38e45058fd957bf00a72bbe17feac43b1c15a689c029c732`. For a build-only stage, `node:22-alpine` is smaller (no Debian cruft). The `@sha256` pin MUST be provided per repo convention. The planner must fetch the current `node:22-alpine` digest at plan time — do not ship without a pin.

**`.dockerignore` update required:** The current `.dockerignore` uses `**` to exclude everything and only re-includes specific patterns. `dashboard/web/src/**` and `dashboard/web/package*.json` and the vite/ts config files are currently excluded. Add the following re-includes:
```
!dashboard/web/src/**
!dashboard/web/index.html
!dashboard/web/package.json
!dashboard/web/package-lock.json
!dashboard/web/vite.config.ts
!dashboard/web/tsconfig*.json
!dashboard/web/.nvmrc
```
Do NOT add `!dashboard/web/node_modules/**` — npm ci runs inside the container and populates node_modules there. The build context stays lean (~544KB of source files).

**Example Dockerfile.dashboard structure:**
```dockerfile
# syntax=docker/dockerfile:1
# Stage 0: build the React SPA from source (never trusts committed dist/).
# --platform=$BUILDPLATFORM: npm runs natively on the builder arch, not under QEMU.
FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:<pin> AS spa-builder
WORKDIR /spa
COPY dashboard/web/package.json dashboard/web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY dashboard/web/src ./src
COPY dashboard/web/index.html dashboard/web/vite.config.ts dashboard/web/tsconfig*.json ./
RUN npm run build
# Output: /spa/dist/

# Stage 1: compile the Go dashboard binary with the freshly built SPA embedded.
FROM --platform=$BUILDPLATFORM golang:1.26@sha256:11fd8f7f63db3b6fb198797042ba4c40a4a34dc83325d3328ca3bc4bb7726786 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Replace the committed dist/ with the freshly built SPA.
RUN rm -rf cmd/dashboard/embed/dist && mkdir -p cmd/dashboard/embed/dist
COPY --from=spa-builder /spa/dist/ cmd/dashboard/embed/dist/
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o dashboard ./cmd/dashboard

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/dashboard .
USER 65532:65532
ENTRYPOINT ["/dashboard"]
```

**Note:** `npm run test` (the vitest run, which includes the 500KB bundle-size gate) runs as part of `make dashboard-frontend` locally. In the Dockerfile, running `npm run test` inside the container is optional and adds build time. The staleness gate in CI serves the same quality purpose. The bundle-size gate specifically reads from `dist/` (see `bundle-size.test.ts` line 26: `const distDir = resolve(process.cwd(), "dist")`), which the node stage produces. The planner must decide whether to include `npm run test` in the Dockerfile or omit it (the CI gate catches regressions; Dockerfile is the ship path).

### Pattern 2: Staleness Gate (verify-dashboard-freshness)

**What:** A Makefile target that runs `make dashboard-frontend` then checks `git diff --quiet cmd/dashboard/embed/dist/`. If the fresh build diverges from committed, the gate fails. Identical in shape to the `helmify-verify` job that protects chart reproducibility.

**When to use:** In PR CI (`ci.yaml`) and at release time (`release.yaml`). This catches the scenario where a developer modifies `dashboard/web/src` and commits without regenerating `dist/`.

**Determinism:** Vite uses content-based chunk hashes. Same source tree produces the same chunk filename (`index-BEfeN1Kf.js`) and same bytes. The git diff gate is reliable. [ASSUMED — Vite content-hash determinism is well-documented behavior but was not verified by a test rebuild in this session; the committed chunk filename being stable across the Phase 21-02 → current period supports this.]

**Example Makefile target:**
```makefile
.PHONY: verify-dashboard-freshness
verify-dashboard-freshness: ## Gate: rebuild the SPA and fail if cmd/dashboard/embed/dist/ differs from committed (FIX-01 staleness gate).
    $(MAKE) dashboard-frontend
    @if ! git diff --quiet cmd/dashboard/embed/dist/; then \
        echo "FAIL: cmd/dashboard/embed/dist/ is stale — run 'make dashboard-frontend' and commit the result"; \
        git diff --stat cmd/dashboard/embed/dist/; \
        exit 1; \
    fi
    @echo "PASS: cmd/dashboard/embed/dist/ matches a fresh make dashboard-frontend"
```

**CI integration — `ci.yaml`:**
Add a step after the existing `go vet` step (or as a separate job alongside `helm-lint`). Requires `setup-node` with `node-version: '22'` and `cache: 'npm'` pointing at `dashboard/web/package-lock.json`.

```yaml
- name: Setup Node.js (dashboard SPA gate)
  uses: actions/setup-node@v4
  with:
    node-version: '22'
    cache: 'npm'
    cache-dependency-path: dashboard/web/package-lock.json

- name: Verify dashboard embed freshness (FIX-01)
  run: make verify-dashboard-freshness
```

**CI integration — `release.yaml`:**
The existing `helmify-verify` job is the best home: it already runs on both rc and full tags, is a cheap non-blocking gate, and uses `contents: read` only. Add the same `setup-node` + `verify-dashboard-freshness` steps. Alternatively, add a parallel `frontend-verify` job with the same `needs: []` and `if: true` (fires on both rc and full tags), then gate `build-images` on it.

### Pattern 3: Telemetry-Tab Proof (Criterion #3)

**What:** A lightweight check that the binary embedded `dist/` contains the post-telemetry SPA, not a stale pre-telemetry bundle.

**Marker token:** `panel-cache-efficiency` is the most specific string present in `TelemetryView.tsx` (the `data-testid` on the CacheEfficiencyPanel, line 764 of `TelemetryView.tsx`). It appears in the committed bundle and is NOT in a pre-Phase-16 build. It survives Vite minification because it's a string literal attribute value.

**Cheapest check:** A shell one-liner in CI or in the Go `dashboard_test.go` kind_e2e suite:
```bash
# After building the dashboard binary (or the image):
grep -q 'panel-cache-efficiency' cmd/dashboard/embed/dist/assets/*.js \
  && echo "PASS: telemetry bundle present" \
  || (echo "FAIL: embedded bundle missing telemetry content"; exit 1)
```

This can live as a new `verify-dashboard-telemetry` Makefile target, called from the same CI step that runs `verify-dashboard-freshness`, or wired into `dashboard-build` as a post-build assertion. The planner should decide whether this is a Makefile gate, a CI step, or a new Go test in `dashboard_test.go`.

**Alternative (Go test):** Add a `TestEmbeddedBundleTelemetry` plain Go test in `cmd/dashboard/embed/` that opens `dist/assets/*.js` via `embed.FS` and asserts `strings.Contains(content, "panel-cache-efficiency")`. This compiles with the package and runs under `make test`. Con: it fails if dist/ is stale locally, which is a runtime confusion (the build failed, not the test). The shell/CI approach is cleaner for this specific check.

### Anti-Patterns to Avoid

- **Relying on committed `dist/` in the release path:** the current bug; eliminated by the multi-stage Dockerfile.
- **Running npm under QEMU arm64 emulation in the Docker stage:** slow (2-5 min per build) and the cause of the `timeout-minutes: 30` on build-images. Avoided by `--platform=$BUILDPLATFORM` on the node stage.
- **Auto-installing node in the Go builder stage:** unnecessary complexity; the multi-stage pattern keeps runtimes separated.
- **Adding `npm run test` to the Dockerfile's node stage:** the vitest bundle-size test reads from `dist/` which is produced in the same stage — technically works, but it adds minutes to every image build. Better run in CI's freshness gate or locally.
- **Caching node_modules across matrix jobs in `build-images`:** the `type=gha,scope=${{ matrix.component }}` cache already scopes per-component. Add `--mount=type=cache,target=/root/.npm` inside the Dockerfile's node stage for npm's own cache (avoids re-downloading packages on retries).
- **Hash-only staleness check (content-hash diff):** `git diff --quiet` is simpler and sufficient given Vite's content-determinism. No need for a bespoke hash script.

---

## Decision: Should `cmd/dashboard/embed/dist/` Stay Committed? (Option A vs B)

### Option A — Keep tracked, add staleness gate (RECOMMENDED)

**Approach:** Keep `cmd/dashboard/embed/dist/` committed to git. Add a CI `verify-dashboard-freshness` gate. Update `Dockerfile.dashboard` to multi-stage so the image build regenerates from source regardless.

**Pros:**
- `go build ./cmd/dashboard`, `go vet ./...`, `make test`, and `go test ./...` all compile without any pre-step. The `//go:embed all:dist` directive requires `dist/` to be present and non-empty at compile time; if it is absent, `go build` fails with "pattern all:dist: no matching files found". Tracking `dist/` means `make test` and `go vet ./...` work immediately after `git clone`.
- Matches the ROADMAP success criterion #2 wording: "CI fails a build whose embedded `dist` is older than the dashboard source" — implies there is a committed dist that can be checked for staleness.
- The multi-stage Dockerfile generates a fresh bundle anyway at image build time; what's committed is irrelevant to the published image.
- No blast-radius risk: CI (unit tests, vet, lint) continues to work without any pre-build step.
- Precedent: the demo-fixture takes the opposite approach (gitignored, generated at build time) but its Dockerfile handles it with a COPY from the source path. The dashboard differs because `cmd/dashboard/embed/dist/` is imported by `go test ./...` whereas `cmd/tide-demo-init/fixture/` is only needed when building `cmd/tide-demo-init`.

**Cons:**
- `dist/` can drift from source if a developer commits `dashboard/web/src` changes without regenerating. This is the very bug being fixed — mitigated by the staleness gate.

### Option B — Gitignore dist, generate in build (NOT RECOMMENDED for this phase)

**Approach:** Add `cmd/dashboard/embed/dist/` to `.gitignore`. Require callers to run `make dashboard-frontend` before any `go build ./cmd/dashboard` or `go test ./...`.

**Blast radius if dist/ is absent:**
- `go vet ./...` (called by `make vet`, `make test`, `make build`) fails: embed directive cannot find the directory.
- `go test -short ./...` (called by `make test`, `make test-only`, CI's `Unit suite (TEST-01)`) fails: `cmd/dashboard/embed` is in `go list ./...` output.
- `make lint` → `demo-fixture` → `go vet ./...` → fails.
- ALL CI jobs in `ci.yaml` that run `make vet`, `make test`, or `go vet ./...` break.
- New contributors who `git clone` and run `make test` get a cryptic compile error.

**Mitigation cost:** Add `make dashboard-frontend` as a prerequisite to `vet`, `test`, `test-only`, `lint`. This downloads npm and runs a Vite build for every CI run — adding ~2-3 minutes to the per-push critical path (TEST-01 would no longer fit in 120s).

**Verdict:** Option B requires a much larger change surface and breaks the fast CI path. Option A solves the ship-time bug (multi-stage Dockerfile) and catches future drift (staleness gate) without touching the test pipeline. Use Option A.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Multi-arch node stage | Custom cross-compilation | `--platform=$BUILDPLATFORM` | BuildKit handles it; npm itself is not cross-compiled |
| npm cache in Docker | Custom layer caching | `--mount=type=cache,target=/root/.npm` | BuildKit cache mounts; already used in Go builder stage |
| Staleness detection | Bespoke hash script | `git diff --quiet` after rebuild | Exact same pattern as `helmify-verify`; idiomatic for this repo |
| Telemetry proof | Browser automation | `grep` on embedded JS | String literal attributes survive minification; zero deps |

---

## Common Pitfalls

### Pitfall 1: QEMU arm64 npm install slowness
**What goes wrong:** A `FROM node:22-alpine` stage without `--platform=$BUILDPLATFORM` runs under QEMU emulation when building the `linux/arm64` platform slice. npm install under emulation takes 2-5 minutes (documented for `tide-claude-subagent` in `release.yaml` line 249).
**Why it happens:** BuildKit runs each stage on the target architecture by default; QEMU emulates arm64 on the amd64 CI runner.
**How to avoid:** Add `--platform=$BUILDPLATFORM` to the node stage declaration. The node stage produces the SPA (platform-agnostic JS/CSS); it does not need to run on the target arch.
**Warning signs:** `build-images` matrix job for `tide-dashboard` takes >5 min when it previously took <2 min.

### Pitfall 2: `dashboard/web` source excluded from Docker build context
**What goes wrong:** The current `.dockerignore` uses `**` to exclude everything. `dashboard/web/src/` is excluded. Without the `!dashboard/web/**` re-include (minus `node_modules`), the COPY step in the node build stage fails silently or copies 0 files.
**Why it happens:** The whitelist `.dockerignore` pattern was written before the multi-stage build was planned.
**How to avoid:** Add explicit re-includes for `dashboard/web` source files. Do NOT add `!dashboard/web/node_modules/**` — this would bloat the build context by 227MB.
**Warning signs:** Node stage's `COPY dashboard/web/src ./src` copies 0 files; Vite build produces an empty `dist/`.

### Pitfall 3: Vite build non-determinism (false alarm risk)
**What goes wrong:** If Vite produces different output across runs (different chunk filenames), the `git diff --quiet` staleness gate always fails even when nothing in `src/` changed.
**Why it happens:** Some build tools use timestamps or random seeds for chunk naming.
**How to avoid:** Vite uses content-based hashes; same input → same chunk filename. The committed `index-BEfeN1Kf.js` has remained stable across the Phase 15→21 period (no chunk rename observed). The `--mode production` flag (Vite default for `npm run build`) is deterministic. [ASSUMED: determinism is documented Vite behavior; not verified by an experiment in this session — low risk based on observed stability.]
**Warning signs:** `verify-dashboard-freshness` fails consistently after a fresh rebuild of unchanged source.

### Pitfall 4: `npm ci` fails without `node_modules` in build context
**What goes wrong:** `npm ci` requires `package.json` and `package-lock.json` to be present. If the `.dockerignore` re-includes only `dashboard/web/src/**` but not `dashboard/web/package*.json`, the node stage fails at `npm ci`.
**How to avoid:** Explicitly re-include `!dashboard/web/package.json` and `!dashboard/web/package-lock.json` in `.dockerignore`.

### Pitfall 5: `//go:embed all:dist` fails if `dist/` has no files
**What goes wrong:** Go's `embed` package requires at least one file to match the pattern. An empty `dist/` directory causes `go build` to error: "pattern all:dist: no matching files found".
**How to avoid:** Under Option A (dist tracked), `dist/` always has the three committed files. Under the multi-stage approach, the `COPY --from=spa-builder /spa/dist/ cmd/dashboard/embed/dist/` step always produces a non-empty dist before `go build` runs.

### Pitfall 6: `@sha256` pin required for node base image
**What goes wrong:** Without a digest pin, a `node:22-alpine` tag can silently update to a newer image version between CI runs, producing non-reproducible builds.
**How to avoid:** Pin the node stage image with `@sha256:<digest>` per repo convention (observed in `Dockerfile.dashboard` line 17 for golang, `claude-subagent/Dockerfile` line 46 for node:22-slim). The planner must fetch the current `node:22-alpine` digest at plan authoring time.

---

## Code Examples

### Multi-Stage Dockerfile.dashboard (skeleton — planner fills in `@sha256` pin)

```dockerfile
# syntax=docker/dockerfile:1
# Stage 0 — SPA builder (native arch, never QEMU emulated).
# Regenerates cmd/dashboard/embed/dist from current dashboard/web/src.
FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:<PLANNER_MUST_FILL> AS spa-builder
WORKDIR /spa
COPY dashboard/web/package.json dashboard/web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --prefer-offline
COPY dashboard/web/src ./src
COPY dashboard/web/index.html \
     dashboard/web/vite.config.ts \
     dashboard/web/tsconfig.json \
     dashboard/web/tsconfig.app.json \
     dashboard/web/tsconfig.node.json ./
RUN npm run build

# Stage 1 — Go builder.
FROM --platform=$BUILDPLATFORM golang:1.26@sha256:11fd8f7f63db3b6fb198797042ba4c40a4a34dc83325d3328ca3bc4bb7726786 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN rm -rf cmd/dashboard/embed/dist
COPY --from=spa-builder /spa/dist/ cmd/dashboard/embed/dist/
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o dashboard ./cmd/dashboard

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/dashboard .
USER 65532:65532
ENTRYPOINT ["/dashboard"]
```

### verify-dashboard-freshness Makefile target

```makefile
.PHONY: verify-dashboard-freshness
verify-dashboard-freshness: ## Rebuild the SPA and fail if cmd/dashboard/embed/dist/ diverges from committed (FIX-01).
	$(MAKE) dashboard-frontend
	@if ! git diff --quiet cmd/dashboard/embed/dist/; then \
		echo "FAIL: cmd/dashboard/embed/dist/ is stale"; \
		echo "Run 'make dashboard-frontend' and commit the updated dist/ before merging."; \
		git diff --stat cmd/dashboard/embed/dist/; \
		exit 1; \
	fi
	@echo "PASS: cmd/dashboard/embed/dist/ is fresh"
```

### Telemetry bundle assertion

```bash
# Shell gate (in Makefile or CI):
@MARKER="panel-cache-efficiency"; \
if grep -qr "$$MARKER" cmd/dashboard/embed/dist/assets/*.js 2>/dev/null; then \
    echo "PASS: embedded bundle contains telemetry content ($$MARKER)"; \
else \
    echo "FAIL: embedded bundle missing telemetry marker '$$MARKER' — stale pre-telemetry bundle?"; \
    exit 1; \
fi
```

### `.dockerignore` additions

```
# Phase 22: multi-stage SPA build — expose dashboard/web source to node stage.
# node_modules/ is intentionally excluded (npm ci runs inside the container).
!dashboard/web/src/**
!dashboard/web/index.html
!dashboard/web/package.json
!dashboard/web/package-lock.json
!dashboard/web/vite.config.ts
!dashboard/web/tsconfig.json
!dashboard/web/tsconfig.app.json
!dashboard/web/tsconfig.node.json
!dashboard/web/.nvmrc
```

### CI staleness gate step (`ci.yaml`)

```yaml
- name: Setup Node.js 22 (dashboard SPA gate)
  uses: actions/setup-node@v4
  with:
    node-version: '22'
    cache: 'npm'
    cache-dependency-path: dashboard/web/package-lock.json

- name: Verify dashboard embed freshness (FIX-01)
  run: make verify-dashboard-freshness
```

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | vitest 1.6.1 (frontend unit); Ginkgo v2.28 (Go); shell assertions (CI gates) |
| Config file | `dashboard/web/vitest.config.ts` |
| Quick run command | `cd dashboard/web && npm run test` |
| Full suite command | `make test` (Go units) + `make verify-dashboard-freshness` (freshness) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| FIX-01a | Dashboard image build regenerates SPA from source | Integration (Docker build smoke) | `docker build -f Dockerfile.dashboard . --target spa-builder` | ❌ Wave 0 (new Dockerfile stage) |
| FIX-01b | CI fails when committed dist/ is stale | Shell gate | `make verify-dashboard-freshness` | ❌ Wave 0 (new Makefile target) |
| FIX-01c | Embedded bundle contains Telemetry tab content | Shell assertion | `make verify-dashboard-telemetry` (new target) or inline in `verify-dashboard-freshness` | ❌ Wave 0 |
| FIX-01d | Bundle size ≤ 500KB | Unit (vitest) | `cd dashboard/web && npm run test` | ✅ `src/__tests__/bundle-size.test.ts` |

### Sampling Rate

- **Per task commit:** `make verify-dashboard-freshness` (runs `make dashboard-frontend` + git diff — ~2 min)
- **Per wave merge:** Full `make test` (Go units) + `make verify-dashboard-freshness`
- **Phase gate:** Multi-stage Dockerfile build smoke + `make verify-dashboard-freshness` green

### Wave 0 Gaps

- [ ] `Makefile` — add `verify-dashboard-freshness` and `verify-dashboard-telemetry` targets
- [ ] `Dockerfile.dashboard` — add node spa-builder stage
- [ ] `.dockerignore` — add dashboard/web source re-includes
- [ ] `.github/workflows/ci.yaml` — add setup-node + verify-dashboard-freshness step
- [ ] `.github/workflows/release.yaml` — add verify-dashboard-freshness to helmify-verify job (or new job)

---

## Security Domain

This phase makes no network-exposed changes. No new ASVS categories apply. The node build stage runs `npm ci` (deterministic lockfile install), which is the safe pattern (no `npm install` without lockfile). No new secrets or credentials are introduced.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Node.js 22 | `make verify-dashboard-freshness`, Docker spa-builder stage | ✓ | v22.22.1 (local) | actions/setup-node@v4 in CI |
| npm | `npm ci`, `npm run build` | ✓ | bundled with Node | — |
| Docker Buildx | Multi-stage image build | ✓ (CI: setup-buildx-action@v3) | — | — |
| QEMU (for arm64) | `linux/arm64` platform in build-images matrix | ✓ (CI: setup-qemu-action@v3) | — | NOT needed for node stage (--platform=$BUILDPLATFORM) |

**Missing dependencies with no fallback:** None. All dependencies are available in the existing CI environment.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Vite content-based chunk hashes are deterministic (same source → same filename + bytes) | Pitfall 3, staleness gate | If non-deterministic, `git diff` staleness gate produces false positives; would need a content-hash comparison script instead |
| A2 | The multi-stage `COPY --from=spa-builder` runs the correct node version and produces the same bundle as local `make dashboard-frontend` | Pattern 1 | Mismatch could mean CI gate passes locally but Docker produces a different bundle; risk is low given Node 22 version lock |

---

## Open Questions (RESOLVED)

All three questions were resolved during planning (Phase 22 plans `22-01`/`22-02`) and locked into the plans' `<locked_decisions>` blocks: (1) RESOLVED — omit `npm run test` from the Dockerfile node stage; (2) RESOLVED — add the gate as a STEP in the existing `helmify-verify` job, not a new job; (3) RESOLVED — `node:22-alpine` for the spa-builder stage.

1. **Should `npm run test` (vitest bundle-size gate) run inside the Dockerfile node stage?**
   - What we know: bundle-size.test.ts reads from `dist/` after `npm run build` — technically runnable in the node stage. Adds ~30-60s to every `docker build`.
   - What's unclear: Is catching a bundle bloat during `docker build` worth the time cost when `make dashboard-frontend` (local) and `verify-dashboard-freshness` (CI gate) already run it?
   - Recommendation: Omit from Dockerfile. CI gate and local `make dashboard-frontend` both run `npm run test`. Dockerfile is the ship path, not the quality gate.

2. **Where exactly should `verify-dashboard-freshness` land in `release.yaml`?**
   - What we know: `helmify-verify` job is the existing pattern — runs on both rc and full tags, `contents: read` only, cheap. Adding it there is minimal surface.
   - What's unclear: Should it be a step in `helmify-verify` (rename the job to "release gates" or "reproducibility gates") or a new parallel job?
   - Recommendation: Add as a step in the existing `helmify-verify` job (rename the job to `release-gates` or add after the chart verification steps). Both gates are "reproducibility" checks; a single job simplifies the DAG.

3. **`node:22-alpine` vs `node:22-slim` for the spa-builder stage?**
   - What we know: `node:22-slim` (Debian) is used by `claude-subagent` for runtime (needs Debian utilities). For a build-only stage, `node:22-alpine` is leaner.
   - Recommendation: Use `node:22-alpine` for the build stage — smaller image, no runtime required.

---

## Sources

### Primary (HIGH confidence)
- Direct file read: `Dockerfile.dashboard` (verified build steps and comment at line 15)
- Direct file read: `.github/workflows/release.yaml` (verified build-images matrix, no npm steps)
- Direct file read: `cmd/dashboard/embed/embed.go` (verified `//go:embed all:dist`)
- Direct file read: `Makefile` (verified `dashboard-frontend` target, `verify-*` patterns, `demo-fixture` precedent)
- Direct file read: `.dockerignore` (verified `!cmd/dashboard/embed/dist/**` re-include)
- `git ls-files cmd/dashboard/embed/dist/` (verified 3 files tracked)
- `git log --oneline -- cmd/dashboard/embed/dist/ | head -5` (verified most recent commit is 3499011)
- `grep -c "panel-cache-efficiency|telemetry-view" cmd/dashboard/embed/dist/assets/*.js` (verified telemetry content present)
- Direct file read: `dashboard/web/src/components/TelemetryView.tsx` (verified data-testid="panel-cache-efficiency" at line 764)
- Direct file read: `dashboard/web/src/App.tsx` (verified data-testid="view-tab-telemetry" at line 162)
- Direct file read: `dashboard/web/src/__tests__/bundle-size.test.ts` (verified 500KB gate)
- `md5` comparison of `dashboard/web/dist/assets/index-BEfeN1Kf.js` vs committed embed/dist (verified matching hashes)
- Direct file read: `.planning/REQUIREMENTS.md` FIX-01 wording
- Direct file read: `.planning/ROADMAP.md` Phase 22 success criteria

### Secondary (MEDIUM confidence)
- `release.yaml` line 249-251 comment documenting QEMU npm install timing (2-5 min)
- `.nvmrc` pinning Node 22

### Tertiary (LOW confidence)
- None

---

## Metadata

**Confidence breakdown:**
- Root cause: HIGH — verified by direct file inspection and git log
- Fix mechanism (multi-stage Dockerfile): HIGH — exact precedent in tide-demo-init/Dockerfile pattern; BuildKit docs for `--platform=$BUILDPLATFORM`
- Staleness gate: HIGH — exact precedent in `helmify-verify` job; same `git diff --quiet` pattern
- Vite determinism: MEDIUM — documented behavior, empirically stable, but not run-tested in this session (A1 in assumptions log)

**Research date:** 2026-06-16
**Valid until:** Stable until Dockerfile.dashboard, release.yaml, or Makefile are significantly restructured — not expected before v1.0.3.
