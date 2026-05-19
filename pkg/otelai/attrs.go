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
	"strconv"

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

// OpenInference key prefixes / names. Constants here are the single source
// of truth so any spec drift surfaces in exactly one location.
//
// Reference: https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md
const (
	keyLLMInputMessagesPrefix  = "llm.input_messages"
	keyLLMOutputMessagesPrefix = "llm.output_messages"
	keyMessageRoleSuffix       = ".message.role"
	keyMessageContentSuffix    = ".message.content"

	keyTokenCountPrompt           = "llm.token_count.prompt"
	keyTokenCountCompletion       = "llm.token_count.completion"
	keyTokenCountCacheReadPrompt  = "llm.token_count.prompt_details.cache_read"
	keyTokenCountCacheWritePrompt = "llm.token_count.prompt_details.cache_write"

	keySpanKind             = "openinference.span.kind"
	keyLLMSystem            = "llm.system"
	keyAgentName            = "agent.name"
	keyAgentRole            = "agent.role"
	keyAgentInvocationLevel = "agent.invocation.level"

	keyArtifactPath = "gen_ai.artifact_path"

	spanKindAgent = "AGENT"
	llmSystem     = "anthropic"
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
	return flattenMessages(keyLLMInputMessagesPrefix, msgs)
}

// LLMOutputMessages flattens a slice of output (assistant/tool) messages
// into OpenInference's per-index attribute encoding. Same shape as
// LLMInputMessages, with the `llm.output_messages.` prefix.
func LLMOutputMessages(msgs []Message) []attribute.KeyValue {
	return flattenMessages(keyLLMOutputMessagesPrefix, msgs)
}

// flattenMessages emits the 2*N flat-keyed attributes for a per-index
// `<prefix>.<i>.message.role` / `<prefix>.<i>.message.content` pairing.
// Internal helper — the public surface is LLMInputMessages /
// LLMOutputMessages so that the D-O5 enforcement test sees exactly two
// payload-bearing public functions.
func flattenMessages(prefix string, msgs []Message) []attribute.KeyValue {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, 2*len(msgs))
	for i, m := range msgs {
		idx := strconv.Itoa(i)
		out = append(out,
			attribute.String(prefix+"."+idx+keyMessageRoleSuffix, m.Role),
			attribute.String(prefix+"."+idx+keyMessageContentSuffix, m.Content),
		)
	}
	return out
}

// TokenCount returns the four token-accounting attributes per the
// OpenInference spec's `llm.token_count.*` family. The four are:
//
//   - llm.token_count.prompt                          — uncached prompt tokens
//   - llm.token_count.completion                       — completion tokens
//   - llm.token_count.prompt_details.cache_read        — Anthropic cache HITS
//   - llm.token_count.prompt_details.cache_write       — Anthropic cache MISSES
//
// `llm.token_count.total` is INTENTIONALLY OMITTED — consumers can sum the
// four parts. Emitting it ourselves would double-count when downstream tools
// (Phoenix, etc.) recompute it.
func TokenCount(prompt, completion, cacheRead, cacheWrite int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int(keyTokenCountPrompt, prompt),
		attribute.Int(keyTokenCountCompletion, completion),
		attribute.Int(keyTokenCountCacheReadPrompt, cacheRead),
		attribute.Int(keyTokenCountCacheWritePrompt, cacheWrite),
	}
}

// AgentInvocation returns the five attributes that identify a single
// orchestrator-side subagent dispatch. Use on the parent span of a
// `tide.dispatch.<level>` chain so Phoenix / LangSmith / Arize can group by
// hierarchy level.
//
//   - openinference.span.kind=AGENT
//   - llm.system=anthropic     (only v1.0 backend; subagent-runtime extension
//     point per the Subagent interface will rewrite
//     this in future)
//   - agent.name=<name>        (the dispatch span name — `tide.dispatch.<level>`)
//   - agent.role=<role>        (planner|executor — matches POOL-01 vocabulary)
//   - agent.invocation.level=<level>  (milestone|phase|plan|task — the
//     hierarchy level the dispatch represents)
func AgentInvocation(name, role, level string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(keySpanKind, spanKindAgent),
		attribute.String(keyLLMSystem, llmSystem),
		attribute.String(keyAgentName, name),
		attribute.String(keyAgentRole, role),
		attribute.String(keyAgentInvocationLevel, level),
	}
}

// ArtifactPath returns a SINGLE attribute referencing the PVC path of a
// full event/payload log. This is the D-O5 contract: traces carry only the
// path reference, never the inline payload. Trace consumers fetch the full
// log on demand from the PVC via tide artifact-get (CLI-04 / Phase 4 D-C3).
//
// NOTE: signature returns attribute.KeyValue (not a slice) intentionally
// per CONTEXT D-O4. Calling sites use it as one element in a
// SetAttributes(...) variadic call.
func ArtifactPath(path string) attribute.KeyValue {
	return attribute.String(keyArtifactPath, path)
}
