---
phase: 32-d3-dispatch-concurrency-cap
reviewed: 2026-06-28T00:00:00Z
depth: standard
files_reviewed: 9
files_reviewed_list:
  - internal/controller/dispatch_helpers.go
  - internal/pool/pool.go
  - internal/config/config.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/dispatch_concurrency_cap_test.go
  - internal/controller/adoption_lifecycle_test.go
findings:
  critical: 0
  warning: 3
  info: 3
  total: 6
status: issues_found
---

# Phase 32: Code Review Report

**Reviewed:** 2026-06-28
**Depth:** standard
**Files Reviewed:** 9
**Status:** issues_found

## Summary

Reviewed the D3 in-flight concurrency cap (Option B) plus the carried-in WR-01/02/03 hardening. The core mechanism is sound and the adversarial focus areas hold up:

- **Gate placement (all four sites):** The `plannerInFlightCount` gate is placed BEFORE `PlannerPool.Acquire` at every dispatch site — milestone (`milestone_controller.go:384` gate, `:397` Acquire), phase (`phase_controller.go:382` / `:395`), plan (`plan_controller.go:388` / `:401`), and project (`project_controller.go:1184` / `:1197`). No slot leak. `TestGatePrecedesAcquire_SlotNotConsumed` proves the ordering invariant against a real pool.
- **Non-terminal counting:** `plannerInFlightCount` correctly counts via `!isJobTerminal(&job)` (JobComplete/JobFailed conditions), NOT `Status.Active` — so pending/uncreated-pod Jobs still count. `isJobTerminal` (task_controller.go:1706) is correct.
- **Selector scope:** `MatchingLabels{"tideproject.k8s/role": "planner"}` matches only planner Jobs. Verified against `jobspec.go:217/231` (executor="executor"), reporter (`reporter_jobspec.go`="reporter"), and import (`importJobRoleLabel`) — no cross-pool bleed. CONCUR-03 (no executor/task identifier in the planner gate) holds.
- **Return shape:** Cap-reached returns `RequeueAfter: 10s, nil` (not an error); List errors are wrapped as `fmt.Errorf("planner in-flight count: %w", err)`. Matches CONCUR-04.
- **WR-01 suppression patch:** switched to `MergeFromWithOptimisticLock` (project_controller.go:1157) so the "conflict is retryable" comment is now true; the conflict is surfaced as an error to requeue (`:1169`). Correct.
- **WR-02/03 marker stamps:** milestone/phase/plan re-fetch + `RetryOnConflict` + `MergeFromWithOptimisticLock` + idempotent early-return + requeue-on-exhaustion. `RollUpUsage` still runs BEFORE the marker stamp at all three sites — no ordering regression.

No BLOCKER-class defects found. Three WARNINGs and three INFO items below.

## Warnings

### WR-01: Cap gate counts stuck planner Jobs that outlived the cache's view of their terminal state, but the durable-marker stamp does not protect against the same cache lag for the cap

**File:** `internal/controller/dispatch_helpers.go:304-322`
**Issue:** `plannerInFlightCount` lists via the **cached** client (`r.Client`). The whole cap correctness depends on a planner Job eventually reaching a terminal condition so the count drains. Planner Jobs are created via `podjob.BuildJobSpec(JobKindPlanner)` with `DefaultCaps` bounding `ActiveDeadlineSeconds`, so a hung pod becomes `JobFailed` and the count frees — good. But there is one path where a non-terminal Job can persist indefinitely in the cache view while the cap is held: if a planner Job's owner CRD is deleted but the Job's `ownerRef.BlockOwnerDeletion`/foreground GC stalls (the same envtest-GC-never-runs class documented for `deleteFailedPushJob` at `project_controller.go:886-894`), the Job lingers non-terminal and permanently consumes one cap slot cluster-wide. This is a real-but-narrow stall risk, not a data-loss bug. The cap is global across all projects (per `values.yaml:79-87`), so a single wedged Job in one namespace throttles dispatch in every other namespace.
**Fix:** Consider excluding Jobs whose `DeletionTimestamp` is set from the in-flight count (a Job mid-deletion is no longer competing for the budget):
```go
for i := range jobs.Items {
    if !jobs.Items[i].DeletionTimestamp.IsZero() {
        continue // mid-deletion — not competing for a slot
    }
    if !isJobTerminal(&jobs.Items[i]) {
        n++
    }
}
```
At minimum, document the wedged-Job-throttles-all-namespaces blast radius in the `plannerInFlightCount` doc comment so operators know to look here when dispatch stalls cluster-wide.

### WR-02: Stale doc comment claims planner pool "default size 16" — the config default is now 4

**File:** `internal/controller/milestone_controller.go:68-69`
**Issue:** The `PlannerPool` field comment reads "(Phase 1 POOL-01, default size 16)". This phase changed the `plannerConcurrency` default from 16 to 4 in `config.go:117` to match the Helm chart contract (`values.yaml:88` = 4). The comment is now wrong and will mislead anyone reasoning about the cap budget — exactly the value this phase makes load-bearing. The same "size 16" framing does not appear on the phase/plan/project reconcilers (they say "up-stack reconciler acquires plannerPool only"), so this is a single drifted comment.
**Fix:** Update to reflect the chart-driven default:
```go
// PlannerPool is the planner-pool semaphore (Phase 1 POOL-01, default
// capacity 4 per charts/tide/values.yaml plannerConcurrency).
```

### WR-03: Cap default (4) is narrower than the documented minimum-safe planning-wave width, risking unnecessary dispatch serialization

**File:** `internal/config/config.go:117` (default 4) vs `charts/tide/values.yaml:84-87`
**Issue:** `values.yaml` itself states the cap "must be sized at least as wide as the widest expected planning wave (e.g. a 6-phase milestone needs plannerConcurrency >= 6 to avoid serialising phase dispatch)," yet ships a default of 4. With the gate being `inFlight >= Capacity()` counted **globally** across project+milestone+phase+plan levels, a single milestone fanning out to 5+ phases will park phase planners behind the cap and serialize them at 10s/retry. This does not deadlock (single-shot planner Jobs terminate and free slots, and `RequeueAfter` keeps retrying — no dropped work), so it is a throughput/quality concern, not a correctness BLOCKER. But the default contradicts the file's own stated sizing rule.
**Fix:** Either raise the default to satisfy the documented `>= widest-wave` rule, or soften the values.yaml comment to acknowledge that 4 is a deliberately conservative single-node-memory floor that intentionally serializes wide waves. Keep code and chart consistent — they currently agree on 4 but the prose argues for 6.

## Info

### IN-01: Test reimplements `strings.Contains` instead of importing it

**File:** `internal/controller/dispatch_helpers_test.go:387-399`
**Issue:** `contains` / `containsHelper` hand-roll substring search "to avoid importing strings." This is dead-weight complexity in a test file; `strings.Contains` is stdlib and already transitively available. The hand-rolled `contains` also has an odd `len(s) > 0 &&` clause that is redundant given the preceding `len(s) >= len(sub)` guard.
**Fix:** Replace both helpers with `strings.Contains(string(envBytes), "\"sharedContext\"")` and drop the `import` aversion note.

### IN-02: `Pool.Capacity()` doc omits the zero-capacity edge that config validation actually guarantees away

**File:** `internal/pool/pool.go:80-86`
**Issue:** `Capacity()` returns `cap(p.sem)`. If a Pool were ever constructed with capacity 0, the gate `inFlight >= 0` would be true immediately and dispatch would park forever. This cannot happen today because `config.go:resolveField` rejects any concurrency `< 1` and the gate is guarded by `if r.PlannerPool != nil`, but the invariant ("capacity is always >= 1, enforced upstream in config.Load") is load-bearing for the cap and is documented nowhere near `Capacity()`.
**Fix:** Add one line to the `Capacity()` doc noting the >= 1 invariant is enforced by `config.Load` so a future caller constructing `pool.New(0, ...)` is warned the cap gate will hard-stall.

### IN-03: Cap-gate unit tests do not cover the List-error branch

**File:** `internal/controller/dispatch_concurrency_cap_test.go` (whole file) / `dispatch_helpers_test.go:438-508`
**Issue:** `TestPlannerInFlightCount` covers count/namespace behavior and the cap-gate tests cover the park path, but no test exercises `plannerInFlightCount` returning a non-nil error and the reconciler wrapping it as `fmt.Errorf("planner in-flight count: %w", err)` → requeue. The error-wrapping branch (a focus area: "List errors wrapped as errors") is asserted only by reading, not by a test. A fake client with an injected List error (`interceptor.Funcs{List: ...}`) would close this.
**Fix:** Add a case using `fake.NewClientBuilder().WithInterceptorFuncs(...)` to force a List error and assert the milestone dispatch returns a non-nil error (not a silent park).

---

_Reviewed: 2026-06-28_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
