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

### Token, cost, and duration metrics

Phase 5 adds six metrics incremented in the `TaskReconciler` terminal-success
branch from the provider Anthropic `Usage` struct. **All six use the
`{project, phase, wave}` label set.** The `wave` label value is the owning
Wave CRD name resolved by walking the Task owner-reference chain (the same
pattern already used for `project` resolution). No new CRD fields and no
`.status` changes are required.

| Metric | Type | Label set | Source / where incremented |
| ------ | ---- | --------- | -------------------------- |
| `tide_tokens_input_total` | Counter | `project`, `phase`, `wave` | `TaskReconciler` terminal-success — `Usage.InputTokens` |
| `tide_tokens_output_total` | Counter | `project`, `phase`, `wave` | `TaskReconciler` terminal-success — `Usage.OutputTokens` |
| `tide_tokens_cache_read_total` | Counter | `project`, `phase`, `wave` | `TaskReconciler` terminal-success — `Usage.CacheReadInputTokens` |
| `tide_tokens_cache_creation_total` | Counter | `project`, `phase`, `wave` | `TaskReconciler` terminal-success — `Usage.CacheCreationInputTokens` |
| `tide_cost_cents_total` | Counter | `project`, `phase`, `wave` | `TaskReconciler` terminal-success — derived from `Usage` token counts and model pricing |
| `tide_task_duration_seconds` | Histogram | `project`, `phase`, `wave` | `TaskReconciler` terminal-success — wall-clock from task dispatch to terminal state |

### Cardinality budget

The approved label dimensions bound time-series growth to four aggregation
levels:

| Aggregation level | Labels present |
| ----------------- | -------------- |
| Project roll-up | `project` |
| Phase roll-up | `project`, `phase` |
| Plan roll-up | `project`, `phase`, `plan` |
| Wave roll-up | `project`, `phase`, `wave` |

**Per-task Prometheus labels are forbidden.** A single run may contain
thousands of Tasks; adding a `task` label produces unbounded cardinality and
violates Pitfall 17. Per-task detail is available via CRD `.status` fields
and OpenTelemetry spans, both indexed and queryable without cardinality risk.

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
| `prometheus.retentionTime`                        | `15d`   | TSDB retention — maps to `--storage.tsdb.retention.time` on the operator-managed Prometheus |
| `prometheus.endpoint`                             | `""`    | Prometheus base URL — injected server-side as `PROM_ENDPOINT` into the dashboard process (see [PromQL proxy](#promql-proxy)); no effect when empty |

The ServiceMonitor selects the existing controller metrics Service
(`control-plane: controller-manager`) on port `https` (8443) with
`insecureSkipVerify: true` against the self-signed webhook cert. Phase 5
issues a proper CA bundle via cert-manager.

> **Note:** the chart ships a `ServiceMonitor` only — it does **not** bundle
> a Prometheus server. `prometheus.retentionTime` and `prometheus.endpoint`
> are consumed by an operator-managed Prometheus instance that the cluster
> administrator provides separately.

#### Retention sizing

`prometheus.retentionTime` maps directly to the
`--storage.tsdb.retention.time` flag on the operator-managed Prometheus. The
`15d` default is sized for a single multi-day run plus a one-week
post-completion analysis window. For organizations tracking cost trends
across **multiple runs**, `30d` or longer is recommended — extend the value
accordingly:

```bash
helm upgrade tide ./charts/tide -n tide-system \
  --set prometheus.retentionTime=30d
```

### PromQL proxy

The TIDE dashboard proxies all Prometheus queries through the existing
[chi](https://github.com/go-chi/chi) HTTP server rather than having the
browser contact Prometheus directly. Two routes are registered:

| Route | Query type |
| ----- | ---------- |
| `GET /api/v1/query` | Instant query |
| `GET /api/v1/query_range` | Range query |

This design provides:

- **Single-origin semantics** — the browser talks only to the dashboard
  origin; no cross-origin requests to Prometheus are required.
- **Zero CORS reconfiguration** — the Prometheus instance needs no
  `--web.cors.origin` flags or middleware changes.
- **Endpoint confinement** — the Prometheus URL (`prometheus.endpoint`) is
  stored server-side and never exposed to the client.

`prometheus.endpoint` is injected as the `PROM_ENDPOINT` environment
variable into the dashboard process **only when non-empty**. When the value
is empty the proxy routes return `503 Service Unavailable` rather than
panicking, so clusters without a Prometheus instance remain functional for
all non-metrics features.

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
