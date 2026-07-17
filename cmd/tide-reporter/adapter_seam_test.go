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

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/jsquirrelz/tide/pkg/otelai"
)

// TestAdapterSeam_SelfInstrumentingRuntimeNoDuplicateSpans is the ADAPT-01
// D-09 contract test: it proves that a self-instrumenting runtime's own
// span and a reporter run with --skip-message-spans set can share one
// tracetest.InMemoryExporter and produce exactly one span total — zero
// synthesized duplicates.
//
// Reuses installStubTracerProvider and writeTraceOnlyFixture verbatim from
// main_test.go (same package, unexported file-scope helpers).
//
// Env-carrier extraction only (otelai.ExtractRemoteParent) — no
// vendor-specific span-shape assumption. The stub runtime stands in for
// any future self-instrumenting Subagent implementation.
func TestAdapterSeam_SelfInstrumentingRuntimeNoDuplicateSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	installStubTracerProvider(t, exp)
	// installStubTracerProvider's newTracerProvider seam only installs the
	// global TracerProvider lazily, when runWithClient later invokes it — but
	// the stub's own span below is created BEFORE runWithClient runs, so the
	// global provider must be wired to exp here too (same exporter; harmless
	// to overwrite again once runWithClient's own seam call fires).
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))

	traceID, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	traceParent := otelai.FormatTraceparent(traceID, spanID, true)

	// (a) env-carrier extraction: a stub self-instrumenting runtime extracts
	// the injected traceparent via the SAME primitive the reporter itself
	// uses, and starts/ends its own span on that context — generic
	// env-carrier mechanics only, zero vendor-specific span shape.
	stubCtx := otelai.ExtractRemoteParent(context.Background(), traceParent)
	_, stubSpan := otel.Tracer("stub-self-instrumenting-runtime").Start(stubCtx, "stub.graph.invoke")
	stubSpan.End()

	// (b) zero duplicates: a REAL 2-call events.jsonl sits on disk (would
	// normally yield 2 synthesized spans); SkipMessageSpans: true must
	// suppress synthesis entirely.
	workspace := t.TempDir()
	taskUID := "task-self-instrumenting"
	writeTraceOnlyFixture(t, workspace, taskUID)

	cfg := reporterConfig{
		TraceOnly: true, TaskUID: taskUID, Workspace: workspace,
		TraceParent: traceParent, SkipMessageSpans: true,
	}

	var stderr bytes.Buffer
	code := runWithClient(context.Background(), cfg, nil, &stderr, nil)
	if code != exitSuccess {
		t.Fatalf("runWithClient exit=%d, want exitSuccess; stderr=%q", code, stderr.String())
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want exactly 1 (the stub runtime's own — zero synthesized)", len(spans))
	}
	if spans[0].Name != "stub.graph.invoke" {
		t.Errorf("unexpected span in exporter: %q (synthesis was not skipped)", spans[0].Name)
	}
	if spans[0].SpanContext.TraceID() != traceID {
		t.Errorf("stub span TraceID = %s, want %s (env-carrier extraction did not activate)", spans[0].SpanContext.TraceID(), traceID)
	}

	// (c) no sentinel: a skipped run never touches the .spans-emitted path.
	sentinelPath := filepath.Join(workspace, "envelopes", taskUID, ".spans-emitted")
	if _, err := os.Stat(sentinelPath); !os.IsNotExist(err) {
		t.Errorf("os.Stat(sentinel) err = %v, want a not-exist error (D-05: skipped run writes no sentinel)", err)
	}
}
