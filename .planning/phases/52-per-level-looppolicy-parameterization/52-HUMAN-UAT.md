---
status: complete
phase: 52-per-level-looppolicy-parameterization
source: [52-VERIFICATION.md]
started: 2026-07-20T11:05:00Z
updated: 2026-07-21T02:00:00Z
---

## Current Test

[none — all tests resolved]

## Tests

### 1. Operator-gated billable live-loop proof (52-11 Task 2)
expected: |
  Plan-check loop — a Project whose `Verification.Plan` default is Locked with a
  real gate command + a deliberately weak first Plan attempt drives:
  Verifying → `tide-verifier-plan-<uid>-1` → REPAIRABLE → child-Task deletion →
  `tide-plan-<uid>-2` planner Job carrying the findings block in its prompt →
  APPROVED (loop closes) or resolved escalation.

  Level-verify — a Project with a phase-level contract: after the phase's
  children Succeed, `tide-verifier-phase-<uid>-1`'s init container provisions
  the worktree, the gate command runs for real, a non-APPROVED verdict parks the
  phase at AwaitingApproval; `tide approve` resumes it to Succeeded with exactly
  one verifier Job.

  ESC-04 rails — `kubectl get jobs -l tideproject.k8s/role=verifier` counts stay
  ≤ the concurrency cap throughout.
result: |
  PASSED (2026-07-20/21, operator-approved billable run on kind-tide-test).
  Level-verify: both legs — red gate → REPAIRABLE stub → maxIterations:0
  exhaustion → AwaitingApproval park → `tide approve` → Succeeded with exactly
  one verifier Job (tide-lv2); green gate → APPROVED → Succeeded, no park
  (tide-lv3). Plan-check: full loop on tide-lv5 — Verifying → verifier-1 →
  REPAIRABLE → child-Task deletion → findings-seeded tide-plan-<uid>-2
  (EnvelopeIn.RepairFindings via the replan-findings annotation) → attempt-2
  Verifying → verifier-2 → REPAIRABLE → D-05 stall exhaustion →
  AwaitingApproval → `tide approve` → resolved escalation: held Task
  dispatched, Project Complete, exactly 2 verifier Jobs. ESC-04: cluster-wide
  role=verifier peak 2 (≤ cap 2), concurrently-Running ≤ 1. Spend ≈ <$0.25
  total (5 real sonnet-4-6 verifier calls) against $5/project caps.

  The gate earned its keep three times over: DEFECT-A (CEL round-trip,
  8e5f7a49, prior session), DEFECT-B (attempt-blind reporter Job name
  dead-stalled every re-plan, 1d09e049), DEFECT-C (operator approval of an
  exhausted plan-check loop silently swallowed, 5d2c299f) — all root-fixed
  with RED-first regression specs, all re-proven live. Full narrative in
  52-11-SUMMARY.md.

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none]
