# Phase 17: Address tech debt — Plan label backfill + gate hardening - Pattern Map

**Mapped:** 2026-06-12
**Files analyzed:** 6 source + 3 test (across 5 in-scope items; 1 DEFER)
**Analogs found:** 9 / 9 (every in-scope fix has a read-confirmed in-tree sibling)

## Orientation

This phase is pure internal-consistency work. **Every fix copies a pattern that already ships in this same codebase** at a sibling level (milestone/phase backfill → plan; plan's 12-05 reject-first → milestone/phase; milestone/phase non-fatal envelope-read → plan). There is no greenfield design here — the analog blocks below are the literal templates the executor replicates, with the noted adaptations.

All source edits MUST route through the active GSD plan (per CLAUDE.md GSD enforcement). All file access for this mapping was read-only.

## File Classification

| Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---------------|------|-----------|----------------|---------------|
| `internal/controller/plan_controller.go` (Item 1 backfill) | controller | event-driven (reconcile) | `phase_controller.go:168-186` backfill block | exact (sibling level) |
| `internal/controller/milestone_controller.go` (Item 2 reject reorder) | controller | event-driven (completion handler) | `plan_controller.go:466-472` reject-first | exact (sibling level, 12-05 template) |
| `internal/controller/phase_controller.go` (Item 2 reject reorder) | controller | event-driven (completion handler) | `plan_controller.go:466-472` reject-first | exact (sibling level, 12-05 template) |
| `cmd/tide/approve.go` (Item 3 guard narrow) | CLI command | request-response (one-shot) | `approve.go:155-188` approveLevel discovery | role-match (same file, narrow scope) |
| `internal/controller/plan_controller.go` (Item 5 envelope non-fatal) | controller | event-driven (completion handler) | `phase_controller.go:462-476` / `milestone_controller.go:529-545` non-fatal read | exact (sibling level, Pitfall-1) |
| `internal/reporter/materialize.go` (Item 6 *Project stamp) | service (reporter) | transform (child materialization) | `materialize.go:254-257` existing stamp call | role-match (same site, one special-case) |
| `internal/controller/plan_controller_test.go` (Items 1, 5 specs) | test (envtest) | — | `phase_controller_test.go:270-334` backfill spec | exact |
| `internal/controller/milestone_controller_test.go` + `phase_controller_test.go` (Item 2 specs; Item 6 row) | test (envtest) | — | `milestone_controller_test.go:436-488` backfill/stamp spec | exact |
| `cmd/tide/approve_test.go` (Item 3 row) | test (go-unit, fake client) | — | `approve_test.go:173-217` failed-level table tests | exact |

## Pattern Assignments

### Item 1 — `internal/controller/plan_controller.go` :: Plan-level project-label backfill (controller, event-driven) — HEADLINE

**Analog:** `phase_controller.go:168-186` (also `milestone_controller.go:178-194`). Both are read-confirmed identical-shape backfill blocks with a sibling `resolveProjectName*` helper.

**Insertion point:** Between step 4 (owner-ref, ends `plan_controller.go:173`) and step 5 (dispatcher seam, `:179`). This ordering is load-bearing — see Pitfall 2.

**Analog block to copy** (`phase_controller.go:174-186`):
```go
// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Phase
// itself when the label is absent. Heals pre-Phase-15 CRs created by the
// reporter before D-01 was in place. Guard: only patch when label is
// missing so the second reconcile is a no-op (T-15-03 / idempotent).
// Runs BEFORE reconcilePlannerDispatch so parked AwaitingApproval CRs
// also self-heal on their first post-upgrade reconcile.
if phase.Labels[owner.LabelProject] == "" {
    projectName := r.resolveProjectNameForPhase(ctx, &phase)
    if projectName != "" {
        patch := client.MergeFrom(phase.DeepCopy())
        if phase.Labels == nil {
            phase.Labels = map[string]string{}
        }
        phase.Labels[owner.LabelProject] = projectName
        if err := r.Patch(ctx, &phase, patch); err != nil {
            return ctrl.Result{}, fmt.Errorf("backfill project label on phase %s: %w", phase.Name, err)
        }
    }
}
```

**REQUIRED ADAPTATION — resolver returns an error, not "":** The Plan already has `resolveProjectName` (`plan_controller.go:1415-1425`), which returns `(string, error)` with `ErrParentUnresolved` on miss — UNLIKE the milestone/phase helpers, which return a bare `""`. Do NOT add a new `resolveProjectNameForPlan`; reuse the existing `resolveProjectName`. Adapt the guard to treat `err != nil || name == ""` as "skip silently":

```go
// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Plan itself when
// absent. Heals pre-Phase-15 Plan CRs on upgraded clusters. Runs BEFORE dispatch
// so a parked AwaitingApproval Plan self-heals on its first post-upgrade reconcile.
if plan.Labels[owner.LabelProject] == "" {
    if name, err := r.resolveProjectName(ctx, &plan); err == nil && name != "" {
        patch := client.MergeFrom(plan.DeepCopy())
        if plan.Labels == nil {
            plan.Labels = map[string]string{}
        }
        plan.Labels[owner.LabelProject] = name
        if err := r.Patch(ctx, &plan, patch); err != nil {
            return ctrl.Result{}, fmt.Errorf("backfill project label on plan %s: %w", plan.Name, err)
        }
    }
}
```

**Existing resolver to reuse** (`plan_controller.go:1415-1425`) — do NOT hand-roll a new chain walker:
```go
func (r *PlanReconciler) resolveProjectName(ctx context.Context, plan *tideprojectv1alpha1.Plan) (string, error) {
    if name, ok := plan.Labels["tideproject.k8s/project"]; ok && name != "" {
        return name, nil
    }
    if project := r.resolveProjectForPlan(ctx, plan); project != nil { // Plan→Phase→Milestone→Project, :829
        return project.Name, nil
    }
    return "", ErrParentUnresolved
}
```
Note: use `owner.LabelProject` (not the string literal) for the absent-guard key, matching milestone/phase. `resolveProjectName`'s fast-path already keys on the literal internally — fine to leave.

**Label constant:** `owner.LabelProject` = `"tideproject.k8s/project"` (`internal/owner/label.go:33`). Never write the literal.

---

### Item 2 — `milestone_controller.go` + `phase_controller.go` :: Reject short-circuit before reporter spawn (controller, event-driven)

**Analog:** `plan_controller.go:466-472` — the 12-05 "reject short-circuit FIRST" block, placed BEFORE the reporter spawn (`:513`) and BEFORE the envelope read (`:489`).

**Analog block (the template — `plan_controller.go:466-472`):**
```go
// Phase 12 / Phase 04.1: reject short-circuit FIRST — operator stop should always
// halt, regardless of envelope availability or read errors.
// Mirrors milestone_controller.go:442-449 ("reject short-circuit FIRST").
// D-05: park (not fail) — in-flight Jobs drain; state is preserved for resume.
if project != nil && gates.CheckRejected(project) {
    return r.patchPlanRejected(ctx, plan, gates.RejectedReason(project))
}
```

**Bug shape — phase (`phase_controller.go:446-512`):** The reject check at `:510-512` currently sits AFTER `spawnReporterIfNeeded` (`:483`), after budget rollup (`:489`), after the billing-halt backstop (`:496`). MOVE the reject block to be the **first statement after `projectUID` is derived** (i.e. right after `:452`, before the envelope-read block at `:462`).

```go
// CURRENT (phase_controller.go:510-512) — fires too late:
if project != nil && gates.CheckRejected(project) {
    return r.patchPhaseRejected(ctx, ph, gates.RejectedReason(project))
}
```
The phase already calls `r.patchPhaseRejected` (`phase_controller.go:677`) — just relocate the existing 3-line block upward; delete it from `:510-512`.

**Bug shape — milestone (`milestone_controller.go:505-556`):** The reject check at `:515-517` sits AFTER the envelope-read block (`:529-545`) but BEFORE `spawnReporterIfNeeded` (`:556`). Per RESEARCH line 101/274 the milestone's reporter spawn is at `:556` and reject at `:515` — reject is already ahead of the spawn here, BUT it is after the envelope read. For consistency with the 12-05 plan template (reject is the FIRST thing after `project` resolution, ahead of BOTH read and spawn), move the milestone reject block (`:515-517`) to immediately after `projectUID` derivation (`:508`), ahead of the envelope-read block at `:529`.

```go
// milestone reject block to relocate (milestone_controller.go:515-517):
if project != nil && gates.CheckRejected(project) {
    return r.patchMilestoneRejected(ctx, ms, gates.RejectedReason(project))
}
```

> Executor note: re-read both completion handlers in full before reordering — `:483`/`:556` `spawnReporterIfNeeded` returns `(isFirstCompletion, err)` consumed by the budget-rollup guard below it; ensure the relocated reject block sits ABOVE the `spawnReporterIfNeeded` call so the early `return` prevents the spawn (that is the entire point — Pitfall 3: prevent a NEW spawn, never delete an in-flight Job).

**Reporter spawn helper (do not modify):** `spawnReporterIfNeeded` (`dispatch_helpers.go:67`) is idempotent (AlreadyExists = ok). The fix is purely ordering — the early `return` on reject means the function is never reached.

---

### Item 3 — `cmd/tide/approve.go` :: Narrow D-07 failed-level guard to approval target (CLI, request-response)

**DESIGN FORK (RESEARCH A1 / Open Question 1):** Option A (narrow guard to the level being approved) vs Option B (keep project-wide, also apply to `--wave`). RESEARCH default-lean and project memory (fix-thoroughly-on-TIDE) both favor **A**. Surface in discuss/plan before implementing; this map documents the A-shape.

**Analog:** `approve.go:155-188` (`approveLevel` discovery body) and `findFailedLevel` (`:194+`).

**Current over-blocking guard (`approve.go:152-163`):**
```go
// D-07: check for Failed levels BEFORE the AwaitingApproval search.
if obj, kind, err := findFailedLevel(ctx, c, ns, projectName); err != nil {
    return err
} else if obj != nil {
    detail := buildFailureDetail(obj)
    return fmt.Errorf(
        "tide: level %q (%s) has failed%s; approval never retries failed work — use 'tide resume %s --retry-failed' to recover",
        obj.GetName(), kind, detail, projectName,
    )
}
```

**Discovery order it precedes (`approve.go:165-187`):** the guard runs before `findAwaitingMilestone → findAwaitingPhase → findAwaitingPlan → findAwaitingTask`. **Option A fix:** reorder so the AwaitingApproval target is discovered FIRST, then refuse only if THAT specific object is `Status.Phase=="Failed"` — not if some unrelated sibling is. Reuse `buildFailureDetail(obj)` for the message. The `findFailedLevel` project-wide scan (`:194`) can be retired or kept as a helper only for the targeted check.

**`--wave` path consistency (`approve.go:71-74`):** `approveRun` returns `approveWave` before ever reaching `approveLevel`'s guard. Under Option A the targeted guard belongs in the level path only (a `--wave` approve targets a specific Plan/wave, not a project-wide gate) — document the decision in the plan.

---

### Item 5 — `internal/controller/plan_controller.go` :: Envelope-read error non-fatal (controller, event-driven)

**Analog:** `phase_controller.go:462-476` AND `milestone_controller.go:529-545` — both treat a `ReadOut` error as **non-fatal** (log + defer to children-based succession via `envReadOK`/`envReaderPresent` sentinels). The plan handler is the outlier that wedges terminal `Failed`.

**Plan's buggy terminal branch (`plan_controller.go:489-505`) — REPLACE:**
```go
var readErr error
out, readErr = r.EnvReader.ReadOut(ctx, projectUID, string(plan.UID))
if readErr != nil {
    patch := client.MergeFrom(plan.DeepCopy())
    plan.Status.Phase = "Failed"
    meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionFailed,
        Status:             metav1.ConditionTrue,
        Reason:             "EnvelopeReadFailed",
        Message:            readErr.Error(),
        LastTransitionTime: metav1.Now(),
    })
    if pErr := r.Status().Patch(ctx, plan, patch); pErr != nil {
        return ctrl.Result{}, pErr
    }
    return ctrl.Result{}, nil
}
```

**Non-fatal template to copy (`phase_controller.go:462-476`):**
```go
var out pkgdispatch.EnvelopeOut
envReadOK := false
envReaderPresent := r.EnvReader != nil
if r.EnvReader != nil {
    var readErr error
    out, readErr = r.EnvReader.ReadOut(ctx, projectUID, string(ph.UID))
    if readErr != nil {
        // Non-fatal: log and defer to hasChildPlans fallback.
        logger.Error(readErr, "phase planner envelope tiny-status read failed (non-fatal); deferring to children-based succession", "phase", ph.Name)
    } else {
        envReadOK = true
    }
} else {
    logger.V(1).Info("no env reader; skipping tiny-status read", "phase", ph.Name)
}
```

**Milestone variant (`milestone_controller.go:529-545`)** carries the fuller comment explaining `envReaderPresent` distinguishes "no reader" (unit-test fallback) from "read error" (transient) so the children-based `BoundaryDetected` succession still fires. Adapt the plan handler to use both sentinels and route to the plan's existing children-based fallback (`hasChildPlans`/`hasChildTasks` — grep the plan handler's succession block below the read site). The plan currently has a `nil`-EnvReader fallback at `:479-487` (clears `Status.Phase=""`); keep that, and make the read-error case fall through to the same children-based path rather than setting `Failed`.

> Executor note: re-read `plan_controller.go:505-560` (the post-read succession/reporter block, not loaded here) before rewiring — the plan handler's downstream logic gates on `out.ChildCount`, so the non-fatal path must mirror how phase/milestone defer to the all-children-Succeeded signal that does NOT depend on the envelope.

---

### Item 6 — `internal/reporter/materialize.go` :: Project→Milestone reporter-edge label stamp (service/reporter, transform)

**Analog:** the existing stamp call at `materialize.go:254-257` (same site, one special-case added).

**Current stamp (`materialize.go:254-257`):**
```go
// D-01 (CUTS-01): stamp the canonical project label from the parent at
// create-site. Fail-open when the parent has no project label (StampProjectLabel
// is a no-op on empty string — RESEARCH Pitfall 1).
owner.StampProjectLabel(obj, parent.GetLabels()[owner.LabelProject])
```

**The gap:** When `parent` is a `*Project`, a Project does not carry `tideproject.k8s/project` pointing at itself, so `parent.GetLabels()[owner.LabelProject]` is `""` and the child Milestone is created unlabeled (the D-03 milestone backfill later heals the symptom, but create-site stays a no-op for this one edge — 15 WR-03).

**Fix shape:** resolve the project name to `parent.GetName()` when the parent IS a Project, else fall back to the parent's label:
```go
projectName := parent.GetLabels()[owner.LabelProject]
if _, isProject := parent.(*tideprojectv1alpha1.Project); isProject {
    projectName = parent.GetName()
}
owner.StampProjectLabel(obj, projectName)
```
(Confirm `parent`'s concrete/interface type at the call site by re-reading `materialize.go` around the `MaterializeChildCRDs` signature — it is passed as a parent object; the `*Project` type-switch above is the one-liner RESEARCH line 143 prescribes.)

**Label helper (`internal/owner/label.go:45`)** is already idempotent and no-ops on empty string — do not reimplement.

## Shared Patterns

### Idempotent label backfill (Items 1, 6)
**Source:** `milestone_controller.go:182-193` / `phase_controller.go:174-185`
**Apply to:** Plan backfill (Item 1).
**Pattern:** absent-guard (`Labels[owner.LabelProject] == ""`) → resolve → `client.MergeFrom(obj.DeepCopy())` → nil-map init → set label → `r.Patch`. The absent-guard makes the second reconcile a no-op (the idempotency contract every backfill test asserts via unchanged `ResourceVersion`).

### Reject-first short-circuit (Item 2)
**Source:** `plan_controller.go:466-472`
**Apply to:** `MilestoneReconciler.handleJobCompletion`, `PhaseReconciler.handleJobCompletion`.
**Pattern:** `if project != nil && gates.CheckRejected(project) { return r.patch<Level>Rejected(ctx, obj, gates.RejectedReason(project)) }` as the FIRST statement after project resolution, ahead of reporter spawn and envelope read. Each level already has its `patch<Level>Rejected` helper (`milestone:764`, `phase:677`, `plan:709`).

### Non-fatal envelope-read with sentinels (Item 5)
**Source:** `phase_controller.go:462-476`, `milestone_controller.go:529-545`
**Apply to:** plan completion handler.
**Pattern:** `envReadOK`/`envReaderPresent` bools; read error → `logger.Error(... "non-fatal ...")` and fall through to children-based succession; never set terminal `Status.Phase="Failed"` on a transient read error.

### Project-name resolution — reuse, don't hand-roll (Item 1)
**Source:** `resolveProjectName` (`plan_controller.go:1415`) → `resolveProjectForPlan` (`:829`, fast-path label + Plan→Phase→Milestone→Project walk; the `Items[0]` mis-routing fallback was already removed per `:1413`).
**Apply to:** Plan backfill. Returns `ErrParentUnresolved` on miss → treat as skip.

## Test Pattern Assignments

### Items 1 & 5 — `internal/controller/plan_controller_test.go` (new specs)
**Analog:** `phase_controller_test.go:270-334` — `Describe("PhaseReconciler — D-03 project-label backfill (CUTS-01)")`.
**Backfill spec recipe (mirror verbatim, swap level):** create Project → Milestone (with `ProjectRef`) → Phase (with `MilestoneRef`) → Plan (with `Spec.PhaseRef`, **labels intentionally absent**); construct `&PlanReconciler{Client: mgrClient, Scheme: ...}` with **no Dispatcher** (drives steps 1-5 only); `reconcileWithRetry(r.Reconcile, ..., 5)`; assert `after.Labels["tideproject.k8s/project"] == projName`; record `ResourceVersion`, reconcile again, assert unchanged. Finalizer-strip + delete cleanup block per `phase_controller_test.go:332-339`.
**Envelope-non-fatal spec (Item 5):** stub `EnvReader` returning an error for the Plan's UID, drive `handlePlannerJobCompletion`, assert `plan.Status.Phase != "Failed"` (requeues/defers) — mirrors milestone Pitfall-1 spec shape. Grep `milestone_controller_test.go` for the existing EnvReader-error / Pitfall-1 spec to copy the stub wiring.

### Item 2 — `milestone_controller_test.go` + `phase_controller_test.go` (new specs)
**Analog (structure):** the backfill `Describe` blocks (`milestone:436`, `phase:270`) for envtest scaffolding; no existing reject-after-spawn spec (grep returned none — net-new coverage).
**Recipe:** Project carrying the reject annotation so `gates.CheckRejected(project)` is true → drive the completion handler → assert (a) level parked Rejected, and (b) **no `tide-reporter-<uid>` Job exists** in the namespace (the load-bearing assertion). Do NOT assert a Job was deleted — assert none was created (Pitfall 3). Grep the existing `gates.CheckRejected` / reject tests for the reject-annotation setup helper.

### Item 3 — `cmd/tide/approve_test.go` (new table row)
**Analog:** `approve_test.go:173-217` — `TestApproveRunFailedLevelError` / `TestApproveFailedLevelErrorIncludesReason` (plain Go tests, `fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(...)`). Helpers `makeProject`, `makeFailedMilestone` already exist.
**Option-A row:** one Failed Plan AND one healthy AwaitingApproval Phase on the same project → assert `approveRun(...)` SUCCEEDS against the Phase (the healthy unrelated level is approvable despite the unrelated Failed Plan). Plus a `--wave` row pinning the chosen semantics. (Inverse of the current `TestApproveRunFailedLevelError`, which asserts the over-block.)

### Item 6 — `milestone_controller_test.go` (extend backfill spec)
**Analog:** `milestone_controller_test.go:436-488`.
**Recipe:** add a Project-parent row asserting the Milestone child carries `tideproject.k8s/project == projectName` **at create time** (before any backfill reconcile), exercising the reporter `MaterializeChildCRDs` `*Project` special-case. Grep `internal/reporter/*_test.go` for an existing `MaterializeChildCRDs` test to host the row if a reporter-side test is the better home.

## No Analog Found

| Item | Role | Reason |
|------|------|--------|
| Item 4 (WR-01 `checkParentApproval` fail-open) | controller helper | DEFER per RESEARCH (informer-lag-bounded, documented design choice). No edit → no analog needed. If included as belt-and-suspenders, analog is the existing `client.IgnoreNotFound` site at `dispatch_helpers.go:304-329` (return `(false, requeueErr)` instead of `(false, nil)`). |

There are NO net-new files. Every source edit lands in an existing file; every test extends an existing `*_test.go`. (RESEARCH "Wave 0 Gaps": no new spec files needed.)

## Metadata

**Analog search scope:** `internal/controller/` (plan/phase/milestone reconcilers + tests, dispatch_helpers), `internal/reporter/materialize.go`, `internal/owner/label.go`, `cmd/tide/approve.go` + `approve_test.go`.
**Files scanned:** 9 read + 3 grep passes (reject helpers, stamp call sites, approve test structure).
**Pattern extraction date:** 2026-06-12
**Validation framework:** Ginkgo v2.28 + Gomega + envtest (`make test` unit tier; `make test-int` for gate — read `MAKE_EXIT` + grep `^--- FAIL|^FAIL\s`, never the Ginkgo summary alone, per Pitfall 4).
