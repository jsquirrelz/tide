---
name: phase-41-refactoring-review
description: Internal refactoring review (12 items) — the non-breaking cleanup track; candidate Phase 41
type: phase-seed
captured: 2026-07-09
source: operator-shared refactoring review (generated against current source, file:line-verified)
relates_to:
  - phase-40 (v1alpha removal + semantic rename — the migration track this review defers to)
  - STAGE-02 (subagent.levels rename; REQUIREMENTS.md Future Requirements)
provenance: >
  Shared by the operator 2026-07-09 during v1.0.7 close-out. The Phase 40/41 planning
  artifacts were authored on the operator's other machine and never pushed to this origin
  (confirmed absent from every worktree/branch/stash/reflog/dangling commit here). This file
  is the in-repo capture so the refactoring track is tracked HERE and not stranded.
---

# Phase 41 (candidate) — TIDE Refactoring Review

**Scope:** `internal/controller`, `internal/*` support packages, `api/`, `cmd/`, and test structure. Generated files, PROJECT, and `.planning/` artifacts excluded per AGENTS.md. All findings verified against current source with file:line evidence. No edits made.

**Overall impression:** the codebase is unusually well-documented and already extracts shared logic deliberately (`dispatch_helpers.go`, `boundary_push.go`, `git_writer.go`, `internal/gates`, `internal/finalizer`). The opportunities are mostly about finishing those extractions and tightening conventions — not restructuring.

## Quick wins (low risk, high clarity payoff)

### 1. Typed Status.Phase constants for Milestone/Phase/Plan/Task/Wave
- **Files:** `api/v1alpha2/{milestone,phase,plan,task,wave}_types.go`; ~90 literal sites across `internal/controller/*.go`, `cmd/tide/{resume,approve}.go`, `cmd/dashboard/api/projects.go`, tests.
- **Problem:** `Status.Phase` is an untyped string on all six kinds. Project already has constants (`project_types.go:436-457` — `PhaseInitialized`, `PhaseRunning`, …), but every other kind compares/assigns raw literals: `"Succeeded"`, `"Failed"`, `"Running"`, `"AwaitingApproval"`, `"Pending"`, `"ZeroMembers"`. A typo (`"Succeded"`) compiles silently and breaks indegree math, gate holds, and CLI behavior.
- **Shape:** add string constants (e.g. `LevelPhaseSucceeded = "Succeeded"`) to `api/v1alpha2` and mechanically replace literals. Keep the field type `string` (no CRD schema change). Optionally add `+kubebuilder:validation:Enum` later as a separate change.
- **Risk:** Low (pure literal substitution). **Verify:** `go build ./...`, `go test ./internal/controller/... ./cmd/tide/...`, `rg '"Succeeded"' internal/controller cmd`.

### 2. Replace hand-rolled condition loops with meta.IsStatusConditionTrue
- **Files:** `internal/controller/billing_halt.go:78-89`, `failure_halt.go:56-67`, `budget_blocked.go:~59`, plus inline "already halted" loop at `failure_halt.go:93-98`.
- **Problem:** three helpers manually iterate `project.Status.Conditions` to test `Type==X && Status==True`; apimachinery's `meta.IsStatusConditionTrue` does exactly this.
- **Shape:** keep the nil-safe wrappers (`checkBillingHalt(project)` etc. — the nil-project guard is load-bearing), replace the loop body with `meta.IsStatusConditionTrue(...)`.
- **Risk:** Low. **Verify:** `go test ./internal/controller/... -run 'Halt|Budget'`.

### 3. Fix stale scheme comment + duplicated AddToScheme in cmd/manager/main.go
- **Files:** `cmd/manager/main.go:303-311`.
- **Problem:** the comment says v1alpha1 remains registered so the manager can decode surviving v1alpha1 objects, but the code calls `tidev1alpha2.AddToScheme(scheme)` **twice** and never registers v1alpha1. The comment actively misleads.
- **Shape:** delete the duplicate line; correct the comment to what actually happens (`checkSchemaRevisionGuard` reads the served v1alpha2 shape). If v1alpha1 decoding is genuinely required, that's a bug to raise — confirm with `project_controller_v2_guard_test.go` first.
- **Risk:** Low. **Verify:** `go test ./internal/controller/... -run V2Guard`, `go build ./cmd/manager`.

### 4. Delete dead code and dead struct fields
- **Files:** `task_controller.go:1379-1438` (`gateDispatch`, `ensureJob`, both `//nolint:unused` "retained per plan grep contract"); `TaskReconcilerDeps.SubagentImage` (`task_controller.go:98`), `MilestoneReconciler.SubagentImage` (`milestone_controller.go:85`), Phase/Plan/Project `.SubagentImage` — "dead since Phase 13" yet still wired in `cmd/manager/main.go` + fixtures; `WaveReconciler.PlannerPool/ExecutorPool/Dispatcher-pools` (`wave_controller.go:58-62`) — never used.
- **Problem:** `ensureJob` duplicates `createDispatchJob`'s Job-build block; drifts when `BuildOptions` grows. The grep-contract phases have shipped.
- **Shape:** confirm the grep contracts are historical (`rg 'ensureJob|gateDispatch' .planning/ --files-with-matches` shows only old plans), then delete the functions + dead `SubagentImage` fields end-to-end (main.go wiring + fixtures).
- **Risk:** Low-Medium (touches wiring; mechanical). **Verify:** `go build ./...`, `go vet ./...`, `go test ./internal/controller/... ./cmd/manager/...`.

### 5. Fix mojibake in comments
- **Files:** `internal/controller/dispatch_helpers.go` (18 lines), `internal/subagent/anthropic/subagent.go` (9 lines).
- **Problem:** corrupted UTF-8 (`â` where em-dashes/arrows should be) — degrades greppability, reads as encoding damage. Comment-only.
- **Shape:** restore `—`/`→`. **Risk:** Low. **Verify:** `rg 'â' --count` returns nothing; `go build ./...`.

### 6. Use apierrors.IsConflict in the test helper; unify the three reconcile*N drivers
- **Files:** `task_controller_test.go:83-112` (`reconcileN`, `isConflict` string-matches "the object has been modified"), `plan_controller_test.go:153` (`reconcilePlanN`), `wave_controller_test.go:146` (`reconcileWaveN`).
- **Problem:** `isConflict` matches error text instead of `apierrors.IsConflict(err)` — brittle. The three N-times drivers are copies differing only in receiver type.
- **Shape:** one generic `reconcileN(r reconcile.Reconciler, name types.NamespacedName, n int)` in a shared `_test.go` helper; swap the string match for `apierrors.IsConflict`.
- **Risk:** Low (test-only). **Verify:** `go test ./internal/controller/...`.

## Larger refactors (worth doing, plan deliberately)

### 7. Extract the shared "dispatch-entry holds" gate chain
- **Files:** `milestone_controller.go:330-380`, `phase_controller.go:322-378`, `plan_controller.go:~340-380`, `task_controller.go:360-458`.
- **Problem:** the ordered gate sequence — `gates.CheckRejected` → `checkParentApproval` → import-pending (`ConditionImportComplete`) → `checkBillingHalt` → `checkFailureHalt` → `checkBudgetBlocked && !budget.IsBypassed` — is duplicated near-verbatim at four dispatch sites, same requeue intervals (5s/30s), same "position BEFORE pool acquire (Pitfall 2)" invariant re-asserted in comments. Phase 28's import hold changed four files; the next gate needs the same 4-way edit; ordering drift is exactly the bug class the comments warn about.
- **Why:** highest-leverage extraction — centralizes an ordering invariant that today exists only as replicated comments; directly serves idempotency/correctness (a missed gate = budget/billing leak).
- **Shape:** single helper in `dispatch_helpers.go`, e.g. `func checkDispatchHolds(ctx, c, project, level, objName) (held bool, result ctrl.Result)` covering project-scoped holds (billing, failure, budget, import). Keep level-specific holds (reject-with-status-patch, parent approval, task reservation headroom) at call sites. Migrate one controller per PR.
- **Risk:** Medium — gate ordering is semantically load-bearing (slot leaks, park-before-acquire). Preserve exact order + requeue values. **Verify:** `go test ./internal/controller/... -run 'Gates|Halt|Budget|Import'`; envtest gates/budget suites; run targeted packages, not repo-wide `make test` in-sandbox.

### 8. Consolidate planner-reconciler dependencies into a PlannerDeps carrier struct
- **Files:** `{milestone,phase,plan,project}_controller.go` struct defs; `cmd/manager/main.go:410-524`.
- **Problem:** the four planner reconcilers each declare the same 9 dispatch-tier fields (`Dispatcher`, `EnvReader`, `SigningKey`, `CredproxyImage`, `TidePushImage`, `ReporterImage`, `HelmProviderDefaults`, `PricingOverridesJSON`, dead `SubagentImage`), wired four times in main.go with repeated "CR-01 fix: Dispatcher must be assigned…" comments. The cascade-8 bug (never-assigned `Dispatcher`) is exactly the failure mode this invites.
- **Why:** `TaskReconcilerDeps` (`task_controller.go:90-122`) already establishes the pattern; extending it makes a forgotten wiring a one-place mistake.
- **Shape:** `PlannerReconcilerDeps` in `dispatch_helpers.go`, embed in the four reconcilers, build once in main.go. `cmd/manager/wiring_test.go` + `wave_dispatcher_wiring_test.go` already lock wiring — extend them.
- **Risk:** Medium (wide, mechanical). **Verify:** `go test ./cmd/manager/... ./internal/controller/...`; grep no reconciler constructed with a zero Deps.

### 9. Normalize ConditionParentUnresolved polarity
- **Files:** `task_controller.go:344-355` (sets `Status=True` when unresolved) vs `milestone_controller.go:924-940` + `phase_controller.go:853-869` (`surfaceParentRefUnresolved` sets `Status=False` when unresolved, Reason `ParentRefNotFound`).
- **Problem:** same condition type carries opposite polarity per controller. `ParentUnresolved=False` means "fine" on a Task, "parent missing" on a Milestone — conditions are the operator API; a type must have one truth semantics.
- **Shape:** pick `True == parent unresolved` (matches the name + Task usage), fix `surfaceParentRefUnresolved`, clear the condition (`False/ParentResolved`) once the parent appears. Visible status change — document + sweep tests/dashboard (`rg ConditionParentUnresolved`).
- **Risk:** Medium (observable status semantics; a bug fix but consumers may assert current behavior). **Verify:** `go test ./internal/controller/... -run 'Parent'`, `parent_unresolved_test.go`, `rg -l ConditionParentUnresolved cmd/dashboard`.

### 10. Extract the duplicated "AwaitingApproval consume-and-resume" and patchXxx* status helpers
- **Files:** `milestone_controller.go:257-284` + `phase_controller.go:251-278` (identical approve-annotation consume + Running/ApprovedByUser two-step; a third copy inside each `handleJobCompletion` at `milestone_controller.go:686-716`); the four-per-level `patch{Milestone,Phase,Plan,Task}{Succeeded,Failed,Rejected,AwaitingApproval}` families (16 near-identical funcs); `countChild{Phases,Plans,Tasks,Milestones}` (namespace-wide List + ownerRef filter, four copies).
- **Problem:** every level-status transition is ~12 lines of `MergeFrom → set Phase → SetStatusCondition → Status().Patch` duplicated with only the message varying. Approval semantics (annotation-consume ordering, the D-04 "never jump to Succeeded past children" invariant) live in three copies that must stay in lockstep.
- **Shape:** small package-level helpers taking `client.Object` + `*[]metav1.Condition` + phase pointer, e.g. `parkAwaitingApproval(...)` and `consumeApproveAndResume(...)`. NOT a generic level-reconciler — just leaf status-mutation primitives, following `triggerBoundaryPush`/`spawnReporterIfNeeded`. Do NOT unify `reconcilePlannerDispatch`/`handleJobCompletion` wholesale — the ~10% that differs (ChildCount gating, rollup markers, fallbacks) encodes cascade fixes; extract shared leaves first and let bodies shrink.
- **Risk:** Medium. **Verify:** `go test ./internal/controller/... -run 'Gates|Approve|Boundary'`; envtest `gates_test.go`, `annotation_patch_test.go`.

### 11. Centralize repeated magic literals
- **Files:** PVC name `"tide-projects"` hard-coded in `task_controller.go:765,1417`, `milestone_controller.go:490`, `phase_controller.go:452`, plan controller — while `defaultSharedPVCName` exists (`project_controller.go:122`) and a `--workspaces-pvc-name` flag these sites ignore; proxy endpoint `"https://127.0.0.1:8443"` at every planner dispatch; planner default `Iterations = 20` inlined 3+ sites; label keys `"tideproject.k8s/wave-paused"`, `"…/wave-index"`, `"…/attempt"`, and raw `"tideproject.k8s/project"` in `task_controller.go:1054` despite `owner.LabelProject` existing.
- **Problem:** the configurable `SharedPVCName` is silently non-configurable for task/planner Jobs — the literal wins. A latent config bug, not just style.
- **Shape:** constants in one place (label keys → `internal/owner` next to `LabelProject`); plumb the PVC name from the reconciler field.
- **Risk:** Low-Medium (PVC-name plumb changes behavior only for non-default configs — the point). **Verify:** `rg '"tide-projects"' internal | grep -v _test` returns only the constant def.

### 12. Align log messages with AGENTS.md K8s style — as one sweep, or drop the rule
- **Files:** all of `internal/controller` (47 lowercase-initial `logger.Info/Error`, 0 uppercase — internally consistent but violates the checked-in "Start from a capital letter").
- **Problem:** either the convention or the code is wrong; today it's both-ways.
- **Shape:** if aligning, single mechanical PR. CAUTION: log text is load-bearing — `phase_gates_test.go` + the CLAUDE.md runtime-gate verification protocols grep exact strings (`"creating job"`, `"dispatch held"`); each changed message needs its greps updated in the same commit. Given that coupling, amending AGENTS.md's logging section may be cheaper.
- **Risk:** Medium — from the test/verification greps, not the code. **Verify:** `rg -l 'dispatch held|creating job' internal test .planning` before/after; `go test ./internal/controller/...`.

## Do NOT refactor yet
- `zz_generated.deepcopy.go`, `config/crd/bases/*`, `config/rbac/role.yaml`, `config/webhook/manifests.yaml`, PROJECT — generated; regenerate via `make manifests generate` only.
- `api/v1alpha1/` — vestigial-looking but intentionally retained for the SCHEMA-03 RequiresReinstall guard and referenced by `task_controller.go`'s owner-walk (`"tideproject.k8s/v1alpha1"` APIVersion match). **Removal is a migration decision, not a refactor** → that's Phase 40.
- The `//nolint:gocyclo` flat state machines (`reconcilePhase3Lifecycle`, `reconcileBoundaryPush`, `handleJobCompletion` bodies) — explicit doctrine "a flat state machine of mutually exclusive arms; splitting obscures the contract"; each arm carries cascade-numbered rationale. Shrink indirectly via items 7 + 10.
- `ctrl.Result{Requeue: true}` (14 sites; deprecating per SA1019 nolint at `project_controller.go:587`) — fold into the eventual controller-runtime bump.
- wave/plan webhook tests in `internal/controller/` — mildly misplaced but reuse the single shared envtest BeforeSuite (TEST-01 budget); moving costs a second envtest boot.
- `.planning/`, GSD artifacts, `charts/tide/values.yaml` — workflow state + FIXED contract.

## Suggested sequencing
2 → 3 → 5 → 6 → 4 → 1 (quick wins, each independently shippable), then 7 → 8 → 10 (structural track), with 9 + 11 as focused correctness fixes and 12 decided as a policy question first. Every item routes through GSD (`/gsd:quick` for the quick wins) before any edits, per repo enforcement.
