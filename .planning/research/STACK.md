# Stack Research — TIDE v1.0.8 "Phoenix Rising"

**Domain:** OpenInference span emission (Go, K8s operator) + self-hosted Arize Phoenix on Kubernetes
**Researched:** 2026-07-15
**Confidence:** HIGH — every dependency claim below was verified against the locally-vendored `go.opentelemetry.io/otel*` v1.43.0 source (module cache), the Arize Phoenix Helm chart's `Chart.yaml`/`values.yaml` fetched live from `main`, and the OpenInference Go module's `go.mod`/`pkg.go.dev` page. Two items (exact chart/app version, and the OpenInference Go module's pre-1.0 status) are flagged MEDIUM because they move on a near-daily release cadence — re-verify the pinned numbers immediately before the implementation phase.

**Scope note:** This is an *additions* pass on top of an already-pinned stack (OTel Go v1.43, `pkg/otelai` helpers, `internal/otelinit` provider). The single biggest finding: **the Go-side span-emission mechanics need zero new go.mod dependencies** — W3C propagation, retroactive timestamps, and remote-span-context reconstruction are already present in the pinned `go.opentelemetry.io/otel` v1.43.0 family. The real additions are (1) one small official OpenInference Go module to replace hand-rolled attribute-key strings, and (2) a self-hosted Phoenix Helm chart (external install, not a TIDE subchart, per the TELEM-01 precedent).

## Recommended Stack

### Core Technologies (new for v1.0.8)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `go.opentelemetry.io/otel/propagation` | **v1.43.0 (already pinned, zero bump)** | W3C `traceparent` inject/extract across the manager → K8s Job pod boundary | Sub-package of the core `go.opentelemetry.io/otel` module already required at v1.43.0 — confirmed present in the local module cache (`$GOMODCACHE/go.opentelemetry.io/otel@v1.43.0/propagation/trace_context.go`). `propagation.TraceContext{}.Inject(ctx, propagation.MapCarrier{})` produces the header string to set as a Job pod env var; `.Extract(ctx, carrier)` on the reporter/executor side reconstructs a *remote* `SpanContext` via `trace.ContextWithRemoteSpanContext`. No new dependency, no version change. |
| `go.opentelemetry.io/otel/trace` `WithTimestamp` | **v1.43.0 (already pinned)** | Retroactive span creation with explicit start/end timestamps, driven from `events.jsonl` capture (which already has real wall-clock times per line) | Verified directly against the vendored source: `trace.WithTimestamp(t time.Time) SpanEventOption` (`trace@v1.43.0/config.go:251`) — `SpanEventOption` satisfies both `SpanStartOption` and `SpanEndOption`, so the exact same call works at `tracer.Start(ctx, name, trace.WithTimestamp(startTime))` and `span.End(trace.WithTimestamp(endTime))`. This is the whole mechanism the reporter Job needs to synthesize spans after the fact from a JSONL log — no separate "backdated span" API exists or is needed. |
| `go.opentelemetry.io/otel/trace` `NewSpanContext` / `ContextWithRemoteSpanContext` | **v1.43.0 (already pinned)** | Reconstruct the manager's dispatch-span identity inside the reporter Job process so synthesized spans parent under the correct trace | Verified present (`trace@v1.43.0/trace.go:244`, `context.go:29`). This is the seam the milestone's runtime-neutrality constraint depends on: the manager injects `traceparent`; both the JSONL-synthesizing adapter (today) and a self-instrumenting LangGraph runtime (future) extract it identically via `propagation.TraceContext{}`, so neither cares which side created the parent. |
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` | **v0.1.1** (tagged 2026-05-22; independently-versioned Go submodule, `go 1.25` floor — satisfied by TIDE's `go 1.26.0`) | Canonical, spec-generated attribute-key constants (`LLMInputMessages`, `LLMTokenCountPromptDetailsCacheRead`, `SpanKindAgent`, etc.) | **New dependency, zero transitive deps** (`go.mod` is two lines: module + `go 1.25`). Replaces the hand-rolled string constants in `pkg/otelai/attrs.go` (`keyLLMInputMessagesPrefix = "llm.input_messages"` etc.) with the package Arize itself generates from the Python source-of-truth spec. Directly serves the milestone's runtime-neutrality requirement #3 ("attribute/span-kind conventions follow OpenInference semconv exactly ... so Phoenix queries survive the runtime migration") — importing the canonical constants makes spec drift a compile-time `go get -u` diff instead of a silent string mismatch against a future `openinference-instrumentation-langchain`-authored span. Verified the constant *values* already match `pkg/otelai`'s hand-rolled ones (`llm.token_count.prompt_details.cache_read` / `cache_write` are identical strings) — this is a safe, behavior-preserving swap. |
| `github.com/Arize-ai/openinference/go/openinference-instrumentation` | **v0.1.1** (same tag/repo as above) | Context-attribute propagation (`session.id`, `metadata`) and `OPENINFERENCE_HIDE_*`-driven masking, if TIDE later wants to redact prompt/response content from spans | Optional for the milestone's stated scope (dispatch-chain + LLM-message spans) but same-repo, same-version, zero-friction addition — only pull it in if a masking/redaction requirement surfaces. Its one dependency is `go.opentelemetry.io/otel/trace` (already pinned). Not required to ship v1.0.8; listed so the decision to defer it is explicit rather than an oversight. |

### Supporting technology: Self-hosted Arize Phoenix (Kubernetes)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| Phoenix Helm chart | `oci://registry-1.docker.io/arizephoenix/phoenix-helm` **chart 10.0.0** (`appVersion: "18.0.0"`, image tag `arizephoenix/phoenix:version-18.0.0-nonroot`) | Self-hosted trace store + UI + OTLP ingest, installed as a documented separate `helm install`, NOT a TIDE subchart | Official chart, published by the Arize maintainers directly to Docker Hub OCI (verified `Chart.yaml` on `main`: `type: application`, `dependencies: [postgres@1.5.8 condition postgresql.enabled]`). Matches the milestone's stated posture exactly ("no subchart dependency, no version coupling to Arize's chart" — the TELEM-01 pattern already used for Prometheus in v1.0.7). **Caveat (MEDIUM confidence on the exact number):** this chart ships near-daily (9.0.14 → 9.0.22 → 10.0.0 across ~9 days as of this research date) — re-check the current chart/app version immediately before authoring INSTALL.md, and pin the exact version string in both `values.yaml` examples and CI, never `latest`/floating. |
| Bundled PostgreSQL (`postgresql.enabled: true`, chart **default**) | groundhog2k `postgres` chart **v1.5.8** (Phoenix chart's own pinned dependency) | Phoenix's own trace/eval/dataset store | This is Phoenix's storage, **not** TIDE's — does not touch TIDE's own "CRD-`.status`-only, no external DB" constraint (that constraint governs TIDE's orchestration state; Phoenix is an external, optional observability sink TIDE merely emits OTLP to). Recommend documenting this as the default path for the recipe: it is the chart's tested happy path, and Phoenix's own docs state Postgres is recommended for production ("Cannot be enabled simultaneously with `persistence.enabled=true`" — the two storage modes are mutually exclusive at the chart level). |
| SQLite-on-PVC alternative (`persistence.enabled: true`, `postgresql.enabled: false`) | N/A (bundled in the `arizephoenix/phoenix` image) | Lightweight single-binary storage for a kind/dev cluster or a low-traffic self-host | Default PVC size **20Gi**, `ReadWriteOnce`. Good fit for TIDE's own dev loop (kind clusters, `kind-tide-dogfood`-style throwaway or durable-but-single-operator clusters) where a second Postgres pod is pure overhead. Document as the "quickstart / single-operator" path, Postgres as the "durable / production" path — mirrors the chart's own two documented strategies exactly, don't invent a third. |
| OTLP ports: **4317** (gRPC) / **6006** (HTTP + UI) | Chart defaults, unconfigurable via `server.port` for HTTP only | Where TIDE's already-pinned `otlptracegrpc` exporter connects | TIDE's `internal/otelinit/provider.go` already builds an OTLP **gRPC** exporter (`otlptracegrpc.New`), so target Phoenix's **4317** gRPC port, not the 6006 HTTP/UI port. Confirmed via Phoenix's `docs/phoenix/self-hosting/configuration.mdx`: "By default, port 6006 is used for the UI and OTLP traces via HTTP, while port 4317 is used for OTLP traces via gRPC." No TIDE code path uses `otlptracehttp` — don't introduce it just to hit 6006. |

### What this milestone needs NO new dependency for

| Capability | Covered by |
|---|---|
| W3C `traceparent` propagation into Job pod env | `propagation.TraceContext{}` + `propagation.MapCarrier` — already in pinned `go.opentelemetry.io/otel` v1.43.0 |
| Retroactive span start/end timestamps | `trace.WithTimestamp(t)` — already in pinned `go.opentelemetry.io/otel/trace` v1.43.0 |
| Remote parent SpanContext reconstruction (reporter Job continuing the manager's trace) | `trace.NewSpanContext` + `trace.ContextWithRemoteSpanContext` — already in pinned `go.opentelemetry.io/otel/trace` v1.43.0 |
| OTLP auth header injection if Phoenix `auth.enableAuth` is left at its chart default (`true`) | `OTEL_EXPORTER_OTLP_HEADERS` env var — `otlptracegrpc.New` reads it automatically via the shared `oconf` package whenever the caller does **not** pass an explicit `WithHeaders(...)` option (TIDE's `provider.go` doesn't), per the sibling `otlpmetrichttp`/`otlptracehttp` doc comments in the same SDK family. Set it from a K8s Secret in the chart, no code change. |
| Routing traces to a named Phoenix project | `OTEL_RESOURCE_ATTRIBUTES=openinference.project.name=tide` — TIDE's `resource.WithFromEnv()` already honors `OTEL_RESOURCE_ATTRIBUTES`; the `openinference.project.name` key is Phoenix's documented canonical resource attribute (confirmed against `docs/phoenix/tracing/concepts-tracing/otel-openinference/resource.mdx`). **Important:** the alternative `x-project-name` HTTP header is HTTP-OTLP-only and does **not** work over gRPC — don't reach for it. |
| Reporter Job emitting OTel spans as a second binary | Reuse `internal/otelinit.NewTracerProvider` from `cmd/tide-reporter/main.go`. Confirmed by grep that `otelinit` is currently wired into `cmd/manager/main.go` only (`cmd/manager/otel_test.go`, `internal/otelinit/provider_test.go`) — `cmd/tide-reporter` has none yet. This is new *call-site* wiring, not a new dependency; same package, same module. |

## Installation

```bash
# Go — new OpenInference semconv + instrumentation modules (zero transitive deps beyond
# go.opentelemetry.io/otel/trace, already required)
go get github.com/Arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1
go get github.com/Arize-ai/openinference/go/openinference-instrumentation@v0.1.1   # only if masking/context-propagation is in scope
go mod tidy
```

```bash
# Self-hosted Phoenix — separate helm release, NOT a TIDE chart dependency
export CHART_URL=oci://registry-1.docker.io/arizephoenix/phoenix-helm
export CHART_VERSION=10.0.0   # RE-VERIFY at implementation time — chart ships ~daily

# Quickstart / single-operator (SQLite on PVC, no Postgres pod):
helm install tide-phoenix $CHART_URL --version $CHART_VERSION \
  --namespace tide-observability --create-namespace \
  --set postgresql.enabled=false \
  --set persistence.enabled=true \
  --set auth.enableAuth=false   # lab/demo only — see "What NOT to Use"

# Durable / production (bundled Postgres, chart default):
helm install tide-phoenix $CHART_URL --version $CHART_VERSION \
  --namespace tide-observability --create-namespace
  # postgresql.enabled=true and auth.enableAuth=true are chart defaults;
  # supply auth.secret + OTEL_EXPORTER_OTLP_HEADERS wiring — see Integration Notes
```

```bash
# Point TIDE's already-pinned OTLP exporter at Phoenix (bare host:port, NO scheme —
# see the WithEndpoint pitfall below)
helm upgrade tide ./charts/tide \
  --set otel.exporter.endpoint="tide-phoenix.tide-observability.svc.cluster.local:4317" \
  --set otel.serviceName="tide-controller-manager"
```

## Integration Notes

### The `otel.exporter.endpoint` value MUST be bare `host:port`, no scheme

`internal/otelinit/provider.go` reads `OTEL_EXPORTER_OTLP_ENDPOINT` with plain `os.Getenv` and passes it straight into `otlptracegrpc.WithEndpoint(endpoint)`. The OTel Go SDK's own doc comment for that option is explicit: *"The provided endpoint should resemble `example.com:4317` (no scheme or path)."* Because TIDE calls `WithEndpoint` explicitly (rather than relying on the SDK's own env-var autoconfigure path, which *does* accept a scheme via `WithEndpointURL`), setting the chart's `otel.exporter.endpoint` to `http://tide-phoenix...:4317` (the form the raw OTLP env-var spec technically allows) will NOT work here — use `tide-phoenix.tide-observability.svc.cluster.local:4317`. This is a pre-existing nuance in `provider.go`, not a new bug, but it is exactly the kind of thing that will silently no-op a Phoenix recipe if copy-pasted from generic OTel docs. Document this explicitly in INSTALL.md/observability.md.

### Phoenix's chart-level `auth.enableAuth` default is `true` — the raw Docker image default is `false`

Verified directly against the chart's `values.yaml` on `main`: `auth.enableAuth: true` is the **Helm chart's** opinionated default, even though Phoenix's own app-level docs say "Authentication is disabled by default" (true only for a bare `docker run`). Two supported recipe shapes:

- **Lab/demo (fastest path to a queryable trace tree):** `--set auth.enableAuth=false`. Zero secrets to manage, matches a throwaway kind cluster's ergonomics. Call this out as explicitly non-production in the docs.
- **Durable:** leave `auth.enableAuth=true`, supply `auth.secret` (a K8s Secret carrying `PHOENIX_SECRET` + admin password per the chart's documented keys), mint a Phoenix API key, and set `OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer <api-key>` on TIDE's manager/reporter Pods (sourced from a Secret, `EnvFrom`/`valueFrom.secretKeyRef` — same pattern already used at the three git-identity commit sites in `push_helpers.go`). No code change: `otlptracegrpc.New` picks up `OTEL_EXPORTER_OTLP_HEADERS` automatically because `provider.go` never calls `WithHeaders(...)` explicitly.

### Retroactive span synthesis shape (reporter Job, `internal/reporter`)

The reporter Job already parses the same stream-json shape via `internal/subagent/anthropic/stream_parser.go` (used live during dispatch) — the events.jsonl-driven span synthesis is a second consumer of that same per-line shape, read after the fact from the PVC-mounted `events.jsonl`. Per-line, each event already carries a real timestamp, so the synthesis loop is:

```go
ctx := propagation.TraceContext{}.Extract(context.Background(),
    propagation.MapCarrier{"traceparent": os.Getenv("TRACEPARENT")})
tracer := otel.Tracer("tide-reporter")
for _, ev := range events {
    _, span := tracer.Start(ctx, spanNameFor(ev), trace.WithTimestamp(ev.StartTime))
    span.SetAttributes(otelai.AgentInvocation(...)...)
    span.End(trace.WithTimestamp(ev.EndTime))
}
```

This is the exact mechanism the runtime-neutrality constraint depends on: a future self-instrumenting LangGraph runtime extracts the same injected `traceparent` and emits real-time spans via `openinference-instrumentation-langchain` instead — the reporter's synthesis path and the runtime's native path are structurally identical consumers of the same seam, gated by the "self-instrumenting capability flag" the milestone already specifies (reporter skips synthesis when the runtime self-instruments, avoiding double spans).

### `pkg/otelai` migration is additive, not a rewrite

`pkg/otelai/attrs.go`'s public surface (`LLMInputMessages`, `LLMOutputMessages`, `TokenCount`, `AgentInvocation`, `ArtifactPath`) stays — only the private `key*` string constants get sourced from `openinference-semantic-conventions` instead of being hand-typed. Confirmed value-for-value match on the token-count keys already in use, so this is a safe swap, not a behavior change. This also gains TIDE the constants it doesn't have yet but the spec defines (`LLMModelName`, `LLMProvider`, `LLMTokenCountTotal`, `MessageToolCalls`) for free, without hand-typing them — useful groundwork for whichever future phase adds `llm.model_name`/`llm.provider` attributes (both present in the OpenInference spec's example trace output but absent from TIDE's current `AgentInvocation` helper, which hard-codes `llm.system=anthropic` as a comment-documented future extension point).

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|--------------------------|
| Hand-rolled `pkg/otelai` constants stay, backed by `openinference-semantic-conventions` v0.1.1 | Keep the fully hand-rolled string constants (status quo) | Only if the pre-1.0 versioning of the official Go module (v0.1.1, tagged May 2026) is judged too immature to depend on. Given the module is zero-dependency and Arize-maintained as the Go port of the Python source-of-truth, the drift risk of staying hand-rolled is higher than the churn risk of a thin, dependency-free constants package. |
| Phoenix's bundled Postgres (chart default) for the "durable" recipe path | External/self-managed PostgreSQL via `database.url` | If the operator already runs a PostgreSQL fleet (e.g. via an operator like CloudNativePG) and wants Phoenix to reuse it rather than provisioning a second, chart-owned Postgres pod + PVC. The chart supports this via `postgresql.enabled=false` + `database.url` — worth a one-line callout in the docs, not the primary documented path. |
| SQLite-on-PVC for the "quickstart" recipe path | In-memory SQLite (`persistence.inMemory=true`) | Only for genuinely ephemeral demo/CI runs where losing all trace history on pod restart is acceptable — don't default a documented recipe to this, it will surprise a first-time operator. |
| OTLP **gRPC** (port 4317) | OTLP **HTTP** (port 6006, `/v1/traces`) | Never for this milestone — TIDE has zero code paths using `otlptracehttp`, and switching would be a real dependency + code change for no benefit (gRPC is already wired, already the pinned exporter). Keep HTTP as "Phoenix also supports this if you're not using TIDE's exporter" trivia in the docs, not a recommendation. |
| Documented separate `helm install` for Phoenix (TELEM-01 pattern) | Phoenix as a TIDE chart `dependencies:` subchart | If the project ever wants a true one-command `helm install tide` bring-up of the whole observability stack. Explicitly rejected by the milestone's own framing ("no subchart dependency, no version coupling to Arize's chart") and consistent with how Prometheus was handled in v1.0.7 — a subchart dependency means every TIDE chart bump has to track Arize's ~daily release cadence, which is untenable. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|--------------|
| `openinference-instrumentation-anthropic-sdk-go` (Go auto-instrumentor for `anthropics/anthropic-sdk-go`) | Requires `anthropic-sdk-go` **v1.43+** (needs `option.Middleware`, added in that release) — TIDE pins the Anthropic Go SDK at **v1.42.x**, below the floor. More fundamentally, it instruments *direct Go-SDK `/v1/messages` calls*; TIDE's subagent dispatch shells out to the `claude` CLI as a subprocess (`internal/subagent/anthropic/subagent.go`), so there is no in-process Anthropic Go SDK call to attach middleware to on today's dispatch path. | The events.jsonl-driven synthesis path in `internal/reporter` (this milestone). Revisit this package specifically if/when the deferred CACHE-F1 direct-SDK backend ships (per project memory: "direct-SDK subagent backend that sets the system prompt explicitly ... places `cache_control` breakpoints") — that backend would make in-process Go SDK calls and could genuinely use this middleware instead of JSONL-parsing. |
| `otlptracehttp` exporter | Would require a second code path, a second pinned dependency, and targets Phoenix's HTTP port (6006) which TIDE has never used — `otlptracegrpc` is already fully wired and works with Phoenix's 4317 gRPC port out of the box. | Existing `otlptracegrpc` (already pinned v1.43.0) |
| `auth.enableAuth=false` as the ONLY documented Phoenix recipe | It is the chart's non-default, and shipping it as the sole documented path would silently produce an unauthenticated trace store as the "official" TIDE recipe — a real security regression for anyone following INSTALL.md verbatim on a shared cluster. | Document both paths (see Integration Notes); make the "durable" (auth-on) path the one shown first, lab/demo path second and explicitly labeled non-production. |
| Pinning Phoenix's chart with `latest`/no version, or trusting the number in this doc without re-checking | The chart shipped 9 versions in roughly as many days at research time (`9.0.14` → `10.0.0`); the number here will likely be stale by the time this phase executes. | Pin an exact `--version` string, verified fresh, in both the `helm install` docs and any CI step that exercises the recipe. |
| A LangGraph-side Go equivalent for span emission | LangGraph is Python-only; there is no Go LangGraph runtime to instrument, so this concern doesn't apply to the Go side at all. | N/A — the runtime-neutrality contract is trace-context-compatibility (W3C `traceparent` + matching OpenInference attribute keys), not a shared code path. |

## Version Compatibility

| Package | Compatible With | Notes |
|---------|------------------|-------|
| `go.opentelemetry.io/otel/propagation` v1.43.0 | `go.opentelemetry.io/otel` v1.43.0 (already pinned) | Same module, no separate version to track — bundled in the core `otel` require line already in `go.mod`. |
| `go.opentelemetry.io/otel/trace` v1.43.0 `WithTimestamp`/`NewSpanContext` | `go.opentelemetry.io/otel/sdk` v1.43.0 (already pinned) | Both already required at identical `v1.43.0`; no coupling risk — this is the existing "otel trace API is v1.x stable" rule already documented in `CLAUDE.md`. |
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` v0.1.1 | Go `1.25` floor | TIDE's `go 1.26.0` directive satisfies this with headroom. Zero transitive dependencies — cannot introduce a version conflict elsewhere in `go.mod`. |
| `github.com/Arize-ai/openinference/go/openinference-instrumentation` v0.1.1 | `go.opentelemetry.io/otel/trace` v1.43.0 | Its only dependency is the trace API already pinned; safe to add independently of the semconv package. |
| `openinference-instrumentation-langchain` (Python, future LangGraph runtime, **not installed by this milestone**) | `langchain-core` 1.x + partner packages (`langchain-anthropic`, etc.); Python `>=3.10,<3.15` | Latest `0.1.67` (released 2026-07-01). Recorded here only so the Go-side attribute keys can be cross-checked against it: it emits the same flat `llm.input_messages.N.message.role/content` encoding and the same `OpenInferenceSpanKindValues` enum (`TOOL`, `CHAIN`, `LLM`, `RETRIEVER`, `EMBEDDING`, `AGENT`, `RERANKER`, `UNKNOWN`, `GUARDRAIL`, `EVALUATOR`, `PROMPT`) that the Go semconv module above exports — confirming the two sides will produce Phoenix-queryable-identical spans without any TIDE-side translation layer. |
| Phoenix Helm chart 10.0.0 | `postgres` subchart (groundhog2k) v1.5.8, pinned by Phoenix's own `Chart.yaml` | Do not bump the bundled Postgres subchart independently — same "let the parent's go.mod/Chart.yaml dictate" rule TIDE already applies to `k8s.io/*` and `ProtonMail/go-crypto`. |

## Sources

- `go.opentelemetry.io/otel@v1.43.0` local module cache (`propagation/trace_context.go`, verified live) — HIGH (primary source, matches pinned `go.mod`)
- `go.opentelemetry.io/otel/trace@v1.43.0` local module cache (`config.go:251` `WithTimestamp`, `trace.go:244` `NewSpanContext`, `context.go:29` `ContextWithRemoteSpanContext`) — HIGH (primary source)
- Context7 `/open-telemetry/opentelemetry-go` — `WithEndpoint`/`WithHeaders` env-var-fallback doc comments (otlptracegrpc/otlptracehttp/otlpmetrichttp share the `oconf` config package) — HIGH
- [Phoenix self-hosting: Kubernetes (Helm)](https://arize.com/docs/phoenix/self-hosting/deployment-options/kubernetes-helm) — install commands, OCI chart URL — HIGH (official docs, fetched live)
- [Phoenix self-hosting: configuration](https://arize.com/docs/phoenix/self-hosting/configuration) via Context7 `/arize-ai/phoenix` — `PHOENIX_PORT` (6006), `PHOENIX_GRPC_PORT` (4317) — HIGH (official docs)
- `https://raw.githubusercontent.com/Arize-ai/phoenix/main/helm/Chart.yaml` (fetched live 2026-07-15) — chart version 10.0.0, appVersion 18.0.0, postgres subchart 1.5.8 pin — HIGH (primary source, but MEDIUM confidence the number is still current given ~daily release cadence)
- `https://raw.githubusercontent.com/Arize-ai/phoenix/main/helm/values.yaml` (fetched live 2026-07-15) — `auth.enableAuth: true` default, `postgresql.enabled: true` default, `persistence.enabled: false` default, image `arizephoenix/phoenix:version-18.0.0-nonroot` — HIGH (primary source)
- [Phoenix: authentication](https://github.com/arize-ai/phoenix/blob/main/docs/phoenix/self-hosting/features/authentication.mdx) via Context7 — `PHOENIX_ENABLE_AUTH`/`PHOENIX_SECRET`, app-level default is disabled (contrasted with the chart's own `true` default) — HIGH
- [Phoenix: setup-projects](https://github.com/arize-ai/phoenix/blob/main/docs/phoenix/tracing/how-to-tracing/setup-tracing/setup-projects.mdx) via Context7 — `openinference.project.name` resource attribute, `x-project-name` header is HTTP-OTLP-only — HIGH
- Docker Hub `arizephoenix/phoenix-helm` tags page (fetched live 2026-07-15) — release cadence evidence (9.0.14 → 10.0.0 across ~9 days) — HIGH (primary source)
- [OpenInference spec: semantic_conventions.md](https://github.com/arize-ai/openinference/blob/main/spec/semantic_conventions.md) via Context7 `/arize-ai/openinference` — flat-key message encoding, span-kind enum — HIGH
- [OpenInference Go semantic-conventions README + go.mod](https://github.com/arize-ai/openinference/blob/main/go/openinference-semantic-conventions/README.md) (fetched live) — module path, `go 1.25`, exported constants — HIGH
- [pkg.go.dev: openinference-semantic-conventions](https://pkg.go.dev/github.com/Arize-ai/openinference/go/openinference-semantic-conventions) (fetched live 2026-07-15) — v0.1.1, published 2026-05-22, full constant listing — HIGH
- [pkg.go.dev: openinference-instrumentation (Go)](https://pkg.go.dev/github.com/Arize-ai/openinference/go/openinference-instrumentation) (fetched live) — v0.1.1, suppression/propagation/masking API, dependency on `otel/trace` — HIGH
- [openinference-instrumentation-anthropic-sdk-go](https://github.com/Arize-ai/openinference/tree/main/go/openinference-instrumentation-anthropic-sdk-go) — `anthropic-sdk-go` v1.43+ floor (`option.Middleware`) — MEDIUM (WebSearch-summarized, not primary-source-fetched, but consistent with the module's stated purpose)
- [openinference-instrumentation-langchain PyPI](https://pypi.org/project/openinference-instrumentation-langchain/) (fetched live 2026-07-15) — v0.1.67, released 2026-07-01, `langchain-core` + partner packages — HIGH (primary source)
- Local repo grounding: `go.mod` (`go 1.26.0`, `go.opentelemetry.io/otel*` v1.43.0 pins), `charts/tide/values.yaml:410-415` (`otel.exporter.endpoint: ""` already present), `internal/otelinit/provider.go`, `pkg/otelai/attrs.go`, `cmd/tide-reporter/main.go`, `internal/controller/push_helpers.go` (existing `corev1.EnvVar` pattern for per-Job env injection) — HIGH (primary source, read directly)

---
*Stack research for: TIDE v1.0.8 "Phoenix Rising" — OpenInference trace emission + self-hosted Arize Phoenix*
*Researched: 2026-07-15*

