/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reporter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jsquirrelz/tide/pkg/otelai"
)

// testSecret is the exact Anthropic-key-shaped fixture string already
// proven against internal/harness/redact's TestRedactingWriter/TestString
// tables — reused here so MSG-02's redaction tests exercise the project's
// single source of truth for secret patterns rather than a second fixture.
const testSecret = "sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV"

// ─── ReconstructConversation (Task 1, MSG-01 core) ─────────────────────────

// TestReconstructConversation is the MSG-01 core happy path against
// testdata/events_sample.jsonl (3 message_start..message_stop cycles) seeded
// from testdata/in_planner.json.
func TestReconstructConversation(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want exactly 3", len(calls))
	}

	// Call 1's input is exactly the seeded prompt turn.
	call1 := calls[0]
	if len(call1.InputMessages) != 1 {
		t.Fatalf("call1 InputMessages len = %d, want 1 (seed turn only)", len(call1.InputMessages))
	}
	if call1.InputMessages[0].Role != "user" {
		t.Errorf("call1 InputMessages[0].Role = %q, want %q", call1.InputMessages[0].Role, "user")
	}
	if call1.InputMessages[0].Content != "Investigate the plan and confirm tests pass." {
		t.Errorf("call1 InputMessages[0].Content = %q, want the in.json prompt", call1.InputMessages[0].Content)
	}

	// Call 1's output aggregates BOTH the thinking block and the tool_use
	// block into ONE assistant message (Pitfall 1).
	if len(call1.OutputMessages) != 1 {
		t.Fatalf("call1 OutputMessages len = %d, want 1 (aggregated)", len(call1.OutputMessages))
	}
	out1 := call1.OutputMessages[0]
	if len(out1.Contents) != 1 || out1.Contents[0].Type != "reasoning" {
		t.Errorf("call1 output Contents = %+v, want one reasoning block", out1.Contents)
	}
	if len(out1.ToolCalls) != 1 || out1.ToolCalls[0].Name != "Read" {
		t.Errorf("call1 output ToolCalls = %+v, want one Read tool call", out1.ToolCalls)
	}

	// Call 2's input grew by at least 2 turns versus call 1's input: the
	// assistant turn call 1 produced, plus at least one tool_result turn
	// (this fixture has two tool_result events after call 1).
	call2 := calls[1]
	if len(call2.InputMessages) < len(call1.InputMessages)+2 {
		t.Errorf("call2 InputMessages len = %d, want >= call1's (%d) + 2", len(call2.InputMessages), len(call1.InputMessages))
	}

	// Call 2's output is text-free (tool_use only); call 3's output is
	// text-only.
	out2 := calls[1].OutputMessages[0]
	if len(out2.ToolCalls) != 1 || out2.ToolCalls[0].Name != "Bash" {
		t.Errorf("call2 output ToolCalls = %+v, want one Bash tool call", out2.ToolCalls)
	}

	out3 := calls[2].OutputMessages[0]
	if out3.Content != "All tests pass. Task complete." {
		t.Errorf("call3 output Content = %q, want the fixture's text block", out3.Content)
	}
	if len(out3.ToolCalls) != 0 || len(out3.Contents) != 0 {
		t.Errorf("call3 output should carry no tool calls/reasoning blocks, got ToolCalls=%+v Contents=%+v", out3.ToolCalls, out3.Contents)
	}

	for i, c := range calls {
		if !c.TimingSynthetic {
			t.Errorf("calls[%d].TimingSynthetic = false, want true (no in-band absolute call timestamp exists)", i)
		}
		if c.Degraded {
			t.Errorf("calls[%d].Degraded = true, want false (clean fixture)", i)
		}
	}
}

// TestReconstructConversation_SeedsPromptFromPromptPath — Pitfall 2: an
// executor-shaped in.json (.prompt empty, .promptPath set) seeds turn 0 from
// the referenced children/task-NN.json's .spec.prompt, one hop away.
func TestReconstructConversation_SeedsPromptFromPromptPath(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_executor.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("got 0 calls, want at least 1")
	}
	if len(calls[0].InputMessages) != 1 {
		t.Fatalf("call1 InputMessages len = %d, want 1", len(calls[0].InputMessages))
	}
	want := "Fix the flaky test and open a PR."
	if got := calls[0].InputMessages[0].Content; got != want {
		t.Errorf("call1 InputMessages[0].Content = %q, want %q (children/task-01.json's .spec.prompt)", got, want)
	}
}

// TestReconstructConversation_PerCallUsage — D-04: each CallSpan's Usage
// carries that call's message_start input/cache tokens plus the
// message_delta output_tokens.
func TestReconstructConversation_PerCallUsage(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}

	wantUsage := []Usage{
		{InputTokens: 120, OutputTokens: 45, CacheReadTokens: 5, CacheCreationTokens: 10},
		{InputTokens: 300, OutputTokens: 30, CacheReadTokens: 120, CacheCreationTokens: 0},
		{InputTokens: 350, OutputTokens: 12, CacheReadTokens: 300, CacheCreationTokens: 0},
	}
	for i, want := range wantUsage {
		if got := calls[i].Usage; got != want {
			t.Errorf("calls[%d].Usage = %+v, want %+v", i, got, want)
		}
	}
}

// TestReconstructConversation_TolerantSkip — D-05/D-11: a fixture with a
// non-JSON garbage line AND a dangling message_start (no message_stop) still
// returns the prior complete call normally and the dangling call marked
// Degraded — never an error, never a panic.
func TestReconstructConversation_TolerantSkip(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_truncated.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (1 complete + 1 dangling)", len(calls))
	}
	if calls[0].Degraded {
		t.Errorf("calls[0].Degraded = true, want false (complete call)")
	}
	if !calls[1].Degraded {
		t.Errorf("calls[1].Degraded = false, want true (dangling call, no message_stop + garbage line)")
	}
}

// TestReconstructConversation_MissingInJSON — D-05: an absent in.json
// reconstructs the conversation without a seed turn, marking only the first
// CallSpan Degraded — never an error.
func TestReconstructConversation_MissingInJSON(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/does-not-exist.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}
	if len(calls[0].InputMessages) != 0 {
		t.Errorf("call1 InputMessages = %+v, want empty (no seed turn, no prior turns)", calls[0].InputMessages)
	}
	if !calls[0].Degraded {
		t.Errorf("calls[0].Degraded = false, want true (missing in.json)")
	}
	if calls[1].Degraded {
		t.Errorf("calls[1].Degraded = true, want false (degraded marker scoped to first call only)")
	}
}

// ─── EmitSpans (Task 2, MSG-02/MSG-03) ─────────────────────────────────────

// setupSpanExporter swaps the global TracerProvider for an in-memory one
// (synchronous WithSyncer — no flush needed), restoring the previous
// provider via t.Cleanup. Copied verbatim from
// internal/controller/span_emission_unit_test.go's pattern per PATTERNS.md.
func setupSpanExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}

// attrValue scans a span's attributes for key, returning the raw value and
// whether it was found. Copied verbatim from span_emission_unit_test.go.
func attrValue(attrs []attribute.KeyValue, key attribute.Key) (attribute.Value, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return attribute.Value{}, false
}

// TestEmitSpans_SpanShape — the MSG-01/ATTR-01-style happy path: one LLM
// span per reconstructed CallSpan, attribute-complete.
func TestEmitSpans_SpanShape(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}

	const artifactPath = "envelopes/task-uid/events.jsonl"
	if err := EmitSpans(context.Background(), tracer, calls, artifactPath); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	for i, span := range spans {
		if span.Name != calls[i].Model {
			t.Errorf("span[%d].Name = %q, want %q", i, span.Name, calls[i].Model)
		}
		wantAttrs := map[attribute.Key]string{
			"openinference.span.kind": "LLM",
			"llm.provider":            "anthropic",
			"llm.model_name":          calls[i].Model,
			"tide.artifact_path":      artifactPath,
		}
		for key, want := range wantAttrs {
			val, ok := attrValue(span.Attributes, key)
			if !ok {
				t.Errorf("span[%d] missing attribute %q", i, key)
				continue
			}
			if val.AsString() != want {
				t.Errorf("span[%d] attribute %q = %q, want %q", i, key, val.AsString(), want)
			}
		}
		synthetic, ok := attrValue(span.Attributes, "tide.trace.timing_synthetic")
		if !ok || !synthetic.AsBool() {
			t.Errorf("span[%d] missing tide.trace.timing_synthetic=true", i)
		}
		if _, ok := attrValue(span.Attributes, "llm.token_count.prompt"); !ok {
			t.Errorf("span[%d] missing llm.token_count.prompt", i)
		}
	}
}

// TestEmitSpans_Redacts — MSG-02: the planted fixture secret appears in ZERO
// attribute values across ALL emitted spans.
func TestEmitSpans_Redacts(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{
		{
			Model:          "claude-test",
			InputMessages:  []otelai.Message{{Role: "user", Content: "here is a secret: " + testSecret}},
			OutputMessages: []otelai.Message{{Role: "assistant", Content: "ack"}},
		},
	}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl"); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	for _, span := range exp.GetSpans() {
		for _, attr := range span.Attributes {
			if v := attr.Value.Emit(); strings.Contains(v, "sk-ant-api03-") {
				t.Errorf("attribute %q leaked secret: %q", attr.Key, v)
			}
		}
	}
}

// TestEmitSpans_RedactsBeforeTruncate — D-09, locked: a secret positioned so
// that a naive truncate-THEN-redact pipeline would split it across the
// head/tail cut (leaving a partial, non-matching credential fragment
// visible) still shows zero secret bytes when redaction correctly runs
// FIRST. A lucky non-straddling placement could not distinguish correct
// ordering from incorrect — this fixture is deliberately positioned to.
func TestEmitSpans_RedactsBeforeTruncate(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	// truncationHalf = 16384. Position the secret so it starts a few bytes
	// before the head cut and ends after it — split by a naive
	// truncate-first pipeline, but never split by redact-first.
	prefix := strings.Repeat("a", 16370)
	filler := strings.Repeat("b", 23595)
	content := prefix + testSecret + filler // total > maxMessageContentBytes

	calls := []CallSpan{
		{
			Model:         "claude-test",
			InputMessages: []otelai.Message{{Role: "user", Content: content}},
		},
	}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl"); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	for _, span := range exp.GetSpans() {
		for _, attr := range span.Attributes {
			if v := attr.Value.Emit(); strings.Contains(v, "sk-ant-api03-") {
				t.Errorf("attribute %q leaked a straddling secret fragment: %q", attr.Key, v)
			}
		}
	}
}

// TestEmitSpans_TruncatesOversizedMessage — MSG-03/D-08: a >32 KiB single
// message emits head+marker+tail shaped content, the marker cites the
// elided byte count, and the span carries tide.artifact_path.
func TestEmitSpans_TruncatesOversizedMessage(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	content := strings.Repeat("x", 40000) // > maxMessageContentBytes (32 KiB)
	calls := []CallSpan{
		{Model: "claude-test", InputMessages: []otelai.Message{{Role: "user", Content: content}}},
	}
	const artifactPath = "envelopes/task-uid/events.jsonl"
	if err := EmitSpans(context.Background(), tracer, calls, artifactPath); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	val, ok := attrValue(spans[0].Attributes, "llm.input_messages.0.message.content")
	if !ok {
		t.Fatalf("span missing llm.input_messages.0.message.content")
	}
	got := val.AsString()
	if len(got) >= len(content) {
		t.Errorf("content not truncated: len=%d, want < %d", len(got), len(content))
	}
	wantElided := len(content) - 2*(maxMessageContentBytes/2)
	wantMarker := fmt.Sprintf("%d bytes truncated by TIDE", wantElided)
	if !strings.Contains(got, wantMarker) {
		t.Errorf("truncated content missing marker %q: %q", wantMarker, got)
	}

	artifactVal, ok := attrValue(spans[0].Attributes, "tide.artifact_path")
	if !ok || artifactVal.AsString() != artifactPath {
		t.Errorf("tide.artifact_path = %v (found=%v), want %q", artifactVal, ok, artifactPath)
	}
}

// TestEmitSpans_WholeSpanBudget — the secondary whole-span backstop: many
// real-sized (not individually oversized) messages summing past
// maxSpanPayloadBytes degrade that side to role-only content; the other
// side is unaffected.
func TestEmitSpans_WholeSpanBudget(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	inputMsgs := make([]otelai.Message, 0, 20)
	for range 20 {
		inputMsgs = append(inputMsgs, otelai.Message{Role: "user", Content: strings.Repeat("z", 30000)})
	}
	calls := []CallSpan{
		{
			Model:          "claude-test",
			InputMessages:  inputMsgs, // 600,000 B > 512 KiB whole-span budget
			OutputMessages: []otelai.Message{{Role: "assistant", Content: "short reply"}},
		},
	}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl"); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	span := spans[0]

	for i := range inputMsgs {
		key := attribute.Key(fmt.Sprintf("llm.input_messages.%d.message.content", i))
		if val, ok := attrValue(span.Attributes, key); ok && val.AsString() != "" {
			t.Errorf("degraded input side attribute %q = %q, want empty", key, val.AsString())
		}
	}

	outVal, ok := attrValue(span.Attributes, "llm.output_messages.0.message.content")
	if !ok || outVal.AsString() != "short reply" {
		t.Errorf("output side content = %v (found=%v), want %q (output side unaffected)", outVal, ok, "short reply")
	}
	if _, ok := attrValue(span.Attributes, "tide.artifact_path"); !ok {
		t.Errorf("span missing tide.artifact_path")
	}
	degraded, ok := attrValue(span.Attributes, "tide.trace.parse_degraded")
	if !ok || !degraded.AsBool() {
		t.Errorf("span missing tide.trace.parse_degraded=true for whole-span-budget degrade")
	}
}

// TestEmitSpans_DegradedMarker — D-11: a Degraded CallSpan's span carries
// tide.trace.parse_degraded=true.
func TestEmitSpans_DegradedMarker(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{{Model: "claude-test", Degraded: true}}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl"); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	degraded, ok := attrValue(spans[0].Attributes, "tide.trace.parse_degraded")
	if !ok || !degraded.AsBool() {
		t.Errorf("span missing tide.trace.parse_degraded=true")
	}
}

// recordingExporter is a minimal sdktrace.SpanExporter test double recording
// each ExportSpans call's span count and summed attribute-value bytes — the
// only way to observe BatchSpanProcessor's actual chunking behavior
// (tracetest.InMemoryExporter alone does not model batching). Constructed
// inline per PATTERNS.md's "construct only what the test needs" guidance —
// no shared test-double package.
type recordingExporter struct {
	mu      sync.Mutex
	batches [][]tracetest.SpanStub
}

func (e *recordingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.batches = append(e.batches, tracetest.SpanStubsFromReadOnlySpans(spans))
	return nil
}

func (e *recordingExporter) Shutdown(_ context.Context) error { return nil }

// TestEmitSpans_BatchAggregateUnderCeiling — Pitfall 3: the REAL size risk is
// aggregate batch export, not any single oversized span. ~32 real-sized
// CallSpans (input contexts growing to ~300 KiB, never exceeding
// maxSpanPayloadBytes) exported through a BatchSpanProcessor configured via
// OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 (44-02's reporter Job env) must chunk into
// batches of <= 6 spans, each well under the 4 MiB OTLP ceiling.
func TestEmitSpans_BatchAggregateUnderCeiling(t *testing.T) {
	t.Setenv("OTEL_BSP_MAX_EXPORT_BATCH_SIZE", "6") // must precede NewTracerProvider (read at construction)

	rec := &recordingExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(rec))
	tracer := tp.Tracer("test")

	const turnSize = 4800 // real-sized: well under the 32 KiB per-message floor
	calls := make([]CallSpan, 0, 32)
	ctxMsgs := make([]otelai.Message, 0, 64)
	for range 32 {
		ctxMsgs = append(ctxMsgs, otelai.Message{Role: "user", Content: strings.Repeat("x", turnSize)})
		input := append([]otelai.Message(nil), ctxMsgs...)
		calls = append(calls, CallSpan{
			Model:          "claude-test",
			InputMessages:  input,
			OutputMessages: []otelai.Message{{Role: "assistant", Content: strings.Repeat("y", turnSize)}},
			StartTime:      time.Now(),
			EndTime:        time.Now(),
		})
		ctxMsgs = append(ctxMsgs, otelai.Message{Role: "assistant", Content: strings.Repeat("y", turnSize)})
	}

	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl"); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	const maxBatchBytes = 3584 * 1024 // 3.5 MiB
	totalSpans := 0
	for i, batch := range rec.batches {
		if len(batch) > 6 {
			t.Errorf("batch %d has %d spans, want <= 6 (OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6)", i, len(batch))
		}
		batchBytes := 0
		for _, s := range batch {
			for _, a := range s.Attributes {
				batchBytes += len(a.Value.Emit())
			}
		}
		if batchBytes >= maxBatchBytes {
			t.Errorf("batch %d attribute bytes = %d, want < %d (3.5 MiB)", i, batchBytes, maxBatchBytes)
		}
		totalSpans += len(batch)
	}
	if totalSpans != 32 {
		t.Errorf("total spans exported across all batches = %d, want 32", totalSpans)
	}
}
