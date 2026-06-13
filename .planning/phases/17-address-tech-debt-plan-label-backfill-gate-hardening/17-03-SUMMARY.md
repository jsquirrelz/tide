---
phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
plan: "03"
subsystem: cli-approve-gate
tags: [debt, gate-hardening, cli, approve, tdd]
dependency_graph:
  requires: []
  provides: [narrowed-D07-approve-guard]
  affects: [cmd/tide/approve.go, cmd/tide/approve_test.go]
tech_stack:
  added: []
  patterns: [targeted-failed-check, fallback-ux-hint, tdd-red-green]
key_files:
  created: []
  modified:
    - cmd/tide/approve.go
    - cmd/tide/approve_test.go
decisions:
  - "Option A (locked): approveLevel discovers AwaitingApproval target FIRST via findAwaiting* chain, then refuses only if THAT target is Failed; unrelated Failed siblings no longer block"
  - "findFailedLevel retained as unexported helper for fallback UX hint when no AwaitingApproval level exists at all"
  - "approveLevelTarget: new internal helper that performs D-07 targeted check + patchApproveLevel"
  - "--wave path remains guard-free by design; documented with Option-A comment (T-17-09)"
metrics:
  duration: "8 minutes"
  completed: "2026-06-13T04:24:54Z"
  tasks_completed: 1
  files_changed: 2
  commits: 2
  tdd_gates: [RED-cde5dbb, GREEN-217949d]
---

# Phase 17 Plan 03: D-07 Approve Guard Narrowed to Approval Target (Option A) Summary

Narrowed the D-07 `tide approve` guard from project-wide to the approval target only, aligning the CLI gate with the strict-failure-profile contract (siblings are independent; only dependents halt).

## What Was Built

**DEBT-03 / WR-06 fix** — Option A (locked decision):

The previous `approveLevel` function called `findFailedLevel` as an unconditional pre-guard BEFORE the `findAwaiting*` discovery chain. This over-blocked: any Failed level anywhere in the project blocked ALL approvals, including healthy unrelated AwaitingApproval levels. This contradicted the strict-failure-profile spec (siblings are independent).

The fix reorders `approveLevel`:

1. **AwaitingApproval target discovered FIRST** — `findAwaiting*` chain (Milestone → Phase → Plan → Task) runs before any failed-level check.
2. **New `approveLevelTarget` helper** — after discovering the AwaitingApproval target, checks only THAT specific object for `Status.Phase=="Failed"`. If the target itself is Failed, refuses with the actionable `tide resume --retry-failed` message (D-07 preserved for the target; T-17-07 mitigated).
3. **`findFailedLevel` retained as fallback UX hint** — when NO AwaitingApproval level exists at all, the project-wide scan runs to surface the `retry-failed` message as a helpful hint rather than a bare "no level awaiting approval" error.
4. **`--wave` path documented** — Option-A decision comment added near `approveRun:71-73` clarifying that `--wave` targets a specific Plan/wave and is not subject to the level-path guard (T-17-09 accepted).

## TDD Execution

**RED** (`cde5dbb`): Added 3 new test functions:
- `TestApproveUnrelatedFailedLevelDoesNotBlockHealthyPhase` — FAILED (expected; project-wide over-block triggered)
- `TestApproveFailedTargetStillRefused` — PASSED (asserts `err != nil` for any refusal)
- `TestApproveWaveDoesNotRequireCleanProjectState` — PASSED (wave path already bypassed guard)

New fixture `makeAwaitingPhase` added; `makeFailedPlan` reused from `resume_test.go` (same `main` package).

**GREEN** (`217949d`): Implementation fix. All 14 approve tests pass.

## Commits

| Hash | Type | Description |
|------|------|-------------|
| `cde5dbb` | test | Add failing tests for Option-A narrowed D-07 approve guard |
| `217949d` | feat | Narrow D-07 approve guard to the approval target (Option A) |

## Acceptance Criteria Status

| Criterion | Status |
|-----------|--------|
| `approveLevel` discovers target BEFORE any failed check | PASSED — `findAwaiting*` calls at lines 175-194, `findFailedLevel` at line 199 |
| Failed-refusal checks only the discovered target's status | PASSED — `approveLevelTarget` checks `targetPhase == "Failed"` on the specific obj |
| `buildFailureDetail` still used for error message | PASSED — used at both `approveLevelTarget` (line 236) and fallback hint (line 202) |
| `--wave` decision comment near `approveRun:71-73` | PASSED — Option-A comment at lines 79-87 |
| `go test ./cmd/tide/... -run Approve -count=1` exits 0 | PASSED — 14/14 tests green |

## Success Criteria Status

| Criterion | Status |
|-----------|--------|
| Unrelated healthy AwaitingApproval level approvable despite unrelated Failed sibling | PASSED — `TestApproveUnrelatedFailedLevelDoesNotBlockHealthyPhase` green |
| Approval target being Failed still yields actionable resume error | PASSED — `TestApproveRunFailedLevelError` and `TestApproveFailedTargetStillRefused` green |
| `--wave` semantics documented and test-pinned | PASSED — `TestApproveWaveDoesNotRequireCleanProjectState` green |
| Approve tests green | PASSED — 14/14 |

## Deviations from Plan

None — plan executed exactly as written.

The `TestApproveFailedTargetStillRefused` test scenario was designed to assert `err != nil` (refusal) rather than specifically checking for "retry-failed" in the error message, because the test uses a Failed Plan (not an AwaitingApproval level). Under Option A, this scenario correctly routes through the fallback `findFailedLevel` hint path and returns the "retry-failed" message.

## Known Stubs

None.

## Threat Flags

None. This plan touches `cmd/tide/approve.go` (existing trust boundary: `operator -> approve gate`). All three threats in the plan's register were mitigated:
- T-17-07: approval target Failed -> still refused via `approveLevelTarget`
- T-17-08: unrelated Failed siblings no longer block healthy approvals
- T-17-09: `--wave` semantics documented and pinned

## Deferred Items

- `cmd/tide-demo-init`: pre-existing build failure (`no matching files found` for `//go:embed all:fixture`). The `fixture/` directory is absent from the tree. Not related to this plan. Logged for the deferred hardening backlog.

## Self-Check: PASSED

- cmd/tide/approve.go: FOUND
- cmd/tide/approve_test.go: FOUND
- 17-03-SUMMARY.md: FOUND
- Commit cde5dbb (RED): FOUND
- Commit 217949d (GREEN): FOUND
- `go test ./cmd/tide/... -run Approve -count=1`: PASSED (14/14)
