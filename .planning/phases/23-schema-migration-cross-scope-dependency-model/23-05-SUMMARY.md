---
phase: 23-schema-migration-cross-scope-dependency-model
plan: "05"
status: complete
gap_closure: true
requirements: [DEPS-01, DEPS-02, DEPS-03]
completed: 2026-06-16
key_files:
  created: []
  modified:
    - api/v1alpha2/task_types.go
    - api/v1alpha2/plan_types.go
    - api/v1alpha2/milestone_types.go
    - api/v1alpha2/phase_types.go
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - internal/controller/project_controller.go
    - internal/controller/wave_controller.go
    - internal/controller/wave_controller_test.go
    - test/integration/envtest/indegree_test.go
---

# Plan 23-05 Summary — Code-Review Gap Closure (WR-01..04)

Closed all four advisory warnings from `23-REVIEW.md` (0 blockers, 4 warnings). Tasks 1–3 were executed by a worktree subagent; Task 4 (the alias rename) was completed by the orchestrator after the subagent died on a transient API 500 mid-sweep (its 3 clean commits were merged; the partial uncommitted rename was discarded and redone deterministically).

## What changed

- **WR-04 — CEL validation on `dependsOn` (commit `d5790ae`).** Every `dependsOn []string` across the v1alpha2 Task/Plan/Milestone/Phase Kinds now rejects empty-string items (`items:MinLength=1`) and self-reference via a Kind-root `x-kubernetes-validations` rule (`!(self.metadata.name in self.spec.dependsOn)`, message "a <kind> cannot depend on itself"). `Plan.Spec.DependsOn`, which previously had no CEL at all, now carries both. CRDs regenerated. Honors D-01 (flat IDs, no provides/exposes).
- **WR-02 — cycle-gate condition no longer sticky (commit `e590e7f`).** `ProjectReconciler.SetupWithManager` now `.Watches(&Task{}, EnqueueRequestsFromMapFunc(taskToProject))`; the mapper reads `tideproject.k8s/project` and requeues the owning Project, so editing a Task's `dependsOn` to break a cycle clears the `CycleDetected` condition. Watch (not Owns) — Tasks aren't owned by Project.
- **WR-01 — Wave roll-up scoped by project (commit `4fba0b4`).** `wave_controller.reconcileObservational` now filters member Tasks by `owner.LabelProject == Wave.Spec.ProjectRef` (in addition to wave-index), removing the namespace-wide cross-plan/cross-project contamination from bare wave-index matching. The `// TODO(phase-24)` note remains (full global Wave→Task re-association is Phase 24); this is the safe interim scoping.
- **WR-03 — single accurate import alias (commit `3496fd4`).** Renamed the misleading post-migration aliases (`tideprojectv1alpha1`/`tidev1alpha1` that 23-04 left pointing at `api/v1alpha2`) to `tideprojectv1alpha2`/`tidev1alpha2` across 125 files (word-boundary identifier rename only — never touched `v1alpha1` string literals/GVK refs/unservedversion markers). Deduped the two collision files (`wave_controller.go`, `cmd/manager/main.go`) that imported v2 under both aliases. Files importing the *real* `api/v1alpha1` (the old-object-rejection guard test + the three `api/v1alpha1` own-tests) were correctly left untouched.

## Fixture fixes (caught by the post-merge test gate, not the subagent)

- **`wave_controller_test.go` (commit `9a63a50`).** The unit roll-up fixtures didn't stamp the project label WR-01 now filters on → 3 controller specs failed. Added `tideproject.k8s/project: planRef` to the `makeWaveWithTasks` Task fixtures.
- **`indegree_test.go` (commit `08c9d60`).** The integration roll-up Waves set `ProjectRef: planName` while their member Tasks carry `tideproject.k8s/project: indegreeTestProject` → 2 integration specs timed out (60s × 3 flake-attempts). Aligned `Wave.Spec.ProjectRef` to `indegreeTestProject`.

## Verification (observed, not assumed)

- `go build ./...` = 0; `go vet ./...` = 0
- `make verify-no-aggregates` = 0; `make verify-dag-imports` = 0
- `make test` (unit + controller envtest) = 0 (controller pkg 71.3% coverage)
- `make test-int-fast` (Layer A integration) = 0 — **38/38 Specs SUCCESS** in 31s
- Zero `(tideprojectv1alpha1|tidev1alpha1) ".../api/v1alpha2"` remain; real `api/v1alpha1` importers preserved
- All four CRD bases carry a `cannot depend on itself` CEL rule; `Plan` CRD now has dependsOn validation

## Self-Check: PASSED
