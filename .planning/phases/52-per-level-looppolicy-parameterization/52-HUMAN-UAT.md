---
status: partial
phase: 52-per-level-looppolicy-parameterization
source: [52-VERIFICATION.md]
started: 2026-07-20T11:05:00Z
updated: 2026-07-20T11:05:00Z
---

## Current Test

[awaiting operator authorization for billable spend]

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
result: [pending]
why_human: |
  Requires real Anthropic API spend on the kind-tide-test cluster (real key at
  ~/.tide/anthropic.key). A deliberate `checkpoint:human-verify gate=blocking`
  intentionally NOT executed under --auto — billable spend needs explicit
  operator authorization (the 51-08 precedent, where the live gate caught five
  stacked latent defects the green suites missed). Full runbook in 52-11-PLAN.md
  Task 2 how-to-verify.

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
