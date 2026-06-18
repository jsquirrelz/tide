# Architecture Research: Plan-Import / Envelope Resumption

**Domain:** TIDE controller integration — plan-import and envelope resumption for v1.0.3
**Researched:** 2026-06-18
**Confidence:** HIGH (based on direct code reading of all five integration sites)

---

## Context: The UID-Churn Crux

Every TIDE CR gets a K8s-assigned UID at creation time. The PVC envelope path is:

```
/workspaces/<projectUID>/workspace/envelopes/<parentUID>/{in.json,out.json,children/*.json}
```

A salvaged run (the dogfood `salvage-20260618` tree — 3 milestones, 15 phases, 42 plans,
~$90 of planning) has envelopes keyed by the OLD UIDs from the aborted run. A fresh
`kubectl apply` of a new Project gives every CR a new UID assigned by the K8s API server.
The salvaged paths and the new CRs point to entirely different address spaces on the PVC.
This is the crux that any import approach must solve.

---

## Existing Dispatch / Skip / Resume Flow (as-built)

### Planner-dispatch skip check — where it lives

All five planner dispatch sites follow the same pattern. The canonical reference is
`MilestoneReconciler.reconcilePlannerDispatch` in `internal/controller/milestone_controller.go`.
The skip check is at Step 2b (lines ~304-326):

```
Step 2b: Idempotency guard — skip NEW planner dispatch when the Milestone
         already has >=1 child Phase (matched by spec.milestoneRef or ownerRef).
```

For the Project level, `reconcileProjectPlannerDispatch` in `project_controller.go`
(lines ~988-995) uses a different guard: it checks whether a planner Job named
`tide-project-<uid>-1` already exists in etcd.

In both cases, the system has **no awareness of pre-existing envelopes** on the PVC. The
skip is triggered entirely by K8s objects (child CRs or the planner Job itself). There is no
"does out.json exist?" pre-check before dispatching.

### Envelope read path

`FilesystemEnvelopeReader.ReadOut` (`internal/dispatch/podjob/backend.go` lines 94-105)
constructs the path purely from `(projectUID, taskUID)` arguments:

```go
path := filepath.Join(r.WorkspaceRoot, projectUID, "workspace", "envelopes", taskUID, "out.json")
```

The reporter job (`BuildReporterJob` in `reporter_jobspec.go`) mounts the PVC at
`SubPath: fmt.Sprintf("%s/workspace", project.UID)` and receives flags
`--project-uid=<project.UID>` and `--task-uid=<parentUID>`. The path key is always
**the current UID at the time of the call**. Salvaged envelopes under old UIDs are
unreachable without an explicit bridge.

### "Valid envelope" check

There is no explicit validity check beyond "does `out.json` parse as `EnvelopeOut`?".
The `EnvelopeOut.TaskUID` field echoes back from `EnvelopeIn.TaskUID`, but no reconciler
validates `out.TaskUID == currentObject.UID`. The caller treats any parseable `out.json`
at the expected path as authoritative. A copied envelope re-keyed to the new UID path is
treated as valid as long as the JSON parses — the import seam can exploit this.

---

## Candidate Approaches

### Approach A: Name-Based / Stable-Plan-Key Envelope Lookup

**Core idea:** Change `FilesystemEnvelopeReader.ReadOut` to search for an existing envelope
by a fallback key derived from the CR's `metadata.name` rather than only by UID.

**Where it hooks:** `FilesystemEnvelopeReader.ReadOut` would grow a secondary resolution
path:

```
primary:  workspaceRoot/<projectUID>/workspace/envelopes/<objectUID>/out.json
fallback: workspaceRoot/<projectUID>/workspace/envelopes/by-name/<objectName>/out.json
```

The planner-skip checks at Step 2b would also need updating to call this fallback read
and treat a hit as "already completed" — currently they test only child CR existence.

**New Project spec state needed:**
If the salvaged envelopes are in a different project's workspace (different PVC subPath),
a `spec.importSource.pvcSubPath` field is required. For same-project salvage, no new
field is needed.

**Why Approach A breaks down for `salvage-20260618`:**
The salvaged `pvc-envelopes.tgz` contains envelopes at `envelopes/<oldUID>/out.json` paths
only — the original TIDE run never wrote `envelopes/by-name/<name>/` paths, because that
path structure did not exist. Approach A therefore requires a migration step to create
by-name copies or symlinks. This migration is functionally equivalent to what Approach B
does explicitly, but with added permanent complexity to the hot read path (dual-path
resolution on every `ReadOut` call, cross-project pollution risk if name-uniqueness is not
enforced globally). Approach A is the right choice only if the system had been writing
by-name paths from the start.

---

### Approach B: UID-Rewrite Import Step (RECOMMENDED)

**Core idea:** A one-shot pre-reconcile step reads the salvage manifest, materializes the
CR tree in K8s (which assigns new UIDs), then dispatches a `tide-import` Job that copies
the old-UID envelope trees to the new-UID paths. After the Job succeeds, the normal
reconcile flow finds:
- Child CRs already present in etcd (skip guards fire naturally).
- Envelopes at the correct new-UID paths (ReadOut succeeds).

The five reconcilers and the reporter job are **unchanged** — they see exactly what they
would see after a normal planner run. No special import logic runs in the steady-state
reconcile.

**Why Approach B is correct:**
- The existing planner-skip guards are proven and battle-tested across 26 phases. They test
  child CR existence + Job/envelope presence. Approach B satisfies both conditions without
  modifying the guards.
- The envelope path contract (`envelopes/<uid>/out.json`) is used by five reconcilers, the
  reporter job, and two `EnvelopeReader` implementations. Changing the path scheme risks
  regression across all of them. Approach B leaves the path contract untouched.
- The salvaged tgz already has old-UID paths. Approach B re-keys them as a one-time step.

---

## Recommended Architecture: Approach B in Detail

### System Overview

```
  IMPORT PHASE (one-shot, pre-reconcile, gated by ImportComplete condition)
  ┌──────────────────────────────────────────────────────────────────────────┐
  │  Operator creates Project with spec.importSource set                     │
  │                                                                          │
  │  ImportController (new — internal/controller/import_controller.go)       │
  │    State machine: (absent) → Pending → CreatingCRs                       │
  │                              → CopyingEnvelopes → Complete / Failed      │
  │                                                                          │
  │  CreatingCRs step:                                                       │
  │    Reads SeedManifestConfigMap (name → oldUID mapping)                   │
  │    kubectl-creates Milestone/Phase/Plan/Task CRs from seed               │
  │    K8s assigns NEW UIDs to each CR                                       │
  │    Records (name → oldUID, name → newUID) rekey table in a ConfigMap     │
  │                                                                          │
  │  CopyingEnvelopes step:                                                  │
  │    Dispatches tide-import Job (cmd/tide-import/main.go)                  │
  │      mounts salvage PVC subPath (old envelopes) read-only                │
  │      mounts new project PVC subPath read-write                           │
  │      for each (oldUID, newUID) pair:                                     │
  │        cp -n envelopes/<oldUID>/ envelopes/<newUID>/  (no-clobber)       │
  │        patch out.json: if taskUID != newUID → rewrite to newUID          │
  │    Sets ImportComplete=True condition on Project.Status                  │
  └──────────────────────────────────────────────────────────────────────────┘
              │
              │ ImportComplete=True
              ▼
  NORMAL RECONCILE PHASE (existing, UNCHANGED)
  ┌──────────────────────────────────────────────────────────────────────────┐
  │  ProjectReconciler.reconcileProjectPlannerDispatch                       │
  │    Step 2b: planner Job tide-project-<newUID>-1 not found                │
  │    Project.Status.Phase == Running                                        │
  │    → handleProjectJobCompletion called via TTL/GC branch                 │
  │      (project_controller.go ~970-975: "envelope lives on PVC keyed by   │
  │       UID, not on the Job; fall through to completion")                  │
  │    → ReadOut succeeds at envelopes/<newProjectUID>/out.json              │
  │    → reporter Job spawned, reads out.json, materializes child Milestones │
  │      (already in etcd with new UIDs — import created them)               │
  │                                                                          │
  │  MilestoneReconciler Step 2b:                                            │
  │    Child Phase CRs already exist (spec.milestoneRef match)               │
  │    → planner dispatch skipped; succession proceeds                       │
  │                                                                          │
  │  ... repeats at Phase → Plan levels ...                                  │
  │                                                                          │
  │  TaskReconciler:                                                         │
  │    Task CRs from seed carry DependsOn edges                              │
  │    → assembleProjectDepGraph picks them up on first reconcile            │
  │    → global wave schedule derived normally by deriveGlobalWaves          │
  │    → Tasks with existing out.json are treated as completed               │
  │      (wave derivation advances based on Task.Status.Phase == Succeeded)  │
  └──────────────────────────────────────────────────────────────────────────┘
```

### Component Boundaries

| Component | File | New or Modified | Responsibility |
|-----------|------|-----------------|----------------|
| `ImportSourceRef` struct | `api/v1alpha2/project_types.go` | NEW field on `ProjectSpec` | Operator-declared reference: seed ConfigMap name + salvaged PVC subPath |
| `ImportController` | `internal/controller/import_controller.go` | NEW controller | State machine: materialize CRs, dispatch tide-import Job, set ImportComplete condition |
| `tide-import` binary | `cmd/tide-import/main.go` | NEW binary | In-pod: copies old-UID envelope trees to new-UID paths; patches `out.json` TaskUID |
| `reconcileProjectPlannerDispatch` | `internal/controller/project_controller.go` | MODIFIED (one guard) | Park if `spec.importSource != nil` and `ImportComplete != True` |
| `reconcilePlannerDispatch` | `milestone_controller.go`, `phase_controller.go`, `plan_controller.go` | MODIFIED (same guard, three sites) | Same park condition at milestone/phase/plan levels |
| `FilesystemEnvelopeReader.ReadOut` | `internal/dispatch/podjob/backend.go` | UNCHANGED | Reads `envelopes/<uid>/out.json` — import puts files there |
| `BuildReporterJob` | `internal/controller/reporter_jobspec.go` | UNCHANGED | Reporter reads envelopes keyed by new UID, which import copied |
| Wave derivation | `internal/controller/project_controller.go` | UNCHANGED | Operates on Task CRs; import materializes them via normal creation |

### New ProjectSpec Field

```go
// In api/v1alpha2/project_types.go, added to ProjectSpec:

// ImportSource, when set, activates the one-shot envelope-import path.
// After the import completes (ImportComplete condition on Status), the
// reconciler proceeds identically to a fresh project — no import logic
// runs in the steady-state reconcile.
// +optional
ImportSource *ImportSourceRef `json:"importSource,omitempty"`
```

```go
// New type in api/v1alpha2/import_types.go:

// ImportSourceRef carries the two references an import needs.
type ImportSourceRef struct {
    // SeedManifestConfigMap names a ConfigMap in the project namespace
    // carrying the seed manifest JSON (name-to-old-UID mapping for every
    // Milestone, Phase, Plan, and Task to be created).
    // +kubebuilder:validation:MinLength=1
    SeedManifestConfigMap string `json:"seedManifestConfigMap"`

    // SalvagedPVCSubPath is the sub-path within the shared tide-projects PVC
    // where the salvaged envelopes reside, e.g. "<oldProjectUID>/workspace".
    // The import Job mounts this sub-path read-only alongside the new
    // project's workspace.
    // +kubebuilder:validation:MinLength=1
    SalvagedPVCSubPath string `json:"salvagedPVCSubPath"`
}
```

### Seed Manifest ConfigMap

Carries a structured JSON document mapping each CR's stable name to its salvaged UID:

```json
{
  "apiVersion": "tideproject.k8s/v1",
  "kind": "ImportSeedManifest",
  "milestones": [
    {
      "name": "milestone-01-codex-subagent",
      "savedUID": "df5a0c1f-...",
      "dependsOn": [],
      "status": "Succeeded"
    }
  ],
  "phases": [
    {
      "name": "phase-01-provider-switch",
      "milestoneRef": "milestone-01-codex-subagent",
      "savedUID": "...",
      "dependsOn": [],
      "status": "Succeeded"
    }
  ],
  "plans": [...],
  "tasks": [...]
}
```

The `status` field is used by the ImportController to patch each CR's initial
`Status.Phase` after creation — a Milestone whose entire Phase tree is imported should be
patched to `Succeeded` immediately so BoundaryDetected succession fires on first reconcile
without waiting for children to re-complete.

The operator generates this ConfigMap from `salvage-20260618/SEED-OUTLINE.md` and the
unpacked `pvc-envelopes.tgz` using the `tide import seed` CLI helper (Phase C-01 below).

### Import Controller State Machine

```
Status.ImportPhase transitions on Project.Status.Conditions:

  (no ImportSource set) → ImportController does not watch

  ImportSource set:
    Pending          → (ImportController reconciles)
    Pending          → CreatingCRs        applies seed CRs to etcd
    CreatingCRs      → CopyingEnvelopes   dispatches tide-import Job
    CopyingEnvelopes → Complete           tide-import Job Succeeded
    *                → Failed             any terminal error

  Retry: operator adds annotation tideproject.k8s/retry-import=true
    Failed → (annotation consumed) → Pending  (re-run from start)
```

The `ConditionImportComplete` condition on `Project.Status` is the downstream gate checked
by all five planner dispatch sites.

### The ImportComplete Guard (all five dispatch sites)

Insert immediately after Step 1 (terminal short-circuit), before pool acquire and before
Job creation (Pitfall 2 compliance):

```go
// Guard: if an ImportSource is declared, park until import completes.
// Position: after terminal short-circuit, before pool acquire (Pitfall 2).
if project.Spec.ImportSource != nil {
    c := meta.FindStatusCondition(project.Status.Conditions, ConditionImportComplete)
    if c == nil || c.Status != metav1.ConditionTrue {
        logger.V(1).Info("import pending; holding planner dispatch", "project", project.Name)
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }
}
```

This guard is identical at all five planner dispatch sites
(`reconcileProjectPlannerDispatch`, `MilestoneReconciler.reconcilePlannerDispatch`,
`PhaseReconciler.reconcilePlannerDispatch`, `PlanReconciler.reconcilePlannerDispatch`,
`TaskReconciler.reconcileDispatch`).

### Data Flow: Import Path

```
Operator creates SeedManifestConfigMap + Project with spec.importSource set
    ↓
ImportController: Pending → CreatingCRs
    reads SeedManifestConfigMap
    creates Milestone/Phase/Plan/Task CRs (K8s assigns new UIDs)
    records (name → oldUID, name → newUID) in rekey ConfigMap
    patches each CR's Status.Phase to match salvaged status
    → transition to CopyingEnvelopes

ImportController: CopyingEnvelopes
    creates tide-import Job (name: tide-import-<projectUID>)
          |
          | mounts: tide-projects at subPath/<oldProjectUID>/workspace (read-only)
          | mounts: tide-projects at subPath/<newProjectUID>/workspace (read-write)
          |
          for each (oldUID, newUID) in rekey table:
            if /new/envelopes/<newUID>/out.json does not exist:
              cp -r /old/envelopes/<oldUID>/ /new/envelopes/<newUID>/
            if /new/envelopes/<newUID>/out.json.taskUID != newUID:
              rewrite taskUID field (read-modify-write with rename-atomic)
          writes completion report to termination message

ImportController: Job Succeeded → sets ConditionImportComplete=True

ProjectReconciler: next reconcile
    reconcileProjectPlannerDispatch:
      ImportComplete guard passes
      Step 2b: Job tide-project-<newUID>-1 not found
      Phase == Running (patched by ImportController)
      → handleProjectJobCompletion (TTL/GC branch, lines ~970-975)
      → ReadOut(projectUID, projectUID) → succeeds (envelope copied)
      → reporter Job spawned
      → materializes child Milestones (already in etcd, ownerRef resolution fires)

MilestoneReconciler: each Milestone
    Step 2b: child Phases found by spec.milestoneRef match
    → planner dispatch skipped
    Phase == Succeeded (patched by ImportController)
    → checkProjectComplete fires on next Project reconcile

... repeats at Phase → Plan levels ...

Wave derivation: assembleProjectDepGraph lists Tasks by project label
    → edges from Task.Spec.DependsOn (imported from seed)
    → deriveGlobalWaves computes fresh schedule from imported graph
    → Tasks with Status.Phase == Succeeded: not re-dispatched (TaskReconciler terminal short-circuit)
    → Tasks with missing status (partial salvage): normal dispatch fires
```

### Interaction with Global Execution DAG and Wave Derivation

The import path materializes Task CRs carrying the same `dependsOn` edges declared in the
seed manifest (from `SEED-OUTLINE.md`). No special handling is required:

- `assembleProjectDepGraph` lists Tasks by `owner.LabelProject` label. The ImportController
  stamps this label on every Task CR at creation time (mirrors Phase 15 label discipline and
  the WR-04 requirement that unlabeled Tasks generate an observable warning).

- `deriveGlobalWaves` derives the schedule from whatever Task edges are present. Imported
  Tasks with their original `dependsOn` declarations produce the same wave structure as the
  original run.

- `checkGlobalCycleGate` runs identically — the salvaged plan passed cycle validation the
  first time, so it will pass again. If the seed manifest was edited to introduce a cycle,
  the `CycleDetected` condition surfaces normally.

- Wave prune loop: Wave CRs are **not imported** (they are always re-derived). The import
  does not create Wave CRs. `deriveGlobalWaves` creates them fresh on the first reconcile
  after import. This is correct per PERSIST-03 / D-10 (schedule never stored in status).

- Task wave-index labels (`tideproject.k8s/wave-index`): set by `stampGlobalTaskLabels` on
  the normal reconcile path after `deriveGlobalWaves` runs. The ImportController does not
  set these; they are derived from the current graph, not imported from the salvage.

**IMPORTANT: do not import Wave CRs or wave-index labels.** These are always rederived from
the execution DAG per PERSIST-03. Importing a stale wave schedule would conflict with
`deriveGlobalWaves` and produce incorrect task ordering.

---

## Idempotency Analysis

| Operation | Idempotency mechanism |
|-----------|----------------------|
| Creating CRs from seed manifest | Name-keyed check before Create; AlreadyExists = success |
| Dispatching tide-import Job | Deterministic name `tide-import-<projectUID>`; AlreadyExists = success |
| Copying envelope files | `cp -n` (no-clobber): skip if destination already exists |
| Patching `out.json` TaskUID | Read-then-skip: if `taskUID == newUID`, do not rewrite |
| Setting ImportComplete condition | `meta.SetStatusCondition` is idempotent |
| Reporter Job spawn post-import | Existing `isFirstCompletion` + AlreadyExists guard in `handleProjectJobCompletion` |
| Retry annotation handling | Annotation consumed on first observation (one-shot, mirrors bypass-push-lease pattern) |

The whole flow is safe to restart at any point. A partial copy (tide-import Job killed mid-run)
leaves some envelopes copied and some not. On Job retry, the no-clobber copy skips
already-present envelopes and proceeds to the remaining ones.

---

## Failure and Partial-Import Handling

| Failure scenario | Detection | Recovery |
|-----------------|-----------|----------|
| SeedManifestConfigMap not found | ImportController `IsNotFound` → `ImportPhase=Failed` + condition | Operator creates ConfigMap; clear via retry annotation |
| CR Create fails (validation error) | ImportController Create returns error → `ImportPhase=Failed` | Operator fixes CR spec; retry annotation |
| tide-import Job fails | `isJobFailed` in ImportController → `ImportPhase=Failed` | Inspect Job logs; retry annotation re-dispatches Job |
| Partial envelope copy (Job killed) | On retry: `cp -n` skips existing, copies remaining | Transparent |
| Salvaged envelope corrupted | `ReadOut` returns parse error; reconciler logs non-fatal, defers to child-based BoundaryDetected succession | Child CRs exist; when all Succeed, level Succeeds normally |
| No salvaged envelope for a CR | `ReadOut` returns not-found; reconciler falls through to normal planner dispatch | That CR re-plans fresh — correct for partial salvage |
| ImportController restarted mid-state | State machine re-derives from `Status.ImportPhase` condition; idempotent steps skip already-done work | Automatic |

The last two rows describe the **graceful partial salvage** behavior: levels with missing
envelopes dispatch fresh planners; levels with present envelopes skip planner dispatch.
No manual intervention required for partial salvage.

---

## Patterns to Follow

### Pattern 1: One-Shot Import Job as Deterministic, Idempotent Batch

Name: `tide-import-<projectUID>`. BackoffLimit=2, TTLSecondsAfterFinished=600. Mirrors
`tide-reporter-<uid>` and `tide-push-<uid>` naming. AlreadyExists on Create = idempotent
success. ImportController does NOT re-create a succeeded import Job once
`ImportComplete=True` is set.

### Pattern 2: Annotation-Driven Retry (mirrors bypass-push-lease)

When `ImportPhase=Failed`, operator adds annotation `tideproject.k8s/retry-import=true`.
ImportController consumes the annotation, deletes the failed Job, resets ImportPhase to
`Pending`, and retries. Mirrors `bypassPushLeaseAnnotation` pattern in
`project_controller.go:124` exactly.

### Pattern 3: Status Snapshot in Seed Manifest Drives CR Patching

The seed manifest carries a `status` field per CR. The ImportController patches each CR's
`Status.Phase` to the salvaged value immediately after creation. A `Succeeded` Milestone
with `Succeeded` Phase children causes `BoundaryDetected` to return true on the first
reconcile, advancing the succession without waiting for child re-execution. This is the
key mechanism that avoids re-dispatching planners that have already run.

---

## Anti-Patterns to Avoid

### Anti-Pattern 1: Modifying the Envelope Path Contract

**What:** Adding a by-name fallback to `FilesystemEnvelopeReader.ReadOut` so it searches
`envelopes/by-name/<name>/out.json` when the UID path misses.

**Why wrong:** The path contract (`envelopes/<uid>/out.json`) is used by five reconcilers,
the reporter Job binary, and two `EnvelopeReader` implementations. A dual-path scheme adds
permanent complexity to the hot read path, requires all future envelope writers to maintain
both paths, and introduces cross-project pollution risk. The by-name paths don't exist in
the salvaged tgz anyway — a migration step is required regardless.

**Instead:** Approach B. The path contract stays clean; complexity is in the one-shot import.

### Anti-Pattern 2: Importing Wave CRs

**What:** Including Wave CRs in the seed manifest and creating them in ImportController's
`CreatingCRs` step.

**Why wrong:** Waves are derived from the execution DAG on every reconcile (PERSIST-03 /
D-10). Importing a Wave CR with stale `TaskRefs` would conflict with `deriveGlobalWaves`
which creates Waves named `tide-wave-<project>-<N>`. The prune loop would then try to
delete the imported Waves if their index is outside the new schedule.

**Instead:** Import only Milestone, Phase, Plan, and Task CRs. Wave CRs are always
re-derived fresh.

### Anti-Pattern 3: Setting TaskUID Rewrite Without Atomic Write

**What:** Read `out.json`, modify the `taskUID` field, write back to the same path with
`os.WriteFile`.

**Why wrong:** If the tide-import Job is killed between the read and the write, the file is
left empty or corrupted. On retry, the `cp -n` no-clobber skips the copy (destination exists)
but the destination is now a corrupted file.

**Instead:** Write to a temp file in the same directory, then `os.Rename` (atomic on Linux
tmpfs/ext4). If the rename never completes, the original file is untouched; on retry the
patch step re-runs cleanly.

### Anti-Pattern 4: Patching Task Status.Phase to Succeeded Without Checking Wave Membership

**What:** Blindly patching all imported Tasks to `Status.Phase=Succeeded` so the
TaskReconciler terminal short-circuit fires on the first reconcile.

**Why wrong:** If the salvaged Task was `Failed` or `Running` at the time of salvage, setting
it to `Succeeded` creates an inconsistency between the envelope's `ExitCode` (non-zero) and
the CR status. The budget rollup and failure-halt logic both key on `Task.Status.Phase`.

**Instead:** The seed manifest carries the original `status` value. The ImportController
patches each Task's Status.Phase to the salvaged value, not unconditionally to `Succeeded`.
Failed Tasks from the salvaged run remain `Failed` — the operator can selectively retry them
via the normal `tide resume --retry-failed` path.

### Anti-Pattern 5: Importing Envelopes at the Manager's WorkspaceRoot vs PVC SubPath

**What:** Mounting the PVC in the tide-import Job at the Manager's mount point (`/workspaces`
with no subPath) and copying across the full tree.

**Why wrong:** The Manager mounts the PVC at `/workspaces` (no subPath); Task pods mount with
`subPath: {project-uid}/workspace`. The tide-import Job needs to write to the Task-pod
subPath layout, not the Manager layout. Using the Manager's mount point means the import Job
runs without namespace isolation between old and new project trees, risking accidental
overwrite.

**Instead:** Mount the PVC in the import Job with two explicit subPath mounts:
`subPath: <oldUID>/workspace` at `/old-workspace` (read-only) and
`subPath: <newUID>/workspace` at `/new-workspace` (read-write). This mirrors how Task pods
mount their own slice of the PVC.

---

## Build Order

Respects import dependencies. The import feature is new infrastructure placed entirely
outside the existing reconcile hot path.

```
Phase A — Foundation (no controller changes, additive schema only)

  A-01: Define ImportSourceRef struct and ImportSeedManifest schema
        Files: api/v1alpha2/project_types.go (add ImportSource *ImportSourceRef field)
               api/v1alpha2/import_types.go (new: ImportSourceRef, ImportSeedManifest)
        Gate: make manifests generate; CRD YAML carries importSource field;
              make test exits 0.

  A-02: Author tide-import binary (cmd/tide-import/main.go)
        Reads rekey table from env-injected JSON; cp -n + TaskUID rename-atomic patch;
        writes completion report to stdout for termination message.
        Gate: unit tests offline with fake filesystem (testenv tempdir).

Phase B — Import Controller and Guards

  B-01: ImportController state machine
        File: internal/controller/import_controller.go (new)
        State: Pending → CreatingCRs → CopyingEnvelopes → Complete / Failed
        CR materialization from SeedManifestConfigMap + rekey table.
        Dispatches tide-import Job. Sets ConditionImportComplete.
        Gate: envtest covering all state transitions + partial-failure recovery
              + retry annotation + idempotent restart.

  B-02: ImportComplete guard at all five planner dispatch sites
        Files: project_controller.go (~line 950, after Step 1 terminal short-circuit)
               milestone_controller.go (~line 243, same position)
               phase_controller.go (equivalent position)
               plan_controller.go (equivalent position)
               task_controller.go (equivalent position)
        Gate: existing envtest suite passes unmodified + new test per file:
              "import-pending holds planner dispatch".

Phase C — Operator Tooling

  C-01: tide import seed CLI subcommand (cmd/tide/import_seed.go)
        Reads salvage-20260618/SEED-OUTLINE.md + pvc-envelopes.tgz content.
        Emits SeedManifestConfigMap YAML + ImportSourceRef YAML snippet to stdout.
        Gate: golden-file test against salvage-20260618 fixtures.

  C-02: Dogfood import end-to-end (test/integration/kind/import_test.go)
        Applies salvage-20260618 fixtures to kind cluster.
        Asserts all Milestones reach Succeeded without any planner re-dispatch.
        Gate: IMPORT-E2E-01.
```

**Dependencies between phases:**
- A-01 must precede B-01 and B-02 (controller depends on API type).
- A-02 must precede B-01 (ImportController dispatches the binary as a Job).
- B-01 and B-02 can be developed in parallel once A-01 is complete.
- C-01 and C-02 depend on B-01 + B-02 being complete.

**Carry-in debt to flag in PITFALLS.md:**
- Status snapshot accuracy: if the seed manifest omits CR status patches, BoundaryDetected
  may not fire, causing the Project to stall waiting for children that will never re-run.
- TaskUID rename-atomic on shared PVC (tmpfs rename semantics vary by volume driver).
- WR-04 label discipline: materialized Task CRs must carry `tideproject.k8s/project` label
  at Create time, not via the backfill path, to appear in `assembleProjectDepGraph`.

---

## Sources

All findings are from direct reading of the production codebase at HEAD:

- `internal/controller/project_controller.go` — `reconcileProjectPlannerDispatch` (lines ~941-1102), `handleProjectJobCompletion` (lines ~1117-1219), `assembleProjectDepGraph` + `deriveGlobalWaves` (lines ~1474-1728)
- `internal/controller/milestone_controller.go` — `reconcilePlannerDispatch` (lines ~239-493), `handleJobCompletion` (lines ~507-717)
- `internal/controller/reporter_jobspec.go` — `BuildReporterJob` (full file)
- `internal/dispatch/podjob/backend.go` — `FilesystemEnvelopeReader.ReadOut` (lines ~94-105), `EnvelopeReader` interface
- `pkg/dispatch/envelope.go` — `EnvelopeOut`, `EnvelopeIn`, `TerminationStub` (full file)
- `api/v1alpha2/project_types.go` — `ProjectSpec` (lines ~299-388); `ImportSource` does not yet exist
- `examples/projects/dogfood/salvage-20260618/SEED-OUTLINE.md` — 3 milestones, 15 phases, 42 plans salvage target

---

*Architecture research for: TIDE v1.0.3 plan-import / envelope resumption*
*Researched: 2026-06-18*
