# Requirements: TIDE v1.0.2 — Ebb Tide (Token & Cost Optimization)

**Defined:** 2026-06-15
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.

**Milestone goal:** Cut TIDE's per-run token spend without degrading output quality — the cost-reduction prep that makes a second TIDE-on-TIDE dogfood run affordable.

**Binding constraints (apply to every requirement below):**
- Stay CLI-based — TIDE shells to `claude -p --bare`; it does NOT set `cache_control` (the CLI does). No direct-SDK backend this milestone.
- Provider-agnostic by design; verified live on the Claude path. No Anthropic-only assumptions.
- Best-effort reduction, **quality-gated** — no hard numeric cost target; the eval harness guards that token cuts don't regress output.
- Do NOT rebuild cost accounting / pricing / metrics — they are already correct (`pricing.go`, `stream_parser.go`, `metrics/registry.go`).

## Milestone v1.0.2 Requirements

Requirements for this milestone. Each maps to exactly one roadmap phase.

### Eval Harness (EVAL)

The quality + cost gate. Must land before any prompt/template change.

- [ ] **EVAL-01**: A maintainer can capture a frozen baseline (golden renders of all five templates + a recorded usage snapshot) from the current v1.0.1 templates, committed under `testdata/`, so later changes are measured against a stable reference.
- [ ] **EVAL-02**: The harness runs deterministic protocol-compliance checks as the primary quality gate — child-CRD parse success, declared-output-path presence, and DAG acyclicity — with no LLM judge required for the gate.
- [ ] **EVAL-03**: The harness golden-file snapshot-tests every rendered template (via `goldie/v2`, test-only), runs in `make test-unit` with zero network, and flags accidental prompt growth.
- [ ] **EVAL-04**: The harness computes cost deltas by delegating to the existing `estimatedCostCents` (asserting parity, no re-implementation), and reports REALIZED savings per wave (cache-write premium subtracted), not gross per-dispatch read discount.
- [ ] **EVAL-05**: A maintainer can run pre-flight token counting via the Anthropic `count_tokens` endpoint through the existing credproxy (stdlib `net/http`, no SDK) behind a `//go:build eval` tag, exposed as a `make eval` target.
- [ ] **EVAL-06**: The harness regression-gates prompt/template changes — a change that grows tokens beyond a tuned threshold or fails a protocol-compliance check is caught in CI.

### Prompt Structuring & Token Minimization (PROMPT)

- [ ] **PROMPT-01**: All five prompt templates are reordered stable-prefix-first (role preamble → fixed instructions → shared context → volatile metadata → per-task prompt).
- [ ] **PROMPT-02**: Volatile per-dispatch metadata (`TaskUID`, `Provider.*`, branch) is moved out of the stable prefix into the suffix so wave-sibling dispatches no longer diverge in the prefix.
- [ ] **PROMPT-03**: Each template carries a "why-this-line" annotation produced before trimming, so no load-bearing instruction (proven necessary by a prior production cascade) is removed blindly.
- [ ] **PROMPT-04**: Non-essential boilerplate is trimmed one section at a time, each commit gated green by the eval harness (protocol-compliance preserved).
- [ ] **PROMPT-05**: Structured data interpolated into the stable prefix is serialized deterministically (stable key order) so identical inputs render identical bytes.

### Cache-Aware Shared Context (CACHE)

- [ ] **CACHE-01**: A spike verifies whether stable-prefix-first ordering yields cross-pod prefix-cache hits across wave siblings under `claude -p --bare` (the working-directory-embedding question); its result gates CACHE-02/03 and is recorded as a decision.
- [ ] **CACHE-02**: `EnvelopeIn` gains an additive `SharedContext` field (omitempty; executor path ignores it) that the controller populates identically for all wave siblings.
- [ ] **CACHE-03**: Planner templates reference `SharedContext`, hoisting shared plan/phase context into the stable prefix and growing it toward the provider's cacheable minimum (≥1,024 tokens Sonnet/Opus; documented gap for Haiku 4,096).
- [ ] **CACHE-04**: Shared context is fed as curated summaries rather than verbatim PLAN.md / phase-brief dumps, cutting tokens without losing the load-bearing context.
- [ ] **CACHE-05**: The optimization carries no Anthropic-only assumptions — the design is verified provider-neutral and live-verified on the Claude path (OpenAI/Codex parity deferred to the run-#2 milestone).

### Cost & Cache Observability (OBSV)

- [ ] **OBSV-01**: Per-level token accounting is queryable — the existing token counters are labeled so spend can be attributed per level (project/phase/plan/wave already present; extend as needed).
- [ ] **OBSV-02**: A cache-hit-rate metric is derived from dispatch usage (`cache_read` vs `cache_creation`) and emitted via the existing Prometheus surface.
- [ ] **OBSV-03**: The read-only dashboard surfaces a cache-efficiency panel (hit ratio, creation tokens, realized savings) reading the existing counters — no backend dispatch-path changes.

## Future Requirements

Deferred to a later milestone. Tracked, not in this roadmap.

### Cost (COST)

- **COST-F1**: Per-level token budget profiles exposed as Helm values / CRD fields so operators can cap spend per level.
- **COST-F2**: Wave-sibling warm-up dispatch (pre-warm the shared prefix cache before fanning out a wave).

### Caching (CACHE)

- **CACHE-F1**: A direct-SDK subagent backend that sets explicit `cache_control` breakpoints / TTL (provider-controlled caching, bigger lift than the CLI path).

### Eval (EVAL)

- **EVAL-F1**: A full LLM-as-judge semantic-quality scoring pipeline (beyond the deterministic protocol-compliance gate).

## Out of Scope

Explicitly excluded this milestone. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| OpenAI/Codex subagent backend | Belongs to the dogfood-run-#2 milestone; v1.0.2 is provider-agnostic by design but live-verifies on the Claude path only. |
| Dogfood run #2 execution | This is the *pre*-run cost-optimization milestone; the run itself is next. |
| Rebuilding cost accounting / pricing / metrics | Already correct (`pricing.go`, `stream_parser.go`, `metrics/registry.go`); only surfacing/consuming is in scope. |
| Anthropic Go SDK adoption | CLAUDE.md anti-pattern — stay CLI-based. `count_tokens` is reached via stdlib `net/http` through the credproxy. |
| `cache_control` markers from TIDE | Not possible on the CLI dispatch path — the CLI owns breakpoint placement; the lever is prompt structuring. |
| Forced shared worktree for KV-cache sharing | Violates the per-task pod isolation contract. |

## Traceability

Which phases cover which requirements. Filled in during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| EVAL-01 | Phase 18 | Pending |
| EVAL-02 | Phase 18 | Pending |
| EVAL-03 | Phase 18 | Pending |
| EVAL-04 | Phase 18 | Pending |
| EVAL-05 | Phase 18 | Pending |
| EVAL-06 | Phase 18 | Pending |
| PROMPT-01 | Phase 19 | Pending |
| PROMPT-02 | Phase 19 | Pending |
| PROMPT-03 | Phase 19 | Pending |
| PROMPT-04 | Phase 19 | Pending |
| PROMPT-05 | Phase 19 | Pending |
| CACHE-01 | Phase 20 | Pending |
| CACHE-02 | Phase 20 | Pending |
| CACHE-03 | Phase 20 | Pending |
| CACHE-04 | Phase 20 | Pending |
| CACHE-05 | Phase 20 | Pending |
| OBSV-01 | Phase 21 | Pending |
| OBSV-02 | Phase 21 | Pending |
| OBSV-03 | Phase 21 | Pending |

**Coverage:**
- Milestone requirements: 19 total
- Mapped to phases: 19
- Unmapped: 0 ✓

---
*Requirements defined: 2026-06-15*
*Last updated: 2026-06-15 — traceability filled at roadmap creation*
