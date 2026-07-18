---
phase: 43-task-level-parity-trace-context-propagation
reviewed: 2026-07-16T00:00:00Z
depth: standard
files_reviewed: 32
files_reviewed_list:
  - api/v1alpha3/milestone_types.go
  - api/v1alpha3/phase_types.go
  - api/v1alpha3/plan_types.go
  - api/v1alpha3/project_types.go
  - api/v1alpha3/task_types.go
  - charts/tide-crds/templates/milestone-crd.yaml
  - charts/tide-crds/templates/phase-crd.yaml
  - charts/tide-crds/templates/plan-crd.yaml
  - charts/tide-crds/templates/project-crd.yaml
  - charts/tide-crds/templates/task-crd.yaml
  - cmd/tide-reporter/main_test.go
  - cmd/tide-reporter/main.go
  - config/crd/bases/tideproject.k8s_milestones.yaml
  - config/crd/bases/tideproject.k8s_phases.yaml
  - config/crd/bases/tideproject.k8s_plans.yaml
  - config/crd/bases/tideproject.k8s_projects.yaml
  - config/crd/bases/tideproject.k8s_tasks.yaml
  - internal/controller/dispatch_helpers.go
  - internal/controller/dispatch_traceparent_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/reporter_jobspec_test.go
  - internal/controller/reporter_jobspec.go
  - internal/controller/span_emission_test.go
  - internal/controller/span_emission_unit_test.go
  - internal/controller/span_emission.go
  - internal/controller/task_controller.go
  - internal/controller/task_dispatch_traceparent_test.go
  - internal/dispatch/podjob/jobspec_test.go
  - internal/dispatch/podjob/jobspec.go
findings:
  critical: 1
  warning: 2
  info: 2
  total: 5
status: issues_found
---

# Phase 43: Code Review Report

**Reviewed:** 2026-07-16T00:00:00Z
**Depth:** standard
**Files Reviewed:** 32
**Status:** issues_found

## Summary

Phase 43 adds parenting-aware span synthesis (TRACE-02), durable per-level
`{Level}TraceSpanID` persistence (PROP-02), and W3C `TRACEPARENT`/`--traceparent`
propagation into dispatch Jobs and the reporter (PROP-01), and generalizes Phase
42's span synthesizer to the Task level (TRACE-01). The scope was reviewed
against the actual diff (`git diff f571918..HEAD`, the tip of Phase 42, `f571918`,
through the current `HEAD`) rather than re-reading the ~11.7k lines of
pre-existing controller code wholesale.

The mechanics are largely sound and well tested: the build is clean (`go build
./...`, `go vet ./...`), all new/updated unit tests pass, the five CRDs' new
status fields are byte-consistent between `charts/tide-crds/templates/*.yaml`
and `config/crd/bases/*.yaml`, and the parent/child span-ID wiring is internally
consistent across all five levels (Milestone→Project, Phase→Milestone,
Plan→Phase, Task→Plan, Project→root — no swapped-parent bugs found).

However, tracing through the new "mark-then-emit" gate at all five completion
handlers surfaced a genuine regression against the codebase's own documented
invariant: three of the five levels (Milestone, Phase, Plan) can durably stamp
the `{Level}SpanEmittedUID` marker for a Job attempt whose span will *never* be
emitted, because Phase 43 added two new silent-no-op conditions to
`synthesizePlannerSpan` (nil `project`, `TraceIDFromUID` error) without adding
them to the marker-stamp gate. Task's own `emitTaskSpanOnce` (added in this same
phase) demonstrates the correct fix already exists in the codebase, which is
strong evidence this is an oversight rather than a deliberate asymmetry. See
CR-01.

## Critical Issues

### CR-01: Marker-stamp gate doesn't account for nil-project/TraceID-error no-ops — permanent silent span loss for Milestone/Phase/Plan

**File:** `internal/controller/milestone_controller.go:574` (mirrored at `internal/controller/phase_controller.go:526` and `internal/controller/plan_controller.go:570`)

**Issue:**

`synthesizePlannerSpan`'s own doc comment (`internal/controller/span_emission.go:96-99`)
states the binding invariant this phase must preserve:

> Callers MUST gate the SpanEmittedUID marker stamp on this predicate
> [`plannerSpanResolvable`]: mark-then-emit ordering (42-REVIEW WR-01) stamps
> the marker BEFORE emitting, and a stamp without a subsequent emission would
> suppress the attempt's span forever.

Phase 43 added **two new ways** `synthesizePlannerSpan` can no-op and return
`(zero SpanID, false)` without emitting anything: a nil `project` (D-02/Pitfall
5, `span_emission.go:154-157`) and a `TraceIDFromUID` error
(`span_emission.go:158-162`). Neither condition was added to the three
planner-tier marker-stamp gates:

```go
// milestone_controller.go:574 (identical shape at phase_controller.go:526, plan_controller.go:570)
if completedJob != nil && ms.Status.MilestoneSpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(completedJob) {
    stamped := false
    if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
        ...
        latest.Status.MilestoneSpanEmittedUID = string(completedJob.UID)
        return r.Status().Patch(ctx, latest, markerPatch)
    }); mErr != nil {
        ...
    } else if stamped {
        var parentSpanID trace.SpanID
        if project != nil {
            parentSpanID = spanIDFromHexOrZero(project.Status.ProjectTraceSpanID)
        }
        thisSpanID, emitted := synthesizePlannerSpan(ctx, "milestone", project, ..., parentSpanID)
        if emitted { ... }
        // <-- if !emitted (nil project), the marker is ALREADY stamped above.
        //     This Job attempt's span is now permanently unrecoverable.
    }
}
```

The `if project != nil { parentSpanID = ... }` guard right next to the call
(`milestone_controller.go:602-605`) even carries a comment that concedes the
exact failure mode: *"guard project != nil to avoid a nil-pointer dereference
(a nil project also makes the synthesizer a no-op)"* — but the marker was
already durably stamped several lines earlier, unconditionally on `project`.

Since `project` is resolved via a **fresh, single-shot `r.Get`** inside each of
these three handlers (`milestone_controller.go:509-515`,
`phase_controller.go:472` via `resolveProject`,
`plan_controller.go:506` via `resolveProjectForPlan`'s up-to-4-hop chain walk)
with no retry/backoff and no upstream gate preventing `handleJobCompletion`
from running with a nil result (unlike Task — see below), any transient
resolution failure (informer cache lag on a fresh Manager restart/leader
failover, a momentary API-server hiccup) at the exact moment a Milestone/Phase/
Plan planner Job completes will:

1. Durably stamp `{Level}SpanEmittedUID` for that Job's UID (gate condition
   only checks `completedJob`/marker/`plannerSpanResolvable`, never `project`).
2. Call `synthesizePlannerSpan`, which no-ops (`emitted=false`) because
   `project == nil`.
3. Never emit a span for that Job attempt, and never persist
   `{Level}TraceSpanID` — **permanently**, because Milestone/Phase/Plan planner
   dispatch is documented as "single-shot per ROADMAP scope" (no retry Job),
   so this exact `completedJob.UID` is never revisited, and the marker check
   at the top of the block (`!= string(completedJob.UID)`) will forever be
   false on any future reconcile of the same object.

This also permanently breaks parent-linkage for every descendant level below
the affected one (a Phase whose parent Milestone hit this never gets a parented
span; the dispatch-prep `TraceParent` env var for that Phase's own Job is also
permanently empty), silently degrading the entire point of Phase 43 (connected
trace propagation) with no operator-visible signal (only a `logf.Info` at
`span_emission.go:155`, not a condition/metric).

Task's own `emitTaskSpanOnce` (added in this same phase,
`internal/controller/task_controller.go:956`) shows the fix was already
identified once in this diff:

```go
// task_controller.go:956
if completedJob == nil || project == nil || task.Status.TaskSpanEmittedUID == string(completedJob.UID) || !plannerSpanResolvable(completedJob) {
    return
}
```

Task checks `project == nil` **before** attempting to stamp the marker. The
three planner-tier handlers do not. (Project's own handler needs no such guard
— `project` there is always the reconciled object itself, never nil.)

No test in `span_emission_test.go`, `dispatch_traceparent_test.go`, or the unit
suite exercises "planner Job completes while the parent Project Get fails" for
Milestone/Phase/Plan, so this gap has zero coverage today.

**Fix:** Add `project == nil` (and ideally hoist the `TraceIDFromUID` validity
check) to the three planner-tier marker-stamp gates, mirroring Task:

```go
if completedJob != nil && project != nil && ms.Status.MilestoneSpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(completedJob) {
    ...
}
```

Apply the identical change at `phase_controller.go:526` and
`plan_controller.go:570`. Add an envtest spec per level (Milestone/Phase/Plan)
that seeds an unresolvable `ProjectRef`/parent-chain, drives
`handleJobCompletion`, and asserts the marker is **not** stamped (so a later
reconcile — after the transient failure clears — gets a chance to emit).

## Warnings

### WR-01: Post-emission `{Level}TraceSpanID` persist failure is a permanent, unretried degradation

**File:** `internal/controller/milestone_controller.go:611-624` (mirrored at `phase_controller.go:568-581`, `plan_controller.go:612-625`, `task_controller.go:1004-1017`, `project_controller.go:1861-1874`)

**Issue:** Even when `synthesizePlannerSpan` succeeds (`emitted=true`), the
follow-up `retry.RetryOnConflict` that persists `{Level}TraceSpanID` is a
**separate** patch from the marker stamp, and its own failure is logged
non-fatally with no further recovery path:

```go
logger.Error(tErr, "MilestoneTraceSpanID patch failed (non-fatal); child parent-linkage degraded for this level", "milestone", ms.Name)
```

Because the enclosing block is gated on `{Level}SpanEmittedUID`, which is
already durably stamped by this point, this specific Job attempt's block will
never re-execute. If the optimistic-lock retry (`retry.DefaultRetry`, a handful
of short-backoff attempts) exhausts against a transient API-server or conflict
storm, `{Level}TraceSpanID` is permanently empty for that level even though the
span itself *was* successfully exported — every descendant level's dispatch-prep
`TraceParent` and completion-time `parentSpanID` for this parent will forever
resolve to an unnested span. This is an accepted, documented tradeoff
("non-fatal ... degraded") but there is no operator-facing signal (metric,
condition, or event) distinguishing "never had a parent" from "lost the parent
link due to a transient patch failure" — both look identical (empty
`{Level}TraceSpanID`) from the outside.

**Fix:** At minimum, emit a Prometheus counter or a non-terminal Condition on
this failure path (mirroring the existing `RolledUp`/`SpanEmitted` marker
observability conventions elsewhere in this codebase) so a wave of these
failures is detectable rather than silently absorbed into "just another
unnested span."

### WR-02: Task's AGENT span status reflects the Job's own outcome, not the Task's final controller-side verdict

**File:** `internal/controller/task_controller.go:1066-1070`

**Issue:** `emitTaskSpanOnce`'s "call site 2" fires immediately after a
successful envelope read, **before** the `OutputValidationError` /
`OutputPathsViolation` / standard-result branch divergence
(`task_controller.go:1072` onward). `synthesizePlannerSpan` derives the span's
OTel status purely from `isJobFailed(completedJob)` — the underlying Job's own
success/failure — not from the Task's eventual terminal disposition. So a Task
whose subagent Job **succeeded** but which the controller subsequently fails
with `OutputPathsViolation` or `OutputValidationError` (real, exercised
terminal paths just below the emission call) will have its AGENT span recorded
with `codes.Ok` in Phoenix/the tracing backend, even though the Task CRD itself
lands `Status.Phase=Failed`. This is consistent with the four planner-level
spans (which similarly reflect Job outcome, not any later reporter/materialize
failure), so it may be the intended semantic — but it is not documented
anywhere, and it means an operator using the Phase 46 dashboard deep-link to
triage a Failed task by its trace will see a green/Ok span for the exact
dispatch that "failed."

**Fix:** Either (a) document this explicitly in `emitTaskSpanOnce`'s comment
("span status is job-outcome-only; controller-side policy failures are not
reflected") so it isn't mistaken for a bug later, or (b) if the intent is for
the span to reflect the Task's final verdict, move call site 2 after output
validation and thread the eventual reason/status back into
`synthesizePlannerSpan`.

## Info

### IN-01: Redundant parent-object fetch on every Phase/Plan dispatch and completion

**File:** `internal/controller/phase_controller.go:417-423`, `:557-563`; `internal/controller/plan_controller.go:437-443`, `:594-600`

**Issue:** `PhaseReconciler.resolveProject` (`phase_controller.go:863-879`)
already performs a `r.Get` on the parent Milestone while walking to Project,
but the new PROP-01/TRACE-02 code fetches the **same** Milestone object again,
separately, at both the dispatch-prep site and the completion-handler site.
`PlanReconciler.resolveProjectForPlan`'s slow path (`plan_controller.go:981-1014`)
has the identical duplication for Phase whenever the label fast-path isn't hit.
This doubles the API-server round trips on these hot paths. (Not flagged as a
performance defect per se — it's a correctness-adjacent code-quality/API-load
concern, not an algorithmic complexity one.)

**Fix:** Have `resolveProject`/`resolveProjectForPlan` optionally return the
already-fetched intermediate parent object (or cache it on the reconciler
struct for the duration of the call), and thread it into the trace-parenting
code instead of re-fetching.

### IN-02: `cmd/tide-reporter`'s new `--traceparent` flag is parsed but never consumed

**File:** `cmd/tide-reporter/main.go:67`, `:95`, `:108`

**Issue:** `reporterConfig.TraceParent` is parsed from the new `--traceparent`
flag and returned by `parseFlags`, but nothing in `run`/`runWithClient`
(`main.go:128-222`) reads `cfg.TraceParent`. This is explicitly documented as
"consumed starting Phase 44," so it's intentional forward-compat plumbing, not
a bug — flagging only so it isn't forgotten, and so a future
unused-field/dead-code lint pass doesn't need to rediscover the rationale.

**Fix:** None required now; revisit when Phase 44 wires the reporter's own
message spans under this parent.

---

_Reviewed: 2026-07-16T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
