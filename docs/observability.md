# TIDE Observability

Phase 4 stub — Phase 5 expands with example dashboards, alert rules,
and tail-sampling tuning guidance. Treat this as the env-var + ServiceMonitor
reference for v0.1.x.

## Three signal types

| Signal  | Where                                              | Notes                                                |
| ------- | -------------------------------------------------- | ---------------------------------------------------- |
| Logs    | controller + subagent pods, JSON via zap-behind-logr | Structured `project/phase/plan` labels per record (D-X1) |
| Metrics | `:8443/metrics` on the controller-manager Service  | Bounded cardinality — no `task` label (Pitfall 17)   |
| Traces  | OTLP gRPC, OpenInference attribute names           | Hand-rolled `pkg/otelai` (no Go OpenInference SDK in 2026) |

The same `{project, phase, plan}` label set flows through all three so
grep-correlation across logs/metrics/traces works without bespoke joining.

## Metrics

Metrics ship live on the controller-manager metrics Service:

```bash
kubectl port-forward -n tide-system svc/tide-controller-manager-metrics-service 8443:8443
curl -k https://localhost:8443/metrics | grep '^tide_'
```

The Phase 4 metric set includes:

- `tide_gate_evaluations_total{project, phase, decision}`
- `tide_secret_leak_blocked_total{project, phase, plan}` (D-W1)
- `tide_budget_overruns_total{project}`
- `tide_dispatch_duration_seconds{level}`

A `cmd/tide-lint` analyzer (Phase 1 POOL-03 extension) forbids metric
definitions with a `task` label so the cardinality bound is enforced at
compile time. See `internal/metrics/cardinality_test.go` for the canonical
allow-list.

### Prometheus ServiceMonitor

The chart ships a `ServiceMonitor` template, **default off** per the
CLAUDE.md anti-pattern:

> "Default the chart's ServiceMonitor to prometheus.enabled=false to avoid
> CRD-not-found on plain clusters."

Enable explicitly once the prometheus-operator CRDs are present:

```bash
helm upgrade tide ./charts/tide -n tide-system \
  --set prometheus.serviceMonitor.enabled=true
```

Tunables (`charts/tide/values.yaml`):

| Helm value                                        | Default | Purpose                |
| ------------------------------------------------- | ------- | ---------------------- |
| `prometheus.serviceMonitor.enabled`               | `false` | Toggle ServiceMonitor  |
| `prometheus.serviceMonitor.interval`              | `30s`   | Scrape interval        |
| `prometheus.serviceMonitor.scrapeTimeout`         | `10s`   | Scrape timeout         |

The ServiceMonitor selects the existing controller metrics Service
(`control-plane: controller-manager`) on port `https` (8443) with
`insecureSkipVerify: true` against the self-signed webhook cert. Phase 5
issues a proper CA bundle via cert-manager.

## Tracing

OpenTelemetry tracing is **env-driven** (per Pitfall 24: never bake a
sampler into Go code; let `OTEL_TRACES_SAMPLER` env vars govern). Empty
exporter endpoint → no-op `TracerProvider` (zero overhead, default).

| Env var                          | Helm value                       | Default                          |
| -------------------------------- | -------------------------------- | -------------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT`    | `otel.exporter.endpoint`         | `""` (→ no-op)                   |
| `OTEL_TRACES_SAMPLER`            | `otel.tracesSampler`             | `parentbased_traceidratio`       |
| `OTEL_TRACES_SAMPLER_ARG`        | `otel.tracesSamplerArg`          | `0.1`                            |
| `OTEL_SERVICE_NAME`              | `otel.serviceName`               | `tide-controller-manager`        |

Enable a real exporter by pointing the chart at an OTLP collector:

```bash
helm upgrade tide ./charts/tide -n tide-system \
  --set otel.exporter.endpoint=otel-collector.observability.svc:4317
```

The same env vars wire into the dashboard Deployment (with
`OTEL_SERVICE_NAME=tide-dashboard` hardcoded so traces from the two
processes are distinguishable in collectors).

### OpenInference attribute names

Spans use the OpenInference vocabulary (Phoenix, LangSmith, Arize all
consume it natively). Helpers in `pkg/otelai/`:

- `LLMInputMessages`, `LLMOutputMessages` — flattened message arrays
- `TokenCount` — input/output token totals
- `AgentInvocation` — agent + version on the parent dispatch span
- `ArtifactPath` — PVC reference (never inline LLM payload as a span attr — D-O5)

D-O5 is **enforced at the public API surface** via
`TestNoPayloadHelperOnPublicSurface`: any future helper that accepts inline
LLM payload bytes as a top-level attribute value fails CI. See
`pkg/otelai/doc.go` for the spec citation.

## Logs

Structured JSON via zap-behind-logr (~3× the field-heavy throughput of
slog per RESEARCH/STACK.md). Every reconcile log line carries the
canonical `{project, phase, plan}` label set so log-aggregator filters
align with metric/trace queries.

## What's coming

Phase 5 expands this doc with:

- Example Grafana dashboards (controller + dashboard + per-Project)
- Prometheus alert rule starter set (`SecretLeakBlockedBurst`,
  `BudgetExceededRate`, `DispatchLatencyP99`)
- OpenTelemetry collector deployment recipe (tail-sampling on by default)
- Trace-driven debugging walkthroughs for the four reconciler dispatch sites
