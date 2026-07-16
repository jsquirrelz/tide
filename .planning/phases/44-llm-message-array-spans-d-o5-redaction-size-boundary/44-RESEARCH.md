# Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary - Research

**Researched:** 2026-07-16
**Domain:** OpenInference LLM-kind span synthesis from a raw Claude Code `stream-json` audit log, redaction, and OTLP size-boundary engineering
**Confidence:** HIGH (all load-bearing claims verified directly against 58 real fixture files, the vendored `go.opentelemetry.io/otel` v1.43.0 SDK source, and the vendored `openinference-semantic-conventions` v0.1.1 Go module source — no claim in the Standard Stack / Architecture / Size-Boundary sections rests on training-data recall alone)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** One LLM span per API call (`message_start`..`message_stop` cycle — ~32 for the largest real fixture). `input_messages` = the conversation context at that call; `output_messages` = that call's assistant turn. Accepted cost: input context repeats across calls (~quadratic total payload) — which is exactly why D-07's size-model research is load-bearing. If research finds this model untenable at real fixture sizes, it must surface that BEFORE planning locks it.
- **D-02 (coverage):** All five levels emit message spans. Planner-level reporter runs (materialization mode) also call the synthesizer; LLM spans nest under that level's AGENT span. Trace-only spawns exist only where no materialization run happens: Task completions, and failed Jobs at any level.
- **D-03 (non-text content):** Spec encoding if the module has it — research checks whether the pinned `openinference-semantic-conventions` v0.1.1 module carries `message.tool_calls`/tool-call keys. If yes, tool calls get spec-native encoding; if no, fall back to stringified-into-content. Thinking blocks excluded either way unless the module names them.
- **D-04 (token counts):** Per-call token counts on each LLM span from the stream's `message_delta` usage, via the existing `TokenCount` helper. Research must verify how Phoenix aggregates trace/session cost: the Task AGENT span (Phase 43) will carry totals, so if Phoenix sums ALL spans, per-call + totals double-count — research pins which level keeps counts.
- **D-05 (failure parity):** Failed Jobs at every level get a trace-only reporter spawn. The synthesizer tolerates a truncated/partial `events.jsonl` (mid-stream kill) and emits what it can with a degraded-marker attribute.
- **D-06 (spawn gating):** The manager skips trace-only reporter spawns when no OTLP endpoint is configured (same value it forwards into Job env).
- **D-07 (RESEARCH-DEFERRED, model included):** Research must validate the size-bounding MODEL, not just pick constants: per-message cap, whole-span budget, BatchSpanProcessor/exporter batch-size math — or a different shape entirely if per-call full-input-context repetition is untenable at real sizes. Constants come from the real dogfood fixtures. Any finding that ripples back into D-01's granularity model must be surfaced before planning locks it. MSG-03's floor: per-message truncation with explicit markers + `ArtifactPath(events.jsonl)` co-attribute, documented threshold, guard test updated deliberately.
- **D-08 (truncation shape):** Head+tail with middle elision — keep the first X and last Y bytes with the marker between.
- **D-09 (ordering, locked):** Redaction runs BEFORE truncation. Not negotiable.
- **D-10 (exit codes):** Best-effort exit 0 everywhere. Synth/export failures log to stderr but never fail any reporter run.
- **D-11 (parse strictness):** Tolerant-skip with marker. Unparseable/unexpected lines are skipped; the synthesizer emits whatever conversation it reconstructs, stamped with a degraded-marker attribute.
- **D-12 (flush bound):** Bounded flush timeout — `Shutdown` runs under a context deadline (constant picked in planning, order of seconds).

### Claude's Discretion

- Span timing when `events.jsonl` carries no per-event timestamps (whether it does is a research fact) — marked-synthetic (a `tide.*` attribute flagging non-measured timing) is the floor.
- Trace-only mode's invocation surface (flag vs env var on the reporter), where the non-streaming redaction helper lives (research suggested `redact.String` reusing `SecretPatterns`), the exact guard-test update mechanics, LLM span naming, and the flush-timeout constant.
- RBAC/chart deltas implied by the wider reporter role.

### Deferred Ideas (OUT OF SCOPE)

None — discussion stayed within phase scope (the one boundary extension, all-level coverage, was folded in as D-02 rather than deferred).

Explicitly NOT this phase (Phase 43's territory): Task AGENT dispatch spans, `traceparent` injection into Job/reporter env, `.status.trace` persistence, tree parenting — Phase 43 is not yet planned; 44's plans must consume its outputs as a dependency. Phase 45's adapter seam. Phase 46's enrichment.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| MSG-01 | Executor (Task) level gains a trace-only reporter mode (no child-CR materialization) reading `events.jsonl` | Real schema fully reverse-engineered against 58 fixture files (see Architecture Patterns §Schema); confirms `ParseStream`'s raw tee captures full `stream-json` fidelity; identifies the `in.json`/`PromptPath` gap that must be closed for MSG-01 to produce a *complete* first-call context (see Pitfall "Missing call-1 prompt") |
| MSG-02 | `LLMInputMessages`/`LLMOutputMessages` populated only after `redact.SecretPatterns` pass | `internal/harness/redact` inspected directly — no non-streaming string helper exists yet; concrete `redact.String` shape specified in Code Examples; D-09 ordering (redact-before-truncate) validated against the streaming `RedactingWriter`'s own tail-keep-buffer rationale (same class of bug: truncating first can split a secret across the cut) |
| MSG-03 | Size-guarded under OTLP 4MB ceiling: per-message truncation + `ArtifactPath` co-attribute, documented threshold, guard test updated deliberately | Concrete byte thresholds derived from real per-message, per-call, and per-task size distributions across 1256 real API calls / 40 real Tasks (see Size-Boundary Model, the phase's central research deliverable) — validates D-01, locates the REAL bottleneck at BatchSpanProcessor batch aggregation (not per-span size), and gives an exact, zero-code-change mitigation (`OTEL_BSP_MAX_EXPORT_BATCH_SIZE`) |
| TRACE-03 | Every span-emitting one-shot binary carries TracerProvider construction AND deferred-Shutdown flush discipline | `cmd/tide-reporter/main.go` confirmed as the ONLY one-shot binary in the repo about to gain its first `otelinit.NewTracerProvider` call site (grepped: only `cmd/manager/main.go` calls it today); manager's existing 5-second bounded-Shutdown pattern read directly and reusable as D-12's constant |

</phase_requirements>

## Summary

Phase 44 is TIDE's highest-risk v1.0.8 phase for a concrete reason this research now has real data on: `events.jsonl`'s raw `stream-json` tee, reconstructed under D-01's locked one-span-per-API-call model, produces real per-Task payloads of 4–11 MB when every call's full input context is naively inlined — comfortably enough to blow the OTLP gRPC 4 MB ceiling. But the mechanism of that failure is *not* what Pitfall 2 originally framed ("one fat span"): across 1,256 real reconstructed API calls drawn from 40 real dogfood Tasks, **no single call's full input context ever exceeded ~334 KiB** — three orders of magnitude under any span-level ceiling. The actual risk is aggregate: a Task's ~32 (up to 56) LLM-kind spans, each individually modest, are queued and exported together by the SDK's `BatchSpanProcessor`, and 19 of 40 real Tasks (47.5%) would sum past 4 MB in that single export batch even with zero per-message truncation applied. This is exactly the failure mode the user's own framing ("why aren't messages being sent more often and aggregated with a trace_id") was pointing at — not "one message too big," but "too many small messages exported at once." The fix requires no architectural change to D-01: it is `OTEL_BSP_MAX_EXPORT_BATCH_SIZE`, an environment variable the vendored OTel Go SDK already reads automatically inside `NewBatchSpanProcessor` (confirmed by reading `sdk/trace/batch_span_processor.go` and `sdk/trace/internal/env/env.go` directly) — meaning `internal/otelinit.NewTracerProvider`'s existing `sdktrace.WithBatcher(exp)` call, unmodified, already honors it the moment the reporter's Job env sets it. D-01 stands **validated as-is**; the real fix lives in Job-env configuration, not span granularity.

Two further real-fixture findings change what "correct" looks like for this phase beyond size. First, `events.jsonl` never carries the *first* turn of a conversation — the system/task prompt that seeds call #1 is passed to the CLI out-of-band and is not in the stream tee at all (verified: byte 0 of every fixture file starts at `system.init`, never a `user`-role prompt). Every reconstructed conversation is systematically missing its own foundation unless the reporter also reads `in.json` (planner dispatches: `.prompt` field, already durable at the same PVC directory) or, for executor Tasks, follows `in.json`'s `PromptPath` to the `children/task-NN.json` artifact's `.spec.prompt` — both already durably written to the same PVC subPath the reporter already mounts, zero new I/O surface. Second, the pinned `openinference-semantic-conventions` v0.1.1 module — read directly from the vendored module cache — resolves D-03 conclusively: it DOES carry full tool-call attribute keys with ready-made indexer helpers (`LLMOutputMessageToolCallKey`, `MessageToolCalls`, `ToolCallFunctionName`/`Arguments`) AND a `message.contents`/`message_content.type=reasoning`+`signature` shape that is a direct structural match for Anthropic's `thinking` content block (verified against a real fixture's raw `thinking` block, which carries exactly the `signature` field the module names). Both should get spec-native encoding, not stringified fallback — this requires extending `pkg/otelai`'s `Message`/`flattenMessages` shape beyond its current flat two-key encoding, a real implementation task this phase must schedule, not a doc-only decision.

**Primary recommendation:** Keep D-01 exactly as locked (one span per API call). Add three independent, additive size/aggregation controls, none of which change span granularity: (1) a 32 KiB per-message head+tail truncation floor (MSG-03's mandated mechanism, positioned ~1 KiB above the real p99 single-turn size so it almost never fires on real content and exists purely as a pathological-input backstop); (2) a 512 KiB whole-span budget as a secondary backstop (real max observed is 334 KiB, so normal operation never trips it); (3) `OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6` set as a literal env var on the reporter Job's container spec — the actual fix for the real aggregate-batch risk, requiring zero Go code changes to `otelinit`. Separately, flag — do not silently resolve — a genuine Phoenix cost-double-counting risk this phase's own D-02 scope (all-five-level coverage) creates: Phoenix's documented cumulative trace/session/project token rollup sums every span's `llm.token_count.*` attributes, so once LLM children exist under an AGENT span that ALSO carries a pre-summed total (already shipped for 4 levels in Phase 42), the rollup double-counts. This crosses into Phase 42/43 territory and needs an explicit planner/discuss-phase decision, not a Phase-44-only fix.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `events.jsonl` parsing → conversation reconstruction | In-namespace reporter Job (`internal/reporter/tracesynth.go`) | — | Same trust/mount boundary as existing `MaterializeChildCRDs`; manager never mounts project PVCs (Pitfall 23 precedent) |
| Secret redaction of message content | In-namespace reporter Job (`internal/harness/redact`) | — | Reporter is the one place with both the raw payload and the outbound OTLP hop — same boundary shape as credproxy's env-var boundary |
| Size truncation / ArtifactPath fallback decision | In-namespace reporter Job (`tracesynth.go`) | `pkg/otelai` (attribute shape only) | Truncation is a call-site policy decision (which bytes to keep), not an attribute-encoding concern; `pkg/otelai` stays a thin, policy-free helper layer per its existing doc.go contract |
| TracerProvider construction + flush discipline | `cmd/tide-reporter/main.go` | `internal/otelinit` (unchanged constructor) | First one-shot-binary call site; `internal/otelinit.NewTracerProvider` itself needs zero changes — only the caller's shutdown discipline is new |
| BatchSpanProcessor batch-size tuning | Reporter Job env (`internal/controller/reporter_jobspec.go`) | — | SDK-internal, env-var-driven (`OTEL_BSP_MAX_EXPORT_BATCH_SIZE`) — zero Go code touches the OTel SDK's batching logic; this is Job-spec plumbing, same tier as the existing `OTEL_EXPORTER_OTLP_ENDPOINT` forwarding this phase already adds |
| Prompt-seed reconstruction (`in.json` / `PromptPath` read) | In-namespace reporter Job (`tracesynth.go`) | — | Same PVC, same subPath mount the reporter already has for `out.json`/`events.jsonl`; zero new mount/RBAC surface |
| Tool-call / thinking-block structured attribute encoding | `pkg/otelai` (new helper(s) alongside `LLMInputMessages`/`LLMOutputMessages`) | — | Spec-key resolution is exactly `pkg/otelai`'s existing job (ATTR-03 precedent); the reporter classifies content-block types and calls the right helper, mirroring the existing `AgentInvocation`/`TokenCount` split |

## Standard Stack

### Core

No new external dependencies this phase. Every library this phase needs is already pinned in `go.mod` from Phase 42:

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.opentelemetry.io/otel` (+ `/sdk`, `/trace`) | v1.43.0 (pinned, unchanged) | `trace.WithTimestamp`, `sdktrace.WithBatcher`, `BatchSpanProcessor` env-driven config | Already the project's OTel SDK; `NewBatchSpanProcessor`'s automatic `OTEL_BSP_*` env-var reads are the load-bearing mechanism this phase's size-boundary mitigation depends on — verified directly in `sdk/trace/batch_span_processor.go` and `sdk/trace/internal/env/env.go` |
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` | v0.1.1 (pinned, unchanged — D-06 declined a drift-guard, exact pin only) | Attribute-key constants + indexer helpers for messages, tool calls, and message-contents (reasoning blocks) | Confirmed via direct source read (`attributes.go`, `indexers.go`) to carry `MessageToolCalls`, `ToolCallFunctionName`, `ToolCallFunctionArgumentsJSON`, `LLMOutputMessageToolCallKey(i,j,child)`, `LLMInputMessageToolCallIDKey(i)`, AND `MessageContents`/`MessageContentType`(`"reasoning"`)/`MessageContentSignature` — this resolves D-03 with certainty, not inference |
| `internal/harness/redact` | in-repo, unchanged package, new function needed | Secret redaction before span emission (MSG-02) | `SecretPatterns` (6-pattern denylist) already exists and is exactly what MSG-02 mandates; only a new non-streaming `String(s string) string` helper is missing (the existing `RedactingWriter` is stream-oriented and doesn't fit per-message string redaction) |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `internal/otelinit` | in-repo, unchanged | `NewTracerProvider(ctx)` — env-driven, no-op without endpoint | `cmd/tide-reporter/main.go`'s first call site (TRACE-03); constructor itself needs zero changes |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Per-message + whole-span + batch-size triple guard (recommended) | Per-message truncation alone | Insufficient per real fixture data — bounds individual message size but does nothing about the aggregate-batch risk, which is the dominant real failure mode (19/40 real Tasks exceed 4MB in total naive payload while zero exceed it per-message) |
| `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` env tuning (recommended) | A custom span-batching/chunking layer in `tracesynth.go` | Reinvents SDK-native, env-var-driven functionality that already exists and is already how the manager's own `otelinit.NewTracerProvider` constructs its processor — no new code, no new bug surface |
| Reading `in.json`/`PromptPath` to seed conversation turn 0 (recommended) | Accepting call #1's context as genuinely empty | Ships a message-array feature whose very first (and often most instructive) turn is silently missing on every single Task — reads as a synthesis bug to anyone inspecting Phoenix, not a documented limitation |

**Installation:**

No `go get`/`npm install` needed — every dependency this phase uses is already in `go.mod` (pinned by Phase 42). This phase adds one new Go file (`internal/reporter/tracesynth.go`), extends `pkg/otelai` with new helper(s), and extends `internal/harness/redact` with one new function — no manifest changes.

**Version verification:**

```
$ grep -E "openinference-semantic-conventions|go.opentelemetry.io/otel " go.mod
	github.com/Arize-ai/openinference/go/openinference-semantic-conventions v0.1.1
	go.opentelemetry.io/otel v1.43.0
```
Confirmed identical to Phase 42's pin — no version drift, no re-verification against the upstream registry needed (D-06 already declined a drift-guard test; the exact pin is enforced by `go.sum` + PR review only).

## Package Legitimacy Audit

Not applicable — this phase installs zero new external packages. All libraries used (`go.opentelemetry.io/otel*`, `openinference-semantic-conventions`) were already vetted and pinned during Phase 42; this phase only adds new call sites against already-pinned versions.

## Architecture Patterns

### `events.jsonl` Schema — Verified Against 58 Real Fixture Files

`.planning/phases/44-.../` research parsed all 58 real `events.jsonl` files at `examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/*/events.jsonl` (the largest: `80621bfb-.../events.jsonl`, 7,476 lines / 2,611,497 bytes, matching the CONTEXT.md-cited fixture exactly). Verified line-type distribution for the largest file:

```
total lines 7476, bad json 0
stream_event  7323   (bulk: 7099 content_block_delta + 64 content_block_start/stop pairs
                       + 32 message_start + 32 message_delta + 32 message_stop)
assistant       65   (one event PER COMPLETED CONTENT BLOCK, NOT per API call — see below)
user            53   (tool_result messages; ALL carry a top-level "timestamp" field)
system          34   (CLI init/status noise; carries session_id, model, tool allowlist)
result           1   (terminal summary: total_cost_usd, aggregate usage, num_turns)
```

**Load-bearing structural finding — `assistant`-type events are per-content-block, not per-message:** a single API call's complete assistant turn is NOT one `assistant` JSON line. It is *multiple* `assistant` lines, each carrying exactly one content block (`thinking`, `tool_use`, or `text`), all sharing the same `message.id`, interleaved with `stream_event` deltas, bounded by one `message_start`..`message_stop` pair. Verified directly:

```
2  stream_event message_start          (msg_01XcAf..., new API call begins)
8  ASSISTANT  id=msg_01XcAf... content_types=['thinking']
22 ASSISTANT  id=msg_01XcAf... content_types=['tool_use']
37 ASSISTANT  id=msg_01XcAf... content_types=['tool_use']
40 stream_event message_stop            (same msg_01XcAf... — call complete)
41 USER  (tool_result for the first tool_use)
42 USER  (tool_result for the second tool_use)
44 stream_event message_start           (next API call begins)
```

**Reconstruction algorithm (verified against the corpus, not hypothesized):** walk the file in order; on `message_start`, snapshot the current `conversation` list as that call's `input_messages`; accumulate every subsequent `assistant`-type event's `content` blocks (same `message.id`) into a buffer; on `message_stop`, append the buffer as one `{role: assistant, content: [...]}` turn to `conversation` and record it as that call's `output_messages`; on each `user`-type event, append it directly to `conversation` (each carries a `timestamp`). 32 `message_start`/`message_stop` pairs in the largest fixture — an exact match for CONTEXT.md's "~32 API calls" figure.

**Timestamps — confirmed asymmetric, not absent:** `assistant`-type events carry NO `timestamp` field (0/65 in the largest fixture); `stream_event`-type events (including `message_start`/`message_stop`) carry NO `timestamp` field either — only `ttft_ms` (time-to-first-token, relative, not absolute). `user`-type events (tool results) DO carry an absolute `timestamp` (53/53 in the largest fixture, ISO-8601 `Z`-suffixed). This means: **span start/end times for a per-call LLM span cannot be derived from a real, in-band absolute timestamp for the call itself** — only the tool-result turns bracketing it have real clock data. Per Claude's Discretion on this point, the marked-synthetic floor from CONTEXT.md is the correct default: derive an approximate window by interpolating between the nearest preceding/following `user`-event timestamps (or, absent any, the Job's own `Status.{StartTime,CompletionTime}` divided proportionally across the call count), and stamp a `tide.trace.timing_synthetic=true`-style attribute (mirroring the existing `EnvelopeDegraded()` marker-attribute pattern) so Phoenix consumers can distinguish measured from interpolated timing.

**The missing call-1 prompt — a real correctness gap, not a corner case:** every one of the 58 fixture files begins with `type=system, subtype=init` (CLI init noise) and the FIRST `stream_event message_start` follows almost immediately — there is no `user`-role event anywhere before it carrying the actual task prompt. The system/task prompt that the CLI actually sent as call #1's input is passed out-of-band (stdin/CLI arg) and is never teed into `events.jsonl`. Reconstructing "the full input context at call 1" from `events.jsonl` alone yields the technically-correct-but-useless answer: **zero messages**. This is closeable, cheaply: `internal/dispatch/podjob/jobspec.go`'s init container already writes the FULL `EnvelopeIn` (which carries `.Prompt` verbatim for planner/materialization dispatches) to `/workspace/envelopes/{task-uid}/in.json` — same directory, same PVC, same subPath the reporter already mounts for `out.json`. For executor (Task) dispatches, `EnvelopeIn.Prompt` is empty and `EnvelopeIn.PromptPath` instead points at `envelopes/<plannerUID>/children/task-NN.json`, whose `.spec.prompt` field (per `api/v1alpha3/task_types.go:95-107`) is the real instruction text — a second, already-durable PVC read, same subPath. The reporter should read `in.json` first and seed conversation turn 0 from whichever of `.prompt` / `.promptPath→.spec.prompt` applies, BEFORE reconstructing the rest from `events.jsonl`. Zero new mounts, zero new RBAC — this is the same PVC subPath already granted.

### Size-Boundary Model (D-07) — Real-Fixture Validation

This is the phase's central research deliverable. All numbers below come from parsing all 58 real fixture files (40 of which reconstructed at least one complete API call) with the reconstruction algorithm above; every conversation "turn" size is `len(json.dumps(content_blocks))` — i.e. the actual attribute-value bytes `LLMInputMessages`/`LLMOutputMessages` would serialize, not the raw JSONL line bytes.

| Metric | n | max | p99 | p95 | p50 |
|--------|---|-----|-----|-----|-----|
| Single conversation-turn content size (one message body) | 3,217 turns | 61,169 B (~60 KiB) | 31,020 B | 16,065 B | 616 B |
| Per-call FULL input-context size (one LLM span's `LLMInputMessages` payload) | 1,256 calls | 341,838 B (~334 KiB) | 332,006 B | 281,155 B | 142,222 B |
| Per-call output size (one LLM span's `LLMOutputMessages` payload) | 1,256 calls | 61,169 B | 23,343 B | 7,829 B | 627 B |
| Per-Task TOTAL naive payload (sum of every call's full input+output — i.e. zero truncation, one export batch) | 40 tasks | 10,920,488 B (~10.4 MiB) | — | 8,638,426 B | 4,046,114 B |
| API calls per Task | 40 tasks | 56 | — | 54 | mean 31.4 |
| Max conversation turns inside any single call's context | 1,256 calls | 139 turns | — | — | — |

**Verdict on D-01 (span granularity): VALIDATED, not overturned.** The largest real per-call `LLMInputMessages` payload ever observed across 1,256 calls is 341,838 bytes — under 350 KiB, three orders of magnitude below the 4 MB OTLP ceiling, and this already reflects D-01's "worst case" (the LAST call in a long conversation, carrying the full accumulated history). No per-message truncation or ArtifactPath fallback would ever fire for input context on this real corpus at any threshold above ~350 KiB. **The real risk is aggregate, not per-span:** summing every call's naive payload for one Task (what a `BatchSpanProcessor` would attempt to export together if all of a Task's ~32 spans queue up and flush in one batch) reaches up to 10.9 MB, and **19 of 40 real Tasks (47.5%) exceed the 4 MB ceiling in this aggregate sense — with zero individual span ever approaching it.** This is the mechanism the user's challenge ("why aren't messages being sent more often and aggregated with a trace_id") was correctly intuiting: the danger is many small-but-nonzero spans landing in the same export request, not one oversized span.

**The fix requires zero architectural change and zero new Go code in `internal/otelinit`.** Read directly from the vendored SDK (`go.opentelemetry.io/otel/sdk@v1.43.0/trace/batch_span_processor.go`):

```go
// NewBatchSpanProcessor creates a new SpanProcessor...
func NewBatchSpanProcessor(exporter SpanExporter, options ...BatchSpanProcessorOption) SpanProcessor {
	maxQueueSize := env.BatchSpanProcessorMaxQueueSize(DefaultMaxQueueSize)
	maxExportBatchSize := env.BatchSpanProcessorMaxExportBatchSize(DefaultMaxExportBatchSize)
	// ...
}
```
and `sdk/trace/internal/env/env.go`:
```go
BatchSpanProcessorMaxQueueSizeKey       = "OTEL_BSP_MAX_QUEUE_SIZE"       // default 2048
BatchSpanProcessorMaxExportBatchSizeKey = "OTEL_BSP_MAX_EXPORT_BATCH_SIZE" // default 512
BatchSpanProcessorScheduleDelayKey      = "OTEL_BSP_SCHEDULE_DELAY"        // default 5000ms
BatchSpanProcessorExportTimeoutKey      = "OTEL_BSP_EXPORT_TIMEOUT"        // default 30000ms
```
`internal/otelinit.NewTracerProvider` calls `sdktrace.WithBatcher(exp)` with **no explicit `BatchSpanProcessorOption`s**, so these four env vars are the SOLE configuration surface today — confirmed by reading `WithBatcher`'s implementation (`WithBatcher(e, opts...) → NewBatchSpanProcessor(e, opts...)`). Setting `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` on the reporter container's env (a plain `corev1.EnvVar` addition in `BuildReporterJob`, the same file already gaining `OTEL_EXPORTER_OTLP_ENDPOINT` forwarding per the milestone's Build Order step 5) changes the reporter's batching behavior with **zero code changes to `otelinit` or any span-emission call site.** `ForceFlush`/`Shutdown` (which TRACE-03 wires up) drain the queue through the SAME `MaxExportBatchSize`-chunking code path as normal operation (verified: `processQueue`'s `case sd := <-bsp.queue` branch checks `len(bsp.batch) >= bsp.o.MaxExportBatchSize` on every enqueue, including the ones consumed while draining toward a `ForceFlush` marker) — so the mitigation holds even on the reporter's single terminal flush, not just steady-state.

**Concrete recommended constants (three independent, additive controls):**

1. **Per-message truncation floor (MSG-03's mandated mechanism, D-08 head+tail shape):** `32 KiB` (32,768 bytes) per individual message's content string. Positioned ~1 KiB above the real p99 single-turn size (31,020 B) — this threshold will almost never fire on real conversational content and exists specifically as a backstop against one pathological turn (e.g., a tool result that dumps an entire generated file). When it fires: keep the first ~16 KiB + last ~16 KiB with an explicit marker string between them (e.g. `[...N bytes truncated...]`), matching D-08 exactly, applied AFTER redaction per D-09.
2. **Whole-span budget (recommended secondary backstop, beyond MSG-03's literal per-message floor):** `512 KiB` for the SUM of one LLM span's `LLMInputMessages`+`LLMOutputMessages` attribute bytes. Real max observed is 334 KiB, so this will not trigger on the current corpus but bounds future growth (larger repos, more turns per call — 139 turns was the real observed max in one call's context) without requiring per-message truncation to somehow shrink an already-small aggregate. Above this cap: degrade to `ArtifactPath`-only for that side (input or output) with a `tide.*` marker attribute, rather than attempting to truncate dozens of already-small messages individually.
3. **`OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6`** set as a literal env var on the reporter Job's container spec (`BuildReporterJob`/`ReporterOptions`) — the actual fix for the confirmed real aggregate-batch risk. Math: 6 spans × 512 KiB whole-span cap = 3 MiB, leaving ~25% headroom under the 4 MiB ceiling for protobuf/gRPC framing overhead and resource attributes. Given real per-Task span counts (mean 31.4, max 56, per this corpus), this yields roughly 5–10 export RPCs per Task — negligible for a short-lived Job. No new Helm value needed (avoids touching the FIXED `values.yaml` contract, per CLAUDE.md) — this can be a hardcoded literal in `BuildReporterJob`'s env list, exactly like the existing hardcoded `TTLSecondsAfterFinished: 300`.

### D-03 Resolution — Tool Calls and Thinking Blocks Both Get Spec-Native Encoding

Read directly from the vendored module (`$(go env GOMODCACHE)/github.com/!arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1/attributes.go` + `indexers.go`):

```go
// attributes.go
MessageToolCalls              = "message.tool_calls"
MessageToolCallID             = "message.tool_call_id"
ToolCallID                    = "tool_call.id"
ToolCallFunctionName          = "tool_call.function.name"
ToolCallFunctionArgumentsJSON = "tool_call.function.arguments"
ToolCallReasoningSignature    = "tool_call.reasoning_signature"

MessageContents                = "message.contents"        // nested contents array
MessageContentType             = "message_content.type"    // "text"|"image"|"audio"|"reasoning"|"tool_use"
MessageContentText             = "message_content.text"
MessageContentSignature        = "message_content.signature" // "opaque provider reasoning-continuity fields verbatim"

// indexers.go
func LLMOutputMessageToolCallKey(i, j int, child string) string // "llm.output_messages.0.message.tool_calls.0.tool_call.function.name"
func LLMInputMessageToolCallIDKey(i int) string
```

Both halves of D-03 resolve to **yes, spec-native**:

- **Tool calls:** `message.tool_calls.<j>.tool_call.function.{name,arguments}` + `.id`, addressable via the module's own `LLMOutputMessageToolCallKey(i, j, child)` indexer. A real fixture's `tool_use` content block (`{"type":"tool_use","id":"toolu_...","name":"Bash","input":{...}}`) maps directly onto this shape.
- **Thinking blocks:** `message.contents.<k>.message_content.type="reasoning"` + `.text` (the thinking text) + `.signature` (the opaque continuity token) is a **structural match, not a guess** — verified against a real fixture's raw `thinking` block: `{"type":"thinking","thinking":"Let me read the project spec...","signature":"EpICCmUIDhgC..."}`. The module's own doc comment names exactly this use case ("MessageContentSignature... capture[s] opaque provider reasoning-continuity fields verbatim").

**Implementation consequence — a real finding that updates CONTEXT.md's stated prior:** `pkg/otelai/attrs.go`'s current `Message{Role, Content string}` + `flattenMessages` only emit the flat two-key `message.role`/`message.content` shape. Neither tool calls nor thinking blocks fit that shape without either (a) stringifying them into `Content` (the D-03 fallback CONTEXT.md described as the "no" branch — now confirmed NOT applicable) or (b) extending `pkg/otelai` with new helper(s)/struct fields that emit the nested `message.tool_calls.*` / `message.contents.*` keys via the module's own indexers. Since the module DOES have these keys, **(b) is the correct path** — this is a real new implementation task for this phase's plan (a new exported helper or an extended `Message` struct, following the exact ATTR-03 pattern already established: resolve every spec-family key from `semconv.*`, never hand-roll). It does not conflict with `TestNoPayloadHelperOnPublicSurface`'s forbidden-name list (`Payload`/`InlinePayload`/`RawContent`/`Body`/`MessageBody`) or `TestKeysUseSemconvModule`'s hand-rolled-literal guard — both guards remain satisfiable by a correctly-named, semconv-backed addition.

### Recommended `tracesynth.go` Shape

```go
// internal/reporter/tracesynth.go (new file; no internal/controller import,
// mirrors materialize.go's import-safety contract)

// ReconstructConversation walks events.jsonl (tolerant-skip per D-11) and
// returns one CallSpan per message_start..message_stop cycle, seeded with
// the in.json-derived prompt as conversation turn 0.
type CallSpan struct {
	InputMessages  []otelai.Message   // + structured tool-call/reasoning extensions
	OutputMessages []otelai.Message
	Usage          Usage              // per-call message_delta usage (D-04)
	Degraded       bool               // D-11 marker: parse gap or truncated stream
	TimingSynthetic bool              // Claude's Discretion: no real per-call timestamp
}

func ReconstructConversation(eventsPath, inJSONPath string) ([]CallSpan, error)

// EmitSpans creates one LLM-kind child span per CallSpan under the parented
// context extracted from --traceparent, applying redact.String (D-09: before
// truncation) then per-message + whole-span truncation (this doc's Size-
// Boundary Model) before calling otelai.LLMInputMessages/LLMOutputMessages.
func EmitSpans(ctx context.Context, tracer trace.Tracer, calls []CallSpan, eventsPath string) error
```

### Anti-Patterns to Avoid

- **Truncating before redacting (violates locked D-09):** exactly the same class of bug the streaming `RedactingWriter`'s tail-keep buffer already defends against for chunk boundaries — a secret pattern split across a truncation cut no longer matches the regex, so a partial credential survives into the span. Redact the FULL message string first, then truncate the redacted result.
- **Treating "per-message cap" as sufficient for MSG-03 without a batch-size control:** per-message truncation alone (with any reasonable threshold) does not touch the 47.5%-of-real-Tasks aggregate-batch risk this research measured — the risk is in how many spans get exported together, not any single message's size.
- **Building a custom batching/chunking layer inside `tracesynth.go`:** the OTel Go SDK's `BatchSpanProcessor` already does exactly this, already reads `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` from the environment automatically, and is already the processor `otelinit.NewTracerProvider` constructs. Reimplementing chunking in application code duplicates SDK behavior for no benefit.
- **Reconstructing "input context" from `events.jsonl` alone and treating an empty call-1 context as correct:** ships a message-array feature that is silently incomplete on every single Task from the very first call — read `in.json`/`PromptPath` first.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Bounding OTLP export batch size | A custom span queue/chunker in `tracesynth.go` or `cmd/tide-reporter` | `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` env var on the reporter Job container | SDK-native, already read automatically by `NewBatchSpanProcessor`; zero new code, zero new bug surface, verified present in the pinned v1.43.0 SDK |
| Secret redaction | A new regex/scrubbing implementation for message content | `internal/harness/redact.SecretPatterns` (existing 6-pattern denylist) via a new thin `redact.String(s string) string` wrapper | Already the project's single source of truth for secret patterns (HARN-04); a second implementation risks pattern drift between the stdout-redaction path and the trace-redaction path |
| Tool-call / reasoning-block attribute keys | Hand-rolled string keys or ad-hoc JSON-stringify-into-content | `openinference-semantic-conventions`'s `MessageToolCalls`/`ToolCallFunctionName`/`MessageContents`/`MessageContentType` constants + indexer helpers | Already vendored, already spec-exact, already what `openinference-instrumentation-langchain` will emit natively post-LangGraph migration (runtime-neutrality lock) |
| Trace-context extraction from `--traceparent` | A custom W3C traceparent parser | `propagation.TraceContext{}.Extract` (already used by Phase 42/43's `pkg/otelai/tracecontext.go`) | Already built, already unit-tested, standard library-grade primitive |

**Key insight:** every "hard part" of this phase (batching, redaction denylist, spec attribute keys, trace-context parsing) already exists somewhere in the codebase or the pinned SDK. The actual new work is narrow: parse the real schema correctly (this document's Architecture Patterns section), pick real thresholds (this document's Size-Boundary Model), and wire the pieces together in one new file plus a handful of new `pkg/otelai` helpers.

## Common Pitfalls

### Pitfall 1: Assuming `assistant`-type events are one-per-call (they are one-per-content-block)

**What goes wrong:** A naive reconstruction that treats every top-level `assistant` JSONL event as a complete message will silently drop content — e.g., only keep the LAST content block per call (overwriting `thinking`/first `tool_use` blocks) or emit N separate (incomplete) output messages per call instead of one complete one.
**Why it happens:** The CLI's `stream-json` tee is easy to misread as "one JSON object per logical message" by analogy with the Anthropic Messages API's own request/response shape — the intermediate CLI representation is finer-grained.
**How to avoid:** Group by shared `message.id` between `message_start` and the following `message_stop`; concatenate `content` arrays in event order.
**Warning signs:** Reconstructed output messages missing `thinking` blocks or missing all but the last tool call in a multi-tool-use turn.

### Pitfall 2: Missing the call-1 prompt gap

**What goes wrong:** Every Task's message-array trace shows an empty or near-empty `input_messages` for its very first LLM span, because `events.jsonl` genuinely never contains the seed prompt.
**Why it happens:** The seed prompt is CLI-injected out-of-band (stdin/arg), not stream-tee'd; this is easy to miss because the FIRST `message_start`'s usage block (`cache_creation_input_tokens`, etc.) looks like real accounted-for context, giving false confidence the reconstruction is complete.
**How to avoid:** Read `in.json` (same PVC directory) first; follow `PromptPath` one hop for executor Tasks.
**Warning signs:** A live Phoenix trace where call #1's `LLMInputMessages` array is empty or trivially short while later calls show hundreds of KB of context.

### Pitfall 3: Per-message truncation alone "looks done" but doesn't fix the real risk

**What goes wrong:** A plan that implements MSG-03's per-message cap, verifies with a synthetic oversized single message, and calls the OTLP-ceiling risk closed — while the REAL risk (aggregate batch export across ~32 modest spans) remains untouched and unverified.
**Why it happens:** Pitfall 2 in `.planning/research/PITFALLS.md` was framed (before this phase's fixture analysis) as "one fat span drops a whole batch" — true in principle, but this phase's real-data pass found it essentially never happens on real content (max observed per-span payload: 334 KiB). The aggregate-batch mechanism is a DIFFERENT, unverified-until-now failure mode.
**How to avoid:** Verify against a synthetic Task with ~32+ real-sized (not oversized) spans and assert the actual number of OTLP export RPCs / the max bytes per RPC, not just a single-oversized-message test.
**Warning signs:** The only size-boundary test in the diff constructs one huge message; no test constructs many normal-sized spans and asserts batch-count/size behavior.

### Pitfall 4: Truncating before redacting

**What goes wrong:** A secret pattern is split by a truncation cut (e.g., an API key cut mid-string by head/tail elision) and no longer matches `SecretPatterns`'s regex, so half the key survives, unredacted, into the emitted span.
**Why it happens:** It is tempting to truncate first (cheaper — you only redact the smaller, kept substring) — but this is exactly backwards per D-09.
**How to avoid:** Redact the FULL string first (regardless of eventual truncation), then truncate the redacted result. Mirrors the exact rationale documented in `internal/harness/redact/redact.go`'s own tail-keep-buffer comment (a different instance of the same "boundary/cut can defeat pattern matching" class of bug).
**Warning signs:** A test that redacts a synthetic secret AFTER truncating a message content string, rather than before.

### Pitfall 5: Copying `otelinit.NewTracerProvider()` into `tide-reporter`'s `main()` without also copying the manager's deferred-shutdown discipline

**What goes wrong:** Every reporter run silently drops its spans — Phoenix shows the level's AGENT dispatch span (once Phase 43 lands) with zero LLM children, looking like tracesynth never ran.
**Why it happens:** `cmd/manager/main.go`'s correct pattern (5-second bounded `context.WithTimeout` + deferred `otelShutdown(shutdownCtx)`) lives in a file most reporter-focused work won't touch; `tide-reporter`'s three bare `os.Exit(N)` call sites (in `main()`, not `run()`) don't naturally `defer` anything.
**How to avoid:** Place the TracerProvider construction AND a `defer`'d bounded-context `Shutdown` call INSIDE `run()`/`runWithClient()` (which already returns an `int` through every code path, unlike `main()`'s direct `os.Exit` calls) so every one of `runWithClient`'s existing early-return paths (missing flags, K8s API errors, materialize failures) still flushes. `main()` then does `os.Exit(run(...))` exactly as today — no path bypasses the new deferred Shutdown, because the defer lives one level up from `os.Exit`, not beside it.
**Warning signs:** `defer` placed in `main()` beside `os.Exit()` calls (which never run deferred functions) rather than inside `run()`.

## Code Examples

### Verified pattern: `NewBatchSpanProcessor`'s automatic env-var read (no code change required)

```go
// Source: go.opentelemetry.io/otel/sdk@v1.43.0/trace/batch_span_processor.go
// (vendored module cache, read directly)
func NewBatchSpanProcessor(exporter SpanExporter, options ...BatchSpanProcessorOption) SpanProcessor {
	maxQueueSize := env.BatchSpanProcessorMaxQueueSize(DefaultMaxQueueSize)             // OTEL_BSP_MAX_QUEUE_SIZE, default 2048
	maxExportBatchSize := env.BatchSpanProcessorMaxExportBatchSize(DefaultMaxExportBatchSize) // OTEL_BSP_MAX_EXPORT_BATCH_SIZE, default 512
	// ... options passed to WithBatcher(exp, opts...) can still override, but
	// otelinit.NewTracerProvider passes none today — env vars are authoritative.
}
```
Reporter Job env addition (in `BuildReporterJob`, `internal/controller/reporter_jobspec.go`):
```go
corev1.EnvVar{Name: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE", Value: "6"},
```

### Verified pattern: conversation reconstruction (matches the real fixture structure exactly)

```go
// Source: this research's fixture-parsing script, structurally equivalent to
// the recommended tracesynth.go shape; verified against 40 real dogfood Tasks.
var conversation []Turn
var currentCallBlocks []ContentBlock
for _, ev := range events {
	switch {
	case ev.Type == "stream_event" && ev.Event.Type == "message_start":
		callInput := snapshot(conversation) // this call's input_messages
		currentCallBlocks = nil
	case ev.Type == "stream_event" && ev.Event.Type == "message_stop":
		conversation = append(conversation, Turn{Role: "assistant", Content: currentCallBlocks})
		callOutput := currentCallBlocks // this call's output_messages
	case ev.Type == "assistant":
		currentCallBlocks = append(currentCallBlocks, ev.Message.Content...) // per-block accretion
	case ev.Type == "user":
		conversation = append(conversation, Turn{Role: "user", Content: ev.Message.Content, Timestamp: ev.Timestamp})
	}
}
```

### Verified pattern: redact-then-truncate ordering (D-09)

```go
// internal/harness/redact — new non-streaming helper (does not exist yet)
func String(s string) string {
	for _, re := range SecretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// tracesynth.go call site — redact BEFORE truncate, never the reverse
content := redact.String(rawMessageContent)
content = truncateHeadTail(content, 32*1024) // D-08 shape, AFTER redaction
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| CONTEXT.md's stated prior: "attrs.go unchanged, bounding lives in tracesynth.go" (D-03 exploration note) | `pkg/otelai` needs new tool-call/reasoning-block helper(s) alongside the unchanged `LLMInputMessages`/`LLMOutputMessages` (bounding logic itself does stay in `tracesynth.go`) | This research (module read directly, v0.1.1 confirmed to carry tool-call + reasoning keys) | The "attrs.go unchanged" framing was a reasonable prior before the module was read; it is now falsified for the tool-call/reasoning-encoding piece specifically — bounding/truncation logic's location is unaffected |
| Pitfall 2's original framing ("one fat span drops a whole batch") | Confirmed real risk is aggregate multi-span batch size, not per-span size, on this real corpus | This research (1,256 real calls measured) | Verification tests must target batch-count/aggregate-size behavior, not just single-oversized-message handling |

**Deprecated/outdated:** none — this is a net-new capability, not a migration.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | gRPC's default max-receive-message-size (4 MB) applies per RPC call (i.e., to the whole serialized `ExportTraceServiceRequest`, covering all spans in one batch), not per individual span field — standard `grpc-go` behavior, not verified against TIDE's own exporter config in this session | Size-Boundary Model | If gRPC were instead configured with a larger custom `MaxRecvMsgSize` somewhere, the whole aggregate-batch risk calculus would change; low risk since `internal/otelinit/provider.go` passes no custom gRPC dial options and Phoenix's own docs (Context7-verified) independently corroborate "gRPC has message-size limits... spans exceeding 4 MB... can hit these limits," consistent with the standard default |
| A2 | The 40 successfully-reconstructed fixture Tasks are representative of real production Task sizes (not a biased sample of only large/complex Tasks) | Size-Boundary Model | If real production Tasks are systematically smaller (or larger) than this dogfood salvage sample, the recommended constants (32 KiB/512 KiB/batch-size 6) may be more conservative or more lenient than ideal — low risk since the recommended constants have wide safety margins in both directions (10x+ headroom against p99 in the per-message case) |
| A3 | Phoenix's documented "cumulative_token_count_total" trace/session rollup literally sums every span's `llm.token_count.total` attribute within scope, with no built-in deduplication for parent/child span relationships | Summary (Phoenix double-count flag) | If Phoenix's rollup is actually smarter (e.g., only sums leaf LLM-kind spans, excluding AGENT-kind spans from the total), the double-count risk this research flags would be a non-issue; the Context7-sourced docs describe the rollup as "cumulative" without qualifying which span kinds are included, so this is inferred, not directly confirmed against an AGENT+children fixture in Phoenix itself |

**If this table is empty:** N/A — three claims above need light confirmation (A1/A2 are low-risk with wide safety margins already baked in; A3 is a genuine open architecture question flagged explicitly in Summary and Open Questions, not silently assumed away).

## Open Questions

1. **Phoenix cost/token double-counting once this phase's LLM children exist under an AGENT span that already carries pre-summed totals (D-04, crosses into Phase 42/43 territory)**
   - What we know: Phoenix's official docs (Context7-verified, `docs/phoenix/release-notes/05-2026/05-05-2026-rest-api-updates.mdx` and `06-2025/06-25-2025-cost-tracking.mdx`) describe trace/session/project token and cost views as "cumulative" / "rolled up" — consistent with summing every span's `llm.token_count.*` attributes in scope. Phase 42 already ships `TokenCount(...)` (a pre-summed grand total) on all four planner-level AGENT spans; Phase 43 (not yet planned) will add the same to Task's AGENT span. This phase's own D-02 scope explicitly adds LLM-kind children with THEIR OWN per-call `TokenCount` under every one of those same five AGENT spans (in materialization mode).
   - What's unclear: whether Phoenix's rollup is naive-sum-of-everything (confirmed double-count) or has some AGENT/LLM-kind-aware deduplication (not found in the docs surfaced this session) — and, either way, which tier (the AGENT span or the LLM children) should be the source of truth for a level's token/cost total going forward.
   - Recommendation: do not resolve silently. Surface at `/gsd:discuss-phase` or as an explicit planner decision point before this phase's plan touches Phase 42's already-shipped `TokenCount` call sites. Two live options: (a) drop `llm.token_count.*` from an AGENT span the moment it gains LLM children (requires a small Phase 42-territory follow-up, not purely additive), matching how native `openinference-instrumentation-langchain` AGENT-kind wrapper spans typically behave (a wrapper doesn't restate its children's sums); or (b) accept the inflation at the trace/session/project ROLLUP level only (per-span data in Phoenix's UI stays correct either way) and document it as a known limitation, deferring the real fix. This affects PROOF-01's cost/token accuracy bar at milestone close, so it should not ship unresolved and undocumented.

2. **Exact absolute span timing for a per-call LLM span, given `events.jsonl` has no in-band absolute timestamp for `message_start`/`message_stop`/`assistant` events**
   - What we know: only `user`-type (tool_result) events carry an absolute ISO-8601 `timestamp`; `stream_event`/`assistant` events carry none (verified: 0/65 `assistant` events, 0/N `stream_event` events across the largest fixture all lack `timestamp`; only `ttft_ms`, a relative time-to-first-token figure, is present on `message_start` stream events).
   - What's unclear: whether interpolating between bracketing `user`-event timestamps (when present) produces visually coherent Phoenix waterfalls, or whether proportional-division-across-the-Job-window (the CONTEXT.md-cited fallback) is simpler and good enough given Pitfall 5 (cross-pod clock skew) already flags this class of rendering risk as inherent to retroactive synthesis.
   - Recommendation: planner picks whichever composes cleanest with Phase 43's own timestamp-sourcing decisions (out of this phase's scope to invent independently); mark synthesized timing explicitly (`tide.trace.timing_synthetic=true`-shaped attribute, mirroring the existing `EnvelopeDegraded()` pattern) regardless of which interpolation strategy is chosen, per the Claude's-Discretion floor CONTEXT.md already set.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Building `tide-reporter`, `pkg/otelai`, `internal/harness/redact` | ✓ | go1.26.3 darwin/amd64 | — |
| `go.opentelemetry.io/otel*` v1.43.0 (vendored) | TracerProvider, BatchSpanProcessor env-driven config | ✓ | v1.43.0 (module cache present) | — |
| `openinference-semantic-conventions` v0.1.1 (vendored) | Attribute-key constants, tool-call/message-contents indexers | ✓ | v0.1.1 (module cache present) | — |
| Docker | `make test-int-kind-prep` (building the `tide-reporter` image for kind) | ✓ | — | — |
| kind | Layer B integration verification | ✓ | v0.31.0 | — |
| Real dogfood fixture corpus | D-07 constant derivation (this research) | ✓ | 58 files, `examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/` | — |

**Missing dependencies with no fallback:** none.

**Missing dependencies with fallback:** none — every dependency this phase needs was already present and pinned before this research began.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (plain `go test`, no Ginkgo needed for `pkg/otelai`/`internal/reporter`/`internal/harness/redact` — matches the existing `attrs_test.go`/`materialize_test.go`/`main_test.go` convention) |
| Config file | none — plain `go test ./...` |
| Quick run command | `go test ./pkg/otelai/... ./internal/reporter/... ./internal/harness/redact/... ./cmd/tide-reporter/...` |
| Full suite command | `make test` (unit tier) then `make test-int-fast` (Layer A envtest, if this phase's manager-side spawn-gating (D-06) touches a reconciler) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| MSG-01 | Reporter trace-only mode reads `events.jsonl` and reconstructs conversation | unit | `go test ./internal/reporter/... -run TestReconstructConversation` | ❌ Wave 0 |
| MSG-01 | Reporter seeds call-1 context from `in.json`/`PromptPath` | unit | `go test ./internal/reporter/... -run TestReconstructConversation_SeedsPrompt` | ❌ Wave 0 |
| MSG-02 | Known secret pattern injected into a fixture message never appears in emitted span attributes | unit | `go test ./internal/reporter/... -run TestEmitSpans_Redacts` | ❌ Wave 0 |
| MSG-02 | `redact.String` exists and applies `SecretPatterns` non-streaming | unit | `go test ./internal/harness/redact/... -run TestString` | ❌ Wave 0 |
| MSG-03 | Oversized single message truncates head+tail with marker (D-08), `ArtifactPath` co-attribute present | unit | `go test ./internal/reporter/... -run TestEmitSpans_TruncatesOversizedMessage` | ❌ Wave 0 |
| MSG-03 | Redaction runs before truncation (D-09) — secret split across the truncation cut is still redacted | unit | `go test ./internal/reporter/... -run TestEmitSpans_RedactsBeforeTruncate` | ❌ Wave 0 |
| MSG-03 | A synthetic ~32-span Task-sized batch (real-sized, NOT oversized, spans) asserts export RPC count/max-bytes-per-RPC stays under a safe margin | integration | `go test ./internal/reporter/... -run TestEmitSpans_BatchAggregateUnderCeiling` (fake OTLP collector capturing raw Export() calls) | ❌ Wave 0 |
| MSG-03 | `TestNoPayloadHelperOnPublicSurface` still passes after any new `pkg/otelai` helper additions | unit (existing, regression) | `go test ./pkg/otelai/... -run TestNoPayloadHelperOnPublicSurface` | ✅ (existing, `attrs_test.go:187`) |
| TRACE-03 | `tide-reporter` flushes spans to a fake OTLP collector before process exit on EVERY exit path (success, generic failure, invariant violation) | integration | `go test ./cmd/tide-reporter/... -run TestRun.*Shutdown` | ❌ Wave 0 |
| D-11 | Tolerant-skip: a truncated/malformed `events.jsonl` still emits whatever conversation is reconstructable, stamped degraded | unit | `go test ./internal/reporter/... -run TestReconstructConversation_TolerantSkip` | ❌ Wave 0 |
| D-03 | Tool-use and thinking content blocks emit spec-native keys (`message.tool_calls.*`, `message.contents.*`), not stringified fallback | unit | `go test ./pkg/otelai/... -run TestToolCall.*|TestReasoning.*` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test ./pkg/otelai/... ./internal/reporter/... ./internal/harness/redact/... ./cmd/tide-reporter/...`
- **Per wave merge:** `make test` (full unit tier)
- **Phase gate:** `make test` green + the batch-aggregate test (`TestEmitSpans_BatchAggregateUnderCeiling`) explicitly asserting real observed sizes (32 calls, sizes drawn from or modeled on this research's fixture distribution) stay under the configured `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` × per-span-cap product, before `/gsd:verify-work`.

### Wave 0 Gaps

- [ ] `internal/reporter/tracesynth_test.go` — covers MSG-01, MSG-03, D-11 (new file, new package-level tests)
- [ ] `internal/harness/redact/redact_test.go` extension — covers MSG-02's new `String()` helper (existing file, new test function)
- [ ] `cmd/tide-reporter/main_test.go` extension — covers TRACE-03's shutdown-on-every-exit-path behavior (existing file; needs a fake OTLP collector test double, does not exist yet)
- [ ] `pkg/otelai/attrs_test.go` extension — covers D-03's new tool-call/reasoning helper(s) (existing file, new test functions; must not break `TestNoPayloadHelperOnPublicSurface`/`TestKeysUseSemconvModule`)
- [ ] A committed test fixture: a small, synthetic (non-secret, redaction-test-safe) `events.jsonl` sample mirroring the real schema (message_start/content_block_*/message_stop cycles, a tool_use + thinking block, a tool_result with an injected fake-but-pattern-matching secret) — the real dogfood fixtures at `examples/projects/dogfood/salvage-20260618/` should NOT be imported into `_test.go` fixtures verbatim (unknown provenance/size for CI, and this research's redaction-pass check needs a KNOWN injected secret to assert against, which the real fixtures don't have)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | This phase adds no new authentication surface (reporter SA/RBAC is existing infra, unchanged by this phase) |
| V3 Session Management | no | N/A |
| V4 Access Control | no | Trace-only reporter mode needs no CR-write RBAC (Claude's Discretion note in CONTEXT.md); existing `tide-reporter` SA/Role is unchanged or narrowed, never widened |
| V5 Input Validation | yes | `events.jsonl` parsing is tolerant-skip (D-11) over untrusted/malformed input by design — the parser must never panic or hang on adversarial or corrupted JSONL (mirrors `ParseStream`'s existing defensive posture); the 16 MB per-line budget enforced by `common.ReadLines` already bounds a single malicious line |
| V6 Cryptography | no | No new crypto surface; redaction is pattern-matching, not cryptographic |

**This phase's actual primary security concern falls outside the ASVS categories above:** data-exposure prevention (MSG-02's redaction mandate) is closest to ASVS V14 (Configuration)/data-protection concerns broadly, not cleanly one of V2/V3/V4/V5/V6. Treat it as the phase's headline security requirement regardless of ASVS-category fit:

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Secret/credential leakage from repo content echoed by the model into `events.jsonl`, then into a Phoenix span (unredacted) | Information Disclosure | `redact.SecretPatterns` mandatory pass (MSG-02) BEFORE any content reaches `LLMInputMessages`/`LLMOutputMessages`, applied BEFORE truncation (D-09) so a truncation cut cannot defeat pattern matching |
| Adversarial/corrupted `events.jsonl` (malformed JSON, absurdly long lines, malformed nested structures) causing the reporter to panic or hang | Denial of Service | Tolerant-skip parsing (D-11), reuse of `common.ReadLines`'s existing 16 MB per-line budget; no new unbounded-read surface introduced |
| A future adversarial Task producing an extremely large or deeply-repetitive conversation, inflating span/attribute volume toward the OTLP ceiling | Denial of Service (against the tracing pipeline, not TIDE's core dispatch) | Per-message (32 KiB) + whole-span (512 KiB) truncation caps + `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` batch tuning (this document's Size-Boundary Model) bound worst-case payload regardless of conversation size; D-10's best-effort exit-0 posture ensures a pathological trace-export failure never blocks the Task's own completion reporting |

## Project Constraints (from CLAUDE.md)

Extracted directives from the repo's `CLAUDE.md` that bind this phase's plan:

- **GSD Workflow Enforcement:** all implementation must route through `/gsd:execute-phase` (or `/gsd:quick`/`/gsd:debug` for out-of-band fixes) — no direct edits outside an active plan. This RESEARCH.md and the phase's eventual PLAN.md are the sanctioned path.
- **`charts/tide/values.yaml` is a FIXED contract** — "binary catches up to chart, never reverse." This research confirms the phase's recommendations (`OTEL_EXPORTER_OTLP_ENDPOINT` forwarding, `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` tuning) require **zero `values.yaml` changes** — both are Go-code-level env vars set on the reporter container the manager already constructs at runtime (`BuildReporterJob`), not new Helm-templated values. If the plan later wants `OTEL_BSP_MAX_EXPORT_BATCH_SIZE` operator-tunable, that WOULD touch the fixed contract and needs explicit confirmation first.
- **Observe First:** any BLOCKED runtime gate during this phase's execution should be diagnosed via `kubectl logs`/envelope/VERIFICATION.md frontmatter before hypothesizing — applies to any live-cluster verification of the reporter's trace-only spawn (Phase 43-dependent, likely deferred to a later phase's live proof).
- **`make test-int` exit ≠ Ginkgo green** — the plan's verification steps must read `MAKE_EXIT` and grep for `^--- FAIL|^FAIL\s`, not just the Ginkgo summary line, per the project's own documented near-miss (Phase 7's dropped chart-template block).
- **Don't hand-roll a Go OpenInference SDK** — `pkg/otelai` stays "thin helpers," per its own doc.go contract; this research's recommended tool-call/reasoning helper additions extend that same thin-helper pattern, not a new abstraction layer.
- **Never hardcode secrets** — not directly implicated by this phase's own code (no new secrets introduced), but MSG-02's entire purpose is preventing OTHER secrets (leaked via repo content) from reaching a span; the redaction pass is this phase's enforcement of that broader project rule.
- **Match surrounding code / no action-narrating comments / tests come with the change** — apply directly to the new `tracesynth.go` file, `pkg/otelai` helper additions, and `redact.String` addition; house style is comment-dense, `D-XX`-decision-referencing, `// Source:`-cited where a claim is drawn from spec/docs (matches this document's own citation style, inherited from `attrs.go`/`doc.go`).
- **Subagent model tuning section (CLAUDE.md):** not applicable to this phase — that section concerns the `claude` CLI dispatch surface for planner/executor subagents, not the reporter or trace-emission code path.

