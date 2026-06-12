---
phase: 16-telemetry-completion
plan: "07"
subsystem: controller-metrics
tags: [metrics, prometheus, task-controller, plan-controller, telemetry, cr-02, wr-04]
dependency_graph:
  requires: ["16-02"]
  provides: ["CR-02-task-counters", "CR-02-wave-counter", "WR-04-histogram-guard"]
  affects: ["internal/controller/task_controller.go", "internal/controller/plan_controller.go"]
tech_stack:
  added: []
  patterns:
    - metricFailureReason bounded enum mapping (T-16-21 cardinality guard)
    - signed-duration guard (d>=0) before Observe (WR-04 histogram protection)
    - label-resolve-once before loop in materializeWaves (sentinel pattern)
key_files:
  created:
    - internal/controller/plan_controller_metrics_test.go
  modified:
    - internal/controller/task_controller.go
    - internal/controller/task_controller_metrics_test.go
decisions:
  - "metricFailureReason does not reuse conditionReasonFromEnvelopeResult — that function produces capitalised/unbounded strings (Pitfall 17 cardinality risk); the bounded enum is implemented from scratch"
  - "EnvelopeReadFailed branch deliberately excluded from emitTaskMetrics — parity with D-12 budget-rollup commit points (seams 1+2 only, per CR-02 scope)"
  - "WavesDispatchedTotal Inc() placed in else branch of Create err check — only fires on nil err, not on IsAlreadyExists (watch-lag race) and not on the Get-existing branch (replay)"
  - "Label resolution for materializeWaves is non-fatal — on resolveProjectName error use 'unknown' so wave materialization never fails due to metrics"
metrics:
  duration: "7 minutes"
  completed_date: "2026-06-12"
  tasks_completed: 2
  tasks_total: 2
  files_changed: 3
  files_created: 1
---

# Phase 16 Plan 07: Controller Metrics Emission (CR-02 + WR-04) Summary

Wired the three registered-but-never-emitted metric counter families into production call sites, and patched the histogram negative-duration vulnerability.

## What Was Built

### Task 1: TasksCompletedTotal/TasksFailedTotal emission + negative-duration guard

**`internal/controller/task_controller.go`:**

1. Added `metricFailureReason(envelopeResult string, exitCode int) string` — maps envelope result/exitCode onto the bounded 6-value enum `{exit-1, gitleaks, lease, auth, internal, budget}`. `"cap-hit"` → `"budget"`, `"output-paths-violation"` → `"internal"`, any other with `exitCode != 0` → `"exit-1"`, default → `"internal"`. Envelope free-text never reaches a label value (T-16-21 cardinality bomb mitigation).

2. Extended `emitTaskMetrics` with a trailing `failureReason string` parameter. After the six TELEM-03 metrics: `failureReason == ""` increments `TasksCompletedTotal.WithLabelValues(project, phase, plan)` (3-label, no wave per registry.go); non-empty increments `TasksFailedTotal.WithLabelValues(project, phase, plan, failureReason)` (4-label).

3. Updated the three call sites:
   - Seam 1 (`:864`, OutputValidationError): passes `"internal"`
   - Seam 2 (`:896`, OutputPathsViolation): passes `"internal"`
   - Seam 3 (`:952`, standard terminal): computes `metricFailureReason(out.Result, out.ExitCode)` on the Failed arm, `""` on Succeeded

4. WR-04: replaced unguarded `Observe(duration.Seconds())` with signed-duration guard — computes `d := completedAt.Sub(task.Status.StartedAt.Time)`; only calls `Observe(d.Seconds())` when `d >= 0`; on negative logs at `V(1)` with task name and both timestamps as stale-envelope/clock-skew signal.

5. Added deliberate exclusion comment at EnvelopeReadFailed branch: no `emitTaskMetrics` call, maintaining strict parity with D-12 budget-rollup commit points.

**`internal/controller/task_controller_metrics_test.go`:**

- Updated existing `TestEmitTaskMetrics_EndToEnd` and `TestEmitTaskMetrics_PhaseMissSentinel` to pass `""` as failureReason (Succeeded semantics); added assertions for `TasksCompletedTotal` increment and `TasksFailedTotal` non-increment.
- `TestEmitTaskMetrics_FailedReason`: verifies `failureReason="budget"` increments `TasksFailedTotal[budget]` by 1 and does not touch `TasksCompletedTotal`. Unique labels `proj-fr1`.
- `TestMetricFailureReason`: table test covering all 6 `metricFailureReason` cases.
- `TestEmitTaskMetrics_NegativeDuration_WR04`: `completedAt = now - 5m`, `startedAt = now` — counters still fire, no panic, histogram guard skips observation.

### Task 2: WavesDispatchedTotal emission at materializeWaves

**`internal/controller/plan_controller.go`:**

1. Added `tidemetrics "github.com/jsquirrelz/tide/internal/metrics"` import (matching task_controller.go alias).

2. In `materializeWaves`, before the layer loop: resolves `projectName` via `r.resolveProjectName(ctx, plan)` (falls back to `"unknown"` on error — non-fatal); resolves `phaseName` from `plan.Spec.PhaseRef` (falls back to `"unknown"` when empty). Never emits empty label values (Metric Label Sentinel, Pitfall 4).

3. `tidemetrics.WavesDispatchedTotal.WithLabelValues(projectName, phaseName, plan.Name).Inc()` placed in the `else` branch of the `r.Create` error check — fires ONLY when Create returns `nil`. Not emitted on `IsAlreadyExists` (watch-lag race where a prior reconcile already counted it) and not emitted on the Get-existing branch (replay). Mirrors D-12 exactly-once shape.

4. `wave_controller.go` untouched — observational only per D-B2/D-B4.

**`internal/controller/plan_controller_metrics_test.go`** (new file):

- `TestMaterializeWaves_CreateOnce`: 2 layers, no Waves in fake client → creates 2, counter == 2.
- `TestMaterializeWaves_IdempotentReplay`: second call with same inputs (Waves now exist) → counter stays at 2, no double count.
- `TestMaterializeWaves_UnknownSentinel`: empty PhaseRef + no project label → counter increments with `{project=unknown, phase=unknown, plan=plan-mw3}`.

## Deviations from Plan

None — plan executed exactly as written. All three tasks (seams 1, 2, 3 plus the deliberate EnvelopeReadFailed exclusion) and WR-04 guard implemented as specified.

## Known Stubs

None. All metric emission is wired to live production call sites.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. All changes are pure in-process metric counter increments within existing controller methods.

## Verification Results

```
go build ./internal/... ./pkg/... ./api/...   PASS
go vet ./internal/controller/...              PASS
go test -run 'TestEmitTaskMetrics|TestMetricFailureReason|TestResolveWave|TestMaterializeWaves' -count=1  PASS (10/10 tests)
go test ./internal/metrics/... -count=1       PASS
git diff HEAD -- internal/metrics/registry.go EMPTY (locked)
grep -c 'TasksCompletedTotal.WithLabelValues' internal/controller/task_controller.go   → 1
grep -c 'TasksFailedTotal.WithLabelValues' internal/controller/task_controller.go       → 1
grep -c 'WavesDispatchedTotal.WithLabelValues' internal/controller/plan_controller.go  → 1
grep -c 'WavesDispatchedTotal' internal/controller/wave_controller.go                  → 0
```

## Task Commits

| Task | Commit | Description |
|------|--------|-------------|
| 1: TasksCompleted/Failed + WR-04 | `bbc8ead` | feat(16-07): emit TasksCompletedTotal/TasksFailedTotal + guard negative durations |
| 2: WavesDispatched at materializeWaves | `3299aeb` | feat(16-07): emit WavesDispatchedTotal at materializeWaves Create commit point |

## Self-Check: PASSED

All files exist, both task commits verified in git log, all tests green, build and vet clean.
