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

package otelai

import (
	"fmt"

	semconv "github.com/Arize-ai/openinference/go/openinference-semantic-conventions"
	"go.opentelemetry.io/otel/attribute"
)

// Message is the role+content shape that LLMInputMessages and
// LLMOutputMessages flatten into per-index OpenInference attribute keys.
//
// Role is one of {"user", "assistant", "system", "tool"} per the
// Arize OpenInference spec (May 2026).
//
// Content is the raw message text used ONLY for outbound serialization into
// the attribute VALUE — there is no validation, escaping, or transformation.
// Per the Phase 44 D-O5 contract (see doc.go), production callers populate
// Content with bounded-inline text that has already passed the mandatory
// redact-then-truncate pipeline (internal/harness/redact.String, then
// per-message truncation, D-09) and pair the span with an ArtifactPath
// co-attribute referencing the full-fidelity record.
//
// ToolCalls and Contents are optional Phase 44 (D-03) extensions for
// spec-native tool-call and structured-content-block (e.g. reasoning/
// "thinking") encoding. Nil (the zero value) preserves the legacy flat
// role/content-only encoding — zero behavior change for pre-Phase-44 call
// sites.
type Message struct {
	Role    string
	Content string

	// ToolCalls carries this message's tool invocations, encoded via the
	// module's own message.tool_calls indexer family (D-03: the pinned
	// openinference-semantic-conventions v0.1.1 module carries these keys,
	// so tool calls get spec-native encoding rather than being stringified
	// into Content).
	ToolCalls []ToolCall

	// Contents carries this message's structured content blocks — e.g. an
	// Anthropic `thinking` block, encoded as message_content.type=
	// "reasoning" (D-03) — via the module's message.contents family.
	Contents []MessageContent
}

// ToolCall is one tool invocation within a message's message.tool_calls
// array (D-03). ArgumentsJSON carries the raw JSON-encoded arguments string
// exactly as observed on the wire — this package does not parse, validate,
// or re-serialize it.
type ToolCall struct {
	ID            string
	Name          string
	ArgumentsJSON string
}

// MessageContent is one structured content block within a message's
// message.contents array (D-03) — e.g. a "reasoning" (Anthropic `thinking`)
// block. Signature carries the opaque provider reasoning-continuity token
// verbatim (the module's own doc comment: "opaque provider
// reasoning-continuity fields"); omitted from the emitted attributes when
// empty (input-side tool_result blocks carry none).
type MessageContent struct {
	Type      string
	Text      string
	Signature string
}

// TIDE-custom keys with no counterpart in the official
// openinference-semantic-conventions Go module (D-05 rename bucket — the
// module was downloaded and read directly at plan-authoring time; none of
// these eight exist anywhere in it). Every other attribute key emitted by
// this package resolves from the semconv.* constants below — see
// TestKeysUseSemconvModule (attrs_test.go) for the source-grep guard that
// enforces this split at PR-review time (ATTR-03).
const (
	keyAgentRole            = "tide.role"
	keyAgentInvocationLevel = "tide.invocation.level"
	keyArtifactPath         = "tide.artifact_path"
	keyExitCode             = "tide.exit_code"
	keyReason               = "tide.reason"
	keyEnvelopeDegraded     = "tide.envelope.degraded"
	// keyTimingSynthetic is Phase 44's D-05/Claude's-Discretion timing-floor
	// marker: events.jsonl carries no absolute timestamp for
	// message_start/message_stop/assistant events, so a per-call LLM span's
	// timing is interpolated/proportional rather than measured. No module
	// counterpart exists.
	keyTimingSynthetic = "tide.trace.timing_synthetic"
	// keyParseDegraded is Phase 44's D-05/D-11 marker: the conversation was
	// reconstructed from an events.jsonl stream with skipped or truncated
	// lines (tolerant-skip parse posture). No module counterpart exists.
	keyParseDegraded = "tide.trace.parse_degraded"
)

// LLMInputMessages flattens a slice of input messages into OpenInference's
// per-index attribute encoding. For N input messages this returns exactly
// 2*N attributes: a `.message.role` and `.message.content` per message.
//
// The flat-key encoding (NOT a JSON array on a single attribute) is what
// Phoenix, LangSmith, and Arize Platform all consume.
//
// Per D-O5 the caller decides whether `Content` carries the real payload
// (high-cardinality attribute value bytes go to the OTel collector + etcd)
// or defers to ArtifactPath. There is no inline-vs-reference helper here —
// the choice lives at the call site.
func LLMInputMessages(msgs []Message) []attribute.KeyValue {
	return flattenMessages(true, msgs)
}

// LLMOutputMessages flattens a slice of output (assistant/tool) messages
// into OpenInference's per-index attribute encoding. Same shape as
// LLMInputMessages, with the `llm.output_messages.` prefix.
func LLMOutputMessages(msgs []Message) []attribute.KeyValue {
	return flattenMessages(false, msgs)
}

// flattenMessages emits the flat-keyed attributes for a per-index
// `<prefix>.<i>.message.role` / `<prefix>.<i>.message.content` pairing,
// using the module's own indexer helpers so the emitted key strings track
// the spec exactly. Internal helper — the public surface is
// LLMInputMessages / LLMOutputMessages so that the D-O5 enforcement test
// sees exactly two payload-bearing public functions.
//
// Phase 44 (D-03) extension: when a message's ToolCalls or Contents field is
// non-empty, additional spec-native attributes are appended per tool call /
// content block via the module's own indexers (tool calls) or
// constant-composed keys (content blocks — the module has no public
// message.contents indexer). Nil ToolCalls/Contents emits exactly the legacy
// 2-attribute-per-message shape — zero behavior change for pre-Phase-44
// call sites.
func flattenMessages(input bool, msgs []Message) []attribute.KeyValue {
	if len(msgs) == 0 {
		return nil
	}
	prefix := semconv.LLMOutputMessages
	toolCallKey := semconv.LLMOutputMessageToolCallKey
	if input {
		prefix = semconv.LLMInputMessages
		toolCallKey = semconv.LLMInputMessageToolCallKey
	}

	out := make([]attribute.KeyValue, 0, 2*len(msgs))
	for i, m := range msgs {
		roleKey, contentKey := semconv.LLMOutputMessageRoleKey(i), semconv.LLMOutputMessageContentKey(i)
		if input {
			roleKey, contentKey = semconv.LLMInputMessageRoleKey(i), semconv.LLMInputMessageContentKey(i)
		}
		out = append(out,
			attribute.String(roleKey, m.Role),
			attribute.String(contentKey, m.Content),
		)

		for j, c := range m.ToolCalls {
			out = append(out,
				attribute.String(toolCallKey(i, j, semconv.ToolCallID), c.ID),
				attribute.String(toolCallKey(i, j, semconv.ToolCallFunctionName), c.Name),
				attribute.String(toolCallKey(i, j, semconv.ToolCallFunctionArgumentsJSON), c.ArgumentsJSON),
			)
		}

		for k, mc := range m.Contents {
			out = append(out,
				attribute.String(messageContentKey(prefix, i, k, semconv.MessageContentType), mc.Type),
				attribute.String(messageContentKey(prefix, i, k, semconv.MessageContentText), mc.Text),
			)
			if mc.Signature != "" {
				out = append(out, attribute.String(messageContentKey(prefix, i, k, semconv.MessageContentSignature), mc.Signature))
			}
		}
	}
	return out
}

// messageContentKey composes the "<prefix>.<i>.message.contents.<k>.<child>"
// key from semconv constants (D-03). There is no public indexer for
// message.contents in the vendored module — unlike
// LLM{Input,Output}MessageToolCallKey — so this composition is the correct
// shape: it satisfies TestKeysUseSemconvModule (the guard rejects raw
// "llm."-prefixed string literals, not constant-composed keys) while never
// hand-typing a spec-family literal.
func messageContentKey(prefix string, i, k int, child string) string {
	return fmt.Sprintf("%s.%d.%s.%d.%s", prefix, i, semconv.MessageContents, k, child)
}

// TokenCount returns the five token-accounting attributes per the
// OpenInference spec's `llm.token_count.*` family:
//
//   - llm.token_count.prompt                     — FULL prompt tokens,
//     INCLUDING the cache_read/cache_write subsets below. Phoenix's own
//     cost-calculator source treats prompt_details.* as subsets OF prompt,
//     not additions to it (D-08) — callers must pre-sum
//     InputTokens+CacheReadTokens+CacheCreationTokens before calling this
//     helper; this package does not read Usage directly.
//   - llm.token_count.completion                 — completion tokens
//   - llm.token_count.prompt_details.cache_read   — Anthropic cache HITS (subset of prompt)
//   - llm.token_count.prompt_details.cache_write  — Anthropic cache MISSES (subset of prompt)
//   - llm.token_count.total                       — prompt + completion, per
//     Phoenix's documented cost formula (ATTR-02/D-08). No separate
//     double-counting risk: the cache buckets are already inside `prompt`.
func TokenCount(prompt, completion, cacheRead, cacheWrite int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int(semconv.LLMTokenCountPrompt, prompt),
		attribute.Int(semconv.LLMTokenCountCompletion, completion),
		attribute.Int(semconv.LLMTokenCountPromptDetailsCacheRead, cacheRead),
		attribute.Int(semconv.LLMTokenCountPromptDetailsCacheWrite, cacheWrite),
		attribute.Int(semconv.LLMTokenCountTotal, prompt+completion),
	}
}

// AgentInvocation returns the five attributes that identify a single
// orchestrator-side subagent dispatch. Use on the parent span of a
// `tide.dispatch.<level>` chain so Phoenix / LangSmith / Arize can group by
// hierarchy level.
//
//   - openinference.span.kind=AGENT
//   - llm.system=<system>       (D-07: caller-supplied — pass
//     ResolveProvider(...).Vendor, never a hardcoded constant; this is the
//     extension point the OpenAI backend / LangGraph runtime will use)
//   - agent.name=<name>         (the dispatch span name — `tide.dispatch.<level>`)
//   - tide.role=<role>          (planner|executor — matches POOL-01 vocabulary;
//     D-05: no module counterpart)
//   - tide.invocation.level=<level>  (milestone|phase|plan|task — the
//     hierarchy level the dispatch represents; D-05: no module counterpart)
func AgentInvocation(system, name, role, level string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(semconv.OpenInferenceSpanKind, semconv.SpanKindAgent),
		attribute.String(semconv.LLMSystem, system),
		attribute.String(semconv.AgentName, name),
		attribute.String(keyAgentRole, role),
		attribute.String(keyAgentInvocationLevel, level),
	}
}

// ArtifactPath returns a SINGLE attribute referencing the PVC path of a
// full event/payload log. This is the D-O5 contract: traces carry only the
// path reference, never the inline payload. Trace consumers fetch the full
// log on demand from the PVC via tide artifact-get (CLI-04 / Phase 4 D-C3).
//
// D-05: the key is tide.artifact_path — the prior namespace squat on the
// rejected OTel GenAI semconv is dead; no module counterpart exists for an
// artifact-reference attribute.
//
// NOTE: signature returns attribute.KeyValue (not a slice) intentionally
// per CONTEXT D-O4. Calling sites use it as one element in a
// SetAttributes(...) variadic call.
func ArtifactPath(path string) attribute.KeyValue {
	return attribute.String(keyArtifactPath, path)
}

// LLMIdentity returns the llm.provider attribute, plus llm.model_name when
// model is non-empty (ATTR-01). The resolved model can legitimately be ""
// when no tier configured one for a given level+project — that is a genuine
// config gap, not a bug, so callers must never pass through an empty string:
// Phoenix renders a missing attribute as "no cost data," a clearer signal
// than llm.model_name="" (Pitfall 5).
//
//   - llm.provider=<provider>  (always)
//   - llm.model_name=<model>   (only when model != "")
func LLMIdentity(provider, model string) []attribute.KeyValue {
	out := []attribute.KeyValue{
		attribute.String(semconv.LLMProvider, provider),
	}
	if model != "" {
		out = append(out, attribute.String(semconv.LLMModelName, model))
	}
	return out
}

// FailureDetail returns the two D-03 failure-classification attributes
// attached to a failed dispatch span alongside its Error span status.
// Setting the span status itself (codes.Error, reason) is the caller's
// responsibility — see internal/controller's completion handlers.
//
//   - tide.exit_code=<exitCode>  (D-05: no module counterpart)
//   - tide.reason=<reason>       (D-05: no module counterpart — free-form
//     classification string, e.g. "cap-hit", "forced-failure",
//     "output-path-violation", "token-expired")
func FailureDetail(exitCode int, reason string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int(keyExitCode, exitCode),
		attribute.String(keyReason, reason),
	}
}

// EnvelopeDegraded returns the single D-04 marker attribute for a span whose
// envelope could not be read (envReadOK=false). Usage/model attributes are
// simply absent on a degraded span — this marker makes the degradation
// queryable in Phoenix's filter DSL rather than silently indistinguishable
// from "no usage incurred."
//
//   - tide.envelope.degraded=true
func EnvelopeDegraded() attribute.KeyValue {
	return attribute.Bool(keyEnvelopeDegraded, true)
}

// LLMSpanKind returns the single attribute marking a span as an LLM-kind
// span (openinference.span.kind=LLM). AgentInvocation hardcodes
// semconv.SpanKindAgent and is NOT reusable for this purpose — LLMSpanKind
// is the sibling helper for the per-API-call spans internal/reporter's
// tracesynth.go emits (Phase 44 D-01), so tracesynth never hand-rolls
// "openinference.span.kind".
//
//   - openinference.span.kind=LLM
func LLMSpanKind() attribute.KeyValue {
	return attribute.String(semconv.OpenInferenceSpanKind, semconv.SpanKindLLM)
}

// TimingSynthetic returns the single Phase 44 marker attribute (see
// keyTimingSynthetic above for the key) for a span whose start/end
// timestamps are interpolated or proportionally derived rather than
// measured from a real in-band timestamp. events.jsonl carries no absolute
// timestamp for message_start/message_stop/assistant events (RESEARCH.md
// "Timestamps — confirmed asymmetric, not absent") — this is the
// Claude's-Discretion timing floor CONTEXT.md mandates whenever a span's
// timing is synthesized rather than measured. Value is always true; mirrors
// EnvelopeDegraded's marker-attribute convention.
func TimingSynthetic() attribute.KeyValue {
	return attribute.Bool(keyTimingSynthetic, true)
}

// ParseDegraded returns the single D-11 marker attribute (see
// keyParseDegraded above for the key) for a span whose conversation was
// reconstructed from an events.jsonl stream containing skipped or truncated
// lines (tolerant-skip parse posture, matching ParseStream's own defensive
// posture). Value is always true; mirrors EnvelopeDegraded's
// marker-attribute convention.
func ParseDegraded() attribute.KeyValue {
	return attribute.Bool(keyParseDegraded, true)
}
