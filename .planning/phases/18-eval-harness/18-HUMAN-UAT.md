---
status: complete
phase: 18-eval-harness
source: [18-VERIFICATION.md]
started: 2026-06-15T00:00:00Z
updated: 2026-06-15T18:36:00Z
---

## Current Test

[all tests complete]

## Tests

### 1. Live `make eval` count_tokens run (EVAL-05)
expected: Run `make eval` against a reachable credproxy with a valid signed token. Per-template real `input_tokens` printed for all five templates, each with a 1024-token cache-floor PASS/FAIL line; signed token value never appears in output (only `token present: true`); target fails closed if `TIDE_PROXY_ENDPOINT` or `TIDE_SIGNED_TOKEN` is unset.
result: PASSED (2026-06-15) — ran live against the real Anthropic count_tokens API via a locally-run credproxy (real ANTHROPIC_API_KEY + HMAC token). All 4 acceptance criteria met: per-template input_tokens for all five (project 660, milestone 612, phase 648, plan 1168, task 559), each with a 1024 cache-floor PASS/FAIL line; token never printed (only `token present: true`); fail-closed verified separately. Tool exit=1 is its correct below-floor signal, not a harness failure: 4/5 templates below the 1024 floor (all 5 below the 4096 Haiku floor) — the baseline Phases 19–20 will lift. Full evidence: 18-EVAL-05-LIVE-RESULT.md.

## Summary

total: 1
passed: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None.
