# Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary - Context

**Gathered:** 2026-07-16
**Status:** Ready for planning

<domain>
## Phase Boundary

The reporter gains a trace-only mode (no child-CR materialization) that reads a completed Job's `events.jsonl` from the PVC and emits redacted, size-bounded OpenInference `LLMInputMessages`/`LLMOutputMessages` spans (`openinference.span.kind=LLM`, nested under the level's AGENT dispatch span) — plus `tide-reporter`'s first TracerProvider call site with shutdown-on-every-exit-path flush discipline (TRACE-03). New synthesizer lives in `internal/reporter/tracesynth.go` (separate from `materialize.go`, per research ARCHITECTURE).

**Requirements:** MSG-01 (trace-only reporter mode reading `events.jsonl`), MSG-02 (every message passes `internal/harness/redact.SecretPatterns` before emission), MSG-03 (size-guarded under the OTLP 4 MB ceiling, truncation markers + `ArtifactPath` co-attribute, guard test updated deliberately), TRACE-03 (deferred Shutdown on every reporter exit path).

**Coverage extension (D-02):** the user chose all-five-level coverage — planner-level reporter runs also synthesize message spans, not just Task. MSG-01's formal phase gate still binds the Task level; the wider coverage is implementation scope, not new gate criteria.

**Explicitly NOT this phase:** Phase 43's territory (Task AGENT dispatch spans, `traceparent` injection into Job/reporter env, `.status.trace` persistence, tree parenting) — Phase 43 is not yet planned; 44's plans must consume its outputs as a dependency, not re-implement them. Phase 45's adapter seam (self-instrumenting capability flag) — 44 builds the synthesizer 45 wraps. Phase 46's enrichment (sampler default, session.id, metadata/tags, deep link).

</domain>

<decisions>
## Implementation Decisions

### Span granularity
- **D-01:** **One LLM span per API call** (`message_start`..`message_stop` cycle — ~32 for the largest real fixture). `input_messages` = the conversation context at that call; `output_messages` = that call's assistant turn. This mirrors what `openinference-instrumentation-langchain` emits natively, so Phoenix queries survive the LangGraph migration (runtime-neutrality lock). Accepted cost: input context repeats across calls (~quadratic total payload) — which is exactly why D-07's size-model research is load-bearing. If research finds this model untenable at real fixture sizes, it must surface that BEFORE planning locks it (see D-07).
- **D-02 (coverage):** **All five levels emit message spans.** Planner-level reporter runs (materialization mode) also call the synthesizer; LLM spans nest under that level's AGENT span. Trace-only spawns exist only where no materialization run happens: Task completions, and failed Jobs at any level.
- **D-03 (non-text content):** **Spec encoding if the module has it** — research checks whether the pinned `openinference-semantic-conventions` v0.1.1 module carries `message.tool_calls`/tool-call keys. If yes, tool calls get spec-native encoding; if no, fall back to stringified-into-content. Thinking blocks excluded either way unless the module names them. (Phase 42 D-05 policy applied to a new surface.)
- **D-04 (token counts):** **Per-call token counts on each LLM span** from the stream's `message_delta` usage, via the existing `TokenCount` helper (Phase 42 D-08 pre-sum semantics). Research must verify how Phoenix aggregates trace/session cost: the Task AGENT span (Phase 43) will carry totals, so if Phoenix sums ALL spans, per-call + totals double-count — research pins which level keeps counts.

### Level coverage operational rules
- **D-05 (failure parity):** Failed Jobs at every level get a trace-only reporter spawn (extends Phase 42 D-01). The synthesizer tolerates a truncated/partial `events.jsonl` (mid-stream kill) and emits what it can with a degraded-marker attribute (D-04-style). A failed Task's conversation is the highest-value debugging trace.
- **D-06 (spawn gating):** The manager **skips trace-only reporter spawns when no OTLP endpoint is configured** (same value it forwards into Job env). Zero Job churn on plain clusters; materialization-mode runs are unaffected (their synth step no-ops naturally via the no-op provider).

### Size boundary (D-O5 resolution)
- **D-07 (RESEARCH-DEFERRED, model included):** User: "I think we might be modeling this wrong." Research must validate the size-bounding **model**, not just pick constants: per-message cap, whole-span budget, BatchSpanProcessor/exporter batch-size math — or a different shape entirely if per-call full-input-context repetition is untenable at real sizes. Constants come from the real dogfood fixtures (`examples/projects/dogfood/salvage-20260618/` — largest raw file 2.5 MB / 7,476 lines / ~32 API calls). Any finding that ripples back into D-01's granularity model must be surfaced before planning locks it. MSG-03's requirement text is the floor: per-message truncation with explicit markers + `ArtifactPath(events.jsonl)` co-attribute on the same span, documented threshold, guard test updated deliberately.
- **D-08 (truncation shape):** When truncation applies, **head+tail with middle elision** — keep the first X and last Y bytes with the marker between. Conversations carry signal at both ends (instructions up top, errors/conclusions at the bottom).
- **D-09 (ordering, locked):** **Redaction runs BEFORE truncation.** Truncating first can split a secret across the cut so the pattern no longer matches, leaving a partial credential visible. Not negotiable.

### Failure posture
- **D-10 (exit codes):** **Best-effort exit 0 everywhere.** Synth/export failures log to stderr but never fail any reporter run — combined runs report materialization's outcome alone; trace-only runs exit 0 regardless. Extends the "observability must not gate" precedent (Phase 42 D-04); no new exit-code class; avoids the duplicate-span retry problem (reporter-synthesized span IDs mint fresh per run, so Job retries would re-emit the conversation as duplicate spans). Accepted cost: a broken tracing pipeline is visible only in pod logs.
- **D-11 (parse strictness):** **Tolerant-skip with marker.** Unparseable/unexpected lines are skipped; the synthesizer emits whatever conversation it reconstructs, stamped with a degraded-marker attribute. Matches `ParseStream`'s existing defensive posture and is required by D-05's failure parity.
- **D-12 (flush bound):** **Bounded flush timeout** — `Shutdown` runs under a context deadline (constant picked in planning, order of seconds). A dead collector delays reporter exit by at most that bound; the drop is logged. A hung external dependency can never wedge the pipeline.

### Claude's Discretion
- **Span timing when `events.jsonl` carries no per-event timestamps** (whether it does is a research fact): planner/researcher picks whatever composes best with the schema findings — marked-synthetic (a `tide.*` attribute flagging non-measured timing) is the floor. "Real if present, else proportional across the Job window" was the presented prior.
- Trace-only mode's invocation surface (flag vs env var on the reporter), where the non-streaming redaction helper lives (research suggested `redact.String` reusing `SecretPatterns`), the exact guard-test update mechanics (research prior: `attrs.go` unchanged, bounding lives in `tracesynth.go`), LLM span naming, and the flush-timeout constant.
- RBAC/chart deltas implied by the wider reporter role (trace-only spawns need no CR-write perms — planner may split or reuse the SA as fits).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.0.8 Phoenix Rising)
- `.planning/research/SUMMARY.md` — §"Phase 3: LLM Message-Array Spans" (this phase's research shape); the three load-bearing design decisions; open-questions list (events.jsonl schema + byte threshold explicitly unresolved)
- `.planning/research/ARCHITECTURE.md` — Pattern 3 (reporter trace-only extension), Pattern 5 (D-O5 boundary: inline redacted + ArtifactPath co-attribute), `tracesynth.go` file placement rationale, span-kind=LLM encoding notes, Suggested Build Order step 5
- `.planning/research/PITFALLS.md` — Pitfall 1 (unredacted `events.jsonl` → Phoenix), Pitfall 2 (OTLP 4 MB ceiling, one fat span drops the whole batch), Pitfall 4 (unflushed short-lived Job drops spans)

### Requirements and constraints
- `.planning/REQUIREMENTS.md` — MSG-01/02/03 + TRACE-03 exact text (this phase); ADAPT-01 (Phase 45 boundary — build the synthesizer, not the seam)
- `.planning/STATE.md` §"v1.0.8 binding constraints" — retroactive synthesis, deterministic TraceID, D-O5 as a real open call, events.jsonl deliberately raw at source
- `.planning/phases/42-trace-context-foundation-planner-level-span-emission/42-CONTEXT.md` — Phase 42's D-01..D-08 policies this phase extends (failure-path spans, `tide.*` namespace, module pin, provider-derived identity, token pre-sum semantics)

### Existing code (surfaces this phase touches)
- `pkg/otelai/attrs.go` + `pkg/otelai/doc.go` §"D-O5 — no payload inlining" — `LLMInputMessages`/`LLMOutputMessages`/`TokenCount`/`ArtifactPath` helpers and the D-O5 rule text this phase evolves
- `pkg/otelai/attrs_test.go:182` — `TestNoPayloadHelperOnPublicSurface`, the guard MSG-03 says to update deliberately, never delete
- `internal/harness/redact/redact.go` + `internal/harness/redact/patterns.go` — `SecretPatterns` (the mandated MSG-02 pass) and the streaming `RedactingWriter` (this phase likely needs a non-streaming string helper beside it)
- `internal/subagent/anthropic/stream_parser.go` + `internal/subagent/anthropic/subagent.go` — the `events.jsonl` producer: schema comments, tee mechanics, 16 MB per-line budget, `<WorkspaceRoot>/envelopes/<TaskUID>/events.jsonl` path convention
- `cmd/tide-reporter/main.go` — exit-code map (0/1/2), `run()`/`runWithClient()` seam (already structured for deferred shutdown inside `run`), flag surface gaining the trace-only mode
- `internal/reporter/materialize.go` — the materialization logic the combined run keeps orthogonal to `tracesynth.go`
- `internal/otelinit/provider.go` — env-driven TracerProvider the reporter gains its first call site of; no-op-without-endpoint posture D-06 leans on

### Real fixture data (research MUST use these)
- `examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/*/events.jsonl` — 20+ real multi-turn files (largest 2.5 MB / 7,476 lines / ~32 API calls / 65 assistant + 53 tool_result messages; bulk is `stream_event` deltas, complete messages in top-level `assistant`/`user` events). The roadmap's research flag — exact multi-turn schema + concrete byte threshold — resolves against these, not the `stream_parser.go` comment alone.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/otelai` message helpers (`LLMInputMessages`/`LLMOutputMessages`) — built for exactly this, zero production call sites; this phase creates the first callers. `TokenCount` carries D-08 pre-sum semantics; `ArtifactPath` is the co-attribute MSG-03 mandates.
- `internal/harness/redact.SecretPatterns` — the compiled six-pattern denylist; MSG-02 requires it as the mandatory pass. The streaming `RedactingWriter` shape doesn't fit per-message string redaction — a small non-streaming helper in the same package is the research-suggested shape.
- `internal/otelinit.NewTracerProvider` — env-driven, no-op without endpoint; the reporter gains its first call site (TRACE-03), and D-06's spawn-gating reads the same config the manager already forwards.
- Phase 42's `pkg/otelai/tracecontext.go` (`TraceIDFromUID`, `ExtractRemoteParent`) — the reporter extracts its parent context from the `traceparent` env Phase 43 injects.

### Established Patterns
- **Defensive line-parsing** (`ParseStream` tolerates non-JSON lines) — D-11 extends this posture to the read side.
- **Observability must not gate** (Phase 42 D-04, envelope-degraded marker) — D-05/D-10/D-11 all extend it: degraded spans over missing spans, exit 0 over Job churn.
- **Guard/ratchet tests as house style** — `TestNoPayloadHelperOnPublicSurface` gets a deliberate update reflecting the bounded-payload surface; the D-O5 doc.go text evolves from "prefer ArtifactPath" to "bounded-inline + ArtifactPath co-attribute."
- **`os.Exit(run(...))` seam in `cmd/tide-reporter`** — main already routes through a testable `run()`; TRACE-03's deferred Shutdown slots inside `run()` so every exit path flushes.

### Integration Points
- `cmd/tide-reporter/main.go` flag surface + `internal/reporter/tracesynth.go` (new) — the phase's core.
- Manager-side spawn sites: Task completion (new trace-only spawn), failed-Job completions at all levels (new trace-only spawn), existing planner materialization spawns (gain the synth step). Exact wiring depends on Phase 43's reporter-env `traceparent` work — 43 is not yet planned, so 44's planner must treat the propagation contract (`traceparent` in reporter Job env, `.status.trace` fields) as an upstream dependency.
- `BuildReporterJob`/reporter Job builder — gains `OTEL_EXPORTER_OTLP_ENDPOINT` forwarding (research Build Order step 5).

</code_context>

<specifics>
## Specific Ideas

- The user challenged the size-bounding framing directly ("why aren't messages being sent more often and aggregated with a trace_id?" → "I think we might be modeling this wrong") and chose to defer the model itself to research rather than lock either cap shape. Research owns the model, with explicit license to overturn the per-call full-context assumption if fixture data says so — do not treat D-07 as "pick a number for the both-caps design."
- All-five-level coverage (D-02) was a deliberate user extension past MSG-01's Task-only letter — richest Phoenix picture in one milestone, accepted as added blast radius on the highest-risk phase. Keep the formal gate bound to Task.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope (the one boundary extension, all-level coverage, was folded in as D-02 rather than deferred).

### Reviewed Todos (not folded)
Same four keyword matches as Phase 42, carried forward with Phase 42's dispositions (reviewed there 2026-07-15; reasoning applies identically — no tracing overlap):
- `2026-07-03-signed-commits-verified-badge.md` — git-identity scope, keyword false-positive.
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` — W-2 dispatch-gate concern, next-milestone candidate.
- `2026-07-12-task-dispatch-gate-order-divergence.md` — W-2 sibling, same disposition.
- `cache-f1-direct-sdk-cross-pod-caching.md` — deferred vNext+, no overlap.

</deferred>

---

*Phase: 44-LLM Message-Array Spans + D-O5 Redaction/Size Boundary*
*Context gathered: 2026-07-16*
