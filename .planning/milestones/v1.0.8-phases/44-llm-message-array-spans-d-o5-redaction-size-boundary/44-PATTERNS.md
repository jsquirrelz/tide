# Phase 44: LLM Message-Array Spans + D-O5 Redaction/Size Boundary - Pattern Map

**Mapped:** 2026-07-16
**Files analyzed:** 13 (3 new, 10 modified)
**Analogs found:** 13 / 13 (one — the fake-OTLP shutdown test double — maps to a repurposed in-repo pattern, not a literal file; flagged in its own section)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `internal/reporter/tracesynth.go` (new) | service (synthesizer) | file-I/O → transform → event-driven span emission | `internal/reporter/materialize.go` (package/doc-comment/import-safety shape) + `internal/controller/span_emission.go` (span-emission mechanics) | role-match (composite: two analogs, no single exact one exists yet) |
| `internal/reporter/tracesynth_test.go` (new) | test | transform / span-assertion | `internal/controller/span_emission_unit_test.go` | exact (same `tracetest.InMemoryExporter` pattern, same plain-`testing.T` style) |
| `internal/reporter/testdata/*.jsonl` (new fixture) | test fixture | file-I/O | none in-repo (real fixtures at `examples/projects/dogfood/...` explicitly must NOT be imported verbatim per RESEARCH Wave-0 gaps) | no analog — author from schema doc |
| `internal/harness/redact/redact.go` (modify — add `String`) | utility | transform | same file's `RedactingWriter.Close()` (lines 87-102) | exact |
| `internal/harness/redact/redact_test.go` (modify) | test | transform | same file's `TestRedactingWriter` table (lines 56-100) | exact |
| `cmd/tide-reporter/main.go` (modify — flags, TracerProvider, deferred Shutdown) | controller (one-shot binary entrypoint) | request-response / event-driven | `cmd/manager/main.go` lines 256-290 (TracerProvider + deferred bounded Shutdown) — grafted onto `cmd/tide-reporter/main.go`'s own existing `run()`/`runWithClient()` seam | exact (cross-binary, same otelinit contract) |
| `cmd/tide-reporter/main_test.go` (modify) | test | request-response | same file's `TestRunHappyPath` (lines 76+) for flag/exit-code style + `internal/controller/span_emission_unit_test.go`'s `setupSpanExporter` for the Shutdown-flush assertion | role-match |
| `pkg/otelai/attrs.go` (modify — tool-call/reasoning helpers) | utility (attribute helpers) | transform | same file's `flattenMessages`/`LLMInputMessages` (lines 66-99) | exact |
| `pkg/otelai/attrs_test.go` (modify) | test | transform | same file's per-helper tests (`TestFailureDetail` etc., lines 161-180) + the two guard tests (`TestNoPayloadHelperOnPublicSurface`, `TestKeysUseSemconvModule`, lines 187-236) | exact |
| `pkg/otelai/doc.go` (modify — D-O5 text evolution) | config/doc | — | same file's "D-O5 — no payload inlining" section (lines 60-75) | exact |
| `internal/controller/reporter_jobspec.go` (modify — OTLP env forwarding + batch-size literal) | config (Job builder) | request-response (Job spec construction) | `internal/dispatch/podjob/jobspec.go` lines 372-415 (`subagentEnv` conditional-append pattern) | role-match |
| `internal/controller/reporter_jobspec_test.go` (modify) | test | — | same file's `TestBuildReporterJob_Image` (lines 368-394) style | exact |
| `internal/controller/dispatch_helpers.go` (modify — D-06 spawn gating) | controller (spawn helper) | event-driven | same file's `spawnReporterIfNeeded` (lines 80-130, the function itself gains the new gate) | exact (self-extension) |

## Pattern Assignments

### `internal/reporter/tracesynth.go` (new file — service/synthesizer)

**Analog 1 — package shape and import-safety contract:** `internal/reporter/materialize.go`

**Imports pattern** (materialize.go lines 29-44):
```go
package reporter

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)
```
`tracesynth.go` should mirror the SAME "no `internal/controller` back-edge" contract stated in materialize.go's package doc (lines 17-28) but its actual imports will differ: `otel/trace`, `pkg/otelai`, `internal/harness/redact`, plus stdlib `bufio`/`encoding/json`/`os` for JSONL reading — no K8s client needed for the synth path itself (pure file → span transform). State the import-safety contract in the file's own header comment, copying the "intentionally import-safe from cmd binaries" framing verbatim.

**D-XX decision-tagged comment style** (materialize.go lines 46-54, 151-165 — the load-bearing convention to copy):
```go
// maxSharedContextBytes is the etcd DoS guard for LLM-authored SharedContext
// blobs (T-20-03-01). etcd imposes a hard ~1.5 MiB per-object limit; curated
// wave-scoped summaries are expected to be ~300–700 tokens (~300–700 bytes),
// well within this cap. ...
// See CONTEXT.md D-04 and RESEARCH.md Security Domain for rationale.
const maxSharedContextBytes = 64 * 1024
```
Apply this exact citation style to the new constants this phase introduces: a 32 KiB per-message truncation floor (D-08, cite RESEARCH.md Size-Boundary Model), a 512 KiB whole-span budget (secondary backstop), and any degraded/synthetic-timing marker constants (D-11, Claude's Discretion timing floor).

**Analog 2 — span-emission mechanics:** `internal/controller/span_emission.go`

**Core span-creation pattern to copy** (span_emission.go lines 130-164):
```go
tracer := otel.Tracer("tide.dispatch")
spanName := "tide.dispatch." + level
_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

span.SetAttributes(otelai.AgentInvocation(provider.Vendor, spanName, "planner", level)...)
span.SetAttributes(otelai.LLMIdentity(provider.Vendor, provider.Model)...)
// ... conditional token-count / degraded-marker attributes ...

if isJobFailed(completedJob) {
	span.SetStatus(codes.Error, out.Reason)
} else {
	span.SetStatus(codes.Ok, "")
}
span.End(trace.WithTimestamp(endTime))
```
`EmitSpans` in `tracesynth.go` follows the SAME shape per `CallSpan`: `tracer.Start(ctx, "<name>", trace.WithTimestamp(...))`, `SetAttributes` with `otelai.LLMInputMessages`/`otelai.LLMOutputMessages`/`otelai.TokenCount`, `openinference.span.kind=LLM` (research: use a helper analogous to `AgentInvocation` but for LLM kind — check whether `pkg/otelai` needs a small `LLMSpanKind()`-style addition or whether `attribute.String(semconv.OpenInferenceSpanKind, semconv.SpanKindLLM)` is set inline; `AgentInvocation` hardcodes `SpanKindAgent` so it is NOT reusable as-is for LLM-kind spans — a new call site inlines this attribute or a thin new helper is added), then `span.End(trace.WithTimestamp(...))`. Never hold a span open across a function return (comment at span_emission.go lines 20-22 states this constraint explicitly — copy it verbatim into tracesynth.go's header).

**Timestamp resolution pattern** (span_emission.go lines 45-67, `spanEndTime`): mirror this defensive nil-check shape for the marked-synthetic timing fallback (Claude's Discretion / Open Question 2) — never fabricate wall-clock "now" as a silent substitute; when no real timestamp is derivable, stamp the `tide.trace.timing_synthetic=true`-shaped marker attribute (mirrors `EnvelopeDegraded()`'s existing one-attribute-marker convention at `pkg/otelai/attrs.go` lines 201-210) rather than silently interpolating without a flag.

**Analog 3 — tolerant JSONL line parsing (D-11):** `internal/subagent/anthropic/stream_parser.go`

**Defensive parse-skip pattern to copy** (stream_parser.go lines 82-111):
```go
err := common.ReadLines(r, func(line []byte) error {
	var ev streamEvent
	if jerr := json.Unmarshal(line, &ev); jerr != nil {
		// Tolerate non-JSON lines — they have already been teed to
		// rawSink for Phase 4 forensic analysis.
		return nil
	}
	// ... type-switch on ev.Type ...
	return nil
})
```
`ReconstructConversation`'s JSONL walk uses the identical "unmarshal error → skip, never propagate" posture, stamping a degraded-marker (D-11) instead of returning an error. Reuse `internal/subagent/common.ReadLines` directly (already enforces the 16 MB per-line budget — do not reimplement).

**message_start/message_stop grouping algorithm** (see RESEARCH.md "Reconstruction algorithm" — this is a NEW algorithm with no existing in-repo analog; RESEARCH.md's own "Verified pattern: conversation reconstruction" code block is the closest available reference and should be read directly rather than re-derived):
```go
var conversation []Turn
var currentCallBlocks []ContentBlock
for _, ev := range events {
	switch {
	case ev.Type == "stream_event" && ev.Event.Type == "message_start":
		callInput := snapshot(conversation)
		currentCallBlocks = nil
	case ev.Type == "stream_event" && ev.Event.Type == "message_stop":
		conversation = append(conversation, Turn{Role: "assistant", Content: currentCallBlocks})
		callOutput := currentCallBlocks
	case ev.Type == "assistant":
		currentCallBlocks = append(currentCallBlocks, ev.Message.Content...)
	case ev.Type == "user":
		conversation = append(conversation, Turn{Role: "user", Content: ev.Message.Content, Timestamp: ev.Timestamp})
	}
}
```

**Error handling pattern:** materialize.go's `fmt.Errorf("MaterializeChildCRDs: ...: %w", err)` wrapping convention (lines 205-299) — every returned error is prefixed with the function name for grep-ability. Apply identically: `fmt.Errorf("ReconstructConversation: read %q: %w", eventsPath, err)`.

---

### `internal/reporter/tracesynth_test.go` (new file — test)

**Analog:** `internal/controller/span_emission_unit_test.go`

**Exporter-swap harness to copy verbatim** (span_emission_unit_test.go lines 44-55):
```go
func setupSpanExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}
```
This is THE reusable pattern for every `EmitSpans`-touching test in `tracesynth_test.go` — `sdktrace.WithSyncer` makes export synchronous so no `ForceFlush`/sleep is needed in unit tests (defer that to the TRACE-03 shutdown test in `main_test.go`, which specifically needs to prove the async batch path flushes).

**Attribute-lookup helper to copy verbatim** (same file, lines 57-66):
```go
func attrValue(attrs []attribute.KeyValue, key attribute.Key) (attribute.Value, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return attribute.Value{}, false
}
```

**Table-driven span-attribute assertion shape to copy** (span_emission_unit_test.go lines 210-295, `TestSynthesizePlannerSpanSucceededComplete`): build the input (here: a fixture-shaped `[]CallSpan` or raw `events.jsonl` bytes), call `EmitSpans`, then assert on `exp.GetSpans()[i].Attributes` via `attrValue` for each expected key (`llm.input_messages.0.message.role`, `openinference.span.kind=LLM`, `llm.token_count.*`, `tide.artifact_path`, degraded/synthetic markers). Mirror the exact `wantStringAttrs`/`wantIntAttrs` map-driven loop style (lines 262-294) rather than one assertion per line.

**Pitfall-3-driven test requirement (batch-aggregate, not single-oversized-message):** per RESEARCH.md Pitfall 3 and the Phase Requirements → Test Map row for `TestEmitSpans_BatchAggregateUnderCeiling`, at least one test MUST construct ~32 real-sized (not oversized) `CallSpan`s and assert on export RPC count / bytes-per-RPC via a fake `trace.SpanExporter` capturing raw `Export()` calls — `tracetest.InMemoryExporter` alone does not model batching; a small custom `SpanExporter` stub (recording each `Export([]tracetest.SpanStub)` call) is new and has no direct in-repo analog — write it inline in the test file following the same minimal-fake style as `cmd/tide-reporter/main_test.go`'s `buildFakeClient` (construct only what the test needs, no shared test-double package).

---

### `internal/harness/redact/redact.go` (modify — add `String`)

**Analog:** same file's `Close()` method (lines 84-102), which already runs `SecretPatterns` over a full in-memory buffer (not a stream) as its final pass.

**Pattern to copy** (redact.go lines 87-96, adapted to a standalone function):
```go
func (w *RedactingWriter) Close() error {
	if len(w.tail) > 0 {
		buf := w.tail
		for _, re := range w.patterns {
			buf = re.ReplaceAll(buf, []byte("[REDACTED]"))
		}
		if _, err := w.dst.Write(buf); err != nil {
			return err
		}
		w.tail = w.tail[:0]
	}
	// ...
}
```
New `String(s string) string` (RESEARCH.md's own recommended shape, Code Examples section):
```go
// String applies SecretPatterns to a single in-memory string and returns
// the redacted result. Unlike RedactingWriter (stream-oriented, tail-keep
// buffered across Write calls), String operates on the full string at once —
// the correct shape for per-message redaction (MSG-02) where the entire
// message content is already materialized before it is ever emitted onto a
// span. D-09: callers MUST call String before any truncation step — see
// tracesynth.go's EmitSpans for the call site ordering.
func String(s string) string {
	b := []byte(s)
	for _, re := range SecretPatterns {
		b = re.ReplaceAll(b, []byte("[REDACTED]"))
	}
	return string(b)
}
```
Place in `redact.go` beside `RedactingWriter`/`Close()` — same file, same package, reuses the package-level `SecretPatterns` var from `patterns.go` (no new import beyond what's already there: `regexp` is already imported transitively via `patterns.go`, `redact.go` itself needs no additional import for this addition beyond what it already has).

---

### `internal/harness/redact/redact_test.go` (modify)

**Analog:** same file's `TestRedactingWriter` table (lines 56-100) and the `singleWriteRedact` helper (lines 36-53).

**Pattern to copy** — table-driven cases reusing the EXACT SAME secret-pattern fixtures already proven against `RedactingWriter` (Anthropic key, JWT, AWS key, GitHub PAT, Slack token), just calling `String(tc.input)` directly instead of routing through a `bytes.Buffer` + `Write`/`Close`:
```go
func TestString(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{name: "RedactsAnthropicKey", input: "here is sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV the rest", want: "here is [REDACTED] the rest"},
		// ... same table as TestRedactingWriter, lines 62-91 ...
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := String(tc.input); got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
```
Add a dedicated `TestString_RedactsBeforeTruncateOrdering`-style test only in `tracesynth_test.go` (D-09 is a call-site ordering concern, not a `redact` package concern — `redact_test.go` only needs to prove `String` itself redacts correctly).

---

### `cmd/tide-reporter/main.go` (modify — trace-only mode, TracerProvider, deferred Shutdown)

**Analog:** `cmd/manager/main.go` lines 256-290 (TracerProvider construction + deferred bounded Shutdown) — TRANSPLANTED into `tide-reporter`'s existing `run()`/`runWithClient()` seam (Pitfall 5's explicit warning: do NOT put this in `main()` beside `os.Exit`).

**Pattern to copy** (manager main.go lines 260-290, adapted):
```go
// Establish signalCtx early (tide-reporter's main() already does this at
// lines 103-104 — reuse the EXISTING ctx, don't create a second one).
tp, otelShutdown, err := otelinit.NewTracerProvider(ctx)
if err != nil {
	fmt.Fprintf(stderr, "tide-reporter: otel init failed: %v\n", err)
	return exitGenericFail
}
defer func() {
	// D-12: bounded flush timeout — order of seconds, mirrors manager's 5s.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := otelShutdown(shutdownCtx); err != nil {
		fmt.Fprintf(stderr, "tide-reporter: otel shutdown failed: %v\n", err)
		// D-10: best-effort — log, never fail the run.
	}
}()
```
CRITICAL per Pitfall 5 and CONTEXT.md D-12: this `defer` must live INSIDE `runWithClient` (which already returns `int` through every code path — see the existing `return exitInvariant`/`exitGenericFail`/`exitSuccess` statements at lines 126-205), NOT inside `main()`. `main()` keeps its unchanged `os.Exit(run(ctx, cfg, os.Stdout, os.Stderr))` wrapper (line 106) — no path bypasses the new deferred Shutdown because it is one level below `os.Exit`, exactly as manager's pattern places it one level below its own `os.Exit(1)` calls.

**Flag-surface extension pattern** (main.go lines 79-92, the existing `flag.NewFlagSet` block): add the trace-only mode's new flags (Claude's Discretion: flag vs env var — RESEARCH leans flag, matching this file's existing all-flag convention) in the SAME `fs.String(...)` block style, e.g. `fs.Bool("trace-only", false, "...")`, threaded into `reporterConfig` exactly like the six existing fields (lines 60-67).

**Exit-code map convention** (main.go lines 28-32, 69-76) — D-10 says "best-effort exit 0 everywhere" for synth/export failures specifically; this does NOT change the existing `exitSuccess`/`exitGenericFail`/`exitInvariant` map for the MATERIALIZATION path — it only means tracesynth/export errors must be caught and logged without returning a non-zero code, a NEW branch distinct from the existing invariant-violation checks. Document this distinction in the updated package doc-comment (lines 17-32).

---

### `cmd/tide-reporter/main_test.go` (modify)

**Analog 1 — flag/exit-code test style:** same file's `TestRunHappyPath` (lines 76+), `buildFakeClient`, `writeOutJSON` helpers (lines 31-62) — reuse verbatim for any new trace-only-mode test that also touches the K8s client path.

**Analog 2 — Shutdown-flush proof:** `internal/controller/span_emission_unit_test.go`'s `setupSpanExporter` (lines 44-55) is the closest available building block, but TRACE-03 needs to prove the ASYNC `otelinit.NewTracerProvider`'s real SDK path (not `WithSyncer`) actually flushes on every `runWithClient` exit path — `sdktrace.WithSyncer` bypasses the exact batching-then-flush code path TRACE-03 is testing. The test needs a small `trace.SpanExporter` stub (recording `Export` calls, `Shutdown` calls) passed into a `sdktrace.NewTracerProvider(sdktrace.WithBatcher(stubExporter))`, then swapped via `otel.SetTracerProvider` before calling `runWithClient` with each of its early-return paths (missing flags, K8s API error, materialize failure) and asserting the stub's `Shutdown` was invoked before `runWithClient` returns. No existing file has this exact stub — write it inline, following `buildFakeClient`'s "construct only what the test needs" minimalism (main_test.go lines 41-46).

---

### `pkg/otelai/attrs.go` (modify — tool-call / reasoning-block helpers)

**Analog:** same file's `flattenMessages` (lines 77-99) and `TokenCount` (lines 116-124) — the established "resolve every key from `semconv.*`, return `[]attribute.KeyValue`" shape.

**Pattern to copy and extend** (attrs.go lines 83-99):
```go
func flattenMessages(input bool, msgs []Message) []attribute.KeyValue {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, 2*len(msgs))
	for i, m := range msgs {
		roleKey, contentKey := semconv.LLMOutputMessageRoleKey(i), semconv.LLMOutputMessageContentKey(i)
		if input {
			roleKey, contentKey = semconv.LLMInputMessageRoleKey(i), semconv.LLMInputMessageContentKey(i)
		}
		out = append(out, attribute.String(roleKey, m.Role), attribute.String(contentKey, m.Content))
	}
	return out
}
```
D-03's resolution requires a NEW sibling function using the module's own indexer helpers (confirmed present in RESEARCH.md's "D-03 Resolution" section, read directly from the vendored module):
```go
// Source: github.com/Arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1
// indexers.go — LLMOutputMessageToolCallKey(i, j, child string) string
//   e.g. "llm.output_messages.0.message.tool_calls.0.tool_call.function.name"
func LLMOutputMessageToolCalls(msgIdx int, calls []ToolCall) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 3*len(calls))
	for j, c := range calls {
		out = append(out,
			attribute.String(semconv.LLMOutputMessageToolCallKey(msgIdx, j, semconv.ToolCallID), c.ID),
			attribute.String(semconv.LLMOutputMessageToolCallKey(msgIdx, j, semconv.ToolCallFunctionName), c.Name),
			attribute.String(semconv.LLMOutputMessageToolCallKey(msgIdx, j, semconv.ToolCallFunctionArgumentsJSON), c.ArgumentsJSON),
		)
	}
	return out
}
```
(exact indexer call signature/constant names must be re-verified against the vendored module at implementation time — RESEARCH.md cites them from a direct source read but the planner/executor should re-confirm via `go doc` before coding, since this pattern map does not re-verify library internals.)

Naming constraint (D-O5 guard, see below): the new struct/function names must NOT match `TestNoPayloadHelperOnPublicSurface`'s forbidden list (`Payload`, `InlinePayload`, `RawContent`, `Body`, `MessageBody`) — `ToolCall`, `MessageContents`, `Reasoning*` names are all safe per RESEARCH.md's explicit confirmation.

**Doc-comment update pattern** (attrs.go lines 39-53, the "TIDE-custom keys" const block and its citation of `TestKeysUseSemconvModule`): any NEW hand-rolled string literal must go through the SAME const-block-with-citation treatment if it has no module counterpart, or must resolve via `semconv.*` if it does — `TestKeysUseSemconvModule`'s regex (`"(llm\.|openinference\.|gen_ai\.|agent\.)`) will fail the build on any hand-rolled `llm.`/`openinference.` literal, so all new tool-call/reasoning keys MUST come from `semconv.*` constants, never typed inline.

---

### `pkg/otelai/attrs_test.go` (modify)

**Analog:** same file's per-helper unit tests (`TestFailureDetail` lines 161-170, `TestEnvelopeDegraded` lines 174-180) — `reflect.DeepEqual` against a hand-built `[]attribute.KeyValue`/`attribute.KeyValue` literal.

**Guard-test regression requirement (do not delete, do extend if needed):**
```go
func TestNoPayloadHelperOnPublicSurface(t *testing.T) {
	// ... source-greps attrs.go for forbidden func-name substrings ...
}
func TestKeysUseSemconvModule(t *testing.T) {
	// ... source-greps comment-stripped attrs.go for hand-rolled spec-family literals ...
}
```
(attrs_test.go lines 187-236). MSG-03's phase requirement explicitly says this guard gets "updated deliberately, never deleted" — if the new tool-call/reasoning helpers require a new forbidden-name entry or a naming adjustment, edit the `forbidden` slice (line 198-204) additively; never remove an existing guard assertion.

**New test pattern to add** (mirrors `TestEmptyInputsNoPanic`, lines 241-253, for nil/empty-slice defensiveness on the new tool-call/reasoning helper(s)) plus a positive-path test in the `TestFailureDetail`/`TestEnvelopeDegraded` reflect.DeepEqual style asserting the exact key strings the new helper emits (e.g. `llm.output_messages.0.message.tool_calls.0.tool_call.function.name`).

---

### `pkg/otelai/doc.go` (modify — D-O5 text evolution)

**Analog:** same file's "D-O5 — no payload inlining" section (lines 60-75) and the "Public surface (8 helpers)" enumeration (lines 38-58).

**Pattern:** this is a doc-comment-only edit — update the helper count/list (lines 38-58) to include the new tool-call/reasoning helper(s), and evolve the D-O5 prose (RESEARCH.md's own State-of-the-Art table flags this exact wording change: "prefer ArtifactPath" → "bounded-inline + ArtifactPath co-attribute"). Keep the file's existing citation style (`# D-O5 — no payload inlining` heading, `TestNoPayloadHelperOnPublicSurface source-greps ... to enforce this at PR-review time` sentence) — extend rather than rewrite.

---

### `internal/controller/reporter_jobspec.go` (modify — OTLP env forwarding + batch-size literal)

**Analog:** `internal/dispatch/podjob/jobspec.go` lines 372-415 (`subagentEnv` conditional-append pattern — the established house style for "add an env var only when a value is configured").

**Pattern to copy** (jobspec.go lines 393-401, adapted):
```go
// D-06 / Build Order step 5: forward the manager's own OTLP endpoint into
// the reporter Job so its TracerProvider construction (TRACE-03) resolves
// the same collector. Absent when unset — reporter's otelinit falls back
// to its own no-op path, matching D-06's spawn-gating rationale.
if opts.OTLPEndpoint != "" {
	env = append(env,
		corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: opts.OTLPEndpoint},
		// Hardcoded literal, NOT a Helm value — CLAUDE.md's values.yaml FIXED
		// contract; math in RESEARCH.md Size-Boundary Model (6 spans x 512KiB
		// cap = 3MiB, ~25% headroom under the 4MiB OTLP ceiling).
		corev1.EnvVar{Name: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE", Value: "6"},
	)
}
```
`BuildReporterJob`'s `corev1.Container{...}` currently has no `Env` field set at all (reporter_jobspec.go lines 193-211) — this is a NEW field on the container, not an edit to an existing env list. Add `ReporterOptions.OTLPEndpoint string` (mirrors the existing `ReporterOptions.ReporterImage` field shape, lines 74-80) as the new option callers populate.

**Call-site wiring (D-06 spawn gating):** `internal/controller/dispatch_helpers.go`'s `spawnReporterIfNeeded` (lines 80-130) is where `ReporterOptions{ReporterImage: reporterImage}` is currently constructed (line 119-120) and is the natural place to also read the OTLP endpoint (`os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")`, matching `internal/otelinit/provider.go` line 61's own read) and skip the trace-only spawn path entirely when empty — mirrors the "same value it forwards into Job env" framing in CONTEXT.md D-06 verbatim.

---

### `internal/controller/reporter_jobspec_test.go` (modify)

**Analog:** same file's `TestBuildReporterJob_Image` (lines 368-394) — asserts a single field (`opts.ReporterImage` → `job.Spec.Template.Spec.Containers[0].Image`) round-trips correctly; extend with an analogous `TestBuildReporterJob_OTLPEndpointEnv` asserting the new conditional `Env` entries appear (and a companion `TestBuildReporterJob_NoOTLPEndpointNoEnv` asserting absence when `opts.OTLPEndpoint == ""`, mirroring `TestBuildReporterJob_EmptyImageStillBuilds`'s "still builds when empty" pattern, lines 436+).

---

## Shared Patterns

### Bounded-flush deferred Shutdown (D-12 / TRACE-03)
**Source:** `cmd/manager/main.go` lines 280-290
**Apply to:** `cmd/tide-reporter/main.go`'s `runWithClient` (NOT `main()` — Pitfall 5)
```go
defer func() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := otelShutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "otel shutdown failed") // or fmt.Fprintf(stderr, ...) in tide-reporter
	}
}()
```

### Redact-before-truncate ordering (D-09, non-negotiable)
**Source:** RESEARCH.md Code Examples ("Verified pattern: redact-then-truncate ordering")
**Apply to:** every call site in `tracesynth.go`'s `EmitSpans` that constructs a message's content string before handing it to `otelai.LLMInputMessages`/`LLMOutputMessages`
```go
content := redact.String(rawMessageContent)              // MSG-02, always first
content = truncateHeadTail(content, 32*1024)              // D-08 shape, AFTER redaction only
```
This is the single most safety-critical pattern in the phase — CLAUDE.md and RESEARCH.md both flag it as "the same class of bug" as the existing `RedactingWriter`'s tail-keep buffer rationale. Any test authored against this call site must assert ordering, not just final output (a test that only checks "secret absent from output" cannot distinguish correct-order-redaction from a lucky non-split case).

### `openinference.span.kind` attribute discipline
**Source:** `pkg/otelai/attrs.go`'s `AgentInvocation` (hardcodes `semconv.SpanKindAgent`) — `tracesynth.go`'s LLM spans need the sibling `semconv.SpanKindLLM` value instead; there is no existing `LLMInvocation`-style helper, so either add one (mirroring `AgentInvocation`'s five-attribute shape but for LLM kind) or set `attribute.String(semconv.OpenInferenceSpanKind, semconv.SpanKindLLM)` inline at the `tracer.Start`/`SetAttributes` call site in `tracesynth.go`. Prefer the helper (keeps `pkg/otelai` as the single place spec-key resolution happens, per ATTR-03 and the "thin helpers" doc.go contract) — this is itself one of the "new implementation task" items RESEARCH.md flags, not purely additive to existing helpers.

### D-O5 guard-test non-regression
**Source:** `pkg/otelai/attrs_test.go` lines 187-236 (`TestNoPayloadHelperOnPublicSurface`, `TestKeysUseSemconvModule`)
**Apply to:** every new `pkg/otelai` helper this phase adds
Both guards MUST still pass after any `attrs.go` edit. Extend the `forbidden` slice (line 198) additively if a new name class needs blocking; never delete an assertion. New spec-family attribute keys (`message.tool_calls.*`, `message.contents.*`) MUST resolve via `semconv.*` constants — a hand-rolled `"message.tool_calls."` string literal anywhere outside a comment fails `TestKeysUseSemconvModule`.

### Best-effort exit 0 / never gate on tracing (D-10)
**Source:** Phase 42 D-04 precedent (`otelai.EnvelopeDegraded()`), extended by this phase's D-05/D-10/D-11
**Apply to:** `cmd/tide-reporter/main.go`'s trace-only mode AND `tracesynth.go`'s synth/export error paths
Every synth/export failure logs to stderr and returns/continues — it must never change `runWithClient`'s existing `exitSuccess`/`exitGenericFail`/`exitInvariant` decision for the MATERIALIZATION half of a combined run. A pure trace-only run (no materialization) exits 0 regardless of synth/export outcome.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/reporter/testdata/events.jsonl` (or similar synthetic fixture) | test fixture | file-I/O | No committed `events.jsonl`-shaped fixture exists in-repo yet; RESEARCH.md's own Wave-0 gap list explicitly forbids importing the real dogfood fixtures verbatim (unknown provenance/size for CI, and the redaction test needs a KNOWN injected secret the real fixtures don't have). Author from the schema documented in RESEARCH.md's "Architecture Patterns" section (message_start/content_block_*/message_stop cycles, one tool_use + one thinking block, one tool_result carrying a fake-but-pattern-matching secret) rather than copying an existing file. |
| Fake OTLP/`SpanExporter` stub proving Shutdown-on-every-exit-path (TRACE-03) | test double | event-driven | No fake `trace.SpanExporter` recording `Export`/`Shutdown` calls exists anywhere in the repo (`tracetest.InMemoryExporter` + `WithSyncer` is synchronous and bypasses the exact async-batch-then-flush path TRACE-03 tests). Write inline in `cmd/tide-reporter/main_test.go` following `buildFakeClient`'s "construct only what the test needs" minimalism — see Pattern Assignments above. |
| `LLMInvocation`-style span-kind=LLM attribute helper in `pkg/otelai` | utility | transform | `AgentInvocation` hardcodes `SpanKindAgent`; no sibling LLM-kind helper exists. This is genuinely new implementation work RESEARCH.md flags explicitly ("a real implementation task this phase must schedule, not a doc-only decision") — closest available shape to mirror is `AgentInvocation` itself (see Shared Patterns above). |

## Metadata

**Analog search scope:** `internal/reporter/`, `internal/controller/` (span_emission*, reporter_jobspec*, dispatch_helpers.go), `internal/harness/redact/`, `internal/otelinit/`, `pkg/otelai/`, `cmd/tide-reporter/`, `cmd/manager/main.go`, `internal/subagent/anthropic/` (stream_parser.go), `internal/dispatch/podjob/jobspec.go`
**Files scanned:** 24 read in full or targeted (materialize.go, main.go ×2, attrs.go, attrs_test.go, doc.go, redact.go, redact_test.go, patterns.go, redact/doc.go, provider.go, provider_test.go, span_emission.go, span_emission_unit_test.go, span_emission_test.go, tracecontext.go, reporter_jobspec.go, reporter_jobspec_test.go (grepped), stream_parser.go, jobspec.go (targeted), dispatch_helpers.go (grepped), task_controller.go (grepped), task_types.go (grepped), envelope.go (grepped), manager main.go otel section)
**Pattern extraction date:** 2026-07-16
