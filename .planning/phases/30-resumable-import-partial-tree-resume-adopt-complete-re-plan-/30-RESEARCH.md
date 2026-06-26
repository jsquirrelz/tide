# Phase 30: Resumable Import — Partial-Tree Resume - Research

**Researched:** 2026-06-25
**Domain:** Import controller (import_controller.go), per-node status machines, global Execution DAG (depgraph.go), project planner idempotency guard (project_controller.go)
**Confidence:** HIGH — all findings verified directly from source code

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Fork 2 (LOCKED):** Incomplete/missing-envelope node is materialized as a fresh/Pending state (CR created, NO envelope copied) so its parent or itself re-authors against current main. Node identity/UID preserved.

**Fork 3 (LOCKED):** Tighten the project-level adoption guard to gate on materialized children / ImportComplete state, NOT Job presence. MUST NOT regress the N>1-milestone incremental-materialization case.

**Fork 4 (REQUIRED):** New test tier driving a PARTIAL import (mixed complete/incomplete envelopes) all the way to Project=Complete.

### Claude's Discretion
- Exact field/status value and where the branch lives (ImportController materialization vs a helper).
- Test fixture shape (reuse run #2 salvage bundle vs a minimal hand-authored mixed bundle).
- Whether the project-guard tightening is a new condition or a refactor of the existing guard block.

### Deferred Ideas (OUT OF SCOPE)
- Dogfood run #2 re-attempt (unblocked by this phase but not run here).
- OpenAI backend — separate milestone.
</user_constraints>

---

## Summary

Phase 30 fixes a silent two-part defect in the import feature. The `ImportController.reconcileCreatingCRs` materializes every seed node at its salvaged status (e.g. `Running`) unconditionally. The `tide-import` Job's completeness guard correctly skips incomplete envelopes. These two halves don't reconcile: a node imported as `Running` with no envelope behind it stalls the controller permanently. Run #2 produced 40 such zombie nodes.

The fix requires three coordinated changes: (1) a per-node branch in `reconcileCreatingCRs` — complete envelope → adopt current behavior; incomplete/missing → materialize with status `""` (empty, not "Running") so the parent planner re-authors it; (2) tighten the project-level planner idempotency guard to check `ImportComplete=True` condition, not just Job presence; (3) a new test tier that drives a mixed partial/complete import all the way to `Project=Complete`.

**Primary recommendation:** Materialize incomplete nodes with `Status.Phase=""` (empty string). This is the exact initial state every fresh CR has before any reconcile runs, which means it threads every existing controller state machine cleanly without touching the adoption gate or any idempotency guard.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Per-node complete/incomplete branch | ImportController (import_controller.go:reconcileCreatingCRs) | — | This is where status is stamped from the seed; the per-node branch belongs here |
| Re-planning of incomplete nodes | Milestone/Phase/PlanReconciler (existing planner dispatch) | — | Fresh/empty nodes fall into the normal fresh-authoring path automatically |
| Project-level adoption guard tighten | ProjectReconciler (project_controller.go:1037-1052) | — | The Job-presence guard is the one place that doesn't block post-ImportComplete dispatch |
| Dependency consistency for re-planned nodes | TaskReconciler/depgraph.go (computeGlobalIndegree) | — | Tasks are re-derived from re-planned Plans; global indegree re-resolves on each reconcile |
| Partial-tree-to-completion test coverage | test/integration/kind/import_resume_test.go | envtest (import_controller_test.go) | Kind test needed because it drives full Job dispatch + adoption cascade |

---

## Open Question Resolution (the core of this research)

### OQ-1: Exact Re-Plannable Status (Fork 1)

**Finding: Use `Status.Phase=""` (empty string) for incomplete nodes. This is VERIFIED as the correct choice by tracing every state machine in the controller suite.**

Tracing the status machine at each level reveals the following:

**Milestone controller** (`milestone_controller.go:241`):
```go
if ms.Status.Phase == "Succeeded" || ms.Status.Phase == "Failed" {
    return ctrl.Result{}, nil  // terminal short-circuit
}
// ...
if ms.Status.Phase == "AwaitingApproval" { ... }  // approval gate
if ms.Status.Phase == "Running" { ... }           // job-terminal branch
// Falls through to idempotency guard at Step 2b:
// checks for child Phases by spec.milestoneRef
```

A Milestone with `Status.Phase=""` falls through to Step 2b, the idempotency guard that checks if child Phases already exist (by `spec.milestoneRef`). If no child Phases exist (the incomplete case), it proceeds to dispatch a planner Job. This is exactly re-plannable behavior.

**Phase controller** (`phase_controller.go:236`):
```go
if ph.Status.Phase == "Succeeded" || ph.Status.Phase == "Failed" { return }
if ph.Status.Phase == "AwaitingApproval" { ... }
if ph.Status.Phase == "Running" { ... }
// Falls to idempotency guard: checks for child Plans by spec.phaseRef
```

Same pattern. A Phase with `Status.Phase=""` and no child Plans triggers planner dispatch. [VERIFIED: phase_controller.go:234-317]

**Plan controller** (`plan_controller.go:249-323`):
```go
if plan.Status.Phase == "AwaitingApproval" { ... }
// No-op if Tasks already exist
if len(taskList.Items) > 0 { return ctrl.Result{}, false, nil }
if plan.Status.Phase == "Succeeded" || plan.Status.Phase == "Failed" { return }
if plan.Status.Phase == "Running" { ... }
// Falls to D-02 descent hold and then planner dispatch
```

A Plan with `Status.Phase=""` and no Tasks (because no envelope was copied) triggers the planner path to dispatch a new plan-planner Job. [VERIFIED: plan_controller.go:282-295]

**Import platform/adoption gate** (all three controllers check `ConditionImportComplete` BEFORE dispatching):
- milestone_controller.go:370-377: parks if `ImportComplete != True`
- phase_controller.go:368-376: parks if `ImportComplete != True`
- plan_controller.go:374-381: parks if `ImportComplete != True`

A node with `Status.Phase=""` is parked by the import hold UNTIL `ImportComplete=True` fires, at which point it falls into the normal authoring path. So the sequencing is: ImportComplete fires → nodes with `""` status proceed through normal authoring → idempotency guard passes (no children) → planner dispatches.

**Does "Pending" also work?**

`Status.Phase="Pending"` is defined at the Project level (`api/v1alpha2/project_types.go:418`, `PhasePending = "Pending"`). It is NOT defined as a constant for Milestone/Phase/Plan — those levels do not have a "Pending" constant. More importantly, the milestone, phase, and plan controllers have NO early-return branch for `"Pending"` status — they all fall through identically to `""`. So `"Pending"` would also work, but:

1. `""` is the natural zero-value that every newly-created CR starts with.
2. `""` requires ZERO code path changes in the three planner controllers.
3. `""` is already the status of incomplete nodes in the normal non-import path.
4. The existing comment at `import_controller.go:422` says `"not blanket Succeeded"` — the seed status field is only set when `msSeed.Status != ""`, so leaving it empty for incomplete nodes requires only an `if isComplete { ... setStatus from seed }` branch, not an explicit `setStatus("")`.

**Verdict: Use `Status.Phase=""` (empty string). Do NOT set `ValidationState` for incomplete Plans (the `Validated` stamp at line 533 is part of the complete-node path and must be omitted for incomplete ones).**

The complete-node path for Plans already does this:
```go
// import_controller.go:521-533
if plSeed.Status != "" {
    pl.Status.Phase = plSeed.Status
    pl.Status.ValidationState = "Validated"  // only for complete nodes
    r.Status().Patch(ctx, pl, statusPatch)
}
```

For an incomplete Plan, `plSeed.Status` should be `""` in the seed, so the entire `if` block is skipped. This means the incomplete Plan gets neither a Phase stamp nor a ValidationState stamp — it is truly fresh, and the plan_controller's wave-materialization gate (`plan.Status.ValidationState != "Validated"`) correctly blocks wave materialization until the plan-planner re-runs and stamps `Validated`. [VERIFIED: import_controller.go:519-538; plan_controller.go:1045]

**Summary table — what to set for incomplete nodes:**

| Level | Status.Phase | ValidationState | Notes |
|-------|-------------|-----------------|-------|
| Milestone (incomplete) | `""` | n/a | No stamp; planner re-dispatches when ImportComplete fires |
| Phase (incomplete) | `""` | n/a | No stamp; planner re-dispatches when ImportComplete fires |
| Plan (incomplete) | `""` | DO NOT set Validated | No stamp; plan-planner re-dispatches; wave gate stays closed |
| Task | Tasks are never in the seed manifest | — | Task CRDs come only from reporter/envelope; not seeded |

**The adoption gate for complete nodes is unaffected:** complete nodes are materialized with `Status.Phase = seed.Status` (e.g. "Succeeded" or "Running") as today, which trips the terminal short-circuit or Running branch and prevents a fresh planner dispatch. This path is unchanged.

---

### OQ-2: Dependency Consistency for Re-Planned (Incomplete) Nodes

**Finding: Identity is preserved (UID and name are stable), but re-planned children get NEW UIDs. Adopted dependents reference SCOPE NAMES (Plan/Phase names), not Task UIDs, so the new-UID children are correctly wired. One landmine exists at the Task level.**

Tracing `computeGlobalIndegree` and `buildGlobalEdges`:

`computeGlobalIndegree` (task_controller.go:1290-1338) builds a `buildScopeResolver` from the current in-memory lists of Tasks, Plans, Phases, Milestones. It resolves each `DependsOn` entry by scope name — not by UID. A DependsOn like `"plan-foo"` expands to all Tasks currently in the namespace that have `spec.planRef == "plan-foo"`.

`buildGlobalEdges` (depgraph.go:201-263) follows the same pattern for wave derivation. It is called every reconcile from `assembleProjectDepGraph` in project_controller.go, so the edge set is always freshly derived.

**Why the re-planned node's children stay consistent:**

When an incomplete node (e.g. a Plan named `"plan-foo"`) is re-planned:
1. Its name stays the same: `"plan-foo"`.
2. Its UID is new (assigned at `client.Create` time, same as any fresh CR).
3. The plan-planner Job is dispatched, authors a new envelope, and the reporter materializes new Task CRDs with their own UIDs.
4. `buildScopeResolver` builds `tasksByPlan["plan-foo"] = [new-task-1, new-task-2, ...]` on every reconcile.
5. A completed Task (adopted) whose `spec.dependsOn` includes `"plan-foo"` will resolve to the NEW tasks in `computeGlobalIndegree`, and its indegree remains blocked until those new tasks Succeed.

**This is the correct behavior.** The adopted dependent stays blocked on the re-planned node's new children, and releases only when they complete. The global indegree model re-derives on every reconcile, so there is no staleness.

**The one landmine — cross-plan Task-to-Task DependsOn by old UID:**

If an adopted completed Task has a `spec.dependsOn` that references a specific old Task name (e.g. `"task-plan-foo-1"`) that was authored under the original run, and that exact Task name no longer exists in the re-planned subtree (because the plan-planner re-authored with different task names), then `resolveScope` returns empty → `computeGlobalIndegree` increments by 1 (conservative: unresolved = unsatisfied). The dependent task is permanently blocked.

**Severity assessment:** This landmine only triggers if: (a) a completed adopted Task has a direct-by-name `dependsOn` on a specific Task from a re-planned Plan AND (b) the re-plan produces different Task names. In the run #2 salvage bundle, cross-plan dependencies are expressed at the Plan level (`"plan-foo"` not `"task-plan-foo-1"`), so this path should not trigger for the typical case. However, it is a real risk for trees with fine-grained task-level cross-plan edges. [VERIFIED: depgraph.go:107-151, task_controller.go:1310-1325]

**Action:** Document this as a known limitation in code comments. The fix for Phase 30 does not need to solve it — the run #2 salvage bundle uses coarse Plan-level dependsOn. If task-level cross-plan edges appear in practice, that is a separate research question.

**Summary:** As long as dependsOn references use scope names (Plan/Phase names), not specific Task names, re-planned nodes regenerate their children with new UIDs and the global indegree model re-derives correctly on every reconcile. No state update or wave-cache invalidation is needed — the re-derive model (no cached schedule) handles it automatically. [VERIFIED: import_controller.go:28-30 "NEVER creates Wave CRs (D-09)"; task_controller.go:1290-1338]

---

### OQ-3: Project-Guard Tightening Shape (Fork 3)

**Finding: The correct signal is `ImportComplete=True` already checked at line 1078-1084 — but it only blocks dispatch BEFORE the condition fires. Post-ImportComplete, the Job-presence guard at 1037-1052 is the only remaining gate. The fix is: add an additional early-return arm BEFORE the Job-presence check that checks `ImportComplete=True` plus the presence of owned child Milestones.**

Tracing `reconcileProjectPlannerDispatch` (project_controller.go:997-1052):

```go
// Step 1: Terminal short-circuit
switch project.Status.Phase {
case PhaseComplete, PhaseInitFailed:
    return  // already done
}

// Step 2: On Running — check Job terminal state
if project.Status.Phase == "Running" {
    ... // job-terminal check
}

// Step 2b: Idempotency guard — Job-presence-based
{
    r.Get(..., jobName, &existingJob)
    if err == nil {
        return ctrl.Result{}, nil  // Job exists → skip
    }
}

// Import hold (lines 1078-1084)
if project.Spec.ImportSource != nil {
    if ImportComplete != True {
        return  // park
    }
}
```

The import hold at 1078-1084 blocks dispatch UNTIL `ImportComplete=True`. Once it fires, the hold releases and dispatch proceeds. But the Job-presence check at Step 2b fires FIRST (the check is above the import hold in the code). If no planner Job exists (because import happened in a fresh cluster where the project planner never ran), the Job-presence guard passes, and the project planner dispatches.

Run #2 confirmed this: a `tide-project` planner Job fired post-`ImportComplete`. This is the defect.

**The fix shape:**

Add a new early-return arm immediately after Step 2b (or as part of Step 2b) that detects: "This project has ImportSource AND ImportComplete=True AND has owned Milestone children." When all three are true, the project planner has already been supplanted by the import materialization and must not re-dispatch.

```go
// New: import adoption guard (post-ImportComplete)
// Position: AFTER the terminal short-circuit and Running branch, BEFORE Step 2b
// or as a new check BEFORE Job creation in Step 2b.
if project.Spec.ImportSource != nil {
    c := meta.FindStatusCondition(project.Status.Conditions, ConditionImportComplete)
    if c != nil && c.Status == metav1.ConditionTrue {
        // Import complete: check if milestones have already materialized.
        // If any Milestone with spec.projectRef == project.Name exists,
        // the import tree is the authoritative materialization.
        var msList MilestoneList
        r.List(ctx, &msList, client.InNamespace(project.Namespace))
        for _, ms := range msList.Items {
            if ms.Spec.ProjectRef == project.Name {
                return ctrl.Result{}, nil  // imported; project planner must not re-dispatch
            }
        }
    }
}
```

**Why this doesn't regress N>1 milestones:** The existing comment at lines 1037-1043 explains that a count-based guard would abort mid-stream (N milestones materialize incrementally). But this new guard checks ONLY when `ImportComplete=True`, not during the incremental materialization window. By the time `ImportComplete=True` fires, ALL milestones in the seed manifest have been materialized (the `reconcileCreatingCRs` loop materializes all of them atomically before transitioning to CopyingEnvelopes). So the `len(msList.Items) > 0` check is safe post-`ImportComplete`. [VERIFIED: import_controller.go:388-431, 567-578]

**Alternative position:** The guard can also go between the import hold check (line 1078) and the pool acquire (line 1087). This is cleaner — once through the import hold, add a second check: "if we got here AND import is complete AND milestones exist, skip dispatch." This avoids touching the Step 2b comment block.

**The existing comment-described concern (N milestones mid-stream) is NOT applicable to this new guard** because the new guard gates on `ImportComplete=True`, which only fires after all CRs are materialized. [VERIFIED: import_controller.go:701-706 `succeedImport`]

---

## Architecture Patterns

### The per-node branch in reconcileCreatingCRs

The branch is structural: `reconcileCreatingCRs` currently applies the same `if msSeed.Status != "" { ... }` pattern for all three levels (Milestone, Phase, Plan). The fix adds a `isComplete` flag sourced from the seed manifest.

However, the seed manifest today stores `Status` as a string — it does not carry the `isComplete` flag explicitly. The completeness information lives in the `tide-import` Job's copy of `isEnvelopeComplete`, which runs at Job time (after CRs are materialized). The ImportController sees only the seed manifest.

**The signal available at reconcileCreatingCRs time is: the seed entry's `Status` field.**

In the current export tooling (Phase 29), the seed manifest is generated from the live CRD state. A node that is `Running` with a complete envelope has `Status: "Running"`. A node that is `Running` with an incomplete envelope also has `Status: "Running"` — the seed does not carry completeness.

**This means the completeness branch cannot be purely status-based from the seed.** The seed must be extended (or the tooling must be extended) to carry the completeness signal explicitly.

**Two sub-options (Claude's Discretion):**

1. **Add a boolean `Incomplete bool` field to `seedEntry`**: the export tooling checks `isEnvelopeComplete` at export time and sets `Incomplete: true` for nodes whose envelopes failed the guard. `reconcileCreatingCRs` reads `seed.Incomplete` and skips the status patch for those nodes. This is a one-field schema extension to the seed manifest and the export CLI.

2. **Set `Status: ""` in the seed for incomplete nodes**: the export tooling leaves `Status` empty for nodes that fail `isEnvelopeComplete`. `reconcileCreatingCRs` already skips the status patch when `Status == ""` (the `if msSeed.Status != "" { ... }` guard). No schema change needed — the existing empty-status logic does the right thing.

**Option 2 is strongly preferred**: zero schema change, the existing guard in `reconcileCreatingCRs` does the right thing automatically, and the fix is entirely in the export tooling's seed generation. [VERIFIED: import_controller.go:424-430 for Milestone; 471-477 for Phase; 519-537 for Plan]

The export tooling is in `cmd/tide/cmd/export_envelopes.go` (or equivalent). This must be verified to confirm the seed-generation path that sets `Status`. [ASSUMED — export tooling path not read in this session]

### Data flow after the fix

```
tide export-envelopes
  → for each node: if isEnvelopeComplete → set seed.Status = live Phase
                   else                  → set seed.Status = "" (or omit)
  → writes seed-manifest.json with mixed Status values

ImportController.reconcileCreatingCRs
  → for each seed entry:
      client.Create (new UID, name preserved)
      if seed.Status != "":           ← existing guard, unchanged
          patch Status.Phase = seed.Status  (adopt: "Running"/"Succeeded")
          if Plan: patch ValidationState = "Validated"
      else:                           ← incomplete path (now exercised)
          (no patch: Status.Phase stays "")
  → writes rekey table (all nodes, complete + incomplete)

tide-import Job
  → for each rekey entry:
      if isEnvelopeComplete → copy envelope  (unchanged)
      else                  → report.Incomplete++ (unchanged)
  → exit 0 (success even with incomplete nodes, IMPORT-02 still works)

ImportController sets ConditionImportComplete=True

Controllers unblock:
  → Nodes with Status="" enter normal fresh-authoring path
  → Nodes with Status="Running"/"Succeeded" take the terminal short-circuit (adopted)
```

[VERIFIED for controller state machine paths above; ASSUMED for export tooling behavior]

---

## Standard Stack

No new packages. This phase is a code-only change to existing controllers and the export CLI.

## Package Legitimacy Audit

No external packages introduced. Section skipped.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Tracking completeness in the seed | A new CRD field or ConfigMap | Empty `Status` field in seedEntry | The existing `if status != ""` guard in reconcileCreatingCRs already handles it |
| Dependency consistency for re-planned nodes | Cache invalidation or wave-cache rewrite | The existing rederive-on-every-reconcile model | The Wave is always re-derived (D-09); no cached schedule to invalidate |
| Project adoption detection | A new condition on Project | `ImportComplete=True` + owned Milestone check | Both signals are already in-memory on every reconcile |

---

## Common Pitfalls

### Pitfall 1: Seeding `Status="Pending"` for incomplete nodes

**What goes wrong:** Setting `Status.Phase="Pending"` explicitly on incomplete nodes appears harmless, but "Pending" is not a defined constant at the Milestone/Phase/Plan level — it is defined only for Project. If future code adds a `case "Pending"` short-circuit to the milestone or phase controller, these nodes would silently park.

**How to avoid:** Use `Status.Phase=""` (empty string, i.e. skip the status patch entirely). This is the K8s zero-value and is what every fresh CR starts with.

### Pitfall 2: Stamping `ValidationState="Validated"` on incomplete Plans

**What goes wrong:** The `import_controller.go:533` line stamps `ValidationState="Validated"` on Plans only inside the `if plSeed.Status != ""` block. If this is mistakenly applied to incomplete Plans, `reconcileWaveMaterialization` bypasses the `ValidationState != "Validated"` gate and tries to derive waves from zero Tasks — which ComputeWaves handles (zero nodes = no waves), but the plan then tries to push-and-succeed immediately with no Tasks, which is wrong.

**How to avoid:** The fix is to ensure the `Validated` stamp lives entirely inside the complete-node path. The current code structure already does this IF `plSeed.Status == ""` for incomplete nodes (the whole block is skipped). No new guard is needed if the export tooling correctly omits `Status` for incomplete nodes.

### Pitfall 3: Breaking the N>1-milestone incremental-materialization case

**What goes wrong:** Checking `len(ownedMilestones) > 0` as the project-planner idempotency guard WITHOUT gating on `ImportComplete=True` would fire while milestones are still being materialized mid-stream, aborting the project planner before all milestones are created.

**How to avoid:** The new guard (Fork 3) MUST be inside a `if ImportComplete=True` branch. That condition only fires after `reconcileCreatingCRs` completes for all nodes. [VERIFIED: import_controller.go:567-578 transitions to CopyingEnvelopes only after ALL CRs are materialized]

### Pitfall 4: Task-name-level cross-plan DependsOn landmine

**What goes wrong:** A completed adopted Task with `spec.dependsOn` referencing a specific Task name from a re-planned Plan will permanently block if the re-plan authors Tasks with different names.

**How to avoid:** Document as a known limitation. In the run #2 salvage tree, cross-plan edges use Plan-level scope names. If a future tree has task-level cross-plan edges, this requires additional investigation (not in scope for Phase 30).

### Pitfall 5: tide-import Job exit-code contract vs. incomplete nodes

**What goes wrong:** The `tide-import` binary currently exits 0 even when some envelopes are incomplete (it reports `incomplete` count but does NOT fail the Job). This is correct behavior — the Job should still succeed and let the controller set `ImportComplete=True`. If someone changes the Job to exit non-zero on any incomplete node, the `ImportComplete=True` condition never fires and all nodes remain permanently parked.

**How to avoid:** Do NOT change the `tide-import` Job's exit-code contract. The `report.Incomplete` counter is informational only. [VERIFIED: cmd/tide-import/main.go:295 exits 0 regardless of incomplete count]

### Pitfall 6: Rekey table for incomplete nodes

**What goes wrong:** The rekey table is built from ALL seed entries (complete + incomplete). For an incomplete node, the rekey table contains a row with `OldUID` and `NewUID`, but the `tide-import` Job will skip copying the envelope. This means the rekey table row exists but is effectively unused for incomplete nodes. The `tide-import` binary handles this cleanly — it reads `out.json`, calls `isEnvelopeComplete`, and skips without touching the destination directory. The destination UID directory for an incomplete node is never created by the import Job.

**Consequence:** When the re-planned incomplete node later gets a new envelope (from its fresh planner dispatch), the reporter materializes Tasks using the NEW UID path. The incomplete node's rekey table row is harmless but vestigial.

**How to avoid:** No action needed. The existing behavior is correct. [VERIFIED: cmd/tide-import/main.go:204-226]

---

## Existing Test Coverage Gap (Fork 4)

### What `import_resume_test.go` currently asserts (Tier b)

Tier b (`importResumeSalvageNS`) does:
1. Imports salvage-20260618 bundle (60 complete + 40 incomplete envelopes).
2. Waits for `ImportComplete=True`.
3. Asserts 0 planner Jobs for `level=milestone` (15-second Consistently window).
4. Asserts 0 planner Jobs for `level=phase` (15-second Consistently window).
5. Asserts `CostSpentCents == 0` immediately post-import.

**What Tier b does NOT assert:**
- That the 40 incomplete Plan nodes eventually get re-planned (planner Jobs DO dispatch for plan level — "D-17: plan planners legitimately re-run").
- That the re-planned Plans materialize Tasks.
- That the Tasks execute and Succeed.
- That the Project reaches `Status.Phase=Complete`.
- That the adopted complete nodes (their Tasks) are not re-dispatched.

The comment at line 37-39 explicitly says `D-17: we do NOT assert plan-level planners — those re-run.` This is the coverage gap that allowed the zombie stall to ship green.

### What the new Tier c must assert

A new `Tier c` test must:
1. Import a PARTIAL bundle (some complete envelopes, some incomplete).
2. Wait for `ImportComplete=True`.
3. Assert that incomplete nodes (Plans with `Status.Phase=""`) eventually get re-planned (planner Jobs appear for those Plans).
4. Assert that re-planned Plans materialize Tasks.
5. Assert that the Project reaches `Status.Phase=Complete` (or all Milestones reach `Succeeded`).

### Fixture shape for Tier c

**Option A: Reuse salvage-20260618 bundle (run #2 fixture)**

Pros: Real-world shape, already present in the repo.

Cons: Large (60 complete + 40 incomplete Milestones/Phases/Plans across a multi-milestone tree); driving to `Project=Complete` requires all 40 re-plans to complete with stub subagents; test runtime is significant (potentially 15+ minutes).

**Option B: Minimal hand-authored mixed fixture**

Create a small fixture (1 Milestone / 2 Phases / 4 Plans) where 2 Plans have complete envelopes (with Tasks) and 2 Plans have no envelopes. This drives the critical path — adopt 2, re-plan 2, all Tasks succeed, Project completes — in a controlled, fast test.

Pros: Fast, deterministic, no dependency on the large salvage bundle.

Cons: Does not test the real run #2 shape.

**Recommended (Claude's Discretion):** Option B for the kind Tier c test. The salvage bundle is already exercised by Tier b (gate assertion). Tier c needs to prove the end-to-end outcome, not the specific salvage shape. A minimal fixture is more reliable and faster.

The fixture should live at `test/integration/kind/testdata/import-partial-fixture/` and contain:
- `project.yaml` with `spec.importSource` pointing to the seed ConfigMap.
- `pvc-envelopes.tgz` with envelopes for the complete Plans only.
- `seed-manifest.json` with all 4 Plans, 2 with `status: "Running"` and 2 with `status: ""`.
- Pre-authored `out.json` for the 2 complete Plans (Tasks created by stub subagents).

An envtest-level test could also cover the partial-tree materialization path cheaply (just asserts that incomplete nodes get `Status.Phase=""` and complete nodes get the salvaged status), separate from the kind Tier c that goes all the way to `Complete`.

### envtest coverage for the new branch (import_controller_test.go extension)

The existing `import_controller_test.go` has Test 1 (adoption happy path) and Test 2 (cycle detection). A new **Test N (partial-tree materialization)** should:
- Create a seed manifest where 2 Plans have `status: "Running"` and 2 Plans have `status: ""`.
- Drive `reconcileCreatingCRs`.
- Assert: Plans with `status: "Running"` get `Status.Phase = "Running"` AND `ValidationState = "Validated"`.
- Assert: Plans with `status: ""` get `Status.Phase = ""` AND `ValidationState = ""`.

This is a fast envtest test that directly verifies the per-node branch without a kind cluster.

---

## Runtime State Inventory

This is a code-change phase, not a rename/migration phase. Section is omitted per the trigger condition.

---

## Environment Availability

The `kind-tide-dogfood` cluster is already up (v1.0.4 chart, per FINDINGS.md). The fix requires no new cluster state — the test uses a fresh namespace. The `make test-int` workflow is the standard validation path.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| kind-tide-dogfood cluster | Fork 4 Tier c test | Available (per FINDINGS.md) | v1.0.4 chart | Rebuild with `make kind-create` |
| tide binary (bin/tide) | kind Tier c test | Available via `make test-int-kind-prep` | — | Build first |
| salvage-20260618 bundle | Tier b (existing) | Present at examples/projects/dogfood/salvage-20260618/ | — | — |
| Partial fixture | Fork 4 Tier c (new) | Not yet created (Wave 0 gap) | — | Hand-author in Wave 0 |

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (kind layer), envtest (unit layer) |
| Config file | test/integration/kind/kind_suite_test.go (kind); internal/controller/suite_test.go (envtest) |
| Quick run command | `go test ./internal/controller/ -run TestImport -v -count=1` |
| Full suite command | `make test-int` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| IMPORT-BRANCH-01 | Incomplete node gets Status.Phase="" | unit/envtest | `go test ./internal/controller/ -run "TestImportController.*partial" -v` | No — Wave 0 gap |
| IMPORT-BRANCH-02 | Complete node gets salvaged status + Validated | unit/envtest | same | No — Wave 0 gap |
| IMPORT-BRANCH-03 | Incomplete Plan skips ValidationState=Validated | unit/envtest | same | No — Wave 0 gap |
| IMPORT-GUARD-01 | Project planner does not re-dispatch post-ImportComplete | unit/envtest or kind | `go test ./internal/controller/ -run "TestProjectPlanner.*import" -v` | No — Wave 0 gap |
| IMPORT-E2E-01 | Partial import drives to Project=Complete (Tier c) | kind E2E | `make test-int-kind` (Label: kind, long) | No — Wave 0 gap |

### Wave 0 Gaps
- [ ] `test/integration/kind/testdata/import-partial-fixture/` — hand-authored partial fixture bundle for Tier c
- [ ] `internal/controller/import_controller_test.go` — Test N (partial-tree materialization branch assertions)
- [ ] `test/integration/kind/import_resume_test.go` — Tier c spec (partial-to-complete E2E)

---

## Security Domain

This phase touches the import controller (CR materialization) and export tooling (seed generation). No new auth surfaces, no new network calls, no cryptographic operations. The existing path-traversal defense in `tide-import` is unchanged. The K8s RBAC for `ImportReconciler` is unchanged.

ASVS V5 (Input Validation) is the only relevant category — the seed manifest is operator-supplied input, and the existing `containedJoin` + Kind allowlist guards remain in force. No new untrusted input surfaces are introduced.

---

## Code Examples

### Complete/incomplete branch in reconcileCreatingCRs (schematic)

The current structure for Plans (lines 518-537) is:
```go
if plSeed.Status != "" {
    statusPatch := client.MergeFrom(pl.DeepCopy())
    pl.Status.Phase = plSeed.Status
    pl.Status.ValidationState = "Validated"  // arm wave path
    r.Status().Patch(ctx, pl, statusPatch)
}
```

For incomplete nodes, `plSeed.Status == ""` (export tooling leaves it empty), so this entire block is skipped. The Plan CR is created with `Status.Phase = ""` and `ValidationState = ""`. No additional code change in `reconcileCreatingCRs` is needed beyond the export tooling change that populates `plSeed.Status = ""` for incomplete nodes.

The same pattern applies to Milestones (lines 424-430) and Phases (lines 471-477) — both already gate on `msSeed.Status != ""` / `phSeed.Status != ""`.

### Project-guard tightening (schematic)

Insert after the import hold at project_controller.go:1084 (or as a new arm between 1084 and the pool acquire at 1087):

```go
// Post-ImportComplete adoption guard: if import is complete and Milestones
// already exist, the import tree is the authoritative materialization.
// The project planner must not re-dispatch. This is distinct from the
// pre-ImportComplete hold above: that hold parks until import fires; this
// guard permanently skips project-planner dispatch once import has succeeded
// and milestone materialization is confirmed. Not gated on Milestone count
// (safe because ImportComplete=True fires only after all CRs are materialized
// in reconcileCreatingCRs, so the milestone list is always complete here).
if project.Spec.ImportSource != nil {
    if importCond := meta.FindStatusCondition(project.Status.Conditions,
        tidev1alpha2.ConditionImportComplete); importCond != nil &&
        importCond.Status == metav1.ConditionTrue {
        var msList tidev1alpha2.MilestoneList
        if listErr := r.List(ctx, &msList, client.InNamespace(project.Namespace)); listErr == nil {
            for i := range msList.Items {
                if msList.Items[i].Spec.ProjectRef == project.Name {
                    return ctrl.Result{}, nil // import adopted; project planner must not re-dispatch
                }
            }
        }
    }
}
```

[Source: verified structure of project_controller.go:1078-1092 and import_controller.go:567-578]

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Export tooling (cmd/tide/cmd/export_envelopes.go) sets seedEntry.Status from the live CR status at export time, and can be changed to set Status="" for incomplete envelopes | Architecture Patterns | If the export tooling does not read live CR status, a different signal path is needed |
| A2 | The partial fixture Tier c test should live at testdata/import-partial-fixture/ as a hand-authored bundle | Validation Architecture | Design choice; Claude's Discretion |
| A3 | The run #2 salvage-20260618 bundle's cross-plan DependsOn edges are Plan-level (not Task-name-level), so the UID-mismatch landmine does not trigger | Dependency Consistency | If task-level edges exist in the bundle, some adopted Tasks may be permanently blocked post-re-plan |

**If this table is non-empty:** A1 requires verification against the export tooling source before implementing. A3 can be verified by `grep -r "dependsOn" examples/projects/dogfood/salvage-20260618/` on the actual bundle.

---

## Open Questions

1. **Export tooling path for seed.Status population**
   - What we know: `cmd/tide-import/main.go:isEnvelopeComplete` is the completeness signal in the binary.
   - What's unclear: Does the export CLI (tide export-envelopes) read the envelope to determine completeness when generating the seed manifest, or does it use live CR status?
   - Recommendation: Read `cmd/tide/cmd/export_envelopes.go` before implementing Wave 0. If the seed is built from live CR status (not envelope inspection), the fix requires the export CLI to also inspect envelopes and set `Status=""` for incomplete ones.

2. **Tier c test: stub subagent behavior for re-planned incomplete Plans**
   - What we know: Tier a (small fixture) uses stub subagents to drive milestones to Succeeded.
   - What's unclear: Do the existing stub subagents in the kind test infra also handle plan-level re-planning correctly for the partial fixture?
   - Recommendation: Use the same stub subagent pattern from Tier a. The partial fixture Plans with `Status=""` will get re-planned by plan-planners; stub subagents should materialize Tasks that Succeed.

---

## Sources

### Primary (HIGH confidence)
All findings in this research are verified directly from the codebase. No external sources were needed.

- `internal/controller/import_controller.go` — reconcileCreatingCRs, state machine, status patching (lines 385-538)
- `internal/controller/milestone_controller.go` — status machine, import hold, idempotency guard (lines 230-380)
- `internal/controller/phase_controller.go` — status machine, import hold, idempotency guard (lines 234-380)
- `internal/controller/plan_controller.go` — status machine, import hold, ValidationState guard (lines 240-395, 1035-1080)
- `internal/controller/project_controller.go` — project planner dispatch, idempotency guard (lines 997-1084)
- `internal/controller/depgraph.go` — buildScopeResolver, resolveScope, buildGlobalEdges (lines 57-264)
- `internal/controller/task_controller.go` — computeGlobalIndegree, checkReadinessGates (lines 460-507, 1266-1338)
- `cmd/tide-import/main.go` — isEnvelopeComplete, completeness guard, exit code contract (lines 315-331, 204-226)
- `api/v1alpha2/project_types.go` — PhasePending, PhaseComplete constants (lines 417-441)
- `api/v1alpha2/shared_types.go` — ConditionImportComplete, ReasonImportSucceeded (lines 265-289)
- `test/integration/kind/import_resume_test.go` — Tier a and Tier b coverage (full file)

### Secondary (MEDIUM confidence)
- `.planning/dogfood/run-2-FINDINGS.md` — run #2 evidence, defect description, fix shape

---

## Metadata

**Confidence breakdown:**
- Fork 1 (re-plannable status): HIGH — traced every state machine branch in 5 controllers
- Fork 3 (project-guard tightening): HIGH — traced reconcileProjectPlannerDispatch exactly
- Dependency consistency: HIGH — traced computeGlobalIndegree and buildGlobalEdges
- Test coverage gap: HIGH — read Tier b in full; the coverage gap is explicit
- Export tooling path: LOW (A1) — not read in this session; must verify before implementing

**Research date:** 2026-06-25
**Valid until:** 2026-07-25 (30 days; codebase is stable)
