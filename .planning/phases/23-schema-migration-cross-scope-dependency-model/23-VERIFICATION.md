---
phase: 23-schema-migration-cross-scope-dependency-model
verified: 2026-06-16T00:00:00Z
status: passed
score: 6/6 must-haves verified
overrides_applied: 0
---

# Phase 23: Schema Migration + Cross-Scope Dependency Model Verification Report

**Phase Goal:** The CRD surface is re-shaped so wave derivation/ownership lives at Project scope and tasks can declare dependencies across plan/phase/milestone boundaries — all reconciled into one global Execution DAG that rejects cycles at validation — shipped behind a documented migration path that never silently corrupts an in-flight Project.
**Verified:** 2026-06-16
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria + PLAN must_haves)

| # | Truth (Success Criterion) | Status | Evidence |
|---|---------------------------|--------|----------|
| 1 | SC#1 — A Task can declare a dependency on a Task in another Plan/Phase/Milestone via a qualified ref; D-F1 plan-local restriction retired | ✓ VERIFIED | `api/v1alpha2/task_types.go:87` `DependsOn []string`; doc (l.69) "plan-local D-F1 restriction is retired"; zero `sibling` stale docs across task/phase/milestone. `TestTaskDependsOn` passes (cross-plan + scope-node names retained). Cycle gate `assembleProjectDepGraph` (project_controller.go:1442) wires task-to-task edges from `Task.Spec.DependsOn` across all plans in the namespace. |
| 2 | SC#2 — Plan/Phase/Milestone interface deps reconciled into the global task DAG (coarse edges coexist/resolve) | ✓ VERIFIED | `PlanSpec.DependsOn` added (`plan_types.go:38`); Phase/Milestone docs broadened to "any level node" (`phase_types.go:29`, `milestone_types.go:29`); D-01 honored — zero `provides/exposes/interfaceID` fields. Coarse refs conservatively skipped at the Phase-23 gate with in-code OQ#3 justification (project_controller.go:1464-1476); full fan-out is the deliberate Phase-24 deferral. `TestPlanDependsOn` passes; `TestGlobalCycleDetection/coarse_plan-scope_dep_does_NOT_trip` passes. |
| 3 | SC#3 — A cyclic global dep set is rejected at validation with involved nodes surfaced; no run, no recovery | ✓ VERIFIED | `checkGlobalCycleGate` (project_controller.go:1492) calls `dag.ComputeWaves`, errors.As `*dag.CycleError`, sets `CycleDetected`/`GlobalCycleDetected` condition with `cyc.InvolvedNodes` in message; NOT a TerminalError (allows requeue on fix), no schedule stored. Wired into Reconcile before dispatch (l.258). `TestGlobalCycleDetection` asserts BOTH involved nodes named in the condition message. |
| 4 | SC#4 — Wave ownership moved off Plan to Project scope; `{project,phase,plan,wave}` label set preserved with `wave` = global index; `task` label forbidden | ✓ VERIFIED | `WaveSpec{ProjectRef,WaveIndex}` no PlanRef (`wave_types.go`); all 6 CRDs v1alpha2 storage. `registry.go` six TELEM-03 metrics carry `[]string{"project","phase","plan","wave"}`; zero `"task"` literal. `resolveWave` (task_controller.go) reads Wave owner-ref name = global wave; resemantics documented (l.992-997). `TestWaveLabel` passes (arity lock + global-source + no-task). |
| 5 | SC#5 — Documented migration path with version bump and no silent data loss | ✓ VERIFIED | `docs/migration/v1alpha1-to-v1alpha2.md` (195 lines): what-changed, version-bump, `kubectl delete project` reinstall, fail-closed `RequiresReinstall` net. SchemaRevision guard `checkSchemaRevisionGuard` (project_controller.go:1399) returns `reconcile.TerminalError` on missing discriminator — fail-closed, never silently runs. `TestOldShapeRejection` passes; guard test keeps deliberate empty SchemaRevision. No conversion webhook (`strategy: Webhook`=0 across 6 CRDs; `plan_conversion.go` deleted). |
| 6 | Consumer migration complete — operator compiles/vets/RUNS on the served version (v1alpha2) | ✓ VERIFIED | `go build ./...`=0, `go vet ./...`=0. Zero `For(&tideprojectv1alpha1.`/`Owns(&tideprojectv1alpha1.` in internal/controller/*.go. `internal/webhook/v1alpha1/` deleted; helpers relocated to v1alpha2. Only remaining v1alpha1 imports are the 3 legitimate `api/v1alpha1/*_test.go` self-tests. envtest suite compiles + registers v1alpha2; no `webhookv1alpha1` refs. Full controller package + webhook + reporter + dashboard tests pass. |

**Score:** 6/6 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `api/v1alpha2/wave_types.go` | WaveSpec{ProjectRef,WaveIndex}, no PlanRef, storageversion | ✓ VERIFIED | ProjectRef (MinLength=1) + global WaveIndex (Minimum=0); no PlanRef; `+kubebuilder:storageversion` present |
| `api/v1alpha2/task_types.go` | DependsOn broadened any-level, D-F1 retired | ✓ VERIFIED | DependsOn field + retirement doc; XValidation rejects empty entries |
| `api/v1alpha2/plan_types.go` | DependsOn added + storageversion | ✓ VERIFIED | DependsOn added; storageversion moved off v1alpha1 Plan (0 there, 1 here) |
| `api/v1alpha2/project_types.go` | SchemaRevision Required+Enum=v1alpha2 | ✓ VERIFIED | SchemaRevision (l.311) with Required + Enum=v1alpha2 markers |
| `internal/controller/project_controller.go` | RequiresReinstall guard + cycle gate | ✓ VERIFIED | Both guards present, wired into Reconcile before dispatch, substantive logic |
| `internal/webhook/v1alpha2/{file_touch_utils,strict_mode}.go` | relocated v1alpha2-typed helpers | ✓ VERIFIED | Both exist; consumed by plan_controller via webhookv1alpha2 |
| `internal/metrics/wave_label_test.go` | arity lock test | ✓ VERIFIED | TestWaveLabel asserts {project,phase,plan,wave} + no task |
| `docs/migration/v1alpha1-to-v1alpha2.md` | migration/reinstall doc ≥25 lines | ✓ VERIFIED | 195 lines, all required sections |
| `api/v1alpha1/plan_conversion.go` | DELETED (D-09) | ✓ VERIFIED | File absent; `internal/webhook/v1alpha1/` dir also absent |
| 6× `config/crd/bases/*.yaml` | v1alpha2 served+storage, v1alpha1 unserved | ✓ VERIFIED | All 6: v1alpha1 served:false/storage:false, v1alpha2 served:true/storage:true |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `internal/controller/*_controller.go` | `api/v1alpha2` | For()/Owns() GVKs | ✓ WIRED | 0 v1alpha1 For/Owns; project For(&tidev1alpha2.Project) |
| `project_controller.go` | `pkg/dag.ComputeWaves` | global dep-graph + CycleError | ✓ WIRED | assembleProjectDepGraph → ComputeWaves → CycleError surfaced |
| `plan_controller.go` | `internal/webhook/v1alpha2` | FileTouch helper import | ✓ WIRED | consumes relocated v1alpha2 helpers; builds clean |
| `task_controller.go resolveWave` | metrics wave label | Wave owner-ref name | ✓ WIRED | returns global Wave owner-ref name → emitTaskMetrics |
| `cmd/manager/main.go` | `api/v1alpha2` | AddToScheme | ✓ WIRED | manager registers v1alpha2; runs on served version |
| `Makefile verify-no-aggregates` | `api/v1alpha2/*_types.go` | grep glob | ✓ WIRED | guard greps both v1alpha1+v1alpha2; exits 0 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Operator compiles | `go build ./...` | exit 0 | ✓ PASS |
| Operator vets clean | `go vet ./...` | exit 0 | ✓ PASS |
| No aggregate schedule cached | `make verify-no-aggregates` | OK, exit 0 | ✓ PASS |
| pkg/dag import firewall | `make verify-dag-imports` | OK, exit 0 | ✓ PASS |
| Schema field shapes | `go test ./api/v1alpha2 -run 'TestWaveSpec\|TestTaskDependsOn\|TestPlanDependsOn'` | ok | ✓ PASS |
| Old-shape rejection + cycle gate | `go test ./internal/controller -run 'TestOldShapeRejection\|TestGlobalCycleDetection'` | ok (cycle surfaces both nodes) | ✓ PASS |
| Metric arity lock | `go test ./internal/metrics -run TestWaveLabel` | ok | ✓ PASS |
| envtest suite compiles + v1alpha2 scheme | `go test -c ./test/integration/envtest/...` | exit 0, 0 webhookv1alpha1 refs | ✓ PASS |
| Full controller package | `go test ./internal/controller/... -count=1` | ok (64s) | ✓ PASS |
| Consumer pkgs (webhook/reporter/dashboard) | `go test ./internal/webhook/... ./internal/reporter/... ./cmd/dashboard/...` | all ok | ✓ PASS |
| No dependency drift | `git diff go.mod go.sum` | empty | ✓ PASS |
| No uncommitted CRD/codegen drift | `git status --porcelain` | clean | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| SCHEMA-01 | 23-01 | Wave ownership Plan→Project; global wave index | ✓ SATISFIED | WaveSpec{ProjectRef,WaveIndex}; CRDs storage=v1alpha2; truth #4 |
| SCHEMA-02 | 23-03 | `wave` label resemanticized global; {project,phase,plan,wave} kept; task forbidden | ✓ SATISFIED | registry.go arity; resolveWave global source; TestWaveLabel; truth #4 |
| SCHEMA-03 | 23-02, 23-03, 23-04 | Documented migration + version bump; no silent corruption | ✓ SATISFIED | migration doc + RequiresReinstall fail-closed guard + operator-on-served-version; truths #5,#6 |
| DEPS-01 | 23-01, 23-02 | Task deps cross plan/phase/milestone; D-F1 retired | ✓ SATISFIED | task DependsOn broadened; cycle gate wires cross-plan edges; truth #1 |
| DEPS-02 | 23-01 | Plan/Phase/Milestone interface deps into global DAG | ✓ SATISFIED | Plan.DependsOn added; coarse refs skipped conservatively (Phase-24 fan-out); truth #2 |
| DEPS-03 | 23-03 | Cyclic global DAG rejected at validation, involved nodes surfaced, no recovery | ✓ SATISFIED | checkGlobalCycleGate + CycleError + InvolvedNodes condition; truth #3 |

All 6 requirement IDs from PLAN frontmatter are accounted for and satisfied. No orphaned requirements (REQUIREMENTS.md maps exactly these 6 to Phase 23).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| internal/controller/{wave,plan}_controller.go | various | `TODO(phase-24)` stubs | ℹ️ Info | DELIBERATE per scope note — wave-derivation engine is Phase 24. All reference formal follow-up (phase-24); not debt-gate blockers. wave_controller compiles against v1alpha2 WaveSpec (ProjectRef), Ready condition intact. |
| internal/controller/project_controller.go | 1432-1433 | doc comment says "v1alpha1.Tasks" while code uses `tidev1alpha2.TaskList` | ℹ️ Info | Cosmetic stale comment only; code is correct v1alpha2. No functional impact. |
| internal/controller/project_controller.go | 251-261 | redundant secondary v2 Get (primary `project` is already v1alpha2) | ℹ️ Info | Belt-and-suspenders; harmless. Plan 23-04 marked this collapse optional. |

No TBD/FIXME/XXX debt markers in any phase-modified production file.

### Probe Execution

No `scripts/*/tests/probe-*.sh` declared or implied for this schema/migration phase. N/A.

### Human Verification Required

None. All criteria verified programmatically via build/vet/test/grep against the merged codebase.

**Informational note (not a gap):** Layer B kind-e2e (`make test-int` full, Docker-gated) was NOT run by the orchestrator or this verification. For Phase 23, the unique concern Layer B would catch beyond envtest — "operator runs on the served version at runtime" — is already covered by: (a) zero reconcilers watching unserved v1alpha1 GVKs, (b) envtest scheme registering v1alpha2, (c) CRD served/storage flags correct on all 6 CRDs, (d) manager AddToScheme(v1alpha2). The global wave-derivation/dispatch behavior that a full kind run exercises is deliberately stubbed (`TODO(phase-24)`), so Layer B sign-off is appropriately deferred to Phase 24+. Not required for Phase 23 sign-off.

### Gaps Summary

No gaps. The phase goal is genuinely achieved: the CRD surface is re-shaped (Wave re-owned Plan→Project with a global WaveIndex; `dependsOn` broadened to any-level on every level with no competing interface-id system), one global cross-scope cycle gate rejects cyclic task graphs at validation surfacing involved nodes with no recovery, and the breaking migration ships behind a documented reinstall path plus a fail-closed `RequiresReinstall` guard that never silently runs old-shape objects. The late-discovered consumer-migration gap (plan 23-04) is fully closed — the operator compiles, vets, and runs on the served v1alpha2 version with no reconciler bound to an unserved GVK. The wave-derivation engine and coarse-ref fan-out are deliberate, in-code-marked Phase-24 deferrals consistent with the phase boundary.

CLAUDE.md doctrine honored: no cached schedule (verify-no-aggregates green, gate discards ComputeWaves output), values.yaml untouched, cycle detection controller-side (CEL-except-cycle-detection), pkg/dag kept k8s-free (verify-dag-imports green).

---

_Verified: 2026-06-16_
_Verifier: Claude (gsd-verifier)_

---

## Post-Verification: Code-Review Gap Closure (plan 23-05)

After verification passed, the advisory code-review gate (`23-REVIEW.md`: 0 blockers, 4 warnings) was acted on in full per user decision. Plan 23-05 closed all four:

- **WR-04** (real, vs locked D-01) — CEL now rejects empty-string + self-reference `dependsOn` on Task/Plan/Milestone/Phase; `Plan.DependsOn` gained the validation it lacked.
- **WR-02** (real, vs DEPS-03) — `ProjectReconciler` watches `Task`, so a fixed cycle clears the `CycleDetected` condition (no longer sticky).
- **WR-01** — Wave observational roll-up is now project-scoped (`owner.LabelProject == Wave.Spec.ProjectRef`); no cross-plan contamination.
- **WR-03** — single accurate `api/v1alpha2` import alias across 125 files.

Two test fixtures (`wave_controller_test.go`, `indegree_test.go`) were updated to stamp/align the project label the WR-01 scoping requires. Full bar re-confirmed green: `go build`/`go vet`=0, `make verify-no-aggregates`/`verify-dag-imports`=0, `make test`=0, `make test-int-fast`=0 (38/38 specs). Phase status remains **passed**.
