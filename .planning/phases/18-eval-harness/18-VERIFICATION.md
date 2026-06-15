---
phase: 18-eval-harness
verified: 2026-06-15T00:00:00Z
status: verified
score: 6/6 must-haves verified (automated); live EVAL-05 check PASSED (2026-06-15)
overrides_applied: 0
human_verification:
  - test: "Run `make eval` against a reachable credproxy with a valid signed token (EVAL-05 live surface)."
    expected: "Per-template real `input_tokens` printed for all five templates, each with a 1024-token cache-floor PASS/FAIL line; signed token value never appears in output (only `token present: true`); target fails closed if TIDE_PROXY_ENDPOINT or TIDE_SIGNED_TOKEN is unset."
    why_human: "The count_tokens POST crosses the credproxy network boundary and needs real Anthropic credentials. Zero-network verification can only confirm the tool compiles under -tags eval, is excluded from the default build, wires the count_tokens route + anthropic-version header, and the Makefile guards both env vars. It cannot exercise the live HTTP path."
    result: "PASSED 2026-06-15 — ran live against the real Anthropic count_tokens API via a locally-run credproxy. All 4 criteria met (per-template tokens for all five: project 660 / milestone 612 / phase 648 / plan 1168 / task 559; cache-floor PASS/FAIL per line; token never printed; fail-closed confirmed). Tool exit=1 = correct below-floor signal (4/5 below 1024). Evidence: 18-EVAL-05-LIVE-RESULT.md."
notes:
  - "WR-01 (advisory): task_executor.golden + ratchet + count_tokens floor are rendered with the single planner/plan fixture (Role: planner, Level: plan), a body production never sends (executor dispatches are Role: executor / Level: task). The frozen baseline EXISTS and is deterministic for all five templates, so the phase GOAL (a frozen baseline + deterministic gate) is met — but one of five templates' baseline is non-representative. This is a baseline-fidelity defect, not a goal failure or unmet requirement. Recommend fixing before Phase 19 trims the executor template, since the executor ratchet currently guards the wrong body."
  - "REQUIREMENTS.md EVAL-03/06 reference `make test-unit`; the phase implemented under the D-02a design decision (no test-unit target — eval tests run in the existing `make test` / `go test ./...` tier). The roadmap success-criteria wording (`make test-unit`, `testdata/baselines/`) predates D-02a; the actual implementation (`testdata/goldie/` + `testdata/ratchets/` under `make test`) satisfies the requirement's INTENT (zero-network golden gate auto-run in CI). Verified the intent, noting the wording drift."
---

# Phase 18: Eval Harness Verification Report

**Phase Goal:** A frozen v1.0.1 baseline and deterministic quality gate exist, so every subsequent template or prompt change can be measured and regression-gated without manual review.
**Verified:** 2026-06-15
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A maintainer runs `make test` / `go test ./...` and goldie snapshot tests confirm all five rendered templates match committed `testdata/goldie/*.golden` (zero-network, deterministic) | ✓ VERIFIED | `go test -count=1 ./internal/eval/...` green; 5 `TestGoldenRender_*` pass; 5 `.golden` files present; re-run mutates no file (`git status --porcelain testdata/` clean) |
| 2 | A PR that grows any template's rendered byte count beyond the committed per-template ratchet ceiling fails automatically (EVAL-06 regression gate) | ✓ VERIFIED | 5 `TestByteRatchet_*` pass; 5 ratchet `.txt` files, each an integer equal to its golden byte size (tight ceiling: e.g. plan_planner 4281=4281); test t.Errorf names the file on growth |
| 3 | The internal/eval package runs under the existing `make test` tier — no CI config change, no build tag, no new `test-unit` target (D-02/D-02a) | ✓ VERIFIED | No `//go:build` directive anywhere in `internal/eval/` (`grep go:build` rc=1); `grep -nE '^test-unit:' Makefile` rc=1 (absent); tests are in the default `go test ./...` set |
| 4 | A PR that breaks child-CRD parse success, declared-output-path presence, or DAG acyclicity is caught by the deterministic protocol-compliance gate (EVAL-02, no LLM judge) | ✓ VERIFIED | `TestReadChildCRDs_*` (12 cases incl. bad-kind, missing-name, symlink, malformed) green in package anthropic; `TestDAGAcyclicity_Acyclic/Cyclic` green using `errors.As(*dag.CycleError)`; `TestDeclaredOutputPaths_Presence` green; all zero-network |
| 5 | Cost-parity asserts the existing `(*Anthropic).estimatedCostCents` within 1 cent and reports REALIZED per-wave savings (cache-write premium subtracted), no re-implementation (EVAL-04) | ✓ VERIFIED | `cost_parity_test.go` is `package anthropic`, calls `estimatedCostCents` 9×; realized-savings subtests delegate both cached/uncached branches (930→774 cents at scale, premium absorbed); `pricing.go` 0 diff vs base |
| 6 | A `count_tokens` pre-flight ships as `cmd/tide-eval` behind `//go:build eval`, invoked by one `make eval` target, POSTs to credproxy via stdlib net/http with no SDK (EVAL-05) | ✓ VERIFIED (build/wiring); ? live run needs human | Line 1 `//go:build eval`; `go build ./...` rc=0 + `go list ./cmd/tide-eval/` reports build-constraint exclusion; `go vet -tags eval` rc=0; wires `/v1/messages/count_tokens` + `anthropic-version: 2023-06-01`; no SDK import. Live HTTP path → human |

**Score:** 6/6 truths verified by zero-network automation; truth 6's live network surface routed to human verification.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/eval/doc.go` | package eval doc + import-boundary statement | ✓ VERIFIED | `package eval`; doc names forbidden imports (controller/budget/metrics/api) |
| `internal/eval/render_test.go` | goldie golden + byte ratchet for all five templates | ✓ VERIFIED | `goldie.New` + `WithFixtureDir("testdata/goldie")`; 5 golden + 5 ratchet tests pass |
| `internal/eval/testdata/goldie/*.golden` (5) | frozen v1.0.1 golden renders | ✓ VERIFIED | exactly 5 present; deterministic |
| `internal/eval/testdata/ratchets/*.txt` (5) | byte-count ceilings | ✓ VERIFIED | exactly 5; each a single integer = golden size |
| `internal/eval/protocol_test.go` | DAG acyclicity + output-path check | ✓ VERIFIED | `dag.ComputeWaves`, `errors.As(*CycleError)` |
| `internal/eval/cost_replay_test.go` | ParseStream replay of four-field Usage fixture | ✓ VERIFIED | `TestCostReplay_ParseStream` green |
| `internal/eval/testdata/fixtures/stream_real.jsonl` | synthetic events.jsonl, four token dims non-zero | ✓ VERIFIED | present; consumed by cost-replay test |
| `internal/subagent/anthropic/cost_parity_test.go` | in-package parity + realized savings | ✓ VERIFIED | `package anthropic`, reaches unexported `estimatedCostCents` |
| `internal/subagent/anthropic/protocol_compliance_test.go` | in-package readChildCRDs tests | ✓ VERIFIED | `package anthropic`, reaches unexported `readChildCRDs` |
| `cmd/tide-eval/main.go` | count_tokens pre-flight behind `//go:build eval` | ✓ VERIFIED | tag on line 1; excluded from default build; never logs token |
| `Makefile` `eval` target | the one new target (no test-unit) | ✓ VERIFIED | `^eval:` present with `##` help; guards both env vars; `go run -tags eval` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| render_test.go | common.LoadPromptTemplate | render each (role,level) → goldie.Assert | ✓ WIRED | imports `internal/subagent/common`; 5 renders assert goldens |
| render_test.go | testdata/ratchets/<name>.txt | os.ReadFile + Atoi; fail if over ceiling | ✓ WIRED | ratchet tests read + compare |
| cost_parity_test.go | (*Anthropic).estimatedCostCents | in-package call over Usage fixtures | ✓ WIRED | 9 call sites, both savings branches |
| protocol_compliance_test.go | readChildCRDs | t.TempDir() JSON fixtures | ✓ WIRED | 6+ call sites, valid/bad-kind/missing-name |
| protocol_test.go | pkg/dag.ComputeWaves | acyclic→waves, cyclic→*CycleError | ✓ WIRED | imports `pkg/dag`; errors.As assertion |
| cost_replay_test.go | anthropic.ParseStream | replay stream_real.jsonl, assert 4 fields | ✓ WIRED | imports subagent/anthropic |
| cmd/tide-eval | credproxy POST /v1/messages/count_tokens | stdlib net/http + anthropic-version header | ✓ WIRED (compile/structure) | route + header present; live exchange → human |
| Makefile eval | cmd/tide-eval | go run -tags eval ./cmd/tide-eval/ | ✓ WIRED | line 217 |

### Locked-File Integrity (delegation, not re-implementation)

| File | Expected | Status |
|------|----------|--------|
| `internal/subagent/anthropic/pricing.go` | unchanged vs phase base | ✓ 0 diff lines vs `105fa43` |
| `internal/subagent/anthropic/subagent.go` | unchanged vs phase base | ✓ 0 diff lines vs `105fa43` |

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|-------------|-------------|--------|----------|
| EVAL-01 | 18-01 | ✓ SATISFIED | 5 frozen golden renders + ratchets committed under `internal/eval/testdata/` |
| EVAL-02 | 18-02 | ✓ SATISFIED | Deterministic protocol-compliance gate (readChildCRDs, DAG acyclicity, output-path); no LLM judge |
| EVAL-03 | 18-01 | ✓ SATISFIED | goldie/v2 (test-only, v2.8.0 indirect), zero-network, flags growth. Runs under `make test` not `make test-unit` per D-02a — intent met |
| EVAL-04 | 18-02 | ✓ SATISFIED | Delegates to `estimatedCostCents`, parity asserted, realized per-wave savings (930→774); no re-implementation |
| EVAL-05 | 18-03 | ⚠️ SATISFIED (build); live run → human | `cmd/tide-eval` behind `//go:build eval`, stdlib net/http, no SDK, `make eval` target; live count_tokens exchange needs credproxy+token |
| EVAL-06 | 18-01 | ✓ SATISFIED | Byte ratchets + protocol gate auto-run in the existing `make test` / `go test ./...` CI tier |

All 6 phase requirement IDs accounted for; none orphaned. No requirement ID maps to phase 18 in REQUIREMENTS.md that is absent from a plan's `requirements` field.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Default build excludes eval tool | `go build ./...` + `go list ./cmd/tide-eval/` | build rc=0; list reports "build constraints exclude all Go files" | ✓ PASS |
| Eval tool compiles under tag | `go vet -tags eval ./cmd/tide-eval/` | rc=0 | ✓ PASS |
| Deterministic gate green | `go test -count=1 ./internal/eval/... ./internal/subagent/anthropic/...` | all PASS (fresh, uncached) | ✓ PASS |
| Golden determinism | re-run eval tests, `git status testdata/` | no file mutation | ✓ PASS |
| 5 goldens + 5 ratchets | `ls testdata/goldie/*.golden`, `ls testdata/ratchets/*.txt` | 5 and 5 | ✓ PASS |
| No Anthropic SDK | grep new code + go.mod for `anthropic-sdk` | rc=1 (none) | ✓ PASS |
| Live count_tokens | `make eval` | requires credproxy + token | ? SKIP → human |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No TBD/FIXME/XXX debt markers in any phase-18 file | — | — |
| `internal/eval/render_test.go` / `cmd/tide-eval/main.go` | fixture | task_executor rendered with planner/plan fixture (WR-01) | ⚠️ Warning | One of five baselines non-representative; goal still met (baseline exists, deterministic). See notes. |

Debt-marker scan: no `TBD`/`FIXME`/`XXX` in any modified file. WR-02..04 and IN-01..05 from 18-REVIEW.md are advisory quality notes (overclaim wording, growth-only ratchet, missing http timeout, fixture-drift) — none mark a requirement unmet; treated as notes per instructions.

### Human Verification Required

#### 1. Live `make eval` count_tokens pre-flight (EVAL-05)

**Test:** With a credproxy running and a valid HMAC signed token, export `TIDE_PROXY_ENDPOINT` and `TIDE_SIGNED_TOKEN`, then run `make eval`.
**Expected:** Per-template real `input_tokens` printed for all five templates, each with a `cache-floor(1024): PASS/FAIL` line. The signed token value must never appear in output (only `token present: true`). With either env var unset, the target prints an ERROR and exits 1 (fails closed).
**Why human:** The count_tokens POST crosses the credproxy network boundary and needs real Anthropic credentials. Zero-network verification confirmed compile-under-tag, default-build exclusion, route + mandatory header wiring, no-SDK, no-token-leak structure, and the Makefile env guards — but cannot exercise the live HTTP exchange.

### Gaps Summary

No goal-blocking gaps. The frozen v1.0.1 baseline (5 golden renders + 5 byte ratchets) exists and is deterministic; the deterministic quality gate (protocol-compliance + cost-parity, zero-network, no LLM judge) runs green under the existing `make test` tier and auto-gates template growth — the phase goal is achieved. The locked cost/pricing files are byte-identical to base (delegation, not re-implementation). The single online surface (`make eval`) is build-isolated behind `//go:build eval` and verified at the compile/wiring level; its live token-count run is the lone item needing human verification.

WR-01 is a real baseline-fidelity warning (the executor template's golden/ratchet/floor measure a planner/plan body) but does not unmeet any requirement — a frozen, deterministic baseline does exist for all five templates. It should be fixed before Phase 19 trims the executor template, otherwise the executor ratchet guards the wrong body. The REQUIREMENTS.md `make test-unit` wording was superseded by the recorded D-02a decision; the implemented `make test` gate satisfies the requirement intent.

---

_Verified: 2026-06-15_
_Verifier: Claude (gsd-verifier)_
