# Phase 45: Runtime-Neutral Adapter Seam - Context

**Gathered:** 2026-07-16 (--auto mode: all gray areas auto-resolved to the research-recommended option; every selection logged in 45-DISCUSSION-LOG.md)
**Status:** Ready for planning

<domain>
## Phase Boundary

Wrap Phase 44's `events.jsonl`→spans synthesizer (`internal/reporter/tracesynth.go`) in the runtime-neutral adapter seam (ADAPT-01): a self-instrumenting capability flag derived from the manager's resolved `Provider.Vendor` travels **as data** to the reporter; the reporter skips message-span synthesis entirely when the flag is set; and a contract test with a stub self-instrumenting runtime proves zero duplicate spans end-to-end (env-carrier extraction only — no LangGraph-specific span shape assumed).

**Pure forward-compatibility scaffolding — behavior unchanged today.** Every current vendor resolves to "not self-instrumenting" (the only vendor is `anthropic`), so all Phase 42–44 span behavior is byte-identical after this phase. The seam and its contract test exist now so double-emission isn't discovered for the first time when the LangGraph beachhead milestone lands.

**Requirements:** ADAPT-01 (ROADMAP.md Phase 45 section, 3 success criteria: flag-as-data never per-runtime branch; reporter skips synthesis when set; stub contract test proves zero duplicates).

**Explicitly NOT this phase:** the actual LangGraph adapter or any second runtime (vNext milestone — REQUIREMENTS.md "LangGraph native-emission flag-flip" is explicitly future); manager-side trace-only spawn-gating on the capability flag (see Deferred Ideas); Phase 46's enrichment (sampler, session.id, metadata/tags, deep link); Phase 47's Phoenix install + live proof; any change to span shapes, redaction, or size bounds (Phase 44's D-01..D-12 stand as shipped).

</domain>

<decisions>
## Implementation Decisions

### Capability-flag data model (research-recommended shape, auto-selected)
- **D-01:** The capability lookup is **new `pkg/dispatch/vendor_capabilities.go`** — a data-shaped `SelfInstruments(vendor string) bool` lookup ("a handful of lines, not a new package" per research ARCHITECTURE component table). The manager computes it from the same resolved `ProviderSpec.Vendor` that `ResolveProvider` (`internal/controller/dispatch_helpers.go:271`) already produces. No per-runtime `if vendor == ...` branch appears in any reporter or controller call site — call sites consult the lookup and carry the boolean.
- **D-02 (trust posture):** The flag is **manager-computed and travels on the reporter Job spec (manager-controlled), never derived from pod-writable data**. `in.json` on the PVC carries `Provider.Vendor` but is writable by the semi-trusted subagent pod — the reporter must not trust it for a skip decision (research SUMMARY step 5: "without trusting the semi-trusted subagent pod to self-report"). The Job args/env are the tamper-proof channel.
- **D-03 (default-safe polarity):** Unknown vendor, absent table entry, or absent flag **defaults to "synthesize"** — a false "native" assumption silently produces zero spans, while a false "synthesize" produces (worst-case, and only once a self-instrumenting runtime exists) duplicates that are at least visible. Pitfall 7's warning sign verbatim: "an unset flag should default to 'synthesize,' never to 'assume native.'"

### Flag transport and skip point
- **D-04:** The flag travels as a **new `ReporterOptions` field → CLI arg on the reporter Job** (matching `reporter_jobspec.go`'s 100% Args-based convention; `TraceParent` precedent). Research's suggested name is `--emit-message-spans=<bool>`; exact name/polarity is planner's discretion within D-03's default-safe rule (absent flag must mean synthesize). Wire it at both builder consumers: `spawnReporterIfNeeded` (combined/materialization shape, 4 planner levels) and `spawnTaskTraceReporterIfNeeded` (trace-only shape, Task success + failure paths at `task_controller.go:1124/1153`).
- **D-05 (single skip point):** **The reporter's `synthesizeSpans` (`cmd/tide-reporter/main.go:316`) is the SOLE skip point** — it returns before `ReconstructConversation` when the flag says skip (no sentinel write; the sentinel dedupes emission, and a skipped run emits nothing). Pitfall 7: "keep the self-instrumenting capability flag as a single source of truth the reporter reads at parse time... so there's no ambiguity window where both paths fire." No heuristic detection ("does events.jsonl look OpenInference-shaped") — ever. Combined-mode planner reporters still run materialization unconditionally; the flag disables only the synth step, uniformly for both Job shapes.
- **D-06 (no spawn-gating this phase):** The manager **still spawns trace-only reporter Jobs for self-instrumenting vendors** (zero such vendors exist today, so this is not real churn). Skipping the spawn is a data-driven optimization with real payoff only when LangGraph lands — deferred to that milestone so today's behavior stays byte-identical and the skip logic lives in exactly one place (D-05).

### Adapter seam shape
- **D-07:** Per research Pattern 4, **the seam is the capability flag + the W3C `traceparent` env contract — NOT a Go interface**. A future self-instrumenting `Subagent` implementation does ordinary live instrumentation inside its own synchronous `Run()` (tracer.Start/defer End — the "held-open spans" reconcile constraint doesn't apply in-pod); it needs no synthesis hook to implement. Do NOT extract a `TraceSynthesizer` interface, do NOT move `tracesynth.go` into `internal/subagent/anthropic/` — it stays where it is, import-safe, as the anthropic-CLI runtime's adapter implementation.
- **D-08 (legibility):** Update the doc contracts so the seam is discoverable: `tracesynth.go`'s package comment states it is the anthropic-CLI runtime's trace adapter (it parses that runtime's `events.jsonl` format) and names `pkg/dispatch.SelfInstruments` as the routing datum; `vendor_capabilities.go` documents the default-safe rule (D-03) and the trust rationale (D-02). ADAPT-01's "per-runtime adapter behind the Subagent seam" is satisfied by adapter = tracesynth (per-runtime parse logic), routing = capability data — a second runtime slots in without touching any TIDE call site.

### Contract-test strategy
- **D-09:** One in-process contract test (house convention: `tracetest.SpanRecorder`, plain Go test — envtest not required for the contract itself) with a **stub self-instrumenting runtime** proving BOTH directions:
  - **(a) env-carrier extraction:** a synthetic `TRACEPARENT` env value is extracted via the W3C propagator and becomes the active context BEFORE any span starts — the stub's own span parents under the injected trace/span IDs. Generic env-carrier mechanics only; zero LangGraph span-shape assertions (research §137: the real span tree is the LangGraph milestone's research question, not guessable now).
  - **(b) zero duplicates:** with a **valid, real-shaped `events.jsonl` present on disk** and the self-instrumenting flag set, the reporter path emits ZERO synthesized spans — the recorded span set is exactly the stub's own spans. This is the "no double-emission path exists" proof.
- **D-10 (default-safe direction pinned):** The test suite also pins the inverse: flag absent / vendor unknown → synthesis proceeds (existing Phase 44 tests already cover synthesis mechanics; add the explicit unknown-vendor/absent-flag case so D-03's polarity can't silently invert). `SelfInstruments("anthropic") == false` is asserted directly.
- **D-11 (spawn-site coverage):** A cheap unit assertion on `BuildReporterJob` (and the two spawn helpers if planner judges it worthwhile) that the flag rides the Job args as computed from the vendor lookup — data flowing end-to-end, no branch.

### Claude's Discretion
- Exact flag name and polarity encoding (`--emit-message-spans` vs `--skip-synthesis` etc.), within D-03's absent-means-synthesize rule.
- `SelfInstruments` shape: bare func over a package-level table vs a tiny `Capabilities` struct — keep to one bit unless the planner finds a forced second capability; how the stub vendor is injected for tests (test-local table, injectable lookup var, or exported test hook) without polluting the production table.
- Whether `ReporterOptions` carries the boolean or the vendor string (boolean recommended — keeps the lookup at one manager-side site).
- Placement/wording of the D-08 doc-contract updates; whether `pkg/otelai/doc.go` or `attrs.go:232`'s existing "ResolveProvider(...).Vendor, never a hardcoded constant" comment gets cross-referenced.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.0.8 Phoenix Rising — this phase is research "Phase 4", HIGH confidence, no dedicated research pass flagged)
- `.planning/research/SUMMARY.md` — §"Phase 4: Self-Instrumenting Runtime Adapter Seam" (sequencing rationale, zero-behavioral-effect framing); Suggested Build Order step 5/6 (`pkg/dispatch/vendor_capabilities.go`, `--emit-message-spans` wiring); §confidence (line ~115): LangGraph span-tree shape explicitly NOT verified — contract test must be generic
- `.planning/research/ARCHITECTURE.md` — **Pattern 4 (self-instrumenting-runtime adapter seam = vendor capability flag, NOT a Go interface call — the D-07 lock)**; component table rows for `pkg/dispatch` (capability lookup) and `cmd/tide-reporter`/`internal/reporter` (skip when self-instrumenting); diagram line ~29 (`--emit-message-spans=<self-instrument capability>` on the reporter spawn)
- `.planning/research/PITFALLS.md` — **Pitfall 7 (double-instrumentation: both failure directions, the default-safe rule, single-source-of-truth-at-parse-time, and the stub-runtime env-carrier test recommendation — D-03/D-05/D-09 all trace here)**; Pitfall 7's env-carrier caveat: auto-instrumentors do NOT read `TRACEPARENT` env for free — extraction is explicit adapter code (why criterion 3 tests exactly this)

### Requirements and constraints
- `.planning/REQUIREMENTS.md` §"Runtime-Neutral Adapter Seam (ADAPT)" — ADAPT-01 exact text; §Future Requirements "LangGraph native-emission flag-flip" (the seam ships now, activation waits — scope fence)
- `.planning/ROADMAP.md` §"Phase 45: Runtime-Neutral Adapter Seam" — goal, depends-on (Phase 44), 3 success criteria
- `.planning/PROJECT.md` §"Runtime-neutrality constraints (2026-07-15)" — the milestone-level lock this phase implements clause (2) of: "the events.jsonl parser is a runtime ADAPTER behind the Subagent seam with a self-instrumenting capability flag (reporter skips synthesis for runtimes that emit natively — no double spans)"
- `.planning/STATE.md` §"v1.0.8 binding constraints" — runtime-neutrality summary; observability-never-gates posture

### Prior phase context (decisions this phase composes with)
- `.planning/phases/44-llm-message-array-spans-d-o5-redaction-size-boundary/44-CONTEXT.md` — D-06 (OTLP spawn-gating the capability flag composes with), D-10 (best-effort exit 0 — the skip path must preserve it), D-02 (all-five-level coverage: the flag must reach BOTH reporter Job shapes)
- `.planning/phases/42-trace-context-foundation-planner-level-span-emission/42-CONTEXT.md` — D-07 (provider identity derived from dispatch data, never hardcoded — the same principle D-01 extends to capabilities)

### Existing code (surfaces this phase touches)
- `internal/reporter/tracesynth.go` — `ReconstructConversation(eventsPath, inJSONPath, workspaceRoot)` + `EmitSpans(ctx, tracer, calls, artifactPath)`: the synthesizer being wrapped; stays in place per D-07, gains the D-08 doc contract
- `cmd/tide-reporter/main.go` — `parseFlags` (line ~113, gains the new flag), `synthesizeSpans` (line ~316, the D-05 single skip point, called from both the trace-only branch and the combined-mode path), sentinel mechanics (skip path writes no sentinel)
- `internal/controller/reporter_jobspec.go` — `ReporterOptions` (line 74: `TraceParent`/`OTLPEndpoint`/`TraceOnly`/`TraceOnlyJobKey` precedents for how data rides Args vs Env) + `BuildReporterJob` arg assembly (trace-only vs materialization shapes)
- `internal/controller/dispatch_helpers.go` — `ResolveProvider` (line ~271: Vendor pinned "anthropic", the flag's data source) + `spawnReporterIfNeeded` (line 93: combined-shape spawn, 4 planner levels)
- `internal/controller/task_controller.go` — `spawnTaskTraceReporterIfNeeded` (line ~1058; called at 1124/1153 for success+failure) — the trace-only spawn gaining the flag
- `pkg/dispatch/provider.go` — `ProviderSpec.Vendor` (line 40: "the provider sentinel string the subagent image checks at startup") — the vendor vocabulary `vendor_capabilities.go` keys on; `vendor_capabilities.go` lands beside it
- `internal/subagent/anthropic/subagent.go` — `vendorSentinel` fail-fast firewall (line ~218): the existing compile-time vendor-string agreement the capability table joins
- `pkg/otelai/tracecontext.go` — `ExtractRemoteParent` (the env-carrier extraction primitive the D-09 contract test exercises)
- `internal/controller/task_traceonly_reporter_test.go` + `internal/reporter/tracesynth_test.go` + `cmd/tide-reporter/main_test.go` — existing test conventions to mirror (and the Phase 44 suites that must stay green unchanged — the behavior-unchanged proof)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `ReporterOptions`/`BuildReporterJob` already model exactly how per-spawn data rides to the reporter (Args-based convention with documented Env exception for `OTLPEndpoint`) — the new flag is one more field + arg, no new mechanism.
- `ResolveProvider` is the single manager-side site where `Vendor` is resolved — the natural place the capability lookup keys off; both spawn helpers already receive its output context.
- `otelai.ExtractRemoteParent` + `tracetest.SpanRecorder` conventions (used across `span_emission_test.go`, `tracesynth_test.go`) give the D-09 contract test its mechanics for free.
- `internal/reporter/testdata/` real-shaped `events.jsonl` fixtures from Phase 44 — reuse for the "valid events.jsonl present, yet zero synthesized spans" assertion.

### Established Patterns
- **Provider identity derived from dispatch data, never hardcoded** (Phase 42 D-07 replaced the `llmSystem = "anthropic"` constant) — D-01 is the same move for capabilities.
- **Observability never gates** (Phase 44 D-10, exit-0 posture) — the skip path is trivially compatible (skipping cannot fail), but the new flag parse must not introduce a new exit-code class.
- **Data-not-code across the pod boundary** — `vendorSentinel` fail-fast + envelope `Provider.Vendor` are compile-time string agreements; the capability table extends this vocabulary rather than inventing a parallel one.
- **Guard tests as house style** — D-10's default-polarity pin is this phase's guard-shaped test.

### Integration Points
- `cmd/tide-reporter/main.go` flag surface + `synthesizeSpans` early-return (the phase's core reporter-side change).
- `ReporterOptions` → both spawn helpers (`spawnReporterIfNeeded`, `spawnTaskTraceReporterIfNeeded`) — the manager-side wiring, computed once from the vendor lookup.
- `pkg/dispatch/vendor_capabilities.go` (new) beside `provider.go` — no new package, no new deps.

</code_context>

<specifics>
## Specific Ideas

No user-specific vision requests — this phase was auto-discussed (`--auto`); all four gray areas resolved to the milestone research's explicit recommendations (research designed this seam at code level: file name, function shape, flag wiring, pitfall posture, and test strategy). The one framing clarification worth flagging to the planner: ROADMAP's "per-runtime adapter behind the Subagent seam" phrasing could read as a Go-interface extraction — research Pattern 4 explicitly rejects that shape (D-07); the requirement's own colon-clause defines the operational meaning (flag-as-data + reporter skip + contract test), and that is the locked interpretation.

</specifics>

<deferred>
## Deferred Ideas

- **Manager-side trace-only spawn-gating on the capability flag** (skip spawning the trace-only reporter Job entirely for self-instrumenting vendors — zero Job churn, extends Phase 44 D-06's posture) — real payoff only when a self-instrumenting runtime exists; belongs in the LangGraph beachhead milestone alongside the flag-flip activation (REQUIREMENTS.md Future Requirements).
- **The actual LangGraph adapter + native-emission activation** — vNext milestone by explicit requirement; its research owns the real `openinference-instrumentation-langchain` span-tree shape.

### Reviewed Todos (not folded)
Same four keyword matches as Phases 42–44, carried forward with the dispositions locked there (reviewed 2026-07-15/16; reasoning applies identically — no tracing/adapter overlap):
- `2026-07-03-signed-commits-verified-badge.md` — git-identity/GPG scope, keyword false-positive.
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` — W-2 dispatch-gate ordering concern, next-milestone candidate.
- `2026-07-12-task-dispatch-gate-order-divergence.md` — W-2 sibling, same disposition.
- `cache-f1-direct-sdk-cross-pod-caching.md` — deferred vNext+, no overlap.

</deferred>

---

*Phase: 45-Runtime-Neutral Adapter Seam*
*Context gathered: 2026-07-16*
