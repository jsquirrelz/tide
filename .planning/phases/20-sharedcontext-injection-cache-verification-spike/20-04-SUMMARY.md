---
phase: 20-sharedcontext-injection-cache-verification-spike
plan: "04"
subsystem: cache-spike-harness
tags: [cache, spike, credproxy, eval-harness, security]
dependency_graph:
  requires: []
  provides: [CACHE-01-harness, credproxy-tee-body-dir]
  affects: [cmd/tide-spike, internal/credproxy, Makefile]
tech_stack:
  added: []
  patterns:
    - "//go:build spike build-tag pattern mirroring cmd/tide-eval"
    - "credproxy TeeBodyDir opt-in flag — io.LimitReader + restore body for proxy"
    - "exec.CommandContext shelling to claude -p --bare with distinct --add-dir per dispatch"
key_files:
  created:
    - cmd/tide-spike/main.go
  modified:
    - internal/credproxy/server.go
    - internal/credproxy/server_test.go
    - cmd/credproxy/main.go
    - Makefile
decisions:
  - "Credproxy body tee added as a runtime flag (not build tag) on the shared binary"
  - "Spike uses two UnixNano-suffixed eventsDir paths (pod-uid-A-<ns> / pod-uid-B-<ns>) to faithfully simulate two-pod divergence"
  - "runDispatch wrapper removed; runDispatchTyped returns concrete *dispatchResult to avoid interface complexity"
  - "Credproxy does NOT log full request bodies at any level (A2 confirmed) — --tee-body-dir flag added"
metrics:
  duration_minutes: 35
  completed_date: "2026-06-15"
  tasks: 2
  files_changed: 5
---

# Phase 20 Plan 04: CACHE-01 Spike Harness Summary

Build the CACHE-01 verification spike: a `//go:build spike` harness (`cmd/tide-spike/main.go`) and credproxy body-tee mechanism (`--tee-body-dir` flag) for the FAIL-path diff. The spike dispatches a synthetic ≥1,024-token identical-prefix prompt twice with distinct `--add-dir` eventsDir paths to simulate two-pod behavior, reads `CacheReadTokens` from ParseStream, and renders a scriptable PASS/FAIL verdict.

## What Was Built

**Task 1 — Credproxy outbound request-body tee:**

Added `TeeBodyDir string` field to `credproxy.Proxy`. When non-empty, each forwarded `/v1/messages` request body is read via `io.LimitReader` (1 MiB cap), written to `req-N.json` (mutex-guarded sequential numbering), then **restored on `r.Body`** so the reverse-proxy forwards it unmodified. Default empty = disabled — zero behavior change to production runs. The `--tee-body-dir` flag was added to `cmd/credproxy/main.go`. Three unit tests added: body-written-and-key-absent assertion, sequential numbering, disabled-by-default.

**Security invariant verified:** The tee captures only the outbound request BODY. The real `ANTHROPIC_API_KEY` is injected by the proxy Director as a header AFTER the body is read — it never appears in teed files. Asserted by `TestTeeBodyDir_WritesRequestBodyToFile`.

**Prior logging depth confirmed (A2 assumption):** The credproxy only logs the billing-halt event (`log.Printf("billing-halt: ...")`). No full body logging at any level exists — the `--tee-body-dir` flag is the required addition.

**Task 2 — tide-spike harness + make spike target:**

Created `cmd/tide-spike/main.go` (`//go:build spike` at line 1). Mirrors `cmd/tide-eval` exactly:
- `requireFlag` copied verbatim — fail-closed, token never printed (T-20-04-03)
- Flags: `-proxy` (env `TIDE_PROXY_ENDPOINT`), `-token` (env `TIDE_SIGNED_TOKEN`), `-model`

Synthetic probe: 12× repetitions of a ~600-char stable policy paragraph (~1,800 Sonnet tokens, well above the 1,024-floor) plus per-dispatch unique tail (`tailA`/`tailB`). Both dispatch prompts share a byte-identical prefix.

Two dispatches via `exec.CommandContext`:
- Dispatch A: `--add-dir /tmp/spike-events/pod-uid-A-<ns>/`
- Dispatch B: `--add-dir /tmp/spike-events/pod-uid-B-<ns>/`
- `cmd.Dir` intentionally NOT set (matches production: `grep -c "cmd\.Dir" subagent.go == 0`)
- `ANTHROPIC_BASE_URL`, `ANTHROPIC_API_KEY=<signedToken>`, `NODE_EXTRA_CA_CERTS` env wired

Verdict reads `usage.CacheReadTokens` from `ParseStream` (already parses `cache_read_input_tokens`):
- PASS (exit 0): dispatch B `CacheReadTokens > 0`
- FAIL (exit 1): prints first ~500 bytes of system field from `req-1.json`/`req-2.json` for diff

Makefile `spike:` target mirrors `eval:` — fail-closed on missing `TIDE_PROXY_ENDPOINT` / `TIDE_SIGNED_TOKEN`.

## Verification Results

```
go build -tags spike ./cmd/tide-spike/   → exit 0
go vet -tags spike ./cmd/tide-spike/     → exit 0
go build ./internal/credproxy/...        → exit 0
go vet ./internal/credproxy/...          → exit 0
go test ./internal/credproxy/...         → ok (6 existing + 3 new tests)
grep -n "spike:" Makefile                → 223:spike: ## cross-pod cache prefix spike ...
grep -c "//go:build spike" cmd/tide-spike/main.go → 1
```

## Deviations from Plan

**1. [Rule 1 - Bug] Removed dead `runDispatch` interface wrapper**
- **Found during:** Task 2 compile
- **Issue:** Initial draft had a `runDispatch` function returning `interface{ GetCacheReadTokens() int64 }` as an intermediate abstraction, but the call sites referenced concrete struct fields (`usageA.CacheReadTokens`) — incompatible with the interface return type.
- **Fix:** Removed the dead wrapper entirely; callers use `runDispatchTyped` which returns `*dispatchResult` directly. Simpler and type-safe.
- **Files modified:** `cmd/tide-spike/main.go`
- **Commit:** 3b154d9

**2. [Rule 1 - Bug] Removed stray `io` import**
- **Found during:** Task 2 compile
- **Issue:** Initial draft imported `"io"` but the spike doesn't use `io.Writer` directly (ParseStream takes it internally).
- **Fix:** Removed unused import.
- **Files modified:** `cmd/tide-spike/main.go`
- **Commit:** 3b154d9

**Research Assumption A2 confirmed:** Credproxy does NOT log full request bodies at any log level. The `--tee-body-dir` flag was the required addition (not a pre-existing DEBUG log path). Documented here per plan instruction.

## Known Stubs

None. The harness builds and is fully wired. The live execution and PROJECT.md verdict recording are deferred to Plan 05 (Wave 3) per the plan's explicit `NOTE`.

## Threat Flags

No new security-relevant surface beyond what the plan's threat model covers. The body tee is disabled by default; when enabled it writes only the request body (not headers containing the API key). T-20-04-01 mitigation confirmed by unit test.

## Self-Check: PASSED

- `cmd/tide-spike/main.go` — FOUND
- `20-04-SUMMARY.md` — FOUND
- Commit `9eb4d82` (Task 1: credproxy tee) — FOUND
- Commit `3b154d9` (Task 2: spike harness + make spike) — FOUND
- `go build -tags spike ./cmd/tide-spike/` → exit 0
- `go test ./internal/credproxy/...` → ok
