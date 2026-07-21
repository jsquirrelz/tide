---
phase: 49-common-loop-contract-verdict-envelope-persistence-schema
plan: 01
subsystem: api
tags: [kubebuilder, controller-runtime, crd-schema, deepcopy, go]

# Dependency graph
requires:
  - phase: 40-crank-04-schema-migration
    provides: api/v1alpha3 as the sole served+storage CRD version
provides:
  - LoopPolicy / LoopStatus / EvaluationSummary standalone, deepcopy-generated Go types in api/v1alpha3
  - AutonomyLevel / EscalationPolicy / ExitReason enum types with kubebuilder Enum validation
  - Compile-time structural guard (TestLoopStatus_NoForbiddenFields) pinning LOOP-03 (no iteration history in .status)
  - Synthetic-embedder proof (TestLoopContract_Embeddable) that the types are embeddable in any domain CRD Spec/Status
affects: [50-execution-loop-halt-condition, 51-task-loop-dispatch-verifier]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Compile-time forbidden-field struct literal guard (mirrors pkg/dispatch's TestTerminationStub_NoForbiddenFields) applied to a CRD status type for the first time"
    - "metav1.Time round-trip test construction uses .Local() up front to match k8s.io/apimachinery's UnmarshalJSON normalization, avoiding a reflect.DeepEqual false-negative"

key-files:
  created:
    - api/v1alpha3/loop_types.go
    - api/v1alpha3/loop_types_test.go
  modified:
    - api/v1alpha3/zz_generated.deepcopy.go

key-decisions:
  - "LoopPolicy/LoopStatus/EvaluationSummary declared standalone (no +kubebuilder:object:root) and embedded in no Kind this phase — make manifests confirmed zero CRD-YAML diff"
  - "EvaluationSummary.Decision is a plain string (not the imported pkg/dispatch.GateDecision type), per D-01's two-homes decoupling precedent mirroring Caps/pkg/dispatch.Caps"
  - "ExitReason vocabulary (approved/iterationsExhausted/durationExhausted/budgetExhausted/escalated) is a new, intentionally small set distinct from Phase 50's Execution-loop terminal reasons, per RESEARCH Assumption A2 (Claude's Discretion)"

patterns-established:
  - "Pattern: shared CRD-embeddable loop contract types live in api/v1alpha3/loop_types.go, get DeepCopy for free via the package-level +kubebuilder:object:generate=true marker, and require zero per-type markers"
  - "Pattern: LOOP-03-style history-prevention is enforced by a compile-time struct-literal test naming every field, not merely a runtime size/shape assertion"

requirements-completed: [LOOP-01, LOOP-02, LOOP-03]

# Metrics
duration: 7min
completed: 2026-07-18
---

# Phase 49 Plan 01: Common Loop Contract Types Summary

**LoopPolicy/LoopStatus/EvaluationSummary shared, deepcopy-generated `api/v1alpha3` types with five-element-loop doc-comments and a compile-time guard against iteration-history creep.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-07-18T21:45:46Z (approx, first plan-related commit)
- **Completed:** 2026-07-18T21:52:06Z
- **Tasks:** 2
- **Files modified:** 3 (2 created, 1 regenerated)

## Accomplishments
- Authored `LoopPolicy` (repeat policy: MaxIterations/MaxDuration/BudgetCents/Autonomy/EvaluatorRef/EscalationPolicy) and `LoopStatus` (observed state: Iteration/ParentRunID/LastEvaluation/ExitReason/CostCents/Conditions) as standalone `api/v1alpha3` types, each carrying a type-level godoc naming all five loop elements (goal/spec, mutable candidate, evaluator/environment feedback, repeat policy, bounded exit/escalation) per LOOP-02
- Authored `EvaluationSummary`, the bounded current-iteration verdict projection embedded at `LoopStatus.LastEvaluation`, with a "Design note" doc-comment mirroring the `Caps`/`pkg/dispatch.Caps` two-homes decoupling precedent
- Regenerated `zz_generated.deepcopy.go` via `make generate` (zero manual edits) and confirmed `make manifests` produces zero CRD-YAML diff — the types are standalone and embedded in no Kind this phase
- Wrote `loop_types_test.go`: JSON round-trip tests for both types, a compile-time `TestLoopStatus_NoForbiddenFields` structural guard pinning LOOP-03, and a synthetic-embedder `TestLoopContract_Embeddable` test proving the types round-trip and deep-copy correctly when embedded in a Spec/Status shape

## Task Commits

Each task was committed atomically:

1. **Task 1: Author loop_types.go (LoopPolicy, LoopStatus, EvaluationSummary) + regenerate deepcopy** - `7366d70` (feat)
2. **Task 2: loop_types_test.go — round-trip, LOOP-03 structural guard, synthetic-embedder proof** - `a73d772` (test)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `api/v1alpha3/loop_types.go` - `LoopPolicy`, `LoopStatus`, `EvaluationSummary` structs + `AutonomyLevel`/`EscalationPolicy`/`ExitReason` enum types
- `api/v1alpha3/loop_types_test.go` - round-trip tests, LOOP-03 compile-time guard, embeddability proof
- `api/v1alpha3/zz_generated.deepcopy.go` - `make generate`-regenerated `DeepCopy`/`DeepCopyInto` for the three new structs

## Decisions Made
- Followed CONTEXT.md D-01/D-06 and PATTERNS.md field-type table exactly: `int64` for cents, `*metav1.Duration` for bounded duration, `int32` for iteration counters, plain `string` for `EvaluatorRef` (never `corev1.LocalObjectReference`)
- `ExitReason`'s concrete enum values (`approved`/`iterationsExhausted`/`durationExhausted`/`budgetExhausted`/`escalated`) were left to Claude's Discretion per RESEARCH Assumption A2; chose a value set that names each repeat-policy dimension (iterations/duration/budget) plus the two non-exhaustion outcomes (approved, escalated), deliberately distinct from Phase 50's separate Execution-loop terminal-reason vocabulary

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] metav1.Time round-trip test false-negative under reflect.DeepEqual**
- **Found during:** Task 2 (writing `TestLoopStatus_JSONRoundTrip` / `TestLoopContract_Embeddable`)
- **Issue:** `metav1.Time.UnmarshalJSON` normalizes to `.Local()` (k8s.io/apimachinery convention). Constructing the expected `EvaluationSummary.CompletedAt` with `time.Date(..., time.UTC)` produced a `time.Time` whose `Location` differed from the post-round-trip value even though both represented the identical instant, causing `reflect.DeepEqual` to report a spurious mismatch.
- **Fix:** Construct the test fixture's `CompletedAt` with `.Local()` applied up front (`time.Date(...).Local()`), matching the representation `UnmarshalJSON` produces, so the pre- and post-round-trip values are `reflect.DeepEqual`-equal by construction.
- **Files modified:** `api/v1alpha3/loop_types_test.go`
- **Verification:** `go test ./api/v1alpha3/... -run 'TestLoop' -count=1 -v` — all 4 tests pass
- **Committed in:** `a73d772` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug fix, test-only, no production code affected)
**Impact on plan:** Test-construction fix only; no scope creep, no change to the locked type shapes.

## Issues Encountered
None beyond the deviation documented above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `LoopPolicy`/`LoopStatus`/`EvaluationSummary` are locked, deepcopy-generated, and proven embeddable — Phase 50 (halt-condition/reconciler logic) and Phase 51 (Task loop dispatch, `TaskSpec`/`TaskStatus` embedding) can build directly on this contract without further schema churn.
- `make manifests` confirmed zero CRD-YAML diff — no premature CRD embedding occurred, matching the plan's scope discipline (types are not embedded into `TaskSpec`/`TaskStatus` this phase; that is Phase 51's TASK-01).
- Plan 49-02 (verdict/envelope schema in `pkg/dispatch`) and 49-03/49-04 are independent of this plan's specific field choices beyond the `EvaluationSummary` shape already locked here.

---
*Phase: 49-common-loop-contract-verdict-envelope-persistence-schema*
*Completed: 2026-07-18*

## Self-Check: PASSED

- FOUND: api/v1alpha3/loop_types.go
- FOUND: api/v1alpha3/loop_types_test.go
- FOUND: api/v1alpha3/zz_generated.deepcopy.go
- FOUND: .planning/phases/49-common-loop-contract-verdict-envelope-persistence-schema/49-01-SUMMARY.md
- FOUND commit: 7366d70 (Task 1)
- FOUND commit: a73d772 (Task 2)
- FOUND commit: 368d1d6 (SUMMARY.md)
