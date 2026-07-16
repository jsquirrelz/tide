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

// Package otelai provides thin OpenInference attribute helpers for OTel
// spans emitted by the TIDE orchestrator's subagent-dispatch chain.
//
// Background — no Go OpenInference SDK exists in 2026 (Phase 4 D-O4); span
// lifecycle remains the caller's via the standard
// go.opentelemetry.io/otel/trace API. What changed in Phase 42 (ATTR-03):
// spec-backed attribute keys now resolve from the official
// github.com/Arize-ai/openinference/go/openinference-semantic-conventions Go
// module (pinned exactly at v0.1.1, D-06) instead of hand-typed string
// constants. The May 2026 spec is hosted at
//
//	https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md
//
// and is consumed verbatim by Arize Phoenix, LangSmith, and Arize Platform.
// This package exports thin helpers that each return
// go.opentelemetry.io/otel/attribute.KeyValue values matching the spec's
// flat-keyed encoding (NOT JSON arrays); TIDE-custom keys with no module
// counterpart live under an explicit tide.* namespace (D-05) so nothing
// masquerades as spec vocabulary. Callers apply these helpers' output via
// Span.SetAttributes(...).
//
// # Public surface (11 helpers)
//
//   - LLMInputMessages(msgs)        → []attribute.KeyValue (2*N entries,
//     flat-keyed; PLUS 3 more per Message.ToolCalls entry and 2-3 more per
//     Message.Contents entry when populated — Phase 44 D-03 spec-native
//     tool-call/reasoning-block encoding. Nil ToolCalls/Contents preserves
//     the legacy 2*N-only shape.)
//   - LLMOutputMessages(msgs)       → same shape, `llm.output_messages.` prefix
//   - TokenCount(p, c, cr, cw)      → []attribute.KeyValue (exactly 5 entries,
//     including llm.token_count.total = p+c per Phoenix's cost formula, D-08)
//   - AgentInvocation(sys,n,r,lvl)  → []attribute.KeyValue (exactly 5 entries;
//     llm.system is caller-supplied, D-07)
//   - ArtifactPath(path)            → attribute.KeyValue   (exactly 1 entry;
//     key is tide.artifact_path, D-05)
//   - LLMIdentity(provider, model)  → []attribute.KeyValue (1 or 2 entries;
//     llm.model_name omitted when model == "")
//   - FailureDetail(exitCode, reason) → []attribute.KeyValue (exactly 2 entries;
//     tide.exit_code / tide.reason, D-03)
//   - EnvelopeDegraded()            → attribute.KeyValue   (exactly 1 entry;
//     tide.envelope.degraded=true, D-04)
//   - LLMSpanKind()                 → attribute.KeyValue   (exactly 1 entry;
//     openinference.span.kind=LLM, Phase 44 D-01/D-03 — the LLM-kind sibling
//     of AgentInvocation's hardcoded SpanKindAgent)
//   - TimingSynthetic()             → attribute.KeyValue   (exactly 1 entry;
//     tide.trace.timing_synthetic=true marker, Phase 44 Claude's-Discretion
//     timing floor)
//   - ParseDegraded()               → attribute.KeyValue   (exactly 1 entry;
//     tide.trace.parse_degraded=true marker, Phase 44 D-11)
//
// Pure trace-context primitives (TraceIDFromUID, FormatTraceparent,
// ExtractRemoteParent) live in tracecontext.go, authored by the sibling
// Phase 42 plan 42-02 — they are NOT payload-bearing attribute helpers and
// are exempt from the "11 helpers" count above.
//
// # D-O5 — no payload inlining
//
// There is NO InlinePayload helper. There is NO RawContent helper. There is
// NO Body helper. Inlining a raw, unredacted, unbounded LLM payload as a
// span attribute VALUE is a known failure mode (etcd bloat, collector OOM,
// PII/secret leakage into long-term trace storage). The unit test
// TestNoPayloadHelperOnPublicSurface source-greps attrs.go to enforce this
// at PR-review time — any future helper named
// Payload/InlinePayload/RawContent/Body/MessageBody fails CI. This
// enforcement is unchanged by Phase 44.
//
// Phase 44 evolves the CONTRACT, not the guard: the boundary is now
// bounded-inline PLUS ArtifactPath co-attribute, not "always defer to
// ArtifactPath." A production caller (internal/reporter's tracesynth.go)
// MAY inline message content via LLMInputMessages / LLMOutputMessages once
// it has passed the mandatory redact-then-truncate pipeline: (1)
// internal/harness/redact.String — the full SecretPatterns denylist pass
// (MSG-02) — THEN (2) per-message head+tail truncation (MSG-03/D-08).
// Never the reverse: D-09 requires redaction before truncation, because a
// truncation cut can split a secret so the pattern no longer matches. The
// SAME span also carries an ArtifactPath co-attribute referencing the
// full-fidelity, unredacted events.jsonl on the shared PVC, so a trace
// consumer always has a path to the complete record even when the inline
// value was bounded.
//
// The Message struct's `Content` field carries this bounded-inline text.
// Test fixtures and trusted system-prompt cases may still populate it
// directly without the pipeline; production call sites MUST run the
// redact-then-truncate pipeline first (MSG-02/MSG-03/D-09).
//
// # Sampler interplay
//
// This package is pure attribute construction — it does NOT create spans.
// Sampling is controlled by the env-driven sampler at TracerProvider build
// time (internal/otelinit + OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG;
// see Pitfall 24). Callers that pass these attributes into spans rejected
// by the sampler incur exactly zero collector/etcd cost.
package otelai
