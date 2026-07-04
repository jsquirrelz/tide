---
phase: 39-pre-flight-tech-debt-hardening
plan: "01"
subsystem: config/helm
tags: [preflight, oom-safety, helm, configmap, concurrency]
requirements: [PREFLIGHT-01]

dependency_graph:
  requires: []
  provides:
    - plannerConcurrency configmap default 4 (chart + source script)
    - pure-Go config default assertion (PREFLIGHT-01)
    - helm-template render contract test (PREFLIGHT-01)
  affects:
    - charts/tide/templates/configmap.yaml
    - hack/helm/augment-tide-chart.sh
    - internal/config/config_default_test.go
    - test/integration/kind/configmap_planner_concurrency_test.go

tech_stack:
  added: []
  patterns:
    - helm-template render contract test in test/integration/kind/ (plain go-test, not Ginkgo)
    - pure-Go config.Load assertion in internal/config/ package

key_files:
  modified:
    - charts/tide/templates/configmap.yaml
    - hack/helm/augment-tide-chart.sh
  created:
    - internal/config/config_default_test.go
    - test/integration/kind/configmap_planner_concurrency_test.go

decisions:
  - "Changed hack/helm/augment-tide-chart.sh (source) and regenerated charts/tide/templates/configmap.yaml — the chart-reproducibility pre-commit hook regenerates charts/ from the script, so the script is the authoritative source"

metrics:
  duration: ~10m
  completed: "2026-06-29"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 4
---

# Phase 39 Plan 01: plannerConcurrency Default Correction (PREFLIGHT-01) Summary

**One-liner:** Corrected Helm configmap fallback from `default 16` to `default 4` in both the source script and generated chart, pinned by a pure-Go config resolution test and a helm-template render contract test.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Fix configmap plannerConcurrency Helm fallback 16 → 4 | 5da4df6 | hack/helm/augment-tide-chart.sh, charts/tide/templates/configmap.yaml |
| 2 | Pure-Go config default assertion (PREFLIGHT-01) | 6b4c3ef | internal/config/config_default_test.go |
| 3 | Kind helm-template render contract test | 6f20c29 | test/integration/kind/configmap_planner_concurrency_test.go |

## Verification Results

- `helm template tide charts/tide | grep plannerConcurrency` → `plannerConcurrency: 4` (PASS)
- `grep -E 'plannerConcurrency:.*default 4' charts/tide/templates/configmap.yaml` → match (PASS)
- `grep -E 'plannerConcurrency:.*default 16' charts/tide/templates/configmap.yaml` → no match (PASS)
- `go test ./internal/config/ -run TestDefault -count=1` → PASS (3 tests)
- `go test ./test/integration/kind/ -run TestConfigMapPlannerConcurrency -count=1` → exit 0 (PASS)
- `git diff --stat charts/tide/values.yaml` → no changes (PASS)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Edited source script, not just generated chart**

- **Found during:** Task 1 commit
- **Issue:** The pre-commit `chart-reproducibility` hook runs `make helm`, which regenerates `charts/tide/templates/configmap.yaml` from `hack/helm/augment-tide-chart.sh`. Editing the generated chart file directly caused the hook to fail and revert the change.
- **Fix:** Applied the `| default 16 → | default 4` change in `hack/helm/augment-tide-chart.sh` (the actual source), then ran `make helm` to regenerate the chart. Both files staged together.
- **Files modified:** hack/helm/augment-tide-chart.sh, charts/tide/templates/configmap.yaml
- **Commit:** 5da4df6

### Plan Note: `default 16` still present in configmap (task reconciler)

The plan acceptance criterion states `grep 'default 16' charts/tide/templates/configmap.yaml` returns no matches. This is an overly broad check — `maxConcurrentReconciles.task: | default 16` is intentional (task reconciler concurrency defaults to 16, unchanged by this plan). The specific `plannerConcurrency` line no longer carries `default 16`; the task-reconciler line is correct and untouched. The targeted checks all pass.

## Known Stubs

None.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes introduced.

## Self-Check: PASSED

- [x] hack/helm/augment-tide-chart.sh exists and contains `default 4`
- [x] charts/tide/templates/configmap.yaml exists and contains `default 4`
- [x] internal/config/config_default_test.go exists
- [x] test/integration/kind/configmap_planner_concurrency_test.go exists
- [x] Commits 5da4df6, 6b4c3ef, 6f20c29 exist in git log
