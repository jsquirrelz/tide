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
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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

// TestSeedPrompt_RejectsPromptPathOutsideWorkspace — promptPath comes from
// the subagent-writable in.json, so a traversal or symlink pointing outside
// workspaceRoot must degrade (no seed, first call Degraded) instead of
// reading reporter-privileged files into the trace stream. An in-workspace
// promptPath keeps working through the confined resolution.
func TestSeedPrompt_RejectsPromptPathOutsideWorkspace(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "children"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// A perfectly valid prompt artifact sitting OUTSIDE the workspace root.
	const outsideArtifact = `{"spec":{"prompt":"stolen file contents"}}`
	outsidePath := filepath.Join(base, "outside.json")
	if err := os.WriteFile(outsidePath, []byte(outsideArtifact), 0o644); err != nil {
		t.Fatalf("WriteFile outside.json: %v", err)
	}
	// And a valid one INSIDE it.
	if err := os.WriteFile(filepath.Join(workspace, "children", "task-01.json"),
		[]byte(`{"spec":{"prompt":"legit prompt"}}`), 0o644); err != nil {
		t.Fatalf("WriteFile task-01.json: %v", err)
	}
	// A symlink INSIDE the workspace pointing OUTSIDE it.
	if err := os.Symlink(outsidePath, filepath.Join(workspace, "children", "sneaky-link.json")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	writeInJSON := func(t *testing.T, promptPath string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "in.json")
		if err := os.WriteFile(p, []byte(`{"promptPath":`+strconv.Quote(promptPath)+`}`), 0o644); err != nil {
			t.Fatalf("WriteFile in.json: %v", err)
		}
		return p
	}

	cases := []struct {
		name       string
		promptPath string
		wantOK     bool
		wantPrompt string
	}{
		{"in-workspace path resolves", "children/task-01.json", true, "legit prompt"},
		{"dot-dot traversal rejected", "../outside.json", false, ""},
		{"absolute path rejected", outsidePath, false, ""},
		{"symlink escape rejected", "children/sneaky-link.json", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prompt, ok := seedPrompt(writeInJSON(t, tc.promptPath), workspace)
			if ok != tc.wantOK || prompt != tc.wantPrompt {
				t.Errorf("seedPrompt = (%q, %v), want (%q, %v)", prompt, ok, tc.wantPrompt, tc.wantOK)
			}
		})
	}

	// End-to-end posture: a traversal promptPath degrades the first call
	// (D-05 marker) rather than erroring the reconstruction.
	inJSON := writeInJSON(t, "../outside.json")
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", inJSON, workspace)
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("got 0 calls, want at least 1")
	}
	if !calls[0].Degraded {
		t.Errorf("calls[0].Degraded = false, want true (rejected promptPath degrades the seed)")
	}
	for _, m := range calls[0].InputMessages {
		if strings.Contains(m.Content, "stolen file contents") {
			t.Errorf("out-of-workspace file contents leaked into the seed turn")
		}
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

// TestReconstructConversation_OversizedLineReturnsPartial — D-05/D-11: one
// line over common.ReadLines's 16 MB budget makes the scanner unable to
// resume (bufio.ErrTooLong), but every call reconstructed BEFORE the bad
// line is still returned alongside the error — including the still-open
// pending call, flushed as Degraded — never a bare error with zero calls.
func TestReconstructConversation_OversizedLineReturnsPartial(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")

	var b strings.Builder
	// Call 1: complete message_start..message_stop cycle.
	b.WriteString(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":10}}}}` + "\n")
	b.WriteString(`{"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"first"}]}}` + "\n")
	b.WriteString(`{"type":"stream_event","event":{"type":"message_stop"}}` + "\n")
	// Call 2: opened but never stopped — the oversized line kills the read.
	b.WriteString(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_2","model":"claude-test"}}}` + "\n")
	b.WriteString(`{"type":"assistant","message":{"id":"msg_2","content":[{"type":"text","text":"second"}]}}` + "\n")
	b.WriteString(strings.Repeat("x", 17*1024*1024) + "\n") // > 16 MB line budget
	if err := os.WriteFile(eventsPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	calls, err := ReconstructConversation(eventsPath, "", "")
	if err == nil {
		t.Fatal("expected a read error for the oversized line, got nil")
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (1 complete + 1 pending flushed on read error)", len(calls))
	}
	if calls[0].OutputMessages[0].Content != "first" {
		t.Errorf("calls[0] output = %q, want %q", calls[0].OutputMessages[0].Content, "first")
	}
	if !calls[1].Degraded {
		t.Errorf("calls[1].Degraded = false, want true (pending call flushed on read error)")
	}
	if calls[1].OutputMessages[0].Content != "second" {
		t.Errorf("calls[1] output = %q, want %q (pending content preserved)", calls[1].OutputMessages[0].Content, "second")
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
	if err := EmitSpans(context.Background(), tracer, calls, artifactPath, "", "", nil, "", ""); err != nil {
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
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
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
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
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

// TestEmitSpans_SignatureRedactedTruncatedAndCounted — MSG-02/MSG-03: a
// reasoning block's Signature passes the same redact-then-truncate pipeline
// as every other content string and counts toward the whole-span budget —
// it is stream-derived (subagent-writable) like Content and must not be the
// one unbounded, unredacted string in the pipeline.
func TestEmitSpans_SignatureRedactedTruncatedAndCounted(t *testing.T) {
	t.Run("redacted", func(t *testing.T) {
		exp := setupSpanExporter(t)
		tracer := otel.Tracer("test")

		calls := []CallSpan{{
			Model: "claude-test",
			OutputMessages: []otelai.Message{{
				Role:     "assistant",
				Contents: []otelai.MessageContent{{Type: "reasoning", Text: "thinking", Signature: "sig-" + testSecret}},
			}},
		}}
		if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
			t.Fatalf("EmitSpans: %v", err)
		}
		for _, span := range exp.GetSpans() {
			for _, attr := range span.Attributes {
				if v := attr.Value.Emit(); strings.Contains(v, "sk-ant-api03-") {
					t.Errorf("attribute %q leaked secret via Signature: %q", attr.Key, v)
				}
			}
		}
	})

	t.Run("truncated over per-message floor", func(t *testing.T) {
		exp := setupSpanExporter(t)
		tracer := otel.Tracer("test")

		calls := []CallSpan{{
			Model: "claude-test",
			OutputMessages: []otelai.Message{{
				Role:     "assistant",
				Contents: []otelai.MessageContent{{Type: "reasoning", Text: "t", Signature: strings.Repeat("s", 40000)}},
			}},
		}}
		if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
			t.Fatalf("EmitSpans: %v", err)
		}
		spans := exp.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("got %d spans, want 1", len(spans))
		}
		val, ok := attrValue(spans[0].Attributes, "llm.output_messages.0.message.contents.0.message_content.signature")
		if !ok {
			t.Fatal("span missing the signature attribute")
		}
		if got := val.AsString(); len(got) >= 40000 || !strings.Contains(got, "bytes truncated by TIDE") {
			t.Errorf("signature not head+tail truncated: len=%d", len(got))
		}
	})

	t.Run("counts toward whole-span budget", func(t *testing.T) {
		exp := setupSpanExporter(t)
		tracer := otel.Tracer("test")

		// 20 reasoning blocks × 30 KB signatures = 600,000 B of signature
		// bytes alone (each under the 32 KiB per-message floor) — over the
		// 512 KiB joint budget only if signatures are counted.
		contents := make([]otelai.MessageContent, 0, 20)
		for range 20 {
			contents = append(contents, otelai.MessageContent{Type: "reasoning", Text: "t", Signature: strings.Repeat("s", 30000)})
		}
		calls := []CallSpan{{
			Model:          "claude-test",
			OutputMessages: []otelai.Message{{Role: "assistant", Contents: contents}},
		}}
		if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
			t.Fatalf("EmitSpans: %v", err)
		}
		spans := exp.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("got %d spans, want 1", len(spans))
		}
		if val, ok := attrValue(spans[0].Attributes, "llm.output_messages.0.message.contents.0.message_content.signature"); ok && val.AsString() != "" {
			t.Errorf("signature attribute survived a budget degrade: %d bytes", len(val.AsString()))
		}
		degraded, ok := attrValue(spans[0].Attributes, "tide.trace.parse_degraded")
		if !ok || !degraded.AsBool() {
			t.Errorf("span missing tide.trace.parse_degraded=true (signature bytes must trip the budget)")
		}
	})
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
	if err := EmitSpans(context.Background(), tracer, calls, artifactPath, "", "", nil, "", ""); err != nil {
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
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
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
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
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

// TestEmitSpans_EnrichmentTriple — 46 OBS-02/OBS-03: when sessionID,
// metadataJSON, and tags are all set, EVERY emitted LLM span carries
// session.id, metadata (the JSON string verbatim), and tag.tags as a native
// STRINGSLICE — asserted across a multi-call fixture.
func TestEmitSpans_EnrichmentTriple(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{
		{Model: "claude-test-1", InputMessages: []otelai.Message{{Role: "user", Content: "hi"}}},
		{Model: "claude-test-2", InputMessages: []otelai.Message{{Role: "user", Content: "again"}}},
	}
	const metadataJSON = `{"level":"task"}`
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl",
		"uid-1", metadataJSON, []string{"task", "strict"}, "", ""); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	for i, span := range spans {
		sessionVal, ok := attrValue(span.Attributes, "session.id")
		if !ok || sessionVal.AsString() != "uid-1" {
			t.Errorf("span[%d] session.id = %v (ok=%v), want %q", i, sessionVal, ok, "uid-1")
		}
		metaVal, ok := attrValue(span.Attributes, "metadata")
		if !ok || metaVal.AsString() != metadataJSON {
			t.Errorf("span[%d] metadata = %v (ok=%v), want %q", i, metaVal, ok, metadataJSON)
		}
		tagsVal, ok := attrValue(span.Attributes, "tag.tags")
		if !ok {
			t.Errorf("span[%d] missing tag.tags", i)
			continue
		}
		if tagsVal.Type() != attribute.STRINGSLICE {
			t.Errorf("span[%d] tag.tags type = %v, want attribute.STRINGSLICE", i, tagsVal.Type())
		}
		want := []string{"task", "strict"}
		if got := tagsVal.AsStringSlice(); !reflect.DeepEqual(got, want) {
			t.Errorf("span[%d] tag.tags = %v, want %v", i, got, want)
		}
	}
}

// TestEmitSpans_EnrichmentTripleOmittedWhenEmpty — 46 OBS-02/OBS-03: when
// sessionID, metadataJSON, and tags are all empty/nil, none of the three
// enrichment attribute keys appear on any span (absent, never fabricated).
func TestEmitSpans_EnrichmentTripleOmittedWhenEmpty(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{{Model: "claude-test", InputMessages: []otelai.Message{{Role: "user", Content: "hi"}}}}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	for _, key := range []attribute.Key{"session.id", "metadata", "tag.tags"} {
		if _, ok := attrValue(spans[0].Attributes, key); ok {
			t.Errorf("span carries %q attribute; want omitted when input is empty", key)
		}
	}
}

// TestEmitSpans_TokenCountUnchangedWithEnrichment — D-03: per-call
// llm.token_count.* attributes stay present and unaffected when the
// enrichment triple is also set — the two attribute families are additive,
// not mutually exclusive.
func TestEmitSpans_TokenCountUnchangedWithEnrichment(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{{
		Model:         "claude-test",
		InputMessages: []otelai.Message{{Role: "user", Content: "hi"}},
		Usage:         Usage{InputTokens: 10, OutputTokens: 5},
	}}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "uid-1", "", nil, "", ""); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}
	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if _, ok := attrValue(spans[0].Attributes, "llm.token_count.prompt"); !ok {
		t.Errorf("span missing llm.token_count.prompt when enrichment triple is also set")
	}
}

// TestEmitSpans_LoopIdentityIndexed — 50 D-01/D-05: when attemptID is set,
// every emitted LLM span carries loop.run_id == attemptID and a 1-indexed
// loop.iteration matching the call's position in calls (1, 2, 3 across a
// 3-CallSpan fixture) — the LLM-span correlating subset. loopRunID is
// threaded but not asserted here (signature symmetry only, not yet stamped).
func TestEmitSpans_LoopIdentityIndexed(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{
		{Model: "claude-test-1", InputMessages: []otelai.Message{{Role: "user", Content: "one"}}},
		{Model: "claude-test-2", InputMessages: []otelai.Message{{Role: "user", Content: "two"}}},
		{Model: "claude-test-3", InputMessages: []otelai.Message{{Role: "user", Content: "three"}}},
	}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl",
		"", "", nil, "abc-2", "abc"); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("got %d spans, want 3", len(spans))
	}
	for i, span := range spans {
		runIDVal, ok := attrValue(span.Attributes, "loop.run_id")
		if !ok || runIDVal.AsString() != "abc-2" {
			t.Errorf("span[%d] loop.run_id = %v (ok=%v), want %q", i, runIDVal, ok, "abc-2")
		}
		iterVal, ok := attrValue(span.Attributes, "loop.iteration")
		if !ok || iterVal.AsInt64() != int64(i+1) {
			t.Errorf("span[%d] loop.iteration = %v (ok=%v), want %d", i, iterVal, ok, i+1)
		}
	}
}

// TestEmitSpans_LoopIdentityOmittedWhenEmpty — 50 D-01/D-05: when attemptID
// is empty, neither loop.run_id nor loop.iteration appears on any span
// (absent when empty, never a fabricated empty value).
func TestEmitSpans_LoopIdentityOmittedWhenEmpty(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	calls := []CallSpan{{Model: "claude-test", InputMessages: []otelai.Message{{Role: "user", Content: "hi"}}}}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	for _, key := range []attribute.Key{"loop.run_id", "loop.iteration"} {
		if _, ok := attrValue(spans[0].Attributes, key); ok {
			t.Errorf("span carries %q attribute; want omitted when attemptID is empty", key)
		}
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

	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
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

// TestEmitSpans_ZeroTimeFallbacksPreserveOrdering — a CallSpan with either
// timestamp zero (interpolation found no user event on that side) must still
// emit EndTime >= StartTime: the missing side collapses onto the known side
// (zero duration) rather than falling back to time.Now() on one side only,
// which produced negative-duration first-call spans and latency-inflated
// last-call spans on every real multi-call conversation.
func TestEmitSpans_ZeroTimeFallbacksPreserveOrdering(t *testing.T) {
	historical := time.Now().Add(-10 * time.Minute)

	cases := []struct {
		name  string
		start time.Time
		end   time.Time
	}{
		{"zero start, historical end (first call)", time.Time{}, historical},
		{"historical start, zero end (last call)", historical, time.Time{}},
		{"both zero", time.Time{}, time.Time{}},
		{"end before start (defensive clamp)", historical.Add(time.Minute), historical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp := setupSpanExporter(t)
			tracer := otel.Tracer("test")

			calls := []CallSpan{{Model: "claude-test", StartTime: tc.start, EndTime: tc.end, TimingSynthetic: true}}
			if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
				t.Fatalf("EmitSpans: %v", err)
			}
			spans := exp.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("got %d spans, want 1", len(spans))
			}
			span := spans[0]
			if span.EndTime.Before(span.StartTime) {
				t.Errorf("span EndTime %v before StartTime %v — negative duration", span.EndTime, span.StartTime)
			}
			if !tc.start.IsZero() && !span.StartTime.Equal(tc.start) && !tc.end.Before(tc.start) {
				t.Errorf("span StartTime = %v, want the supplied historical %v", span.StartTime, tc.start)
			}
			synthetic, ok := attrValue(span.Attributes, "tide.trace.timing_synthetic")
			if !ok || !synthetic.AsBool() {
				t.Errorf("span missing tide.trace.timing_synthetic=true")
			}
		})
	}
}

// TestEmitSpans_WholeSpanBudgetJointAcrossSides — CR-01: maxSpanPayloadBytes
// bounds the SUM of one span's input+output attribute bytes, not each side
// independently. Two sides each under the cap alone but jointly over it must
// degrade — larger side first, remaining side kept when it fits alone — so a
// 6-span batch stays under the 4 MiB OTLP ceiling.
func TestEmitSpans_WholeSpanBudgetJointAcrossSides(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	// input 360,000 B + output 240,000 B: each side < 512 KiB (524,288 B)
	// alone; the sum (600,000 B) exceeds the joint budget.
	inputMsgs := make([]otelai.Message, 0, 12)
	for range 12 {
		inputMsgs = append(inputMsgs, otelai.Message{Role: "user", Content: strings.Repeat("z", 30000)})
	}
	outputMsgs := make([]otelai.Message, 0, 8)
	for range 8 {
		outputMsgs = append(outputMsgs, otelai.Message{Role: "assistant", Content: strings.Repeat("w", 30000)})
	}
	calls := []CallSpan{
		{Model: "claude-test", InputMessages: inputMsgs, OutputMessages: outputMsgs},
	}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	span := spans[0]

	// The larger (input) side degraded to role-only.
	for i := range inputMsgs {
		key := attribute.Key(fmt.Sprintf("llm.input_messages.%d.message.content", i))
		if val, ok := attrValue(span.Attributes, key); ok && val.AsString() != "" {
			t.Errorf("degraded input side attribute %q = %d bytes, want empty", key, len(val.AsString()))
		}
	}
	// The smaller (output) side fits the budget alone and keeps content.
	outVal, ok := attrValue(span.Attributes, "llm.output_messages.0.message.content")
	if !ok || len(outVal.AsString()) != 30000 {
		t.Errorf("output side content len = %d (found=%v), want 30000 (kept — fits budget alone)", len(outVal.AsString()), ok)
	}
	degraded, ok := attrValue(span.Attributes, "tide.trace.parse_degraded")
	if !ok || !degraded.AsBool() {
		t.Errorf("span missing tide.trace.parse_degraded=true for joint-budget degrade")
	}

	// The whole-span invariant CR-01 exists for: total message attribute
	// bytes on the span stay under maxSpanPayloadBytes.
	totalMsgBytes := 0
	for _, a := range span.Attributes {
		k := string(a.Key)
		if strings.HasPrefix(k, "llm.input_messages.") || strings.HasPrefix(k, "llm.output_messages.") {
			totalMsgBytes += len(a.Value.Emit())
		}
	}
	if totalMsgBytes > maxSpanPayloadBytes {
		t.Errorf("span message attribute bytes = %d, want <= %d (joint whole-span budget)", totalMsgBytes, maxSpanPayloadBytes)
	}
}

// TestEmitSpans_WholeSpanBudgetBothSidesOver — CR-01 second tier: when the
// side surviving the first degrade STILL exceeds the joint budget alone,
// both sides degrade to role-only.
func TestEmitSpans_WholeSpanBudgetBothSidesOver(t *testing.T) {
	exp := setupSpanExporter(t)
	tracer := otel.Tracer("test")

	// input 600,000 B and output 570,000 B: each side alone exceeds the
	// 512 KiB budget.
	inputMsgs := make([]otelai.Message, 0, 20)
	for range 20 {
		inputMsgs = append(inputMsgs, otelai.Message{Role: "user", Content: strings.Repeat("z", 30000)})
	}
	outputMsgs := make([]otelai.Message, 0, 19)
	for range 19 {
		outputMsgs = append(outputMsgs, otelai.Message{Role: "assistant", Content: strings.Repeat("w", 30000)})
	}
	calls := []CallSpan{
		{Model: "claude-test", InputMessages: inputMsgs, OutputMessages: outputMsgs},
	}
	if err := EmitSpans(context.Background(), tracer, calls, "envelopes/task-uid/events.jsonl", "", "", nil, "", ""); err != nil {
		t.Fatalf("EmitSpans: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	for _, a := range spans[0].Attributes {
		k := string(a.Key)
		if strings.HasSuffix(k, ".message.content") && a.Value.AsString() != "" {
			t.Errorf("attribute %q = %d bytes, want empty (both sides role-only)", k, len(a.Value.AsString()))
		}
	}
	degraded, ok := attrValue(spans[0].Attributes, "tide.trace.parse_degraded")
	if !ok || !degraded.AsBool() {
		t.Errorf("span missing tide.trace.parse_degraded=true for double-sided degrade")
	}
}
