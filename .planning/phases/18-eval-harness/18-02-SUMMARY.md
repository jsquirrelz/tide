---
phase: 18-eval-harness
plan: "02"
subsystem: eval-harness
tags: [testing, eval, cost-parity, protocol-compliance, dag, caching]
dependency_graph:
  requires: ["18-01"]
  provides: ["EVAL-02", "EVAL-04"]
  affects: ["internal/eval", "internal/subagent/anthropic"]
tech_stack:
  added: []
  patterns:
    - in-package tests for unexported symbol access (package anthropic test files)
    - t.TempDir() filesystem fixture isolation
    - errors.As for typed error assertion on *dag.CycleError
    - synthetic JSONL fixture for deterministic cost replay
key_files:
  created:
    - internal/subagent/anthropic/cost_parity_test.go
    - internal/subagent/anthropic/protocol_compliance_test.go
    - internal/eval/protocol_test.go
    - internal/eval/cost_replay_test.go
    - internal/eval/testdata/fixtures/stream_real.jsonl
  modified: []
decisions:
  - "ChildCRDSpec JSON format uses top-level 'name' field (not metadata.name); PATTERNS.md fixture was K8s-style but the struct is not a K8s Object"
  - "TestCostParity_RealizedSavings uses M-token scale to avoid ceiling-division tie at small token counts"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-15"
  tasks_completed: 3
  files_created: 5
---

# Phase 18 Plan 02: Protocol-Compliance Gate + Cost-Parity Test Summary

Deterministic protocol-compliance gate + cost-parity/realized-savings tests — zero-network, no LLM judge, all running under `make test`.

## What Was Built

### Task 1: In-package cost-parity + realized-savings test (commit 10647e0)

`internal/subagent/anthropic/cost_parity_test.go` (package anthropic):

- `TestCostParity_FourField`: asserts `estimatedCostCents("claude-sonnet-4-6", u)` equals the hand-computed value (399 cents) for a four-field Usage (InputTokens=500K, OutputTokens=100K, CacheReadTokens=800K, CacheCreationTokens=200K). Includes inline arithmetic comment per pricing_test.go convention. Also asserts zero Usage returns 0.
- `TestCostParity_RealizedSavings`: constructs uncached (5.2M input + 200K output) and cached (1M input + 200K output + 1.6M cacheRead + 2.4M cacheCreation) scenarios and asserts costWithCaching < costNoCaching — proves REALIZED savings after absorbing the cache-write premium.
- `TestCostParity_RealizedSavings_AtScale`: verifies exact cent values (930 vs 774 cents = 156 cents net savings) at 1M-token scale. Both branches delegate entirely to `estimatedCostCents`; no hand-rolled rate arithmetic.

### Task 2: In-package readChildCRDs protocol-compliance test (commit 7779232)

`internal/subagent/anthropic/protocol_compliance_test.go` (package anthropic):

- `TestReadChildCRDs_ValidTask`: well-formed Task JSON (`{"kind":"Task","name":"task-01","spec":{...}}`) → 1 spec, no error.
- `TestReadChildCRDs_BadKind`: kind "Forbidden" (not in childKindAllowlist) → error. Exercises T-18-02-01 mitigation.
- `TestReadChildCRDs_MissingName`: empty name field → error. Uses `t.TempDir()` for all fixtures (no pre-committed testdata JSON).

### Task 3: DAG acyclicity, output-path check, cost-replay, fixture (commit ec8fa94)

`internal/eval/protocol_test.go` (package eval):
- `TestDAGAcyclicity_AcyclicFixture`: 3-node linear DAG → 3 waves, no error.
- `TestDAGAcyclicity_CyclicFixture`: 2-node cycle → `*dag.CycleError` via `errors.As`.
- `TestDeclaredOutputPaths_Presence`: structural check — non-empty DeclaredOutputPaths passes, empty fails.

`internal/eval/cost_replay_test.go` (package eval):
- `TestCostReplay_ParseStream`: reads `testdata/fixtures/stream_real.jsonl`, calls `anthropic.ParseStream`, asserts all four token dimensions (input=500, output=100, cacheRead=800, cacheCreation=1200).

`internal/eval/testdata/fixtures/stream_real.jsonl`: synthetic three-line JSONL modeling a cache-warm second dispatch (all four token dimensions non-zero; `cache_creation_input_tokens=1200` covers the write-premium path).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] ChildCRDSpec JSON format mismatch in PATTERNS.md**
- **Found during:** Task 2 (TestReadChildCRDs_ValidTask failing)
- **Issue:** The PATTERNS.md fixture used K8s-style `{"metadata":{"name":"task-01"}}` but `ChildCRDSpec` (pkg/dispatch/childcrd.go) has a top-level `"name"` JSON field — it is not a K8s Object.
- **Fix:** Used the correct format `{"kind":"Task","name":"task-01","spec":{}}` matching `childcrd_read_test.go` conventions.
- **Files modified:** `internal/subagent/anthropic/protocol_compliance_test.go`
- **Commit:** 7779232

**2. [Rule 1 - Bug] Ceiling-division tie at small token counts in TestCostParity_RealizedSavings**
- **Found during:** Task 1 (TestCostParity_RealizedSavings failing)
- **Issue:** The PATTERNS.md example used 2600/100 token counts that both ceil to 1 cent, making the savings assertion ambiguous.
- **Fix:** Scaled the token counts to M-token units (5.2M uncached vs 1M+cacheRead+cacheCreation cached) where the savings are unambiguous. Kept an AtScale variant that also verifies the exact values (930 vs 774 cents).
- **Files modified:** `internal/subagent/anthropic/cost_parity_test.go`
- **Commit:** 10647e0

## Verification

- `go test ./internal/eval/... ./internal/subagent/anthropic/...` exits 0 (all tests pass)
- `git diff --quiet internal/subagent/anthropic/pricing.go internal/subagent/anthropic/subagent.go` — both locked files untouched
- `go build ./internal/eval/...` exits 0 (no import cycle)
- `stream_real.jsonl` has `cache_creation_input_tokens":1200` (non-zero)
- `protocol_test.go` imports none of internal/controller, internal/budget, internal/metrics, api/v1alpha1

## Known Stubs

None — all test paths are fully wired to existing production functions (`estimatedCostCents`, `readChildCRDs`, `dag.ComputeWaves`, `anthropic.ParseStream`).

## Threat Flags

No new production code or network endpoints were introduced. This plan adds only test files and a synthetic fixture (T-18-02-02: accept).

## Self-Check: PASSED

Files exist:
- FOUND: internal/subagent/anthropic/cost_parity_test.go
- FOUND: internal/subagent/anthropic/protocol_compliance_test.go
- FOUND: internal/eval/protocol_test.go
- FOUND: internal/eval/cost_replay_test.go
- FOUND: internal/eval/testdata/fixtures/stream_real.jsonl

Commits exist: 10647e0, 7779232, ec8fa94
