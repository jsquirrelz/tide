---
phase: 20-sharedcontext-injection-cache-verification-spike
verified: 2026-06-15T21:05:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
gate_decision: APPROVED
---

# Phase 20: SharedContext Injection + Cache Verification Spike — Verification Report

**Phase Goal:** A spike verifies whether stable-prefix-first ordering yields cross-pod prefix-cache hits across wave siblings under `claude -p --bare`; if it does, `EnvelopeIn` gains a `SharedContext` field populated identically for all wave siblings to grow the shared cacheable prefix to ≥1,024 tokens; if it does not, this phase closes as best-effort token minimization with the spike decision recorded.
**Verified:** 2026-06-15T21:05:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

This is a **spike phase with conditional success criteria**. The spike settled the cross-pod caching question with live evidence (3 runs on `kind-tide-dogfood`, real Anthropic API, `claude-sonnet-4-6`) and recorded the decision. The verdict — *cross-pod caching fires but caller-controlled content does not cache-read on the CLI path* — triggered the contingency branch: SharedContext ships unconditionally as token-minimization, cache payoff deferred to a direct-SDK backend (CACHE-F1). The spike doing its job (settle + record) IS the success condition for criterion 1; the SharedContext field shipped unconditionally for criteria 2–5.

### Observable Truths

| # | Truth (ROADMAP Success Criterion) | Status | Evidence |
|---|-----------------------------------|--------|----------|
| 1 | Spike result committed as a decision in PROJECT.md | ✓ VERIFIED | `.planning/PROJECT.md:124,126–152` — full CACHE-01 decision record: "cross-pod caching fires but caller content does not cache-read; reframed to token-minimization; cache deferred to CACHE-F1." Live evidence: dispatch A `cache_creation=12296`, B `cache_read=1307`/`cache_creation=10989`. Per-provider floor table + D-08 resolution present. |
| 2 | `EnvelopeIn` gains `SharedContext string` (omitempty), executor ignores it, `BuildPlannerEnvelope` populates identically for all wave siblings | ✓ VERIFIED | `envelope.go:157` (`SharedContext string \`json:"sharedContext,omitempty"\``); `dispatch_helpers.go:223,235` (param + stamp); executor `buildEnvelopeIn` sets it NOWHERE (grep returns 0 in `task_controller.go`); lock test `TestBuildEnvelopeInExecutorIgnoresSharedContext` asserts `==""`; sibling-identity test `TestBuildPlannerEnvelopeSharedContext` |
| 3 | Planner templates reference `{{.SharedContext}}` in the stable prefix; ≥1,024-token floor reached OR Haiku gap documented | ✓ VERIFIED | All four `{milestone,project,phase,plan}_planner.tmpl` interpolate `{{if .SharedContext}}{{.SharedContext}}` in the D-07 slot; `task_executor.tmpl:24` is a `{{/* */}}` comment only (no interpolation). Floor: documented as conditional/moot in PROJECT.md:137 (caller content does not cache on CLI path), per-provider floor table at PROJECT.md:139 — honestly recorded, not a failure |
| 4 | SharedContext populated from curated summaries, not verbatim dumps | ✓ VERIFIED | `materialize.go:225,233,241,260` — byte-identical stamp from single `child.SharedContext` across all 4 kinds; 64 KiB cap at `materialize.go:54,210`; curation source is the parent planner's curated wave-scoped blob (D-04, PROJECT.md:137 ~300–700 tok) |
| 5 | No Anthropic-only assumptions; provider-neutral; OpenAI/Codex parity deferred in decision record | ✓ VERIFIED | Provider-neutrality grep returns ZERO vendor-coupled SharedContext lines across envelope.go/childcrd.go/dispatch_helpers.go/4 templates; PROJECT.md:150 records "OpenAI/Codex live parity deferred to run-#2 milestone"; field lives on provider-agnostic `EnvelopeIn` |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/dispatch/envelope.go` | SharedContext on EnvelopeIn + EnvelopeOut (omitempty) | ✓ VERIFIED | Lines 157, 231 — both fields with `omitempty` |
| `pkg/dispatch/childcrd.go` | SharedContext carry field on ChildCRDSpec | ✓ VERIFIED | Line 75, omitempty, orchestrator-set (not LLM-authored) |
| `api/v1alpha1/{task,phase,plan,milestone}_types.go` | SharedContext on all 4 specs; ProjectSpec none | ✓ VERIFIED | 4 specs carry it; `project_types.go` has none (root, correct per D-07) |
| `internal/controller/dispatch_helpers.go` | BuildPlannerEnvelope sharedContext param + stamp | ✓ VERIFIED | Signature line 223, stamp line 235; all 5 call sites pass parent Spec.SharedContext or "" |
| `internal/reporter/materialize.go` | Byte-identical stamp on 4 kinds + 64 KiB cap | ✓ VERIFIED | 4× `Spec.SharedContext = child.SharedContext`; cap at line 210 |
| `internal/subagent/common/templates/*_planner.tmpl` | `{{.SharedContext}}` in 4 planners, NOT executor | ✓ VERIFIED | 4 planners interpolate; executor comment-only |
| `cmd/tide-spike/main.go` | `//go:build spike` cross-pod cache harness | ✓ VERIFIED | Build tag line 1; distinct `--add-dir` paths; ParseStream→CacheReadTokens verdict; `go build -tags spike` exit 0 |
| `internal/credproxy/server.go` | TeeBodyDir FAIL-path body tee | ✓ VERIFIED | TeeBodyDir field + io.LimitReader tee + body restore; disabled by default |
| `.planning/PROJECT.md` | CACHE-01 decision + floor table + CACHE-05 deferral | ✓ VERIFIED | Lines 124–152 |

### Key Link Verification

| From | To | Via | Status |
|------|----|----|--------|
| EnvelopeOut.SharedContext | ChildCRDSpec.SharedContext → child Spec | MaterializeChildCRDs per-Kind stamp | ✓ WIRED (4 stamps) |
| parent CRD Spec.SharedContext | EnvelopeIn.SharedContext | BuildPlannerEnvelope sharedContext param (5 call sites) | ✓ WIRED |
| EnvelopeIn.SharedContext | planner template reserved slot | `{{if .SharedContext}}` guard | ✓ WIRED (4 templates) |
| spike harness | stream_parser ParseStream | CacheReadTokens verdict | ✓ WIRED |
| spike FAIL path | credproxy TeeBodyDir | body tee for diff | ✓ WIRED |
| make spike verdict | PROJECT.md Key Decisions | decision-record prose w/ root cause | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build clean | `go build ./...` | exit 0 | ✓ PASS |
| Spike binary builds | `go build -tags spike ./cmd/tide-spike/` | exit 0 | ✓ PASS |
| Phase-20 unit suite | `go test ./pkg/dispatch/... ./api/... ./internal/{controller,reporter,eval,credproxy}/...` | all ok (controller 62.4s) | ✓ PASS |
| Executor-omit lock | `TestBuildEnvelopeInExecutorIgnoresSharedContext` | green | ✓ PASS |
| Provider-neutrality | grep vendor terms on SharedContext path | 0 matches | ✓ PASS |

Live spike execution (make spike against real Anthropic API) is a one-time recorded result, not re-run here — its verdict is captured in PROJECT.md:131–133 with reproducible token counts. Re-running would consume real API budget and is not required for goal verification.

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|---------------|-------------|--------|----------|
| CACHE-01 | 20-04, 20-05 | Spike verifies cross-pod caching; result recorded as decision | ✓ SATISFIED | Spike harness + 3 live runs + PROJECT.md decision record |
| CACHE-02 | 20-01, 20-03 | EnvelopeIn additive SharedContext (executor ignores); controller populates siblings identically | ✓ SATISFIED | Field + stamp + executor-omit lock test |
| CACHE-03 | 20-02 | Planner templates reference SharedContext, growing stable prefix | ✓ SATISFIED | 4 templates interpolate; golden + ordering tests |
| CACHE-04 | 20-03 | Curated summaries not verbatim dumps | ✓ SATISFIED | Byte-identical single-blob stamp; 64 KiB cap; D-04 curation source |
| CACHE-05 | 20-05 | No Anthropic-only assumptions; provider-neutral; OpenAI parity deferred | ✓ SATISFIED | Grep-clean; deferral noted in PROJECT.md:150 |

**Note on CACHE-05 / REQUIREMENTS.md `[ ]` Pending:** REQUIREMENTS.md marks CACHE-05 as Pending because its requirement text bundles "live-verified on the Claude path" with "OpenAI/Codex parity deferred to run-#2." The *in-phase obligation* — provider-neutral design, grep-verified clean, with the deferral recorded as a decision — is fully met. The ROADMAP Phase 20 success criterion 5 (the actual phase contract) asks only that the design be provider-agnostic AND the decision record note the run-#2 deferral, both of which are TRUE. The residual "Pending" marker tracks the *next-milestone* live-OpenAI work, which is correctly out of Phase 20 scope. This is not a Phase 20 gap.

All 5 declared requirement IDs (CACHE-01..05) are accounted for and satisfied within phase scope. No orphaned requirements: REQUIREMENTS.md maps exactly CACHE-01..05 to Phase 20.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None | — | No TBD/FIXME/XXX/PLACEHOLDER in any phase-modified production file; no stub returns on the SharedContext path |

### Deferred Items (informational, not gaps)

| Item | Addressed In | Evidence |
|------|-------------|----------|
| Cross-pod *cache benefit* on caller content | CACHE-F1 (next milestone) | `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md` + REQUIREMENTS.md:62 — requires a direct-SDK backend owning the request body to place `cache_control`; CLI path has no suppression lever for the `cch` nonce |
| Live OpenAI/Codex provider parity (CACHE-05 live half) | run-#2 milestone | PROJECT.md:150 deferral note |
| Precise per-template live floor measurement | CACHE-F1 follow-up | PROJECT.md:137 — moot for cache decision on CLI path |

These are explicitly scoped to later work in the decision record and do NOT block Phase 20 goal achievement.

### Human Verification Required

None. The phase produces code (verified by build + targeted unit suite, all green) and a recorded decision (verified by reading PROJECT.md). The live spike result is a settled, recorded artifact. No visual/UX/real-time behavior requires human testing.

### Gaps Summary

No gaps. All 5 ROADMAP success criteria are observably TRUE in the codebase:
1. The spike decision is recorded in PROJECT.md with live evidence and the original blocker explicitly REFUTED.
2. `EnvelopeIn.SharedContext` (omitempty) exists, the executor path provably omits it (lock test green), and BuildPlannerEnvelope stamps it byte-identically across siblings.
3. Four planner templates interpolate `{{.SharedContext}}` in the stable prefix; the executor template does not. The ≥1,024-token floor is honestly recorded as conditional/moot for the cache benefit per the spike finding — not a failure.
4. SharedContext flows as a single curated blob stamped byte-identically with a 64 KiB etcd guard.
5. The SharedContext path carries zero vendor-coupled lines; OpenAI/Codex parity is explicitly deferred in the decision record.

Build is clean, the phase-20 unit suite is green, the spike binary compiles under its build tag, and no debt markers exist in modified files.

---

_Verified: 2026-06-15T21:05:00Z_
_Verifier: Claude (gsd-verifier)_
