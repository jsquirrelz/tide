# Domain Pitfalls — v1.0.3 Planning Resumption & Cost Resilience

**Domain:** Resume/import layer on a Kubernetes CRD reconciler that dispatches LLM Jobs with UID-keyed PVC artifacts
**Researched:** 2026-06-18
**Milestone context:** v1.0.3 — observed bug class: `bypass-budget=true` annotation sets `Status.Phase = Pending` → re-init fires → re-clone fires → looping reporters against already-authored children
**Confidence:** HIGH — derived from direct codebase inspection (`project_controller.go`, `backend.go`, `dispatch_helpers.go`, `depgraph.go`, `pkg/dispatch/envelope.go`) and cross-referenced against `.planning/PROJECT.md` constraints and `README.md` spec invariants

> **Scope.** These pitfalls cover adding (1) artifact-import / stage-skip resumption and (2) safe halt-resume to the existing TIDE controller. Each pitfall is at the intersection of: (a) the five-level paradigm with UID-keyed PVC envelopes, (b) controller-runtime's at-least-once reconcile model, and (c) the CRD-status-only persistence constraint. Prior milestone pitfalls (Pitfalls 1-24) remain valid and are not repeated here.

---

## Critical Pitfalls

### Pitfall R-01: Budget-bypass clears to `Pending` instead of `Running`, triggering re-init

**Severity:** Critical (observed, root-cause identified)

**What goes wrong:**
`handleBudgetGate` at `project_controller.go:1257` patches `Status.Phase = PhasePending` on bypass-clear. `reconcileProjectPhase2` then sees a non-Running, non-Initialized project and falls through to the init-Job check. The init-Job name is `tide-init-<project.UID>` (deterministic, `project_controller.go:339`), but its TTL is 300s (`buildInitJob:1321`). When a budget halt happens during a long planning wave, the init-Job has long since been TTL-GC'd. The init-Job lookup returns NotFound; `ensureInitJob` creates a new one; the workspace re-initializes (overwriting the `workspace/` subPath on the PVC); the clone Job (`tide-clone-<UID>`, also TTL-GC'd) re-dispatches; and the project loops. Reporter Jobs that already materialized Milestone children read from a workspace that has been wiped underneath them.

**Why it happens:**
The push-lease bypass at `reconcilePhase3Lifecycle:530` correctly resets to `PhaseRunning`. The budget bypass path at line 1257 diverged and was never brought into alignment with that pattern. `PhasePending` is the pre-init phase; resetting to it is semantically "restart from scratch."

**Code locations:**
- `project_controller.go:1257` — wrong target phase on bypass-clear
- `project_controller.go:339` — init-Job check runs on every reconcile for non-Running/non-Initialized projects
- `project_controller.go:1321` — init-Job TTL = 300s
- `project_controller.go:500-510` — `Status.Git.BranchName` is the reliable "already-initialized" sentinel (set after init, never cleared)

**Prevention:**
- Fix line 1257: set `Status.Phase = tidev1alpha2.PhaseRunning`. Guard this with a check that confirms the project has already advanced past init — the most reliable signal is `project.Status.Git.BranchName != ""` (set at `reconcilePhase3Lifecycle:504`, never cleared).
- Also gate init-Job dispatch on `project.Status.Git.BranchName == ""` so a second init-Job is structurally impossible once the project has cloned.
- Test: envtest scenario where init-Job has been TTL-GC'd, bypass fires, verify no new init-Job is created and `Status.Phase` lands at `Running`.

**Warning signs:**
- `tide-init-<UID>` Job appearing in a namespace that already has Milestone children
- `Status.Phase` oscillating between `Pending` and `Running` in project Events
- Reporter Jobs re-running after a bypass annotation is consumed

**Phase to address:** Phase 1 of v1.0.3 — this is the directly observed bug; must be fixed before any import work.

---

### Pitfall R-02: UID-churn aliasing — importing the wrong level's envelope

**Severity:** Critical (silent planning corruption)

**What goes wrong:**
Envelopes are keyed by UID: `{projectUID}/workspace/envelopes/{taskUID}/out.json` (per `backend.go:95`). When a project is re-applied after a budget halt, all CRD objects get new UIDs from the API server. The salvaged envelopes on the PVC still carry the old UIDs. An import step that resolves old-UID envelopes onto new-UID objects must use a stable secondary key (object name). If the name-based lookup collides — two Milestones with similar names, or a name collision across levels — the envelope for Milestone B's planner gets injected as if it were Milestone A's planner output. The ChildCRDs in that envelope contain Phase names scoped to Milestone B, materialized under Milestone A. The planning DAG is silently corrupted; the global Execution DAG built on it has wrong edges.

**Why it happens:**
The PVC path is UID-keyed for runtime (correct — UIDs are unique per object-lifetime). Import must bridge from UID-keyed storage to name-keyed lookup. This bridging logic is new code with no existing pattern in the codebase. Without explicit content-identity validation, a name collision causes silent mis-routing.

**Code locations:**
- `backend.go:95` — `filepath.Join(r.WorkspaceRoot, projectUID, "workspace", "envelopes", taskUID, "out.json")`
- `pkg/dispatch/envelope.go:49` — `EnvelopeIn.TaskUID` is the only identifier carried forward in the envelope
- `project_controller.go:1131` — `ReadOut(ctx, string(project.UID), string(project.UID))` — both segments are the *current* project's UID

**Prevention:**
- The import layer must treat salvaged envelopes as untrusted foreign data (same threat model as the T-308 ChildCRDSpec allowlist). Before injecting a salvaged envelope:
  1. Parse it and read `EnvelopeOut.ChildCRDs[*].Name`. Verify each Name matches an expected child of the *current* object at this level, not just any object.
  2. Reject envelopes whose `apiVersion` does not match `APIVersionV1Alpha1` via the existing `ValidateAPIVersionKind` (`envelope.go:407`).
  3. Record an `importSourcePath` alongside the accepted envelope in a new `Status.Import` section for operator-visible provenance.
- Design the stable-key for import as `{project.Name}/{level}/{objectName}`, not the raw PVC path.

**Warning signs:**
- Phase CRDs appearing under the wrong Milestone parent after import
- Global indegree mismatches: tasks from wrong plans counted as predecessors
- `Status.Conditions` showing `ConditionAuthoringPlanner=True` but ChildCRDs pointing to wrong-level children

**Phase to address:** Phase 2 of v1.0.3 (envelope-import design specification, before any import code is written).

---

### Pitfall R-03: Stale or partial-write envelope accepted as valid — planner skipped incorrectly

**Severity:** Critical (silent planning corruption)

**What goes wrong:**
A planner Job was in-flight when the budget halt fired. The Job was killed mid-write. The result is a partial `out.json`. If the planner used write-then-rename (write to a temp, then `mv` to `out.json`) and the crash happened after the rename, the file is structurally complete JSON but only contains the children emitted up to that point. `FilesystemEnvelopeReader.ReadOut` (`backend.go:94`) does a single `os.ReadFile` + `json.Unmarshal` — no completeness check. `ValidateAPIVersionKind` (`envelope.go:407`) checks `apiVersion` and `kind` only. The import layer accepts the envelope as valid, skips the planner, and materializes a partial child set. Downstream levels that depend on missing children are never dispatched; the global indegree map is wrong.

**Code locations:**
- `backend.go:94-105` — `ReadOut` has no completeness check beyond valid JSON parse
- `pkg/dispatch/envelope.go:400-409` — `ValidateAPIVersionKind` checks only `apiVersion` and `kind`
- `project_controller.go:1204` — `out.ChildCount` exists and is already used as a succession guard; the same field must gate import validation

**Prevention:**
- Before import, validate `len(ChildCRDs) == ChildCount`. A mismatch means a partial write; reject the envelope and re-plan.
- Optionally, require the planner harness to write `"complete": true` as the final field in `out.json` (written only after the full JSON is formed). The reader rejects envelopes without this sentinel.
- Log the envelope source path, `ChildCount`, and `len(ChildCRDs)` as structured fields before committing any planner-skip decision.

**Warning signs:**
- Fewer Milestone/Phase/Plan/Task children than expected after a resume
- `checkProjectComplete` firing on a project that has not finished planning
- Global indegree map showing 0 for tasks that should have predecessors from unimported plans

**Phase to address:** Phase 2 of v1.0.3 (import completeness validation).

---

### Pitfall R-04: TTL-GC race — reporter Job GC'd before completion handler fires, causing budget double-count

**Severity:** Critical (financial correctness + this is the observed class of bug)

**What goes wrong:**
The planner Job succeeds. Its TTL is 600s (`jobspec.go:73`). The reporter Job that materializes children also has a TTL (`reporter_jobspec.go:175`). If the project is in BudgetExceeded long enough for both Jobs to TTL-GC, then on bypass-clear the reconcile path sees no reporter Job and sets `isFirstCompletion = true` (`project_controller.go:1156-1175`). The `isFirstCompletion && envReadOK` guard fires `budget.RollUpUsage` (`project_controller.go:1178-1182`) a second time for the same planner envelope. `Status.Budget.CostSpentCents` increases by the planner's cost again — which may immediately re-trigger the budget cap that was just bypassed.

**Why it happens:**
The `isFirstCompletion` guard uses reporter-Job *existence* as its signal. The reporter Job is an ephemeral K8s resource with a TTL. A long-lived halt makes reporter-Job-existence an unreliable indicator of whether rollup has already been performed.

**Code locations:**
- `project_controller.go:1156-1162` — reporter Job existence check drives `isFirstCompletion`
- `project_controller.go:1178-1182` — `if isFirstCompletion && envReadOK { budget.RollUpUsage(...) }`
- `jobspec.go:73` — planner Job TTL = 600s
- `reporter_jobspec.go:175` — reporter Job TTL

**Prevention:**
- Replace the `isFirstCompletion` guard with a durable status field: `Status.Budget.PlannerRolledUpUID` (or a per-level map). After rollup fires for a given planner job name, record that job name. On subsequent reconciles, skip rollup if `PlannerRolledUpUID == currentPlannerJobName`.
- For the import path specifically: when injecting a salvaged envelope, set `skipBudgetRollup = true` unconditionally — the prior run's cost was already recorded in `Status.Budget.CostSpentCents` and must not be re-counted.

**Warning signs:**
- `Status.Budget.CostSpentCents` increasing by the same amount twice in Events
- Budget cap re-triggering immediately after bypass annotation is consumed
- `ConditionBudgetExceeded` flipping back to True within seconds of bypass

**Phase to address:** Phase 1 of v1.0.3 — prerequisite to all import work; this is a standalone correctness bug on the existing bypass path.

---

### Pitfall R-05: Partial-plan import corrupts the global Execution DAG

**Severity:** Critical (silent correctness, possibly undetectable without explicit validation)

**What goes wrong:**
Import succeeds for all Milestones and Phases but only partially for Plans — e.g., the planner for Milestone B Phase 2 was in-flight at halt time and its envelope is absent. Tasks from the imported plans exist as CRD objects; their `DependsOn` references task names from the missing plans. The `computeGlobalIndegree` function at `task_controller.go:481` re-derives the indegree map from all current Tasks. For a task reference that does not resolve to any existing Task CRD, the `depgraph.go` resolver returns empty (`depgraph.go:28: "An unresolved ref returns empty (conservative — never invents an edge, D-06)"`). Tasks that should have waited for missing-plan tasks now have indegree 0 and dispatch immediately. The global wave schedule is wrong with no error surfaced.

**Why it happens:**
The depgraph resolver's conservative design (unresolved ref = no edge) is correct for the normal case (a typo should not block indefinitely). For import, "no such task" means "task not yet materialized," not "wrong reference." The resolver cannot distinguish these cases.

**Code locations:**
- `depgraph.go:28` — conservative empty-return on unresolved scope
- `task_controller.go:470-482` — `computeGlobalIndegree` lists all Tasks; missing tasks silently produce zero indegree contribution

**Prevention:**
- Import atomicity must be per-Milestone: either all Plans for a Milestone are imported (every plan envelope valid + complete), or none are and the Milestone re-plans. Partial Milestone import is rejected.
- Before committing any import, verify the full task set is self-consistent: every `Task.Spec.DependsOn` entry must resolve to an existing Task CRD. Unresolved refs at import time are an import failure, not a silent skip.
- Surface a `Status.Condition` of type `ImportIncomplete` that blocks execution dispatch (TaskReconciler checks it before indegree computation). Clear it only when the full consistency check passes.

**Warning signs:**
- Tasks dispatching before their declared predecessors complete on a resumed run
- Plans completing faster than expected on resume (sign of missing wait edges)
- `computeGlobalIndegree` returning 0 for tasks with non-empty `DependsOn`

**Phase to address:** Phase 2 of v1.0.3 (import atomicity contract and import-consistency pre-check).

---

### Pitfall R-06: Schema mismatch — salvaged v1alpha1 envelope ChildCRDSpec decoded into v1alpha2 typed structs

**Severity:** Serious (import silently drops v1alpha2-required fields)

**What goes wrong:**
The dogfood run #2 salvage artifacts were authored by the v1alpha1 planner. The v1.0.2 CRD schema is v1alpha2. The envelope contract version check (`ValidateAPIVersionKind`, `envelope.go:407`) correctly accepts envelopes with `apiVersion = "tideproject.k8s/v1alpha1"`. But the `ChildCRDSpec.Spec.Raw` bytes inside those envelopes were produced against the v1alpha1 typed spec. Phase 23 added `Wave.Spec.ProjectRef` replacing `Wave.Spec.PlanRef`. When `MaterializeChildCRDs` decodes `Spec.Raw` into the v1alpha2 Wave struct, `ProjectRef` is zero-valued. A Wave with empty `ProjectRef` cannot be traced back to its Project by the global wave derivation engine; it becomes an orphan Wave that never dispatches.

**Code locations:**
- `dispatch_helpers.go:32-36` — `MaterializeChildCRDs` decodes `Spec.Raw` into typed v1alpha2 structs without schema-version awareness
- `pkg/dispatch/envelope.go:407-409` — version check is at the envelope level, not at the per-child-spec level
- Phase 23 schema migration — introduced `Wave.Spec.ProjectRef`

**Prevention:**
- The import path must run a v1alpha1→v1alpha2 conversion on each `ChildCRDSpec.Spec.Raw` before materialization. The conversion function is already scaffolded in the API package (the conversion webhook infrastructure from Phase 23).
- For the v1.0.3 dogfood import: the salvage directory must be pre-processed through a one-shot migration tool (`tide import --upgrade-schema`) that rewrites `Spec.Raw` bytes before the controller sees them.
- Acceptance test: import a v1alpha1 salvage fixture, verify all Wave CRDs have non-empty `Spec.ProjectRef`.

**Warning signs:**
- Wave CRDs with empty `Spec.ProjectRef` after import
- `computeGlobalIndegree` returning wrong counts because Waves cannot be traced to their Project
- Import appearing to succeed but execution never dispatching

**Phase to address:** Phase 2 of v1.0.3 (schema-version-aware materializer and one-shot migration tool for the dogfood salvage).

---

## Moderate Pitfalls

### Pitfall R-07: Clone Job re-dispatches on resume because TTL-GC'd Job is the only guard

**What goes wrong:**
After R-01 is fixed (bypass targets `Running`), the clone-Job dispatch at `reconcilePhase3Lifecycle:549` still checks only for Job existence: `cloneErr = apierrors.IsNotFound`. The clone Job TTL is 300s (`push_helpers.go:212`). On resume after a long halt, the clone Job is gone. A new clone Job is dispatched into an already-initialized workspace. `git clone` fails with "destination path already exists"; the project stalls at `InitFailed`.

**Code locations:**
- `project_controller.go:549-571` — clone Job creation gated only on `apierrors.IsNotFound(cloneErr)`
- `push_helpers.go:212` — clone Job TTL = 300s

**Prevention:**
- Add `Status.Git.CloneComplete: bool` (set to `true` when the clone Job succeeds, never cleared). Gate the clone-Job dispatch on `!project.Status.Git.CloneComplete`. This is the durable idempotency guard that Job-existence-based logic cannot provide across a TTL gap.

**Warning signs:**
- Second `tide-clone-<UID>` Job appearing in project Events after a resume
- Clone Job failing with "destination path already exists"
- `Status.Phase = InitFailed` on a project that had previously been Running

**Phase to address:** Phase 1 of v1.0.3 (alongside R-01 fix).

---

### Pitfall R-08: Cap-raise ergonomics — raising `AbsoluteCapCents` leaves `RollingCapCents` re-halting

**What goes wrong:**
A project hits `AbsoluteCapCents`. The operator raises `AbsoluteCapCents` in the Project spec. The rolling-window cap (`RollingCapCents`) was not raised; the trailing spend is above the rolling cap. The spec change triggers a reconcile; `handleBudgetGate` checks `IsCapExceeded` which evaluates the rolling cap; the project re-halts immediately. The operator raised the cap but the project still halts — confusing without a clear error distinguishing which cap fired.

**Code locations:**
- `budget/cap.go:64` — two cap forms: absolute and TTL-bypass
- `handleBudgetGate:1227` — checks both caps without surfacing which one triggered

**Prevention:**
- `tide resume` CLI verb must display *both* cap values and current spend before proceeding, and indicate which cap is currently exceeded.
- Promote the TTL-bypass form (`bypass-budget-until=<RFC3339>`, already documented at `budget/cap.go:64`) as the default ergonomic for cap-raise-and-resume so the project does not immediately re-halt while the operator adjusts both caps.
- Consider a combined `CapExceededReason` field in the `BudgetExceeded` condition that names which cap triggered.

**Warning signs:**
- `ConditionBudgetExceeded` cycling between True and False within a single reconcile window
- Operator annotation consumed but project re-halts before any dispatch fires
- Confusing event sequence with back-to-back cap-exceeded events citing different cap types

**Phase to address:** Phase 1 of v1.0.3.

---

### Pitfall R-09: At-least-once reconcile causes double-import on informer lag

**What goes wrong:**
The import step runs during reconcile; creates child CRDs; the status patch setting `ImportComplete = true` fails with a ResourceVersion conflict; the reconcile errors and retries. On the second reconcile, the import step runs again. `MaterializeChildCRDs` hits `AlreadyExists` for all children (idempotent at the K8s API level). But side effects that fire during import — recording `importSourceUID` annotations, triggering budget-rollup suppression, writing provenance fields — are not idempotent if they append to a list or patch over a field with a non-idempotent operation.

**Prevention:**
- Check a `Status.Condition` of type `ImportComplete` as the *first step* of the import block. If already True, skip the entire import block unconditionally. This is the correct controller-runtime idiom for once-only bootstrap actions.
- All import side effects must be idempotent patch operations (use `meta.SetStatusCondition`, `MergeFrom` patches, never append-to-slice operations within the reconcile critical section).

**Warning signs:**
- `Annotations["importSourceUID"]` values appearing duplicated or with malformed multi-value formats
- Import-related Events appearing twice in `kubectl describe project`
- Budget rollup recording two entries for the same salvaged planner UID

**Phase to address:** Phase 2 of v1.0.3 (import idempotency design).

---

### Pitfall R-10: Attacker-supplied envelopes injected via PVC

**What goes wrong:**
The import step reads `out.json` from the PVC. The PVC is a shared namespace resource. If another pod in the same namespace writes to `{projectUID}/workspace/envelopes/{levelUID}/out.json`, it can inject arbitrary `ChildCRDSpec.Spec.Raw` bytes. The Kind allowlist (`dispatch_helpers.go:33`, T-308 mitigation) prevents creating arbitrary CRDs. But within the allowed Kinds, `Spec.Raw` is decoded and applied. A crafted spec can set `Task.Spec.Prompt` to a prompt-injection payload that is then sent to the next subagent.

**Prevention:**
- Import paths must apply the same `ValidateAPIVersionKind` call as the runtime materializer, plus a content-origin check. The recommended approach: require that `out.json` on the PVC was written by a subagent pod running as UID 1000 (enforce via PVC POSIX ownership), or HMAC-sign `out.json` using the project's signing key (the same key used for credproxy tokens at `project_controller.go:1045`). The signing key differs between runs, so for salvage import, rely on operator-gated invocation (`tide import`) as the trust boundary.
- Apply the same path-traversal defense as `FilesystemEnvelopeReader.ReadPrompt` (`backend.go:116-127`) to all import path lookups.

**Warning signs:**
- `out.json` files on the PVC with POSIX owner other than UID 1000
- Unexpected CRD objects appearing after import with unusual `Spec.Prompt` fields
- Subagents executing prompts that reference content from unrelated projects

**Phase to address:** Phase 2 of v1.0.3 (import security model — define trust boundary before writing import code).

---

### Pitfall R-11: Resume re-dispatches already-Succeeded execution Tasks

**What goes wrong:**
On resume with import, the `TaskReconciler` re-derives readiness from `computeGlobalIndegree` on every reconcile. If any code path in the resume sequence clears `Status.Phase` on a Succeeded Task (an over-eager `tide resume --retry-failed` is the documented class of this bug, caught in Phase 25 code review), those Tasks re-dispatch — potentially overwriting already-merged commits on the run branch.

**Code locations:**
- Phase 25 time-fence fix (referenced in MEMORY.md): the correct fix requires both (a) only clearing Tasks in `Failed` phase and (b) a time-fence guard to prevent clearing a Task that succeeded after the resume command was issued.

**Prevention:**
- The import path must never touch `Status.Phase` on Tasks with `Status.Phase = Succeeded`. Write a pre-import invariant check: enumerate all Tasks, assert none transition from Succeeded to any other phase during import.
- Acceptance test: import a salvage fixture that includes completed Tasks; verify none are re-dispatched; verify the run branch gains no new commits from re-executed Tasks.

**Warning signs:**
- Tasks with `Status.Phase = Succeeded` appearing in the `ConditionAuthoringPlanner` active set
- Duplicate commits on the run branch (same diff applied twice)
- `computeGlobalIndegree` returning 0 for tasks that should be blocked by Succeeded predecessors

**Phase to address:** Phase 2 of v1.0.3 (import must not touch execution-layer Tasks).

---

### Pitfall R-12: Cycle detection bypassed for imported plan trees

**What goes wrong:**
The Plan admission webhook runs `ComputeWaves` with `CycleError` detection at apply time. CRD objects created via `client.Create` from within the controller bypass the admission webhook. A salvaged plan tree that contained a cycle (authored by a hallucinating planner) is imported without validation. The global Execution DAG contains a cycle; `computeGlobalIndegree` never reaches 0 for the cyclic tasks; those tasks never dispatch; the project stalls silently with no error surfaced.

**Prevention:**
- The import step must explicitly call `dag.ComputeWaves` on the full imported task set *before* creating any child CRDs. If `CycleError` is returned, the import fails with an operator-visible condition: `ImportFailed / CyclicPlanDetected`. The spec invariant is explicit (`README.md`: "Cycles are bugs, not runtime conditions"). The import path is not an exception.

**Warning signs:**
- Tasks with indegree > 0 that never reach 0 after import completes
- Project stalled indefinitely at `Running` with no active dispatch
- `dag.ComputeWaves` not called anywhere in the import code path

**Phase to address:** Phase 2 of v1.0.3.

---

## Minor Pitfalls

### Pitfall R-13: Budget double-count from salvage import re-triggering cost rollup for already-counted planners

**What goes wrong:**
Salvaged planner envelopes carry `out.Usage` (token counts, cost). If the import step triggers `budget.RollUpUsage` for each imported envelope, and the original run already rolled those costs before the halt, `Status.Budget.CostSpentCents` is inflated by the prior run's planning cost. This may cause an instant budget halt on the resumed run and obscures the true cost of the resume work.

**Prevention:**
- Import-path envelope injection must suppress budget rollup. Either zero out `out.Usage` in the salvaged envelope before it enters the completion handler, or add a `salvageImport: true` flag that the completion handler checks before calling `budget.RollUpUsage`.
- Expose the prior run's cost as a separate `Status.Budget.SalvagedPlanningCostCents` field so operators can see what was inherited vs. newly spent.

**Phase to address:** Phase 2 of v1.0.3.

---

### Pitfall R-14: Envelope `apiVersion` constant not bumped if v1.0.3 changes the envelope schema

**What goes wrong:**
`APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"` is a constant in `pkg/dispatch/envelope.go:24`. If v1.0.3 adds a field to `EnvelopeOut` that is required for the import path, old envelopes from the salvage will silently read the zero value for that field. `ValidateAPIVersionKind` does not guard against missing fields within a version.

**Prevention:**
- v1.0.3 must not add required fields to `EnvelopeOut` or `EnvelopeIn` without bumping the constant to `v1alpha2`. Optional fields with `omitempty` are safe.
- The salvage import path should use a new wrapper type (`SalvageImportManifest`) that wraps `EnvelopeOut` rather than extending the envelope contract, keeping the existing contract stable.

**Phase to address:** Phase 2 of v1.0.3 (schema design review before any new fields are added).

---

## Phase-Specific Warnings

| Phase / Topic | Likely Pitfall | Mitigation |
|---------------|---------------|------------|
| Phase 1 — Budget-bypass correctness | R-01 (Pending re-init), R-04 (reporter TTL-GC double rollup), R-07 (clone re-dispatch) | Fix bypass to target `Running`; add durable `UsageRolledUp` guard; add `Status.Git.CloneComplete` flag |
| Phase 1 — Budget-bypass ergonomics | R-08 (cap-raise leaves rolling cap re-halting) | Surface both cap values in `tide resume`; promote TTL-bypass form as default |
| Phase 2 — Import design | R-02 (UID aliasing), R-03 (partial-write acceptance), R-05 (partial-plan DAG corruption), R-06 (v1alpha1 schema mismatch), R-10 (PVC injection), R-11 (Succeeded task re-dispatch), R-12 (cycle bypass) | Full import spec before code: stable-key lookup, ChildCount completeness check, Milestone-level atomicity, schema conversion, trust boundary, `ImportComplete` condition guard, cycle pre-check |
| Phase 2 — Import financial correctness | R-09 (at-least-once double-import), R-13 (budget double-count from salvage) | `ImportComplete` condition as first-step guard; suppress rollup on salvaged envelopes |
| Any phase touching `EnvelopeOut` schema | R-14 (version constant not bumped) | Only add `omitempty` optional fields; use `SalvageImportManifest` wrapper for import-specific fields |

---

## Sources

- Direct inspection: `/Users/justinsearles/Projects/tide/internal/controller/project_controller.go` (lines 339, 969-972, 1156-1182, 1257, 1300-1321)
- Direct inspection: `/Users/justinsearles/Projects/tide/internal/dispatch/podjob/backend.go` (lines 92-141)
- Direct inspection: `/Users/justinsearles/Projects/tide/pkg/dispatch/envelope.go` (lines 21-24, 400-409)
- Direct inspection: `/Users/justinsearles/Projects/tide/pkg/dispatch/childcrd.go` (T-308 threat comment)
- Direct inspection: `/Users/justinsearles/Projects/tide/internal/controller/depgraph.go` (lines 17-30)
- Direct inspection: `/Users/justinsearles/Projects/tide/internal/controller/dispatch_helpers.go` (lines 17-36)
- Direct inspection: `/Users/justinsearles/Projects/tide/internal/dispatch/podjob/jobspec.go` (lines 72-73: `DefaultTTLSecondsAfterFinished = 600`)
- Direct inspection: `/Users/justinsearles/Projects/tide/internal/controller/reporter_jobspec.go` (line 175: reporter Job TTL)
- `.planning/PROJECT.md` — milestone v1.0.3 scope, constraints, and key decisions
- `README.md` spec — resumption invariants (§"Failure handling at wave boundaries"), cycle detection, CRD-status-only persistence
- MEMORY.md — Phase 25 code review finding: `resume --retry-failed` clearing FailureHalt before resetting Failed tasks; time-fence fix pattern
