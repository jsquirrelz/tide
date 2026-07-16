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

// tracecontext.go holds the pure, K8s-independent trace-context primitives:
// deterministic TraceID derivation from a Project UID, W3C traceparent
// formatting, and remote-parent extraction. This is the Phase 43 propagation
// seam (PROP-01/02) — Phase 42 defines and unit-proves these; Phase 43 wires
// them into Job/reporter env. Imports are LIMITED to stdlib plus
// go.opentelemetry.io/otel/{trace,propagation} — never a Kubernetes API
// package, never controller-runtime, and never a UUID-parsing dependency
// (keeps go.mod untouched; wave-1 sibling plan 42-01 owns go.mod).
package otelai

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TraceIDFromUID derives a deterministic OTel TraceID from a K8s object UID.
//
// A K8s UID is a canonical 8-4-4-4-12 UUID string — exactly 128 bits, the
// width of a TraceID. This strips the dashes, lowercases the result, and
// hands the 32-char hex string to trace.TraceIDFromHex, which enforces
// length, hex-validity, and rejects the invalid all-zero ID.
//
// Same UID always yields the same TraceID — the v1.0.8 "deterministic
// TraceID from Project.UID" binding constraint (PROJECT.md / STATE.md).
// Every reconciler, the reporter, and a future self-instrumenting subagent
// independently compute the same TraceID from Project.UID without any wire
// transfer of the trace ID itself; only the parent SpanID needs to travel
// (Phase 43 — the consumer of this function).
//
// Takes a plain string (NOT the Kubernetes apimachinery UID type) to keep
// this package free of Kubernetes imports; callers pass string(project.UID).
//
// Source: Phase 42 / v1.0.8 TRACE foundation; ARCHITECTURE.md Pattern 2.
func TraceIDFromUID(uid string) (trace.TraceID, error) {
	hex := strings.ToLower(strings.ReplaceAll(uid, "-", ""))
	return trace.TraceIDFromHex(hex)
}

// FormatTraceparent renders a W3C traceparent header
// ("00-<32hex-traceid>-<16hex-spanid>-<2hex-flags>") for the given IDs via
// propagation.TraceContext{}.Inject on a MapCarrier — never a hand-rolled
// format string. sampled controls the trailing flags byte: "01" when true,
// "00" when false.
//
// Returns "" when the trace/span ID pair is invalid (Inject no-ops on an
// invalid SpanContext) — e.g. a zero-value trace.TraceID{} or
// trace.SpanID{}.
//
// Reference: https://www.w3.org/TR/trace-context/#traceparent-header
func FormatTraceparent(traceID trace.TraceID, spanID trace.SpanID, sampled bool) string {
	flags := trace.TraceFlags(0)
	if sampled {
		flags = trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		Remote:     true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// ExtractRemoteParent parses a traceparent header into ctx as a remote
// SpanContext via propagation.TraceContext{}.Extract. On malformed input the
// returned ctx carries no valid span context — callers check
// trace.SpanContextFromContext(ctx).IsValid(); ExtractRemoteParent never
// panics.
//
// Reference: https://www.w3.org/TR/trace-context/#traceparent-header
func ExtractRemoteParent(ctx context.Context, traceparent string) context.Context {
	carrier := propagation.MapCarrier{"traceparent": traceparent}
	return propagation.TraceContext{}.Extract(ctx, carrier)
}
