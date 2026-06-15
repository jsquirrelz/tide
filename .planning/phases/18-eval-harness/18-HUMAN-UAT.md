---
status: partial
phase: 18-eval-harness
source: [18-VERIFICATION.md]
started: 2026-06-15T00:00:00Z
updated: 2026-06-15T00:00:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Live `make eval` count_tokens run (EVAL-05)
expected: Run `make eval` against a reachable credproxy with a valid signed token. Per-template real `input_tokens` printed for all five templates, each with a 1024-token cache-floor PASS/FAIL line; signed token value never appears in output (only `token present: true`); target fails closed if `TIDE_PROXY_ENDPOINT` or `TIDE_SIGNED_TOKEN` is unset.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
