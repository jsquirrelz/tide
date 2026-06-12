---
phase: 16-telemetry-completion
plan: "02"
subsystem: metrics/telemetry
tags: [metrics, prometheus, telem-03, cost-accounting, task-controller]
dependency_graph:
  requires: []
  provides: [TELEM-03-metrics, task-cost-emission]
  affects: [internal/metrics, internal/controller/task_controller.go]
tech_stack:
  added: []
  patterns: [CounterVec-with-4-labels, HistogramVec-with-custom-buckets, non-fatal-emit-after-rollup]
key_files:
  created:
    - internal/controller/task_controller_metrics_test.go
  modified:
    - internal/metrics/registry.go
    - internal/metrics/registry_test.go
    - internal/controller/task_controller.go
decisions:
  - "Phase resolution via PlanRef→Plan.Spec.PhaseRef (not a task label — tideproject.k8s/phase does not exist)"
  - "resolveWave reads in-memory OwnerReferences (no API call) — O(1), no cache miss risk"
  - "emitTaskMetrics is non-fatal: task already terminal at RollUpUsage sites"
metrics:
  duration: "~30 minutes"
  completed: "2026-06-12"
  tasks_completed: 2
  tasks_total: 2
---

# Phase 16 Plan 02: Six Locked TELEM-03 Metrics Registered and Emitted

## One-liner

Six Prometheus metrics for token/cost/duration telemetry registered with 4-label `{project,phase,plan,wave}` arity and emitted at all three `budget.RollUpUsage` terminal seams in `TaskReconciler`.

## Summary

TELEM-03 closed: the Cost Over Time, Token Breakdown, and related Grafana panels now have data to display. The six locked metrics (`tide_tokens_input_total`, `tide_tokens_output_total`, `tide_tokens_cache_read_total`, `tide_tokens_cache_creation_total`, `tide_cost_cents_total`, `tide_task_duration_seconds`) are registered in the single `metrics.Registry.MustRegister` call in `internal/metrics/registry.go` and emitted from `TaskReconciler.handleJobCompletion` immediately after each of the three `budget.RollUpUsage` sites — guaranteeing Prometheus cost totals can never diverge from Budget accounting (D-12).

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Register six locked TELEM-03 metrics | 0787e94 | internal/metrics/registry.go, registry_test.go |
| 2 | Emit metrics at three RollUpUsage terminal seams | 21d77ba | internal/controller/task_controller.go, task_controller_metrics_test.go |

## What Was Built

### Task 1: Metric Registration (`internal/metrics/registry.go`)

- Added `taskDurationBuckets = []float64{30, 60, 120, 300, 600, 1200, 1800, 3600, 7200}` beside `dispatchLatencyBuckets` (D-11 — Prometheus defaults top out at 10s, useless for agentic tasks).
- Declared six exported vars with correct types (five `*prometheus.CounterVec`, one `*prometheus.HistogramVec`).
- Constructed all six in `init()` with metric names matching the locked MILESTONE.md table (49e93cb) and label slice `[]string{"project", "phase", "plan", "wave"}` on all six.
- Extended the existing single `metrics.Registry.MustRegister(...)` call — no second call added (double-register panics).

### Task 1: Registry Tests (`internal/metrics/registry_test.go`)

- Seeded all six new families with `__seed__` sentinel (4 label values) so `TestRegistry_AllMetricFamiliesPresent` sees them.
- Extended expected-family list with all six locked names.
- Added per-metric arity tests (`WithLabelValues("p","ph","pl","w")`) for all six.
- Added `TestRegistry_TaskDurationBuckets` source check asserting the exact D-11 bucket slice.
- `TestRegistry_NoTaskLabel` green — no `"task"` quoted literal in registry.go (comment was reworded to avoid the literal).

### Task 2: Helpers in `task_controller.go`

**`resolveWave(task)`**: Iterates `task.GetOwnerReferences()`, returns first ref with `Kind == "Wave"` name. Returns `"unknown"` on miss. No API call — OwnerReferences are in-memory. O(N refs).

**`emitTaskMetrics(ctx, task, project, usage, completedAt)`**: Resolves four label values:
- `project` = `project.Name`
- `plan` = `task.Spec.PlanRef` (fallback `"unknown"` if empty)
- `phase` = fetched via `r.Get` of the Plan named by `PlanRef`, then `plan.Spec.PhaseRef` (PLANNER CORRECTION honored: `tideproject.k8s/phase` label does not exist in codebase — would silently emit `phase=""` on every series)
- `wave` = `resolveWave(task)`

Emits all five counters and histogram (duration only when `StartedAt != nil && !completedAt.IsZero()`). Returns `error` for non-fatal logging.

**Three emission sites**, immediately after each `budget.RollUpUsage` block:
- Output-validation-error path (seam 1)
- Output-paths-violation path (seam 2)
- Standard terminal path (seam 3)

### Task 2: Tests (`task_controller_metrics_test.go`)

- `TestResolveWave_WaveOwner`: Wave OwnerRef → returns Wave name.
- `TestResolveWave_NoWaveOwner`: No Wave owner → returns `"unknown"`.
- `TestEmitTaskMetrics_EndToEnd`: Fake client with Plan+Task+Project; asserts all six metric values via `testutil.ToFloat64` and `testutil.CollectAndCount`. Uses unique label set `"proj-m1"/"phase-m1"/"plan-m1"/"tide-wave-x-0"` to avoid cross-test interference.
- `TestEmitTaskMetrics_PhaseMissSentinel`: Missing Plan → `phase="unknown"` sentinel used; counter lands at expected value.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] NoTaskLabel test tripped by comment containing `"task"` literal**

- **Found during:** Task 1 first test run
- **Issue:** The `TestRegistry_NoTaskLabel` test does a raw `strings.Contains` for `"task"` (quoted) anywhere in registry.go. The added comment `"only the literal \"task\" is forbidden"` contained the exact byte sequence `"task"` and failed the test.
- **Fix:** Rewrote comment to `"the task label is forbidden"` (no quoted string literal). No behavior change.
- **Files modified:** `internal/metrics/registry.go`
- **Commit:** 0787e94 (same task commit — fixed before commit)

## Known Stubs

None. All six metrics are wired end-to-end: registered in init(), emitted at all three terminal seams, label values resolved from real CRD names.

## Threat Flags

No new threat surface beyond the T-16-05/T-16-06/T-16-07 items already in the plan's threat model. All label values are K8s resource names (CRD names), never envelope free-text or secrets.

## Verification

- `go build ./internal/... ./api/... ./cmd/manager/...` exits 0
- `go test ./internal/metrics/... -count=1` exits 0 (all 16 tests pass including NoTaskLabel)
- `go test ./internal/controller/... -run 'TestResolveWave|TestEmitTaskMetrics' -count=1` exits 0
- `go vet ./internal/...` exits 0
- Six metric names confirmed present in registry.go; single MustRegister call confirmed

## Self-Check

Files created/modified:
- internal/metrics/registry.go — FOUND (extended with 6 metrics, confirmed by tests)
- internal/metrics/registry_test.go — FOUND (extended with new tests)
- internal/controller/task_controller.go — FOUND (resolveWave + emitTaskMetrics + 3 call sites)
- internal/controller/task_controller_metrics_test.go — FOUND (new file, created)

Commits:
- 0787e94 (Task 1: metric registration) — verified
- 21d77ba (Task 2: emission at RollUpUsage seams) — verified
