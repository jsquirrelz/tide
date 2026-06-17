---
phase: 25-global-dispatch-failure-semantics-gates-resumption
reviewed: 2026-06-17T00:00:00Z
depth: standard
files_reviewed: 16
files_reviewed_list:
  - api/v1alpha2/project_types.go
  - api/v1alpha2/shared_types.go
  - api/v1alpha2/task_types.go
  - cmd/tide/resume.go
  - cmd/tide/resume_failure_test.go
  - internal/controller/depgraph.go
  - internal/controller/depgraph_test.go
  - internal/controller/failure_halt.go
  - internal/controller/failure_halt_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/task_controller.go
  - internal/controller/task_global_dispatch_test.go
  - test/integration/envtest/global_dispatch_test.go
findings:
  critical: 2
  warning: 5
  info: 4
  total: 11
status: issues_found
---

# Phase 25: Code Review Report

**Reviewed:** 2026-06-17
**Depth:** standard
**Files Reviewed:** 16
**Status:** issues_found

## Summary

Reviewed the Phase 25 widening of task dispatch from plan-local to project-global
indegree (DISP-01), the conservative failure-halt mirroring billing-halt (DISP-02),
the task-level approve gate composing with global readiness (DISP-03), and the
`tide resume --retry-failed` recovery path (RESUME-01).

The core mechanism is well-built: the shared `buildScopeResolver` genuinely backs
both `assembleProjectDepGraph` (wave derivation) and `computeGlobalIndegree`
(dispatch), so the D-01 "never disagree" invariant holds; edge de-dup is correct;
the conservative profile gates all four up-stack execution dispatch sites and
correctly skips the project planner site; the watch mapper re-enqueues both
direct-name and coarse-ref dependents.

Two correctness defects rise to BLOCKER. First, `tide resume --retry-failed`
clears `ConditionFailureHalt` **before** it resets the Failed Task phases, opening
a re-stamp race: any reconcile firing on a still-Failed Task between the two
operations re-stamps the halt, leaving the project frozen after a "successful"
resume. This is the exact race the BillingHalt path mitigates with a resume-time
fence — and `FailureHalt` was given the annotation constant
(`AnnotationFailureResumedAt`) but never wires the fence. Second, that missing
fence is itself fail-closed-but-wrong: even with no `--retry-failed` race,
`setFailureHaltIfNeeded` re-fires on every Failed-Task reconcile with no time
guard, so a single stale Failed Task that outlives a resume re-halts the project.

The remaining findings are watch-liveness gaps (resolved-but-unlabeled member
tasks never re-enqueue their dependents), conservative-profile dependent stalls,
and several maintainability issues in the resolver's name-collision handling.

## Critical Issues

### CR-01: `tide resume --retry-failed` clears FailureHalt before resetting Failed tasks — re-stamp race re-freezes the project

**File:** `cmd/tide/resume.go:131-152`
**Issue:**
`resumeRun` clears `ConditionFailureHalt` first (lines 136-149), then calls
`retryFailedLevels` to reset Failed Task phases (line 151). Between those two
operations every Failed Task still has `Status.Phase == "Failed"`. The
TaskReconciler terminal short-circuit re-stamps the halt on exactly those tasks:

```go
// task_controller.go:308-322 — gateChecks terminal short-circuit
if task.Status.Phase == "Failed" {
    if project, pErr := r.resolveProject(ctx, task); pErr == nil && project != nil {
        if hErr := setFailureHaltIfNeeded(ctx, r.Client, project); hErr != nil { ... }
    }
    ...
}
```

And `handleJobCompletion` (task_controller.go:967-974) also re-stamps on the
failed-result branch. Any reconcile triggered during the resume window (the
manager runs continuously; the global Task watch fires on the very status patch
resume itself issues) re-asserts `FailureHalt=True`. The operator sees `resume`
print "cleared FailureHalt" and exit 0, but the project stays halted —
indistinguishable from the run-1 recovery failure this verb was built to replace.

This is the precise hazard BillingHalt guards against with the
`jobStart < AnnotationBillingResumedAt` time-fence (`billing_halt.go:113-120`).
FailureHalt has the annotation constant (`AnnotationFailureResumedAt`,
shared_types.go:249-253) but never stamps or reads it.

**Fix:** Reset the Failed Task phases FIRST, then clear the halt last (so no task
can re-stamp after the clear); AND/OR stamp+honor the resume fence. Minimal safe
ordering change:

```go
if !retryFailed {
    return nil
}
// Reset Failed levels first — no task can re-stamp once it leaves "Failed".
if err := retryFailedLevels(ctx, c, ns, projectName, out); err != nil {
    return err
}
// Re-fetch for a fresh resourceVersion, then clear FailureHalt last.
if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: projectName}, &proj); err != nil {
    return fmt.Errorf("re-get project for FailureHalt clear: %w", err)
}
// ... RemoveStatusCondition(ConditionFailureHalt) + Status().Patch ...
```

Ordering alone narrows but does not fully close the window (a reconcile can still
land mid-`retryFailedLevels` before all tasks are reset). Pair it with the
time-fence: stamp `AnnotationFailureResumedAt` in resume and gate
`setFailureHaltIfNeeded` on `jobStart`/`task.Status.CompletedAt < resumedAt`,
mirroring `setBillingHaltIfNeeded`.

### CR-02: `setFailureHaltIfNeeded` has no resume time-fence — a stale Failed task re-halts after recovery

**File:** `internal/controller/failure_halt.go:64-96`
**Issue:**
`setFailureHaltIfNeeded` stamps `FailureHalt=True` whenever the profile is
conservative and the condition is not already True. It accepts no `jobStart` /
resume timestamp and consults no annotation. Contrast `setBillingHaltIfNeeded`,
which takes `jobStart time.Time` and refuses to re-stamp when
`jobStart.Before(resumedAt)` (billing_halt.go:104-120). The phase-spec risk list
calls out this exact class as "fail-open"; here it is the inverse — fail-closed
re-halt — but it equally defeats recovery.

Concretely: operator runs `tide resume --retry-failed`; a Failed Task whose status
patch has not yet landed (informer lag, or a Task the operator did not intend to
retry but that is still Failed for an unrelated reason) reconciles after the clear
and re-stamps the halt. There is no signal that distinguishes a pre-resume
straggler from a genuine fresh post-resume failure. The annotation
`AnnotationFailureResumedAt` exists specifically to carry that fence (its doc
comment, shared_types.go:249-253, says "only needed if the reconciler gates
re-stamping FailureHalt against this timestamp") but no code path ever writes or
reads it.

**Fix:** Thread the completion time into the re-stamp and fence on the resume
annotation, mirroring billing:

```go
func setFailureHaltIfNeeded(ctx context.Context, c client.Client,
    project *tideprojectv1alpha2.Project, taskCompletedAt time.Time) error {
    // ... profile + already-halted checks ...
    if !taskCompletedAt.IsZero() {
        if v, ok := project.Annotations[tideprojectv1alpha2.AnnotationFailureResumedAt]; ok {
            if resumedAt, err := time.Parse(time.RFC3339, v); err == nil &&
                taskCompletedAt.Before(resumedAt) {
                return nil // pre-resume straggler — do not re-halt
            }
        }
    }
    // ... stamp FailureHalt=True ...
}
```

and have resume.go stamp `AnnotationFailureResumedAt` (it already has the
metadata-patch idiom from the BillingHalt path, resume.go:110-118). Pass
`task.Status.CompletedAt` (or the completed Job's CreationTimestamp) from both
call sites in task_controller.go.

## Warnings

### WR-01: globalDependentsMapper cannot re-enqueue coarse-ref dependents of a resolved-but-unlabeled predecessor — liveness gap until resync

**File:** `internal/controller/task_controller.go:1488-1556`
**Issue:**
The mapper lists only project-labeled tasks (`MatchingLabels{owner.LabelProject:
projectName}`, line 1500-1503) and self-skips. It builds `matchable` from the
*changed* task's name + ancestor scope names, then scans the labeled task set for
dependents. Two stall paths:

1. If the changed (predecessor) task lacks the project label, line 1493-1496
   returns `nil` — none of its dependents are re-enqueued on completion. The
   dependent then waits for the next periodic resync (default 10h) to re-evaluate
   indegree. `assembleProjectDepGraph` already documents unlabeled tasks as a
   known gap (WR-04, project_controller.go:1466-1469), but the dispatch-liveness
   consequence is not covered there.
2. A dependent whose `DependsOn` names a Phase/Milestone is only re-enqueued if
   `ancestorScopeNames(task.Spec.PlanRef)` resolves that phase/milestone — which
   requires the changed task's Plan→Phase→Milestone chain to be fully present and
   labeled in the lists. A missing Phase CR (informer lag) yields `phaseName=""`,
   so a milestone-ref dependent is silently not re-enqueued.

These are not data-loss, but they degrade the "no dependent stalls until resync"
guarantee the phase brief flags as a watch-liveness risk.

**Fix:** When the changed task lacks the label, fall back to a namespace-wide Task
list (the mapper already does namespace-wide lists for plans/phases/milestones).
Alternatively, register a field index on the project label so the backfill gap
cannot hide a predecessor. At minimum, document the resync-bounded staleness next
to the mapper and emit a V(1) log when `projectName == ""` so the stall is
observable.

### WR-02: globalDependentsMapper Watches has no event predicate — fires full 4-List re-derive on every Task event incl. no-op metadata updates

**File:** `internal/controller/task_controller.go:1684-1687`
**Issue:**
The `globalDependentsMapper` is wired as a bare `Watches(&Task{}, ...)` with no
`builder.WithPredicates(...)`. Every Task event in the watched namespace —
including finalizer adds, owner-ref updates, label stamps, and the mapper/For
re-reconcile's own status patches — invokes the mapper, which issues four List
calls (tasks, plans, phases, milestones) and rebuilds the resolver each time. The
sibling annotation-only Watches right below it (line 1688-1694) correctly scopes
with `annotationOnly`. The asymmetry means the heaviest mapper runs unfiltered.

Correctness is preserved (the mapper is idempotent), and raw O(n) cost is out of
v1 scope — but the missing predicate also means a no-op update storm can mask the
WR-01 liveness gap by sheer requeue volume rather than by design, which is fragile.

**Fix:** Constrain the mapper watch to the events that actually change readiness.
A `predicate.Funcs{UpdateFunc: ...}` that fires only when `Status.Phase` changes
(the only field `computeGlobalIndegree` reads on predecessors) eliminates the
no-op churn while keeping completion/failure/hold transitions live.

### WR-03: Conservative failure-halt permanently strands dependents and never reaches Complete without operator action — verify intended

**File:** `internal/controller/failure_halt.go:51-62`, `internal/controller/task_controller.go:393-397`
**Issue:**
Under conservative profile, the first task failure stamps `FailureHalt=True` and
all four up-stack dispatch gates park with a 30s requeue indefinitely. There is no
auto-recovery and `checkProjectComplete` requires all Milestones Succeeded, which
can never happen while a Task is Failed. This is the documented design (cleared
only by `tide resume --retry-failed`) — but combined with CR-01/CR-02 it means a
conservative-profile project that hits any failure is unrecoverable via the
sanctioned verb until those bugs are fixed. Flagging so the reviewer confirms the
interaction is acceptable and that the integration suite exercises the
*post-resume re-dispatch* path, not just the halt-stamp (the current envtest spec,
global_dispatch_test.go:264-306, asserts only that the halt is set, never that
resume clears it and dispatch resumes).

**Fix:** Add an envtest spec that (a) drives a conservative failure to halt,
(b) runs the `--retry-failed` recovery, and (c) asserts a previously-blocked task
reaches Running. That spec would have caught CR-01.

### WR-04: resolveScope name-collision precedence can silently drop edges across scope levels

**File:** `internal/controller/depgraph.go:95-126`
**Issue:**
`resolveScope` returns on the FIRST level that matches: task name → plan name →
phase name → milestone name. If two levels share a name (a Plan named `foo` and a
Phase also named `foo`, or a Task whose name equals a Plan name), only the first
match's member set is returned and the other scope's members are silently omitted
from the edge set. K8s does not prevent a Task and a Plan from sharing a name
(different Kinds, same namespace), so a coarse ref `DependsOn: ["foo"]` intending
the Plan would resolve to the single Task `foo` instead. Because dispatch and wave
derivation use the same resolver they stay mutually consistent (D-01 holds), but
both are consistently *wrong* — a missed dependency edge is a fail-open dispatch
(the dependent runs before its true predecessors).

**Fix:** Either (a) document and enforce a namespace-wide name-uniqueness
constraint across Task/Plan/Phase/Milestone (CEL or admission), or (b) make
DependsOn entries carry an explicit Kind discriminator so resolution is
unambiguous. At minimum, log at V(1) when a scope name matches at more than one
level so the collision is observable.

### WR-05: `retryFailedLevels` blanks `Status.Conditions` on reset, discarding diagnostic history without a generation bump to guarantee re-reconcile

**File:** `cmd/tide/resume.go:182-184` (and the three sibling blocks at 207-209, 232-234, 257-259)
**Issue:**
Each reset sets `item.Status.Phase = ""` and `item.Status.Conditions = nil` then
stamps a single `ResumedByUser` condition. Two concerns: (1) wiping all prior
conditions destroys the failure diagnostics (the `Failed` condition Message that
told the operator *why* it failed) — there is no record left once reset. (2) The
reset is a status-subresource patch; status patches do not bump
`metadata.generation`, and the Task For() watch in this codebase relies on a mix
of generation/annotation predicates elsewhere — confirm the TaskReconciler For()
watch actually re-reconciles on this status-only change. (The global Task watch
mapper WR-02 has no predicate so it *will* fire, which likely saves it, but that
is incidental coupling, not a designed guarantee.)

**Fix:** Preserve the terminal failure condition (or copy its Message into the
ResumedByUser condition) for post-mortem. Verify via an envtest that a
status-only phase reset deterministically re-triggers dispatch rather than relying
on the unfiltered mapper.

## Info

### IN-01: `buildScopeResolver` accepts a `ms []Milestone` arg it never indexes

**File:** `internal/controller/depgraph.go:56-83`
**Issue:** The `ms` parameter is discarded (`_ = ms`, line 82) — milestones are
reached transitively via `phaseToMS`. The signature implies milestone data is
consumed when it is not, and every caller allocates/passes the slice needlessly.
**Fix:** Drop the parameter (and the `_ = ms`), or add a comment at the call sites
that it is reserved for a future direct milestone index. Keeping the unused arg is
mild API noise.

### IN-02: `gateDispatch` and `ensureJob` are dead code retained only for a grep contract

**File:** `internal/controller/task_controller.go:1343-1355`, `1361-1402`
**Issue:** Both carry `//nolint:unused` with "intentionally retained per plan grep
contract; wired by a later phase." `ensureJob` duplicates `createDispatchJob`'s
build/create/reserve logic, so a future edit to one can silently diverge from the
other. Dead duplicated dispatch logic is a maintenance trap.
**Fix:** Delete now that the real dispatch path (`createDispatchJob`) is wired, or
have `createDispatchJob` delegate to `ensureJob` so there is a single source of
truth.

### IN-03: `deriveGlobalWaves` prune is knowingly incapable of protecting in-flight waves (OQ-3 deferred)

**File:** `internal/controller/project_controller.go:1632-1647`
**Issue:** The prune deletes any Wave with `WaveIndex >= len(globalWaves)` with no
in-flight guard; the comment acknowledges a re-derivation that drops a wave can
delete a Wave whose Tasks are still running. Self-documented as out of scope for
Phase 25, but it is a latent correctness risk for the global re-derive model the
phase introduces (a DependsOn edit shrinking the wave count mid-run).
**Fix:** Track as a Phase 25+ follow-up; ensure the wave controller can
distinguish "no tasks assigned" from "tasks in-flight" before the guard is added.

### IN-04: terminology mismatch — "four EXECUTION dispatch sites" are actually planner-dispatch reconcilers

**File:** `internal/controller/failure_halt.go:29-33`
**Issue:** The comment says checkFailureHalt is added to "the four EXECUTION
dispatch sites (task/plan/phase/milestone controllers)" and NOT the planner site.
But milestone/phase/plan reconcilers dispatch `JobKindPlanner` jobs
(milestone_controller.go:453, phase_controller.go:415, plan_controller.go:433) —
only TaskReconciler dispatches `JobKindExecutor`. The gate placement matches the
phase brief's wording ("gate all FOUR execution dispatch sites
(task/plan/phase/milestone)") so behavior is as specified, but the in-code label
"EXECUTION" is misleading and contradicts failure_halt.go's own rationale that
"gating planning would wrongly freeze authoring of already-approved scopes" —
which is exactly what gating the three planner reconcilers does.
**Fix:** Reconcile the comment with reality: these are the four up-stack dispatch
sites; conservative halt deliberately freezes further planner *and* executor
dispatch project-wide except the root project planner. Clarify why freezing
mid-stack planning is acceptable here (the failure already invalidates downstream
authoring) vs. the stated rationale.

---

_Reviewed: 2026-06-17_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
