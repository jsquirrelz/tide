# Phase 27: Budget-Bypass Resume Correctness - Research

**Researched:** 2026-06-18
**Domain:** Kubernetes CRD controller — budget halt/resume, clone Job idempotency, planner cost rollup, dual budget caps
**Confidence:** HIGH

## Summary

Phase 27 is a five-requirement correctness phase. All five bugs are code-located in `internal/controller/project_controller.go` and `internal/budget/cap.go`. No new packages, no new binaries, no import-path changes. The phase scope is additive status fields (`CloneComplete bool` on `GitStatus`, `PlannerRolledUpUID string` on `BudgetStatus`) plus targeted one-line behavioral fixes and new envtest coverage.

The root cause of BYPASS-01 and BYPASS-02 is the same code path: `handleBudgetGate` at `project_controller.go:1257` sets `Status.Phase = PhasePending` instead of `PhaseRunning`. Because `PhasePending` routes the next reconcile through `reconcileProjectPhase2` Step 3 (init Job creation), and the init Job has a 300s TTL (always GC'd after a long halt), a new init Job fires unconditionally — wiping the workspace. BYPASS-02 (clone re-dispatch) is a downstream consequence of the same line-1257 bug: even if BYPASS-01 is fixed (bypass targets `PhaseRunning`), the clone Job check at `project_controller.go:549-571` is currently guarded only by `apierrors.IsNotFound(cloneErr)`, which is true after the clone Job's 300s TTL expires. A durable `Status.Git.CloneComplete` sentinel is needed.

BYPASS-03 (budget double-count) is an independent bug in `handleProjectJobCompletion:1156-1175`. The `isFirstCompletion` flag is set `true` only when the reporter Job does not exist. After a halt long enough for the reporter Job TTL to expire (also 300s, `reporter_jobspec.go:148`), the reporter Job is gone, `isFirstCompletion` becomes `true` again, and `budget.RollUpUsage` fires a second time.

BYPASS-04 (cap-raise re-halts) is in `handleBudgetGate`'s two-branch structure. The bypass branch (lines 1240-1273) consumes the one-shot annotation and returns immediately. The next reconcile has `bypassed=false` and calls `budget.IsCapExceeded(project)` which ORs both `AbsoluteCapCents` and `RollingWindowCapCents` (cap.go:44-57). If only AbsoluteCap was raised, RollingWindowCap still fires at line 1275, immediately re-halting.

BYPASS-05 is the only non-bug requirement: the regression test that locks in the `2a5e0dc` ordering fix. The test already exists in `project_planner_completion_test.go` and is currently GREEN. BYPASS-05 requires verifying the test is in the standard suite and adds a second assertion scenario: the "TTL-GC'd planner Job" path (nil Job to `handleProjectJobCompletion`) also spawns the reporter and rolls up cost.

**Primary recommendation:** Fix BYPASS-01 (one-line change: `PhasePending` → `PhaseRunning`), add `Status.Git.CloneComplete` guard (BYPASS-02), replace `isFirstCompletion` with `PlannerRolledUpUID` marker (BYPASS-03), evaluate both caps together before halting (BYPASS-04), verify/extend QQH-01 envtest coverage (BYPASS-05). No new packages. No chart changes. CRD manifest regen required for the two new status fields.

## User Constraints (from ROADMAP — no CONTEXT.md for this phase)

All five success criteria are locked. Verbatim from ROADMAP:

1. Clearing a budget halt resumes the project at `Running`, not `Pending` — no workspace re-init / re-clone Job fires when `Status.Git.BranchName` is already set.
2. A resume never re-dispatches the clone Job when the workspace is already initialized — the guard is a durable `CloneComplete` status flag, not reporter-Job existence (TTL-GC-safe).
3. Planning cost rolls up exactly once across a halt→resume cycle — a durable `PlannerRolledUpUID` marker prevents double-count when the reporter Job has been garbage-collected during a halt.
4. Raising the absolute budget cap alone clears a budget halt without the rolling-window cap immediately re-halting dispatch (both cap values evaluated together before halting resumes).
5. An envtest asserts that when the planner Job completes, the reporter Job spawns AND the planner cost rolls up while the planner Job still exists — locking in the `2a5e0dc` ordering fix against regression.

**Out of scope:** Import path (Phase 28), operator tooling (Phase 29), chart/values.yaml changes, import-path rename.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BYPASS-01 | Bypass-clear resumes at `Running`, not `Pending` | Root cause at `project_controller.go:1257`; fix is one line; guard on `Status.Git.BranchName` |
| BYPASS-02 | Resume never re-runs init/clone Jobs when workspace initialized | Clone dispatch guard at `project_controller.go:549-571` needs `!project.Status.Git.CloneComplete` sentinel; new `GitStatus.CloneComplete bool` field |
| BYPASS-03 | Planner cost rolled up exactly once across halt→resume | `isFirstCompletion` guard at `project_controller.go:1156-1175` uses reporter Job existence (unreliable after TTL); new `BudgetStatus.PlannerRolledUpUID string` field |
| BYPASS-04 | Raising absolute cap alone clears halt without rolling-window re-halt | `budget.IsCapExceeded` evaluates caps independently; fix: in bypass path, re-evaluate ONLY the cap being bypassed or skip re-halt when active bypass annotation present |
| BYPASS-05 | Envtest regression for `2a5e0dc` ordering fix | Test at `project_planner_completion_test.go` already covers the case; verify it's in the suite; add TTL-GC path scenario |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Budget halt / bypass annotation handling | Controller (ProjectReconciler) | Budget package (budget/cap.go) | Controller owns state machine; budget package is a pure helper |
| Clone Job idempotency guard | Controller (ProjectReconciler) | CRD Status (GitStatus) | Guard must survive controller restart; durable in `.status` |
| Planner cost rollup idempotency | Controller (ProjectReconciler) | CRD Status (BudgetStatus) | ditto — must survive TTL-GC of the reporter Job |
| CRD schema additions | API package (api/v1alpha2) | Generated manifests (config/crd/bases) | Source is struct tags; manifests derived by `make manifests` |
| Regression tests | Controller test package | Suite test helpers | Ginkgo envtest in `internal/controller/` |

## Standard Stack

### Core (existing — no new additions)

| Component | Version / Location | Purpose |
|-----------|-------------------|---------|
| controller-runtime | v0.24.1 (go.mod) | Reconcile loop, status patch, `meta.SetStatusCondition` |
| kubebuilder struct tags | `+kubebuilder:validation:*` | CRD schema generation; `+optional` for new fields |
| `internal/budget/cap.go` | current HEAD | `IsCapExceeded`, `IsBypassed`, `ConsumeBypass` |
| `internal/controller/project_controller.go` | current HEAD | `handleBudgetGate`, `reconcileProjectPhase2`, `handleProjectJobCompletion`, `reconcilePhase3Lifecycle` |
| `api/v1alpha2/project_types.go` | current HEAD | `GitStatus`, `BudgetStatus`, `ProjectStatus` |
| Ginkgo v2 / Gomega | existing test deps | envtest suite pattern |

No new packages. No `go.mod` changes. [VERIFIED: live codebase grep]

## Package Legitimacy Audit

Not applicable — this phase installs zero external packages. All implementation uses existing packages already in `go.mod`.

## Architecture Patterns

### System Architecture Diagram

```
Operator applies bypass annotation (tideproject.k8s/bypass-budget=true)
        |
        v
ProjectReconciler.Reconcile()
  --> reconcileProjectPhase2()
        --> handleBudgetGate()    [project_controller.go:1227]
              BudgetExceeded && bypassed?
                YES --> consume annotation
                        FIX BYPASS-01: set Phase=Running (not Pending)
                        clear ConditionBudgetExceeded
                        FIX BYPASS-04: skip re-halt check since actively bypassed
                        return (requeue)
                        |
        --> (next reconcile, Phase=Running)
              reconcilePhase3Lifecycle()
                --> Step 1: BranchName != "" → skip branch init  [line 502]
                --> Step 3: Clone Job dispatch
                      FIX BYPASS-02: CloneComplete? YES → skip dispatch
                    |
        --> reconcileProjectPlannerDispatch()
              Phase=Running + Job GC'd → handleProjectJobCompletion(nil)
                --> spawn reporter Job (idempotent, AlreadyExists=ok)
                --> FIX BYPASS-03: PlannerRolledUpUID == jobName? NO → rollup; record UID
                                                                  YES → skip rollup
```

### Recommended Project Structure

No structural changes. All modifications are within existing files:

```
internal/controller/
├── project_controller.go        # BYPASS-01/02/03/04 fixes
internal/budget/
├── cap.go                       # BYPASS-04: IsCapExceeded re-evaluation logic (possible)
api/v1alpha2/
├── project_types.go             # BYPASS-02: GitStatus.CloneComplete
│                                # BYPASS-03: BudgetStatus.PlannerRolledUpUID
config/crd/bases/
├── tideproject.k8s_projects.yaml  # regenerated by make manifests
api/v1alpha2/
├── zz_generated.deepcopy.go     # regenerated by make generate
internal/controller/
├── project_planner_completion_test.go  # BYPASS-05: verify/extend
├── project_controller_test.go   # BYPASS-01: add assertion for target phase
```

### Pattern 1: Phase sentinel guard for already-initialized projects

The correct fix for BYPASS-01 is two parts:

**Part A — Fix the bypass target phase** (`project_controller.go:1257`):

```go
// BEFORE (bug):
project.Status.Phase = tidev1alpha2.PhasePending

// AFTER (fix):
if project.Status.Git.BranchName != "" {
    project.Status.Phase = tidev1alpha2.PhaseRunning
} else {
    project.Status.Phase = tidev1alpha2.PhasePending
}
```

`BranchName` is set at `reconcilePhase3Lifecycle:502-504` and never cleared. It is the reliable sentinel that the project has advanced past init. [VERIFIED: live codebase grep of `project.Status.Git.BranchName`]

**Part B — Optional belt-and-suspenders on init Job dispatch** (`reconcileProjectPhase2:338-351`):

```go
// Guard: if workspace already initialized, skip init-Job dispatch.
if project.Status.Git.BranchName != "" {
    // Already past init — skip init Job dispatch entirely.
    return r.reconcilePhase3Lifecycle(ctx, project)
}
```

This makes a second init-Job structurally impossible even if Part A is wrong. [ASSUMED — pattern derived from code reading; no prior art in this specific function]

### Pattern 2: Durable CloneComplete sentinel (BYPASS-02)

New field in `api/v1alpha2/project_types.go` `GitStatus`:

```go
// CloneComplete is set to true when the clone Job completes successfully.
// Unlike Job existence (which TTL-GCs after 300s), this flag is durable in
// .status and survives a halt→resume cycle. Used as the idempotency guard
// for clone Job dispatch (BYPASS-02 / Phase 27).
// +optional
CloneComplete bool `json:"cloneComplete,omitempty"`
```

Guard in `reconcilePhase3Lifecycle:555` (before clone Job creation):

```go
// BEFORE (bug — TTL-GC unreliable after long halt):
if apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
    // dispatch clone Job

// AFTER (fix — durable flag):
if !project.Status.Git.CloneComplete && apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
    // dispatch clone Job
```

Also: set `CloneComplete=true` when the clone Job succeeds (needs a `handleCloneJobCompletion` step or equivalent — check where clone success is currently detected in `reconcilePhase3Lifecycle`). [ASSUMED — pattern shape is clear; exact location of clone-success detection needs a follow-up grep in the clone job completion path]

### Pattern 3: PlannerRolledUpUID durable marker (BYPASS-03)

New field in `api/v1alpha2/project_types.go` `BudgetStatus`:

```go
// PlannerRolledUpUID is the name of the planner Job whose Usage was last
// successfully rolled up into CostSpentCents. Used to prevent double-counting
// when the reporter Job has TTL-GC'd during a halt→resume cycle (BYPASS-03).
// Set after a successful budget.RollUpUsage call; checked before rolling up.
// +optional
PlannerRolledUpUID string `json:"plannerRolledUpUID,omitempty"`
```

Guard in `handleProjectJobCompletion:1177-1182`:

```go
// BEFORE (bug):
if isFirstCompletion && envReadOK {
    budget.RollUpUsage(ctx, r.Client, project, out.Usage)
}

// AFTER (fix):
if isFirstCompletion && envReadOK {
    if project.Status.Budget.PlannerRolledUpUID != jobName {
        budget.RollUpUsage(ctx, r.Client, project, out.Usage)
        // Patch PlannerRolledUpUID = jobName (idempotent next check)
    }
}
```

`jobName` for the project planner is `tide-project-<uid>-1` (deterministic, set at `project_controller.go:955`). [VERIFIED: line 955 `jobName := fmt.Sprintf("tide-project-%s-1", project.UID)`]

### Pattern 4: BYPASS-04 — prevent rolling cap from immediately re-halting

The current flow after bypass:

1. Bypass fires: consume annotation, set Phase (fixed by BYPASS-01), return `ctrl.Result{}, nil`.
2. Next reconcile: `IsBypassed=false` (annotation consumed), `IsCapExceeded` re-evaluates BOTH caps.
3. If RollingWindowCap is still exceeded (operator only raised AbsoluteCap): line 1275 fires immediately, `Phase=BudgetExceeded` again.

Two viable fix shapes:

**Fix A (preferred) — TTL bypass form as default:** The `bypass-budget-until=<RFC3339>` annotation is not consumed immediately (only the one-shot `bypass-budget=true` is). While the TTL is active, `IsBypassed=true` on every reconcile, so line 1275's `!bypassed` guard never fires. The operator can use `bypass-budget-until=<time far enough away>` to suppress both caps during the raise. Document this as the operator-recommended ergonomic; add a clarifying condition message when which cap was exceeded.

**Fix B — evaluate caps jointly, not independently:** The bypass path in `handleBudgetGate` could re-evaluate `IsCapExceeded` AFTER the bypass consumes the annotation, and only set Phase=BudgetExceeded in the SAME reconcile if the cap is still exceeded without bypass. But this changes `IsCapExceeded` semantics and may affect TaskReconciler (which also calls it).

Fix A is surgical and requires no logic changes to the bypass path itself; it adds a clarifying Event/condition message for WHICH cap fired. Fix B requires more thought about call-site impact. The planner should choose Fix A as the primary approach and document Fix B as a follow-up. [ASSUMED — exact fix shape is a planning decision; code analysis confirms the root cause]

### Anti-Patterns to Avoid

- **Checking reporter Job existence as a rollup idempotency signal** (R-04): TTL-GC makes this unreliable. Always use the durable `PlannerRolledUpUID` marker.
- **Using `Status.Phase=PhasePending` as the bypass target** (BYPASS-01 root cause): `PhasePending` re-enters the init Job dispatch path; `PhaseRunning` skips it.
- **Clearing `PlannerRolledUpUID` on bypass** (BYPASS-03): it must persist across halt→resume; clearing it causes double-count on the next resume.
- **Adding CRD fields as `required`** (R-14): Phase 27 CRD additions must be `+optional` with `omitempty` to be backward-compatible. Zero-value `false` for `CloneComplete` and `""` for `PlannerRolledUpUID` are safe defaults (pre-fix projects simply re-dispatch on resume, which is the current behavior).
- **Skipping `make manifests && make generate`** after adding fields to `api/v1alpha2/project_types.go`: the CRD YAML and `zz_generated.deepcopy.go` will be stale; envtest loads from `config/crd/bases` and will fail with wrong schema.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Durable status update after rollup | Custom status cache | `client.MergeFrom` + `r.Status().Patch` | Existing idempotent patch pattern used throughout controller |
| Detecting which cap fired | Custom audit log | Add `Reason` field to `ConditionBudgetExceeded` condition | Already follows metav1.Condition pattern; `Reason` string distinguishes "AbsoluteCapReached" vs "RollingWindowCapReached" |
| Clone Job TTL | Extending TTL in buildCloneJob | `Status.Git.CloneComplete` durable flag | TTL extension is fragile; durable status survives any halt duration |
| Envtest helper for budget halt | New helper | `stampBudgetSpend` (existing, `budget_blocked_regression_test.go:51`) + `makeFakeJobTerminal` (existing, `milestone_controller_test.go:64`) | Both helpers already exist and are used in neighboring tests |

## Code Investigation Findings

### BYPASS-01: Exact code path

`handleBudgetGate` is called from `reconcileProjectPhase2:314`. It is the FIRST step in Phase 2. After returning, the caller checks `if project.Status.Phase == tidev1alpha2.PhaseBudgetExceeded { return result, nil }` at line 319. Since bypass sets Phase to `PhasePending`, this check does NOT early-return. The function falls through to:

- Step 2: PVC bind check (succeeds — PVC already bound)
- Step 3: Init Job check at line 339: `r.Get(initJobName)` — returns `NotFound` (TTL-GC'd after 300s) — `ensureInitJob` is called — NEW INIT JOB CREATED

The re-init loop is NOT stopped by the idempotency guard at `handleInitJobCompletion:385-402` because that guard checks `project.Status.Phase` BEFORE patching, and `Phase=Pending` is not in the skip-list at line 395-402 (which only skips `Running`, `Complete`, `PushLeaseFailed`, `PushLeakBlocked`).

**Key code citations:**
- `project_controller.go:1257` — target phase bug (`PhasePending` → should be `PhaseRunning`)
- `project_controller.go:309-365` — `reconcileProjectPhase2` step sequence
- `project_controller.go:338-351` — init Job dispatch — no BranchName guard
- `project_controller.go:1308` — init Job TTL = 300s
- `project_controller.go:502-509` — `BranchName` set in `reconcilePhase3Lifecycle`
- `api/v1alpha2/project_types.go:234-250` — `GitStatus` struct (no `CloneComplete` field today)

### BYPASS-02: Clone Job dispatch guard

After BYPASS-01 is fixed (bypass targets `Running`), `reconcilePhase3Lifecycle` is entered. Clone Job dispatch is at `project_controller.go:547-572`:

```go
cloneJobName := fmt.Sprintf("tide-clone-%s", project.UID)
var existingClone batchv1.Job
cloneErr := r.Get(ctx, ..., &existingClone)
if apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
    // ... build and Create clone Job
```

The ONLY guard is `IsNotFound(cloneErr)`. Clone Job TTL is 300s (line 212 in push_helpers.go). After a long halt, the clone Job is gone, so `IsNotFound=true`, and a new clone Job fires into an already-initialized workspace. `git clone` fails with "destination path already exists" → project stalls.

**Fix:** Add `!project.Status.Git.CloneComplete` as a pre-condition. Set `CloneComplete=true` at clone Job success detection (find where clone Job success is handled — needs a targeted grep of clone success path, as this is not in the current research).

**Key code citations:**
- `project_controller.go:549-571` — clone Job dispatch (no `CloneComplete` guard)
- `internal/controller/push_helpers.go:212` — clone Job TTL = 300s [VERIFIED]
- `api/v1alpha2/project_types.go:234` — `GitStatus` struct needs `CloneComplete bool` field

### BYPASS-03: Planner rollup double-count

`handleProjectJobCompletion:1145-1174` sets `isFirstCompletion=true` when:
1. `r.ReporterImage == ""` (no reporter configured — test path only)
2. `r.Get(reporterJobName)` returns `IsNotFound`

After a halt where the reporter Job is GC'd (TTL = 300s, `reporter_jobspec.go:148`), condition 2 is always true. The rollup fires at line 1178-1182:

```go
if isFirstCompletion && envReadOK {
    budget.RollUpUsage(ctx, r.Client, project, out.Usage)
}
```

No durable marker prevents double-count. `BudgetStatus.PlannerRolledUpUID` (new field) must be checked before rolling up and set after rolling up.

**Key code citations:**
- `project_controller.go:1145-1175` — `isFirstCompletion` logic (bug: reporter Job existence)
- `project_controller.go:1178-1182` — rollup call
- `project_controller.go:955` — `jobName := fmt.Sprintf("tide-project-%s-1", project.UID)` — use as rollup marker key
- `internal/controller/reporter_jobspec.go:148` — reporter Job TTL = 300s [VERIFIED: `ttlVal := int32(300)`]
- `api/v1alpha2/project_types.go:257-269` — `BudgetStatus` struct needs `PlannerRolledUpUID string` field

### BYPASS-04: Dual cap re-halt

`budget.IsCapExceeded` (`internal/budget/cap.go:44-57`) evaluates two OR-conditions:

```go
if project.Spec.Budget.AbsoluteCapCents > 0 && project.Status.Budget.CostSpentCents > project.Spec.Budget.AbsoluteCapCents {
    return true
}
if project.Spec.Budget.RollingWindowCapCents > 0 && project.Status.Budget.CostSpentCents > project.Spec.Budget.RollingWindowCapCents {
    return true
}
```

After bypass clears the halt, the next reconcile calls `handleBudgetGate` with `bypassed=false` (one-shot annotation consumed). `capExceeded=IsCapExceeded(project)` — if `RollingWindowCapCents` is still exceeded, `capExceeded=true`. Line 1275: `Phase != BudgetExceeded && capExceeded && !bypassed` → immediate re-halt.

The operator must raise BOTH caps, or use the TTL bypass form (`bypass-budget-until`). The TTL bypass keeps `IsBypassed=true` on every reconcile for the specified duration; the re-halt guard at line 1275 checks `!bypassed`, so re-halt is suppressed for the bypass duration, giving the operator time to raise both caps.

Fix approach: in the bypass path's return annotation, emit a structured Event/condition specifying WHICH cap fired (AbsoluteCap vs RollingCap) with the current spend and both cap values. This tells the operator exactly what to raise. Separately, document the TTL bypass form as the default operator ergonomic. No logic change to `IsCapExceeded` needed.

**Key code citations:**
- `internal/budget/cap.go:44-57` — `IsCapExceeded` (OR of both caps)
- `project_controller.go:1237-1238` — `bypassed` and `capExceeded` computed
- `project_controller.go:1275` — re-halt condition: `!BudgetExceeded && capExceeded && !bypassed`
- `project_controller.go:1280-1284` — re-halt message only cites `AbsoluteCapCents` (does NOT distinguish which cap fired) — this is the observability gap BYPASS-04 must address

### BYPASS-05: QQH-01 regression test status

The test file `project_planner_completion_test.go` was authored in the QQH-01 quick task (commit `9df95fe` RED test + commit `2a5e0dc` fix). The test currently:

- Asserts reporter Job spawns when planner Job is terminal AND still present (not GC'd)
- Asserts `CostSpentCents` reflects planner spend in the same reconcile
- Has a control spec proving no reporter / no budget when Job is non-terminal

The test is in the `internal/controller` package with `Label("envtest")` — it runs under `make test-int-fast` (Layer A envtest). [VERIFIED: grep of label + test file structure]

BYPASS-05's requirement says "locks in the ordering fix against regression." The existing test already covers the primary scenario. The BYPASS-05 task should:

1. Confirm the test passes in the current HEAD (run it, check the pass)
2. Add a companion spec for the TTL-GC path: when `completedJob == nil` (planner Job already GC'd), `handleProjectJobCompletion(nil)` is called, and the reporter + budget still fire. This path is currently exercised by the reconcile fallthrough at `project_controller.go:972`, but NOT explicitly tested with a second `Describe` block in the QQH-01 file.

**Key code citations:**
- `project_planner_completion_test.go` — existing QQH-01 test (GREEN at HEAD)
- `project_controller.go:969-972` — TTL/GC fallthrough: `handleProjectJobCompletion(ctx, project, nil)`
- `project_controller.go:1117` — `handleProjectJobCompletion` signature: `completedJob *batchv1.Job` (nil-safe)

## CRD Schema Additions (BYPASS-02 and BYPASS-03)

Both new fields go in `api/v1alpha2/project_types.go`. Both must be `+optional` with `omitempty`. No new types required.

**`GitStatus` addition (BYPASS-02):**
```go
// CloneComplete is true when the clone Job completed successfully.
// This durable flag gates clone Job re-dispatch on resume, replacing the
// TTL-unreliable Job-existence check (BYPASS-02 / Phase 27).
// +optional
CloneComplete bool `json:"cloneComplete,omitempty"`
```

**`BudgetStatus` addition (BYPASS-03):**
```go
// PlannerRolledUpUID is the name of the most recent planner Job whose Usage
// was successfully rolled up into CostSpentCents. Prevents double-counting
// when the reporter Job has TTL-GC'd during a halt→resume cycle (BYPASS-03 / Phase 27).
// +optional
PlannerRolledUpUID string `json:"plannerRolledUpUID,omitempty"`
```

**Regen steps (always two steps, always both required):**

```bash
make manifests   # regenerates config/crd/bases/tideproject.k8s_projects.yaml
make generate    # regenerates api/v1alpha2/zz_generated.deepcopy.go
```

Both steps must be committed with the type changes. Envtest reads CRD YAML from `config/crd/bases` at startup; stale YAML produces wrong-schema validation errors at test time. [VERIFIED: Makefile targets at lines 52-70]

**Schema bump:** No `APIVersionV1Alpha1` constant bump required. New fields are `omitempty` optional fields only. Existing envelopes that don't set these fields produce safe zero values (`false`, `""`), which are the correct pre-fix defaults. [VERIFIED: envelope version constant at `pkg/dispatch/envelope.go:24`; PITFALLS.md R-14]

## Runtime State Inventory

Not applicable (code/status change only; no stored data migration needed).

New `CloneComplete` field defaults to `false` (zero value) for all existing Projects in etcd. On their next reconcile, `!CloneComplete` is `true`, which means the clone guard passes through — but the existing `IsNotFound(cloneErr)` check still gates the actual Job creation. Projects whose clone Job already GC'd will have `CloneComplete=false` at the time of a bypass resume, meaning the fix at Step 3 correctly dispatches a new clone Job (the workspace is STILL initialized because the init Job ran previously — `BranchName` is set). Wait — this is subtle: BYPASS-02 says "never re-runs the init or clone Jobs when the workspace is already initialized." After adding `CloneComplete`, the controller needs to SET it to `true` when the clone succeeds. For existing pre-fix Projects that have already cloned successfully, `CloneComplete` will be `false` until the next bypass+resume cycle. However, for the INITIAL successful clone run (not a resume), `CloneComplete` gets set to `true` on clone success, and all future resumes are safe. The migration path is: no data migration needed; existing projects in a "running normally" state have their clone Job succeeded in-flight and not GC'd; the flag gets set on next completion observation.

## Common Pitfalls

### Pitfall 1: Forgetting `make generate` after adding fields to `GitStatus`

`zz_generated.deepcopy.go` contains the `DeepCopyInto` for `GitStatus`. If a new field (`CloneComplete bool`) is added without regenerating, `deepcopy.go` omits copying the field, so `client.MergeFrom(project.DeepCopy())` patches produce a `false` zero value on every reconcile, clearing the sentinel.

**Prevention:** Run both `make manifests && make generate` and commit both generated files with the type changes. CI's `make test` runs both before test execution; a CI failure confirms stale generated files.

### Pitfall 2: Setting `PlannerRolledUpUID` AFTER a failed rollup

If `budget.RollUpUsage` fails (non-fatal, logged), the current code continues. If `PlannerRolledUpUID` is set regardless of rollup success, the next reconcile skips the rollup even though it never actually ran. The marker must be set ONLY on rollup success.

**Prevention:** Set `PlannerRolledUpUID` only inside the `if rollErr == nil` branch of the rollup call. On failure, leave the field unset so the next reconcile retries.

### Pitfall 3: `CloneComplete` set before clone Job success is confirmed

If `CloneComplete=true` is set optimistically (at clone Job Create, not at clone Job Succeed), a clone Job that fails leaves the workspace incomplete but the sentinel says "done." Future bypass resumes skip the clone, but the workspace is corrupted.

**Prevention:** Set `CloneComplete=true` only in the clone Job completion handler, after `isJobSucceeded(cloneJob)` returns true. Find the exact location of clone completion handling (likely in `reconcilePhase3Lifecycle` or a dedicated `handleCloneJobCompletion` — needs targeted grep; the current codebase may not have an explicit clone-success handler separate from the PVC-layout check).

### Pitfall 4: BYPASS-04 fix changes IsCapExceeded semantics for TaskReconciler

`TaskReconciler` calls `budget.IsCapExceeded(project)` at the dispatch gate (Phase 14 BUDGET-02/D-04). Any change to `IsCapExceeded` affects both reconcilers. Prefer adding the bypass-aware check in `handleBudgetGate` only (Project-reconciler path), not in `IsCapExceeded` itself.

**Prevention:** Keep `IsCapExceeded` unchanged (evaluates both caps unconditionally). Add the bypass-time ergonomics guidance only in the condition message emitted at halt time and in documentation.

### Pitfall 5: Existing bypass test does not assert target phase

The test `TestProjectReconciler_BypassAnnotation_ClearsBudgetExceeded` at `project_controller_test.go:505` asserts `Phase != "BudgetExceeded"` but does not assert `Phase == "Running"`. This is a test gap that obscures the BYPASS-01 regression. BYPASS-01's fix must add an explicit `Expect(fetched.Status.Phase).To(Equal("Running"))` assertion to this test.

**Code citation:** `project_controller_test.go:555-556` — `Expect(fetched.Status.Phase).NotTo(Equal("BudgetExceeded"), ...)` — missing positive assertion.

## Code Examples

### Existing `mapEnvReader` and `makeFakeJobTerminal` helpers (use directly for new envtest)

```go
// Source: internal/controller/milestone_controller_test.go:64
func makeFakeJobTerminal(ctx context.Context, c client.Client, name, namespace string, succeeded bool) error {
    // patches Job status to terminal without deletion
}

// Source: internal/controller/suite_test.go:89
func newMapEnvReader() *mapEnvReader {
    return &mapEnvReader{byUID: make(map[string]pkgdispatch.EnvelopeOut), ...}
}
```

Both helpers are already in the test package. New tests for BYPASS-01/02/03 can use them directly without adding new helpers.

### Existing `qqhBuildReconciler` (use as base for bypass tests)

```go
// Source: internal/controller/project_planner_completion_test.go:50
func qqhBuildReconciler(envReader *mapEnvReader) *ProjectReconciler {
    return &ProjectReconciler{
        Client: mgrClient, Scheme: ..., Dispatcher: &stubDispatcher{},
        PlannerPool: newPlannerPoolForTest(), EnvReader: envReader,
        SigningKey: testSigningKey, CredproxyImage: ..., ReporterImage: qqhReporterImg,
        SharedPVCName: qqhPVCName, HelmProviderDefaults: ...,
    }
}
```

Use the same reconciler setup for BYPASS-03 (double-count) test, adding assertions on `Status.Budget.CostSpentCents` before and after a simulated halt+GC+resume cycle.

### `stampBudgetSpend` helper (BYPASS-01/04 tests)

```go
// Source: internal/controller/budget_blocked_regression_test.go:51
func stampBudgetSpend(ctx context.Context, projectName string, spentCents int64) {
    // patches project.Status.Budget.CostSpentCents to simulate spend past cap
}
```

Use this to simulate a halted project before testing the bypass annotation.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (existing) |
| Config file | `internal/controller/suite_test.go` (BeforeSuite wires envtest) |
| Quick run command | `cd internal/controller && go test -run TestControllers -v -timeout 5m .` |
| Full suite command | `make test-int-fast` (Layer A envtest, ~90s) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File |
|--------|----------|-----------|-------------------|------|
| BYPASS-01 | Bypass sets `Phase=Running`, not `Pending`; no new init Job | envtest | `make test-int-fast` | `project_controller_test.go` — extend existing bypass test |
| BYPASS-02 | `CloneComplete=true` gates re-clone on resume | envtest | `make test-int-fast` | new spec in `project_phase3_test.go` or new file `project_clone_idempotency_test.go` |
| BYPASS-03 | `PlannerRolledUpUID` prevents double-count when reporter GC'd | envtest | `make test-int-fast` | new spec in `project_planner_completion_test.go` |
| BYPASS-04 | Re-halt condition not fired when bypass TTL active; condition message names which cap | unit | `go test ./internal/budget/ -run TestIsCapExceeded` | extend `internal/budget/` tests OR new spec in `project_controller_test.go` |
| BYPASS-05 | Reporter spawns + budget rolls up while planner Job still exists | envtest (exists) | `make test-int-fast` | `project_planner_completion_test.go` — verify GREEN + add TTL-GC scenario |

### Sampling Rate

- **Per task commit:** `cd internal/controller && go test -run TestControllers -v -timeout 5m . -ginkgo.focus="BYPASS"` (focus tag on new specs)
- **Per wave merge:** `make test-int-fast`
- **Phase gate:** `make test-int-fast` green before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] New test for BYPASS-02 (`project_clone_idempotency_test.go` or new spec in `project_phase3_test.go`) — covers REQ BYPASS-02
- [ ] New spec for BYPASS-03 TTL-GC double-count in `project_planner_completion_test.go` — covers REQ BYPASS-03
- [ ] Extended assertion in existing bypass test at `project_controller_test.go:505` — BYPASS-01 positive `Phase==Running` check
- [ ] TTL-GC path spec in `project_planner_completion_test.go` — BYPASS-05 companion scenario

No framework install needed — envtest infrastructure already in place.

## Security Domain

No new security surface in this phase. Changes are:
- Status field additions (readable only via K8s RBAC; no new permissions needed)
- Logic fixes to existing annotation-driven bypass path (same annotation as before)
- No new external inputs, no new exec surfaces, no new PVC paths

V5 input validation applies only to `PlannerRolledUpUID` (a Job name string): validate that the assigned value matches the expected pattern `tide-project-<uid>-1` before storing. [ASSUMED — standard defensive programming; no ASVS-specific requirement triggers]

## Environment Availability

Step 2.6: SKIPPED (no external dependencies — code/schema-only changes).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | BYPASS-01 fix Part B (BranchName guard at Step 3 of reconcileProjectPhase2) is belt-and-suspenders | Architecture Patterns / Pattern 1 | If wrong, init-Job guard relies solely on BYPASS-01 Phase fix being correct |
| A2 | Fix A (TTL bypass form) is the preferred BYPASS-04 ergonomic; no logic change to IsCapExceeded | Architecture Patterns / Pattern 4 | If wrong, a logic change to IsCapExceeded is needed, which affects TaskReconciler |
| A3 | Clone success is detectable in reconcilePhase3Lifecycle or a named handler near line 550-579 | BYPASS-02 investigation | If wrong, finding clone success detection requires another grep pass |
| A4 | `PlannerRolledUpUID` set to job name (not job UID) is the right marker key | Code Examples | If wrong, the deterministic job name `tide-project-<uid>-1` collision risk is zero, so either key works |

## Open Questions (RESOLVED)

> Both questions are implementation-detail and are resolved within the Phase 27 plans:
> Q1 → 27-02 Task 2 (executor checks `existingClone.Status.Succeeded > 0` in the found-Job
> branch after line 571 and sets `CloneComplete=true`). Q2 → 27-03 Task 2 (new `Describe`
> block added to the existing `project_planner_completion_test.go`).

1. **Where is clone Job success currently detected?** [RESOLVED — 27-02 Task 2]
   - What we know: clone Job is dispatched at `project_controller.go:549-571`; successful result patches `BranchName` at line 502 (set before clone dispatch, not after)
   - What's unclear: there is no explicit "clone succeeded" handler visible in `reconcilePhase3Lifecycle:470-579`. Clone success may be detected by checking `existingClone.Status.Succeeded > 0` on subsequent reconciles — needs targeted grep of `existingClone.Status` usage after the clone dispatch block
   - Recommendation: Add a targeted grep of `existingClone` after line 571 in the first plan-implementation task; or read `reconcilePhase3Lifecycle` fully (lines 470-579 were read only partially)

2. **Should BYPASS-05 add a new `Describe` block in `project_planner_completion_test.go` or a new file?**
   - What we know: the file already has two `Describe` blocks (primary + control); a third "TTL-GC nil Job" `Describe` is the natural fit
   - What's unclear: whether the planner wants a separate file for cleaner blame separation
   - Recommendation: Add the new spec to the existing file with a clear comment referencing BYPASS-05

## Sources

### Primary (HIGH confidence — live codebase, directly read)

- `internal/controller/project_controller.go` — full read of lines 240-1298 (reconcile loop, `handleBudgetGate`, `reconcileProjectPhase2`, `reconcilePhase3Lifecycle`, `reconcileProjectPlannerDispatch`, `handleProjectJobCompletion`, `buildInitJob`)
- `internal/budget/cap.go` — full read (lines 1-106: `IsCapExceeded`, `IsBypassed`, `ConsumeBypass`)
- `api/v1alpha2/project_types.go` — read of `GitStatus` (234-250), `BudgetStatus` (252-269), `ProjectStatus` (420-447), phase constants (388-416)
- `internal/controller/project_planner_completion_test.go` — full read (QQH-01 test, confirms BYPASS-05 coverage exists)
- `internal/controller/project_controller_test.go` — read of bypass test at lines 505-560 (confirms BYPASS-01 test gap)
- `internal/controller/milestone_controller_test.go:38-95` — `newPlannerPoolForTest`, `makeFakeJobTerminal` helpers
- `internal/controller/suite_test.go:82-113` — `mapEnvReader`, `newMapEnvReader`, `testSigningKey`
- `internal/controller/budget_blocked_regression_test.go:51-66` — `stampBudgetSpend` helper
- `internal/controller/reporter_jobspec.go:146-175` — reporter Job TTL = 300s (`ttlVal := int32(300)`)
- `internal/dispatch/podjob/jobspec.go:72-73` — planner Job TTL = 600s (`DefaultTTLSecondsAfterFinished = 600`)
- `.planning/REQUIREMENTS.md` — BYPASS-01..05 requirements
- `.planning/STATE.md` — quick task 260617-qqh, commit 2a5e0dc
- `.planning/research/PITFALLS.md` — R-01..R-14 with code citations
- `.planning/research/SUMMARY.md` — milestone summary and phase structure
- `Makefile:52-70` — `make manifests` and `make generate` commands
- `git show 2a5e0dc` — ordering fix commit (confirms files changed and intent)

### Secondary (MEDIUM confidence — prior research)

- `.planning/research/ARCHITECTURE.md` — import architecture context (Phase 28, not Phase 27; read for background only)

## Metadata

**Confidence breakdown:**
- BYPASS-01 root cause: HIGH — line 1257 confirmed directly; reconcile flow traced end-to-end
- BYPASS-02 root cause: HIGH — clone dispatch guard at line 549-571 confirmed; TTL confirmed
- BYPASS-03 root cause: HIGH — `isFirstCompletion` logic at lines 1145-1175 confirmed; reporter TTL confirmed
- BYPASS-04 root cause: HIGH — `IsCapExceeded` logic confirmed; re-halt condition at line 1275 confirmed
- BYPASS-05 test status: HIGH — test file read in full; existing test confirmed GREEN per commit 2a5e0dc
- Fix shapes: MEDIUM (A1, A2 — implementation details of fixes, not the root causes)

**Research date:** 2026-06-18
**Valid until:** 2026-07-18 (stable controller code; unlikely to drift)
