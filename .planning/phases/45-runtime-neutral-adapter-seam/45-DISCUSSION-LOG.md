# Phase 45: Runtime-Neutral Adapter Seam - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-16
**Phase:** 45-runtime-neutral-adapter-seam
**Mode:** `--auto` (all gray areas auto-selected; every question resolved to the recommended option without user prompts)
**Areas discussed:** Capability-flag data model & trust posture, Flag transport & skip point, Adapter seam shape, Contract-test strategy

---

## Capability-flag data model & trust posture

| Option | Description | Selected |
|--------|-------------|----------|
| `pkg/dispatch/vendor_capabilities.go` data lookup | Research-designed shape (SUMMARY step 5): `SelfInstruments(vendor) bool` beside `provider.go`; manager-computed from resolved `Provider.Vendor`; unknown → false (synthesize) | ✓ |
| Field on `ProviderSpec` in the envelope | Travels in `in.json` — rejected: PVC-resident `in.json` is writable by the semi-trusted subagent pod; also widens a cross-binary contract the executor doesn't need | |
| CRD/chart-config-driven capability map | Operator-editable — rejected: capability is a property of the runtime implementation, not a per-install policy; config drift would violate the single-source-of-truth rule | |

**Auto-selected:** `[auto] Capability data model → "New pkg/dispatch/vendor_capabilities.go, data-shaped SelfInstruments(vendor) lookup, manager-computed, unknown-vendor→false" (recommended default — research SUMMARY step 5 + PITFALLS Pitfall 7)`
**Notes:** Default-safe polarity (absent → synthesize) is Pitfall 7's explicit warning-sign rule; a false "native" assumption silently yields zero spans vs visible duplicates.

---

## Flag transport & skip point

| Option | Description | Selected |
|--------|-------------|----------|
| ReporterOptions field → CLI arg; reporter's `synthesizeSpans` is the sole skip point | Matches reporter_jobspec Args convention + Pitfall 7 "single source of truth read at parse time"; manager Job spec is the pod-tamper-proof channel; absent flag = synthesize | ✓ |
| Manager also skips the trace-only spawn entirely | Zero Job churn (extends 44 D-06) — deferred: no self-instrumenting vendor exists, so no real churn today; a second skip path would split the source of truth; noted for the LangGraph milestone | |
| Reporter derives vendor from `in.json` + its own lookup | No manager wiring — rejected on the D-02 trust posture (pod-writable input driving a skip decision) | |

**Auto-selected:** `[auto] Flag transport & skip point → "ReporterOptions field → CLI arg (suggested --emit-message-spans); synthesizeSpans single skip point; no spawn-gating this phase" (recommended default — research ARCHITECTURE diagram + Pitfall 7)`
**Notes:** Flag disables only the synth step; combined-mode materialization always runs. Skip path writes no sentinel.

---

## Adapter seam shape

| Option | Description | Selected |
|--------|-------------|----------|
| Capability flag + traceparent contract, NOT a Go interface | Research Pattern 4's explicit design: a self-instrumenting `Subagent.Run()` live-instruments in-process and needs no synthesis hook; `tracesynth.go` stays put as the anthropic-CLI runtime's adapter, doc contract updated | ✓ |
| Extract a `TraceSynthesizer` Go interface, move parser under `internal/subagent/anthropic/` | Literal reading of "adapter behind the Subagent seam" — rejected: research Pattern 4 rejects the interface shape; a pure-scaffolding phase shouldn't carry a file-move refactor with zero behavioral payoff | |

**Auto-selected:** `[auto] Adapter seam shape → "Capability flag, NOT a Go interface (research Pattern 4); tracesynth.go stays put; D-08 doc contract makes the seam legible" (recommended default)`
**Notes:** ADAPT-01's own colon-clause defines the operational meaning (flag-as-data + reporter skip + contract test) — flagged in CONTEXT.md Specifics so the planner doesn't re-read the roadmap phrasing as an interface extraction.

---

## Contract-test strategy

| Option | Description | Selected |
|--------|-------------|----------|
| In-process stub-runtime contract test (SpanRecorder): env-carrier extraction + zero-duplicates + default-safe pin | Proves both Pitfall-7 failure directions generically; reuses Phase 44 fixtures for "valid events.jsonl present, zero synthesized spans"; no LangGraph span shapes (research §137) | ✓ |
| Full envtest/kind e2e with a stub subagent image | Heavier fidelity — rejected as the contract vehicle: the seam is process-local (flag → skip; env → context); spawn-site arg wiring gets a cheap unit assertion (D-11) instead | |

**Auto-selected:** `[auto] Contract test → "tracetest.SpanRecorder stub-runtime test: TRACEPARENT env-carrier extraction becomes active context before any span + zero synthesized spans with flag set + default-safe direction pinned (SelfInstruments(\"anthropic\")==false asserted)" (recommended default — PITFALLS Pitfall 7 + research §115/§137)`

---

## Claude's Discretion

- Exact flag name/polarity encoding (within absent-means-synthesize).
- `SelfInstruments` shape (bare func vs tiny Capabilities struct) and the test stub-vendor injection mechanism.
- Whether `ReporterOptions` carries the boolean or the vendor string (boolean recommended).
- Placement/wording of the doc-contract updates (tracesynth.go, vendor_capabilities.go, otelai cross-refs).

## Deferred Ideas

- Manager-side trace-only spawn-gating on the capability flag → LangGraph beachhead milestone (alongside the native-emission flag-flip).
- The actual LangGraph adapter + its span-tree research → vNext milestone by explicit requirement.

## Todo Cross-Reference

`[auto]` All four keyword matches (scores ≥0.4: signed-commits-verified-badge, task-dispatch-gate-order-divergence, cache-f1-direct-sdk-cross-pod-caching; 0.2: project-dispatch-missing-failurehalt-gate) carried forward as **reviewed-not-folded**, honoring the identical dispositions locked in Phases 42/43/44 (keyword false-positives for tracing scope) rather than mechanically folding on score.
