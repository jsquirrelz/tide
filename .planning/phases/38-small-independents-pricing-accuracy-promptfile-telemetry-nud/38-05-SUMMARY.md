---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 05
subsystem: dashboard
tags: [telemetry, banner, dashboard, config-endpoint, TELEM-03]
requires:
  - "PROM_ENDPOINT env precedent in cmd/dashboard/main.go (phase 04)"
  - "PanelState machine + unavailable sentinel in TelemetryView.tsx (phase 16)"
provides:
  - "GET /api/v1/config → {\"telemetryEnabled\": bool} (locked wire contract)"
  - "Dependencies.TelemetryEnabled resolved via telemetryEnabledFromEnv()"
  - "TelemetryDisabledBanner component (two text-distinct states)"
  - "TelemetryView banner derivation per 38-UI-SPEC Banner Contract"
affects:
  - "38-04 (chart side of D-14 passes PROMETHEUS_ENABLED into the dashboard Pod)"
tech-stack:
  added: []
  patterns:
    - "config endpoint injected as resolved bool — handler never reads env"
    - "banner precedence: data suppresses > disabled-by-config > no-data > hidden"
key-files:
  created:
    - cmd/dashboard/api/config.go
    - cmd/dashboard/api/config_test.go
    - cmd/dashboard/main_test.go
    - dashboard/web/src/components/TelemetryDisabledBanner.tsx
    - dashboard/web/src/components/TelemetryDisabledBanner.test.tsx
  modified:
    - cmd/dashboard/main.go
    - cmd/dashboard/router.go
    - dashboard/web/src/components/TelemetryView.tsx
    - dashboard/web/src/components/__tests__/TelemetryView.test.tsx
decisions:
  - "telemetryEnabledFromEnv: literal PROMETHEUS_ENABLED true/false is authoritative (explicit false wins over a set PROM_ENDPOINT); unset/unrecognized falls back to PROM_ENDPOINT presence (D-13/D-14 legacy-chart compatibility)"
  - "Route registered as r.Get(\"/config\") inside the existing /api/v1 Route block (chi nesting convention); composed-path shape locked by TestConfigRouteRegistered walking chi for GET /api/v1/config exactly once"
  - "Any panel with real data suppresses the banner outright, including over telemetryEnabled=false (T-38-15 spoofing mitigation)"
metrics:
  duration: "~10 min"
  completed: "2026-07-11"
status: complete
---

# Phase 38 Plan 05: TELEM-03 Telemetry Disabled Banner Summary

One-boolean config surface (GET /api/v1/config) feeding a single view-level Telemetry banner that text-distinguishes disabled-by-config (PROMETHEUS_ENABLED=false, warning border) from no-data (enabled but zero samples, subtle border), with panel data/loading/unreachable suppressing it entirely.

## Task Commits

| Task | Phase | Commit | Description |
|------|-------|--------|-------------|
| 1 | RED | `d57209a` | failing tests: ConfigHandler wire contract, telemetryEnabledFromEnv table, route-table shape |
| 1 | GREEN | `60b185f` | ConfigHandler + Dependencies.TelemetryEnabled + telemetryEnabledFromEnv + route registration |
| 2 | RED | `3237934` | failing tests: banner component contract + TelemetryView derivation behaviors 3-6 |
| 2 | GREEN | `4efa08e` | TelemetryDisabledBanner component + TelemetryView config fetch and precedence derivation |

## What Was Built

**Server (Task 1):**
- `cmd/dashboard/api/config.go` — `ConfigHandler{TelemetryEnabled bool, Log logr.Logger}`; `Get` writes HTTP 200 `application/json` body `{"telemetryEnabled":<bool>}`. GET-only per DASH-05.
- `cmd/dashboard/router.go` — `Dependencies.TelemetryEnabled` field (doc cites D-14); `r.Get("/config", configHandler.Get)` inside the `/api/v1` Route block; route-table doc line `GET /api/v1/config — telemetry-enabled flag (Phase 38 TELEM-03)`.
- `cmd/dashboard/main.go` — `telemetryEnabledFromEnv()`: `"true"`→true, `"false"`→false (authoritative, wins over a set PROM_ENDPOINT), unset/other → `PROM_ENDPOINT != ""` fallback. Wired into the Dependencies literal.
- Tests: `config_test.go` (exact-body contract), `main_test.go` (env table incl. explicit-false-wins + unset/unrecognized fallback; `TestConfigRouteRegistered` asserts GET /api/v1/config appears exactly once in the walked route table and the boolean flows end-to-end). `TestZeroMutationRoutes` passes with the new route.

**UI (Task 2):**
- `TelemetryDisabledBanner.tsx` — purely presentational; copy verbatim from the 38-UI-SPEC Copywriting Contract; `data-testid="telemetry-disabled-banner"`, `data-state`, `role="status"`; warning border token for disabled-by-config, subtle for no-data; zero interactive elements.
- `TelemetryView.tsx` — one-shot `/api/v1/config` fetch on mount (`telemetryEnabled: boolean | null`, null = unknown/failed); derivation: any panel with points → hidden (T-38-15); `telemetryEnabled === false` OR all panels `unavailable` → disabled-by-config; `telemetryEnabled === true` AND all panels `kind:"data"` with zero points → no-data; loading/unreachable → hidden (per-panel notice owns connectivity). Banner renders as first child of the `telemetry-view` container. PanelState machine, ChartPanel, and TelemetryUnavailableNotice untouched.

## Verification Results

- `go test ./cmd/dashboard/...` — all packages `ok` (host go1.26.4), exit 0
- `npx vitest run TelemetryDisabledBanner TelemetryView` — 35/35 pass (host node 22.22.3)
- Full web suite `npx vitest run` — 282/282 pass across 33 files
- `npm run lint` (tsc -b) — clean
- `gofmt -l` / `go vet ./cmd/dashboard/...` — clean
- `grep -c '"/api/v1/config"' cmd/dashboard/router.go` == 1; `package.json` untouched (no new npm dependency)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Restored npm dependencies in fresh worktree**
- **Found during:** Task 2 RED verification
- **Issue:** `node_modules` absent in the parallel worktree (gitignored); vitest could not resolve
- **Fix:** `npm ci` from the existing lockfile — no new packages added
- **Files modified:** none (node_modules only)

**2. [Rule 1 - Bug-adjacent test adjustment] Scope-query test filtered to query_range calls**
- **Found during:** Task 2 GREEN
- **Issue:** The pre-existing D-02/D-04 test asserted every fetch URL contains `project="p1"`; the new one-shot `/api/v1/config` fetch carries no query param by design, failing the blanket assertion
- **Fix:** Filtered the asserted call list to `query_range` URLs (with a non-empty guard) — the test's intent (every panel query filters by project) is unchanged
- **Files modified:** `dashboard/web/src/components/__tests__/TelemetryView.test.tsx`
- **Commit:** `4efa08e`

**3. Route-literal acceptance criterion met via composed-path comment**
- **Found during:** Task 1
- **Issue:** Acceptance grep expects the literal `"/api/v1/config"` in router.go, but chi convention registers `r.Get("/config", …)` inside the `/api/v1` Route block (registering the full path at top level would conflict with the mount)
- **Fix:** Registration follows the block convention; the composed path is documented in an adjacent comment, and the real invariant (registered exactly once, GET) is locked by `TestConfigRouteRegistered` walking the chi route table

**4. Env test placed in new `cmd/dashboard/main_test.go`**
- Plan's `files_modified` did not list it, but the plan action explicitly directs creating a small main-package test file when `main_test.go` does not exist (telemetryEnabledFromEnv is package-private to `main`)

## TDD Gate Compliance

Both tasks ran RED → GREEN with committed gates: `d57209a`→`60b185f` (Task 1), `3237934`→`4efa08e` (Task 2). RED runs were observed failing (Go compile failure on undefined symbols; vitest 3 failed positive assertions) before implementation. No refactor commits needed.

## Known Stubs

None — the banner is wired to the live `/api/v1/config` endpoint and the existing panel states; no placeholder data paths.

## Threat Model Outcomes

- T-38-13 (mitigate): GET-only handler; `TestZeroMutationRoutes` passes with the new route — verified
- T-38-15 (mitigate): any-panel-data suppression implemented ahead of the disabled-by-config check; legacy env fallback in `telemetryEnabledFromEnv` — both test-covered
- No new security surface beyond the plan's threat model (the endpoint exposes a single boolean already inferable from the unavailable sentinel, T-38-14 accepted)

## Self-Check: PASSED

All created files exist on disk; all four task commits present; key artifact/link patterns (`telemetryEnabled`, `TelemetryDisabledBanner`, `PROMETHEUS_ENABLED`) verified by grep.
