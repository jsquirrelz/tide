# EVAL-05 — Live `make eval` count_tokens result

**Run:** 2026-06-15 · model `claude-sonnet-4-6` · real Anthropic `count_tokens` API via a
locally-run credproxy (HMAC token + real `ANTHROPIC_API_KEY`, user-supplied).

## Setup (host-independent reproduction)

The host is macOS; Go on darwin uses the Security.framework TLS verifier and ignores
`SSL_CERT_FILE`, so the credproxy's self-signed CA could not be trusted by the default
`tide-eval` client on the host. Ran the credproxy + `tide-eval` together inside a
`golang:1.26.3` Linux container (its loopback reaches the in-container credproxy;
`SSL_CERT_FILE` honored on Linux; bridge egress to `api.anthropic.com`). The real key was
passed only as a `600`-mode file; the signing key/token were a throwaway local pair.

## Result

```
tide-eval: token present: true
tide-eval: proxy endpoint: https://127.0.0.1:8443
tide-eval: model: claude-sonnet-4-6

project_planner:   660 tokens — cache-floor(1024): FAIL
milestone_planner: 612 tokens — cache-floor(1024): FAIL
phase_planner:     648 tokens — cache-floor(1024): FAIL
plan_planner:     1168 tokens — cache-floor(1024): PASS
task_executor:     559 tokens — cache-floor(1024): FAIL
tide-eval: one or more templates are below the 1024-token cache floor
```

Tool exit = 1 (its correct "below-floor" signal — NOT a harness failure).

## Acceptance criteria (all met)

1. ✅ Per-template real `input_tokens` printed for all five templates.
2. ✅ Each line carries a 1024-token cache-floor PASS/FAIL verdict.
3. ✅ Signed token value never appears in output — only `token present: true`.
4. ✅ Fails closed when `TIDE_PROXY_ENDPOINT` / `TIDE_SIGNED_TOKEN` unset (verified
   separately on host: `make eval` → `ERROR: … refusing to run eval`, exit non-zero).

## Finding (baseline for Phases 19–20)

4 of 5 templates sit below the 1024 Sonnet/Opus prefix-cache floor; **all 5 are below the
4096 Haiku floor** (the DoD model). This empirically confirms the milestone's load-bearing
research finding: today's ~200–700-token template bodies never trigger prefix caching.
These counts are a FLOOR on billed input (the CLI prepends its own system prompt + tool
schemas; WR-02), so the real billed input is higher — but the relative baseline is what
Phase 19 (reorder + trim) and Phase 20 (SharedContext prefix) act on.
