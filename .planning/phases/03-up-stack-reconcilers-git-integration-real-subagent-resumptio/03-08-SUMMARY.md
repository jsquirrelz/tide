---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 08
subsystem: controllers
tags: [controller-runtime, kubebuilder, k8s-jobs, planner-dispatch, child-crd-materialization, git-push, force-with-lease, bypass-annotation, threat-mitigation, allowlist]

# Dependency graph
requires:
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: "Plan 03-01 (pkg/dispatch.{EnvelopeIn.{Provider,Role,Level}, EnvelopeOut.{ChildCRDs,Git}, ProviderSpec, ChildCRDSpec})"
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: "Plan 03-02 (api/v1alpha1.{SubagentConfig, GitConfig, GitStatus, PhasePushLeaseFailed, PhaseComplete, ConditionAuthoringPlanner, ConditionCloned, ConditionPushLeaseFailed})"
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: "Plan 03-06 (internal/controller/push_helpers.{buildPushJob, buildCloneJob, PushOptions, CloneOptions} + cmd/tide-push binary)"
provides:
  - "internal/controller/dispatch_helpers.{ResolveProvider, BuildPlannerEnvelope, MaterializeChildCRDs} — shared planner-dispatch helpers used by all three up-stack reconcilers"
  - "MilestoneReconciler / PhaseReconciler / PlanReconciler planner-dispatch bodies — each level dispatches a deterministic-named planner Job and materializes child CRDs from EnvelopeOut.ChildCRDs"
  - "T-308 Kind allowlist mitigation — MaterializeChildCRDs rejects any Kind not in {Milestone, Phase, Plan, Task, Wave}"
  - "ProjectReconciler.reconcilePhase3Lifecycle — branch-name init, clone Job dispatch, push Job dispatch at level boundary, push-completion writeback, bypass-annotation recovery"
  - "internal/controller/push_helpers.buildCommitMessage — D-B2 commit-message protocol (4 boundary message shapes locked in)"
affects:
  - "Phase 3 closeout plan (level-boundary detection + push-result envelope parsing)"
  - "Phase 4 (per-level human gates + tide CLI + dashboard)"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-reconciler planner-dispatch body (mirrors Phase 2 D-B1 sole-Job-creator pattern at each up-stack level)"
    - "ChildCRD materialization via Kind allowlist (T-308 mitigation; defense-in-depth gate at the subagent → controller boundary)"
    - "Deterministic Job naming (tide-{level}-{uid}-{attempt} for planner Jobs; tide-push-{project-uid} + tide-clone-{project-uid} for orchestrator Jobs)"
    - "Bypass-annotation recovery pattern (clears halt state + consumes annotation; mirrors Phase 2 D-D4 budget-bypass)"

key-files:
  created:
    - internal/controller/dispatch_helpers.go
    - internal/controller/dispatch_helpers_test.go
    - internal/controller/planner_job_helpers.go
    - internal/controller/plan_planner_test.go
    - internal/controller/project_phase3_test.go
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/milestone_controller_test.go
    - internal/controller/phase_controller.go
    - internal/controller/phase_controller_test.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/push_helpers.go
    - internal/controller/push_helpers_test.go

key-decisions:
  - "Vendor pinned to 'anthropic' in v1.0 (per CONTEXT.md Deferred Ideas — per-vendor selection is a v1.x schema bump, not a v1.0 commitment)"
  - "Planner Job spec kept minimal in 03-08 — the production-ready spec (PVC mount, credproxy sidecar, EnvelopeIn writer init container, signed-token minting) is deferred to a follow-up plan; current plan ships the state-transition shape + grep contract"
  - "Plan boundary fires reconcilePlannerDispatch BEFORE reconcileWaveMaterialization — planner emits Tasks; Phase 2 admission webhook validates; Phase 2 Wave path creates Waves on next reconcile (preserves D-E1 invariant)"
  - "Project-boundary push fires when Status.Phase=Complete (mid-stack boundary detection — Milestone/Phase/Plan-boundary push dispatch — is a follow-up plan that wires child-status watching to push trigger)"
  - "Push failure handler defaults to lease-rejection treatment (Status.Phase=PushLeaseFailed); full reason parsing from cmd/tide-push push-result envelope schema is deferred to a follow-up plan"

patterns-established:
  - "Pattern: shared planner-dispatch helpers — three reconcilers share ~80% of their dispatch code via dispatch_helpers.{ResolveProvider, BuildPlannerEnvelope, MaterializeChildCRDs}; per-reconciler bodies are ~80-100 LOC of level-specific glue"
  - "Pattern: Kind allowlist before server-side create — MaterializeChildCRDs enforces {Milestone, Phase, Plan, Task, Wave} hard-coded allowlist; non-matching Kind returns structured error and parent reconciler patches Status.Phase=Failed"
  - "Pattern: deterministic-name dedup via AlreadyExists — every Job creation (planner, clone, push) treats AlreadyExists as idempotent success (mirrors Phase 2 SUB-03 / Pitfall F watch-lag race handling)"
  - "Pattern: bypass-annotation recovery — operator-applied annotation clears halt state + reconciler consumes annotation + requeues for retry; mirrors Phase 2 D-D4 budget-bypass exactly"

requirements-completed:
  - ART-03
  - ART-04
  - ART-06

# Metrics
duration: 44min
completed: 2026-05-15
---

# Phase 03 Plan 08: Up-Stack Reconciler Planner Dispatch + Project Lifecycle Summary

**Three up-stack reconcilers (Milestone/Phase/Plan) dispatch deterministically-named planner Jobs and materialize child CRDs from EnvelopeOut.ChildCRDs via a shared helper; ProjectReconciler extends with clone Job, push Job at level boundary, branch lifecycle, and bypass-annotation recovery; T-308 Kind allowlist locks the subagent → controller boundary.**

## Performance

- **Duration:** 44 min
- **Started:** 2026-05-16T00:37:06Z
- **Completed:** 2026-05-16T01:21:21Z
- **Tasks:** 4 (Task 1 + Task 2 + Task 3a + Task 3)
- **Files modified:** 13 (5 created, 8 modified)

## Accomplishments

- **dispatch_helpers.go** ships `ResolveProvider` (D-C2 precedence walk), `BuildPlannerEnvelope` (planner-side analog of Phase 2's `buildEnvelopeIn`), and `MaterializeChildCRDs` (server-side-create child CRDs from EnvelopeOut.ChildCRDs with T-308 Kind allowlist). 7 unit tests cover the precedence chain, JSON round-trip, allowlist rejection, and AlreadyExists idempotence.
- **MilestoneReconciler / PhaseReconciler / PlanReconciler** fill in the Phase 3 planner-dispatch body (mirrors Phase 2 D-B1 at each up-stack level). Each reconciler dispatches `tide-{level}-<uid>-<attempt>` (D-B5 dedup), acquires plannerPool before Job creation (D-A4), and on Job terminal state materializes child CRDs.
- **PlanReconciler** runs planner dispatch BEFORE the existing reconcileWaveMaterialization (Phase 2 D-E1 unchanged); on EnvelopeOut, Task CRDs are server-side-created and the admission-webhook → Wave-materialization handoff is preserved.
- **ProjectReconciler.reconcilePhase3Lifecycle** extends with branch-name init (`tide/run-<name>-<unix>` per D-B6, Unix epoch only), clone Job dispatch (`tide-clone-<project-uid>`), push Job dispatch (`tide-push-<project-uid>` per D-B5 serialization), push-completion writeback to Status.Git.LastPushedSHA, and bypass-annotation recovery (`tideproject.k8s/bypass-push-lease=true`).
- **push_helpers.buildCommitMessage** locks in the four D-B2 / W11 boundary commit-message shapes verbatim: `"tide: plan <name> authored + executed"` (only one with `+ executed` suffix), `"tide: phase <name> authored"`, `"tide: milestone <name> authored"`, `"tide: project complete"`.

## Task Commits

Each task was committed atomically (TDD: RED → GREEN for Tasks 1 and 2):

1. **Task 1 RED:** `09e8d3b` — `test(03-08): add failing tests for dispatch_helpers (RED)`
2. **Task 1 GREEN:** `e629631` — `feat(03-08): implement dispatch_helpers (Task 1, GREEN)`
3. **Task 2 RED:** `db0faca` — `test(03-08): add failing tests for up-stack reconciler dispatch (RED)`
4. **Task 2 GREEN:** `fd40a7f` — `feat(03-08): up-stack reconciler bodies — planner dispatch + child materialization (Task 2, GREEN)`
5. **Task 3a:** `8594918` — `feat(03-08): push_helpers.buildCommitMessage — D-B2 boundary commit messages (Task 3a)`
6. **Task 3:** `a0637fd` — `feat(03-08): ProjectReconciler — clone Job + push Job + branch lifecycle + bypass annotation (Task 3)`

## Files Created/Modified

**Created:**
- `internal/controller/dispatch_helpers.go` — Shared planner-dispatch helpers (ResolveProvider + BuildPlannerEnvelope + MaterializeChildCRDs with T-308 Kind allowlist)
- `internal/controller/dispatch_helpers_test.go` — 7 unit tests
- `internal/controller/planner_job_helpers.go` — Shared minimal PodTemplateSpec for planner Jobs
- `internal/controller/plan_planner_test.go` — Plan-level planner dispatch envtest specs
- `internal/controller/project_phase3_test.go` — Project Phase 3 lifecycle envtest specs

**Modified:**
- `internal/controller/milestone_controller.go` — Phase 3 planner-dispatch body + handleJobCompletion + child Phase materialization
- `internal/controller/milestone_controller_test.go` — 3 envtest specs (dispatch, child materialization, Kind allowlist rejection)
- `internal/controller/phase_controller.go` — Phase 3 planner-dispatch body + child Plan materialization
- `internal/controller/phase_controller_test.go` — 1 envtest spec
- `internal/controller/plan_controller.go` — reconcilePlannerDispatch runs BEFORE reconcileWaveMaterialization (Phase 2 D-E1 preserved)
- `internal/controller/project_controller.go` — reconcilePhase3Lifecycle (branch init, clone Job, push Job, bypass)
- `internal/controller/push_helpers.go` — buildCommitMessage (4 D-B2 boundary message shapes)
- `internal/controller/push_helpers_test.go` — 4 buildCommitMessage tests + ArtifactPaths CSV test

## Decisions Made

- **Vendor pinned to `"anthropic"` in v1.0:** Per CONTEXT.md "Deferred Ideas", per-vendor selection (Codex, Gemini, OpenCode, Grok, etc.) is a v1.x schema-bump scope item; v1.0 ships single-vendor-per-Project. ResolveProvider returns Vendor="anthropic" unconditionally.
- **Planner Job spec kept minimal:** The production-ready spec (PVC mount, credproxy sidecar, EnvelopeIn writer init container, signed-token minting) is deferred to a follow-up plan once cmd/manager wires HelmProviderDefaults end-to-end. Current plan ships the state-transition shape + grep contract.
- **Plan boundary dispatches planner Job BEFORE Wave materialization:** PlanReconciler.Reconcile calls reconcilePlannerDispatch first; only when no Tasks exist does it dispatch a planner Job. On EnvelopeOut completion, Task CRDs are created; subsequent reconciles run through the existing reconcileWaveMaterialization path which waits for admission-webhook stamping of ValidationState=Validated.
- **Project-boundary push fires when Status.Phase=Complete:** Mid-stack boundary detection (Milestone/Phase/Plan-boundary push dispatch via child-status watching) is a follow-up plan; current plan wires the buildPushJob shape at the Project terminal boundary.
- **Push failure handler defaults to lease-rejection treatment:** Status.Phase=PushLeaseFailed is set on any push-Job failure. Full reason parsing from the cmd/tide-push push-result envelope schema is deferred to a follow-up plan that aligns the controller-side reader with the binary-side writer.

## Deviations from Plan

### Out-of-Scope Discovery (Logged to deferred-items.md)

**1. [Pre-existing] 24-test controller-suite breakage from plan 03-02's GitConfig CRD validation**
- **Found during:** Task 2 envtest verification
- **Issue:** Plan 03-02 added `GitConfig.RepoURL` as a CRD-level `+kubebuilder:validation:Pattern=^https?://.+` + required when GitConfig present. Pre-existing test fixtures (`makeProjectForTask` in task_controller_test.go and 6+ ProjectReconciler test BeforeEach blocks) create Projects without setting `Git` in Go, but the empty-struct round-trip through envtest's API server triggers the pattern validation.
- **Scope:** Pre-existing breakage. NOT caused by plan 03-08's work. Confirmed by running the controller suite on the base commit (e9c01737) — same 24 failures.
- **Action:** Logged to `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/deferred-items.md` with fix-shape (3-line fixture update per affected test). NOT auto-fixed per scope boundary ("Only auto-fix issues DIRECTLY caused by the current task's changes").
- **Plan 03-08 fixtures use correctly-formed Git config blocks** so the new tests pass; suite progression went from 32→36 passing (4 new specs added).

### Auto-fixed Issues

**1. [Rule 3 - Blocking] envtest binaries missing in worktree filesystem**
- **Found during:** Task 2 verification — `go test` failed BeforeSuite with `open ../../bin/k8s: no such file or directory`.
- **Issue:** Fresh worktrees don't inherit the main repo's `bin/k8s/<version>/{etcd,kube-apiserver,kubectl}` envtest binaries.
- **Fix:** `ln -s /Users/justinsearles/Projects/tide/bin <worktree>/bin` — symlinks the main repo's bin dir so envtest finds the binaries.
- **Files modified:** None tracked in git (bin/ is gitignored).
- **Verification:** `ls bin/k8s/1.36.0-darwin-amd64/` returns expected binaries.
- **Note:** Already logged in deferred-items.md by plan 03-01 with a follow-up fix-shape (make envtest in worktree bootstrap recipe).

**2. [Rule 3 - Blocking] envtest cache lag between k8sClient.Create and mgrClient.Get**
- **Found during:** Task 2 envtest — the reconciler used `mgrClient` (cached client) but tests wrote via `k8sClient` (direct client), causing the reconciler to not see Project/Milestone parents.
- **Fix:** Added `waitForCacheSync` calls in BeforeEach after each `k8sClient.Create` (mirrors the pattern from Phase 2's `makeProjectForTask` in task_controller_test.go).
- **Files modified:** internal/controller/milestone_controller_test.go, internal/controller/phase_controller_test.go
- **Verification:** All 6 Phase 3 specs pass when run together.

**3. [Rule 3 - Blocking] envtest Job status validation requires SuccessCriteriaMet + startTime**
- **Found during:** Task 2 envtest — patching Job.Status.Conditions to `JobComplete=True` failed with "cannot set Complete=True without SuccessCriteriaMet=true" and "status.startTime is required for finished jobs".
- **Fix:** Updated `makeFakeJobTerminal` test helper to set both `SuccessCriteriaMet` + `JobComplete` conditions AND `status.startTime` + `status.completionTime`.
- **Files modified:** internal/controller/milestone_controller_test.go (the helper is exported to phase/plan tests via same-package access).
- **Verification:** All Phase 3 specs pass.

---

**Total deviations:** 3 Rule-3 blocking fixes + 1 out-of-scope discovery logged to deferred-items.md
**Impact on plan:** All auto-fixes were envtest-environment issues, not code-correctness issues. No production code changed beyond plan scope.

## Issues Encountered

- **Repeated worktree-cwd-drift during reconciler-file rewrites:** Initial implementation of milestone_controller.go succeeded, but a subsequent `git checkout` for verification stashed my uncommitted changes and they were lost. Recovered by re-implementing from the plan's <action> blocks (verified at time the second write was committed). Plan completion shape was preserved.
- **CRD validation pattern divergence (out of scope):** See "Out-of-Scope Discovery" above.

## User Setup Required

None - no external service configuration required. Plan 03-08 is controller-internal; user-facing wiring (HelmProviderDefaults injection from values.yaml, push image config in chart) lands in a follow-up plan.

## Next Phase Readiness

**Ready:**
- All four up-stack reconciler bodies dispatch planner Jobs at the correct level with correct deterministic names.
- T-308 Kind allowlist locks the subagent → controller boundary at MaterializeChildCRDs.
- D-B2 commit-message protocol is locked in via buildCommitMessage.
- D-B5 push-Job serialization shape (deterministic name + AlreadyExists idempotent) is wired through ProjectReconciler.
- D-B6 branch-name format (Unix epoch only) is enforced — `make verify-no-aggregates` clean, POOL-03 analyzer clean.

**Blockers / Concerns:**
- The pre-existing 24-test controller-suite breakage (logged in deferred-items.md) gates "Phase 3 considered green" — a follow-up plan needs to update the legacy fixture-set.
- Mid-stack boundary push dispatch (Milestone-boundary push, Phase-boundary push, Plan-boundary push at all-Tasks-Succeeded) is wired only at the Project terminal boundary in plan 03-08; a follow-up plan needs to add child-status watching to trigger push at the intermediate boundaries.
- The push-result envelope schema (cmd/tide-push writes HeadSHA + reason; ProjectReconciler reads them) needs alignment between the binary-side writer and the controller-side reader.

## Threat Flags

None — no new attack surface beyond what plan 03-08's `<threat_model>` already covered. T-308 (Tampering — childCRDs Spec injection) is mitigated via the hard-coded Kind allowlist in `MaterializeChildCRDs`. T-309 (Pool deadlock) is preserved via POOL-03 single-pool-per-reconciler. T-303 (Stale lease) is preserved via PhasePushLeaseFailed + bypass annotation. T-302 (Push race) is preserved via deterministic name + AlreadyExists.

## Self-Check: PASSED

All commits verified to exist in git log:
- `09e8d3b` test(03-08): add failing tests for dispatch_helpers (RED) — FOUND
- `e629631` feat(03-08): implement dispatch_helpers (Task 1, GREEN) — FOUND
- `db0faca` test(03-08): add failing tests for up-stack reconciler dispatch (RED) — FOUND
- `fd40a7f` feat(03-08): up-stack reconciler bodies (Task 2, GREEN) — FOUND
- `8594918` feat(03-08): push_helpers.buildCommitMessage (Task 3a) — FOUND
- `a0637fd` feat(03-08): ProjectReconciler clone+push+bypass (Task 3) — FOUND

All created files exist:
- `internal/controller/dispatch_helpers.go` — FOUND
- `internal/controller/dispatch_helpers_test.go` — FOUND
- `internal/controller/planner_job_helpers.go` — FOUND
- `internal/controller/plan_planner_test.go` — FOUND
- `internal/controller/project_phase3_test.go` — FOUND

All grep gates pass (verified at end of each task). `go build ./...` clean. `go vet ./internal/controller/...` clean. `make verify-no-aggregates` clean. `make tide-lint` clean. All 8 new Phase 3 envtest specs pass together.

---
*Phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio*
*Completed: 2026-05-15*
