---
status: partial
phase: 51-the-task-loop
source: [51-VERIFICATION.md, 51-08-PLAN.md]
started: 2026-07-19
updated: 2026-07-19
---

## Current Test

[awaiting human testing — one operator kind-cluster session covers both items]

## Tests

### 1. Live billable Task-loop proof (Plan 08 Task 2 — blocking checkpoint:human-verify)
expected: On a kind cluster with a real Anthropic key, a Task whose locked `gateCommand` fails is dispatched → the independent read-only `langgraph` verifier runs the gate out-of-band → verdict REPAIRABLE → a fresh, evidence-seeded attempt is minted → on `maxIterations` exhaustion the Task lands in `ConditionVerifyHalt` (distinct from `Failed`); then flipping the gate to pass → APPROVED → Succeeded. Verifier pod is read-only (no git-write creds); cost stays bounded by `LoopPolicy.BudgetCents`.
result: [pending]
why manual: requires the operator's real Anthropic key (`~/.tide/anthropic.key`) + real billable spend; not auto-approvable.

### 2. Live kind concurrency spec (Plan 08 Task 1 — `make test-int`)
expected: `test/integration/kind/verifier_concurrency_test.go` runs live — 3 contract-bearing Tasks dispatch concurrent `role=verifier` Jobs that never exceed the sized `verifierInFlightCount` cap (2), drain to zero, and leave no Task stranded in `Verifying`. (Spec compiles/vets/lints clean; not yet run against a live cluster.)
result: [pending]
why manual: needs a running kind cluster (Layer B); deferred from Task 1 as part of the same operator session.

## Operator runbook (prerequisites now satisfied)
Both prerequisites the executor flagged are FIXED: `TaskReconcilerDeps.VerifierImage` is wired (`450a20e4`, `TIDE_VERIFIER_IMAGE` env), and the verifier Job now carries the `estimated-cost` label for restart-safe reservations (`65747947`). To produce the proof, build+load the `tide-langgraph-verifier` image (Phase 48) into a kind cluster on the current v1alpha3 CRDs, deploy the manager with a real provider secret, apply a Project/Task whose locked `gateCommand` fails, and observe the loop transitions above. See `51-08-SUMMARY.md` for the full step sequence.

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
