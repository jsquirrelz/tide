---
phase: 04
plan: 03
subsystem: observability
tags: [opentelemetry, openinference, tracerprovider, otel-sdk, pkg-otelai, internal-otelinit, d-o3, d-o4, d-o5, pitfall-24]
dependency_graph:
  requires:
    - "04-01 — central Prometheus registry (shipped; this plan establishes the OTel sibling)"
  provides:
    - "pkg/otelai.{LLMInputMessages,LLMOutputMessages,TokenCount,AgentInvocation,ArtifactPath} — five OpenInference attribute helpers (D-O4)"
    - "pkg/otelai.Message — public struct {Role, Content} for input/output flattening"
    - "internal/otelinit.NewTracerProvider(ctx) — TracerProvider lifecycle with no-op fallback (D-O3)"
    - "internal/otelinit.ShutdownFunc — deferred-from-main shutdown shape"
    - "cmd/manager OTel boot path + deferred Shutdown (5s context.Background bounded)"
    - "go.mod direct deps: otel/sdk@v1.43.0, otel/exporters/otlp/otlptrace/otlptracegrpc@v1.43.0, otel/{root,trace}@v1.43.0"
  affects:
    - "04-05 (up-stack reconciler OBS-04 spans) — emits AgentInvocation + ArtifactPath via this seam"
    - "04-06 (boundary push trigger) — span emission across the push lifecycle"
    - "04-09 (Helm chart OTel env block + ServiceMonitor) — Helm sets OTEL_EXPORTER_OTLP_ENDPOINT + OTEL_TRACES_SAMPLER + arg"
    - "All Phase 4 reconciler edits that emit OpenInference attributes — single import path: github.com/jsquirrelz/tide/pkg/otelai"
tech_stack:
  added:
    - "go.opentelemetry.io/otel v1.43.0 (was v1.41.0 indirect; now direct)"
    - "go.opentelemetry.io/otel/sdk v1.43.0 (new direct)"
    - "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0 (was v1.40.0 indirect)"
    - "go.opentelemetry.io/otel/trace v1.43.0 (was v1.41.0 indirect; now direct)"
  patterns:
    - "env-driven OTel config — OTEL_EXPORTER_OTLP_ENDPOINT discriminates real vs no-op path; OTEL_TRACES_SAMPLER + _ARG govern sampling (no WithSampler in code)"
    - "lazy gRPC exporter — otlptracegrpc.New does not dial immediately; constructor success means syntactic validity, not collector reachability"
    - "no-op fallback via tracenoop.NewTracerProvider — invalid SpanContext, zero network traffic, drop-in compatible with otel.Tracer(...) at call sites"
    - "static source-grep for required wire-up substrings (mirrors plan 04-01's TestMetricsBlankImportPresent pattern)"
    - "Go-comment-aware source grep in tests — strip // and /* */ before searching, so doc-comments documenting a forbidden rule do not trip the test"
key_files:
  created:
    - pkg/otelai/attrs.go
    - pkg/otelai/attrs_test.go
    - pkg/otelai/doc.go
    - internal/otelinit/provider.go
    - internal/otelinit/provider_test.go
    - internal/otelinit/doc.go
    - cmd/manager/otel_test.go
  modified:
    - cmd/manager/main.go
    - go.mod
    - go.sum
decisions:
  - "Test 1 in internal/otelinit asserts concrete type 'noop.TracerProvider' (struct value), NOT '*noop.TracerProvider' (pointer) — the OTel Go SDK v1.43 noop package returns a struct value from NewTracerProvider. Plan's expected string was wrong; corrected at test time, implementation unchanged. [Rule 1]"
  - "WithSampler( source-grep test in internal/otelinit strips Go // and /* */ comments before searching, so the file's two documentation comments mentioning WithSampler (Pitfall 24 citations) do not trip the rule while the actual code constraint remains enforced. Plan's verification used shell-comment-aware grep (strips # lines) which would falsely flag the doc comments — Go test is the authoritative enforcement."
  - "signalCtx (from ctrl.SetupSignalHandler) is established once at the top of main, threaded into BOTH otelinit.NewTracerProvider AND mgr.Start. Previously SetupSignalHandler was called inline at mgr.Start only; sharing it avoids registering two signal handlers (controller-runtime panics on second call)."
  - "OTel Shutdown defer uses context.Background() + 5s timeout (NOT signalCtx-derived) because signalCtx is already cancelled when the defer runs at end-of-process. The batch span processor needs a live context to flush outstanding spans to the collector."
  - "go.mod adds otel/sdk + otlptracegrpc + otel as direct deps. otel/trace/noop and otel/sdk/trace are sub-packages of those modules and don't get separate go.mod entries — verified via go build."
  - "otel.SetTracerProvider is called in BOTH the no-op AND real-SDK branches of NewTracerProvider. The no-op set is intentional — without it, reconciler code calling otel.Tracer(...) would land on the SDK's default global (which is also a no-op but is a different no-op object), making TestOTelGlobalTracerProviderSet fail with surprising semantics."
metrics:
  duration_minutes: 32
  completed_date: 2026-05-19
  tasks_completed: 3
  files_created: 7
  files_modified: 3
  commits: 6
---

# Phase 4 Plan 03: pkg/otelai + internal/otelinit Summary

Ship the OpenTelemetry foundation — five OpenInference attribute helpers under `pkg/otelai` (D-O4) plus the env-driven TracerProvider lifecycle under `internal/otelinit` (D-O3) — and wire both to `cmd/manager` boot so the orchestrator emits OpenInference-attributed spans across the dispatch chain when an OTLP endpoint is configured, and degrades to zero-overhead no-op when it is not.

## What landed

### `pkg/otelai/` — five OpenInference attribute helpers (D-O4)

Public surface (locked at exactly five helpers + one struct):

| Helper                          | Returns                  | Arity | OpenInference keys                                                                                  |
| ------------------------------- | ------------------------ | ----- | --------------------------------------------------------------------------------------------------- |
| `LLMInputMessages(msgs)`        | `[]attribute.KeyValue`   | 2*N   | `llm.input_messages.<i>.message.role`, `.content`                                                   |
| `LLMOutputMessages(msgs)`       | `[]attribute.KeyValue`   | 2*N   | `llm.output_messages.<i>.message.role`, `.content`                                                  |
| `TokenCount(p, c, cr, cw)`      | `[]attribute.KeyValue`   | 4     | `llm.token_count.{prompt,completion,prompt_details.cache_read,prompt_details.cache_write}`          |
| `AgentInvocation(name, role, level)` | `[]attribute.KeyValue` | 5     | `openinference.span.kind=AGENT`, `llm.system=anthropic`, `agent.{name,role,invocation.level}`       |
| `ArtifactPath(path)`            | `attribute.KeyValue`     | 1     | `gen_ai.artifact_path`                                                                              |

`Message struct { Role string; Content string }` is the input shape for the message-flattening helpers. All attribute keys are package-level constants — single source of truth so spec drift surfaces in one location.

**D-O5 enforcement at the public API surface.** `TestNoPayloadHelperOnPublicSurface` source-greps `attrs.go` for forbidden exported identifiers (`Payload`, `InlinePayload`, `RawContent`, `Body`, `MessageBody`). Any future helper that would accept inline LLM payload bytes as a top-level attribute value fails CI. `pkg/otelai/doc.go` documents this contract verbatim with the Arize spec URL.

### `internal/otelinit/` — TracerProvider lifecycle with no-op fallback (D-O3)

`NewTracerProvider(ctx)` returns `(trace.TracerProvider, ShutdownFunc, error)`:

- **`OTEL_EXPORTER_OTLP_ENDPOINT` empty** → `tracenoop.NewTracerProvider()` + no-op `Shutdown` closure. `otel.SetTracerProvider` is still called so reconciler code using `otel.Tracer(...)` resolves to this TP and incurs zero overhead. `kind` clusters and local dev work without an OTLP collector.
- **`OTEL_EXPORTER_OTLP_ENDPOINT` set** → `otlptracegrpc.New(ctx, WithEndpoint(endpoint), WithInsecure())` (lazy connect) + `sdktrace.NewTracerProvider(WithBatcher(exp), WithResource(res))`. `resource.New` aggregates `WithFromEnv` + `WithProcess` + `WithTelemetrySDK`, so `OTEL_SERVICE_NAME` + `OTEL_RESOURCE_ATTRIBUTES` flow through unchanged.

**Pitfall 24 mitigation enforced at test time.** Provider construction omits `WithSampler(...)` so `OTEL_TRACES_SAMPLER` + `OTEL_TRACES_SAMPLER_ARG` env vars govern (Helm chart default: `parentbased_traceidratio` arg `0.1`). The file contains two `WithSampler(` substrings — both inside Go `//` documentation comments citing the rule. `TestNoWithSamplerInSource` strips Go comments before searching, so the doc-comments are auditable AND the rule is enforced.

### `cmd/manager/main.go` — OTel boot + deferred Shutdown

Inserted between `setupLog := ctrl.Log.WithName("setup")` and `// 1. Load runtime config`:

```go
signalCtx := ctrl.SetupSignalHandler()

tp, otelShutdown, err := otelinit.NewTracerProvider(signalCtx)
if err != nil { setupLog.Error(err, "otel init failed"); os.Exit(1) }
_ = tp
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := otelShutdown(shutdownCtx); err != nil {
        setupLog.Error(err, "otel shutdown failed")
    }
}()
```

`signalCtx` is established once at the top of `main` and threaded into both `NewTracerProvider` and `mgr.Start(signalCtx)`. Previously `ctrl.SetupSignalHandler()` was inlined at `mgr.Start(...)`; sharing the context avoids registering two signal handlers (controller-runtime panics on the second call).

`context.Background()` (not `signalCtx`) seeds the shutdown timeout because `signalCtx` is already cancelled when this defer runs at end-of-process — the batch span processor needs a live context to flush outstanding spans before the binary exits.

## Test coverage

- `pkg/otelai/attrs_test.go` — 7 tests: per-helper key+value assertions via `reflect.DeepEqual` (locks the flat-keyed encoding), D-O5 source-grep against forbidden exported identifiers, empty-input no-panic.
- `internal/otelinit/provider_test.go` — 5 tests: no-op type assertion when endpoint empty, SDK TracerProvider when endpoint set, Pitfall 24 source-grep (Go-comment-stripped), `otel.GetTracerProvider() == tp` global wiring, no-op tracer produces invalid SpanContext.
- `cmd/manager/otel_test.go` — 2 tests: `TestOtelInitWiredInMain` (static source-grep for import + constructor + deferred Shutdown), `TestManagerOtelInit` (runtime audit of the no-op global handle).

```
ok  github.com/jsquirrelz/tide/pkg/otelai     1.373s
ok  github.com/jsquirrelz/tide/internal/otelinit  1.784s
ok  github.com/jsquirrelz/tide/cmd/manager    2.002s
```

All three packages pass with `-race`. `make tide-lint` clean. `go build ./...` clean. `go vet` clean on all new packages.

## Plan verification block

| Check | Result |
|-------|--------|
| `go test ./pkg/otelai/... ./internal/otelinit/... ./cmd/manager/... -race -v` | PASS |
| `WithSampler(` count in `internal/otelinit/provider.go` (Go-comment-stripped) | 0 |
| `otelinit.NewTracerProvider` count in `cmd/manager/main.go` | 1 |
| `make tide-lint` | clean (exit 0) |
| `go.mod` includes `otel/sdk`, `otel/exporters/otlp/otlptrace/otlptracegrpc`, `otel`, `otel/trace` — all v1.43.0 | yes |

`otel/trace/noop` and `otel/sdk/trace` do NOT appear as separate go.mod entries — they are sub-packages of `otel/trace` and `otel/sdk` respectively, pulled in transitively. Verified via `go build`.

## What downstream plans now consume

| Downstream plan | Consumes |
|----------------|----------|
| **04-05** (up-stack reconciler OBS-04 spans) | `pkg/otelai.AgentInvocation` + `ArtifactPath` for `tide.dispatch.{milestone,phase,plan,task}` spans |
| **04-06** (boundary push trigger) | Same — push lifecycle spans inherit the same attribute vocabulary |
| **04-09** (Helm chart OTel env block) | Sets `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_SAMPLER=parentbased_traceidratio`, `OTEL_TRACES_SAMPLER_ARG=0.1` — these flow into the binary's existing no-WithSampler constructor without code change |
| All Phase 4 span-emitting sites | Single import path: `"github.com/jsquirrelz/tide/pkg/otelai"` |

## TDD Gate Compliance

All three tasks followed strict RED → GREEN cycles. Commit ledger on branch `worktree-agent-af9b824f772add176`:

| Task | Phase | Commit    | Type | Subject                                                       |
| ---- | ----- | --------- | ---- | ------------------------------------------------------------- |
| 1    | RED   | `fe2dd8a` | test | pkg/otelai attribute-helper tests fail to compile             |
| 1    | GREEN | `26553fd` | feat | pkg/otelai five OpenInference attribute helpers (D-O4)        |
| 2    | RED   | `38ff5b0` | test | internal/otelinit TracerProvider lifecycle tests fail         |
| 2    | GREEN | `737838d` | feat | internal/otelinit TracerProvider lifecycle (D-O3)             |
| 3    | RED   | `9056d61` | test | cmd/manager OTel wire-up tests fail                           |
| 3    | GREEN | `35467d7` | feat | wire internal/otelinit into cmd/manager (D-O3)                |

Six commits. Each RED gate was verified by build failure (`undefined:` errors for missing symbols) before the GREEN implementation landed. The GREEN commits are the only places that introduce production code.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] OTel Go SDK v1.43 returns `noop.TracerProvider` as a struct value, not a pointer**

- **Found during:** Task 2 GREEN test run
- **Issue:** The plan's Test 1 asserted `reflect.TypeOf(tp).String() == "*noop.TracerProvider"`. First run failed: actual type is `noop.TracerProvider` (struct value). Verified via standalone debug test against `tracenoop.NewTracerProvider()` — the noop package returns a struct value, not a `*TracerProvider`.
- **Fix:** Corrected both Task-2 type-equality assertions in `internal/otelinit/provider_test.go` (one positive, one negative — same string in both directions). Implementation untouched: it correctly returns whatever `tracenoop.NewTracerProvider()` returns; the test now asserts the right concrete shape.
- **Files modified:** `internal/otelinit/provider_test.go`
- **Commit:** `737838d` (Task 2 GREEN — test correction landed alongside the implementation)
- **Why this counts as Rule 1:** the plan's expected string was a stale hypothesis about the SDK shape. Locking that hypothesis into the test would have produced a false-positive RED gate in CI on every future OTel SDK upgrade. Correcting it to match runtime reality is a bug-fix at the test layer.

### Methodology adjustment (no checkpoint)

**Go-comment-aware source-grep in `TestNoWithSamplerInSource`.** The plan's bash verification used `grep -v '^#' internal/otelinit/provider.go | grep -c "WithSampler("` to assert zero `WithSampler(` occurrences. That command treats `#`-prefixed lines as comments — which is shell syntax, not Go. Go uses `//` and `/* */`. The plan's grep would falsely flag the file's two documentation-comment citations of Pitfall 24.

The Go test (`TestNoWithSamplerInSource`) strips Go comments via a small lexer-style scanner before grepping, so:
- Documentation comments mentioning `WithSampler(` are **allowed** (good — they cite the rule for future readers).
- A real `WithSampler(` call in code would still be **caught** (the rule remains enforced).

I noted this in commit `737838d`'s body and in the decisions block at the top of this SUMMARY. The Go test is the authoritative enforcement; the shell verification's spurious count of 2 is documentation-driven, not a real violation.

### Architectural decisions auto-applied (no checkpoint)

- **Establish `signalCtx` early and share it.** The plan's Task 3 action said "after `ctx := signal.SetupSignalHandler()` (or wherever the manager's context is established — grep for `SetupSignalHandler` or `signal.NotifyContext`)". The existing code had `ctrl.SetupSignalHandler()` inlined at `mgr.Start(...)` only — no shareable ctx. I established `signalCtx := ctrl.SetupSignalHandler()` once right after `setupLog` initialization and threaded it into both the OTel init AND the eventual `mgr.Start(signalCtx)` call. This avoids registering two signal handlers (controller-runtime panics on the second call) and follows the same pattern the plan asked for.
- **`otel.SetTracerProvider` in the no-op branch too.** Without it, reconciler code calling `otel.Tracer(...)` lands on the SDK's default global (a different no-op object), so `TestOTelGlobalTracerProviderSet` would fail with surprising semantics. Both branches now set the global TP explicitly.

## Known Stubs

None. All five public helpers ship live. The TracerProvider lifecycle is real (no-op + SDK). The cmd/manager wire-up is live; the binary builds and emits no errors at boot with `OTEL_EXPORTER_OTLP_ENDPOINT` either set or unset.

## Threat Flags

None new. The plan's `<threat_model>` (T-04-O3 sampler tampering, T-04-O4 PII via traces, T-04-O5 etcd/collector bloat, T-04-O3-noop availability) is fully mitigated:

- **T-04-O3 (sampler tampering):** `WithSampler(` absent from compiled provider.go; `TestNoWithSamplerInSource` enforces at PR time; doc-comments cite Pitfall 24 with rationale.
- **T-04-O4 (PII via traces):** D-O5 enforced at the public API surface by `TestNoPayloadHelperOnPublicSurface`; no helper accepts inline payload content as an attribute value.
- **T-04-O5 (etcd/collector bloat):** `ArtifactPath` is the only payload-reference helper; it emits a single PVC-path string (~256 bytes max).
- **T-04-O3-noop (availability):** `OTEL_EXPORTER_OTLP_ENDPOINT=""` returns a real but no-op TracerProvider; manager start succeeds; no network traffic. Verified by `TestNoOpFallbackWhenEndpointEmpty` + `TestNoOpTracerProducesInvalidSpanContext`.

No new threat surface introduced.

## Self-Check: PASSED

Files exist:
- `pkg/otelai/attrs.go`
- `pkg/otelai/attrs_test.go`
- `pkg/otelai/doc.go`
- `internal/otelinit/provider.go`
- `internal/otelinit/provider_test.go`
- `internal/otelinit/doc.go`
- `cmd/manager/otel_test.go`
- `cmd/manager/main.go` (modified)
- `go.mod` / `go.sum` (modified)

Commits exist on worktree branch (`git log --oneline 3c3c266..HEAD`):
- `fe2dd8a` test(04-03): RED — pkg/otelai
- `26553fd` feat(04-03): GREEN — pkg/otelai
- `38ff5b0` test(04-03): RED — internal/otelinit
- `737838d` feat(04-03): GREEN — internal/otelinit
- `9056d61` test(04-03): RED — cmd/manager wire-up
- `35467d7` feat(04-03): GREEN — cmd/manager wire-up

Tests pass with `-race` on all three target packages. `make tide-lint` clean. `go build ./...` clean. Plan verification block fully satisfied.
