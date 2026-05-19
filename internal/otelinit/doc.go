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

// Package otelinit constructs the orchestrator's OpenTelemetry
// TracerProvider at cmd/manager boot per Phase 4 D-O3.
//
// # No-op fallback
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is unset, NewTracerProvider returns a
// trace/noop TracerProvider. Reconciler code can call otel.Tracer(...) and
// tracer.Start(...) freely without any wrapping `if otelEnabled` checks —
// spans become no-ops at zero cost. This keeps `kind` clusters and local
// dev environments working without an OTLP collector.
//
// # Real SDK path
//
// When the endpoint is set, NewTracerProvider constructs an OTLP gRPC
// exporter (lazy — no immediate connect) behind a batch span processor.
// Resource attributes come from OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES,
// process descriptors, and the OTel SDK identity — three detectors merged
// in order, with later detectors winning on key collisions.
//
// # Env-driven sampler (Pitfall 24)
//
// The TracerProvider constructor MUST NOT pass sdktrace.WithSampler(...).
// Doing so silently overrides the env-driven OTEL_TRACES_SAMPLER and
// OTEL_TRACES_SAMPLER_ARG config. The Helm chart sets these to
// parentbased_traceidratio / "0.1" by default (10% sampling). The
// companion test TestNoWithSamplerInSource source-greps provider.go and
// fails CI if a future refactor reintroduces WithSampler.
//
// # Global TracerProvider
//
// NewTracerProvider calls otel.SetTracerProvider(tp) before returning so
// that reconciler code using package-level otel.Tracer(...) resolves to
// the constructed TP without explicit threading.
//
// # Shutdown contract
//
// The returned ShutdownFunc MUST be deferred from cmd/manager/main.go
// with a bounded context. The batch processor uses it to flush
// outstanding spans to the collector before the binary exits — without
// the deferred call, end-of-run spans are dropped on the floor.
//
// # Environment variables consumed
//
//   - OTEL_EXPORTER_OTLP_ENDPOINT   — gRPC endpoint URL; empty disables tracing
//   - OTEL_SERVICE_NAME             — resource attribute service.name
//   - OTEL_RESOURCE_ATTRIBUTES      — additional resource k=v pairs
//   - OTEL_TRACES_SAMPLER / _ARG    — sampler config (env-driven; see above)
//   - OTEL_EXPORTER_OTLP_HEADERS    — auth headers for collector
//   - OTEL_EXPORTER_OTLP_TIMEOUT    — exporter timeout (ms)
//
// All other env vars natively supported by the OTel Go SDK v1.43.x are
// also honored — this package adds no new env-var surface.
package otelinit
