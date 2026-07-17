---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 07
subsystem: observability
tags: [otel, opentelemetry, phoenix, controller-runtime, crd-status, reporter-job, ttl-gc]

# Dependency graph
requires:
  - phase: 47-06
    provides: prior-wave verification gaps context (CR-01 identified in 47-VERIFICATION.md)
provides:
  - Durable `*ReporterSpawnedUID` marker fields on all five v1alpha3 status types (Milestone/Phase/Plan/Project/Task)
  - Gate+stamp logic at all five reporter-spawn call sites, closing the CR-01 TTL-GC duplicate-reporter window
affects: [47-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Durable per-attempt spawn marker (keyed on completed Job UID, falls back to deterministic Job name when completedJob is nil) gates reporter Job Create, mirroring the proven *RolledUpUID budget-rollup marker idiom"
    - "Stamp via RetryOnConflict + re-fetch-latest + idempotent-equality-check + MergeFromWithOptimisticLock, deliberately unconditional on isFirstCompletion so pre-existing reporters back-fill the marker"

key-files:
  created: []
  modified:
    - api/v1alpha3/milestone_types.go
    - api/v1alpha3/phase_types.go
    - api/v1alpha3/plan_types.go
    - api/v1alpha3/project_types.go
    - api/v1alpha3/task_types.go
    - config/crd/bases/tideproject.k8s_milestones.yaml
    - config/crd/bases/tideproject.k8s_phases.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/crd/bases/tideproject.k8s_projects.yaml
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - charts/tide-crds/templates/milestone-crd.yaml
    - charts/tide-crds/templates/phase-crd.yaml
    - charts/tide-crds/templates/plan-crd.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - charts/tide-crds/templates/task-crd.yaml
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go

key-decisions:
  - "Reused the exact *RolledUpUID stamp idiom (RetryOnConflict + re-fetch + optimistic-lock MergeFrom) for the new spawn markers rather than inventing a lighter-weight scheme, keeping the two independent marker families consistent for future readers"
  - "Task-level marker check has no name-fallback branch (unlike the four planner-level markers) since spawnTaskTraceReporterIfNeeded's early nil-guard already guarantees completedJob != nil at the gate"
  - "zz_generated.deepcopy.go needed no changes -- all five new fields are plain strings, copied by the struct's shallow *out = *in assignment, matching the pre-existing *RolledUpUID/*SpanEmittedUID fields"

patterns-established: []

requirements-completed: [PROOF-01]

# Metrics
duration: 35min
completed: 2026-07-17
---

# Phase 47 Plan 07: Durable Reporter-Spawn Markers Summary

**Five new `*ReporterSpawnedUID` CRD status fields gate all five reporter-Job spawn sites on a durable per-attempt marker, closing the CR-01 TTL-GC duplicate-reporter window that caused Phase 47's live proof to measure only 115/386 fully-enriched LLM spans.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-07-17T18:21:45Z
- **Tasks:** 2/2
- **Files modified:** 20 (10 hand-edited controller/API files + 10 generator output: 5 `config/crd/bases/*.yaml` + 5 `charts/tide-crds/templates/*.yaml`)

## Accomplishments
- Added `MilestoneReporterSpawnedUID`, `PhaseReporterSpawnedUID`, `PlanReporterSpawnedUID`, `ProjectReporterSpawnedUID`, `TaskTraceReporterSpawnedUID` — additive optional string fields on the five v1alpha3 status types, regenerated into CRD manifests and the Helm CRD subchart.
- Gated reporter Job `Create` at all five spawn sites (milestone/phase/plan/project planner-tier + task trace-only tier) on the new durable marker, so a reconcile after the reporter Job's 300s TTL-GC no longer re-Creates a duplicate reporter with recomputed `ReporterOptions`.
- Stamped the marker durably via the `RetryOnConflict` + re-fetch + `MergeFromWithOptimisticLock` idiom immediately after a reporter Job is verifiably observed (newly-Created, `AlreadyExists`, or found-by-name) — deliberately unconditional on `isFirstCompletion` so the marker back-fills for reporters spawned before this fix.
- Preserved `reporter_jobspec.go`'s 300s TTL unchanged and preserved the independent `*RolledUpUID`/`*SpanEmittedUID` marker families untouched — the new markers are a parallel, orthogonal guard.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add reporter-spawned marker fields to all five v1alpha3 status types and regenerate manifests** - `e0b0210` (feat)
2. **Task 2: Gate reporter Create on the marker and stamp it durably at all five spawn sites** - `dfb4d8a` (feat)

**Plan metadata:** committed alongside this SUMMARY.

## Files Created/Modified
- `api/v1alpha3/{milestone,phase,plan,project,task}_types.go` - new `*ReporterSpawnedUID` status field per level, doc-comment mirrors `*RolledUpUID` voice
- `config/crd/bases/tideproject.k8s_{milestones,phases,plans,projects,tasks}.yaml` - regenerated via `make manifests generate`
- `charts/tide-crds/templates/{milestone,phase,plan,project,task}-crd.yaml` - regenerated via `make helm` (chart-reproducibility pre-commit hook requires this; `charts/tide/values.yaml` FIXED contract untouched)
- `internal/controller/milestone_controller.go` - `spawnKey`/marker gate wraps the existing `spawnReporterIfNeeded` call; stamp block added after successful spawn
- `internal/controller/phase_controller.go` - same shape as milestone
- `internal/controller/plan_controller.go` - same shape applied to the inline (non-helper) spawn block; stamp covers all three reachable branches (newly-Created, AlreadyExists, found-by-name)
- `internal/controller/project_controller.go` - same shape applied to the inline spawn arm; `plannerJobName` declaration hoisted earlier and reused at the pre-existing `PlannerRolledUpUID` stamp site
- `internal/controller/task_controller.go` - `spawnTaskTraceReporterIfNeeded` gated at entry on `TaskTraceReporterSpawnedUID`; new `stampTaskTraceReporterSpawnedUID` helper preserves the function's non-fatal (log-and-continue) posture

## Decisions Made
- Mirrored the `*RolledUpUID` stamp idiom exactly (not a simplified variant) so the two marker families read identically to future maintainers and both survive concurrent-reconcile races the same way.
- For the project and plan sites (inline spawn logic, no shared helper), wrapped the entire existing `if/else` block in the marker check rather than refactoring them to call `spawnReporterIfNeeded` — kept the diff mechanical and scoped to the CR-01 fix, no behavior-preserving refactor bundled in.
- Task's marker check omits the name-fallback branch present at the four planner-level sites, since `completedJob` is already guaranteed non-nil by an earlier guard in the same function — matches the plan's explicit instruction.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Regenerated `charts/tide-crds/templates/*.yaml` via `make helm`**
- **Found during:** Task 1 commit (pre-commit hook)
- **Issue:** The repo's `chart-reproducibility` pre-commit hook runs `make helm` and diffs `charts/` against the working tree; it failed because the new CRD status fields propagate into the Helm CRD subchart templates, which had not been regenerated. This is generator output analogous to `config/crd/bases/*.yaml` (both derived from the same `+kubebuilder` markers), not a hand-edit, and `charts/tide/values.yaml` (the FIXED contract per CLAUDE.md) was unaffected.
- **Fix:** Ran `make helm`, staged the five regenerated `charts/tide-crds/templates/*-crd.yaml` files, and included them in the Task 1 commit alongside `config/crd/bases/*.yaml`.
- **Files modified:** `charts/tide-crds/templates/{milestone,phase,plan,project,task}-crd.yaml` (not in the plan's `files_modified` list, since the plan predates this repo's chart-reproducibility hook gaining CRD-subchart coverage)
- **Verification:** `git diff --stat charts/tide/values.yaml` empty; re-ran `git commit` and the chart-reproducibility hook passed on retry.
- **Committed in:** `e0b0210` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary to satisfy an existing repo-wide pre-commit gate; no scope creep — the chart templates are 100% generator output from the same type-level markers the plan already targeted.

## Issues Encountered
- The plan's acceptance-criteria example grep (`grep -A3 'ReporterSpawnedUID' <file> | grep -c 'RetryOnConflict'`) returns 0 on all four planner-tier files because the marker-check line and the `RetryOnConflict` call are more than 3 lines apart in the final code shape (the gate `if`/`else` wraps several lines of reporter-spawn logic before reaching the stamp). The underlying requirement — durable stamp idiom present — is unambiguously satisfied: `grep -c 'RetryOnConflict' <file>` returns 6-7 per planner-tier file and 3 for task_controller.go, and the actual `<verify><automated>` command (`go build && go vet && grep -c 'ReporterSpawnedUID'`) is the authoritative gate and passes cleanly. Not a functional gap, just a line-distance mismatch in the illustrative grep.
- Pre-existing `golangci-lint` finding at `internal/controller/span_emission_unit_test.go:897` (modernize/slicescontains), introduced in Phase 46 commit `565daae`, unrelated to this plan's files — logged to `.planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/deferred-items.md` per the scope-boundary rule, not fixed.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The envtest proof that a TTL-GC'd reporter Job is NOT re-created (asserting `*ReporterSpawnedUID` stays stable across a simulated 300s TTL-GC + reconcile) is deferred to plan 47-10 per this plan's scope (build/unit-level only).
- `make test` unit tier is green (`internal/controller` 74.1% coverage, zero FAIL lines); `go build ./...` exits 0.
- All five spawn sites now share one gate+stamp shape — plan 47-10's envtest can exercise any single level and the pattern generalizes.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*
