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

// Phase 42 Plan 02 Task 1: failing tests for the three trace-context
// primitives (deterministic TraceID derivation, W3C traceparent formatting,
// remote-parent extraction) that this plan's tracecontext.go implements.
// These lock the Phase 43 propagation seam's contract into the repo.
package otelai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

const (
	testUID        = "c2c8a338-1e2b-4c3a-9b1d-0a2f3e4d5c6b"
	testTraceIDHex = "c2c8a3381e2b4c3a9b1d0a2f3e4d5c6b"
	testSpanIDHex  = "00f067aa0ba902b7"
)

// TestTraceContextTraceIDFromUID — a canonical K8s UID deterministically maps
// to a valid 16-byte TraceID; the same UID always yields the same TraceID;
// case is normalized before hex-decoding.
func TestTraceContextTraceIDFromUID(t *testing.T) {
	got, err := TraceIDFromUID(testUID)
	if err != nil {
		t.Fatalf("TraceIDFromUID(%q) returned error: %v", testUID, err)
	}
	if got.String() != testTraceIDHex {
		t.Errorf("TraceIDFromUID(%q).String() = %q, want %q", testUID, got.String(), testTraceIDHex)
	}

	// Determinism: calling twice with the same UID yields identical values.
	got2, err := TraceIDFromUID(testUID)
	if err != nil {
		t.Fatalf("second TraceIDFromUID(%q) returned error: %v", testUID, err)
	}
	if got != got2 {
		t.Errorf("TraceIDFromUID(%q) is not deterministic: first=%v second=%v", testUID, got, got2)
	}

	// Mixed-case UID input yields the same TraceID as lowercase.
	mixedCase := "C2C8A338-1e2B-4C3a-9b1D-0a2f3e4d5c6b"
	gotMixed, err := TraceIDFromUID(mixedCase)
	if err != nil {
		t.Fatalf("TraceIDFromUID(%q) returned error: %v", mixedCase, err)
	}
	if gotMixed != got {
		t.Errorf("TraceIDFromUID(%q) = %v, want %v (mixed-case must match lowercase)", mixedCase, gotMixed, got)
	}
}

// TestTraceContextTraceIDFromUIDInvalid — empty, non-UUID, and the invalid
// all-zero UUID all error; none panic.
func TestTraceContextTraceIDFromUIDInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TraceIDFromUID panicked: %v", r)
		}
	}()

	cases := []string{
		"",
		"not-a-uuid",
		"00000000-0000-0000-0000-000000000000", // valid UUID shape, but the all-zero TraceID is invalid
	}
	for _, uid := range cases {
		if _, err := TraceIDFromUID(uid); err == nil {
			t.Errorf("TraceIDFromUID(%q) returned nil error, want an error", uid)
		}
	}
}

// TestTraceContextFormatTraceparent — exact W3C traceparent string for a
// valid trace/span ID pair, sampled and unsampled; zero-value (invalid) IDs
// yield "" because Inject no-ops on an invalid SpanContext.
func TestTraceContextFormatTraceparent(t *testing.T) {
	traceID, err := trace.TraceIDFromHex(testTraceIDHex)
	if err != nil {
		t.Fatalf("trace.TraceIDFromHex(%q): %v", testTraceIDHex, err)
	}
	spanID, err := trace.SpanIDFromHex(testSpanIDHex)
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex(%q): %v", testSpanIDHex, err)
	}

	wantSampled := "00-" + testTraceIDHex + "-" + testSpanIDHex + "-01"
	if got := FormatTraceparent(traceID, spanID, true); got != wantSampled {
		t.Errorf("FormatTraceparent(sampled=true) = %q, want %q", got, wantSampled)
	}

	wantUnsampled := "00-" + testTraceIDHex + "-" + testSpanIDHex + "-00"
	if got := FormatTraceparent(traceID, spanID, false); got != wantUnsampled {
		t.Errorf("FormatTraceparent(sampled=false) = %q, want %q", got, wantUnsampled)
	}

	if got := FormatTraceparent(trace.TraceID{}, trace.SpanID{}, true); got != "" {
		t.Errorf("FormatTraceparent(zero-value IDs) = %q, want empty string", got)
	}
}

// TestTraceContextExtractRemoteParent — a FormatTraceparent output round-trips
// through ExtractRemoteParent back into a valid, remote SpanContext carrying
// the identical TraceID/SpanID pair.
func TestTraceContextExtractRemoteParent(t *testing.T) {
	traceID, err := trace.TraceIDFromHex(testTraceIDHex)
	if err != nil {
		t.Fatalf("trace.TraceIDFromHex(%q): %v", testTraceIDHex, err)
	}
	spanID, err := trace.SpanIDFromHex(testSpanIDHex)
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex(%q): %v", testSpanIDHex, err)
	}

	traceparent := FormatTraceparent(traceID, spanID, true)
	if traceparent == "" {
		t.Fatalf("FormatTraceparent returned empty string for valid IDs")
	}

	ctx := ExtractRemoteParent(context.Background(), traceparent)
	sc := trace.SpanContextFromContext(ctx)

	if !sc.IsValid() {
		t.Fatalf("ExtractRemoteParent(%q) produced an invalid SpanContext", traceparent)
	}
	if !sc.IsRemote() {
		t.Errorf("ExtractRemoteParent(%q) SpanContext.IsRemote() = false, want true", traceparent)
	}
	if sc.TraceID() != traceID {
		t.Errorf("round-trip TraceID = %v, want %v", sc.TraceID(), traceID)
	}
	if sc.SpanID() != spanID {
		t.Errorf("round-trip SpanID = %v, want %v", sc.SpanID(), spanID)
	}
}

// TestTraceContextExtractMalformedNoPanic — malformed traceparent inputs
// never panic and always yield an invalid SpanContext.
func TestTraceContextExtractMalformedNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ExtractRemoteParent panicked: %v", r)
		}
	}()

	cases := []string{
		"",
		"garbage",
		"00-zzzz-yyyy-01",
		"00-" + testTraceIDHex + "-01", // missing span field
	}
	for _, tp := range cases {
		ctx := ExtractRemoteParent(context.Background(), tp)
		sc := trace.SpanContextFromContext(ctx)
		if sc.IsValid() {
			t.Errorf("ExtractRemoteParent(%q) produced a valid SpanContext, want invalid", tp)
		}
	}
}

// TestTraceContextNoK8sImports enforces the 42-PATTERNS.md "No K8s imports —
// tracecontext.go stays as dependency-light as attrs.go" constraint and the
// milestone ARCHITECTURE.md build-order rationale (pure, zero K8s deps,
// build first). Mirrors TestNoWithSamplerInSource's role: a cheap PR-time
// tripwire for a load-bearing architectural rule. findRepoRoot is defined
// once in attrs_test.go (same package) and reused here.
func TestTraceContextNoK8sImports(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "pkg", "otelai", "tracecontext.go"))
	if err != nil {
		t.Fatalf("read pkg/otelai/tracecontext.go: %v", err)
	}
	src := string(data)
	for _, needle := range []string{"k8s.io/", "sigs.k8s.io/"} {
		if strings.Contains(src, needle) {
			t.Errorf("tracecontext.go must stay K8s-independent (42-PATTERNS.md / ARCHITECTURE.md build-order rationale); found forbidden import substring %q", needle)
		}
	}
}
