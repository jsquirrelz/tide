---
phase: 16-telemetry-completion
plan: "08"
subsystem: config
tags: [config, prometheus, telemetry, docs, dead-code-removal]

# Dependency graph
requires:
  - phase: 16-01
    provides: baseline telemetry wiring verified

provides:
  - Dead prometheusEndpoint YAML config surface removed from internal/config
  - MILESTONE.md corrected to describe env-only mechanism (os.Getenv PROM_ENDPOINT)
  - dashboard-deployment.yaml template comment corrected to name cmd/dashboard/main.go as reader
  - WR-03 gap closed — zero dead config surfaces; every documented surface is consumed by a binary

affects: [16-telemetry-completion]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Env-only wiring for dashboard config (os.Getenv in cmd/dashboard/main.go) — not routed through internal/config"

key-files:
  created: []
  modified:
    - internal/config/config.go
    - internal/config/config_test.go
    - MILESTONE.md
    - charts/tide/templates/dashboard-deployment.yaml

key-decisions:
  - "Delete the dead prometheusEndpoint YAML key and PROM_ENDPOINT env-override block from internal/config (WR-03 option B — remove, not wire)"
  - "Confirm chart is FIXED contract; comment-only edit to dashboard-deployment.yaml; helm render proven byte-identical"

patterns-established:
  - "Dead config surfaces (YAML keys with no binary consumer) are removed, not left as documented-but-ignored fields"

requirements-completed: [TELEM-01]

# Metrics
duration: 12min
completed: 2026-06-12
---

# Phase 16 Plan 08: Dead prometheusEndpoint Config Removal Summary

**Dead `prometheusEndpoint` YAML key and `PROM_ENDPOINT` env-override excised from `internal/config`; docs aligned to the working env-only path (`os.Getenv` in `cmd/dashboard/main.go`)**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-06-12T21:00:00Z
- **Completed:** 2026-06-12T21:12:00Z
- **Tasks:** 1
- **Files modified:** 4

## Accomplishments

- Removed `Config.PrometheusEndpoint` field, `rawConfig.PrometheusEndpoint *string`, and the `Load()` env-override block from `internal/config/config.go` — no binary ever consumed this path
- Deleted the three `TestConfigLoad_PrometheusEndpoint_*` tests that were pinning dead behavior
- Corrected both MILESTONE.md sentences (line 67 and line 126) that falsely claimed `internal/config` gains a `prometheusEndpoint` YAML key — now correctly describes `cmd/dashboard/main.go` reading `PROM_ENDPOINT` via `os.Getenv`
- Updated the `{{- /* ... */}}` template comment in `dashboard-deployment.yaml` (non-rendering, comment-only) to name `cmd/dashboard/main.go` as the reader; helm render output proven byte-identical (only inherently-random `TIDE_SIGNING_KEY` differs across two sequential renders — pre-existing chart randomness, not caused by this change)
- `make helm-assert` passes all 4 permutations (RBAC, EC-7 default/endpoint/retentionTime/lint)
- `go test ./internal/config/... -count=1` green (6 remaining tests); `go build ./cmd/manager/... ./cmd/dashboard/...` clean

## Task Commits

1. **Task 1: Delete the dead PrometheusEndpoint config surface and align docs** - `8ce99f8` (fix)

## Files Created/Modified

- `internal/config/config.go` — `Config.PrometheusEndpoint` field removed; `rawConfig.PrometheusEndpoint *string` removed; doc comment sentence removed; `Load()` PROM_ENDPOINT override block removed
- `internal/config/config_test.go` — Three `TestConfigLoad_PrometheusEndpoint_*` tests deleted
- `MILESTONE.md` — Lines 67 and 126 corrected from `internal/config` YAML-key claim to env-only description
- `charts/tide/templates/dashboard-deployment.yaml` — Template comment updated (non-rendering; rendered output byte-identical)

## Decisions Made

- WR-03 option B confirmed: delete the dead surface rather than wiring the dashboard to load config. The chart is a FIXED contract (`values.yaml` and rendered Helm output must not change), and the dashboard pod has `readOnlyRootFilesystem: true` with no volume mounts — `config.Load("/etc/tide/config.yaml")` would error in the dashboard pod. The env-only path (`os.Getenv` in `cmd/dashboard/main.go`) was always the working mechanism.

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — this plan removes dead code; no stubs introduced.

## Threat Flags

None — this plan deletes a misleading config surface (T-16-24 mitigated: no binary can configure a no-op YAML key); no new trust boundaries introduced.

## Issues Encountered

- `go build ./...` fails on `cmd/tide-demo-init/main.go:112` with "pattern all:fixture: no matching files found" — confirmed pre-existing on the base commit (b72775d). The manager and dashboard binaries (`./cmd/manager/...`, `./cmd/dashboard/...`) and config package all build cleanly. This is out-of-scope for plan 16-08; deferred per scope-boundary rule.

## Next Phase Readiness

- WR-03 is closed: `grep -rn prometheusEndpoint --include='*.go'` returns 0 hits; every documented config surface is consumed by a binary
- TELEM-01 truth holds: `helm value → PROM_ENDPOINT env → dashboard proxy` is the single documented, tested, working path
- Phase 16 gap-closure plans can proceed; no blockers from this plan

---
*Phase: 16-telemetry-completion*
*Completed: 2026-06-12*

## Self-Check: PASSED

- `internal/config/config.go` — exists, `grep -c PrometheusEndpoint` returns 0
- `internal/config/config_test.go` — exists, `grep -c TestConfigLoad_PrometheusEndpoint` returns 0
- `MILESTONE.md` — `grep -cE 'YAML key .?prometheusEndpoint'` returns 0
- `charts/tide/templates/dashboard-deployment.yaml` — `grep -c 'Read by internal/config'` returns 0
- `charts/tide/values.yaml` — `git diff --stat` empty (FIXED contract preserved)
- Commit `8ce99f8` — task fix commit exists and verified
