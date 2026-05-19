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

// Phase 4 Plan 03 Task 2: failing tests for the TracerProvider lifecycle
// in internal/otelinit. Asserts:
//   - no-op fallback when OTEL_EXPORTER_OTLP_ENDPOINT is unset
//   - real SDK TracerProvider when endpoint is set (gRPC exporter is lazy,
//     so construction succeeds even with an unreachable endpoint)
//   - source-grep: provider.go MUST NOT contain `WithSampler(` (Pitfall 24)
//   - otel.SetTracerProvider wires the constructed TP into the global handle
//   - the no-op tracer truly produces no-op spans
package otelinit

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

// TestNoOpFallbackWhenEndpointEmpty — with OTEL_EXPORTER_OTLP_ENDPOINT unset,
// the constructor returns a no-op TracerProvider, never opens a network
// socket, and Shutdown returns nil.
func TestNoOpFallbackWhenEndpointEmpty(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx := context.Background()
	tp, shutdown, err := NewTracerProvider(ctx)
	if err != nil {
		t.Fatalf("NewTracerProvider err = %v, want nil", err)
	}
	if tp == nil {
		t.Fatal("NewTracerProvider returned nil tp")
	}
	if shutdown == nil {
		t.Fatal("NewTracerProvider returned nil shutdown")
	}

	// Concrete type must be *noop.TracerProvider — the only branch in
	// provider.go that exists when the endpoint is empty.
	got := reflect.TypeOf(tp).String()
	if got != "*noop.TracerProvider" {
		t.Errorf("tp concrete type = %q, want %q", got, "*noop.TracerProvider")
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Errorf("shutdown err = %v, want nil", err)
	}
}

// TestRealSDKWhenEndpointSet — with an endpoint set (even an unreachable
// one), the gRPC exporter is lazy so the SDK TracerProvider constructs
// successfully. Shutdown is callable; gRPC client cleanup completes
// within a short deadline because no live RPCs are outstanding.
func TestRealSDKWhenEndpointSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:14317")

	ctx := context.Background()
	tp, shutdown, err := NewTracerProvider(ctx)
	if err != nil {
		t.Fatalf("NewTracerProvider err = %v, want nil (gRPC exporter is lazy)", err)
	}
	if tp == nil {
		t.Fatal("NewTracerProvider returned nil tp")
	}

	// Concrete type must be the SDK TracerProvider, NOT noop.
	got := reflect.TypeOf(tp).String()
	if got == "*noop.TracerProvider" {
		t.Errorf("tp = %q, want SDK TracerProvider when endpoint set", got)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		// Shutdown of an unreachable gRPC exporter may surface a
		// context-deadline-exceeded — accept it; the path is exercised.
		t.Logf("shutdown surfaced (expected for unreachable endpoint): %v", err)
	}
}

// TestNoWithSamplerInSource — Pitfall 24. The provider must NEVER pass
// WithSampler(...) to sdktrace.NewTracerProvider; doing so silently
// overrides the env-driven OTEL_TRACES_SAMPLER config. This test
// source-greps provider.go to enforce the absence at compile-adjacent time.
//
// Comments (single-line `//` or block `/* */`) MAY mention `WithSampler(`
// for documentation purposes; the grep strips comment lines first so a
// hard-comment explaining the rule does not trip the test.
func TestNoWithSamplerInSource(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "otelinit", "provider.go"))
	if err != nil {
		t.Fatalf("read internal/otelinit/provider.go: %v", err)
	}

	stripped := stripGoComments(string(data))
	if strings.Contains(stripped, "WithSampler(") {
		t.Errorf("Pitfall 24 violation: internal/otelinit/provider.go contains WithSampler( outside a comment; OTEL_TRACES_SAMPLER env var will be silently overridden")
	}
}

// TestOTelGlobalTracerProviderSet — NewTracerProvider must call
// otel.SetTracerProvider so package-level otel.Tracer(...) returns the
// constructed TP. Without this, reconciler code that calls
// `otel.Tracer("foo").Start(...)` lands on the default no-op TP regardless
// of whether the exporter is configured.
func TestOTelGlobalTracerProviderSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx := context.Background()
	tp, shutdown, err := NewTracerProvider(ctx)
	if err != nil {
		t.Fatalf("NewTracerProvider err = %v, want nil", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	if otel.GetTracerProvider() != tp {
		t.Errorf("otel.GetTracerProvider() != tp returned by NewTracerProvider; call SetTracerProvider in the constructor")
	}
}

// TestNoOpTracerProducesInvalidSpanContext — assert that the no-op path
// genuinely degrades to no-op semantics: spans started under it carry an
// invalid SpanContext, so traces never reach a collector.
func TestNoOpTracerProducesInvalidSpanContext(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	ctx := context.Background()
	tp, shutdown, err := NewTracerProvider(ctx)
	if err != nil {
		t.Fatalf("NewTracerProvider err = %v, want nil", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	tracer := tp.Tracer("tide-test")
	_, span := tracer.Start(ctx, "test.span")
	defer span.End()
	if span.SpanContext().IsValid() {
		t.Error("no-op tracer produced a valid SpanContext; expected invalid (no recording, no export)")
	}
}

// stripGoComments removes single-line (`// ...`) and block (`/* ... */`)
// comments from Go source so that text inside comments doesn't count toward
// grep-based source assertions. The implementation is intentionally simple
// — it does NOT understand backtick strings or context-aware lexing — but
// is sufficient for provider.go which has no string literal containing
// `WithSampler(`.
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

// findRepoRoot walks up until it finds go.mod. Mirrors the helper in
// pkg/otelai/attrs_test.go but kept local to avoid an internal/otelinit
// package depending on a testing helper from another package.
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
