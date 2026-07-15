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
// # Public surface (8 helpers)
//
//   - LLMInputMessages(msgs)        → []attribute.KeyValue (2*N entries, flat-keyed)
//   - LLMOutputMessages(msgs)       → []attribute.KeyValue (2*N entries, flat-keyed)
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
//
// Pure trace-context primitives (TraceIDFromUID, FormatTraceparent,
// ExtractRemoteParent) live in tracecontext.go, authored by the sibling
// Phase 42 plan 42-02 — they are NOT payload-bearing attribute helpers and
// are exempt from the "8 helpers" count above.
//
// # D-O5 — no payload inlining
//
// There is NO InlinePayload helper. There is NO RawContent helper. There is
// NO Body helper. Inlining LLM payloads as span attribute VALUES is a known
// failure mode (etcd bloat, collector OOM, PII leakage into long-term
// trace storage). Use ArtifactPath to reference the full event log on the
// shared PVC instead. The unit test TestNoPayloadHelperOnPublicSurface
// source-greps attrs.go to enforce this at PR-review time — any future
// helper named Payload/InlinePayload/RawContent/Body/MessageBody fails CI.
//
// The Message struct's `Content` field exists ONLY so callers can choose
// to emit message text VERBATIM via LLMInputMessages / LLMOutputMessages
// when they have already decided the payload is safe (e.g. system prompts).
// Per D-O5 production-path callers SHOULD prefer ArtifactPath; the helpers
// support both shapes so test fixtures and trusted system-prompt cases
// remain expressible.
//
// # Sampler interplay
//
// This package is pure attribute construction — it does NOT create spans.
// Sampling is controlled by the env-driven sampler at TracerProvider build
// time (internal/otelinit + OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG;
// see Pitfall 24). Callers that pass these attributes into spans rejected
// by the sampler incur exactly zero collector/etcd cost.
package otelai
