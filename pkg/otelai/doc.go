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

// Package otelai provides hand-rolled OpenInference attribute helpers for
// OTel spans emitted by the TIDE orchestrator's subagent-dispatch chain.
//
// Background — no Go OpenInference SDK exists in 2026 (Phase 4 D-O4). The
// May 2026 spec is hosted at
//
//	https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md
//
// and is consumed verbatim by Arize Phoenix, LangSmith, and Arize Platform.
// Rather than carry a heavyweight DSL, this package exports exactly five
// thin helpers that each return go.opentelemetry.io/otel/attribute.KeyValue
// values matching the spec's flat-keyed encoding (NOT JSON arrays). Callers
// own span lifecycle via the standard go.opentelemetry.io/otel/trace API
// and apply these helpers' output via Span.SetAttributes(...).
//
// # Public surface (locked at 5 helpers)
//
//   - LLMInputMessages(msgs)   → []attribute.KeyValue (2*N entries, flat-keyed)
//   - LLMOutputMessages(msgs)  → []attribute.KeyValue (2*N entries, flat-keyed)
//   - TokenCount(p, c, cr, cw) → []attribute.KeyValue (exactly 4 entries)
//   - AgentInvocation(...)     → []attribute.KeyValue (exactly 5 entries)
//   - ArtifactPath(path)       → attribute.KeyValue   (exactly 1 entry)
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
