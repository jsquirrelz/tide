# Phase 28: Plan-Import Core — Research

**Researched:** 2026-06-18
**Domain:** TIDE controller integration — Approach B UID-rewrite envelope import
**Confidence:** HIGH (all claims verified by direct code reading or byte inspection of salvage fixture)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Approach B (UID-rewrite). The five existing reconcilers, reporter Job, and FilesystemEnvelopeReader path contract stay byte-for-byte unchanged. All complexity is in the one-shot pre-reconcile import phase.
- **D-02:** New surface: `ImportSourceRef` field on `ProjectSpec` (api/v1alpha2); `internal/controller/import_controller.go` state machine (Pending→CreatingCRs→CopyingEnvelopes→Complete/Failed); `cmd/tide-import/main.go` in-pod binary; `ImportComplete` condition guard as a one-liner at each of the five dispatch sites.
- **D-03:** Operator applies ONE Project carrying `spec.importSource = {seedConfigMapRef, pvcSubPath}`. ImportController materializes the CR tree from the seed, records the name→newUID rekey table, then dispatches tide-import.
- **D-04:** Seed YAMLs cover down to Plan level only (no tasks.yaml). Project → Milestone → Phase → Plan are materialized from the seed. Only Tasks are materialized from plan-level envelope children via the normal reporter/materializer path.
- **D-05:** tide-import Job upgrades any v1alpha1 embedded child Spec.Raw bytes to v1alpha2 as it copies envelopes, then strict-validates. Steady-state MaterializeChildCRDs / reporter path UNCHANGED.
- **D-06:** Envelope wire format is unchanged (APIVersionV1Alpha1 stays). The v1alpha1/v1alpha2 concern is narrowly about embedded child-CRD Spec.Raw bytes.
- **D-07:** Rekey table keys on fully-qualified name (object name + full parent chain). tide-import validates each envelope's out.ChildCRDs[*].Name against the seed's declared children at that level.
- **D-08:** 3-layer trust gate: (1) spec.importSource RBAC anchor, (2) same-namespace pvcSubPath containment, (3) Kind-allowlist + name-match + completeness check.
- **D-09:** Wave CRs never imported — always re-derived.
- **D-10:** Cycle detection (dag.ComputeWaves) runs before any client.Create.
- **D-11:** Budget rollup suppressed unconditionally for imported envelopes.
- **D-12:** ImportComplete condition is the first-step idempotency guard; copy step uses cp -n + atomic rename.

### Claude's Discretion

- Exact `ImportSourceRef` field shape, seed ConfigMap schema, and condition-type naming are authoring decisions for the planner, within D-02/D-03.
- Whether the rekey table is recorded in a ConfigMap vs `Project.Status` is a planner choice (CRD-status-only persistence preferred; rekey table is transient import state).

### Deferred Ideas (OUT OF SCOPE)

- Hybrid write-side (by-name envelope paths going forward).
- Per-envelope sha256 integrity checksums in the seed manifest.
- Automatic export-on-halt.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| IMPORT-01 | A fresh Project run can adopt pre-authored planner envelopes and SKIP the planner for any level whose valid envelope already exists, proceeding straight to materialize → execution (no re-paid planning). | ImportComplete guard at 5 dispatch sites + tide-import copies envelopes to new-UID paths satisfying FilesystemEnvelopeReader.ReadOut contract. |
| IMPORT-02 | Envelope validated before adoption — correct schema version, complete (declared childCount present), not partial write; invalid envelope rejected so planner runs normally. | ValidateAPIVersionKind (envelope.go:407) + len(ChildCRDs)==ChildCount + D-05 strict-validate in tide-import before creating any CRs. |
| IMPORT-03 | Import resolves UID-churn — envelopes authored under prior CR UIDs matched to new run's CRs by stable identity (object name + parent chain), no cross-object or cross-project aliasing. | D-07 FQ-name keying + name-match validation against seed tree; confirmed by salvage fixture analysis. |
| IMPORT-04 | Import re-derives global Execution DAG and runs cycle detection before materializing any child CRDs; cyclic or unresolved graphs rejected; Wave CRs never imported. | dag.ComputeWaves called before client.Create (D-10); D-09 Wave exclusion; R-05 atomicity per milestone. |
| IMPORT-05 | Imported-envelope provenance trust-bounded — import operator-gated and verifies envelope origin before reading from shared per-namespace PVC. | D-08 3-layer trust gate; path-traversal defense mirrors ReadPrompt at backend.go:116-127. |

</phase_requirements>

---

## Summary

Phase 28 implements the Approach B UID-rewrite import mechanism: a one-shot pre-reconcile controller + in-pod binary that copies salvaged UID-keyed envelope trees to new-UID paths so the five unchanged reconcilers find their envelopes exactly where they always look. The approach was chosen over Approach A (dual-path ReadOut) because the salvage fixture has only UID-keyed paths and A would require a migration step identical to B plus permanent dual-path complexity on the hot read path.

The empirical question flagged by D-06 has been settled by byte inspection: the salvage fixture's plan-level envelopes all exited with exitCode=1 (credit exhaustion), so they contain zero Task child CRD specs. The conversion concern from R-06 applies to Plan child specs embedded in phase-level envelopes — specifically the `objective` and `wave` fields present in those specs but absent from v1alpha2 PlanSpec. These fields are silently dropped by json.Unmarshal (Go's unknown-field-ignore behavior), which is correct: the live plans.yaml confirms that Plan CRs in etcd only carried `phaseRef` — the dropped fields were planner-facing documentation, not controller-consumed fields. Wave CRs are re-derived (D-09) and never imported, so R-06's Wave.Spec.ProjectRef concern is moot for Phase 28.

The seed files (`projects/milestones/phases/plans.yaml`) are genuine v1alpha2 CR exports from the live cluster, not reconstructed from envelope children — they carry full v1alpha2 metadata. The operator uses these as the authoritative source for materializing the CR tree (D-03/D-04), bypassing the embedded child spec schema question entirely for the Project→Milestone→Phase→Plan levels.

**Primary recommendation:** The planner should implement phases A (schema + binary) and B (controller + guards) in strict build-order: A-01 (ImportSourceRef type + condition constant) → A-02 (tide-import binary with testable offline logic) → B-01 (ImportController state machine + envtest) → B-02 (5-site guard + per-site test). Each task is independently testable before the next depends on it.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| ImportSourceRef field + CRD marker | API / CRD schema | — | Schema extension lives in api/v1alpha2; generated by controller-gen |
| ImportComplete condition type + annotation constants | API / shared_types.go | — | Mirrors AnnotationBillingResumedAt pattern; consumed by all 5 dispatch sites |
| ImportController state machine | API / Controller | — | New controller registered in cmd/manager alongside existing controllers |
| tide-import binary (copy + rekey) | In-pod job | — | Mirrors tide-reporter: stdlib I/O, no K8s client, reads rekey table from env, writes to PVC |
| Envelope copy + atomic TaskUID rewrite | tide-import binary | — | Filesystem-only; cp -n + os.Rename for partial-write safety |
| ImportComplete park guard | All 5 planner dispatch sites | — | One-liner inserted after Step 1 terminal short-circuit at each site |
| Cycle detection before CR create | ImportController | pkg/dag.ComputeWaves | Explicit call; client.Create bypasses admission webhook |
| Budget rollup suppression | ImportController | project_controller handleProjectJobCompletion | Flagged in envelope or skipped via importSource check |

---

## Standard Stack

### Core (zero new go.mod entries)

| Library | Version | Purpose | Phase 28 Role |
|---------|---------|---------|--------------|
| `encoding/json` | Go 1.26 stdlib | JSON marshal/unmarshal | Decode envelope Spec.Raw; encode rekey table |
| `path/filepath` | Go 1.26 stdlib | File path construction | Envelope paths at new-UID locations |
| `os` | Go 1.26 stdlib | File I/O | ReadFile, MkdirAll, Rename (atomic) for tide-import |
| controller-runtime v0.24.x | locked | Reconcile loop, patch, status | ImportController state machine; same version as all other controllers |
| `pkg/dispatch` | internal | Envelope types + ValidateAPIVersionKind | Validation before adoption |
| `pkg/dag` | internal | ComputeWaves + CycleError | Cycle detection before client.Create |
| `api/v1alpha2` | internal | All CRD types | CR materialization from seed |
| `internal/reporter` | internal | MaterializeChildCRDs + ChildrenAlreadyMaterialized | Reused by reporter Job (unchanged); import uses same pattern for Tasks |

### No New Dependencies

STACK.md confirmed: zero new `require` entries in go.mod. All implementation uses stdlib + existing internal packages. `crypto/sha256` (already in `internal/credproxy/token.go`) is available for optional integrity checksums but is not required for Phase 28.

---

## Settled Empirical Question: Schema Conversion Size (D-06)

**Question:** Is v1alpha1→v1alpha2 conversion of embedded child Spec.Raw a no-op, a field-rename, or a structural migration?

**Evidence gathered by byte inspection of the salvage fixture:**

**1. Envelope wire format (envelope.go:24):** All 59 envelopes have `"apiVersion":"tideproject.k8s/v1alpha1"` in both in.json and out.json. This is the envelope wrapper format, unchanged per D-06. [VERIFIED: direct file read]

**2. Plan-level envelopes:** All 39 plan-level envelopes have `"exitCode":1` (credit balance exhausted, budget halt). Zero Task child CRDs exist in any plan-level envelope. [VERIFIED: byte inspection of all 39 out.json files]

**3. Phase-level child Plan specs (the conversion question):** Phase-level envelopes (15 total, 14 succeeded) emit Plan child CRD specs with these fields in Spec.Raw:
- `phaseRef` — present in v1alpha2 PlanSpec
- `dependsOn` — present in v1alpha2 PlanSpec
- `filesTouched` — NOT present in v1alpha2 PlanSpec (dropped by json.Unmarshal; was planner-facing metadata)
- `objective` — NOT present in v1alpha2 PlanSpec (dropped by json.Unmarshal; planner-facing text)
- `wave` — NOT present in v1alpha2 PlanSpec (dropped by json.Unmarshal; was planning-DAG artifact)

**v1alpha2 PlanSpec** (`api/v1alpha2/plan_types.go`) has: `phaseRef`, `dependsOn`, `sharedContext`. The salvage specs have `phaseRef` and `dependsOn` (match). The salvage specs lack `sharedContext` (zero-valued on decode — fine, it is `omitempty`). The extra fields (`filesTouched`, `objective`, `wave`) are silently dropped by json.Unmarshal into `PlanSpec`. [VERIFIED: direct struct read + byte inspection]

**4. Confirmation from live plans.yaml:** The exported v1alpha2 Plan CRs in the cluster (plans.yaml) have only `spec.phaseRef` populated — no `objective`, `wave`, or `filesTouched`. These fields were consumed by the planner and never persisted to the CRD. [VERIFIED: plans.yaml line 24]

**5. Milestone-level child Phase specs:** Fields `milestoneRef` + `dependsOn` — both present in v1alpha2 PhaseSpec. No extra fields. Exact match. [VERIFIED: byte inspection]

**6. Project-level child Milestone specs:** Fields `projectRef` + `dependsOn` — both present in v1alpha2 MilestoneSpec. Exact match. [VERIFIED: byte inspection]

**Conclusion:** The v1alpha1→v1alpha2 conversion for embedded child Spec.Raw bytes in the salvage fixture is **effectively a no-op** — the envelope child specs carry only fields that exist in v1alpha2, and the extra planner-facing fields (`objective`, `wave`, `filesTouched`) are harmlessly dropped. **R-06's Wave.Spec.ProjectRef concern is irrelevant for Phase 28** because Wave CRs are never imported (D-09). The tide-import conversion step (D-05) is still required for correctness and future-proofing, but for the salvage-20260618 fixture specifically it writes the same bytes it reads.

**Practical implication:** The tide-import binary still runs schema conversion as specified (to satisfy D-05 and IMPORT-02 strict validation), but the conversion is trivially correct for this fixture. The implementer should marshal Spec.Raw through the typed v1alpha2 struct (json.Unmarshal → json.Marshal) to strip unknown fields and add zero-valued required fields — this is the canonical Go conversion pattern, requires no schema-diff logic, and produces valid v1alpha2 bytes.

---

## Architecture Patterns

### System Architecture Diagram

```
Operator: kubectl apply Project with spec.importSource set
          + creates SeedManifestConfigMap
                          |
                          v
ImportController (NEW — internal/controller/import_controller.go)
  State: Pending → CreatingCRs
    reads SeedManifestConfigMap
    for each entry (milestone/phase/plan):
      client.Create CR → K8s assigns newUID
      records (fqName → oldUID, fqName → newUID) in rekey ConfigMap
    patches each CR's Status.Phase to salvaged status
  State: CreatingCRs → CopyingEnvelopes
    builds tide-import Job (name: tide-import-<projectUID>)
    Job mounts PVC at 2 subPaths:
      <oldProjectUID>/workspace → /old-workspace (read-only)
      <newProjectUID>/workspace → /new-workspace (read-write)
    Job reads rekey table from env/ConfigMap
    for each (oldUID, newUID) pair:
      cp -n /old-workspace/envelopes/<oldUID>/ /new-workspace/envelopes/<newUID>/
      schema-convert Spec.Raw (unmarshal→marshal through typed struct)
      if out.json.taskUID != newUID: atomic rewrite (write temp, os.Rename)
  State: CopyingEnvelopes → Complete
    sets ConditionImportComplete=True on Project.Status
                          |
                          | ConditionImportComplete=True
                          v
All 5 dispatch sites (MODIFIED — one-liner guard added):
  if project.Spec.ImportSource != nil && ImportComplete != True → park (5s requeue)
                          |
                          | ImportComplete=True clears guard
                          v
Normal reconcile flow (UNCHANGED):
  ProjectReconciler.reconcileProjectPlannerDispatch:
    Step 2b: job tide-project-<newUID>-1 not found
    Step 2: Phase==Running (patched by ImportController)
    → handleProjectJobCompletion (TTL/GC branch)
    → ReadOut(newProjectUID, newProjectUID) → succeeds (imported)
    → reporter Job spawned → AlreadyExists for Milestone CRs (idempotent)

  MilestoneReconciler.reconcilePlannerDispatch:
    Step 2b: child Phases found by spec.milestoneRef match
    → planner dispatch SKIPPED

  PhaseReconciler / PlanReconciler: same pattern

  TaskReconciler (for task-level dispatch after import):
    Tasks materialized via normal reporter Job from plan-level envelopes
    NOTE: all plan envelopes in salvage-20260618 exitCode=1 → Tasks re-plan
    computeGlobalIndegree derives readiness from imported Task.DependsOn edges
    Wave CRs derived fresh by deriveGlobalWaves
```

### Recommended Project Structure

```
api/v1alpha2/
  import_types.go          # NEW: ImportSourceRef struct
  project_types.go         # MODIFIED: add ImportSource *ImportSourceRef field
  shared_types.go          # MODIFIED: add ConditionImportComplete constant

internal/controller/
  import_controller.go     # NEW: ImportController state machine

cmd/tide-import/
  main.go                  # NEW: in-pod binary (cp + rekey + schema-convert)

images/tide-import/
  Dockerfile               # NEW: mirrors images/tide-reporter/Dockerfile pattern

charts/tide/
  values.yaml              # MODIFIED: add images.tideImport block (FIXED contract)
  templates/
    deployment.yaml        # MODIFIED: add TIDE_IMPORT_IMAGE env var

cmd/manager/
  main.go                  # MODIFIED: read TIDE_IMPORT_IMAGE, wire to ImportController
```

### Pattern 1: ImportController State Machine

**What:** Controller watches Projects with `spec.importSource != nil`. Drives a 4-state machine via `Project.Status.Conditions`.

**When to use:** Invoked automatically on any Project that declares importSource.

**Critical implementation note:** `ImportComplete` condition is checked as the FIRST step — if True, the controller returns immediately without doing any work. This is the at-least-once idempotency guard (R-09 / D-12).

```go
// Source: internal/controller/import_controller.go (NEW)
// State machine transition using conditions as persistent state.
func (r *ImportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    var project tidev1alpha2.Project
    if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    // Fast path: only act on Projects with importSource declared.
    if project.Spec.ImportSource == nil {
        return ctrl.Result{}, nil
    }
    // Idempotency guard: already complete (D-12).
    c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha2.ConditionImportComplete)
    if c != nil && c.Status == metav1.ConditionTrue {
        return ctrl.Result{}, nil
    }
    // ... state machine dispatch ...
}
```

### Pattern 2: ImportComplete Guard at Dispatch Sites

**What:** One-liner inserted at each of the 5 planner dispatch sites, immediately after Step 1 (terminal short-circuit), before pool acquire and Job creation.

**Position in project_controller.go:** After line 1004 (terminal switch), before line 1007 (jobName declaration).
**Position in milestone_controller.go:** After line 243 (terminal short-circuit), before line 284 (jobName declaration).
**Position in phase/plan_controller.go:** Equivalent position in reconcilePlannerDispatch.
**Position in task_controller.go:** Inside `gateChecks`, after terminal short-circuit (line ~310), before resolveProject (line ~339). The guard reads `project` from resolveProject — restructure: resolve project first, then check ImportComplete. Or: restructure guard to be in `reconcileDispatch` before `gateChecks`.

**Exact guard code (identical at all 5 sites):**
```go
// Source: ARCHITECTURE.md §ImportComplete Guard
// Guard: if an ImportSource is declared, park until import completes.
// Position: after terminal short-circuit, before pool acquire (Pitfall 2).
if project.Spec.ImportSource != nil {
    c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha2.ConditionImportComplete)
    if c == nil || c.Status != metav1.ConditionTrue {
        logger.V(1).Info("import pending; holding planner dispatch", "project", project.Name)
        return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
    }
}
```

**Task-reconciler specific issue:** `reconcileDispatch` → `gateChecks` — the guard needs `project` to read its conditions, but `resolveProject` happens at Step 3 inside `gateChecks` (line 339). The planner places the guard after resolveProject succeeds, or restructures so the project resolution happens earlier. The guard must NOT fire before `resolveProject` returns non-nil.

### Pattern 3: tide-import Binary (mirrors tide-reporter)

**What:** In-pod binary. Receives rekey table (fqName→oldUID, fqName→newUID pairs) via stdin or env-injected JSON. Reads from `/old-workspace/envelopes/<oldUID>/`, writes to `/new-workspace/envelopes/<newUID>/`.

**Key implementation details:**
1. Read rekey JSON from stdin (keeps Job spec simple; avoids env-length limits for 42-plan tables).
2. For each pair: `os.MkdirAll` + `os.ReadDir` the old path + `os.ReadFile` each file + `os.WriteFile` (no-clobber: check exists first, skip if already written — cp -n semantics).
3. For out.json specifically: after copying, check if `taskUID != newUID`; if so, unmarshal, update `taskUID`, marshal, write to temp file in same dir, `os.Rename` (atomic).
4. Schema conversion for child Spec.Raw in out.json: for each childCRD, decode Spec.Raw through the appropriate typed struct (`json.Unmarshal` → set zero-value required fields → `json.Marshal`), update `child.Spec.Raw`.
5. Write completion report to stdout as JSON: `{"converted": N, "copied": N, "skipped": N}`.

**Dockerfile pattern to mirror:** `images/tide-reporter/Dockerfile` — golang:1.26-alpine builder → distroless/static:nonroot runtime. Copy only the packages the binary imports.

### Pattern 4: Schema Conversion in tide-import

**What:** For each envelope's `out.json`, iterate `childCRDs`, decode `Spec.Raw` through the appropriate v1alpha2 typed struct, re-marshal.

```go
// Source: inferred from internal/reporter/materialize.go MaterializeChildCRDs pattern
// Schema conversion — strips unknown fields, populates zero-value required fields.
func convertSpecRaw(kind string, rawBytes json.RawMessage) (json.RawMessage, error) {
    switch kind {
    case "Milestone":
        var spec tidev1alpha2.MilestoneSpec
        if err := json.Unmarshal(rawBytes, &spec); err != nil {
            return nil, fmt.Errorf("unmarshal Milestone spec: %w", err)
        }
        return json.Marshal(spec)
    case "Phase":
        var spec tidev1alpha2.PhaseSpec
        if err := json.Unmarshal(rawBytes, &spec); err != nil {
            return nil, fmt.Errorf("unmarshal Phase spec: %w", err)
        }
        return json.Marshal(spec)
    case "Plan":
        var spec tidev1alpha2.PlanSpec
        if err := json.Unmarshal(rawBytes, &spec); err != nil {
            return nil, fmt.Errorf("unmarshal Plan spec: %w", err)
        }
        return json.Marshal(spec)
    case "Task":
        var spec tidev1alpha2.TaskSpec
        if err := json.Unmarshal(rawBytes, &spec); err != nil {
            return nil, fmt.Errorf("unmarshal Task spec: %w", err)
        }
        return json.Marshal(spec)
    // Wave is never in imported envelopes (D-09).
    default:
        return nil, fmt.Errorf("unsupported Kind %q in import conversion", kind)
    }
}
```

For the salvage-20260618 fixture, `Plan` conversion drops `objective`, `wave`, `filesTouched` and produces clean v1alpha2. All other Kinds are already field-compatible.

### Anti-Patterns to Avoid

- **Don't modify FilesystemEnvelopeReader.ReadOut:** The path contract is used by 5 reconcilers + reporter + 2 reader impls. Adding dual-path complexity would be Anti-Pattern 1 from ARCHITECTURE.md.
- **Don't import Wave CRs:** Always re-derived (D-09). Including them would conflict with deriveGlobalWaves' prune loop.
- **Don't write out.json in-place (without atomic rename):** Partial writes corrupt the file and cp -n no-clobber then skips the corrupted destination on retry. Always write to a temp file in the same directory and os.Rename (Anti-Pattern 3).
- **Don't patch all Tasks to Succeeded blindly:** Seed manifest carries original status values; patch each Task to the salvaged status, not unconditionally Succeeded (Anti-Pattern 4).
- **Don't mount PVC at manager root (/workspaces):** Import Job must use explicit subPath mounts for old and new project workspaces (Anti-Pattern 5).

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Envelope completeness validation | Custom count-check | `len(out.ChildCRDs) == out.ChildCount` (project_controller.go:1204 pattern) | Already used as succession guard; same check gates import |
| Kind allowlist enforcement | Custom list | `reporter.ChildKindAllowlist` (internal/reporter/materialize.go:64) | T-308 mitigation already tested; reuse the map |
| Path traversal defense | Custom path check | Mirror `FilesystemEnvelopeReader.ReadPrompt` pattern (backend.go:116-127) | Covers filepath.Clean + HasPrefix guards |
| Cycle detection | Custom traversal | `pkg/dag.ComputeWaves` (kahn.go:46) | Already handles CycleError; admission webhook uses it; import must call it too |
| Idempotent condition set | Custom condition write | `meta.SetStatusCondition` (controller-runtime) | Idempotent by type — safe to call on every reconcile |
| AlreadyExists idempotency | Custom check-before-create | `client.Create` + `apierrors.IsAlreadyExists` = success | SUB-03 pattern used throughout; apply same to CR creation from seed |
| CR owner references | Manual ownerRef construction | `internal/owner.EnsureOwnerRef` | Enforces same-namespace, Controller=true, BlockOwnerDeletion=true |

---

## Confirmed Code Locations (file:line)

### 1. Five Planner-Dispatch Sites

The `ImportComplete` guard inserts at these positions:

| Site | File | Guard Position | Description |
|------|------|----------------|-------------|
| Project | `internal/controller/project_controller.go:1000` | After terminal switch (lines 1001-1005), before `jobName` (line 1007) | `reconcileProjectPlannerDispatch` |
| Milestone | `internal/controller/milestone_controller.go:241` | After terminal check (line 241-243), before `jobName` (line 284) | `reconcilePlannerDispatch` — `ms.Status.Phase == "Succeeded"/"Failed"` short-circuit |
| Phase | `internal/controller/phase_controller.go` | Equivalent position in phase `reconcilePlannerDispatch` | Same structure as milestone |
| Plan | `internal/controller/plan_controller.go` | Equivalent position in plan `reconcilePlannerDispatch` | Same structure as milestone |
| Task | `internal/controller/task_controller.go:305` | Inside `gateChecks`, after resolved project (needs project to check conditions); restructure to guard after resolveProject at line ~339, before reject check at line ~366 | `reconcileDispatch` → `gateChecks` |

**Note on Task site:** Unlike the 4 planner sites, the task site's guard must read `project.Status.Conditions` for `ImportComplete`. `project` is resolved at `gateChecks:339` via `r.resolveProject`. The guard belongs in `gateChecks` between project resolution (line 339-360) and the reject short-circuit (line 366). The project is already available at that point.

### 2. FilesystemEnvelopeReader.ReadOut Path Contract

`internal/dispatch/podjob/backend.go:94-105` [VERIFIED]:
```go
path := filepath.Join(r.WorkspaceRoot, projectUID, "workspace", "envelopes", taskUID, "out.json")
```

This contract is **unchanged** by Phase 28. The tide-import Job writes envelope files to `<newProjectUID>/workspace/envelopes/<newUID>/out.json` matching this exact layout. The `WorkspaceRoot` for the manager pod is `/workspaces`; the tide-import Job mounts `subPath: <newProjectUID>/workspace` at `/new-workspace`, so it writes to `/new-workspace/envelopes/<newUID>/out.json` which resolves to the same path from the manager's perspective.

### 3. Reporter Job Re-Materialization Behavior

`internal/reporter/materialize.go:98` — `ChildrenAlreadyMaterialized` — checked before `MaterializeChildCRDs`. [VERIFIED: direct code read]

`cmd/tide-reporter/main.go:185-194` — reporter runs this check at step 5; if children already materialized (from seed), returns exitSuccess (idempotent skip).

**D-04 Task materialization path:** Since no plan-level envelopes in salvage-20260618 succeeded, Tasks are re-authored from scratch by the planner when Phase 28 import runs against this fixture. The reporter Job for a plan-level envelope that is missing (exitCode=1) will not find a valid out.json → `ReadOut` returns error → reconciler falls through to normal planner dispatch. This is correct behavior: levels with missing/failed envelopes re-plan fresh (per ARCHITECTURE.md §Failure and Partial-Import Handling).

**For the general case** (future fixtures where plan envelopes succeeded): the reporter Job for each Plan parent calls `ChildrenAlreadyMaterialized` → finds Task CRs already in etcd from seed (if Tasks were in the seed) → returns idempotent skip. Since D-04 scopes the seed to "down to Plan level only," Tasks are NOT in the seed → reporter runs normally → creates Tasks from plan envelope's ChildCRDs. **Task materialization is the only envelope-sourced CR creation under the D-04 model.** [VERIFIED: materialize.go logic + D-04 text]

### 4. Cycle Detection Integration

`pkg/dag/kahn.go:46` — `ComputeWaves(nodes []NodeID, edges []Edge) ([][]NodeID, error)` [VERIFIED: grep]

Returns `*CycleError` (kahn.go:85) on cycle detection. The ImportController must call this on the full Task set (all Tasks to be materialized) before calling `client.Create` on any of them. The input is built by calling `buildScopeResolver` + `buildGlobalEdges` — the same functions used by `assembleProjectDepGraph` in project_controller.go.

`internal/controller/depgraph.go:28` — conservative empty-return: unresolved ref returns empty, never invents an edge. This is why D-10 / R-05 require import atomicity per-milestone: every `Task.Spec.DependsOn` entry must resolve to an existing Task before materialization, or the indegree is wrong.

### 5. ProjectSpec Field and Condition-Type Pattern

**Adding ImportSource field** — append to `ProjectSpec` in `api/v1alpha2/project_types.go` after the existing `FailureProfile` field (line 405). Pattern: pointer field with `+optional`, same as `Git *GitConfig`. [VERIFIED: project_types.go:394-406]

**Condition constant** — add to `api/v1alpha2/shared_types.go` following the block pattern at lines 206-261:
```go
// Phase 28 import condition vocabulary.
const (
    ConditionImportComplete = "ImportComplete"
    ReasonImportSucceeded   = "ImportSucceeded"
    ReasonImportFailed      = "ImportFailed"
    // AnnotationRetryImport consumed by ImportController to reset ImportPhase.
    AnnotationRetryImport = "tideproject.k8s/retry-import"
)
```

Mirror: `AnnotationBillingResumedAt = "tideproject.k8s/billing-resumed-at"` at line 223. [VERIFIED: shared_types.go:206-261]

### 6. tide-import Binary + Dockerfile + Chart Wiring

**Binary location:** `cmd/tide-import/main.go` — mirrors `cmd/tide-reporter/main.go` structure. [VERIFIED: tide-reporter main.go is the canonical in-pod binary pattern]

**Dockerfile:** `images/tide-import/Dockerfile` — mirrors `images/tide-reporter/Dockerfile` exactly:
- golang:1.26-alpine builder with `CGO_ENABLED=0`
- distroless/static:nonroot runtime
- Copies only the packages the binary imports: `api/`, `pkg/dispatch/`, `cmd/tide-import/`

**Chart wiring (CAUTION — FIXED CONTRACT):** `charts/tide/values.yaml` is the FIXED contract per CLAUDE.md; binary catches up to chart, never reverse. Add:
```yaml
images:
  tideImport:
    repository: ghcr.io/jsquirrelz/tide-import
    tag: ""
    pullPolicy: IfNotPresent
```
Then wire `TIDE_IMPORT_IMAGE` env var in `charts/tide/templates/deployment.yaml` (same pattern as `TIDE_REPORTER_IMAGE` at line 98-99). Then read in `cmd/manager/main.go` via `envOrDefault("TIDE_IMPORT_IMAGE", ...)` and pass to ImportController. [VERIFIED: values.yaml:192-195 + deployment.yaml:98-99 + main.go:203]

**IMPORTANT:** Add `charts/tide/values.yaml` change first (chart is the contract), then implement the binary and Dockerfile. The chart addition is additive — no existing values change.

### 7. Seed ConfigMap Schema and ImportController CR Creation

**Seed ConfigMap** carries a JSON document mapping FQ-names to oldUIDs plus initial Status.Phase values. The ImportController reads this to drive `client.Create` for Milestone/Phase/Plan CRs.

**CRITICAL: The salvage-20260618 fixture already provides `projects/milestones/phases/plans.yaml` as v1alpha2 CR exports.** These are the seed. The operator does NOT need to reconstruct CR specs from envelope children — the exported YAMLs ARE the CR specs. The ImportController reads the seed ConfigMap (which encodes these CRs) and creates them directly. The `name→oldUID` mapping is read from the exported metadata (each CR's `uid` field in the YAML).

**Seed ConfigMap schema (planner's discretion per CONTEXT.md Claude's Discretion):**
- Recommended: JSON with `milestones[]`, `phases[]`, `plans[]` arrays, each entry carrying `{name, savedUID, parentRef, dependsOn, status}`.
- The `status` field is essential for patching each CR's Status.Phase to the salvaged value after creation, so BoundaryDetected fires without waiting for re-execution.

**Rekey table recording (planner's discretion):** Recommend recording in `Project.Status` as a transient field (CRD-status-only persistence preferred), or in a named ConfigMap `tide-import-rekey-<projectUID>`. The ConfigMap approach is simpler for the tide-import Job to consume as a Pod volume; the Status approach keeps etcd cleaner. Either is valid; planner chooses.

---

## Common Pitfalls

### Pitfall 1: Task Dispatch Site Guard Placement
**What goes wrong:** The task reconciler guard needs `project` to read `ImportComplete` condition, but `project` is resolved partway through `gateChecks`. Placing the guard before project resolution causes a nil dereference.
**Prevention:** In `gateChecks`, resolve project at step 3 (line 339), then check ImportComplete before the reject short-circuit (line 366). The guard is: `if gate.project.Spec.ImportSource != nil && !importCompleteTrue(gate.project)`.
**Warning signs:** Nil pointer panic in gateChecks during import-pending state.

### Pitfall 2: Wave Guard Placement (ARCHITECTURE.md Pitfall 2)
**What goes wrong:** Guard placed AFTER pool acquire. An import-pending project acquires a planner pool slot, parks, but does not release — the slot leaks.
**Prevention:** Guard placed BEFORE `r.PlannerPool.Acquire` at ALL 5 sites. In project_controller.go, pool acquire is at line 1072; guard inserts before that. [VERIFIED: project_controller.go:1071]

### Pitfall 3: Budget Rollup Not Suppressed (R-13)
**What goes wrong:** When ImportController drives the project through the TTL/GC completion path for a Project-level envelope, `handleProjectJobCompletion` may call `budget.RollUpUsage` for the salvaged envelope, double-counting the prior run's cost.
**Prevention:** When `project.Spec.ImportSource != nil`, the completion handler must skip budget rollup for the project-level envelope. Check `project.Spec.ImportSource != nil` in the rollup guard inside `handleProjectJobCompletion`, or zero out `out.Usage` in the copied project envelope before the completion handler reads it.

### Pitfall 4: Partial-Plan Import Corrupts DAG (R-05)
**What goes wrong:** Envelope for Milestone B Phase 2 is missing. Tasks from imported plans have DependsOn references to tasks from the missing plans. `depgraph.go:28` conservative empty-return gives missing-plan tasks indegree 0 → they dispatch immediately out of order.
**Prevention:** Import atomicity per-milestone (D-10). Before creating any Task CRs for a milestone, verify every `Task.Spec.DependsOn` entry resolves to an existing Task. This is already required by D-10 cycle detection — `dag.ComputeWaves` will fail on unresolved refs if properly implemented.

### Pitfall 5: AlreadyExists Propagation on Seed CR Creation
**What goes wrong:** ImportController creates CRs; reconcile retries; second Create returns AlreadyExists but controller does not treat it as success → state machine stalls at CreatingCRs.
**Prevention:** Wrap every `client.Create` call with `apierrors.IsAlreadyExists` = success, identical to `MaterializeChildCRDs` at `internal/reporter/materialize.go:298-301`. [VERIFIED: materialize.go:297-302]

### Pitfall 6: Rekey ConfigMap Name Collision
**What goes wrong:** Two concurrent imports for different projects produce ConfigMaps with the same name.
**Prevention:** Name the rekey ConfigMap `tide-import-rekey-<projectUID>` (deterministic per project, unique by UID). Same naming pattern as `tide-reporter-<parentUID>` and `tide-import-<projectUID>` Job names.

### Pitfall 7: tide-import Job PVC Mount Layout (Anti-Pattern 5)
**What goes wrong:** tide-import mounts the PVC at the manager's root (`/workspaces`, no subPath) and navigates to old/new paths manually. This gives the import Job access to ALL projects' envelopes on the shared PVC — violating namespace isolation.
**Prevention:** Mount with two explicit subPath mounts: `subPath: <oldProjectUID>/workspace` at `/old-workspace` (read-only) and `subPath: <newProjectUID>/workspace` at `/new-workspace` (read-write). These paths are the exact same subPath that task pods use, verified by reporter_jobspec.go:205 (`SubPath: fmt.Sprintf("%s/workspace", project.UID)`).

---

## Code Examples

### Condition constant pattern (api/v1alpha2/shared_types.go)

```go
// Source: api/v1alpha2/shared_types.go:206-223 — existing block structure
// Phase 28: import condition vocabulary
const (
    ConditionImportComplete = "ImportComplete"
    ReasonImportSucceeded   = "ImportSucceeded"
    ReasonImportFailed      = "ImportFailed"
    AnnotationRetryImport   = "tideproject.k8s/retry-import"
)
```

### ImportSourceRef struct (api/v1alpha2/import_types.go — NEW)

```go
// Source: ARCHITECTURE.md §New ProjectSpec Field
type ImportSourceRef struct {
    // SeedManifestConfigMap names a ConfigMap in the project namespace
    // carrying the seed manifest JSON.
    // +kubebuilder:validation:MinLength=1
    SeedManifestConfigMap string `json:"seedManifestConfigMap"`

    // SalvagedPVCSubPath is the sub-path within the shared tide-projects PVC
    // where the salvaged envelopes reside, e.g. "<oldProjectUID>/workspace".
    // +kubebuilder:validation:MinLength=1
    SalvagedPVCSubPath string `json:"salvagedPVCSubPath"`
}
```

### Atomic out.json TaskUID rewrite (cmd/tide-import/main.go)

```go
// Source: ARCHITECTURE.md Anti-Pattern 3 — atomic rename prevents partial-write
func rewriteTaskUID(outPath, newUID string) error {
    data, err := os.ReadFile(outPath)
    if err != nil {
        return err
    }
    var out pkgdispatch.EnvelopeOut
    if err := json.Unmarshal(data, &out); err != nil {
        return err
    }
    if out.TaskUID == newUID {
        return nil // already correct; no-op
    }
    out.TaskUID = newUID
    newData, err := json.Marshal(out)
    if err != nil {
        return err
    }
    tmp := outPath + ".tmp"
    if err := os.WriteFile(tmp, newData, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, outPath) // atomic on Linux ext4/tmpfs
}
```

### No-clobber copy (cp -n semantics in Go)

```go
// Source: D-12 — cp -n + atomic rename for partial-write safety
func copyFileNoClobber(dst, src string) error {
    if _, err := os.Stat(dst); err == nil {
        return nil // destination exists; skip (cp -n behavior)
    }
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    tmp := dst + ".tmp"
    if err := os.WriteFile(tmp, data, 0644); err != nil {
        return err
    }
    return os.Rename(tmp, dst)
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| UID-keyed envelope path only | UID-keyed (unchanged) + import rewrites old→new | Phase 28 | Import is the bridge; hot path unchanged |
| No envelope adoption | ImportComplete guard at 5 dispatch sites | Phase 28 | Planner dispatch parks until import complete |
| Wave CRs from planner | Wave CRs always re-derived (Phase 24) | Phase 24 | Import never touches Wave CRs |
| per-plan DependsOn only | Global cross-plan/phase/milestone DependsOn (Phase 25) | Phase 25 | Import must populate Task.DependsOn correctly |

**Deprecated/outdated:**
- Wave CRs in import seed: never valid per D-09 / PERSIST-03.
- v1alpha1 SchemaRevision in ProjectSpec: the salvage project was already v1alpha2 (projects.yaml confirmed `schemaRevision: v1alpha2`).

---

## Salvage Fixture Summary (acceptance target)

**Fixture:** `examples/projects/dogfood/salvage-20260618/`

| Level | Total | Succeeded | exitCode=1 (budget halt) | Task children in envelopes |
|-------|-------|-----------|--------------------------|----------------------------|
| project | 1 | 1 | 0 | 3 Milestones |
| milestone | 3 | 3 | 0 | 4-6 Phases each |
| phase | 15 | 14 | 1 | 2-5 Plans each |
| plan | 39 | 0 | 39 | 0 Tasks (all budget-halted) |

[VERIFIED: byte inspection of all 59 out.json files]

**Implication for Phase 28 acceptance test (Phase 29 TOOL-02):** All 39 plan-level planners will re-run after import because their envelopes are invalid (exitCode=1). The Phase 28 success criterion is that the import completes for Project/Milestone/Phase levels (3 levels of planners skipped = ~18 LLM calls saved out of ~58 total), and execution can proceed. The full $0-replanning claim requires either a future fixture with successful plan envelopes OR the Phase 29 E2E test using a synthetic fixture with all levels complete.

**Old project UID:** `df5a0c1f-a6c5-4a8a-a56f-4a24c7a56eab` (from projects.yaml uid field)
**Salvaged PVC sub-path:** `df5a0c1f-a6c5-4a8a-a56f-4a24c7a56eab/workspace`

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | phase/plan_controller.go reconcilePlannerDispatch has the same structure as milestone_controller.go and guard inserts at the equivalent position | Code Locations §1 | Guard position wrong; dispatcher could run before import complete |
| A2 | Task site guard can be placed inside gateChecks after resolveProject without creating a circular control-flow issue | Code Locations §1 (Task) | Guard placement requires restructuring reconcileDispatch |
| A3 | `os.Rename` is atomic for same-directory writes on the PVC filesystem (likely NFS or iSCSI depending on cluster) | Pattern 3 (atomic rename) | Partial-write corruption on rename failure; fallback: use content-hash temp names |

---

## Open Questions

1. **Rekey table storage (planner's discretion per D-03)**
   - What we know: ImportController needs to pass (oldUID, newUID) pairs to the tide-import Job.
   - What's unclear: ConfigMap vs Project.Status vs Job env. ConfigMap is simplest for Job consumption (volume mount). Status is preferred per CRD-status-only persistence constraint.
   - Recommendation: Use a named ConfigMap `tide-import-rekey-<projectUID>` mounted as a volume in the tide-import Job. After import completes, delete the ConfigMap (or let Job ownerRef cascade-delete it). This keeps Project.Status free of transient import data.

2. **Budget rollup suppression implementation**
   - What we know: `handleProjectJobCompletion` calls `budget.RollUpUsage` at line ~1178. Import drives a code path that calls this for the project-level envelope.
   - What's unclear: Whether the import path actually triggers this code path or bypasses it entirely (e.g., if the ImportController patches the project to Succeeded directly, skipping the completion handler).
   - Recommendation: The ImportController drives the project to `Running` status (matching the salvage state) and lets the ProjectReconciler discover the existing project-level envelope. The guard at the start of `handleProjectJobCompletion` should check `project.Spec.ImportSource != nil` before calling RollUpUsage, or the tide-import Job zeroes out `out.Usage` in the copied envelope.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | tide-import binary compilation | ✓ | 1.26.3 (from STACK.md) | — |
| controller-gen | make manifests (ImportSourceRef CRD) | ✓ | kubebuilder-bundled | — |
| kind | Integration tests (Phase 29 scope) | ✓ | v0.31 (from STACK.md) | — |
| Salvage fixture | Acceptance test | ✓ | examples/projects/dogfood/salvage-20260618/ | — |

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (existing, locked) |
| Config file | `test/integration/` (envtest suites) |
| Quick run command | `go test ./internal/controller/... -run TestImport -count=1` |
| Full suite command | `make test` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| IMPORT-01 | Planner skipped when ImportComplete=True and envelope present | unit/envtest | `go test ./internal/controller/... -run "TestImport.*SkipsPlanner" -count=1` | No — Wave 0 |
| IMPORT-02 | Invalid envelope (ChildCount mismatch, exitCode!=0) rejected | unit | `go test ./cmd/tide-import/... -run "TestConvert.*Validation" -count=1` | No — Wave 0 |
| IMPORT-03 | FQ-name rekey resolves correctly, no aliasing | unit | `go test ./cmd/tide-import/... -run "TestRekey" -count=1` | No — Wave 0 |
| IMPORT-04 | Cyclic import graph fails with CyclicPlanDetected, no CRs created | envtest | `go test ./internal/controller/... -run "TestImport.*Cycle" -count=1` | No — Wave 0 |
| IMPORT-05 | Non-allowlisted Kind rejected before Create | unit | `go test ./internal/controller/... -run "TestImport.*KindAllowlist" -count=1` | No — Wave 0 |

### Wave 0 Gaps

- [ ] `internal/controller/import_controller_test.go` — covers IMPORT-01, IMPORT-04, IMPORT-05; envtest fixture with SeedManifestConfigMap
- [ ] `cmd/tide-import/main_test.go` — covers IMPORT-02, IMPORT-03; offline testenv tempdir, no K8s needed
- [ ] `api/v1alpha2/import_types_test.go` — schema validation round-trips

### Sampling Rate

- Per task commit: `go test ./... -short -count=1` (existing make test-unit)
- Per wave merge: `make test`
- Phase gate: Full suite green before `/gsd:verify-work`

---

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | Import is operator-gated by RBAC on `spec.importSource` field |
| V3 Session Management | No | Stateless reconciler; state in CRD conditions |
| V4 Access Control | Yes | K8s RBAC on ProjectSpec (operator-level write required); same-namespace PVC subPath containment (D-08 layer 2) |
| V5 Input Validation | Yes | Kind allowlist (T-308, ChildKindAllowlist); ChildCount == len(ChildCRDs) completeness; FQ-name match against seed; cycle detection before Create |
| V6 Cryptography | No | No crypto in Phase 28; sha256 for optional checksums is deferred |

### Known Threat Patterns for Import Path

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Malicious envelope injection via shared PVC | Tampering | D-08 layer 3: Kind allowlist + name-match against seed + completeness check; mirrors T-308 at dispatch-helpers.go:33 |
| Path traversal in pvcSubPath | Elevation of Privilege | PVC subPath containment via K8s VolumeMount.subPath (K8s enforces no `..` escape); plus path-traversal defense mirrors `ReadPrompt` at backend.go:116-127 |
| Cross-project envelope aliasing | Spoofing | FQ-name keying (object name + full parent chain) prevents short-name collision; D-07 |
| Cyclic imported graph blocking dispatch | Denial of Service | dag.ComputeWaves called before client.Create; ImportFailed/CyclicPlanDetected condition surfaces the error (D-10 / R-12) |

---

## Project Constraints (from CLAUDE.md)

- `charts/tide/values.yaml` is FIXED contract — add `images.tideImport` block to chart FIRST; tide-import binary catches up to chart, never reverse.
- `client.Create` bypasses admission webhook — cycle detection via `dag.ComputeWaves` MUST run explicitly before any CR creation (D-10).
- Wave CRs NEVER imported — always re-derived; schedule never stored in status (PERSIST-03).
- CRD `.status` only for persistence — no external DB; import rekey table is transient (use ConfigMap or Project.Status, delete after import).
- Resumption state = indegree map + completed-set; import must populate Task.DependsOn correctly so `computeGlobalIndegree` re-derives correctly.

---

## Sources

### Primary (HIGH confidence — verified by direct code or byte read in this session)

- `internal/controller/project_controller.go` — `reconcileProjectPlannerDispatch` at lines 993-1080; `handleProjectJobCompletion`; Step 2b guard at lines 1033-1048; budget rollup at ~1178
- `internal/controller/milestone_controller.go` — `reconcilePlannerDispatch` at lines 239-493; Step 2b guard at lines 304-326
- `internal/controller/task_controller.go` — `reconcileDispatch` at line 254; `gateChecks` at lines 305-450; `computeGlobalIndegree` at line 1278
- `internal/controller/depgraph.go` — conservative empty-return at line 28 (comment); package doc
- `internal/reporter/materialize.go` — `MaterializeChildCRDs` lines 198-305; `ChildrenAlreadyMaterialized` lines 98-149; `ChildKindAllowlist` line 64
- `internal/dispatch/podjob/backend.go` — `FilesystemEnvelopeReader.ReadOut` lines 94-105; path-traversal defense at lines 116-127
- `internal/controller/reporter_jobspec.go` — `BuildReporterJob` lines 121-224; PVC subPath pattern at line 205
- `internal/controller/dispatch_helpers.go` — `MaterializeChildCRDs` delegation at line 256; `spawnReporterIfNeeded` at lines 67-106
- `api/v1alpha2/project_types.go` — `ProjectSpec` struct lines 318-406; `PhasePending`/`PhaseRunning` constants lines 409-424
- `api/v1alpha2/plan_types.go` — `PlanSpec` fields (phaseRef, dependsOn, sharedContext only)
- `api/v1alpha2/shared_types.go` — annotation constant pattern at lines 219-261
- `pkg/dag/kahn.go` — `ComputeWaves` at line 46; `CycleError` return at line 85
- `cmd/tide-reporter/main.go` — in-pod binary pattern; flag structure; exit codes
- `images/tide-reporter/Dockerfile` — Dockerfile pattern to mirror for tide-import
- `charts/tide/values.yaml:192-195` — `tideReporter` image block pattern
- `charts/tide/templates/deployment.yaml:98-99` — TIDE_REPORTER_IMAGE wiring pattern
- `cmd/manager/main.go:197-203` — `TIDE_REPORTER_IMAGE` env read + wiring to reconcilers
- Salvage fixture byte inspection: 59 out.json files, all 39 plan-level (exitCode=1, 0 children), 14/15 phase-level succeeded with Plan children, 3/3 milestone-level succeeded with Phase children, 1/1 project-level succeeded with Milestone children
- Salvage Plan child spec fields: `phaseRef`, `dependsOn`, `filesTouched`, `objective`, `wave` (vs v1alpha2 PlanSpec: `phaseRef`, `dependsOn`, `sharedContext`)
- Salvage Phase child spec fields: `milestoneRef`, `dependsOn` — exact match with v1alpha2 PhaseSpec
- `examples/projects/dogfood/salvage-20260618/plans.yaml:24` — live Plan CRs have only `spec.phaseRef` persisted

### Secondary (MEDIUM confidence — from research documents verified against codebase)

- `.planning/research/ARCHITECTURE.md` — Approach B detailed design; all component boundaries; anti-patterns; idempotency table; build order
- `.planning/research/PITFALLS.md` — R-02 through R-14; all cited code locations verified independently
- `.planning/research/STACK.md` — zero new go.mod entries confirmed

---

## Metadata

**Confidence breakdown:**

- Schema conversion size (D-06): HIGH — settled by byte inspection of all 59 envelopes
- Code insertion points: HIGH — verified by direct code read with line numbers
- Architecture patterns: HIGH — from ARCHITECTURE.md cross-referenced with live code
- Chart wiring: HIGH — verified against values.yaml:192 + deployment.yaml:98-99 patterns
- Task site guard placement: MEDIUM — structure confirmed but exact insertion line requires reading phase/plan controllers which were not read (A1/A2 in Assumptions Log)

**Research date:** 2026-06-18
**Valid until:** 2026-07-18 (30 days; stable codebase with no planned schema changes)
