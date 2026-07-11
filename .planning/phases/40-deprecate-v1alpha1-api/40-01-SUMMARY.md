---
phase: 40-deprecate-v1alpha1-api
plan: 01
subsystem: api
tags: [kubebuilder, crd, controller-gen, helmify, go]

# Dependency graph
requires:
  - phase: 23-v1alpha2-schema-reshape
    provides: api/v1alpha2 package + 2-version CRD manifests (the copy-and-reshape precedent, commit 67cb313)
provides:
  - api/v1alpha3 Go package (compiling, deepcopy-generated) as the landing zone for the rest of Phase 40
  - 3-version transitional CRD manifests (v1alpha1 dead, v1alpha2 served, v1alpha3 served+storage)
  - regenerated charts/tide-crds/ templates matching the 3-version manifests
  - D-02 artifact-first LevelOverrides/SubagentConfig doc comments (source text for wave-2 consumer migration)
affects: [40-02, 40-03, 40-04, 40-05, 40-06, 40-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "copy-and-reshape API version introduction (Phase 23 precedent): duplicate the prior version's Go package verbatim, apply the version bump + targeted field changes, regenerate deepcopy/manifests/chart in the same commit that moves +kubebuilder:storageversion"

key-files:
  created:
    - api/v1alpha3/groupversion_info.go
    - api/v1alpha3/shared_types.go
    - api/v1alpha3/project_types.go
    - api/v1alpha3/milestone_types.go
    - api/v1alpha3/phase_types.go
    - api/v1alpha3/plan_types.go
    - api/v1alpha3/task_types.go
    - api/v1alpha3/wave_types.go
    - api/v1alpha3/import_types.go
    - api/v1alpha3/zz_generated.deepcopy.go
    - api/v1alpha3/schema_test.go
  modified:
    - api/v1alpha2/milestone_types.go
    - api/v1alpha2/phase_types.go
    - api/v1alpha2/plan_types.go
    - api/v1alpha2/project_types.go
    - api/v1alpha2/task_types.go
    - api/v1alpha2/wave_types.go
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_projects.yaml
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_waves.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - charts/tide-crds/templates/task-crd.yaml
    - charts/tide-crds/templates/wave-crd.yaml
    - api/v1alpha1/phase35_schema_test.go

key-decisions:
  - "ProjectSpec.ModelSelection (struct + field) dropped from v1alpha3 (D-10) — zero readers outside api/, duplicates the wired Subagent.Levels"
  - "LevelOverrides/SubagentConfig doc comments rewritten to the artifact-first D-02 semantics in v1alpha3 only; Go field names and JSON tags unchanged from v1alpha2"
  - "+kubebuilder:storageversion moved from the 6 v1alpha2 Kinds onto the 6 v1alpha3 Kinds atomically (same commit as the manifest regen) — no intermediate two-storage-version state"

patterns-established:
  - "Version-block-count assertions in schema tests should derive the expected count from the CRD's own `name: v1alpha*` occurrences, not hardcode a number — a future v1alpha4 crank shouldn't require a second hand-edit to this class of test"

requirements-completed: [CRANK-01]

# Metrics
duration: ~30min
completed: 2026-07-11
---

# Phase 40 Plan 01: Introduce api/v1alpha3 Summary

**Copy-and-reshape of api/v1alpha2 into api/v1alpha3 (Phase 23 precedent): dropped the dead ModelSelection field (D-10), rewrote LevelOverrides docs to artifact-first semantics (D-02), and regenerated 3-version transitional CRD manifests + chart with v1alpha3 as sole storage version.**

## Performance

- **Duration:** ~30 min
- **Tasks:** 2 completed (plus 1 in-scope deviation fix)
- **Files modified:** 25 (11 created, 14 modified)

## Accomplishments
- New `api/v1alpha3` Go package compiles standalone, mirrors `api/v1alpha2` byte-for-byte except the D-10/D-02 changes, with deepcopy generated via `make generate`.
- `ProjectSpec.ModelSelection` (struct + field) removed from v1alpha3 — the dead field that duplicated `Subagent.Levels` never carries into the new schema.
- `LevelOverrides`/`SubagentConfig` doc comments rewritten to describe the D-02 artifact-first semantics (`levels.milestone` authors MILESTONE.md, `levels.phase` authors phase briefs, `levels.plan` authors PLAN.md + the task DAG, `levels.task` is the executor) — these comments generate directly into the CRD's OpenAPI description fields.
- All 6 CRD manifests regenerated to a 3-version transitional shape: v1alpha1 (served:false/storage:false, unchanged), v1alpha2 (served:true/storage:false — storage version handed off), v1alpha3 (served:true/storage:true — new sole storage version).
- `charts/tide-crds/templates/` regenerated via `make helm-crds`; `make verify-chart-reproducible` confirms no hand-edit drift. `Chart.yaml` untouched (no version bump in this phase per plan constraint).
- New `api/v1alpha3/schema_test.go` (Wave 0 reflect-based structural test) proves the shape: no `ModelSelection`, `LevelOverrides` fields/json-tags intact, `SchemaRevision` discriminator present.

## Task Commits

1. **Task 1: Create api/v1alpha3 as a copy-and-reshape of api/v1alpha2** - `bd480b7` (feat)
2. **Task 2: Schema test + manifest/chart regeneration** - `ee24b57` (test)
3. **Deviation fix: un-hardcode stale baseRef/baseSHA version-count assertions** - `16c5f45` (fix, Rule 1)

_No plan-metadata commit yet — this worktree agent does not update STATE.md/ROADMAP.md; the orchestrator commits those after the wave merges._

## Files Created/Modified

- `api/v1alpha3/groupversion_info.go` - GroupVersion `tideproject.k8s/v1alpha3` + AddToScheme
- `api/v1alpha3/shared_types.go` - status-condition/reason vocabulary + `FailureProfileType` (package-renamed copy; `ReasonRequiresReinstall` prose updated to describe the v1alpha3 reinstall path)
- `api/v1alpha3/project_types.go` - `ProjectSpec`/`ProjectStatus`/`Project` plus `SecretRefs`, `Gates`, `SubagentConfig`, `LevelOverrides`, `LevelConfig`, `GitConfig`, `GitStatus`, `BudgetStatus`, `BoundaryPushStatus` (ModelSelection removed, SchemaRevision Enum=v1alpha3, LevelOverrides docs rewritten)
- `api/v1alpha3/milestone_types.go`, `phase_types.go`, `plan_types.go`, `task_types.go`, `wave_types.go`, `import_types.go` - package-renamed copies, no field changes
- `api/v1alpha3/zz_generated.deepcopy.go` - generated via `make generate`
- `api/v1alpha3/schema_test.go` - new Wave 0 structural test (3 tests, 107 lines)
- `api/v1alpha2/{milestone,phase,plan,task,wave,project}_types.go` - `+kubebuilder:storageversion` marker removed (storage handoff to v1alpha3)
- `config/crd/bases/tideproject.k8s_{milestones,phases,plans,projects,tasks,waves}.yaml` - regenerated via `make manifests`, now carrying 3 version blocks each
- `charts/tide-crds/templates/{milestone,phase,plan,project,task,wave}-crd.yaml` - regenerated via `make helm-crds`
- `api/v1alpha1/phase35_schema_test.go` - version-count assertions un-hardcoded from `2` to a derived count (see Deviations)

## Decisions Made

- Kept `SubagentConfig`/`LevelOverrides`/`ModelSelection`/`Gates`/etc. in `project_types.go` (not `shared_types.go`), matching the actual current v1alpha2 file layout confirmed by direct read (`shared_types.go` in this repo holds only the status-condition/reason vocabulary + `FailureProfileType`). No file reorganization needed — all acceptance-criteria greps for LevelOverrides content resolve correctly against `project_types.go` as-is.
- Left the historical "Phase N condition + reason vocabulary" comment blocks in `shared_types.go` as-is (matching the copy-and-reshape philosophy of preserving historical phase attribution) except the `ReasonRequiresReinstall` guard prose, which was updated to name v1alpha3 since it describes the guard's *current* behavior, not a historical event.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Un-hardcoded stale v1alpha1 CRD version-count assertions**
- **Found during:** Task 2 (`make test` overall verification)
- **Issue:** `api/v1alpha1/phase35_schema_test.go`'s `TestProjectCRDSchemaHasBaseRefBothVersions` and `TestProjectCRDSchemaHasBaseRefPattern` asserted `baseRef:`/`baseSHA:`/pattern-marker occurrences equal exactly `2` ("one per version block"), pinned when only v1alpha1+v1alpha2 existed. Task 2's manifest regeneration legitimately added a third (v1alpha3) version block that also carries these fields, so the count became `3` and both tests failed.
- **Fix:** Derived the expected count from the CRD's own `name: v1alpha*` occurrences (`wantVersions := strings.Count(crd, "name: v1alpha")`) instead of a hardcoded literal, so a future v1alpha4 crank won't need a second hand-edit to this test class.
- **Files modified:** `api/v1alpha1/phase35_schema_test.go`
- **Verification:** `go test ./api/v1alpha1/... -run TestProjectCRDSchemaHasBaseRef` green; full `make test-only` unit tier green (all packages, no other regressions).
- **Committed in:** `16c5f45`

---

**Total deviations:** 1 auto-fixed (Rule 1 - Bug)
**Impact on plan:** Necessary correctness fix directly caused by this plan's own CRD regeneration; no scope creep — no other files touched.

## Issues Encountered

None beyond the deviation above. `bin/controller-gen`, `bin/helmify`, `bin/kustomize`, `bin/setup-envtest`, and `bin/golangci-lint` were seeded into this worktree's gitignored `bin/` from the main repo's cache to avoid redundant downloads; `controller-gen`/`kustomize`/`helmify` were re-downloaded anyway by `go-install-tool`'s pinned-symlink check but resolved fine over network access.

Note for the verifier: Task 1's acceptance-criteria grep `grep -c 'json:"milestone,omitempty"\|...' api/v1alpha3/project_types.go` returns `9`, not the plan's expected `4`. This is because `Gates` (a distinct struct, also in `project_types.go`) declares its own `Milestone`/`Phase`/`Plan`/`Task` `GatePolicy` fields with the same four json tags, independent of `LevelOverrides`. The semantic truth the criterion was checking — LevelOverrides retains all four fields with unchanged json tags — holds; `api/v1alpha3/schema_test.go`'s `TestLevelOverridesShape` asserts it directly by field.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

`api/v1alpha3` is ready as the landing zone for Wave 2 consumer migration (plans 40-02 through 40-07): webhooks, controller dispatch call-site renames, owner-ref checks, scheme registrations, docs/samples, and the envelope decoupling all build on this package. The 3-version transitional CRD state (v1alpha1 dead, v1alpha2 served, v1alpha3 served+storage) is live and reproducible; no blockers for subsequent plans in this phase.

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-11*
