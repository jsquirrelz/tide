---
phase: 22-dashboard-embed-freshness-fix
plan: "02"
subsystem: ci
tags: [github-actions, ci-gate, freshness, node, npm, makefile, release-gate]

requires:
  - "22-01 (make verify-dashboard-freshness Makefile target)"
provides:
  - "PR-time dashboard embed freshness gate in ci.yaml test job (actions/setup-node@v4 + make verify-dashboard-freshness)"
  - "Release-time dashboard embed freshness gate in release.yaml helmify-verify job (same step pair, fires before release/build-images)"
affects: [release.yaml, ci.yaml]

tech-stack:
  added: ["actions/setup-node@v4 (node 22, npm cache on dashboard/web/package-lock.json)"]
  patterns:
    - "Gate step pair pattern: actions/setup-node@v4 (node 22, lockfile-keyed npm cache) + make verify-dashboard-freshness — reused in both ci.yaml and release.yaml"
    - "Defense-in-depth: same gate fires at PR time (ci.yaml) and release time (release.yaml helmify-verify) before any binary/image publishes"

key-files:
  created: []
  modified:
    - .github/workflows/ci.yaml
    - .github/workflows/release.yaml

key-decisions:
  - "Place freshness step pair after go vet and before Prepare test environment in ci.yaml — after checkout+setup-go so working tree and toolchain exist; no disruption to TEST-01/TEST-02 budget steps"
  - "Append freshness step pair AFTER the chart-reproducibility gate in helmify-verify (not a new job) — inherits the job's existing needs/if wiring so it gates both release and build-images on both rc and full tags"
  - "Bump helmify-verify timeout-minutes from 5 to 10 — npm ci + vite build adds ~2-3 min on a cold CI runner; 10 min gives safe headroom without masking a genuine hang"
  - "No job permissions changed — both jobs keep contents: read; the freshness gate is a read-only reproducibility check needing no extra scope (T-22-04 accepted)"

metrics:
  duration: "~2 min"
  completed: "2026-06-16"
---

# Phase 22 Plan 02: CI/Release Freshness Gate Wiring Summary

**Wire `make verify-dashboard-freshness` into ci.yaml (PR-time gate in the `test` job) and release.yaml (release-time gate as a step in `helmify-verify`), using `actions/setup-node@v4` (node 22, npm cache on `dashboard/web/package-lock.json`) before each invocation**

## Performance

- **Duration:** ~2 min
- **Started:** 2026-06-16T10:49:40Z
- **Completed:** 2026-06-16T10:52:00Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- `ci.yaml` `test` job now runs `actions/setup-node@v4` (node 22, npm cache keyed on `dashboard/web/package-lock.json`) followed by `make verify-dashboard-freshness` after `go vet` — every push and PR that edits dashboard source without regenerating `cmd/dashboard/embed/dist/` now fails the CI gates job.
- `release.yaml` `helmify-verify` job now runs the same step pair appended after the "Verify chart tree is reproducible" step — a release tag (rc or full) with a stale dist/ fails helmify-verify before `release` or `build-images` fire.
- `helmify-verify` `timeout-minutes` bumped from 5 to 10 with an explanatory comment (npm ci + vite build adds ~2–3 min on a cold CI runner).
- Both workflow files validate as clean YAML; no new jobs added; `release`/`build-images` `needs:` wiring unchanged; job permissions unchanged (`contents: read`).

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the freshness gate (setup-node + make verify-dashboard-freshness) to ci.yaml** - `8e54487` (feat)
2. **Task 2: Add the freshness gate as a step inside release.yaml's helmify-verify job** - `f560080` (feat)

## Files Created/Modified

- `.github/workflows/ci.yaml` — Added `actions/setup-node@v4` + `make verify-dashboard-freshness` step pair after `go vet` in the `test` job (15 lines inserted)
- `.github/workflows/release.yaml` — Added `actions/setup-node@v4` + `make verify-dashboard-freshness` step pair after "Verify chart tree is reproducible" in `helmify-verify`; bumped `timeout-minutes` from 5 to 10 (21 lines inserted, 1 changed)

## Decisions Made

- Freshness step pair placed after `go vet` in ci.yaml (not after the last verify-* gate before the test environment prep) — the plan said "alongside go vet" and this placement keeps it clearly in the gates group while ensuring checkout+setup-go have already run.
- `timeout-minutes` bumped to 10 in helmify-verify per plan guidance ("bump to a small headroom value (e.g. 10) and note why in a comment") — the npm ci + vite build takes ~45s locally and ~2–3 min on cold CI; 10 min covers the worst case with margin.

## Deviations from Plan

None — plan executed exactly as written. All locked decisions honored, all acceptance criteria verified.

## Known Stubs

None.

## Threat Flags

None. The change adds only CI gate steps inside existing jobs scoped to `contents: read`. No new runtime surface, secrets, or permission scopes introduced. T-22-03 (npm ci deterministic via lockfile) and T-22-04 (read-only scope) dispositions from the plan's threat register are satisfied.

---

## Self-Check

### Files exist:
- `.github/workflows/ci.yaml` — FOUND (modified)
- `.github/workflows/release.yaml` — FOUND (modified)

### Commits exist:
- `8e54487` — feat(22-02): add dashboard embed freshness gate to ci.yaml
- `f560080` — feat(22-02): add dashboard embed freshness gate to release.yaml helmify-verify

### Acceptance criteria verified:

**ci.yaml:**
- `grep -q 'make verify-dashboard-freshness' .github/workflows/ci.yaml` — PASS
- `grep -q 'actions/setup-node@v4' .github/workflows/ci.yaml` — PASS
- `grep -q 'cache-dependency-path: dashboard/web/package-lock.json' .github/workflows/ci.yaml` — PASS
- `grep -q "node-version: '22'" .github/workflows/ci.yaml` — PASS
- setup-node (line 92) appears before verify-dashboard-freshness (line 99) — PASS
- `grep -q 'make test-only' .github/workflows/ci.yaml` (TEST-01 unchanged) — PASS
- `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yaml'))"` — PASS

**release.yaml:**
- `grep -q 'make verify-dashboard-freshness' .github/workflows/release.yaml` — PASS
- verify-dashboard-freshness (line 107) is before pre-flight: job key (line 125) — PASS (within helmify-verify)
- `grep -q 'actions/setup-node@v4' .github/workflows/release.yaml` — PASS
- `grep -q 'cache-dependency-path: dashboard/web/package-lock.json' .github/workflows/release.yaml` — PASS
- `release needs: [helmify-verify, pre-flight]` unchanged — PASS
- `build-images needs: [helmify-verify]` unchanged — PASS
- No new top-level job keys (helmify-verify, pre-flight, release, build-images, chart-publish) — PASS
- `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yaml'))"` — PASS
- `timeout-minutes: 10` in helmify-verify — PASS

## Self-Check: PASSED

*Phase: 22-dashboard-embed-freshness-fix*
*Completed: 2026-06-16*
