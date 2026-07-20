---
phase: 52-per-level-looppolicy-parameterization
plan: 08
subsystem: infra
tags: [kubernetes, controller-runtime, verification-loop, gate-policy, level-verify, otel]

# Dependency graph
requires:
  - phase: 52-per-level-looppolicy-parameterization (plan 02)
    provides: LoopPolicy.Level field + LoopLevel enum, LoopStatus embedded on PhaseStatus/MilestoneStatus/ProjectStatus (api/v1alpha3)
  - phase: 52-per-level-looppolicy-parameterization (plan 03)
    provides: phase_verifier.tmpl/milestone_verifier.tmpl/project_verifier.tmpl + LoadPromptTemplate(role, level) convention
  - phase: 52-per-level-looppolicy-parameterization (plan 04)
    provides: ResolveLoopPolicy / ResolveVerificationSpec resolver (dispatch_helpers.go)
  - phase: 52-per-level-looppolicy-parameterization (plan 05)
    provides: AddReadOnlyWorktree + jobspec.go's WorktreeCheckoutImage/WorktreeBranch/buildWorktreeCheckoutContainer
  - phase: 52-per-level-looppolicy-parameterization (plan 06)
    provides: shared exhaustVerifyLoop D-08 branch point + VerifierJobName(level, parentUID, attempt) generalization
provides:
  - internal/controller/level_verify.go — ONE shared, level-parameterized dispatch/consume/terminal-routing unit (maybeRunLevelVerify, dispatchLevelVerifier, handleLevelVerifierCompletion, exhaustLevelVerify) serving Phase, Milestone, and Project
  - levelVerifyTarget struct — the level-specific accessor shape the three future controller call sites (52-10) build from their own Status fields
  - levelVerifierRenderData — the real dispatch-time render-data contract (EnvelopeIn embed + LevelGoal) phase/milestone/project_verifier.tmpl consume
  - EnvelopeIn.Level doc comment corrected to list all five levels including project
affects: [52-10, phase-53]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "levelVerifyDecision: a pure, I/O-free guard function extracted from maybeRunLevelVerify (inactive/converged/needsDispatch/alreadyVerifying) so the four-way branch is directly unit-testable without a fake client.Client"
    - "levelVerifyTarget: a small accessor struct (Obj/Conditions/PhasePtr/LoopStatus/Level/Goal/ParentSpanID) each of Phase/Milestone/Project's future call sites builds from their own Status fields — the ONLY per-controller code 52-10 will need to write is populating this struct and calling maybeRunLevelVerify"

key-files:
  created:
    - internal/controller/level_verify.go
    - internal/controller/level_verify_unit_test.go
  modified:
    - pkg/dispatch/envelope.go

key-decisions:
  - "attempt is hardcoded to 1 throughout (VerifierJobName, LoopRunID/AttemptID, applyLevelLoopStatus's Iteration) — D-07's MaxIterations:0 clamp means these levels never mint a second quality attempt; Job backoff handles pod-level retries, not this dispatch layer"
  - "exhaustLevelVerify's exit reason is hardcoded to ExitEscalated (not threaded as a parameter) — unlike Task's haltVerify (which also reaches ExitIterationsExhausted via repairOrHalt), every one of exhaustLevelVerify's 6 call sites passes the same value since there is no iteration to exhaust at these levels; golangci-lint's unparam check confirmed this empirically"
  - "PVCName resolves to the package-level defaultSharedPVCName constant directly, not a per-reconciler SharedPVCName override field — maybeRunLevelVerify's declared signature (ctx, c, scheme, deps PlannerReconcilerDeps, project, target) carries no such field and PlannerReconcilerDeps doesn't expose one; in production every reconciler's sharedPVCName() falls back to the same default anyway"
  - "Added a ParentSpanID field to levelVerifyTarget (not explicitly named in the plan's field list) so the EVALUATOR span can be a genuine sibling of the level's own AGENT span (mirrors emitEvaluatorSpanForVerifier's Task-level parent-span resolution) — zero value degrades gracefully via synthesizeEvaluatorSpan's own nil-safety, and 52-10 populates it from each reconciler's own already-resolved AGENT-span parent"
  - "Added a scheme *runtime.Scheme parameter to maybeRunLevelVerify/dispatchLevelVerifier — owner.EnsureOwnerRef requires it and PlannerReconcilerDeps carries no Scheme field; not explicitly listed in the plan's prose signature but structurally required"
  - "buildLevelVerifierEnvelopeIn renders against levelVerifierRenderData (EnvelopeIn embed + LevelGoal), the real dispatch-time equivalent of 52-03's test-only levelVerifierFixture — per Task 1's explicit instruction that 52-07/52-08/52-09 supply these exact structs"

patterns-established:
  - "Pattern: a shared level-verify machine parameterized by a small target-accessor struct, called identically by every future controller call site — no per-controller fork, mirrors the depgraph.go shared-resolver precedent this file's own doc header cites"

requirements-completed: [ESC-01]

# Metrics
duration: 19min
completed: 2026-07-20
---

# Phase 52 Plan 08: Level-Verify Shared Machinery (Phase/Milestone/Project) Summary

**One shared, level-parameterized dispatch/consume/terminal-routing unit (`internal/controller/level_verify.go`) now exists for the Phase/Milestone/Project level-verify loop — zero repair branches, zero level-specific if-statements, every non-APPROVED terminal routing through the single `exhaustVerifyLoop` D-08 branch point — ready for 52-10 to wire onto the three controllers' pre-Succeeded seams.**

## Performance

- **Duration:** 19 min
- **Started:** 2026-07-20T03:41:13-04:00 (first commit)
- **Completed:** 2026-07-20T04:00:01-04:00 (last commit)
- **Tasks:** 2 completed (+ 1 lint-driven follow-up commit)
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments

- Built `maybeRunLevelVerify` — the single entry point the pre-Succeeded seams will call — with the two off-ramps (not-active, and the post-approval `ExitReason` convergence guard, T-52-25) plus the full NotFound→dispatch / terminal→consume / running→requeue three-way, mirroring Task's `checkVerifyingState` exactly.
- `dispatchLevelVerifier` provisions the level-verify Job with every D-10 rail (cap-before-reserve, `AlreadyExists`-as-success) AND the 52-05 worktree-checkout wiring (`WorktreeCheckoutImage`/`WorktreeBranch`) plus `EnvelopeIn.Branch` at the run-branch tip — the piece the Task verifier never needed.
- `handleLevelVerifierCompletion` + `exhaustLevelVerify` consume the verdict fail-closed (unreadable envelope, nil verdict, and BLOCKED all escalate; a deterministic gate-command Finding dominates even an APPROVED verdict) and route every non-APPROVED terminal through the shared `exhaustVerifyLoop`.
- Extracted `levelVerifyDecision` as a pure, directly-testable guard and pinned it with a 7-subtest decision table (inactive × 2, converged × 2, engaging × 3).
- Fixed the stale `EnvelopeIn.Level` doc comment (RESEARCH anti-pattern) — now lists all five levels including `project`.
- `golangci-lint` surfaced two genuine issues (an `unparam` on a hardcoded exit reason, six `unused` functions with no live caller yet) — fixed at the root: removed the dead parameter, and added three end-to-end fake-client tests that exercise the full dispatch/consume machinery, mirroring the codebase's own 51-03 precedent for standalone-emitter code built ahead of its wiring.

## Task Commits

1. **Task 1: level_verify.go — shared dispatch/consume machinery** - `40b70a1a` (feat)
2. **Task 2: Unit tests for the pure parts + EnvelopeIn.Level doc fix** - `7c01f68b` (test)
3. **Lint-driven follow-up: resolve unused/unparam findings** - `7efa7646` (fix)

_No plan-metadata commit yet — orchestrator commits STATE.md/ROADMAP.md centrally after merge (worktree mode)._

## Files Created/Modified

- `internal/controller/level_verify.go` - the shared level-verify machine: `levelVerifyTarget`, `levelVerifierRenderData`, `levelVerifyDecision`, `maybeRunLevelVerify`, `dispatchLevelVerifier`, `buildLevelVerifierEnvelopeIn`, `handleLevelVerifierCompletion`, `exhaustLevelVerify`, `applyLevelLoopStatus`, `settleLevelVerifierSpend`, `emitLevelEvaluatorSpan`, `synthesizeNoLevelVerifyEnvelopeOut`
- `internal/controller/level_verify_unit_test.go` - decision-table + envelope-shape + end-to-end fake-client tests (10 top-level `TestLevelVerify*` functions)
- `pkg/dispatch/envelope.go` - `EnvelopeIn.Level` doc comment corrected to list `project`

## Decisions Made

See `key-decisions` in frontmatter — the six load-bearing calls (attempt fixed at 1, hardcoded `ExitEscalated`, `defaultSharedPVCName` direct use, added `ParentSpanID`/`scheme` params, and the real `levelVerifierRenderData` type) are documented there with rationale.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking issue] `golangci-lint`'s `unparam` flagged `exhaustLevelVerify`'s `exitReason` parameter as constant across every call site**
- **Found during:** Task 2 (post-Task-1 lint pass, part of the plan's own `<verification>` block: "`make lint`... all green")
- **Issue:** All 6 `exhaustLevelVerify` call sites in `handleLevelVerifierCompletion` passed `tideprojectv1alpha3.ExitEscalated` — D-07's `MaxIterations:0` clamp means these levels never reach `ExitIterationsExhausted` (there is no iteration to exhaust), so the parameter never varied.
- **Fix:** Removed the parameter; `exhaustLevelVerify` now declares `const exitReason = tideprojectv1alpha3.ExitEscalated` internally and documents why in its doc comment.
- **Files modified:** `internal/controller/level_verify.go`
- **Verification:** `./bin/golangci-lint run ./internal/controller/... ./pkg/dispatch/...` — 0 issues after the fix.
- **Committed in:** `7efa7646`

**2. [Rule 3 - Blocking issue] `golangci-lint`'s `unused` flagged 6 functions (`maybeRunLevelVerify`, `dispatchLevelVerifier`, `handleLevelVerifierCompletion`, `exhaustLevelVerify`, `settleLevelVerifierSpend`, `emitLevelEvaluatorSpan`) with no live caller anywhere in the repo**
- **Found during:** Task 2 lint pass
- **Issue:** This plan's own objective states 52-10 wires the three controllers' call sites (a later, disjoint plan) — so at this plan's HEAD these functions are genuinely orphaned in the whole-program view `unused` analyzes.
- **Fix:** Added three end-to-end tests using `sigs.k8s.io/controller-runtime/pkg/client/fake` that call `maybeRunLevelVerify` directly and exercise all four branches (needs-dispatch + no-VerifierImage skip, already-verifying + unreadable-envelope escalate leg through `exhaustLevelVerify`/`settleLevelVerifierSpend`/`emitLevelEvaluatorSpan`, and the ESC-04 cap-hit requeue) — mirrors the established codebase precedent (`72e5cfb1`, Phase 51-03's `synthesizeEvaluatorSpan`, which resolved the identical "standalone emitter, no live call site yet" situation the same way). This also caught that `maybeRunLevelVerify`'s own `res ctrl.Result` return value was itself flagged `unparam` once the parameter fix landed — fixed by asserting on `res.RequeueAfter` in the cap-hit test.
- **Files modified:** `internal/controller/level_verify_unit_test.go`
- **Verification:** `./bin/golangci-lint run ./internal/controller/... ./pkg/dispatch/...` — 0 issues; `go test ./internal/controller/... -run 'TestLevelVerify' -count=1 -v` — 10/10 pass.
- **Committed in:** `7efa7646`

## Acceptance Criteria Verification

- `grep -c "switch.*(type)" internal/controller/level_verify.go` = 0 ✓
- `grep -n "exhaustVerifyLoop" internal/controller/level_verify.go` ≥ 1 ✓ (5 references) and `grep -n "dispatchRepair\|repairOrHalt" internal/controller/level_verify.go` = 0 ✓
- `grep -n "Branch" internal/controller/level_verify.go` shows `EnvelopeIn.Branch` and `WorktreeBranch` both populated from the project run-branch field ✓
- `grep -n "ExitReason" internal/controller/level_verify.go` shows the convergence guard in `maybeRunLevelVerify` ✓
- `TestLevelVerify*` unit tests: 10 top-level functions, ≥ 4 named subtests covering inactive/converged/engaging/envelope-shape ✓
- `grep -n "\"project\"" pkg/dispatch/envelope.go` shows the `Level` doc comment now includes `project` ✓
- `go test ./pkg/dispatch/...` green (golden fixtures untouched — comment-only change) ✓
- `go build ./...`, `go vet ./...`, `./bin/golangci-lint run ./internal/controller/... ./pkg/dispatch/...` all clean ✓

## Known Environment Notes

`go build ./...` in a fresh worktree initially fails on `cmd/tide-demo-init`'s `//go:embed all:fixture` (a generated, gitignored directory) — this is a documented, pre-existing worktree-environment fact, not caused by this plan's changes. Ran `go generate ./cmd/tide-demo-init/...` once at the start of this session to unblock repo-wide `go build ./...`/`go vet ./...` runs; it produced no tracked or untracked diffs.

## Known Stubs

None — this plan builds shared machinery with no production call sites yet (by design; 52-10 wires the three controllers). No hardcoded empty values reach any UI/user-facing surface; the "orphaned function" state is purely a build-graph fact, addressed above via direct test coverage, not a functional stub.

## Threat Flags

None beyond what the plan's own `<threat_model>` already registers (T-52-23..26) — this plan implements exactly those mitigations (fail-closed classification, the `ExitReason` convergence guard against re-dispatch DoS, and the `EVALUATOR` span for provenance) with no new surface introduced beyond what was declared.
