# Phase 7: Project-to-Milestone Authoring and Self-Bootstrap — Research

**Researched:** 2026-05-30
**Domain:** Go controller-runtime reconciler wiring, CRD admission, stub-subagent extension, Ginkgo kind integration testing
**Confidence:** HIGH (all claims verified against live codebase files read this session)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01 — Full `$0` self-bootstrap.** Real minimal `Milestone→Phase→Plan→Task` tree to `Complete`.
- **D-02 — No new gate at Project→Milestone.** Project authors and proceeds; existing `gates.Milestone` unchanged.
- **D-03 — No fixture edit needed for gates.** `examples/projects/small/project.yaml` already has all gates=auto.
- **D-04 — No git push on `$0` path.** Smoke fixture has no `spec.git` block; clone/push guard on `project.Spec.Git != nil && project.Spec.Git.RepoURL != ""` (line 439, 458).
- **D-05 — Project is the 5th D-A2 dispatch site.** Mirror `milestone_controller.go:reconcilePlannerDispatch` + `handleJobCompletion`.
- **D-06 — ProjectReconciler struct + manager wiring must gain dispatch deps it lacks.**
- **D-07 — Project `Complete`-detection from child Milestone status.**
- **D-08 — Stub canned tree:** project→Milestone, milestone→Phase, phase→Plan, plan→Task; task=leaf.

### Claude's Discretion
- Exact deterministic child names; placeholder Markdown artifact content.
- Where `Complete`-detection slots into Reconcile ordering relative to `reconcilePhase3Lifecycle`.
- Whether project-level `handleJobCompletion` factors shared logic with milestone's.

### Deferred Ideas (OUT OF SCOPE)
- Multi-Milestone Projects / project-level `dependsOn` ordering.
- Real `git push` at level boundaries on the `$0` path.
- Real Claude-backed authoring (`$25` path).
</user_constraints>

---

## Summary

Phase 7 wires the missing top-level link in TIDE's five-level cascade: a bare `Project` that dispatches a planner Job, reads the authored Milestone from `EnvelopeOut.ChildCRDs`, materializes the Milestone CR, and reaches `Project.Status.Phase=Complete` when all child Milestones succeed — entirely at `$0` with the stub.

The down-stack cascade (Milestone→Phase→Plan→Task) is **fully wired** in the existing reconcilers. The primary risk CONTEXT.md identified — "do piecemeal reconciler gaps block Project=Complete?" — has a concrete answer from reading the live code: **yes, one blocking gap exists that is not in D-01..D-08**: `Plan.Status.ValidationState` must equal `"Validated"` before the PlanReconciler runs `reconcileWaveMaterialization` to create Waves and dispatch Tasks. That string is **never set in production code** — only in tests via direct status patch. The stub emitting a canned Task will stall the Plan at the Wave-materialization gate unless Phase 7 also stamps `ValidationState="Validated"` on the materialized Plan (or a mechanism is added to do so automatically). This is the primary planning-critical discovery.

The second finding is that `budgetExceeded` does not block at `$0`: `IsCapExceeded` checks `AbsoluteCapCents > 0 && spent > cap` — with cap=0, the `> 0` guard short-circuits and the cap is not exceeded. The `$0` smoke path is safe.

**Primary recommendation:** Phase 7 must set `ValidationState="Validated"` on the stub-emitted Plan (or ensure it is stamped by some mechanism) in addition to the five changes in D-01..D-08, or the cascade stops at Plan→Wave boundary and Task executor Jobs never fire.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Project→Milestone planner dispatch | API / Backend (ProjectReconciler) | — | Mirrors MilestoneReconciler pattern; controller owns Job creation |
| Milestone materialization from EnvelopeOut | API / Backend (ProjectReconciler) | — | MaterializeChildCRDs is controller-only write path |
| Stub canned ChildCRDs tree | Stub container (cmd/stub-subagent) | — | EnvelopeOut writer is the subagent binary |
| Project Complete-detection | API / Backend (ProjectReconciler) | — | BoundaryDetected + Owns watch re-enqueues on child status change |
| Wave materialization from Tasks | API / Backend (PlanReconciler) | — | reconcileWaveMaterialization runs after ValidationState=Validated |
| ValidationState stamping | API / Backend (Plan admission webhook) | PlanReconciler (alternative) | Webhook must stamp Validated at admission; currently never set in prod |
| Layer B test | Test infrastructure (kind cluster) | — | Ginkgo Eventually asserts on CRD status |

---

## PRIMARY FINDING: The Down-Stack "Succeeded" Cascade — Verified Against Live Code

### Cascade trace for a 1-1-1-1 stub-fed tree

The following sequence is required for `Project.Status.Phase=Complete`. Each step is verified against a specific file and line.

**Step 1 — Project planner Job completes (NEW, Phase 7):**
- `reconcileProjectPlannerDispatch` fires after `Initialized`; creates `tide-project-<uid>-1`.
- Job succeeds → `handleProjectJobCompletion` reads `EnvelopeOut` via `PodStatusEnvelopeReader`.
- Calls `MaterializeChildCRDs(ctx, client, scheme, project, envOut.ChildCRDs)`.
- Stub emits `ChildCRDs: [{Kind:"Milestone", Name:"<name>", Spec:{"projectRef":"<project>","dependsOn":[]}}]`.
- `MaterializeChildCRDs` creates the Milestone. `[VERIFIED: dispatch_helpers.go:206-213]`

**Step 2 — MilestoneReconciler fires (existing, wired):**
- `reconcilePlannerDispatch` at `milestone_controller.go:184` dispatches `tide-milestone-<uid>-1`.
- Job succeeds → `handleJobCompletion` (line 341) reads EnvelopeOut, calls `MaterializeChildCRDs` for a Phase.
- Stub at `project` level emits a Milestone, at `milestone` level emits a Phase.
- `patchMilestoneSucceeded` sets `ms.Status.Phase="Succeeded"`. `[VERIFIED: milestone_controller.go:434-458]`
- **CRITICAL TIMING**: `handleJobCompletion` calls `patchMilestoneSucceeded` (line 431) BEFORE child Phases have Succeeded. It only defers the boundary push (not the Succeeded patch itself) when `BoundaryDetected` is false. So Milestone reaches `Succeeded` immediately after its own planner Job completes and child Phases materialize — NOT after Phases complete. `[VERIFIED: milestone_controller.go:419-431]`

**Step 3 — PhaseReconciler fires (existing, wired):**
- `reconcilePlannerDispatch` at `phase_controller.go:169` dispatches `tide-phase-<uid>-1`.
- Job succeeds → `handleJobCompletion` (line 281) reads EnvelopeOut, calls `MaterializeChildCRDs` for a Plan.
- **KEY BEHAVIOR**: Phase does NOT Succeed immediately — `patchPhaseSucceeded` is called only when `!detected && !r.hasChildPlans(ctx, ph)` (no child Plans) OR when `detected` (all child Plans Succeeded). `[VERIFIED: phase_controller.go:340-356]`
- With one stub-emitted Plan, `hasChildPlans` returns true → Phase requeues with `RequeueAfter: 5s` waiting for child Plans to Succeed. Phase Succeeded is DEFERRED until Plan=Succeeded. `[VERIFIED: phase_controller.go:348-351]`

**Step 4 — PlanReconciler fires (existing, wired — has blocking gap):**
- `reconcilePlannerDispatch` at `plan_controller.go:199` runs when the Plan has no Tasks.
- Dispatches `tide-plan-<uid>-1`, reads EnvelopeOut → materializes a Task via `MaterializeChildCRDs`.
- Calls `handlePlannerJobCompletion` → clears `Status.Phase=""` → returns to `reconcileWaveMaterialization`. `[VERIFIED: plan_controller.go:444-456]`
- **BLOCKING GAP**: `reconcileWaveMaterialization` line 535: `if plan.Status.ValidationState != "Validated" { return ctrl.Result{}, nil }`. If this is not "Validated", wave materialization is a no-op. Waves are never created. Task executor Jobs never fire. `[VERIFIED: plan_controller.go:535-537]`
- `ValidationState` is **never set to "Validated" in production code**. The Plan admission webhook (plan_webhook.go) only rejects/warns; it does NOT patch `Status.ValidationState`. The only code that writes "Validated" is test code (`plan_controller_test.go:187`, `gates_test.go:271`). `[VERIFIED: grep result — zero production assignments of ValidationState="Validated"]`

**Step 5 — Task executor Job (blocked by Step 4 gap):**
- `TaskReconciler.reconcileDispatch` fires once Task exists and has `wave-index` label.
- Dispatches an executor Job → stub `success` mode → Task `Status.Phase="Succeeded"`.
- But this is unreachable if Wave is never materialized (Step 4 gap blocks it). `[VERIFIED: task_controller.go:126-192]`

**Step 6 — Cascade completion (blocked by Step 4 gap):**
- Phase Succeeded when all child Plans Succeeded.
- Milestone Succeeded (already happened at Step 2 — see note below).
- Project Complete-detection (NEW, Phase 7): `BoundaryDetected(ctx, client, project, "Milestone")` returns true when ≥1 owned Milestone exists and all are Succeeded.
- Patches `project.Status.Phase=Complete`.

### Cascade verdict

**A 1-1-1-1 stub-fed tree will NOT reach Project=Complete without addressing the `ValidationState` gap.** The cascade stalls at Plan→Wave boundary. The planner stub emits a Task, but the Plan never produces a Wave because `reconcileWaveMaterialization` no-ops when `ValidationState != "Validated"`.

**Additionally**: Milestone reaches `Succeeded` immediately after its own planner Job completes (Step 2), which means the Project's `Complete`-detection would fire after just Milestone=Succeeded — BEFORE Phase/Plan/Task have executed anything. This is technically correct per the spec ("Project=Complete when all owned Milestones Succeeded") but means the $0 smoke bar is: Milestone=Succeeded triggers Project=Complete. The full Phase→Plan→Task execution is only observable in the integration test assertion, not required for Project=Complete.

**What Phase 7 must add beyond D-01..D-08:**
1. Fix the `ValidationState` gap: either (a) stub-subagent sets `plan.Status.ValidationState="Validated"` in the Plan's Spec.Raw (not possible — Spec is not Status), or (b) the Plan admission webhook stamps it on create, or (c) `handlePlannerJobCompletion` stamps it after materializing Tasks, or (d) stub emits a Plan whose creation immediately triggers the webhook to stamp Validated.
   - **Correct approach**: The webhook runs on Plan CREATE and must stamp `ValidationState="Validated"` on the Plan's Status after admission. Currently the webhook only validates and returns; it does NOT write to Status. The PlanReconciler's `handlePlannerJobCompletion` (plan_controller.go:355-457) after materializing Tasks and clearing Running phase is the right insertion point: patch `plan.Status.ValidationState = "Validated"` there, since the planner Job just authored the Tasks — they ARE valid. `[VERIFIED: plan_controller.go:355-457]`
   - **Alternative**: The Plan admission webhook's `ValidateCreate` runs on every Plan CREATE (line 93-95 of plan_webhook.go). It currently returns but does NOT mutate Status. Status mutation from an admission webhook requires a mutating webhook (not validating). The current webhook is validating-only (`mutating=false` in the marker at plan_webhook.go:59). So webhook stamping requires adding a mutating webhook or changing to defaulting.
   - **Simplest fix**: In `handlePlannerJobCompletion`, after `MaterializeChildCRDs` and gate checks, before clearing Running phase: patch `plan.Status.ValidationState = "Validated"`. This is the right controller seam — the planner just authored the Tasks, so they are by definition valid for $0 stub execution. `[VERIFIED: plan_controller.go:444-456 is the insertion point]`

---

## Standard Stack

### Core (all verified as already-present in go.mod / codebase)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `sigs.k8s.io/controller-runtime` | v0.24.x | Reconciler framework | Already the project standard; CLAUDE.md locked |
| `github.com/jsquirrelz/tide/pkg/dispatch` | (internal) | EnvelopeIn/Out/ChildCRDSpec types | Proven contract used by all 4 existing dispatch sites |
| `github.com/jsquirrelz/tide/internal/controller` (dispatch_helpers) | (internal) | `BuildPlannerEnvelope`, `MaterializeChildCRDs`, `BoundaryDetected` | Already allowlists Milestone; reused verbatim |
| `github.com/jsquirrelz/tide/internal/gates` | (internal) | `BoundaryDetected`, `EvaluatePolicy`, `CheckApprove` | BoundaryDetected covers the "Milestone" childKind already |
| `github.com/jsquirrelz/tide/internal/credproxy` | (internal) | `credproxy.Sign` for per-dispatch HMAC token | Same pattern as Milestone/Phase/Plan reconcilers |
| `github.com/jsquirrelz/tide/internal/dispatch/podjob` | (internal) | `podjob.BuildJobSpec(JobKindPlanner)`, `PodStatusEnvelopeReader` | Proven; used by all 4 existing planner sites |

**Installation:** No new dependencies. All reuse existing packages already in go.mod.

---

## Architecture Patterns

### System Architecture Diagram

```
bare Project CR applied
      │
      ▼
ProjectReconciler.Reconcile
      │ (init Job done, Phase=Initialized)
      ▼
reconcileProjectPlannerDispatch (NEW)
      │ creates tide-project-<uid>-1 Job (JobKindPlanner)
      │ patches Status.Phase=Running
      ▼
stub-subagent (role=planner, level=project)
      │ emits EnvelopeOut.ChildCRDs=[{Kind:Milestone,Name:...,Spec:{projectRef:...}}]
      ▼
handleProjectJobCompletion (NEW)
      │ reads EnvelopeOut via PodStatusEnvelopeReader
      │ calls MaterializeChildCRDs → creates Milestone CR
      ▼
MilestoneReconciler.reconcilePlannerDispatch
      │ creates tide-milestone-<uid>-1 Job
      │ patches Status.Phase=Running
      ▼
stub-subagent (role=planner, level=milestone)
      │ emits ChildCRDs=[{Kind:Phase,...}]
      ▼
handleJobCompletion → MaterializeChildCRDs → creates Phase CR
      │ patches Milestone.Status.Phase=Succeeded (immediately on Job done)
      ▼
PhaseReconciler.reconcilePlannerDispatch
      │ creates tide-phase-<uid>-1 Job
      ▼
stub-subagent (role=planner, level=phase)
      │ emits ChildCRDs=[{Kind:Plan,...}]
      ▼
handleJobCompletion → MaterializeChildCRDs → creates Plan CR
      │ Phase defers Succeeded until all child Plans Succeeded
      ▼
PlanReconciler.reconcilePlannerDispatch  [plan has no Tasks yet]
      │ creates tide-plan-<uid>-1 Job
      ▼
stub-subagent (role=planner, level=plan)
      │ emits ChildCRDs=[{Kind:Task,...}]
      ▼
handlePlannerJobCompletion → MaterializeChildCRDs → creates Task CR
      │ *** MUST stamp Plan.Status.ValidationState="Validated" HERE (Phase 7 addition) ***
      │ clears Status.Phase=""
      ▼
reconcileWaveMaterialization (plan_controller.go:534)
      │ gate: ValidationState=="Validated" → proceed
      │ ComputeWaves → materializeWaves → creates Wave CR (tide-wave-<uid>-0)
      │ stampTaskLabels → Task gets wave-index=0 + project label
      ▼
WaveReconciler + TaskReconciler (executor dispatch)
      │ Task executor Job → stub success → Task.Status.Phase=Succeeded
      ▼
Plan.Status.Phase stays "" (PlanReconciler has no Succeeded patch)
      │ *** NOTE: Plan itself has no Succeeded state — see below ***
      ▼
Phase: hasChildPlans=true → waits for BoundaryDetected("Plan")
      │ BoundaryDetected lists Plans owned by Phase, checks Status.Phase=Succeeded
      │ *** SECOND GAP: Plan never patches its own Status.Phase=Succeeded ***
```

### Critical Gap #2: Plan Has No Succeeded Patch

Reading `plan_controller.go` fully: `reconcileWaveMaterialization` has no terminal `patchPlanSucceeded` call. The Phase's `handleJobCompletion` calls `gates.BoundaryDetected(ctx, r.Client, ph, "Plan")` which checks `plan.Status.Phase == "Succeeded"` (gates/boundary.go:106-121). But `PlanReconciler` never patches `plan.Status.Phase = "Succeeded"`. The Phase would wait forever for Plans to Succeed.

**Verified search:** `grep -rn "Succeeded" /Users/justinsearles/Projects/tide/internal/controller/plan_controller.go` shows only `"Succeeded"` in the terminal short-circuit guard at line 214 and `CycleDetected` write at line 566. No `patchPlanSucceeded` method exists. `[VERIFIED: plan_controller.go full read — no plan Succeeded patch]`

**Resolution**: Either (a) Phase's `BoundaryDetected("Plan")` call is wrong and Phase should not wait for Plans to Succeed (but this is Phase 04.1 code — it's intentional), or (b) Phase 7 must add `patchPlanSucceeded` to PlanReconciler called when all owned Tasks have Succeeded.

Looking at phase_controller.go lines 340-356 more carefully: the comment says "requeue and wait" for Plans to Succeed. This is intentional design. The Plan needs a Succeeded patch. This is a pre-existing bug that Phase 7 must either fix or work around.

**Work-around for Phase 7**: The Layer B test can assert `Project=Complete` by driving the tree to Milestone=Succeeded (which already happens right after the milestone planner Job completes, per Step 2 above). Milestone=Succeeded is sufficient for `BoundaryDetected(project, "Milestone")` to return true, which triggers `Project.Status.Phase=Complete`. The full Plan/Task execution is NOT required for Project=Complete under the current architecture.

**Confirmed**: Milestone patches `Succeeded` immediately after `handleJobCompletion` (milestone_controller.go:431 calls `patchMilestoneSucceeded` unconditionally after the boundary-push check). So the Project=Complete path is:
1. Project planner Job done → Milestone created
2. Milestone planner Job done → Phase created, **Milestone.Status.Phase=Succeeded** immediately
3. ProjectReconciler re-enqueued (owns Milestone) → `BoundaryDetected(project, "Milestone")` → true → `Project.Status.Phase=Complete`

Steps 3-onward (Phase/Plan/Task) are NOT required for Project=Complete. The SPEC's acceptance criterion 4 says "when the sole child Milestone reaches Succeeded, the Project transitions Running→Complete" — this is achievable.

**For the integration test spec (REQ 5):** "full Milestone→Phase→Plan→Task tree materializes and Project=Complete" — the stub must emit children at all levels so the CRs exist, but Project=Complete fires before they all Succeed. The test should assert: all four CRD kinds exist in the namespace AND Project.Status.Phase=Complete. Asserting Task.Status.Phase=Succeeded requires the ValidationState and Plan Succeeded gaps to be fixed. Whether Phase 7 fixes the full chain or just the top edge needs to be a planning decision.

---

## CRD Admission Constraints on Stub's Canned Child Specs

### Per-Kind minimum valid Spec (verified against api/v1alpha1/*_types.go)

| Kind | Required Fields | Optional Fields | Stub must set |
|------|----------------|-----------------|---------------|
| `Milestone` | `projectRef` (MinLength=1) | `dependsOn` | `projectRef: "<project-name>"` |
| `Phase` | `milestoneRef` (MinLength=1) | `dependsOn` | `milestoneRef: "<milestone-name>"` |
| `Plan` | `phaseRef` (MinLength=1) | — | `phaseRef: "<phase-name>"` |
| `Task` | `planRef` (MinLength=1), `filesTouched` (MinItems=1), `declaredOutputPaths` (MinItems=1) | `dependsOn`, `caps`, `dev` | `planRef: "<plan-name>", filesTouched: ["stub-output.txt"], declaredOutputPaths: ["/workspace/artifacts"]` |
| `Wave` | Stub MUST NOT emit Wave — waves are derived by PlanReconciler | — | Omit entirely |

**Verified sources:**
- `Milestone.Spec.ProjectRef`: `+kubebuilder:validation:MinLength=1` at `milestone_types.go:28` `[VERIFIED]`
- `Phase.Spec.MilestoneRef`: `+kubebuilder:validation:MinLength=1` at `phase_types.go:27` `[VERIFIED]`
- `Plan.Spec.PhaseRef`: `+kubebuilder:validation:MinLength=1` at `plan_types.go:27` `[VERIFIED]`
- `Task.Spec.PlanRef`: `+kubebuilder:validation:MinLength=1` at `task_types.go:71` `[VERIFIED]`
- `Task.Spec.FilesTouched`: `+kubebuilder:validation:MinItems=1` at `task_types.go:80` `[VERIFIED]`
- `Task.Spec.DeclaredOutputPaths`: `+kubebuilder:validation:MinItems=1` at `task_types.go:89` `[VERIFIED]`
- `Task.Spec.DependsOn`: `+optional` — empty is valid for a single-task wave `[VERIFIED]`

No CEL `x-kubernetes-validations` markers visible in these type files (only kubebuilder marker-based validation). No admission webhooks for Milestone, Phase, or Task. The Plan webhook validates cycle detection and file-touch mismatches but does not reject on missing fields (those are OpenAPI schema rejections).

**Note on `DeclaredOutputPaths`**: The `Task.Spec.DeclaredOutputPaths` field also has `MinItems=1` but no `omitempty` in the JSON tag, making it effectively required. The stub must set at least one path, e.g., `["/workspace/artifacts/stub-output"]`.

### Stub ChildCRDSpec JSON shape

The `Spec` field in `ChildCRDSpec` is a `runtime.RawExtension`, meaning the stub must marshal only the `Spec` struct content (not the full CRD), which `MaterializeChildCRDs` unmarshals into `ms.Spec`, `ph.Spec`, etc. `[VERIFIED: dispatch_helpers.go:210-213]`

Example for Task level:
```json
{
  "kind": "Task",
  "name": "stub-task-1",
  "spec": {
    "planRef": "stub-plan-1",
    "filesTouched": ["stub-output.txt"],
    "declaredOutputPaths": ["/workspace/artifacts/stub"],
    "dependsOn": [],
    "dev": {"testMode": "success"}
  }
}
```

**Dev.TestMode in the stub Spec**: The `Task.Spec.Dev.TestMode` is stamped in the Task Spec. When the TaskReconciler dispatches the executor Job, it builds the EnvelopeIn from `task.Spec.Dev.TestMode` (verified: task_controller.go `buildEnvelopeIn` uses `task.Spec.Dev`). Setting `testMode: "success"` in the canned Task Spec ensures the executor exits 0. `[VERIFIED: task_types.go:56-67, TaskDev struct]`

---

## ProjectReconciler → Manager Wiring Delta

### Fields missing from `ProjectReconciler` struct (verified by reading both structs)

**`MilestoneReconciler` has (cmd/manager/main.go lines 345-367):**
```go
EnvReader: envReader,          // *podjob.PodStatusEnvelopeReader
SigningKey: signingKey,         // []byte
CredproxyImage: credproxyImage, // string
SubagentImage: subagentImage,   // string
HelmProviderDefaults: helmProviderDefaults, // controller.ProviderDefaults
```

**`ProjectReconciler` currently has (cmd/manager/main.go lines 326-343):**
```go
Dispatcher: dispatcher,        // already present
TidePushImage: tidePushImage,  // already present
MaxConcurrentReconciles: ...,
WatchNamespace: ...,
```

**Fields to ADD to `ProjectReconciler` struct definition (project_controller.go):**
```go
EnvReader      podjob.EnvelopeReader
SigningKey      []byte
SubagentImage   string
CredproxyImage  string
HelmProviderDefaults ProviderDefaults
```

**Wiring lines to ADD to `cmd/manager/main.go` in the ProjectReconciler registration block:**
```go
EnvReader:            envReader,
SigningKey:            signingKey,
SubagentImage:        subagentImage,
CredproxyImage:       credproxyImage,
HelmProviderDefaults: helmProviderDefaults,
```

The values (`envReader`, `signingKey`, `subagentImage`, `credproxyImage`, `helmProviderDefaults`) are all already computed above the reconciler-registration block. `[VERIFIED: cmd/manager/main.go:305-322 compute all five values; lines 345-367 show how MilestoneReconciler is wired]`

---

## Insertion Point: Project Planner Dispatch in reconcileProjectPhase2

### Where the new dispatch slots in (verified against project_controller.go)

`reconcileProjectPhase2` (line 222) currently:
1. `handleBudgetGate` (step 1)
2. PVC bind check (step 2)
3. Init Job creation (step 3)
4. `handleInitJobCompletion` (step 4) → may call `reconcilePhase3Lifecycle`
5. If `project.Status.Phase == Initialized || Running || PushLeaseFailed || Complete` → calls `reconcilePhase3Lifecycle` (lines 272-277)

**Idempotency concern**: The cascade-13 idempotency guard in `handleInitJobCompletion` (lines 308-316) already short-circuits if Phase is Running, Complete, PushLeaseFailed, PushLeakBlocked — it returns without re-patching Initialized. This means on subsequent reconciles after the planner is dispatched (Phase=Running), `handleInitJobCompletion` returns early and falls through to `reconcilePhase3Lifecycle`.

**Correct insertion point**: A new `reconcileProjectPlannerDispatch` method is called from inside `reconcilePhase3Lifecycle` (or as a parallel branch from `reconcileProjectPhase2`) after the init Job succeeds and the Phase has been set to `Initialized`. The simplest mirror of the milestone pattern: add it as the final step inside `reconcilePhase3Lifecycle`, gated on `project.Status.Phase == Initialized || Running` (not Complete, not PushLeaseFailed).

**Short-circuits needed** (mirroring milestone_controller.go:185-209):
- Terminal: `if project.Status.Phase == Complete { return ctrl.Result{}, nil }` — avoid re-dispatching
- Running: check existing planner Job, if terminal call `handleProjectJobCompletion`
- AwaitingApproval: not needed (D-02 — no gate at Project→Milestone)

**$0 budget gate**: `absoluteCapCents: 0` → `IsCapExceeded` checks `AbsoluteCapCents > 0` first (cap.go:48) → false (0 is not > 0) → cap NOT exceeded. The planner Job will dispatch. `[VERIFIED: cap.go:48]`

**resolveProject for planner envelope**: `BuildPlannerEnvelope` requires a `*Project` as the `project` parameter. At the Project level, the Project IS the parent, so `project` is passed directly. `BuildPlannerEnvelope("project", project, project, attempt, token, caps, proxyEndpoint, r.HelmProviderDefaults)`. `[VERIFIED: dispatch_helpers.go:159 signature]`

---

## Stub-Subagent Extension Shape

### Where the planner-level canned tree emits (verified against cmd/stub-subagent/main.go)

`dispatchSuccess` is called for `testMode==""` or `"success"` (main.go:131-132). Currently ignores `env.Role` and `env.Level` — emits a flat success envelope with empty `ChildCRDs`. `[VERIFIED: main.go:204-244]`

**Required change**: In `dispatchSuccess`, after reading `env.Role` and `env.Level`, branch:
```go
if env.Role == "planner" {
    return dispatchPlannerSuccess(ctx, env, outPath, stderr)
}
// existing executor path
```

`dispatchPlannerSuccess` switches on `env.Level` and emits:
- `"project"` → 1 `ChildCRDSpec{Kind:"Milestone", Name:"stub-milestone-1", Spec:{projectRef:<project-name>}}`
- `"milestone"` → 1 `ChildCRDSpec{Kind:"Phase", Name:"stub-phase-1", Spec:{milestoneRef:<ms-name>}}`
- `"phase"` → 1 `ChildCRDSpec{Kind:"Plan", Name:"stub-plan-1", Spec:{phaseRef:<phase-name>}}`
- `"plan"` → 1 `ChildCRDSpec{Kind:"Task", Name:"stub-task-1", Spec:{planRef:<plan-name>, filesTouched:[...], declaredOutputPaths:[...], dev:{testMode:"success"}}}`
- `"task"` (or any other) → use existing executor path (leaf, no children)

**Deriving parent ref names**: The `EnvelopeIn.TaskUID` carries the UID of the dispatching CRD. However, the name of the parent is NOT in the EnvelopeIn. Options:
1. Look up the parent name from a label in `EnvelopeIn.Provider.Params` (could be injected by the reconciler)
2. Use a fixed stub name (deterministic: `"stub-milestone-1"`, etc.) and rely on `MaterializeChildCRDs` setting the correct namespace and owner ref
3. Encode the parent name in `EnvelopeIn` (would require changes to `BuildPlannerEnvelope`)

**Simplest approach**: Use fixed stub child names (`stub-milestone-1`, `stub-phase-1`, etc.) and derive the parent ref name from the `EnvelopeIn.TaskUID` by having `BuildPlannerEnvelope` inject it in `Provider.Params` (e.g., `params["parentName"]`). Or use a dedicated `EnvelopeIn` field. **Alternatively**: The stub can look up the parent name from a label on the envelope itself — but `EnvelopeIn` has no such field yet.

**Practical resolution**: The planner reconcilers already call `BuildPlannerEnvelope("milestone", ms, project, ...)` where `ms` is the parent object. The parent object's name can be injected into `EnvelopeIn.Prompt` or into `Provider.Params["parentName"]`. For the stub at `$0`, a simple approach: the reconciler passes the parent name via the `EnvelopeIn.Prompt` field (currently empty in stub mode), or `Provider.Params`. The planner at `$0` doesn't use the Prompt for LLM calls, so it can carry metadata. **Recommendation**: Add a `parentName` key to `Provider.Params` inside `BuildPlannerEnvelope` for the specific planner levels, injected at the 5th dispatch site (ProjectReconciler).

**Termination message size**: A single ChildCRDSpec with minimal Spec JSON is well under 4096 bytes (the K8s termination message limit). The canned tree stays small as required. `[VERIFIED: dispatch_helpers.go comment on PodStatusEnvelopeReader; stub design]`

---

## Test Harness: Bare-Project Layer B Spec Pattern

### Established helpers (verified from suite_test.go)

All these are available in the `kind_integration` package:
- `ensureSubagentSA(ns string)` — creates tide-subagent SA in a namespace
- `ensureProjectsPVC(ns string)` — creates tide-projects PVC
- `pvcPrewarmPod(ns string)` — pre-warms WaitForFirstConsumer PVC
- `ensureSigningKeySecret(ns string)` — mirrors tide-signing-key Secret
- `applyFile(path string)` — applies a YAML file via kubectl
- `deleteNamespace(ns string)` — AfterEach cleanup
- `exportKindLogs()` — debug helper on failure

### New fixture pattern (bare-project.yaml)

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: bare-project-test
---
# Per-namespace setup is done by test helpers; this fixture provides only CRDs
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: bare-project
  namespace: bare-project-test
spec:
  targetRepo: file:///tmp/no-such-repo
  budget:
    absoluteCapCents: 0
  subagent:
    image: <stub-image-ref>  # injected by test like up-stack-project.yaml
    model: stub
  gates:
    milestone: auto
    phase: auto
    plan: auto
    task: auto
    pauseBetweenWaves: false
```

### Spec structure

```go
It("bare Project self-bootstraps to Project=Complete", func() {
    ns := "bare-project-test"
    // BeforeEach: ensureSubagentSA(ns), ensureProjectsPVC(ns), pvcPrewarmPod(ns), ensureSigningKeySecret(ns)
    
    applyFile("testdata/bare-project.yaml")
    
    // Assert Milestone materialized
    Eventually(func() error { ... k8sClient.Get milestone ... }, 3*time.Minute, 2*time.Second)
    
    // Assert Phase materialized (owned by Milestone)
    Eventually(func() error { ... k8sClient.Get phase ... }, 4*time.Minute, 2*time.Second)
    
    // Assert Plan materialized (owned by Phase)
    // Assert Task materialized (owned by Plan)
    // Assert Project.Status.Phase == "Complete"
    Eventually(func() (string, error) {
        var p tideprojectv1alpha1.Project
        err := k8sClient.Get(ctx, client.ObjectKey{Name:"bare-project", Namespace:ns}, &p)
        return p.Status.Phase, err
    }, 5*time.Minute, 2*time.Second).Should(Equal("Complete"))
})
```

### Budget constraint

`kindTestTimeout` is currently 18 minutes. A bare-Project spec adds: init Job (~20s) + project planner Job (~30s stub) + milestone planner Job (~30s) + phase planner Job (~30s) + plan planner Job (~30s) + task executor Job (~20s) = ~160s (~2.7 minutes) of Job wall-clock time plus controller reaction latency. Total spec budget of ~5 minutes is well within the 18-minute suite budget. No `kindTestTimeout` bump is needed. `[VERIFIED: suite_test.go:95 kindTestTimeout=18m]`

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Reading planner EnvelopeOut from termination message | Custom pod log scraper | `podjob.PodStatusEnvelopeReader` | Already used by all 4 up-stack reconcilers; proven in Layer B |
| Materializing child CRDs from envelope | Custom Create loop with ad-hoc type switches | `MaterializeChildCRDs` | Allowlist enforcement (T-308), AlreadyExists idempotency, owner-ref wiring all built in |
| Checking "all Milestones Succeeded" | Bespoke list-and-check | `gates.BoundaryDetected(ctx, client, project, "Milestone")` | Already handles the "Milestone" case (boundary.go:68-85); childless=false contract prevents premature Complete |
| Planner Job build | Custom PodSpec | `podjob.BuildJobSpec(JobKindPlanner, opts)` | 600s wall-clock floor, envelope-writer init container, credproxy sidecar, signed token — all required |
| Signing the credproxy token | Custom HMAC | `credproxy.Sign(r.SigningKey, string(project.UID), duration)` | Same signing contract; mismatched impl breaks credproxy auth |

---

## Common Pitfalls

### Pitfall 1: Project=Complete fires too early (before any Task executes)
**What goes wrong:** `BoundaryDetected(project, "Milestone")` returns true as soon as Milestone.Status.Phase="Succeeded". Milestone reaches Succeeded immediately after ITS OWN planner Job completes (not after Phase/Plan/Task). The Project would be Complete before any Task executor Job runs.
**Why it happens:** `patchMilestoneSucceeded` (milestone_controller.go:434) is called unconditionally after gate checks, regardless of child Phase status. The boundary push (BoundaryDetected on Phases) is deferred, but Succeeded patch is not.
**How to avoid:** The SPEC says "Project transitions Running→Complete when all child Milestones Succeeded" — this is correct behavior per the spec. For the acceptance test, assert CRD materialization separately from Project=Complete. Don't assert Project=Complete requires Task execution.
**Warning signs:** Project=Complete observed before Phase/Plan/Task CRDs exist in the cluster.

### Pitfall 2: Plan Wave materialization stalls (ValidationState gap)
**What goes wrong:** `reconcileWaveMaterialization` no-ops if `plan.Status.ValidationState != "Validated"`. Waves are never created. Tasks have no `wave-index` label. TaskReconciler never dispatches executor Jobs.
**Why it happens:** `ValidationState="Validated"` is never set in production code — only in tests. The Plan admission webhook validates but does not mutate Status.
**How to avoid:** Phase 7 must stamp `plan.Status.ValidationState = "Validated"` in `handlePlannerJobCompletion` after materializing Tasks (plan_controller.go ~line 456, before clearing Phase="").
**Warning signs:** Wave CRDs never appear in the namespace after Plan is created; Task CRDs created but never get `wave-index` label; no executor Jobs dispatched.

### Pitfall 3: Plan never patches Succeeded — Phase waits forever
**What goes wrong:** PhaseReconciler.handleJobCompletion calls `gates.BoundaryDetected(ctx, r.Client, ph, "Plan")` which checks `plan.Status.Phase == "Succeeded"`. PlanReconciler never patches `plan.Status.Phase = "Succeeded"`.
**Why it happens:** The Plan's terminal Succeeded state was not implemented. Phase 04.1 added the `hasChildPlans` requeue path to wait, but the Plan's Succeeded patch was never added.
**How to avoid for Phase 7:** If the SPEC acceptance bar is Project=Complete (driven by Milestone=Succeeded), the Phase→Plan→Task chain completing is NOT required for Project=Complete. If the full chain assertion is needed in the Layer B test, Phase 7 must also add `patchPlanSucceeded` to PlanReconciler (called when all owned Tasks are Succeeded, via a `BoundaryDetected(plan, "Task")` check) — or accept that Phase stays non-Succeeded.
**Warning signs:** Phase stays at a requeue loop for 5s indefinitely; Plan.Status.Phase never shows "Succeeded".

### Pitfall 4: resolveProject returns nil for the ProjectReconciler dispatch site
**What goes wrong:** `BuildPlannerEnvelope` and `podjob.BuildJobSpec` take a `*Project` parameter. For the ProjectReconciler, the "project" IS the reconciled object. Using `project` itself as both `parent` and `project` parameter is correct — but if `project` is nil (shouldn't happen in Reconcile) or if `BuildJobSpec` dereferences `opts.Project` without a nil check, it panics.
**Why it happens:** The milestone dispatch (milestone_controller.go:282) does `if project.Spec.ProviderSecretRef != ""` after resolving project — it can tolerate nil but some paths dereference. The ProjectReconciler passes itself, so nil is impossible.
**How to avoid:** Pass `project` as both `parent` (for the envelope's `TaskUID`) and `project` (for provider resolution) in `BuildPlannerEnvelope`. No special nil-check needed.

### Pitfall 5: Cascade-13 idempotency guard re-stomps Phase
**What goes wrong:** If the new `reconcileProjectPlannerDispatch` patches `Status.Phase=Running`, the cascade-13 guard in `handleInitJobCompletion` (project_controller.go:308-316) recognizes Running as "already advanced past Initialized" and returns early. But `reconcilePhase3Lifecycle` (line 273-277 condition) includes `PhaseRunning` → so `reconcilePhase3Lifecycle` still fires. If the new dispatch check is INSIDE `reconcilePhase3Lifecycle`, it will be called on every reconcile while Running. The idempotency of the planner Job creation (AlreadyExists = success) and the Running-phase short-circuit in the new dispatch function prevent re-dispatch.
**How to avoid:** Mirror the milestone Running-check exactly: if Phase=Running, fetch the planner Job and check `isJobTerminal`; if not terminal, return. Only dispatch when Phase != Running and != Complete.

---

## Code Examples

### Pattern 1: reconcileProjectPlannerDispatch (mirror of milestone_controller.go:184)

```go
// Source: milestone_controller.go:184-337 (adapted for Project level)
func (r *ProjectReconciler) reconcileProjectPlannerDispatch(ctx context.Context, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
    // Terminal short-circuit.
    if project.Status.Phase == tideprojectv1alpha1.PhaseComplete || project.Status.Phase == tideprojectv1alpha1.PhaseInitFailed {
        return ctrl.Result{}, nil
    }

    jobName := fmt.Sprintf("tide-project-%s-1", project.UID)

    // Running: check Job terminal state.
    if project.Status.Phase == tideprojectv1alpha1.PhaseRunning {
        var job batchv1.Job
        if err := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: jobName}, &job); err != nil {
            if !apierrors.IsNotFound(err) { return ctrl.Result{}, err }
            return ctrl.Result{}, nil
        }
        if isJobTerminal(&job) {
            return r.handleProjectJobCompletion(ctx, project, &job)
        }
        return ctrl.Result{}, nil
    }

    // Acquire plannerPool (D-A4).
    if r.PlannerPool != nil {
        if err := r.PlannerPool.Acquire(ctx); err != nil { return ctrl.Result{}, err }
        defer r.PlannerPool.Release()
    }

    attempt := 1
    plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
    if plannerCaps.Iterations <= 0 { plannerCaps.Iterations = 20 }

    _, envInJSON, err := BuildPlannerEnvelope("project", project, project, attempt, "", pkgdispatch.Caps{
        WallClockSeconds: int(plannerCaps.WallClockSeconds),
        Iterations:       int(plannerCaps.Iterations),
    }, "https://127.0.0.1:8443", r.HelmProviderDefaults)
    if err != nil { return ctrl.Result{}, fmt.Errorf("build project planner envelope: %w", err) }

    token, err := credproxy.Sign(r.SigningKey, string(project.UID),
        time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
    if err != nil { return ctrl.Result{}, fmt.Errorf("mint project planner signed token: %w", err) }

    subagentImage := r.SubagentImage
    if subagentImage == "" { subagentImage = r.HelmProviderDefaults.Image }

    opts := podjob.BuildOptions{
        Kind: podjob.JobKindPlanner, ParentObj: project, Level: "project", Attempt: attempt,
        Project: project, SignedToken: token, EnvelopeInJSON: envInJSON,
        SubagentImage: subagentImage, CredproxyImage: r.CredproxyImage,
        PVCName: "tide-projects", ProjectUID: string(project.UID), Caps: plannerCaps,
    }
    job := podjob.BuildJobSpec(opts)
    if err := owner.EnsureOwnerRef(job, project, r.Scheme); err != nil { return ctrl.Result{}, err }
    if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) { return ctrl.Result{}, err }

    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Phase = tideprojectv1alpha1.PhaseRunning
    // Set AuthoringPlanner condition
    if err := r.Status().Patch(ctx, project, patch); err != nil { return ctrl.Result{}, err }
    return ctrl.Result{}, nil
}
```

### Pattern 2: Complete-detection insertion point

```go
// Source: milestone_controller.go:419 BoundaryDetected pattern; gates/boundary.go:68-85
// Call in reconcileProjectPlannerDispatch BEFORE dispatching (or in reconcilePhase3Lifecycle):
func (r *ProjectReconciler) checkProjectComplete(ctx context.Context, project *tideprojectv1alpha1.Project) (bool, error) {
    detected, err := gates.BoundaryDetected(ctx, r.Client, project, "Milestone")
    if err != nil || !detected { return false, err }
    patch := client.MergeFrom(project.DeepCopy())
    project.Status.Phase = tideprojectv1alpha1.PhaseComplete
    // Set Succeeded condition
    return true, r.Status().Patch(ctx, project, patch)
}
```

### Pattern 3: ValidationState stamp (critical addition to plan_controller.go)

```go
// Source: plan_controller.go:444-456 — insert BEFORE clearing Phase=""
// In handlePlannerJobCompletion, after MaterializeChildCRDs and gate checks:
if len(envOut.ChildCRDs) > 0 {
    // Stamp ValidationState=Validated after Tasks materialized
    patch := client.MergeFrom(plan.DeepCopy())
    plan.Status.ValidationState = "Validated"
    if err := r.Status().Patch(ctx, plan, patch); err != nil {
        return ctrl.Result{}, err
    }
}
```

### Pattern 4: Stub planner-mode Level switch

```go
// Source: cmd/stub-subagent/main.go:204 dispatchSuccess — extend here
func dispatchPlannerSuccess(ctx context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
    var children []pkgdispatch.ChildCRDSpec
    switch env.Level {
    case "project":
        raw, _ := json.Marshal(map[string]interface{}{"projectRef": parentNameFromParams(env), "dependsOn": []string{}})
        children = []pkgdispatch.ChildCRDSpec{{Kind: "Milestone", Name: "stub-milestone-1", Spec: runtime.RawExtension{Raw: raw}}}
    case "milestone":
        raw, _ := json.Marshal(map[string]interface{}{"milestoneRef": parentNameFromParams(env)})
        children = []pkgdispatch.ChildCRDSpec{{Kind: "Phase", Name: "stub-phase-1", Spec: runtime.RawExtension{Raw: raw}}}
    // ... phase, plan levels
    }
    out := pkgdispatch.EnvelopeOut{/* ... */ ChildCRDs: children}
    // write envelope
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| ProjectReconciler dispatches no planner | Will dispatch planner Job (Phase 7) | Phase 7 | Closes cascade-7 |
| Stub emits no ChildCRDs | Stub branches on Role+Level to emit canned tree | Phase 7 | Enables $0 self-bootstrap |
| ValidationState never set in production | Must be stamped in handlePlannerJobCompletion (Phase 7 addition) | Phase 7 | Unblocks Wave materialization |
| Plan has no Succeeded patch | Plan Succeeded path must be added for full chain (Phase 7 or follow-up) | Phase 7 | Unblocks Phase→Plan boundary |

---

## Open Questions

1. **Where does `checkProjectComplete` slot into `Reconcile`?**
   - What we know: The `Owns(&tideprojectv1alpha1.Milestone{})` watch (project_controller.go:781) re-enqueues the Project when child Milestone status changes. So the reconcile fires when Milestone=Succeeded.
   - What's unclear: Should Complete-detection be INSIDE `reconcilePhase3Lifecycle` or as an early-exit check in `reconcileProjectPhase2` before the planner dispatch path? If inside `reconcilePhase3Lifecycle`, the condition at line 273 (`PhaseRunning || PhaseComplete || PhasePushLeaseFailed || PhaseInitialized`) already routes there; Complete-detection could be the first step.
   - Recommendation: Add `checkProjectComplete` as the FIRST step in `reconcilePhase3Lifecycle`. If it returns true (Complete patched), return immediately. This mirrors how `reconcilePlannerDispatch` starts with a terminal short-circuit.

2. **Should the Plan Succeeded patch be in Phase 7 scope?**
   - What we know: Without `Plan.Status.Phase=Succeeded`, `PhaseReconciler.handleJobCompletion` requeues forever. The SPEC (REQ 4, REQ 5, REQ 6) asserts full tree materialization and Project=Complete. Project=Complete fires on Milestone=Succeeded (regardless of Plan chain). The Layer B spec (REQ 5) requires "full Milestone→Phase→Plan→Task tree materializes" — CRD materialization can be asserted without Plan=Succeeded; Task execution requires the ValidationState fix.
   - Recommendation: Add `patchPlanSucceeded` (gated on `BoundaryDetected(plan, "Task")`) to PlanReconciler as part of Phase 7, so the Layer B test can assert the full chain including Task executor Jobs running. This is a small addition and closes the architectural gap.

3. **How does the stub get the parent name for child `*Ref` fields?**
   - What we know: `EnvelopeIn.TaskUID` is the UID of the dispatching CRD, not its name. `EnvelopeIn.Provider.Params` is available for injection.
   - Recommendation: Inject `parentName` key into `Provider.Params` in `BuildPlannerEnvelope` for the project level dispatch (or inject it at each reconciler's `BuildPlannerEnvelope` call). The stub reads it from `env.Provider.Params["parentName"]`. This is the least-invasive approach without changing `EnvelopeIn`'s schema.

---

## Environment Availability

Step 2.6: No new external dependencies introduced. All tools (kind, helm, kubectl, Docker) are already verified as available from Phase 6. No new audit needed.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega |
| Config file | No dedicated config — test is `go test ./test/integration/kind/...` |
| Quick run command | `make test-int` (runs Layer B + Layer A) |
| Full suite command | `make test-int` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| REQ 1 | Project dispatches planner Job, transitions Initialized→Running | integration (kind) | `make test-int` | Wave 0 — new spec |
| REQ 2 | Milestone CR materializes from EnvelopeOut, idempotent re-reconcile | integration (kind) | `make test-int` | Wave 0 — new spec |
| REQ 3 | Stub emits 1 child/level at each planner level, 0 at task | unit | `go test ./cmd/stub-subagent/...` | Wave 0 — new test |
| REQ 4 | Project transitions Running→Complete when Milestone=Succeeded | integration (kind) | `make test-int` | Wave 0 — new spec |
| REQ 5 | Full tree Milestone→Phase→Plan→Task materializes; Project=Complete | integration (kind) | `make test-int` | Wave 0 — new spec |
| REQ 6 | `make acceptance-v1-smoke` exits 0 at Project=Complete | acceptance | `make acceptance-v1-smoke` | Exists (script unchanged) |

### Wave 0 Gaps

- [ ] `test/integration/kind/bare_project_test.go` — covers REQ 1, REQ 2, REQ 4, REQ 5
- [ ] `test/integration/kind/testdata/bare-project.yaml` — bare Project fixture
- [ ] `cmd/stub-subagent/main_test.go` or dedicated `planner_test.go` — covers REQ 3 (unit: feed EnvelopeIn at each Level, assert ChildCRDs shape)

---

## Security Domain

The phase adds no new network surfaces, auth flows, or credentials. The signing key and credproxy pattern are reused exactly from the proven milestone dispatch site. No new ASVS categories apply beyond those already covered.

---

## Sources

### Primary (HIGH confidence — read directly this session)
- `/Users/justinsearles/Projects/tide/internal/controller/project_controller.go` — full read; struct, Reconcile, reconcileProjectPhase2, handleInitJobCompletion, reconcilePhase3Lifecycle, SetupWithManager
- `/Users/justinsearles/Projects/tide/internal/controller/milestone_controller.go` — full read; reconcilePlannerDispatch, handleJobCompletion, patchMilestoneSucceeded
- `/Users/justinsearles/Projects/tide/internal/controller/phase_controller.go` — full read; handleJobCompletion, hasChildPlans, patchPhaseSucceeded
- `/Users/justinsearles/Projects/tide/internal/controller/plan_controller.go` — full read; reconcilePlannerDispatch, handlePlannerJobCompletion, reconcileWaveMaterialization
- `/Users/justinsearles/Projects/tide/internal/controller/task_controller.go` — partial read (struct, Reconcile, reconcileDispatch gate)
- `/Users/justinsearles/Projects/tide/internal/controller/dispatch_helpers.go` — full read; childKindAllowlist, BuildPlannerEnvelope, MaterializeChildCRDs
- `/Users/justinsearles/Projects/tide/internal/gates/boundary.go` — full read; BoundaryDetected
- `/Users/justinsearles/Projects/tide/internal/budget/cap.go` — full read; IsCapExceeded (cap=0 behavior verified)
- `/Users/justinsearles/Projects/tide/cmd/manager/main.go` — full read; all reconciler wiring
- `/Users/justinsearles/Projects/tide/cmd/stub-subagent/main.go` — full read; dispatchSuccess, no planner branch
- `/Users/justinsearles/Projects/tide/api/v1alpha1/milestone_types.go` — full read; MilestoneSpec.ProjectRef MinLength=1
- `/Users/justinsearles/Projects/tide/api/v1alpha1/phase_types.go` — full read; PhaseSpec.MilestoneRef MinLength=1
- `/Users/justinsearles/Projects/tide/api/v1alpha1/plan_types.go` — full read; PlanSpec.PhaseRef MinLength=1; ValidationState field
- `/Users/justinsearles/Projects/tide/api/v1alpha1/task_types.go` — full read; TaskSpec.PlanRef, FilesTouched, DeclaredOutputPaths MinItems=1
- `/Users/justinsearles/Projects/tide/pkg/dispatch/childcrd.go` — full read; ChildCRDSpec struct
- `/Users/justinsearles/Projects/tide/pkg/dispatch/envelope.go` — full read; EnvelopeIn.Role/Level
- `/Users/justinsearles/Projects/tide/internal/webhook/v1alpha1/plan_webhook.go` — full read; validates but does NOT stamp ValidationState
- `/Users/justinsearles/Projects/tide/test/integration/kind/suite_test.go` — partial read; helpers, kindTestTimeout=18m
- `/Users/justinsearles/Projects/tide/test/integration/kind/up_stack_dispatch_test.go` — full read; pattern for new spec
- `/Users/justinsearles/Projects/tide/test/integration/kind/testdata/up-stack-project.yaml` — full read
- `/Users/justinsearles/Projects/tide/examples/projects/small/project.yaml` — full read; gates all auto, no spec.git
- grep results confirming ValidationState is never assigned "Validated" in production code

### Verification Commands Run
- `grep -rn "ValidationState\s*=" .../internal/ --include="*.go"` → confirmed no production assignment of "Validated"
- `grep -rn '"Validated"' .../... --include="*.go"` → only in test files and constants
- `find .../cmd -name "main.go"` → confirmed cmd/manager/main.go location

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `podjob.DefaultWallClockGraceSeconds` is exported and used in the same way as milestone_controller.go:274 | Code Examples Pattern 1 | Token TTL calculation off; credproxy rejects; low risk (copy-paste from milestone) |
| A2 | `tideprojectv1alpha1.PhaseRunning` is a valid constant (vs literal string "Running") | Code Examples Pattern 1 | Compilation error; easy to fix in plan |

**All other claims in this research were verified against live codebase files read in this session.**

---

## Metadata

**Confidence breakdown:**
- Down-stack cascade analysis: HIGH — read every reconciler; verified line-by-line
- CRD admission constraints: HIGH — read all *_types.go; verified kubebuilder markers
- Manager wiring delta: HIGH — read cmd/manager/main.go fully; compared struct fields
- Stub extension pattern: HIGH — read stub main.go; identified exact insertion point
- ValidationState gap: HIGH — grep confirmed no production assignment; webhook read confirmed it only validates
- Plan Succeeded gap: HIGH — read plan_controller.go fully; no patchPlanSucceeded method exists
- Test harness pattern: HIGH — read suite_test.go helpers and up_stack_dispatch_test.go

**Research date:** 2026-05-30
**Valid until:** 2026-07-30 (stable — fast-moving only if controller-runtime patches change reconciler contract)
