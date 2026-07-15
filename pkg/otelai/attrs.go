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
// the attribute VALUE — there is no validation, escaping, or transformation;
// callers MUST decide whether to populate Content with the real payload or
// to defer to ArtifactPath (D-O5: prefer PVC paths over inlined payload).
type Message struct {
	Role    string
	Content string
}

// TIDE-custom keys with no counterpart in the official
// openinference-semantic-conventions Go module (D-05 rename bucket — the
// module was downloaded and read directly at plan-authoring time; none of
// these six exist anywhere in it). Every other attribute key emitted by this
// package resolves from the semconv.* constants below — see
// TestKeysUseSemconvModule (attrs_test.go) for the source-grep guard that
// enforces this split at PR-review time (ATTR-03).
const (
	keyAgentRole            = "tide.role"
	keyAgentInvocationLevel = "tide.invocation.level"
	keyArtifactPath         = "tide.artifact_path"
	keyExitCode             = "tide.exit_code"
	keyReason               = "tide.reason"
	keyEnvelopeDegraded     = "tide.envelope.degraded"
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

// flattenMessages emits the 2*N flat-keyed attributes for a per-index
// `<prefix>.<i>.message.role` / `<prefix>.<i>.message.content` pairing,
// using the module's own indexer helpers so the emitted key strings track
// the spec exactly. Internal helper — the public surface is
// LLMInputMessages / LLMOutputMessages so that the D-O5 enforcement test
// sees exactly two payload-bearing public functions.
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
		out = append(out,
			attribute.String(roleKey, m.Role),
			attribute.String(contentKey, m.Content),
		)
	}
	return out
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
