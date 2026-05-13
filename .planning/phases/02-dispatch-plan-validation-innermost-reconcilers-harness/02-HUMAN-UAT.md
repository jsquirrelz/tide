---
status: partial
phase: 02-dispatch-plan-validation-innermost-reconcilers-harness
source: [02-VERIFICATION.md]
started: 2026-05-13T00:00:00Z
updated: 2026-05-13T00:00:00Z
---

## Current Test

[awaiting human testing — Docker + kind v0.31 environment required]

## Tests

### 1. Run `make test-int` (Layer B kind suite) — Wave dispatch end-to-end (AC#1)
expected: Three-task wave (alpha/beta/gamma) all reach Succeeded via stub-subagent Jobs in kind cluster; Wave CRD rolls up to Succeeded.
result: [pending]

### 2. Run Layer B 429 storm test — FAIL-03 / AC#4 synthetic kind path
expected: Pre-drained bucket causes Tasks to stay Pending with RateLimitHit condition; `tide_provider_rate_limit_hits_total` counter increments; refill allows dispatch.
result: [pending]

### 3. Run Layer B caps_test — wall-clock cap kills hang-mode Job (AC#5 / HARN-02)
expected: testMode=hang Task Job reaches Failed with reason DeadlineExceeded within activeDeadlineSeconds=70s window.
result: [pending]

### 4. Run Layer B credproxy_test — sidecar starts and emits startup log (AC#5 / HARN-03)
expected: Pod contains tide-credproxy init container; container log contains "credproxy listening on 127.0.0.1:8443".
result: [pending]

### 5. Run Layer B output_test — exceed-output-paths causes Task to fail (AC#5 / HARN-05)
expected: Task with testMode=exceed-output-paths reaches Failed phase after harness output validator detects out-of-scope write.
result: [pending]

### 6. Run Layer B failure_test — failed sibling does not block independent tasks; dependent never dispatches (AC#3 / FAIL-01)
expected: alpha-fail (independent) reaches Succeeded; beta-fail (testMode=fail-exit-1) reaches Failed; gamma-fail (depends on beta) never enters Running.
result: [pending]

## Summary

total: 6
passed: 0
issues: 0
pending: 6
skipped: 0
blocked: 0

## Gaps
