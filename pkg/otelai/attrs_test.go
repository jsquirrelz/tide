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

// Phase 4 Plan 03 Task 1: failing tests for the OpenInference attribute
// helpers (D-O4). These tests lock the spec's flat-keyed encoding into the
// repo so that any future divergence between Arize's OpenInference spec and
// our implementation surfaces loudly. The "no payload helper" test (Test 6) is
// the D-O5 enforcement at the public API surface — there must NEVER be a
// helper that accepts inline message content as a top-level attribute value.
//
// Phase 42 Plan 01: keys now resolve from the official
// openinference-semantic-conventions Go module (ATTR-03/D-05/D-06); three
// keys with no module counterpart moved to the tide.* namespace; TokenCount
// gained llm.token_count.total (ATTR-02/D-08); AgentInvocation gained a
// leading system parameter (D-07); LLMIdentity/FailureDetail/EnvelopeDegraded
// are new helpers.
package otelai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// TestLLMInputMessages — flat-keyed `llm.input_messages.<i>.message.role` /
// `.content` encoding per OpenInference spec (RESEARCH.md §625-658). Keys are
// module-backed but byte-identical to the pre-Phase-42 hand-rolled encoding.
func TestLLMInputMessages(t *testing.T) {
	got := LLMInputMessages([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	want := []attribute.KeyValue{
		attribute.String("llm.input_messages.0.message.role", "user"),
		attribute.String("llm.input_messages.0.message.content", "hi"),
		attribute.String("llm.input_messages.1.message.role", "assistant"),
		attribute.String("llm.input_messages.1.message.content", "hello"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMInputMessages = %v, want %v", got, want)
	}
	if len(got) != 4 {
		t.Errorf("LLMInputMessages returned %d entries, want exactly 4", len(got))
	}
}

// TestLLMOutputMessages — same flat encoding with `llm.output_messages.` prefix.
func TestLLMOutputMessages(t *testing.T) {
	got := LLMOutputMessages([]Message{
		{Role: "assistant", Content: "ok"},
	})
	want := []attribute.KeyValue{
		attribute.String("llm.output_messages.0.message.role", "assistant"),
		attribute.String("llm.output_messages.0.message.content", "ok"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMOutputMessages = %v, want %v", got, want)
	}
}

// TestTokenCount — five int attrs (ATTR-02/D-08): the original four-way split
// plus llm.token_count.total = prompt + completion. `prompt` is now documented
// to carry the FULL prompt count INCLUDING the cache_read/cache_write subsets
// — the re-mapping itself happens at the call site (plan 42-04), not here.
func TestTokenCount(t *testing.T) {
	got := TokenCount(1000, 300, 200, 100)
	want := []attribute.KeyValue{
		attribute.Int("llm.token_count.prompt", 1000),
		attribute.Int("llm.token_count.completion", 300),
		attribute.Int("llm.token_count.prompt_details.cache_read", 200),
		attribute.Int("llm.token_count.prompt_details.cache_write", 100),
		attribute.Int("llm.token_count.total", 1300),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TokenCount = %v, want %v", got, want)
	}
	if len(got) != 5 {
		t.Errorf("TokenCount returned %d entries, want exactly 5", len(got))
	}
}

// TestAgentInvocation — five attrs identifying the orchestrator's subagent
// dispatch site. D-07: llm.system is now a leading caller-supplied parameter,
// not a hardcoded "anthropic" constant. D-05: agent.role/agent.invocation.level
// have no module counterpart and live under tide.*.
func TestAgentInvocation(t *testing.T) {
	got := AgentInvocation("anthropic", "tide.dispatch.milestone", "planner", "milestone")
	want := []attribute.KeyValue{
		attribute.String("openinference.span.kind", "AGENT"),
		attribute.String("llm.system", "anthropic"),
		attribute.String("agent.name", "tide.dispatch.milestone"),
		attribute.String("tide.role", "planner"),
		attribute.String("tide.invocation.level", "milestone"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AgentInvocation = %v, want %v", got, want)
	}
}

// TestLLMOutputMessagesToolCalls — Phase 44 D-03: a message's ToolCalls
// entries emit spec-native message.tool_calls.<j>.tool_call.{id,function.name,
// function.arguments} attributes via the module's own
// LLMOutputMessageToolCallKey indexer, in addition to the legacy role/content
// pair. Exact key strings asserted per MSG-related acceptance criteria.
func TestLLMOutputMessagesToolCalls(t *testing.T) {
	got := LLMOutputMessages([]Message{
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "toolu_1", Name: "Bash", ArgumentsJSON: `{"command":"ls"}`},
			},
		},
	})
	want := []attribute.KeyValue{
		attribute.String("llm.output_messages.0.message.role", "assistant"),
		attribute.String("llm.output_messages.0.message.content", ""),
		attribute.String("llm.output_messages.0.message.tool_calls.0.tool_call.id", "toolu_1"),
		attribute.String("llm.output_messages.0.message.tool_calls.0.tool_call.function.name", "Bash"),
		attribute.String("llm.output_messages.0.message.tool_calls.0.tool_call.function.arguments", `{"command":"ls"}`),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMOutputMessages (tool calls) = %v, want %v", got, want)
	}
}

// TestLLMOutputMessagesReasoningContent — Phase 44 D-03: a message's
// Contents entries emit spec-native message.contents.<k>.message_content.
// {type,text,signature} attributes via constant-composed keys (no public
// module indexer exists for message.contents). Exact key strings asserted.
func TestLLMOutputMessagesReasoningContent(t *testing.T) {
	got := LLMOutputMessages([]Message{
		{
			Role: "assistant",
			Contents: []MessageContent{
				{Type: "reasoning", Text: "Let me read...", Signature: "EpICCmUIDhgC"},
			},
		},
	})
	want := []attribute.KeyValue{
		attribute.String("llm.output_messages.0.message.role", "assistant"),
		attribute.String("llm.output_messages.0.message.content", ""),
		attribute.String("llm.output_messages.0.message.contents.0.message_content.type", "reasoning"),
		attribute.String("llm.output_messages.0.message.contents.0.message_content.text", "Let me read..."),
		attribute.String("llm.output_messages.0.message.contents.0.message_content.signature", "EpICCmUIDhgC"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMOutputMessages (reasoning content) = %v, want %v", got, want)
	}
}

// TestLLMOutputMessagesReasoningContentOmitsEmptySignature — input-side
// tool_result-derived content blocks carry no signature; the attribute must
// be omitted entirely rather than emitted as an empty string.
func TestLLMOutputMessagesReasoningContentOmitsEmptySignature(t *testing.T) {
	got := LLMOutputMessages([]Message{
		{
			Role:     "assistant",
			Contents: []MessageContent{{Type: "text", Text: "hi"}},
		},
	})
	for _, kv := range got {
		if string(kv.Key) == "llm.output_messages.0.message.contents.0.message_content.signature" {
			t.Errorf("expected no signature attribute when Signature is empty, got %v", got)
		}
	}
}

// TestLLMOutputMessagesLegacyShapeUnchanged — Phase 44 backward
// compatibility (D-03): nil ToolCalls and nil Contents must emit exactly
// the legacy 2-key role/content shape, byte-identical to pre-Phase-44
// behavior — zero behavior change for Phase 42 call sites.
func TestLLMOutputMessagesLegacyShapeUnchanged(t *testing.T) {
	got := LLMOutputMessages([]Message{{Role: "assistant", Content: "ok"}})
	want := []attribute.KeyValue{
		attribute.String("llm.output_messages.0.message.role", "assistant"),
		attribute.String("llm.output_messages.0.message.content", "ok"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMOutputMessages (legacy shape) = %v, want %v", got, want)
	}
}

// TestLLMSpanKind — Phase 44 D-01/D-03: the LLM-kind sibling of
// AgentInvocation's hardcoded SpanKindAgent.
func TestLLMSpanKind(t *testing.T) {
	got := LLMSpanKind()
	want := attribute.String("openinference.span.kind", "LLM")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMSpanKind() = %v, want %v", got, want)
	}
}

// TestTimingSynthetic — Phase 44 Claude's-Discretion timing-floor marker.
func TestTimingSynthetic(t *testing.T) {
	got := TimingSynthetic()
	want := attribute.Bool("tide.trace.timing_synthetic", true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TimingSynthetic() = %v, want %v", got, want)
	}
}

// TestParseDegraded — Phase 44 D-11 marker.
func TestParseDegraded(t *testing.T) {
	got := ParseDegraded()
	want := attribute.Bool("tide.trace.parse_degraded", true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDegraded() = %v, want %v", got, want)
	}
}

// TestArtifactPath — single attribute.KeyValue (NOT a slice). D-05: the key is
// now tide.artifact_path — the gen_ai.artifact_path namespace squat is dead.
func TestArtifactPath(t *testing.T) {
	got := ArtifactPath("/workspace/envelopes/abc.jsonl")
	want := attribute.String("tide.artifact_path", "/workspace/envelopes/abc.jsonl")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ArtifactPath = %v, want %v", got, want)
	}
}

// TestLLMIdentity — ATTR-01: llm.provider always; llm.model_name ONLY when
// model is non-empty (Pitfall 5 — never emit an empty-string llm.model_name).
func TestLLMIdentity(t *testing.T) {
	t.Run("with model", func(t *testing.T) {
		got := LLMIdentity("anthropic", "claude-opus-4-8")
		want := []attribute.KeyValue{
			attribute.String("llm.provider", "anthropic"),
			attribute.String("llm.model_name", "claude-opus-4-8"),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LLMIdentity(\"anthropic\", \"claude-opus-4-8\") = %v, want %v", got, want)
		}
		if len(got) != 2 {
			t.Errorf("LLMIdentity with model returned %d entries, want exactly 2", len(got))
		}
	})

	t.Run("empty model omits llm.model_name", func(t *testing.T) {
		got := LLMIdentity("anthropic", "")
		want := []attribute.KeyValue{
			attribute.String("llm.provider", "anthropic"),
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("LLMIdentity(\"anthropic\", \"\") = %v, want %v", got, want)
		}
		if len(got) != 1 {
			t.Errorf("LLMIdentity with empty model returned %d entries, want exactly 1 (no llm.model_name)", len(got))
		}
	})
}

// TestFailureDetail — D-03: exit code + reason as tide.* span attributes
// (no module counterpart exists for either).
func TestFailureDetail(t *testing.T) {
	got := FailureDetail(2, "cap-hit")
	want := []attribute.KeyValue{
		attribute.Int("tide.exit_code", 2),
		attribute.String("tide.reason", "cap-hit"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FailureDetail(2, \"cap-hit\") = %v, want %v", got, want)
	}
}

// TestEnvelopeDegraded — D-04: single marker attribute for a span whose
// envelope could not be read.
func TestEnvelopeDegraded(t *testing.T) {
	got := EnvelopeDegraded()
	want := attribute.Bool("tide.envelope.degraded", true)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EnvelopeDegraded() = %v, want %v", got, want)
	}
}

// TestNoPayloadHelperOnPublicSurface — D-O5 enforcement at the API surface.
// Source-grep attrs.go and reject any exported function whose name suggests
// it accepts raw payload bytes/strings as an attribute value. If a future
// refactor adds `InlinePayload(...)` or `RawContent(...)` etc., this test
// fails loudly and forces a planner-checkpoint conversation.
//
// Phase 44 update (MSG-03, deliberate — the forbidden-name list itself is
// UNCHANGED): D-O5's contract evolved from "always defer to ArtifactPath" to
// "bounded-inline (redact-then-truncate) PLUS ArtifactPath co-attribute"
// (see doc.go). This guard still enforces the same invariant either way —
// no helper may accept RAW, unredacted, unbounded payload content as a
// top-level attribute value.
func TestNoPayloadHelperOnPublicSurface(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "pkg", "otelai", "attrs.go"))
	if err != nil {
		t.Fatalf("read pkg/otelai/attrs.go: %v", err)
	}
	src := string(data)

	// Forbidden exported identifiers — any of these in attrs.go is a D-O5
	// violation. The match is case-insensitive on the function NAME boundary
	// (preceded by `func ` so we don't trip on parameter docs).
	forbidden := []string{
		"func Payload",
		"func InlinePayload",
		"func RawContent",
		"func Body",
		"func MessageBody",
	}
	for _, needle := range forbidden {
		if strings.Contains(src, needle) {
			t.Errorf("D-O5 violation: pkg/otelai/attrs.go contains %q — no public helper may accept inline payload content as a top-level attribute value", needle)
		}
	}
}

// TestKeysUseSemconvModule — ATTR-03 enforcement at the source-grep level.
// Reads the comment-stripped attrs.go and fails if it contains any
// double-quoted string literal beginning with one of the four spec-family
// prefixes (llm., openinference., gen_ai., agent.) — those MUST always
// resolve from the openinference-semantic-conventions module's semconv.*
// constants, never a hand-rolled literal. Only tide.* literals may remain
// hand-rolled (D-05). Mirrors TestNoWithSamplerInSource's source-grep
// convention (internal/otelinit/provider_test.go).
//
// Deliberately does NOT assert a module version pin — D-06 explicitly
// declines that class of version-guard test; this guard enforces WHERE
// keys come from, never WHICH version of the module is in go.mod.
func TestKeysUseSemconvModule(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "pkg", "otelai", "attrs.go"))
	if err != nil {
		t.Fatalf("read pkg/otelai/attrs.go: %v", err)
	}
	stripped := stripGoComments(string(data))

	forbidden := regexp.MustCompile(`"(llm\.|openinference\.|gen_ai\.|agent\.)`)
	if m := forbidden.FindString(stripped); m != "" {
		t.Errorf("ATTR-03 violation: pkg/otelai/attrs.go contains a hand-rolled spec-family string literal (%s...) outside a comment — every spec-backed key must resolve from the openinference-semantic-conventions module (semconv.* constants); only tide.* keys may be hand-rolled (D-05)", m)
	}
}

// TestSessionID — 46 OBS-02: session.id keyed from the semconv module
// (ATTR-03), value is TIDE's own run identity (Project UID) verbatim.
func TestSessionID(t *testing.T) {
	got := SessionID("uid-123")
	want := attribute.String("session.id", "uid-123")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionID(\"uid-123\") = %v, want %v", got, want)
	}
}

// TestMetadata — 46 OBS-03: metadata is a JSON-encoded STRING (Pitfall 4),
// and round-trips via json.Unmarshal back into an equal map.
func TestMetadata(t *testing.T) {
	got, err := Metadata(map[string]string{"level": "task"})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if got.Key != "metadata" {
		t.Errorf("Metadata key = %q, want %q", got.Key, "metadata")
	}
	if got.Value.Type() != attribute.STRING {
		t.Errorf("Metadata().Value.Type() = %v, want attribute.STRING", got.Value.Type())
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(got.Value.AsString()), &decoded); err != nil {
		t.Fatalf("Metadata value did not round-trip as JSON: %v", err)
	}
	want := map[string]string{"level": "task"}
	if !reflect.DeepEqual(decoded, want) {
		t.Errorf("Metadata value decoded = %v, want %v", decoded, want)
	}
}

// TestMetadataJSON — 46 OBS-03: the pre-encoded twin of Metadata() passes
// the input string through verbatim (no re-marshal) under the same key.
func TestMetadataJSON(t *testing.T) {
	const encoded = `{"level":"task"}`
	got := MetadataJSON(encoded)
	want := attribute.String("metadata", encoded)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MetadataJSON(%q) = %v, want %v", encoded, got, want)
	}
	if got.Value.Type() != attribute.STRING {
		t.Errorf("MetadataJSON().Value.Type() = %v, want attribute.STRING", got.Value.Type())
	}
}

// TestTags — 46 OBS-03/D-06: tag.tags is a NATIVE string list
// (attribute.STRINGSLICE), NOT JSON-encoded — a Tags() helper that
// JSON-encodes is the exact Pitfall 4 regression this test guards against.
func TestTags(t *testing.T) {
	got := Tags("task", "approve")
	if got.Key != "tag.tags" {
		t.Errorf("Tags key = %q, want %q", got.Key, "tag.tags")
	}
	if got.Value.Type() != attribute.STRINGSLICE {
		t.Errorf("Tags().Value.Type() = %v, want attribute.STRINGSLICE", got.Value.Type())
	}
	want := []string{"task", "approve"}
	if gotSlice := got.Value.AsStringSlice(); !reflect.DeepEqual(gotSlice, want) {
		t.Errorf("Tags().Value.AsStringSlice() = %v, want %v", gotSlice, want)
	}
}

// TestEmptyInputsNoPanic — defensive against nil / empty slice arguments.
// Result may be either nil or an empty slice — both are acceptable. The
// invariant: NO PANIC, NO out-of-bounds.
//
// Phase 44 extension: nil/empty ToolCalls and Contents on a populated
// Message (the new D-03 extension paths) must not panic either.
func TestEmptyInputsNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LLMInputMessages(nil) panicked: %v", r)
		}
	}()
	if got := LLMInputMessages(nil); len(got) != 0 {
		t.Errorf("LLMInputMessages(nil) returned %d entries, want 0", len(got))
	}
	if got := LLMOutputMessages([]Message{}); len(got) != 0 {
		t.Errorf("LLMOutputMessages([]) returned %d entries, want 0", len(got))
	}
	// D-03 extension paths: nil and empty-slice ToolCalls/Contents.
	got := LLMOutputMessages([]Message{
		{Role: "assistant", Content: "ok", ToolCalls: nil, Contents: nil},
		{Role: "assistant", Content: "ok", ToolCalls: []ToolCall{}, Contents: []MessageContent{}},
	})
	if len(got) != 4 {
		t.Errorf("LLMOutputMessages(nil/empty ToolCalls+Contents) returned %d entries, want exactly 4 (legacy 2-per-message shape)", len(got))
	}
}

// stripGoComments removes single-line (`// ...`) and block (`/* ... */`)
// comments from Go source so that text inside comments doesn't count toward
// grep-based source assertions. Mirrored verbatim from
// internal/otelinit/provider_test.go's stripGoComments — kept as a local
// copy so pkg/otelai doesn't depend on a testing helper from another
// package. The implementation is intentionally simple — it does NOT
// understand backtick strings or context-aware lexing — but is sufficient
// for attrs.go, which has no string literal containing a backtick-quoted
// spec-family prefix.
func stripGoComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	i := 0
	for i < len(src) {
		// Block comment.
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '*' {
			end := strings.Index(src[i+2:], "*/")
			if end == -1 {
				return out.String()
			}
			i += 2 + end + 2
			continue
		}
		// Line comment.
		if i+1 < len(src) && src[i] == '/' && src[i+1] == '/' {
			nl := strings.IndexByte(src[i:], '\n')
			if nl == -1 {
				return out.String()
			}
			i += nl
			continue
		}
		out.WriteByte(src[i])
		i++
	}
	return out.String()
}

// findRepoRoot walks up from the test's CWD until it finds go.mod. Tests run
// with CWD = the package directory, so we expect 2 hops (pkg/otelai → pkg
// → repo root).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("go.mod not found from %s; cannot locate repo root", cwd)
		}
		root = parent
	}
}
