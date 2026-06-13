---
phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
plan: "04"
subsystem: controller
tags: [go, controller-runtime, envtest, plan-controller, envelope-read, non-fatal, pitfall-1]

requires:
  - phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
    provides: "17-01 PlanReconciler backfill changes — plan_controller.go/plan_controller_test.go already edited; wave 2 dep avoids merge collision"

provides:
  - "Non-fatal envelope-read handling in PlanReconciler.handlePlannerJobCompletion via envReadOK/envReaderPresent sentinels"
  - "Regression spec asserting plan.Status.Phase != Failed on transient EnvReader error"
  - "DEBT-04 (CR-01) closed: Plan is no longer permanently wedged to terminal Failed on a transient PVC read error"

affects: [plan-controller, milestone-controller, phase-controller, envelope-read, pitfall-1, tech-debt]

tech-stack:
  added: []
  patterns:
    - "envReadOK/envReaderPresent sentinels (Pitfall-1 pattern): distinguish nil-reader from transient read error; guard envelope-dependent downstream on envReadOK"
    - "Children-based succession fallback: when envReaderPresent and envReadOK=false, requeue 5s rather than auto-succeeding (mirrors phase_controller.go:617-621)"

key-files:
  created: []
  modified:
    - "internal/controller/plan_controller.go — replaced terminal-Failed read-error branch with non-fatal log+fallthrough; added envReadOK/envReaderPresent sentinels; guarded budget rollup, billing halt, and ValidationState stamp on envReadOK; added children-based requeue fallback"
    - "internal/controller/plan_controller_test.go — added 'PlanReconciler — DEBT-04 envelope-read error is non-fatal (Pitfall-1 parity)' Describe block with TDD RED/GREEN cycle"

key-decisions:
  - "Non-fatal pattern: on read error, log and fall through rather than returning immediately — matches phase/milestone Pitfall-1 pattern exactly"
  - "Fallback: when envReaderPresent and no children observed, requeue 5s (envelope may have ChildCount>0 not yet materialized); do NOT auto-succeed a leaf path on read error"
  - "ValidationState stamp gated on envReadOK: only stamp Validated when the envelope read succeeded — prevents the wave path from advancing on a potentially incomplete envelope"
  - "Budget rollup and billing halt gated on envReadOK: out.Usage/ExitCode/Reason are unreliable when the read failed"

patterns-established:
  - "Three-level Pitfall-1 consistency: milestone, phase, and plan all now use the same envReadOK/envReaderPresent sentinel pattern for non-fatal envelope-read handling"

requirements-completed: [DEBT-04]

duration: 23min
completed: 2026-06-13
---

# Phase 17 Plan 04: DEBT-04 Envelope-Read Non-Fatal Summary

**Non-fatal envelope-read in PlanReconciler.handlePlannerJobCompletion using envReadOK/envReaderPresent sentinels, closing DEBT-04 / CR-01**

## Performance

- **Duration:** ~23 min
- **Started:** 2026-06-13T04:30:00Z
- **Completed:** 2026-06-13T04:53:00Z
- **Tasks:** 1 (TDD: RED + GREEN cycle)
- **Files modified:** 2

## Accomplishments

- Replaced the terminal-Failed branch in `handlePlannerJobCompletion` with the Pitfall-1 non-fatal pattern from `milestone_controller.go:529-545` and `phase_controller.go:462-476`
- Introduced `envReadOK` and `envReaderPresent` sentinels to distinguish nil-reader (unit-test/non-Option-C path) from transient read error
- Guarded budget rollup, billing halt backstop, and `ValidationState=Validated` stamp on `envReadOK`; envelope-independent downstream (reporter spawn, approval gate, boundary push) unchanged
- Added children-based fallback: when `envReaderPresent && envReadOK=false && countChildTasks == 0`, requeue 5s instead of auto-succeeding
- All 25 PlanReconciler tests pass; full controller suite (short) passes

## Task Commits

TDD cycle (both commits in Task 1):

1. **Task 1 RED: add failing DEBT-04 spec** — `9299715` (test)
2. **Task 1 GREEN: make envelope-read non-fatal** — `e3a8026` (feat)

## Files Created/Modified

- `internal/controller/plan_controller.go` — Non-fatal read path with envReadOK/envReaderPresent sentinels; guarded downstream; children-based requeue fallback
- `internal/controller/plan_controller_test.go` — DEBT-04 regression spec (TDD RED/GREEN)

## Decisions Made

- `ValidationState=Validated` stamp gated on `envReadOK`: the wave path should only advance when the envelope confirmed the planner completed cleanly — skipping the stamp on a read error preserves the retry loop.
- Children-based fallback requeues 5s when reader is present but unreadable AND no children yet: mirrors `phase_controller.go:617-621`. When children are already observed, the reporter is in flight and we fall through normally.
- Approval gate runs regardless of `envReadOK` (does not depend on `out`), matching the phase/milestone pattern.

## Deviations from Plan

None — plan executed exactly as written. The implementation mirrors the PATTERNS.md Item 5 template and the exact line ranges cited in the PLAN.md interfaces block.

## Issues Encountered

TDD RED: initial test iteration used `mgrClient.Get` (cached client) to check `Status.Phase` — the cache was stale and returned `""` even with the buggy code. Fixed by checking `plan.Status.Phase` in-memory (the buggy code mutates the struct before patching) plus `Consistently` on `k8sClient.Get` (direct API server). The in-memory check was the reliable RED gate.

## Threat Model Coverage

- **T-17-10 (DoS — transient read wedges Plan to Failed):** MITIGATED. Read error now logs non-fatally and requeues; `Status.Phase="Failed"` and `Reason: EnvelopeReadFailed` are removed from the read-error path.
- **T-17-11 (Tampering — non-fatal masks genuine failure):** ACCEPTED. Succession still gates on the all-children-Succeeded signal (`countChildTasks` + `ValidationState` + wave path); a Plan with no successful children does not advance. Envelope is a status optimization, not the success authority — milestone/phase already rely on this.
- **T-17-12 (Tampering — nil/zero-value `out` downstream):** MITIGATED. All downstream code that consumes `out` fields is guarded on `envReadOK`; the fallback requeue path never reaches the ChildCount-gated succession block.

## Verification

```
grep -n 'EnvelopeReadFailed' internal/controller/plan_controller.go   → 0 hits (OK)
grep -nE 'envReadOK|envReaderPresent' internal/controller/plan_controller.go → 13 hits (≥2 OK)
go test ./internal/controller/... -run PlanReconciler -count=1         → ok (25/25 pass)
```

## Self-Check

### Files verified

- `internal/controller/plan_controller.go` — exists, contains `envReadOK` and `envReaderPresent` sentinels, zero `EnvelopeReadFailed` hits
- `internal/controller/plan_controller_test.go` — exists, contains DEBT-04 Describe block with in-memory + `Consistently` assertions
- `.planning/phases/17-address-tech-debt-plan-label-backfill-gate-hardening/17-04-SUMMARY.md` — this file

### Commits verified

- `9299715` — test(17-04): add failing RED spec for DEBT-04 envelope-read non-fatal
- `e3a8026` — feat(17-04): make plan envelope-read error non-fatal (DEBT-04 / CR-01)

## Self-Check: PASSED

---
*Phase: 17-address-tech-debt-plan-label-backfill-gate-hardening*
*Completed: 2026-06-13*
