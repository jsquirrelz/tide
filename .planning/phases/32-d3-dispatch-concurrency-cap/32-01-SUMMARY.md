---
phase: 32-d3-dispatch-concurrency-cap
plan: "01"
subsystem: controller
tags: [concurrency-cap, planner, dispatch, pool, config, chart]

dependency_graph:
  requires: []
  provides:
    - plannerInFlightCount helper (internal/controller/dispatch_helpers.go)
    - Pool.Capacity() accessor (internal/pool/pool.go)
    - D3 cap gate at all four planner dispatch sites
    - plannerConcurrency default=4 in config.go + values.yaml
  affects:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/pool/pool.go
    - internal/config/config.go
    - charts/tide/values.yaml
    - hack/helm/tide-values.yaml

tech_stack:
  added: []
  patterns:
    - live client.List in-flight count gate before PlannerPool.Acquire (D3 Option B)
    - D-03 ordering invariant — gate BEFORE acquire, no slot leak
    - isJobTerminal reuse from task_controller.go (no third copy introduced)
    - TDD RED/GREEN/REFACTOR per task

key_files:
  created:
    - internal/controller/dispatch_concurrency_cap_test.go
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/dispatch_helpers_test.go
    - internal/pool/pool.go
    - internal/pool/pool_test.go
    - internal/config/config.go
    - internal/config/config_test.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - charts/tide/values.yaml
    - hack/helm/tide-values.yaml

decisions:
  - D-01 Option B: live client.List count gate (not semaphore-only); semaphore retained for thundering-herd
  - D-03 ordering invariant: gate returns RequeueAfter BEFORE PlannerPool.Acquire
  - RQ-2 resolved: default 4 (single-node-safe, ~500 MiB/pod, leaves headroom on 7.65 GiB node)
  - RQ-3: global cap (one list, all planner levels), namespace-aware for scoped installs
  - RQ-4: log-line only for v1.0.6 (OBS-01 Prometheus metric deferred per REQUIREMENTS.md)
  - FIXED contract: hack/helm/tide-values.yaml edited first, augment-tide-chart.sh propagates to charts/tide/

metrics:
  duration: "~40 minutes"
  completed_date: "2026-06-28"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 12
---

# Phase 32 Plan 01: D3 Dispatch Concurrency Cap Summary

**One-liner:** Live `client.List` in-flight planner Job count gate wired before `PlannerPool.Acquire` at all four dispatch sites; default `plannerConcurrency` lowered from 16 to single-node-safe 4 in both `config.go` and `values.yaml`.

## What Was Built

### Task 1: plannerInFlightCount helper + Pool.Capacity()

Added `plannerInFlightCount(ctx, client, watchNamespace)` to `internal/controller/dispatch_helpers.go`. The helper lists `batchv1.Job` objects filtered by `tideproject.k8s/role=planner` and returns the count of non-terminal jobs (those where `!isJobTerminal(job)` — reuses the existing helper from `task_controller.go:1706`). Namespace-aware: empty `watchNamespace` counts all namespaces (cluster-scoped install posture); non-empty restricts to the watched namespace.

Added `func (p *Pool) Capacity() int { return cap(p.sem) }` to `internal/pool/pool.go`. Exposes the configured cap for the gate without adding a `PlannerConcurrency int` field to all four reconciler structs.

**Tests (TDD RED→GREEN):**
- `TestPlannerInFlightCount`: 5 table cases — 3 non-terminal returns 3; 2 non-terminal + 1 terminal returns 2; 0 jobs returns 0; namespace-scoped counts only watched namespace; empty watchNamespace counts all
- `TestPoolCapacity`: asserts two pool sizes return correct cap

### Task 2: D3 cap gate at all four dispatch sites

Inserted the gate block at each of the four planner dispatch sites, **before** `PlannerPool.Acquire` (D-03 ordering invariant):

```go
if r.PlannerPool != nil {
    inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
    if err != nil {
        return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err)
    }
    if inFlight >= r.PlannerPool.Capacity() {
        logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
            "inFlight", inFlight, "cap", r.PlannerPool.Capacity(), "<level>", <obj>.Name)
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
    }
}
```

The `plan_controller.go` site returns `(ctrl.Result{RequeueAfter: 10 * time.Second}, true, nil)` to match its `(ctrl.Result, bool, error)` helper signature.

**RequeueAfter = 10s**: longer than import-hold's 5s, shorter than billing-halt's 30s (RESEARCH Pitfall 4).

**Tests (TDD RED→GREEN):** `TestConcurrencyCapGate_MilestoneDispatchParks`, `TestConcurrencyCapGate_RequeueAfterIs10s`, `TestGatePrecedesAcquire_SlotNotConsumed` — all verify cap=1 + 1 in-flight Job parks with RequeueAfter=10s, nil, and the pool semaphore remains unacquired.

### Task 3: Default plannerConcurrency 16→4

Changed the default literal in `internal/config/config.go` (`resolveField("plannerConcurrency", ..., 4, ...)`).

**FIXED contract**: the canonical source is `hack/helm/tide-values.yaml` (not `charts/tide/values.yaml` directly). Updated `hack/helm/tide-values.yaml` first with `plannerConcurrency: 4` and a multi-line sizing comment, then ran `bash hack/helm/augment-tide-chart.sh` to propagate to `charts/tide/values.yaml`. The `chart-reproducibility` pre-commit hook validates that `make helm` produces an identical result — confirming the change is canonical.

**Tests:** `TestDefaultPlannerConcurrency` + fixed `TestConfigLoad_DefaultsApplied` (previously hardcoded 16).

## Commits

| Hash | Type | Description |
|------|------|-------------|
| `8c3fb31` | test | Add failing tests for plannerInFlightCount + Pool.Capacity() |
| `068ecba` | feat | Add plannerInFlightCount helper + Pool.Capacity() |
| `28686a2` | test | Add failing concurrency cap gate tests for milestone dispatch |
| `0bf2880` | feat | Wire D3 cap gate at all four planner dispatch sites |
| `ca2b0b0` | test | Add failing TestDefaultPlannerConcurrency asserting default=4 |
| `e00b99f` | feat | Lower default plannerConcurrency from 16 to 4 (config + chart) |

## Verification

- `go test ./internal/controller/... -run TestPlannerInFlightCount -count=1` — PASSED
- `go test ./internal/controller/... -run Concurrency -count=1` — PASSED
- `go test ./internal/config/... -run TestDefaultPlannerConcurrency -count=1` — PASSED
- `go test ./internal/pool/... -count=1` — PASSED (all pool tests including TestPoolCapacity)
- `go test ./internal/config/... -count=1` — PASSED (all config tests)
- `make lint` — exit 0, 0 issues (crosspool analyzer: planner/executor pools remain separate — CONCUR-03)
- `grep -c 'plannerConcurrency: 16' charts/tide/values.yaml` — returns 0 (CONCUR-02)
- Line ordering: `plannerInFlightCount` call precedes `PlannerPool.Acquire` in all four controller files (CONCUR-01 / D-03)

## Success Criteria

- [x] CONCUR-01: in-flight planner Jobs bounded by live count gate at all four sites
- [x] CONCUR-02: default is 4 in config.go + values.yaml; 16 removed from both
- [x] CONCUR-03: executor pool untouched; make lint crosspool analyzer green
- [x] CONCUR-04: deferred dispatch logs V(1) line + returns RequeueAfter(10s); chart documents sizing constraint

## Deviations from Plan

**1. [Rule 2 - Auto-fix] Updated canonical values.yaml source**

- **Found during:** Task 3 commit
- **Issue:** The `chart-reproducibility` pre-commit hook regenerates `charts/tide/values.yaml` from `hack/helm/tide-values.yaml` via `bash hack/helm/augment-tide-chart.sh`. Editing `charts/tide/values.yaml` directly is overwritten by `make helm`. The correct edit target is `hack/helm/tide-values.yaml`.
- **Fix:** Edited `hack/helm/tide-values.yaml` first (the canonical source), then ran `augment-tide-chart.sh` to propagate. Both files staged together.
- **Files modified:** `hack/helm/tide-values.yaml`, `charts/tide/values.yaml`

**2. [Rule 1 - Bug] Fixed TestConfigLoad_DefaultsApplied hardcoded 16**

- **Found during:** Task 3 GREEN
- **Issue:** The existing `TestConfigLoad_DefaultsApplied` test hardcoded `cfg.PlannerConcurrency != 16`, which would fail after the default change.
- **Fix:** Updated assertion to `!= 4` with a comment explaining the CONCUR-02 change.
- **Files modified:** `internal/config/config_test.go`
- **Commit:** included in `e00b99f`

## TDD Gate Compliance

All three tasks followed RED→GREEN:
- Task 1 RED: `test(32-01)` commit `8c3fb31` (undefined: plannerInFlightCount + Capacity build failures)
- Task 1 GREEN: `feat(32-01)` commit `068ecba`
- Task 2 RED: `test(32-01)` commit `28686a2` (tests fail with "credproxy: signingKey must not be empty" — gate not yet wired)
- Task 2 GREEN: `feat(32-01)` commit `0bf2880`
- Task 3 RED: `test(32-01)` commit `ca2b0b0` (TestDefaultPlannerConcurrency fails: got 16, want 4)
- Task 3 GREEN: `feat(32-01)` commit `e00b99f`

## Known Stubs

None — all helpers are fully wired with real logic. No placeholder data or TODO paths in production code.

## Threat Flags

No new security-relevant surface introduced beyond what the plan's threat model documents (T-32-01, T-32-02, T-32-03). The `plannerInFlightCount` helper reads the informer cache (not the API server), adds no new endpoints, no auth paths, no file access.

## Self-Check

Files exist:
- `internal/controller/dispatch_helpers.go` — plannerInFlightCount present
- `internal/pool/pool.go` — Pool.Capacity() present
- `internal/controller/dispatch_concurrency_cap_test.go` — cap-gate tests present
- `hack/helm/tide-values.yaml` — plannerConcurrency: 4 present
- `charts/tide/values.yaml` — plannerConcurrency: 4 present

Commits exist:
- `8c3fb31`, `068ecba`, `28686a2`, `0bf2880`, `ca2b0b0`, `e00b99f` — all confirmed in git log
