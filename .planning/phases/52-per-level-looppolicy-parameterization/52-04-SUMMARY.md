---
phase: 52-per-level-looppolicy-parameterization
plan: 04
subsystem: infra
tags: [go, controller-runtime, kubebuilder, loop-policy, resolver]

# Dependency graph
requires:
  - phase: 52-01
    provides: "LoopPolicy.Level/LoopLevel schema, Plan.Spec.Verification, Project.Spec.Verification (VerificationDefaults), Plan.Status.LoopStatus, Project.Status.LoopStatus"
provides:
  - "ResolveLoopPolicy(project, plan, task, level) — the one resolver, keyed on level, every reconciler will call for gate policy"
  - "ResolveVerificationSpec(project, plan, task, level) — the raw-contract-fields accessor dispatch sites use to build verifier envelopes"
  - "PlannerReconcilerDeps.VerifierImage/Reservations/ReserveEstimateCents wired in cmd/manager/main.go from the same instances TaskReconciler uses"
  - "TestNoDirectVerificationPolicyReads — SC3 static guard (T-52-11) forbidding direct Spec.Verification.MaxIterations/.OnExhaustion reads outside the resolver"
affects: [52-05, 52-06, 52-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single shared precedence-walk helper (resolveAuthoredVerification) backing two exported entry points (ResolveVerificationSpec / ResolveLoopPolicy) — mirrors ResolveProvider's shape"
    - "Level-keyed switch statements (not CRD-kind type switches) for both default parameterization and escalation-policy defaulting"
    - "Comment-stripped source-scanning static guard test (mirrors internal/metrics/wave_label_test.go's registry.go check) with a named, dated TODO exclusion for a not-yet-migrated call site"

key-files:
  created:
    - internal/controller/dispatch_helpers_loop_policy_test.go
  modified:
    - internal/controller/dispatch_helpers.go
    - cmd/manager/main.go

key-decisions:
  - "Authored-contract activation key is GateCommand != \"\" (matches hasVerificationContract's existing idiom) — a Draft/empty VerificationSpec never activates a stage."
  - "phase/milestone/project MaxIterations clamp to 0 unconditionally inside the resolver, even when an authored contract sets it higher — D-07 encoded structurally, not as a per-call-site if."
  - "EscalationPolicy default differs by level (task/plan -> escalate, phase/milestone/project -> requireApproval) only when OnExhaustion is unset; an authored value always wins at every level."
  - "SC3 static guard excludes task_controller.go with a dated TODO(52-06) comment — Phase 51's Task loop still reads Spec.Verification.MaxIterations directly in repairOrHalt; 52-06 migrates it and removes the exclusion."

requirements-completed: [ESC-01]

# Metrics
duration: 11min
completed: 2026-07-20
---

# Phase 52 Plan 04: Resolver + Deps Plumbing Summary

**One `ResolveLoopPolicy`/`ResolveVerificationSpec` resolver pair (keyed on level, not CRD kind) plus `PlannerReconcilerDeps` verifier-dispatch plumbing shared with TaskReconciler's existing pool.**

## Performance

- **Duration:** 11 min
- **Started:** 2026-07-20T06:02:34Z
- **Completed:** 2026-07-20T06:13:48Z
- **Tasks:** 2
- **Files modified:** 3 (1 created, 2 modified)

## Accomplishments
- `ResolveLoopPolicy(project, plan, task, level)` — the phase's centerpiece (SC3): one resolver function producing every level's effective `LoopPolicy`, with level-default `MaxIterations` (plan defaults to 1, phase/milestone/project clamp to 0 unconditionally) and level-default `EscalationPolicy` (task/plan → escalate, phase/milestone/project → requireApproval) layered over the D-01 Task > Plan > Project precedence chain.
- `ResolveVerificationSpec(project, plan, task, level)` — sibling accessor sharing the same precedence walk (`resolveAuthoredVerification`), for dispatch sites that need the raw contract fields (GateCommand/Commands/RequiredArtifacts/Evaluator).
- `PlannerReconcilerDeps` gained `VerifierImage`/`Reservations`/`ReserveEstimateCents`, wired in `cmd/manager/main.go` from the exact same `verifierImage` var and `reservationStore` instance `TaskReconciler` already receives (D-10/ESC-04: one project-wide budget pool).
- `TestNoDirectVerificationPolicyReads` — the SC3 static guard: walks every `internal/controller/*.go` file (excluding tests and the resolver's own home), strips line comments, and fails the build on any direct `Spec.Verification.MaxIterations`/`.OnExhaustion` read outside the resolver. `task_controller.go` is excluded with a dated `TODO(52-06)` — that migration is a later plan's scope.

## Task Commits

Each task was committed atomically:

1. **Task 1: ResolveLoopPolicy + ResolveVerificationSpec + precedence walk** - `9e5b2e67` (feat)
2. **Task 2: SC3 static guard + PlannerReconcilerDeps plumbing** - `e44098ca` (feat)

**Lint fix:** `b9cf9148` (fix — `strings.Cut` modernize violation caught by `make lint`, see Deviations)

**Plan metadata:** SUMMARY commit (this plan's docs commit, made by the orchestrator after merge)

_Note: the SC3 static guard test (`TestNoDirectVerificationPolicyReads`) was written together with `TestResolveLoopPolicy` in the same file/commit (Task 1) rather than split into Task 2's commit as the plan's task boundary implied — see Deviations._

## Files Created/Modified
- `internal/controller/dispatch_helpers.go` - Adds `projectLevelVerificationDefault`, `resolveAuthoredVerification`, `ResolveVerificationSpec`, `ResolveLoopPolicy` next to `ResolveProvider`; adds `VerifierImage`/`Reservations`/`ReserveEstimateCents` to `PlannerReconcilerDeps`
- `internal/controller/dispatch_helpers_loop_policy_test.go` - `TestResolveLoopPolicy` (8 named subtests covering precedence, defaults, the phase/milestone/project clamp, and escalation-policy differentiation) + `TestNoDirectVerificationPolicyReads` (SC3 static guard)
- `cmd/manager/main.go` - Wires `plannerDeps.VerifierImage`/`.Reservations`/`.ReserveEstimateCents` from the existing `verifierImage` and `reservationStore` variables

## Decisions Made
- Authored-contract activation test is `GateCommand != ""` everywhere (task/plan/project-default), matching `hasVerificationContract`'s existing idiom from Phase 51 — kept identical rather than inventing a second activation signal.
- The phase/milestone/project `MaxIterations` clamp lives inside `ResolveLoopPolicy` as an unconditional `case` branch that overwrites any authored value, per D-07's explicit instruction that this is a structural resolver property, not a per-call-site guard.
- `EscalationPolicy` defaulting is level-keyed (not CRD-kind-keyed) and only applies when `OnExhaustion` is unset — an authored value always wins, at every level, matching the plan's behavior case #5.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `make lint` modernize violation in the new static-guard helper**
- **Found during:** Post-Task-2 verification pass (running `make lint` per the plan's `<verification>` block)
- **Issue:** `stripGoLineComments` used `strings.Index(line, "//")` + manual slicing; golangci-lint's `modernize` check flagged it as simplifiable via `strings.Cut`
- **Fix:** Replaced with `strings.Cut(line, "//")`
- **Files modified:** `internal/controller/dispatch_helpers_loop_policy_test.go`
- **Verification:** `make lint` re-run → `0 issues`; `TestResolveLoopPolicy`/`TestNoDirectVerificationPolicyReads` re-run green
- **Committed in:** `b9cf9148`

---

**Total deviations:** 1 auto-fixed (1 lint/bug), plus 1 sequencing note (below)
**Impact on plan:** No scope creep — the lint fix is a mechanical style correction with no behavior change. The task-boundary sequencing note below has zero functional impact (both tests landed, both pass, both are exercised by the plan's verification command either way).

**Sequencing note (not a Rule 1-4 deviation, no user decision needed):** the plan places `TestNoDirectVerificationPolicyReads` under Task 2, but it was written into the same file as Task 1's `TestResolveLoopPolicy` and landed in Task 1's commit (`9e5b2e67`) instead. Both tests are present, both pass, and Task 2's commit (`e44098ca`) still delivers its own scope (the `PlannerReconcilerDeps` fields + `main.go` wiring) cleanly. Noted for traceability only.

## Issues Encountered
None beyond the lint fix above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `ResolveLoopPolicy`/`ResolveVerificationSpec` are ready for the dispatch-site plans (52-05/52-06/52-07) to call — no reconciler needs to switch on CRD kind to pick gate policy.
- `PlannerReconcilerDeps.VerifierImage`/`.Reservations`/`.ReserveEstimateCents` are wired and ready for the plan-check and phase/milestone/project level-verify dispatch sites to consume.
- The SC3 static guard is live with one documented, dated exclusion (`task_controller.go`, TODO(52-06)) — the Task-migration plan must remove that exclusion as part of its acceptance criteria.
- `go build ./...`, `go vet ./...`, and `make lint` are all clean at this plan's HEAD.

---
*Phase: 52-per-level-looppolicy-parameterization*
*Completed: 2026-07-20*

## Self-Check: PASSED

All created/modified files found on disk; all 4 task/lint-fix/summary commit hashes (`9e5b2e67`, `e44098ca`, `b9cf9148`, `482fcc44`) confirmed present in `git log`.
