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
// repo so that any future drift in either Arize's OpenInference spec OR our
// implementation surfaces loudly. The "no payload helper" test (Test 6) is
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
	"os"
	"path/filepath"
	"reflect"
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

// TestEmptyInputsNoPanic — defensive against nil / empty slice arguments.
// Result may be either nil or an empty slice — both are acceptable. The
// invariant: NO PANIC, NO out-of-bounds.
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
