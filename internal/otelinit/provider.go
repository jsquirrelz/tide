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

package otelinit

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// ShutdownFunc is the deferred-from-main shutdown shape returned by
// NewTracerProvider. Callers MUST invoke it with a bounded context (e.g.
// context.WithTimeout) before the manager process exits so the batch span
// processor flushes outstanding spans to the collector. In the no-op path
// the function is a no-op closure that returns nil unconditionally.
type ShutdownFunc func(context.Context) error

// NewTracerProvider constructs the orchestrator's OTel TracerProvider per
// Phase 4 D-O3. Behavior depends on the OTEL_EXPORTER_OTLP_ENDPOINT env var:
//
//   - Unset / empty → returns a no-op TracerProvider so the orchestrator
//     works in kind clusters with no OTLP collector. Tracer.Start calls are
//     real but emit no spans and open no network sockets.
//   - Set         → constructs the SDK TracerProvider with an OTLP gRPC
//     exporter and a batch span processor. The resource attributes come from
//     OTEL_SERVICE_NAME + OTEL_RESOURCE_ATTRIBUTES + process + telemetry SDK
//     descriptors.
//
// In both branches the package-level otel.SetTracerProvider(...) is called
// so that reconciler code using otel.Tracer(...) resolves to the right TP.
//
// IMPORTANT (Pitfall 24): this constructor MUST NOT pass WithSampler(...) to
// sdktrace.NewTracerProvider. Doing so silently overrides the env-driven
// OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG configuration. The Helm
// chart sets OTEL_TRACES_SAMPLER=parentbased_traceidratio + arg 0.1 by
// default; any in-code WithSampler call invalidates that contract.
// The companion test TestNoWithSamplerInSource source-greps this file to
// enforce the rule at PR time.
func NewTracerProvider(ctx context.Context) (trace.TracerProvider, ShutdownFunc, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// No-op path. The trace/noop package returns a TracerProvider whose
		// Tracer(...) returns a Tracer whose Start(...) returns a span with
		// an invalid SpanContext — no recording, no export. Setting it as
		// the global TracerProvider is correct: reconciler code that calls
		// otel.Tracer("foo") will pick it up and incur zero overhead.
		tp := tracenoop.NewTracerProvider()
		otel.SetTracerProvider(tp)
		return tp, func(context.Context) error { return nil }, nil
	}

	// Real SDK path. otlptracegrpc.New does NOT dial immediately — the
	// underlying gRPC ClientConn is constructed but the actual TCP connect
	// happens lazily on the first Export() call (batch span processor). So
	// constructor errors here surface only if the endpoint URL or TLS
	// options are syntactically invalid, NOT if the collector is down.
	//
	// WithInsecure is the v1.0 default per RESEARCH §711 — production
	// operators configure TLS via OTEL_EXPORTER_OTLP_HEADERS auth tokens or
	// by overriding the chart's controller env block. Phase 5 documents the
	// TLS migration path.
	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otlptracegrpc.New: %w", err)
	}

	// Resource — honor OTEL_SERVICE_NAME + OTEL_RESOURCE_ATTRIBUTES env
	// vars + emit process + telemetry-SDK descriptors. resource.New aggregates
	// the three detectors in order; later detectors override earlier ones if
	// they emit the same key.
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("resource.New: %w", err)
	}

	// MUST NOT pass WithSampler(...) here — env-driven OTEL_TRACES_SAMPLER
	// governs (Pitfall 24, D-O3). The SDK's default when no WithSampler is
	// supplied is ParentBased(AlwaysSample); the env var overrides that
	// default per the OTel spec. Helm chart default is
	// parentbased_traceidratio with arg 0.1 (10% sampling).
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp, tp.Shutdown, nil
}
