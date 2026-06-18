# Phase 28: Plan-Import Core - Context

**Gathered:** 2026-06-18
**Status:** Ready for planning

<domain>
## Phase Boundary

A fresh `kubectl apply` of an already-planned Project adopts pre-authored planner
envelopes and **skips the planner for every level whose valid envelope exists**,
proceeding straight to materialize → execution with no re-paid planning cost. The
phase bridges UID-churn between the salvaged run and the new run, validates every
envelope before adoption, runs cycle detection before materializing any child CRDs,
converts v1alpha1 embedded child specs to v1alpha2, and **never imports Wave CRs**
(always re-derived).

**In scope:** the import *mechanism* (controller + Job + schema/CRD surface) validated
against the `salvage-20260618` fixture. **Out of scope (Phase 29):** the `tide` CLI
export/import-envelopes commands, dry-run mode, and the kind E2E test.

</domain>

<decisions>
## Implementation Decisions

### Import mechanism — Approach B (UID-rewrite import step)
- **D-01:** Adopt **Approach B**, not Approach A. A one-shot pre-reconcile import step
  re-keys old-UID envelope trees to new UIDs; the five reconcilers, the reporter Job,
  and the `FilesystemEnvelopeReader` UID-keyed path contract stay **byte-for-byte
  unchanged**. Rationale: the salvage fixture has only UID-keyed paths (no by-name
  paths ever written), so Approach A would need a migration step *functionally identical
  to B* **plus** permanent dual-path complexity on the hot `ReadOut` path (5 reconcilers +
  reporter + 2 reader impls) and cross-project name-collision risk. B confines all
  complexity to a one-shot phase that disappears after `ImportComplete=True`.
- **D-02:** New surface: `ImportSourceRef` field on `ProjectSpec` (`api/v1alpha2`); a new
  `internal/controller/import_controller.go` state machine
  (`Pending → CreatingCRs → CopyingEnvelopes → Complete / Failed`); a new
  `cmd/tide-import/main.go` in-pod binary (copy + re-key + atomic `out.json` TaskUID
  rewrite); and an `ImportComplete` condition guard added as a **one-liner at each of the
  five dispatch sites** (park if `spec.importSource != nil && ImportComplete != True`).

### Trigger surface — single Project + seed ConfigMap
- **D-03:** Operator applies **one Project** carrying
  `spec.importSource = {seedConfigMapRef, pvcSubPath}`. The `ImportController` reads the
  seed (full CR specs + `name→oldUID` map), **materializes the CR tree itself** (K8s
  assigns new UIDs), records the `name→newUID` rekey table, then dispatches `tide-import`.
  Rationale: matches success-criterion #1 ("fresh kubectl apply of an already-planned
  Project adopts"), and captures the old→new UID mapping **atomically at creation time**.
  Rejected: operator applying the exported CR tree directly — applied CRs get fresh UIDs,
  old UIDs are stripped on create, so the mapping would have to be captured out-of-band
  (fragile, more operator steps).
- **D-04:** The seed YAMLs cover **down to Plan level only** (the fixture has
  `projects/milestones/phases/plans.yaml`, **no `tasks.yaml`**). Therefore Project →
  Milestone → Phase → Plan are materialized from the clean v1alpha2 seed; **only Tasks are
  materialized from plan-level envelope children** via the normal reporter/materializer
  path. This narrows where schema conversion actually matters (see D-05).

### Schema conversion — in the import Job, then strict-validate
- **D-05:** The `tide-import` Job upgrades any v1alpha1 embedded child `Spec.Raw` bytes to
  v1alpha2 **as it copies envelopes to new-UID paths**, then **strict-validates** the
  result against the v1alpha2 struct. The steady-state `MaterializeChildCRDs` /
  reporter path stays **unchanged** (preserves B's core win). A child that cannot be
  converted to valid v1alpha2 **fails the import** (`ImportFailed`) — never silently
  adopted. By reconcile time, all on-disk bytes are valid v1alpha2.
- **D-06:** The envelope *contract* version constant is still `APIVersionV1Alpha1`
  (`pkg/dispatch/envelope.go:24`) on current `main`, and `ValidateAPIVersionKind` requires
  exactly that — so the envelope **wire format is unchanged**; the v1alpha1/v1alpha2 concern
  (R-06) is narrowly about the **embedded child-CRD `Spec.Raw` bytes** (the Task specs),
  not the envelope envelope itself. ⚠ **Research flag:** the salvage `in.json` carries
  `schemaRevision: v1alpha2` and SEED-OUTLINE claims children are v1alpha2, yet R-06
  (today's research) asserts v1alpha1 `Spec.Raw`. The researcher MUST settle this by
  diffing actual salvage child bytes against the v1alpha2 struct to size the conversion
  delta (it may be a no-op, or a real field migration). Do not assume.

### Stable identity & trust matching
- **D-07:** Rekey table keys on the **fully-qualified name = object name + full parent
  chain** (e.g. `milestone-02/phase-03/plan-01-foo`), guaranteeing a 1:1 mapping even where
  sibling subtrees reuse short names (the dogfood tree reuses `plan-01-*` / `phase-01-*`
  across milestones — bare-name keying would alias). `tide-import` validates each
  envelope's `out.ChildCRDs[*].Name` against the **seed's declared children at that level**;
  an envelope declaring a child the seed tree does not contain → `ImportFailed`. Combined
  with the T-308 `Kind` allowlist, this blocks cross-object/cross-project aliasing and
  child injection.
- **D-08:** Provenance is trust-bounded in **three layers** before any `client.Create`:
  (1) import only fires from `spec.importSource`, settable only by an operator with
  namespace RBAC (the trust anchor); (2) `tide-import` reads **only** from the declared
  `pvcSubPath` within **this namespace's** PVC — no cross-namespace reads, no absolute-path
  escape; (3) every materialized child is `Kind`-allowlisted (T-308) + name-matched to the
  seed tree + completeness-checked (`len(ChildCRDs) == ChildCount`). Per-envelope sha256
  checksums are a **deferred nicety** (see Deferred Ideas), not required for Phase 28.

### Invariants carried forward (locked — do not re-litigate)
- **D-09:** **Wave CRs are never imported** — always re-derived by `deriveGlobalWaves` /
  `pkg/dag.ComputeWaves` from the imported Task graph on first reconcile.
- **D-10:** **Cycle detection runs explicitly before any child CRD is created.** `client.Create`
  bypasses the admission webhook, so `dag.ComputeWaves` MUST run on the full task set during
  import; a cyclic/unresolved imported graph → `ImportFailed / CyclicPlanDetected` with **no
  partial CRs created** (import atomicity is per-Milestone; every `Task.Spec.DependsOn` entry
  must resolve before creation — R-05 / `depgraph.go:28` conservative empty-return).
- **D-11:** **Budget rollup is suppressed unconditionally for imported envelopes** — the
  prior run already counted the planning cost (R-13).
- **D-12:** `ImportComplete` condition is the **first-step idempotency guard** (at-least-once
  Job semantics); the copy step uses `cp -n` (no-clobber) + atomic rename for partial-write
  safety (Anti-Pattern 3).

### Claude's Discretion
- Exact `ImportSourceRef` field shape, seed ConfigMap schema, and condition-type naming are
  authoring decisions for the planner, within D-02/D-03.
- Whether the rekey table is recorded in a ConfigMap vs `Project.Status` is a planner choice
  (CRD-status-only persistence preferred; rekey table is transient import state).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### v1.0.3 research (the A-vs-B decision basis — all dated 2026-06-18)
- `.planning/research/ARCHITECTURE.md` — Approach B detailed design: §"Recommended
  Architecture: Approach B in Detail" (system overview, component-boundary table, import
  state machine, data flow, idempotency analysis, anti-patterns 1–5, build order). **This is
  the primary implementation reference for the chosen approach.**
- `.planning/research/SUMMARY.md` — executive synthesis; Approach A vs B framing; Phase 2
  feature list; pitfalls R-01…R-13.
- `.planning/research/FEATURES.md` — Phase 2 import-core "must have" feature set and prior-art
  (Argo/Temporal/Prefect/Dagster/Bazel) convergence.
- `.planning/research/PITFALLS.md` — R-02 (UID aliasing), R-05 (partial-plan corrupts DAG),
  R-06 (v1alpha1 `Spec.Raw`), R-12 (cycle detection bypass), R-13 (rollup suppression).
- `.planning/research/STACK.md` — zero new `go.mod` entries; all stdlib + existing
  controller-runtime patterns.

### Salvage fixture (the acceptance target)
- `examples/projects/dogfood/salvage-20260618/SEED-OUTLINE.md` — 3 milestones / 15 phases /
  42 plans; the human-readable tree the seed encodes.
- `examples/projects/dogfood/salvage-20260618/{projects,milestones,phases,plans}.yaml` —
  exported v1alpha2 CR tree (down to Plan level; **no tasks.yaml**).
- `examples/projects/dogfood/salvage-20260618/pvc-envelopes.tgz` — UID-keyed envelope trees
  (`envelopes/<oldUID>/{in,out}.json`, `children/*.json`, `events.jsonl`).

### Spec invariants
- `README.md` — §"Failure handling at wave boundaries"; waves are derived-not-declared;
  cycles are bugs (refuse, don't recover); resumption = indegree map + completed-set.

### Requirements / roadmap
- `.planning/REQUIREMENTS.md` — IMPORT-01…IMPORT-05 (Phase 28), TOOL-01/02 (Phase 29).
- `.planning/ROADMAP.md` §"Phase 28: Plan-Import Core" — goal + 5 success criteria.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/dispatch/envelope.go:24` `APIVersionV1Alpha1` constant + `ValidateAPIVersionKind`
  (`:400-408`) — envelope completeness/version validation reused by the import validator.
- `internal/controller/dispatch_helpers.go` `MaterializeChildCRDs` + T-308 `Kind` allowlist —
  the child-materialization + trust-boundary pattern the import path validates against
  (D-07/D-08); stays **unchanged** for steady state (D-05).
- `pkg/dag/kahn.go:46` `ComputeWaves` — the cycle-detection + wave-derivation entry point to
  call explicitly before materializing (D-10).
- `internal/controller/task_controller.go:1278` `computeGlobalIndegree` — global readiness
  re-derive; consumes imported Task `DependsOn` edges unchanged.
- `api/v1alpha2/shared_types.go` — annotation-constant pattern (`AnnotationBillingResumedAt`
  at `:216/:253`) to mirror for any import-marker constant.
- `internal/controller/reporter_jobspec.go` `BuildReporterJob` — **unchanged**; reads
  new-UID envelopes that import copied.

### Established Patterns
- **`AlreadyExists`-is-success idempotency** (`internal/dispatch/podjob/names.go`, SUB-03) —
  extends naturally to the planner-skip and import-copy operations; CRs created by import are
  no-ops if the reporter later tries to re-create them.
- **UID-keyed envelope path contract** (`internal/dispatch/podjob/backend.go:95`:
  `WorkspaceRoot/{projectUID}/workspace/envelopes/{taskUID}/out.json`) — the contract import
  must satisfy by placing files at new-UID paths; **not modified** (the Approach B win).
- **Conservative dep-resolution** (`internal/controller/depgraph.go:28`) — unresolved ref =
  no edge → missing Tasks silently get zero indegree and dispatch immediately. Import
  atomicity (D-10) exists precisely to prevent this (R-05).

### Integration Points
- Five planner-dispatch sites get the `ImportComplete` park guard:
  `internal/controller/project_controller.go` (`reconcileProjectPlannerDispatch`) +
  `milestone_controller.go` / `phase_controller.go` / `plan_controller.go`
  (`reconcilePlannerDispatch`).
- New `ImportController` registered in `cmd/manager/` alongside existing controllers.
- New `cmd/tide-import/` binary (stdlib I/O only) + its Dockerfile/chart image wiring.

### Research flags for the phase-researcher
1. **Settle the child-bytes schema contradiction (D-06):** diff actual `salvage-20260618`
   envelope `children/*.json` / `out.json` `Spec.Raw` against the live v1alpha2 structs to
   determine whether conversion is a no-op or a real field migration. `in.json` claims
   `schemaRevision: v1alpha2`; R-06 claims v1alpha1. Bytes decide.
2. **Confirm the Task-materialization path** is the *only* envelope-sourced CR creation under
   the seed-down-to-Plan model (D-04): trace whether the reporter Job re-materializes
   Plan-level children when CRs already exist from the seed (`AlreadyExists`-is-success).
3. **Confirm R-06's `Wave.Spec.ProjectRef` orphan mechanism** is still reachable given waves
   are re-derived (D-09) — it may be moot if Waves never carry imported bytes.

</code_context>

<specifics>
## Specific Ideas

- The whole phase is anchored on making the **real `salvage-20260618` fixture** resumable —
  ~$90 of planning the dogfood run #2 paid before budget-halting with zero execution. "Adopt
  envelopes, skip planners, pay $0 planning" is the concrete bar; the Phase 29 kind E2E test
  asserts it end-to-end.

</specifics>

<deferred>
## Deferred Ideas

- **Hybrid write-side (by-name envelope paths going forward):** have the planner *also* write
  `envelopes/by-name/<fqName>/out.json` at completion so future salvages need no import Job at
  all. This is a **new capability** (roadmap scoped import, not envelope write-format) — its
  own phase / backlog item, not Phase 28. (Surfaced during the A-vs-B discussion.)
- **Per-envelope sha256 integrity checksums in the seed manifest:** stronger tamper/partial-
  write detection beyond `len(ChildCRDs) == ChildCount`. Belongs with the Phase 29
  export/seed tooling that would generate the hashes; deferred from Phase 28's trust gate.
- **Automatic export-on-halt** (snapshot envelopes to a durable bundle when a budget/failure
  halt fires) — already listed as a deferred future requirement; convenience layer atop
  TOOL-01.

None of these block Phase 28; discussion otherwise stayed within phase scope.

</deferred>

---

*Phase: 28-plan-import-core*
*Context gathered: 2026-06-18*
