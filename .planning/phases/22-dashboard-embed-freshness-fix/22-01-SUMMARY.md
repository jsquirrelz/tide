---
phase: 22-dashboard-embed-freshness-fix
plan: "01"
subsystem: infra
tags: [docker, multi-stage-build, node, npm, makefile, spa, vite, go-embed, ci-gate]

requires: []
provides:
  - "Dockerfile.dashboard with digest-pinned node:22-alpine spa-builder stage that regenerates dist/ on every image build"
  - "make verify-dashboard-freshness gate that rebuilds the SPA and fails on stale dist/ or missing telemetry marker"
  - ".dockerignore re-includes for dashboard/web source (excl. node_modules)"
affects: [22-02, release.yaml, ci.yaml]

tech-stack:
  added: ["node:22-alpine@sha256:e58326d0d441090181ac150dc2078d3e2cf6a0d42e809aebba3ef5880935ffdd (build-stage only)"]
  patterns:
    - "Multi-stage Dockerfile: node spa-builder feeds freshly built dist/ into Go builder via COPY --from"
    - "Staleness gate pattern: make target runs make dashboard-frontend then git diff --quiet (mirrors helmify-verify)"

key-files:
  created: []
  modified:
    - Dockerfile.dashboard
    - .dockerignore
    - Makefile

key-decisions:
  - "Keep cmd/dashboard/embed/dist/ tracked in git (Option A) so go vet/make test compile from a clean clone without pre-steps"
  - "Use node:22-alpine (not node:22-slim) for the build-only spa-builder stage — smaller, no runtime deps needed"
  - "Use --platform=$BUILDPLATFORM on the node stage so npm runs natively on the builder arch, never under QEMU emulation"
  - "Use npm ci in the node stage — deterministic lockfile install, never bare npm install"
  - "Omit npm run test from the Dockerfile node stage — CI freshness gate and local make dashboard-frontend already run vitest"
  - "Fold telemetry marker assertion into verify-dashboard-freshness (not a separate target) — single gate covers both staleness and telemetry proxy"

patterns-established:
  - "Docker freshness pattern: COPY . . then rm -rf <embed-dir> + COPY --from=<build-stage> overwrites committed artifacts before go build"
  - "Staleness gate pattern: $(MAKE) <rebuild-target> + git diff --quiet <path> + exit 1 on drift (mirrors helmify-verify job)"

requirements-completed: [FIX-01]

duration: 15min
completed: "2026-06-16"
---

# Phase 22 Plan 01: Dashboard Embed Freshness Fix Summary

**Multi-stage Dockerfile.dashboard with digest-pinned node:22-alpine spa-builder that regenerates dist/ from source on every image build, plus a make verify-dashboard-freshness gate that fails on stale dist/ or a missing panel-cache-efficiency telemetry marker**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-06-16T06:30:00Z
- **Completed:** 2026-06-16T10:44:16Z
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments

- Dockerfile.dashboard now has a digest-pinned `node:22-alpine` `spa-builder` stage that runs `npm ci && npm run build` and feeds `/spa/dist` into the Go builder via `COPY --from=spa-builder` over a freshly removed `cmd/dashboard/embed/dist` — the published image can never again ship a frozen pre-telemetry bundle.
- `.dockerignore` re-includes `dashboard/web` SPA source (9 entries, explicitly excluding `node_modules`) so the build context carries the files the node stage needs.
- `make verify-dashboard-freshness` exists, rebuilds via `$(MAKE) dashboard-frontend`, gates on `git diff --quiet cmd/dashboard/embed/dist/`, and asserts the `panel-cache-efficiency` telemetry marker — verified green on a clean tree and wired for Wave-2 CI/release gate integration.
- `cmd/dashboard/embed/dist/` remains tracked in git; `go vet ./cmd/dashboard/...` and `make test` compile from a clean clone without any pre-step (Option A preserved).

## Task Commits

Each task was committed atomically:

1. **Task 1: Add spa-builder node stage to Dockerfile.dashboard and .dockerignore** - `55812c2` (feat)
2. **Task 2: Add verify-dashboard-freshness Makefile target** - `4fbf794` (feat)

## Files Created/Modified

- `Dockerfile.dashboard` - Added digest-pinned `node:22-alpine` spa-builder stage (Stage 0); Go builder stage now replaces committed dist/ via rm-rf + COPY --from before go build; header comment updated to reflect self-contained build
- `.dockerignore` - Added 9 dashboard/web source re-includes for the node stage build context (node_modules intentionally excluded)
- `Makefile` - Added `.PHONY: verify-dashboard-freshness` target after `dashboard-frontend`; runs rebuild + git diff gate + telemetry marker assert; not wired into vet/test/lint critical path

## Decisions Made

Option A (keep dist/ tracked) confirmed from locked_decisions: gitignoring dist/ would break `go vet ./...`, `make test`, and CI go-unit jobs that compile `cmd/dashboard/embed` — blast radius is too large. The multi-stage Dockerfile makes freshness automatic; the staleness gate catches future drift.

Telemetry marker assertion folded into `verify-dashboard-freshness` rather than a separate target — keeps the CI surface minimal (one step wires both checks).

## Deviations from Plan

None — plan executed exactly as written. All locked decisions honored, all acceptance criteria verified, Docker build smoke test passed with confirmed non-empty `/spa/dist/assets/*.js`.

## Issues Encountered

None. Docker was available locally; the `--target spa-builder` build completed in ~35 seconds (image pulled from cache). The `make verify-dashboard-freshness` full run completed in ~45 seconds locally (npm ci had packages cached). All 204 frontend vitest tests passed green during the Task 2 verification run.

## User Setup Required

None — no external service configuration required. Docker and Node.js 22 are already available. Wave-2 (Plan 22-02) wires the CI/release gate steps that call `make verify-dashboard-freshness` in GitHub Actions.

## Next Phase Readiness

- `make verify-dashboard-freshness` is ready for Wave-2 (Plan 22-02) to add as a step in `ci.yaml` and `release.yaml` `helmify-verify` job.
- Full dashboard image build (`docker build -f Dockerfile.dashboard .`) will regenerate the SPA from source — Wave-2 can add this to the release pipeline with confidence.
- `cmd/dashboard/embed/dist/` remains tracked; no changes to the Go test or vet critical path.

---

## Self-Check

### Created files exist:
- `Dockerfile.dashboard` — FOUND (modified)
- `.dockerignore` — FOUND (modified)
- `Makefile` — FOUND (modified)

### Commits exist:
- `55812c2` — feat(22-01): add spa-builder node stage to Dockerfile.dashboard
- `4fbf794` — feat(22-01): add verify-dashboard-freshness Makefile target

### Acceptance criteria verified:
- `grep -q 'FROM --platform=\$BUILDPLATFORM node:22-alpine@sha256:...' Dockerfile.dashboard` — PASS
- `grep -q 'COPY --from=spa-builder /spa/dist/ cmd/dashboard/embed/dist/' Dockerfile.dashboard` — PASS
- `grep -q 'rm -rf cmd/dashboard/embed/dist' Dockerfile.dashboard` — PASS
- rm-rf (line 44) and COPY --from (line 45) both before go build (line 48) — PASS
- `grep -cE 'npm install' Dockerfile.dashboard` = 0 — PASS
- `grep -c 'npm run test' Dockerfile.dashboard` = 0 — PASS
- `grep -q '!dashboard/web/src/\*\*' .dockerignore` — PASS
- `grep -q '!dashboard/web/package-lock.json' .dockerignore` — PASS
- no `!dashboard/web/node_modules` re-include — PASS
- `docker build -f Dockerfile.dashboard --target spa-builder -t tide-dashboard-spa-smoke .` — PASS (built successfully)
- `docker run --rm tide-dashboard-spa-smoke sh -c 'ls /spa/dist/assets/*.js'` — PASS (`/spa/dist/assets/index-BEfeN1Kf.js`)
- `grep -q '^verify-dashboard-freshness:' Makefile` — PASS
- `grep -q '^\.PHONY: verify-dashboard-freshness' Makefile` — PASS
- target references dashboard-frontend, git diff --quiet, panel-cache-efficiency — PASS
- `make verify-dashboard-freshness` on clean tree — PASS (exits 0, PASS on both diff and telemetry marker)
- not in vet/test/test-only/lint prerequisites — PASS
- `go vet ./cmd/dashboard/...` — PASS
- `git ls-files cmd/dashboard/embed/dist/` — 3 files tracked (Option A preserved)

## Self-Check: PASSED

*Phase: 22-dashboard-embed-freshness-fix*
*Completed: 2026-06-16*
