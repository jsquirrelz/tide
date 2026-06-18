---
phase: 27-budget-bypass-resume-correctness
reviewed: 2026-06-18T16:16:14Z
depth: standard
files_reviewed: 8
files_reviewed_list:
  - api/v1alpha2/project_types.go
  - config/crd/bases/tideproject.k8s_projects.yaml
  - internal/controller/project_controller.go
  - internal/budget/cap.go
  - internal/budget/cap_test.go
  - internal/controller/project_controller_test.go
  - internal/controller/project_clone_idempotency_test.go
  - internal/controller/project_planner_completion_test.go
findings:
  critical: 1
  warning: 3
  info: 4
  total: 8
status: resolved
resolution:
  resolved: 2026-06-18
  fixed_findings: [CR-01, WR-01, WR-02, WR-03]
  deferred_findings: [IN-01, IN-03, IN-04]
  note: >-
    All Critical + Warning findings fixed and verified (commits 3a42a0e, ffb091b,
    5d04779, 9f5cad7). CR-01 and WR-03 are locked by RED-verified new envtests;
    WR-01 rewritten to genuinely exercise the durable marker; WR-02 now re-drives
    Reconcile. IN-02 clarity comment folded into the CR-01 fix. Remaining Info
    findings (IN-01 prefix matching, IN-03 patch-pattern note, IN-04 decouple
    marker from reporter existence) deferred as non-blocking follow-ups.
    controller + budget envtest packages GREEN; go build/vet clean.
---

# Phase 27: Code Review Report

**Reviewed:** 2026-06-18T16:16:14Z
**Depth:** standard (Go-specific per-file analysis)
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Phase 27 lands five durable-flag guards (BYPASS-01..05) on the budget-bypass / resume path.
The schema additions (`CloneComplete`, `PlannerRolledUpUID`, `BypassBaselineCents`) are
correctly shaped (`+optional`, `omitempty`, no version bump) and regenerated into the CRD.
The clone-idempotency guard (BYPASS-02) and the resume-at-Running fix (BYPASS-01) are
correct and well-tested. `IsCapExceeded` is left untouched per plan, with good unit coverage.

The adversarial pass surfaced one **BLOCKER**: the new acknowledged-spend baseline
(`BypassBaselineCents`, BYPASS-04/D-04) is never reset and the `newSpendSinceBypass`
guard is applied to *every* re-halt evaluation — so a rolling-window reset after a bypass
leaves a stale high baseline that silently suppresses a legitimate halt in the new window,
defeating the budget cap. Two **WARNING** findings concern the BYPASS-03 regression test
not actually exercising the marker it is meant to lock in, and the BYPASS-04 "resume sticks"
test using `Consistently` without re-driving reconciles (so it cannot catch a re-halt that
only fires on a later reconcile). Remaining items are robustness / fragility notes.

## Critical Issues

### CR-01: Stale `BypassBaselineCents` defeats the budget halt after a rolling-window reset

**File:** `internal/controller/project_controller.go:1334` (guard) + `:1310` (set) ; interaction with `internal/budget/tally.go:129-138` (`MaybeResetWindow`)
**Confidence:** High (logic is provable from the code); Medium that it triggers in a given deployment (requires a window reset after a bypass — routine on the default 24h rolling window).
**Severity:** BLOCKER — silently allows spend up to the prior baseline in a fresh window before halting; this is exactly the overspend a budget cap exists to prevent.

**Issue:**
`BypassBaselineCents` is set in exactly one place (`:1310`, the bypass-clear branch) and is
**never reset** anywhere (confirmed: `grep BypassBaselineCents` shows only set at :1310,
read at :1334). The re-halt guard at `:1334-1335` applies `newSpendSinceBypass =
CostSpentCents > BypassBaselineCents` to **all** re-halt evaluations, not just the
immediate post-bypass reconcile.

`MaybeResetWindow` (called at the top of `handleBudgetGate`, :1276) zeroes
`CostSpentCents` on window rollover but leaves `BypassBaselineCents` untouched. Sequence:

1. Bypass at spend=200 → `BypassBaselineCents=200`, phase=Running.
2. Rolling window elapses → `MaybeResetWindow` sets `CostSpentCents=0`; baseline stays 200.
3. New spend accrues to 150 in the new window; `RollingWindowCapCents=100`.
4. `capExceeded=true`, `bypassed=false`, but `newSpendSinceBypass = 150 > 200 = false`.
   **Re-halt is suppressed** — the project keeps dispatching despite the new window
   exceeding its cap by 50 cents, and will not halt until spend climbs past the stale 200.

The baseline is meant to acknowledge *already-incurred* spend so a resume sticks; once the
window that incurred it resets to zero, the baseline is meaningless and must not gate the
new window's halt.

**Fix:** Clear (or re-anchor) the baseline when the window resets, and/or scope the
`newSpendSinceBypass` guard so it cannot suppress a halt once spend has been reset below it.
Minimal fix — reset the baseline inside the window-reset path:

```go
// internal/budget/tally.go, in MaybeResetWindow window-elapsed block:
project.Status.Budget.CostSpentCents = 0
project.Status.Budget.TokensSpent = 0
project.Status.Budget.BypassBaselineCents = 0 // stale baseline must not survive a window reset
```

Add an envtest: bypass at spend=200, force a window reset (`WindowStart` in the past +
elapsed duration), stamp new spend > cap but < 200, reconcile, assert phase re-halts to
`BudgetExceeded`. (The current BYPASS-04 specs never reset the window, so this path is
untested.)

## Warnings

### WR-01: BYPASS-03 double-count test passes via the old `isFirstCompletion` guard, not the new marker

**File:** `internal/controller/project_planner_completion_test.go:121-161`
**Confidence:** High — verified by tracing `handleProjectJobCompletion` against the test's call sequence.
**Severity:** WARNING — the headline fix (durable `PlannerRolledUpUID` marker) is not actually locked in by its own regression test; the marker code could be deleted and this test would still pass.

**Issue:**
The double-count guard in `handleProjectJobCompletion` (`:1212-1226`) is nested *inside*
the outer `if isFirstCompletion && envReadOK` block, where `isFirstCompletion` is true only
when the reporter Job is `IsNotFound`. The test calls `handleProjectJobCompletion(ctx, proj, nil)`
twice in a row (lines 140, 147) **without deleting the reporter Job between calls**. The first
call creates `tide-reporter-<uid>` and sets the marker; the second call finds the reporter Job
present → `isFirstCompletion=false` → the function returns before ever reaching the
`PlannerRolledUpUID != plannerJobName` check. So the no-double-count assertion at `:154` holds
because of the *pre-existing* reporter-existence guard, not the new durable marker.

The actual BYPASS-03 scenario is reporter-Job TTL-GC during a halt (reporter absent on resume,
`isFirstCompletion` flips back to true). The test never simulates that, so it does not prove the
marker prevents the double count it was written for.

**Fix:** Between the two calls, delete the reporter Job (simulate the 300s TTL-GC) so the
second call enters with `isFirstCompletion=true` and the marker is the *only* thing preventing
the second rollup:

```go
_, err := r.handleProjectJobCompletion(ctx, proj, nil)
Expect(err).NotTo(HaveOccurred())
// Simulate reporter-Job TTL-GC so isFirstCompletion flips back to true — the real BYPASS-03 path.
reporterJobName := fmt.Sprintf("tide-reporter-%s", proj.UID)
Expect(k8sClient.Delete(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: reporterJobName, Namespace: "default"}}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
Eventually(/* reporter gone */).Should(...)
Expect(mgrClient.Get(ctx, ..., proj)).To(Succeed())
_, err = r.handleProjectJobCompletion(ctx, proj, nil) // now gated only by the marker
```

### WR-02: BYPASS-04 "resume sticks" test uses `Consistently` without re-driving reconciles

**File:** `internal/controller/project_controller_test.go:720-730`
**Confidence:** High.
**Severity:** WARNING — the test cannot observe a re-halt that fires on a *subsequent* reconcile, so it under-verifies the no-re-halt guarantee.

**Issue:**
After Reconcile-3 (`:721`), the `Consistently` block (`:725-730`) only re-`Get`s the Project;
it never calls `reconciler.Reconcile` again. In envtest there is no running manager driving
reconciles, so nothing can change `Status.Phase` during the `Consistently` window — the
assertion is effectively a static re-read of Reconcile-3's outcome dressed up as a
multi-sample stability check. A regression where the re-halt fires on the *second*
post-bypass reconcile (e.g. CR-01's stale-baseline path) would slip past this test.

**Fix:** Drive `reconciler.Reconcile` inside the polled function so each sample reflects a
fresh reconcile:

```go
Consistently(func(g Gomega) {
    _, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: name})
    g.Expect(err).NotTo(HaveOccurred())
    var refreshed tideprojectv1alpha2.Project
    g.Expect(k8sClient.Get(ctx, name, &refreshed)).To(Succeed())
    g.Expect(refreshed.Status.Phase).NotTo(Equal(tideprojectv1alpha2.PhaseBudgetExceeded))
}, 2*time.Second, 200*time.Millisecond).Should(Succeed())
```

### WR-03: A terminally-failed clone Job blocks re-dispatch until its TTL expires (CloneComplete never set, IsNotFound never true)

**File:** `internal/controller/project_controller.go:567` (dispatch guard) + `:590` (set-on-success)
**Confidence:** High on the logic; this is partly pre-existing behavior that BYPASS-02 did not change but also did not address.
**Severity:** WARNING — a clone failure stalls the project for up to the clone Job TTL (300s) with no progress signal; recovers only after TTL-GC re-enables dispatch.

**Issue:**
Dispatch is gated on `!CloneComplete && IsNotFound(cloneErr)` (`:567`). Set-on-success is
gated on `existingClone.Status.Succeeded > 0` (`:590`). When the clone Job exhausts its
`BackoffLimit` and goes terminal-**Failed** (`Failed>0, Succeeded==0`):
- `cloneErr == nil` (Job still exists) → `IsNotFound=false` → dispatch skipped,
- `Succeeded>0` false → `CloneComplete` never set.

The project makes no progress and emits no failure condition for the clone until the Job's
300s TTL elapses and `IsNotFound` flips true. There is no `Status.Failed > 0` arm to surface
a clone-failed phase or to fast-path a re-dispatch.

**Fix:** Add an explicit terminal-failed arm in the clone block — e.g. when
`existingClone.Status.Failed > 0 && Succeeded == 0`, delete the failed Job (so the next
reconcile re-dispatches) and/or set an observability condition. At minimum, document that
clone recovery depends on TTL-GC so an operator reading the code understands the stall window.

## Info

### IN-01: Fragile fixed-width prefix matching in Job cleanup

**File:** `internal/controller/project_planner_completion_test.go:115`, `:179`, `:246`
**Confidence:** High.
**Issue:** `j.Name[:13] == "tide-project-"` / `== "tide-reporter"` relies on both prefixes
being exactly 13 characters (they coincidentally are: `tide-project-` includes the trailing
hyphen, `tide-reporter` does not). A future rename (`tide-reporter-` with a hyphen, length 14)
would silently stop matching reporter Jobs and leak them across specs. Line 246 also contains
a redundant `len(j.Name) > 13 &&` repeated inside the `||`.
**Fix:** Use `strings.HasPrefix(j.Name, "tide-project-")` / `strings.HasPrefix(j.Name, "tide-reporter-")`.

### IN-02: `newSpendSinceBypass` now gates the *first-ever* halt, coupling unrelated logic

**File:** `internal/controller/project_controller.go:1334-1335`
**Confidence:** Medium (works today, but the coupling is non-obvious).
**Issue:** The guard is added to the shared re-halt branch, so it also governs a project's
first-ever budget halt (where `BypassBaselineCents` is the zero value, 0). It happens to be
safe today because `capExceeded` implies `CostSpentCents > cap > 0 >= baseline`, so
`newSpendSinceBypass` is true. But the halt path's correctness now silently depends on the
zero-value of an unrelated bypass field. A reader cannot tell from the branch that the baseline
guard is only meaningful post-bypass.
**Fix:** Consider gating the baseline comparison on "a bypass has been applied" (e.g. only when
`BypassBaselineCents > 0`) or add a comment at the branch explaining why a never-bypassed
project is unaffected. This also interacts with CR-01 — both stem from the baseline being a
global, never-reset field consulted on every halt.

### IN-03: `PlannerRolledUpUID` marker patch taken after `RollUpUsage` mutated `project.Status.Budget` in place

**File:** `internal/controller/project_controller.go:1214-1223`
**Confidence:** High — behavior is correct; flagged for clarity.
**Issue:** `budget.RollUpUsage` re-fetches `latest` and assigns `project.Status.Budget =
latest.Status.Budget` (`tally.go:82`) but does **not** refresh `project.ResourceVersion`. The
subsequent `markerPatch := client.MergeFrom(project.DeepCopy())` (`:1219`) is a non-optimistic
JSON merge patch, so it only sends the changed `plannerRolledUpUID` field and the stale
resourceVersion is irrelevant. This is correct, but the in-place mutation + non-optimistic
follow-up patch is a subtle pattern; a future change to `RollUpUsage` (e.g. switching to an
optimistic-locked merge here) could surface a conflict. No change required; noting the
fragility.

### IN-04: `handleProjectJobCompletion` skips rollup when the reporter Job already exists on the *first* genuine completion

**File:** `internal/controller/project_controller.go:1212`
**Confidence:** Medium.
**Issue:** Because the marker check is nested inside `isFirstCompletion && envReadOK`, if the
reporter Job already exists at the moment of the first real completion (e.g. a re-enqueue that
created the reporter before this path observed the envelope), `isFirstCompletion=false` and the
rollup never happens — and the marker is never set, so a later resume (reporter GC'd) *would*
then roll up. This is the inverse of the double-count: a potential *missed* rollup on first
completion. This is pre-existing `isFirstCompletion` behavior that Phase 27 preserved by design
(the plan explicitly kept the outer condition), but it means rollup-exactly-once is not fully
guaranteed by the marker alone. Worth a follow-up: make the marker the sole rollup gate
(decoupled from reporter-Job existence) so rollup is "exactly once per planner Job" independent
of reporter timing.

---

_Reviewed: 2026-06-18T16:16:14Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
