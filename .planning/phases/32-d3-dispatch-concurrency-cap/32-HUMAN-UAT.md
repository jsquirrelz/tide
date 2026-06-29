---
status: partial
phase: 32-d3-dispatch-concurrency-cap
source: [32-VERIFICATION.md]
started: 2026-06-29
updated: 2026-06-29
---

## Current Test

[awaiting human testing — live-cluster smoke test]

## Tests

### 1. SC#1 — Live steady-state cap observation
expected: With `plannerConcurrency=N` (e.g. 2) and 5+ Milestones enqueued, `kubectl get jobs -l tideproject.k8s/role=planner -w` shows at most N non-terminal (Running/Pending) planner Jobs at any moment; excess dispatches park and appear as new Jobs only as earlier ones reach terminal state. Deferred dispatches emit a V(1) log line and never silently drop.
result: [pending]
why_manual: envtest does not run real pods, so the steady-state running-count cannot be observed in the automated suite. The gate LOGIC is already proven deterministically by `internal/controller/dispatch_concurrency_cap_test.go` (caps at N, returns RequeueAfter); this item is the live-cluster confirmation only.
how_to_run: |
  On a kind cluster with the new image:
    helm upgrade ... --set plannerConcurrency=2
    # apply a Project that fans out to 5+ Milestones
    kubectl get jobs -l tideproject.k8s/role=planner -w
  Confirm ≤2 non-terminal planner Jobs at a time; check manager logs for the deferred-dispatch V(1) lines.

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps

None — all automated gates pass (build, full `make test`, `make lint` 0 issues, code review 0 blockers, schema-drift clean). 5/5 must-haves verified in envtest. Only the live-cluster smoke test for SC#1 remains.
