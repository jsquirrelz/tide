# Phase 42: Trace-Context Foundation + Planner-Level Span Emission - Research

**Researched:** 2026-07-15
**Domain:** OTel Go span synthesis inside K8s controller-runtime reconcilers; OpenInference semantic-convention Go module adoption
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Failure-path span coverage
- **D-01:** Spans emit for **succeeded AND failed** planner-Job completions. Failed completions carry OTel span status `Error`. ATTR-01/02 success criteria formally bind only succeeded levels, so attribute-degraded failure spans do not violate the phase gate.
- **D-02:** **One span per Job attempt**, not per level. Retries (reject/re-plan, `resume --retry-failed`) each produce their own span with that attempt's real `Job.Status.{StartTime,CompletionTime}` — retries are visible in the trace timeline. This aligns with the locked idempotency rule: span creation gates on the same state-transition edges that gate Job creation, which fire once per attempt.
- **D-03:** Failure detail rides as **span status + reason attributes**: status `Error` with the classified Reason as status description, PLUS the envelope's `ExitCode`/`Reason` as span attributes when the envelope is readable — failure class stays queryable in Phoenix's filter DSL.
- **D-04:** When the envelope is unreadable (`envReadOK=false` — possible even on succeeded Jobs), **emit a degraded span anyway**: usage attributes simply absent, plus a marker attribute noting the degradation. Observability must not gate on envelope health. Researcher must pin down a non-envelope source for the resolved model (spec/status) so `llm.model_name` survives degradation where possible — note `plan_controller.go:427`'s comment that the resolved model historically lived only in the PVC envelope.

#### ATTR-03 custom-key policy
- **D-05:** Every spec-backed attribute key resolves from the official `openinference-semantic-conventions` Go module. TIDE-custom keys with no spec counterpart (`gen_ai.artifact_path`, `agent.invocation.level`, and any others research identifies) are **renamed into an explicit `tide.*` namespace** (e.g. `tide.invocation.level`) — nothing masquerades as spec vocabulary, and the `gen_ai.*` squat on the rejected OTel GenAI namespace dies. Renames are free now: zero production call sites, zero consumers. The researcher determines which bucket each existing key falls into (module-defined vs custom).
- **D-06:** The module is pinned **exactly at v0.1.1 — no drift-guard test** (user explicitly declined the drift test). `go.mod` freezes the version; bumps are deliberate PRs reviewed by diff.

#### Attribute value semantics
- **D-07:** `llm.provider` (and `llm.system`) values are **derived from dispatch data** — the provider identity the manager already knows per dispatch (Project spec / provider abstraction) — replacing the hardcoded `llmSystem = "anthropic"` constant in `attrs.go`. One less site to hunt down when the OpenAI backend or LangGraph runtime lands; matches the runtime-neutrality lock.
- **D-08:** Token-count encoding goes **spec-exact, with license to re-map the existing four-way split**. TIDE's current helper encodes `llm.token_count.prompt` as *uncached-only* tokens (disjoint from `prompt_details.cache_read`/`cache_write`); if research confirms Phoenix/OpenInference treat `prompt_details.*` as subsets OF the prompt count, Phase 42 re-maps the split at the emission layer and computes `llm.token_count.total` per Phoenix's documented formula. Correct cost math beats a minimal diff — and the semantics change is free with zero call sites. Researcher must verify the exact Phoenix cost formula against its docs.

### Claude's Discretion
- **Mid-milestone trace shape** (user chose not to discuss): whether Phase 42's four planner spans already share the deterministic Project-UID TraceID (grouped but unparented in Phoenix until 43) or stay independent roots until Phase 43 threads them. Planner picks whichever composes cleanest with 43's parenting work; research's "self-rooted spans appear in Phoenix as soon as this lands" framing is the prior.
- Span/`agent.name` naming (existing docstring convention `tide.dispatch.<level>` is the prior), exact model-ID form (exact resolved ID per the v1.0.7 exact-ID pricing precedent), and how module constants surface inside `pkg/otelai` (direct use vs local re-export).
- Whether `agent.role`/`agent.name` are module-backed or fall under the D-05 `tide.*` rename — a research fact, not a user decision; apply D-05's policy to whatever research finds.

### Deferred Ideas (OUT OF SCOPE)

#### Reviewed Todos (not folded)
- `2026-07-03-signed-commits-verified-badge.md` (GPG signing / SIGN-02..04) — keyword false-positive; git-identity scope, no tracing overlap.
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` (W-2 candidate finding) — dispatch-gate ordering concern in the same controllers this phase touches, but a different concern; stays a next-milestone candidate.
- `2026-07-12-task-dispatch-gate-order-divergence.md` (W-2 sibling finding) — same disposition as above.
- `cache-f1-direct-sdk-cross-pod-caching.md` (CACHE-F1) — deferred vNext+; no overlap.

Also explicitly out of scope per the phase boundary: Task-level span emission, W3C `traceparent` injection into Job/reporter env (PROP-01), the `.status.trace` CRD field and durable ID persistence (PROP-02), parenting all levels into one connected tree (TRACE-02). Phase 42's spans stand alone; Phase 43 threads them.
</user_constraints>

## Summary

Phase 42 is pure composition on top of already-pinned dependencies: `go.opentelemetry.io/otel` v1.43.0's `trace.WithTimestamp`/`propagation.TraceContext` primitives (verified present, zero go.mod bump) and `pkg/otelai`'s five attribute helpers (verified zero production call sites — this phase is their first caller). The milestone-level research (`.planning/research/{SUMMARY,ARCHITECTURE,PITFALLS,STACK}.md`) already worked out the retroactive-synthesis pattern in detail and flagged Phase 1 (= this phase) as HIGH confidence, skip-research. This pass verifies the four research questions CONTEXT.md explicitly assigned (D-04 model source, D-05 module key inventory, D-07 provider plumbing, D-08 token/cost formula) against primary sources — the actual downloaded Go module, the actual `proxy.golang.org`/`sum.golang.org` registries, and the actual four completion-handler call sites — and surfaces three things the milestone-level pass did not resolve: (1) `batchv1.JobStatus.CompletionTime` is documented to be set **only on success**, never on Failed Jobs, which directly threatens D-01's "spans emit for succeeded AND failed" requirement unless the handlers fall back to the terminal `JobCondition.LastTransitionTime`; (2) `completedJob` can legitimately be `nil` (a real, already-exercised code path — the Job TTL-GC'd while the level was still `Running`), meaning span synthesis needs a defined behavior for "no Job object at all," not just "Job exists but envelope unreadable"; (3) `tide resume --retry-failed` does **not** delete and recreate the deterministic-named planner Job — it re-runs `handleJobCompletion` on the *same* terminal Job object — which means D-02's "one span per Job attempt" cannot be satisfied by keying off Job UID; it needs a dedicated, envReadOK-independent durable marker mirroring (not reusing) the existing `XRolledUpUID` pattern.

All four assigned research questions resolve cleanly and with primary-source confidence: the resolved model and the resolved provider vendor are both already available at completion time via `ResolveProvider(project, level, r.Deps.HelmProviderDefaults)` — the exact same pure, nil-safe function already called at dispatch time — so **no new persistence and no envelope change are needed** for ATTR-01. The `openinference-semantic-conventions` Go module (downloaded and read directly from source, cross-verified against `proxy.golang.org` and `sum.golang.org`) defines `LLMModelName`, `LLMProvider`, `LLMTokenCountTotal` and all eight of TIDE's currently-hand-rolled spec-aligned keys — but does **not** define `agent.role`, `agent.invocation.level`, or any `gen_ai.*` key, confirming D-05's rename bucket for exactly those three. Phoenix's own cost-calculator source and a real example trace (both fetched via Context7 from the official `arize-ai/phoenix` repo) confirm `llm.token_count.prompt_details.cache_read`/`cache_write` are **subsets of** `llm.token_count.prompt`, not additions to it — `total = prompt + completion` with no separate double-counting risk — which reverses TIDE's current "prompt = uncached-only" encoding exactly as D-08 anticipated.

**Primary recommendation:** Build `pkg/otelai/tracecontext.go` first (pure, zero K8s deps), then wire retroactive `AGENT`-kind span synthesis into the four completion handlers using a **new, level-scoped, envReadOK-independent durable marker** (mirroring but not reusing `XRolledUpUID`) for D-02/D-04 idempotency, sourcing `llm.model_name`/`llm.provider` via a second `ResolveProvider` call (not a new envelope field), and swapping `pkg/otelai/attrs.go`'s hand-rolled key constants for `semconv.*` per the exact table below.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Trace-context primitives (`TraceIDFromUID`, `FormatTraceparent`, `ExtractRemoteParent`) | API / Backend (Go library, `pkg/otelai`) | — | Pure functions, no K8s client, no I/O — a library concern, not a controller concern |
| Span synthesis at Job completion | API / Backend (`internal/controller` reconcilers) | — | Controller-runtime reconcile bodies are the only place `completedJob.Status` and the resolved `Project`/`ProviderSpec` are both in scope simultaneously |
| OpenInference attribute key mapping | API / Backend (`pkg/otelai/attrs.go`) | — | Pure attribute construction; already the locked "5 helpers" surface (D-O4) |
| Span-emission idempotency marker | Database / Storage (CRD `.status` via K8s API / etcd) | API / Backend (reconciler write path) | Must survive manager restart and reporter-Job TTL-GC — `.status` is TIDE's only durable, resumable state per the project's CRD-status-only persistence rule |
| OTLP export / collector ingest | External Service (Arize Phoenix, out-of-cluster or separate namespace) | — | `otlptracegrpc` already wired in `internal/otelinit`; this phase adds callers, not new export plumbing |

## Project Constraints (from CLAUDE.md)

- **GSD Workflow Enforcement**: no direct repo edits outside `/gsd:execute-phase` or an approved PLAN.md — this research feeds planning, not implementation.
- **`charts/tide/values.yaml` is a FIXED contract** — this phase adds zero chart values (sampler/endpoint plumbing already exists from a prior phase); do not propose chart edits.
- **No `WithSampler(...)` in Go source** — `internal/otelinit/provider.go` is enforced by `TestNoWithSamplerInSource` (source-grep). Phase 42 code must never call `sdktrace.WithSampler` anywhere; sampling stays env-driven.
- **OpenInference semconv, not OTel GenAI semconv** — locked in PROJECT.md's runtime-neutrality section; this research's entire D-05 key table follows that convention exclusively.
- **`make test-int` exit code, not "Ginkgo green"** — any new envtest specs this phase adds must be verified via the `MAKE_EXIT` echo and a `grep -nE '^--- FAIL|^FAIL\s'` pass, not just the Ginkgo summary line (per CLAUDE.md's Phase-7 lesson).
- **Verify before claiming** — every span-emission claim in an eventual VERIFICATION.md must be backed by an actual Phoenix/in-memory-exporter read, not "should work."

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ATTR-01 | Every AGENT/LLM span carries `llm.model_name` and `llm.provider` | D-04 resolves the non-envelope model source (`ResolveProvider(...).Model`, re-derivable at completion time); D-07 resolves the provider source (`ResolveProvider(...).Vendor`, same call) — see Architecture Pattern 1 and Code Examples |
| ATTR-02 | `llm.token_count.total` emitted alongside the existing prompt/completion/cache splits | D-08 verifies the exact Phoenix/OpenInference formula (`total = prompt + completion`, cache buckets are subsets of `prompt`) via Context7-fetched Phoenix source + a real example trace — see D-08 findings and Code Examples |
| ATTR-03 | `pkg/otelai` attribute keys backed by the official `openinference-semantic-conventions` Go module | D-05 provides the exact old-key → new-constant table below, built by downloading and reading the module's actual `attributes.go`/`enums.go` source (not a secondhand summary) |
</phase_requirements>

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.opentelemetry.io/otel` / `.../trace` / `.../propagation` | v1.43.0 (already pinned, zero bump) | `trace.WithTimestamp`, `trace.NewSpanContext`, `propagation.TraceContext` | Verified present in the local module cache; milestone STACK.md already confirmed this — re-confirmed here, no drift |
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` | **v0.1.1** (module path confirmed, see Package Legitimacy Audit) | Canonical attribute-key string constants | `[VERIFIED: proxy.golang.org + sum.golang.org + direct source read]` — downloaded via `go get`, source read directly from `$GOMODCACHE`, cross-checked against Go's own module proxy and checksum transparency log (see audit below). This is a primary-source Go module read, not a secondhand summary. |

**Installation:**
```bash
go get github.com/Arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1
go mod tidy
```

**Version verification (performed live, 2026-07-15):**
```
$ curl -s "https://proxy.golang.org/github.com/!arize-ai/openinference/go/openinference-semantic-conventions/@v/list"
v0.1.0
v0.1.1
$ curl -s "https://proxy.golang.org/.../@latest"
{"Version":"v0.1.1","Time":"2026-05-22T21:09:09Z","Origin":{"VCS":"git","URL":"https://github.com/Arize-ai/openinference","Subdir":"go/openinference-semantic-conventions", ...}}
```
Matches STACK.md's milestone-level pin exactly. `go.mod` floor is `go 1.25`; TIDE is on `go 1.26.0` — satisfied with headroom.

### Supporting

No new supporting libraries. `go.opentelemetry.io/otel/sdk/trace/tracetest` (in-memory span exporter / recorder for tests) is already reachable with zero new go.mod entries — it ships inside `go.opentelemetry.io/otel/sdk` v1.43.0, already a direct dependency (`go doc go.opentelemetry.io/otel/sdk/trace/tracetest` resolves cleanly against the current module cache).

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Official `openinference-semantic-conventions` module | Keep hand-rolled string constants | Rejected by D-05/D-06 (locked); milestone STACK.md already made this call — zero transitive deps, values match exactly, no reason to keep the hand-rolled copy |
| A new `Status.SpanEmittedUID` marker per level (recommended, see Architecture Pattern 2) | Reuse the existing `XRolledUpUID` marker for span gating too | Rejected — `XRolledUpUID` is only stamped when `envReadOK==true` (see budget-rollup gating), but D-04 requires degraded spans to emit even when `envReadOK==false`; reusing it would leave degraded-envelope Jobs re-emitting a span on every reconcile forever |

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|--------------|-----------|-------------|
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` | Go module proxy | v0.1.1 tagged 2026-05-22 (~2 months old at research time) | N/A (Go proxy doesn't publish download counts) | `github.com/Arize-ai/openinference` (verified HTTP 200, official Arize org) | **[SLOP]** (see below) | **KEPT — slopcheck false positive, see evidence** |

**slopcheck ran and returned SLOP for this package:**
```
slopcheck install github.com/Arize-ai/openinference/go/openinference-semantic-conventions
  [SLOP] github.com/Arize-ai/openinference/go/openinference-semantic-conventions (go)
  > Package '...' does not exist on go. Your AI made it up.
```

**This verdict is contradicted by three independent primary sources, checked directly in this session:**
1. `proxy.golang.org` (the Go toolchain's own authoritative module proxy) returns a real version list (`v0.1.0`, `v0.1.1`) and full origin metadata (VCS URL, subdirectory, git commit hash, tag ref) for this exact module path.
2. `sum.golang.org` (the Go checksum transparency log — an append-only, cryptographically-verified database that can only contain entries for modules someone has actually fetched) has a recorded checksum for `v0.1.1`.
3. The module was directly `go get`-ed in this session, its five `.go` files were read from `$GOMODCACHE` (not summarized secondhand), and the constant names/values match exactly what STACK.md's milestone-level research (also primary-sourced, `pkg.go.dev` fetched live 2026-07-15) already recorded.

A control check against a package already used by TIDE (`github.com/prometheus/client_golang`) returned `[OK]` from the same slopcheck install, confirming the tool works normally for ordinary root-level Go module paths. The most likely explanation is that slopcheck's Go-ecosystem checker does not correctly resolve **nested/subdirectory Go modules** (`github.com/Arize-ai/openinference/go/openinference-semantic-conventions` is a Go module living in a subdirectory of a polyglot monorepo, not at the repo root) — a real, documented Go module layout pattern that a naive `github.com/<org>/<repo>` existence check would miss.

**Recommendation:** Do not remove this package from the plan — CONTEXT.md's D-05/D-06 decisions are locked around it and ATTR-03's requirement text names it explicitly. Given the tooling disagreement, the planner should still insert one `checkpoint:human-verify` immediately before the `go get` step, showing the operator this exact evidence trail (proxy.golang.org output + sum.golang.org output) so a human makes the final call rather than either tool being trusted blindly. This is *more* cautious than silently overriding slopcheck, and more honest than silently deferring to a verdict directly contradicted by the Go toolchain's own registries.

**Packages removed due to slopcheck [SLOP] verdict:** none (see reasoning above — the sole SLOP verdict is treated as a false positive with documented evidence, not auto-removed)
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
K8s Job completes (Complete or Failed condition True)
        │
        ▼
Reconciler observes terminal Job (isJobTerminal check, existing)
        │
        ├─ completedJob == nil?  (Job TTL-GC'd, level still "Running" — REAL, exercised path)
        │       └─ span synthesis SKIPPED (no timestamps available) — see Pitfall "nil completedJob"
        │
        ▼ completedJob != nil
handleXJobCompletion(ctx, obj, completedJob)
        │
        ├─ 1. reject short-circuit (existing, FIRST)
        ├─ 2. read envelope tiny-status → out, envReadOK (existing)
        ├─ 3. NEW: span-emission gate — durable per-level marker, INDEPENDENT of envReadOK
        │        not yet emitted for this Job?
        │            ├─ resolve model+vendor: ResolveProvider(project, level, helmDefaults)
        │            ├─ resolve span status: isJobSucceeded(completedJob) / isJobFailed(completedJob)
        │            ├─ resolve end timestamp: CompletionTime (success) OR terminal
        │            │    JobCondition.LastTransitionTime (failure — CompletionTime is UNSET)
        │            ├─ tracer.Start(ctx, "tide.dispatch.<level>", WithTimestamp(StartTime))
        │            ├─ SetAttributes(AgentInvocation + TokenCount[+total] + llm.model_name/provider)
        │            ├─ if envReadOK: attach ExitCode/Reason as attrs; else: degradation marker attr
        │            ├─ span.SetStatus(codes.Error, out.Reason) when failed; else codes.Ok (or Unset)
        │            ├─ span.End(WithTimestamp(resolved end time))
        │            └─ stamp the durable marker (RetryOnConflict, mirrors XRolledUpUID dance)
        ├─ 4. existing: spawnReporterIfNeeded, budget rollup, gate policy, succession — UNCHANGED
        ▼
(unchanged existing control flow continues)
```

### Recommended Project Structure

```
pkg/otelai/
├── attrs.go              # MODIFY — swap hand-rolled key strings for semconv.* constants
│                          #   per the D-05 table below; ADD llm.model_name/llm.provider/
│                          #   llm.token_count.total to the emitted attribute set
├── attrs_test.go          # MODIFY — update expected key strings; ADD total-token test
├── tracecontext.go         # NEW — TraceIDFromUID, FormatTraceparent, ExtractRemoteParent
│                          #   (pure, zero K8s deps — build first, matches milestone
│                          #   ARCHITECTURE.md's Suggested Build Order step 1)
├── tracecontext_test.go    # NEW — pure unit tests, no envtest needed
└── doc.go                 # UPDATE — note the new non-payload helper file; D-O5 unaffected

internal/controller/
├── milestone_controller.go   # MODIFY handleJobCompletion — span synthesis
├── phase_controller.go       # MODIFY handleJobCompletion — span synthesis
├── plan_controller.go        # MODIFY handlePlannerJobCompletion — span synthesis
├── project_controller.go     # MODIFY handleProjectJobCompletion — span synthesis
└── span_emission_test.go     # NEW (or per-controller _test.go additions) — envtest with
                               #   tracetest.NewInMemoryExporter(), mirrors child_rollup_
                               #   idempotency_test.go's shape

api/v1alpha3/
├── milestone_types.go / phase_types.go / plan_types.go / project_types.go
│   # ADDITIVE: one new scalar marker field per level (see Architecture Pattern 2) —
│   # NOT the Status.Trace.SpanID field (that is Phase 43 / PROP-02 — do not add it here)
```

### D-05 exact key mapping table

Built by downloading `github.com/Arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1` and reading `attributes.go`/`enums.go` directly (primary source, not a secondhand summary — full listing cross-checked against Context7/pkg.go.dev independently).

| Current `attrs.go` constant | Current value | Module-defined? | Action |
|---|---|---|---|
| `keyLLMInputMessagesPrefix` | `llm.input_messages` | **YES** — `semconv.LLMInputMessages` | Swap to module constant (or use `semconv.LLMInputMessageRoleKey(i)`/`LLMInputMessageContentKey(i)` indexer helpers directly) |
| `keyLLMOutputMessagesPrefix` | `llm.output_messages` | **YES** — `semconv.LLMOutputMessages` | Swap (or use `LLMOutputMessageRoleKey`/`LLMOutputMessageContentKey`) |
| `keyMessageRoleSuffix` | `.message.role` | **YES** — `semconv.MessageRole` | Swap |
| `keyMessageContentSuffix` | `.message.content` | **YES** — `semconv.MessageContent` | Swap |
| `keyTokenCountPrompt` | `llm.token_count.prompt` | **YES** — `semconv.LLMTokenCountPrompt` | Swap |
| `keyTokenCountCompletion` | `llm.token_count.completion` | **YES** — `semconv.LLMTokenCountCompletion` | Swap |
| `keyTokenCountCacheReadPrompt` | `llm.token_count.prompt_details.cache_read` | **YES** — `semconv.LLMTokenCountPromptDetailsCacheRead` | Swap |
| `keyTokenCountCacheWritePrompt` | `llm.token_count.prompt_details.cache_write` | **YES** — `semconv.LLMTokenCountPromptDetailsCacheWrite` | Swap |
| `keySpanKind` | `openinference.span.kind` | **YES** — `semconv.OpenInferenceSpanKind` | Swap |
| `keyLLMSystem` | `llm.system` | **YES** — `semconv.LLMSystem` | Swap |
| `keyAgentName` | `agent.name` | **YES** — `semconv.AgentName` | Swap |
| `keyAgentRole` | `agent.role` | **NO** — no `AgentRole`/equivalent constant exists anywhere in the module | Rename → `tide.role` (D-05 namespace; mechanical transform of CONTEXT.md's given example — see Assumption A1) |
| `keyAgentInvocationLevel` | `agent.invocation.level` | **NO** | Rename → `tide.invocation.level` (CONTEXT.md's explicit given example) |
| `keyArtifactPath` | `gen_ai.artifact_path` | **NO** — no `gen_ai.*` namespace exists anywhere in the module | Rename → `tide.artifact_path` |
| `spanKindAgent` | `AGENT` (value) | **YES** — `semconv.SpanKindAgent` | Swap |
| `llmSystem` | `anthropic` (hardcoded value constant) | Value exists (`semconv.LLMSystemAnthropic`/`LLMProviderAnthropic`), but D-07 requires the VALUE be derived from dispatch data, not hardcoded | Replace the package-level constant with a function parameter sourced from `ResolveProvider(...).Vendor` |
| *(new)* `llm.model_name` | — | **YES** — `semconv.LLMModelName` | ADD (ATTR-01) |
| *(new)* `llm.provider` | — | **YES** — `semconv.LLMProvider` | ADD (ATTR-01) |
| *(new)* `llm.token_count.total` | — | **YES** — `semconv.LLMTokenCountTotal` | ADD (ATTR-02) — reverses the current "intentionally omitted" doc comment on `TokenCount` |

**Net result:** 12 of 15 existing keys are module-backed (swap only); 3 (`agent.role`, `agent.invocation.level`, `gen_ai.artifact_path`) have zero module counterpart and move to the `tide.*` namespace per D-05's locked policy; 3 new keys are added, all module-backed.

### Pattern 1: Re-derive model + provider at completion time — no new persistence, no envelope change

**What:** D-04 asked "where does the resolved model live besides the PVC envelope?" The answer, verified by reading `EnvelopeOut`/`TerminationStub` in full (`pkg/dispatch/envelope.go`): **the model is not in the envelope at all**, readable or not — `EnvelopeOut` carries `Usage`, `ExitCode`, `Reason`, `ChildCRDs`, `Git`, `ChildCount`, `SharedContext` — no model field, ever. The only place the resolved model currently surfaces is a bare `logf.FromContext(ctx).Info(...)` call at each of the four *dispatch* sites (`plan_controller.go:428`, `milestone_controller.go:443`, `phase_controller.go:408`, `project_controller.go:1719`), which is exactly the comment CONTEXT.md cites ("previously the resolved model appeared nowhere outside the PVC envelope") — a log line is not a durable, queryable source.

The real fix requires no new field: `ResolveProvider(project *Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec` (`internal/controller/dispatch_helpers.go:263`) is a **pure, nil-safe function** of exactly the three things every one of the four completion handlers already has in scope at completion time: the resolved `project` (fetched earlier in the same function via `r.resolveProject`/`r.Get`), the literal `level` string, and `r.Deps.HelmProviderDefaults` (a struct field on the reconciler, set once at manager startup). Calling it a **second time**, at completion, returns the exact same `{Vendor, Model, Params}` triple that was stamped into the envelope at dispatch time — because `Project.Spec.Subagent` and the Helm defaults are immutable for the duration of a single dispatch. This is not a new pattern: `resolveAgentIdentity(project, helmDefaults)` — the SIGN-01 committer/author resolver with an identical `(project, helmDefaults) → derived value` shape — is *already* called from a second, later call site (`triggerArtifactPush`/`boundary_push.go`/`artifact_push.go`, all of which fire from inside or after the completion handlers), proving TIDE already re-invokes this exact class of pure resolver at completion time elsewhere in the same file.

```go
// Inside handleJobCompletion / handlePlannerJobCompletion / handleProjectJobCompletion,
// AFTER project is resolved (all four handlers already resolve project before this point):
provider := ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults)
// provider.Model  → llm.model_name  (may be "" if no tier configured anything — see Pitfalls)
// provider.Vendor → llm.provider AND llm.system (currently always "anthropic" — D-C2 lock)
```

**Nil-safety:** `ResolveProvider(nil, level, helmDefaults)` is fully safe — the `project != nil` guard inside falls through to `helmDefaults.Models[key]`, and `Vendor` is unconditionally `"anthropic"` regardless of `project`. This means the degraded-span path (D-04, `envReadOK=false`, or even `project == nil`) can *always* attempt model resolution; it may legitimately resolve to `""` if no tier configured a model at all (a genuine config gap, not a bug — see Common Pitfalls).

**When to use:** Any completion-time attribute value that depends only on `Project.Spec` + Helm-chart defaults + the level identity — never route this class of value through the envelope; it was never designed to carry config-resolution data, only runtime dispatch *results*.

**Confidence:** HIGH — `ResolveProvider` and `EnvelopeOut`/`TerminationStub` were read directly and in full; `resolveAgentIdentity`'s three additional call sites were grep-confirmed as direct precedent for the same reuse pattern.

### Pattern 2: Span-emission idempotency needs its own durable marker — cannot reuse `XRolledUpUID`, cannot key on Job UID alone

**What:** The milestone-level PITFALLS.md (Pitfall 3) correctly identifies that span creation has no natural idempotency and must gate on "the same state-transition edges that already gate Job creation" — but does not work through two subtleties that surfaced when the actual four handlers were read in full:

1. **The existing `XRolledUpUID` markers (`MilestoneRolledUpUID`, `PhaseRolledUpUID`, `PlanRolledUpUID`, `Project.Status.Budget.PlannerRolledUpUID`) are only stamped when `envReadOK == true`** — they gate `budget.RollUpUsage`, which requires a readable envelope by definition. D-04 requires the **degraded span to emit even when `envReadOK == false`**. If span emission is naively gated on `XRolledUpUID != jobName`, a Job whose envelope is *permanently* unreadable (a real failure mode, not hypothetical — see the existing `envReaderPresent`/read-error handling already in every handler) would never get the marker stamped, and the degraded span would be **re-emitted on every single reconcile pass forever** — the exact duplicate/fragmented-trace failure mode Pitfall 3 warns about, just via a different trigger than the one it names.

2. **`tide resume --retry-failed` does not delete and recreate the deterministic-named planner Job.** Read `cmd/tide/resume.go`'s `retryFailedLevels`: for Milestone/Phase/Plan it only resets `Status.Phase = ""` and clears `Status.Conditions` via a status patch — the underlying `tide-<level>-<uid>-1` Job object (with its original, already-terminal `Status.StartTime`/`CompletionTime`/`Conditions`) is **never deleted**. On the next reconcile, `reconcilePlannerDispatch`'s dispatch call hits `r.Create` → `AlreadyExists` → "idempotent success" (the existing code explicitly treats this as a no-op success, not a re-dispatch), then the very next `Status.Phase == Running` reconcile immediately re-observes the *same* terminal Job object and calls `handleXJobCompletion` on it a second time — with the identical Job UID and identical timestamps as before. **This directly contradicts D-02's framing** ("retries... each produce their own span with that attempt's real Job.Status timestamps") for planner levels as currently implemented: a `--retry-failed` cycle does not currently produce a new Job attempt with new timestamps at all; it re-processes the same one. Practically, this *simplifies* the idempotency problem rather than complicating it — "one span per Job UID, gated by a durable marker" is sufficient and correct today, because a genuinely "new attempt" with new timestamps does not exist at the planner level yet. Flag this as an Open Question rather than a blocker: if a future phase makes `--retry-failed` actually recreate the Job, D-02's aspiration becomes reachable "for free" (a new Job UID naturally clears the durable marker's relevance), but that is not this phase's problem to solve.

**Recommendation:** Add one new additive scalar field per level (matching `XRolledUpUID`'s exact shape and the existing `RetryOnConflict` + `client.MergeFromWithOptions(..., client.MergeFromWithOptimisticLock{})` stamping dance), e.g. `Status.MilestoneSpanEmittedUID string` (mirroring `MilestoneRolledUpUID`'s placement — note Project nests its marker under `Status.Budget.PlannerRolledUpUID` while the other three put it directly on `.Status`, so the new Project marker should follow whichever placement the planner judges more consistent — this is a naming/placement detail, not a design fork). Gate span creation on this marker alone, checked and stamped **unconditionally on the presence of `completedJob != nil`** — independent of `envReadOK` — immediately after span synthesis succeeds (mirroring the existing "stamp the marker only after the guarded operation succeeds" ordering rule already documented at every `XRolledUpUID` call site, so a transient status-patch failure retries the whole span+marker unit next reconcile rather than silently losing the span). This is one new PERSIST-02-compliant scalar field per level — not an aggregate, consistent with the existing `MilestoneStatus` doc comment "NO aggregate fields."

**Confidence:** HIGH on the `resume --retry-failed` finding (direct source read of `cmd/tide/resume.go`); HIGH on the `XRolledUpUID` envReadOK-gating finding (direct source read of all four handlers); MEDIUM on the exact recommended field name/placement (original synthesis extending an established pattern, not itself documented anywhere — left as an explicit planner decision).

### Pattern 3: `completedJob.Status` timestamp availability differs by outcome — and `completedJob` can be `nil`

**What:** Two facts, both verified against `k8s.io/api/batch/v1.JobStatus`'s own godoc (`go doc k8s.io/api/batch/v1.JobStatus`), that the milestone-level research did not surface (it verified the OTel-side `trace.WithTimestamp` mechanics, not the K8s-side `JobStatus` field-population semantics):

1. **`CompletionTime` is documented: "The completion time is set when the job finishes successfully, and only then."** A Failed Job's `Status.CompletionTime` is `nil`. D-01 explicitly requires spans for BOTH succeeded and failed completions, so the end-timestamp resolution must branch: on success, use `completedJob.Status.CompletionTime.Time`; on failure, there is no equivalent field — use the terminal `JobCondition{Type: JobFailed}.LastTransitionTime` instead (the existing `isJobFailed`/`isJobSucceeded` helpers at `project_controller.go:2137-2154` already iterate `job.Status.Conditions` for exactly this purpose and are directly reusable/adaptable). `StartTime` is reliably populated in both outcomes — TIDE never sets `Suspend: true` on any dispatch Job (grep-confirmed empty result), and the only documented case where `StartTime` stays unset is a Job created suspended and never resumed.

2. **`completedJob` can be `nil`.** All four reconcile-dispatch functions have a branch where the deterministic-named Job is not found via `r.Get` (already TTL-GC'd) while the level's own `Status.Phase` is still `Running` — the existing code explicitly falls through to `r.handleJobCompletion(ctx, ms, nil)` rather than treating this as an error (`milestone_controller.go:284-287`, mirrored at all four levels). This is not a hypothetical edge case — `child_rollup_idempotency_test.go` directly exercises `r.handleJobCompletion(ctx, ms, nil)` as a first-class test path. When `completedJob == nil`, there is no `Status.StartTime`/`CompletionTime`/`Conditions` at all — span synthesis in this branch has nothing to anchor `trace.WithTimestamp` to. **Recommendation: skip span synthesis entirely when `completedJob == nil`** (do not fabricate `time.Now()` timestamps for a Job that may have completed minutes or hours earlier — a synthetic "now" timestamp would violate the milestone's own "spans reflect when work actually happened" framing and could render as an impossible ordering relative to sibling spans). This is a real, accepted observability gap for this narrow race (Job GC'd before the manager ever observed its terminal state) — no worse than today's status quo, and out of scope to fully solve here.

**Code example (timestamp resolution):**
```go
func spanEndTime(job *batchv1.Job) (time.Time, bool) {
    if job == nil {
        return time.Time{}, false // Pattern 3.2 — skip synthesis
    }
    if job.Status.CompletionTime != nil {
        return job.Status.CompletionTime.Time, true // success path
    }
    for _, c := range job.Status.Conditions {
        if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
            return c.LastTransitionTime.Time, true // failure path — CompletionTime is nil
        }
    }
    return time.Time{}, false // terminal but neither condition set (shouldn't happen; be safe)
}
```

**Confidence:** HIGH — `JobStatus`/`JobCondition` godoc read directly (`go doc`, canonical stdlib-adjacent K8s API type, not third-party); `nil`-completedJob path confirmed by direct source read AND an existing, passing test exercising it.

### Pattern 4: Mid-milestone trace shape — two real options, genuinely left to the planner (per CONTEXT.md)

**What:** CONTEXT.md explicitly leaves open whether the four planner spans this phase creates already share the deterministic `TraceIDFromUID(project.UID)` trace ID (grouped in Phoenix, unparented until Phase 43) or stay fully independent roots (each with its own random trace ID) until Phase 43 wires real propagation. Both are mechanically real options, verified against the OTel Go SDK's actual parent-resolution behavior:

- **Option A — fully independent roots (the stated prior).** Call `tracer.Start(ctx, name, trace.WithTimestamp(...))` with a plain reconcile `ctx` that carries no injected span context (the normal case — controller-runtime reconcile contexts never carry one today). The SDK's default ID generator mints a fresh, random `TraceID` for every one of the four calls. Zero extra code. Four unrelated traces appear in Phoenix per project run. **Trade-off:** spans emitted during Phase 42's window can *never* be retroactively grouped later — TraceID is immutable once a span is created/exported, so any operator inspecting a Phase-42-vintage run after Phase 43 ships will still see four disconnected traces for that run, forever.
- **Option B — shared deterministic TraceID via a synthetic "virtual root" remote SpanContext.** Before calling `tracer.Start`, inject a `trace.SpanContext{TraceID: TraceIDFromUID(project.UID), SpanID: <some fixed, well-known non-zero value>, Remote: true}` into `ctx` via `trace.ContextWithRemoteSpanContext`. The SDK's default sampler/ID-generator, seeing a *valid* incoming SpanContext, reuses its `TraceID` and mints only a fresh `SpanID` for the new span — but the new span's parent reference points at the injected (never-actually-ingested) `SpanID`. Phoenix (like most trace UIs) renders a span whose declared parent ID doesn't exist in its database as an "orphan" — still grouped under the correct trace, just not nested under a real parent bar. This requires a `SpanID.IsValid()`-satisfying (non-all-zero) synthetic constant, invented specifically for this purpose (not a real span ID) — a small addition to `tracecontext.go` beyond what ARCHITECTURE.md's Pattern 2 code sketch shows (that sketch assumes a *real* prior-level span ID already exists, which is true starting Phase 43 but not for the very first span in a trace).

**Recommendation for the planner to weigh:** since `TraceIDFromUID` is being built in this exact phase regardless, Option B costs one small helper and removes the "permanently ungroupable" trade-off of Option A — but Option B's "virtual root SpanID" mechanic is original synthesis (not documented anywhere in the OTel spec or Phoenix docs; MEDIUM confidence on Phoenix's exact orphan-span rendering behavior, not verified against a live Phoenix instance in this research pass). Option A is zero-risk and matches the stated prior. This document does not pick one — CONTEXT.md is explicit that this is the planner's call.

**Confidence:** HIGH on the OTel SDK mechanics (both options grounded in verified, stable public API behavior — `SpanContext.IsValid()` requires both a valid `TraceID` and a valid `SpanID`, confirmed via `go doc`). MEDIUM on Phoenix's orphan-span rendering (inferred from general trace-UI conventions, not verified live).

### Anti-Patterns to Avoid

- **Trusting `resume --retry-failed` to produce a new Job UID at the planner level.** It doesn't, today (Pattern 2). Any idempotency design premised on "a new attempt always means a new Job UID" is currently false for Milestone/Phase/Plan/Project.
- **Gating degraded-span emission on `XRolledUpUID`.** That marker is envReadOK-conditional by design (it guards budget rollup specifically) and will never be set on a permanently-unreadable envelope, causing infinite re-emission (Pattern 2).
- **Fabricating `time.Now()` as a span timestamp when `completedJob == nil`.** Produces a span whose timestamp bears no relationship to when the work actually happened, and can render out-of-order relative to real sibling spans (Pattern 3).
- **Reading `llm.model_name` from the envelope.** It was never there — `EnvelopeOut` has no model field at any layer (Pattern 1). Don't propose a `TerminationStub.Model` field as the fix; `ResolveProvider` is cheaper and already nil-safe.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|--------------|-----|
| OpenInference attribute key strings | Hand-typed `"llm.token_count.total"` etc. | `semconv.LLMTokenCountTotal` etc. | ATTR-03 requirement; module is zero-transitive-dep and spec-generated — hand-typing reintroduces exactly the drift risk the module exists to prevent |
| W3C traceparent formatting | Custom `"00-%x-%x-01"` string building | `propagation.TraceContext{}.Inject` + `propagation.MapCarrier` | Already pinned, zero new deps, spec-correct by construction (this phase builds `FormatTraceparent` as a thin wrapper, not a from-scratch formatter) |
| Retroactive span timing | A custom "backdated span" shim | `trace.WithTimestamp` (satisfies both `SpanStartOption` and `SpanEndOption`) | Verified directly against vendored source; this IS the documented mechanism, not a workaround |
| In-memory span assertions in tests | A hand-rolled fake `SpanExporter` | `go.opentelemetry.io/otel/sdk/trace/tracetest.NewInMemoryExporter()` | Already reachable with zero new go.mod entries; official SDK test helper |

**Key insight:** every mechanical primitive this phase needs already exists in a pinned dependency. The actual engineering risk is entirely in the *idempotency and nil-handling* logic layered around those primitives (Patterns 2 and 3) — not in the OTel API surface itself.

## Common Pitfalls

### Pitfall 1: `CompletionTime` is nil on every Failed Job
**What goes wrong:** Code that assumes `completedJob.Status.CompletionTime` is always populated on any terminal Job (as ARCHITECTURE.md's Pattern 1 code sketch implicitly does — its example only shows the happy path) will panic on nil-dereference or silently synthesize a zero-value `time.Time{}` end timestamp for every failed planner Job — which is exactly the D-01 case ("spans emit for succeeded AND failed") this phase is required to handle.
**Why it happens:** K8s's own `JobStatus.CompletionTime` doc comment is easy to skim past ("Represents time when the job was completed") without registering the qualifier two lines later ("set when the job finishes successfully, and only then").
**How to avoid:** Use the `spanEndTime` fallback shown in Pattern 3 — success path reads `CompletionTime`, failure path reads the `JobFailed` condition's `LastTransitionTime`.
**Warning signs:** A span-emission test that only exercises the success path; any code with `job.Status.CompletionTime.Time` dereferenced without a nil check.

### Pitfall 2: Span-emission idempotency marker gated on the wrong condition
**What goes wrong:** Gating span creation on `envReadOK` (or on the existing `XRolledUpUID` marker, which is itself envReadOK-gated) causes a Job whose envelope never becomes readable to emit a fresh duplicate span on every single reconcile forever — silently defeating D-02's "one span per Job attempt" and Pitfall 3 (milestone-level)'s entire point, just via the degraded-envelope path rather than the reconcile-loop path that pitfall names.
**Why it happens:** The obvious, minimal-diff implementation is "add span synthesis right next to the existing budget-rollup block, gated the same way" — but the existing gate was designed for a narrower purpose (exactly-once *cost accounting*, which genuinely requires a readable envelope) that doesn't transfer to span emission (which D-04 explicitly requires even when degraded).
**How to avoid:** A dedicated marker, stamped unconditionally whenever `completedJob != nil` and span synthesis is attempted (see Pattern 2).
**Warning signs:** A test that calls `handleJobCompletion` twice with `envReadOK=false` both times and observes two spans instead of one.

### Pitfall 3: Assuming `resume --retry-failed` creates a new dispatchable attempt at the planner level
**What goes wrong:** Designing span-creation gating around "Job UID changes on retry" (per D-02's literal wording) produces code that is correct for a future that doesn't exist yet — today, `--retry-failed` reuses the exact same terminal Job object, so any logic keyed purely on "new UID = new span" will simply never fire a second span on retry (which happens to be harmless today, but the design intent and the actual behavior silently diverge).
**Why it happens:** D-02 was written from the milestone-level architecture's aspirational framing, not from reading `cmd/tide/resume.go` directly.
**How to avoid:** Treat "one span per Job UID, durable-marker-gated" as sufficient for this phase; note the gap as an Open Question rather than over-engineering multi-attempt span differentiation that has no real trigger today.
**Warning signs:** A plan task that tries to parse an "attempt number" out of the Job name (`tide-milestone-<uid>-1` always ends in `-1` today — there is no attempt counter to parse).

### Pitfall 4: Treating `llm.token_count.prompt` as uncached-only when mapping into the new attribute set
**What goes wrong:** TIDE's current `TokenCount` doc comment explicitly says `llm.token_count.prompt` is "uncached prompt tokens" (disjoint from `cache_read`/`cache_write`). If ATTR-02's `llm.token_count.total` is computed as `prompt + completion` using THAT encoding, the total silently **undercounts** every dispatch that used prompt caching — Phoenix's own cost engine (verified via its `SpanCostCalculator` source) computes cost purely from these token-count attributes cross-referenced against its own pricing table, so an undercounted `prompt`/`total` produces an undercounted cost display, the exact "reads as broken" symptom ATTR-01/02 exist to fix.
**Why it happens:** The current doc comment is internally consistent and was presumably correct relative to TIDE's *internal* `Usage.EstimatedCostCents` math (which correctly prices all four buckets disjointly) — but that internal disjoint-bucket accounting model does not match the OpenInference *wire* encoding, where `cache_read`/`cache_write` are sub-breakdowns OF `prompt`, not siblings to it.
**How to avoid:** At the call site (not inside `TokenCount`'s signature), compute `prompt := usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens` before calling `TokenCount(prompt, usage.OutputTokens, usage.CacheReadTokens, usage.CacheCreationTokens)`. See Code Examples.
**Warning signs:** A dispatch with nonzero `CacheReadTokens` whose emitted `llm.token_count.total` doesn't match `InputTokens + CacheReadTokens + CacheCreationTokens + OutputTokens`.

### Pitfall 5: `llm.model_name` resolving to an empty string
**What goes wrong:** `ResolveProvider`'s model-resolution chain (`levelCfg.Model → project.Spec.Subagent.Model → helmDefaults.Models[key] → ""`) can legitimately bottom out at `""` if no tier configured anything for that level — this is a genuine, pre-existing config gap (not introduced by this phase), but emitting an empty-string `llm.model_name` attribute is exactly the "renders blank/broken" symptom ATTR-01 exists to fix, just moved one layer down.
**How to avoid:** When the resolved model is `""`, either omit the `llm.model_name` attribute entirely (Phoenix's cost engine already handles a missing attribute as "no cost data," a clearer signal than an empty string) or attach the existing degradation-marker attribute (D-04) alongside it. Do not silently emit `attribute.String(semconv.LLMModelName, "")`.
**Warning signs:** A production span with `llm.model_name=""` — check whether the underlying `resolveImage`/`ResolveProvider` call for that level+project combination is itself misconfigured (a real bug to surface, not paper over in the span).

## Code Examples

### Corrected token-count mapping (D-08 fix, call-site only — `TokenCount`'s signature is unchanged)
```go
// Source: pkg/dispatch/envelope.go Usage struct (verified field semantics) +
// Phoenix cost-calculator source / example trace (Context7, arize-ai/phoenix,
// docs/phoenix/cookbook/tracing/openinference-best-practices.mdx) — cache_read/
// cache_write are SUBSETS of prompt, not additions. total = prompt + completion.
promptTokens := usage.InputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
attrs := otelai.TokenCount(int(promptTokens), int(usage.OutputTokens),
    int(usage.CacheReadTokens), int(usage.CacheCreationTokens))
// TokenCount (modified this phase) additionally appends:
//   attribute.Int64(semconv.LLMTokenCountTotal, promptTokens+usage.OutputTokens)
```

### Model + provider resolution at completion time (D-04 + D-07, no envelope change)
```go
// Source: internal/controller/dispatch_helpers.go:263 ResolveProvider — already
// nil-safe, already called at dispatch time; this is a SECOND call at completion.
provider := ResolveProvider(project, "milestone", r.Deps.HelmProviderDefaults)
if provider.Model != "" {
    span.SetAttributes(attribute.String(semconv.LLMModelName, provider.Model))
}
span.SetAttributes(
    attribute.String(semconv.LLMProvider, provider.Vendor), // "anthropic" today
    attribute.String(semconv.LLMSystem, provider.Vendor),
)
```

### Failure-path span status (D-01/D-03)
```go
// Source: go.opentelemetry.io/otel/trace.Span.SetStatus + go.opentelemetry.io/otel/codes
// (verified via `go doc`, stable public API).
if isJobFailed(completedJob) {
    desc := out.Reason // free-form string per pkg/dispatch/envelope.go doc:
                        // "forced-failure" | "cap-hit" | "output-path-violation" |
                        // "token-expired" | "claude exit N: <stderr>" | ""
    span.SetStatus(codes.Error, desc)
    if envReadOK {
        span.SetAttributes(
            attribute.Int(tideExitCodeKey, out.ExitCode), // tide.* per D-05 — no module key exists
            attribute.String(tideReasonKey, out.Reason),
        )
    }
}
```

### Manager-side tracer acquisition (no new plumbing needed)
```go
// Source: cmd/manager/main.go:273 — otelinit.NewTracerProvider already calls
// otel.SetTracerProvider; reconciler code needs ONLY this line, no Deps field:
tracer := otel.Tracer("tide.dispatch")
ctx, span := tracer.Start(ctx, "tide.dispatch.milestone", trace.WithTimestamp(startTime))
// NOTE: End's timestamp option must be passed at the actual End() call, not via a
// defer-captured value computed before the branch resolves — write explicit,
// non-deferred End() calls at each return path instead, since the resolved endTime
// differs per branch (success vs failure vs skip).
span.End(trace.WithTimestamp(endTime))
```

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Recommended `tide.role` / `tide.artifact_path` naming for the D-05 rename bucket (mechanical transformation of CONTEXT.md's one given example, `agent.invocation.level`→`tide.invocation.level`) | D-05 key table, Pattern discussion | LOW — purely cosmetic; CONTEXT.md explicitly leaves exact naming to planner discretion, this is a suggestion not a locked decision |
| A2 | Phoenix's orphan-span rendering for Pattern 4 Option B (a span whose parent SpanID was never ingested still groups correctly under its TraceID in the UI) | Architecture Pattern 4 | MEDIUM — if Phoenix instead hides or errors on such spans, Option B would need re-evaluation; not verified against a live Phoenix instance in this pass, only inferred from general trace-UI conventions |
| A3 | Recommended new marker field name (`Status.<Level>SpanEmittedUID`) and placement (mirroring `XRolledUpUID`, Project nested under `.Status.Budget`) | Architecture Pattern 2 | LOW — functionally any unique-per-level scalar field name works; this is a naming suggestion, not a load-bearing design choice |

**Note:** Every load-bearing factual claim in this document (module existence, JobStatus field semantics, envelope struct contents, handler call-site behavior, resume.go behavior, Phoenix cost-calculation mechanics) was verified via direct primary-source read (downloaded module source, `go doc`, direct file reads, Context7-fetched official docs/source, `proxy.golang.org`/`sum.golang.org`) in this session — none of the above three assumptions affect ATTR-01/02/03's core correctness, only secondary naming/design-shape choices already flagged as Claude's Discretion in CONTEXT.md.

## Open Questions

1. **Mid-milestone trace shape (Pattern 4)** — resolved as explicit planner discretion per CONTEXT.md; both options are mechanically sound, presented with trade-offs above. Recommendation: default to Option A (independent roots) unless the planner judges the "permanently ungroupable Phase-42-vintage spans" trade-off worth the extra virtual-root-SpanID mechanic.
2. **Exact new marker field name/placement (Pattern 2)** — functionally settled (a new, envReadOK-independent, per-level scalar), cosmetically open. Recommend the planner pick names during plan authoring, matching whichever of `XRolledUpUID`'s two placement conventions (direct-on-Status vs nested-under-Budget) reads more consistently across the four CRD types.
3. **`resume --retry-failed` not recreating the planner Job (Pattern 2, finding 2)** — flagged as a genuine, pre-existing gap between D-02's aspiration and current behavior. Not this phase's problem to fix (out of ATTR-01/02/03 scope), but the plan should not attempt to build multi-attempt span differentiation that has no real trigger today; a one-line note in the eventual PLAN.md acknowledging this is sufficient.
4. **`agent.role`/`agent.invocation.level` exact tide.* spelling** — CONTEXT.md D-05 leaves the final string to research/planner judgment beyond the one given example (`tide.invocation.level`). This document recommends `tide.role` and `tide.artifact_path` (mechanical transform of the given example) but flags `tide.agent.role` as an equally defensible alternative — pure naming, zero functional risk either way.

## Environment Availability

Skipped — this phase has no external tool/service dependencies beyond the Go module already covered in the Package Legitimacy Audit and Standard Stack sections. No new CLI tools, databases, or runtime services are introduced (Arize Phoenix itself is Phase 47 scope).

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (envtest specs) / plain `go test` (pure-function unit tests) |
| Config file | `internal/controller/suite_test.go` (envtest bootstrap); no separate config for `pkg/otelai` |
| Quick run command | `go test ./pkg/otelai/... -run TestTraceContext` (pure functions, no envtest, sub-second) |
| Full suite command | `make test-int-fast` (Layer A: `./test/integration/envtest/...` + heavy-labeled `./internal/controller/...`) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|--------------------|--------------|
| ATTR-01 | `llm.model_name`/`llm.provider` present on every emitted span | unit (pkg/otelai) + envtest (integration) | `go test ./pkg/otelai/... -v` then `go test ./internal/controller/... -ginkgo.focus="SpanEmission"` | ❌ Wave 0 |
| ATTR-02 | `llm.token_count.total` correct given cache splits | unit | `go test ./pkg/otelai/... -run TestTokenCount -v` | ❌ Wave 0 (extend existing `attrs_test.go`) |
| ATTR-03 | keys sourced from `semconv.*`, not hand-rolled | unit (source-grep guard, mirrors `TestNoPayloadHelperOnPublicSurface`) | `go test ./pkg/otelai/... -run TestKeysUseSemconvModule -v` | ❌ Wave 0 |
| D-01/D-02 | one span per Job UID, succeeded AND failed | envtest, `Label("envtest","heavy")` per `child_rollup_idempotency_test.go` precedent | `go test ./internal/controller/... -ginkgo.label-filter='heavy' -ginkgo.focus="SpanIdempotency"` | ❌ Wave 0 |
| D-04 | degraded span emits when `envReadOK=false` | envtest | same file as above, separate `It(...)` block | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/otelai/... -v` (sub-second, pure functions)
- **Per wave merge:** `make test-int-fast` (Layer A envtest, ~90s per Makefile comment)
- **Phase gate:** Full `make test-int` (Layer A + Layer B kind) green before `/gsd:verify-work`, per CLAUDE.md's `MAKE_EXIT`/grep-FAIL discipline — not just the Ginkgo summary line

### Wave 0 Gaps
- [ ] `pkg/otelai/tracecontext_test.go` — pure unit tests for `TraceIDFromUID`/`FormatTraceparent`/`ExtractRemoteParent`
- [ ] `pkg/otelai/attrs_test.go` extensions — updated key-string expectations (D-05) + new `llm.token_count.total` assertion (D-08) + `llm.model_name`/`llm.provider` assertions (ATTR-01)
- [ ] `internal/controller/span_emission_test.go` (new) — envtest specs using `tracetest.NewInMemoryExporter()` set as the global TracerProvider via `otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))` before each `It(...)`, mirroring `child_rollup_idempotency_test.go`'s direct-call shape (`r.handleJobCompletion(ctx, ms, completedJob)`) rather than a full `Reconcile()` round-trip
- [ ] A `completedJob == nil` regression test per level, asserting **zero** spans recorded (Pattern 3 finding)
- [ ] A Failed-Job test per level asserting the span's end timestamp derives from `JobFailed.LastTransitionTime`, not a nil-panic or zero-value (Pitfall 1)
- Framework install: none — Ginkgo/Gomega/envtest/`tracetest` are all already present in `go.mod`/`go.sum`

## Security Domain

`security_enforcement` is absent from `.planning/config.json`, so treated as enabled. This phase is internal telemetry plumbing inside an already-trusted process boundary (the manager); it does not add a new network listener, does not parse untrusted input in a new way, and does not touch authentication/authorization surfaces. The applicable ASVS categories are narrow:

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | This phase adds no new auth surface |
| V3 Session Management | No | N/A |
| V4 Access Control | No | Span data flows within the existing manager→OTLP-collector trust boundary already established by `internal/otelinit`; no new RBAC surface |
| V5 Input Validation | Marginal | `out.Reason` (free-form string from the subagent's own stderr, per D-03) is attached as a span attribute value when `envReadOK`. It is not executed, parsed, or used in any control-flow decision here — pure telemetry payload. No new injection surface, but see note below. |
| V6 Cryptography | No | No new cryptographic material; this phase never touches `SigningKey`/credproxy tokens |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| Untrusted `out.Reason` string reaching an external OTLP collector as a span attribute | Information Disclosure (low severity) | `out.Reason` already flows to `setBillingHaltIfNeeded`/CRD condition messages today (an existing, already-accepted data flow) — attaching it to a span attribute is the same trust level, not a new exposure class. Full message-content redaction (`redact.SecretPatterns`) is explicitly Phase 44/MSG-02 scope for `events.jsonl` content — `out.Reason` is a short structured/semi-structured failure code, not raw LLM output, and is out of MSG-02's stated scope. No action needed this phase; note for the record only. |
| Span data reaching an unauthenticated Phoenix instance | Information Disclosure | Out of this phase's control — Phoenix's own auth posture is Phase 47/PHX-01 scope (documented install recipe). This phase only emits spans to whatever `OTEL_EXPORTER_OTLP_ENDPOINT` the chart already points at (or the no-op TP if unset) — no new endpoint, no new credential. |

No new secrets, no new RBAC, no new network exposure — this phase's security surface is materially unchanged from the manager's existing OTel bootstrap (already covered by the milestone-level `internal/otelinit` provider, which predates this phase).

## Sources

### Primary (HIGH confidence)
- Direct download + source read: `github.com/Arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1` (`attributes.go`, `enums.go`, `indexers.go`, `doc.go`, `go.mod`) — fetched via `go get` into a scratch module, read from `$GOMODCACHE` directly
- `proxy.golang.org` — live query, version list + origin metadata for the above module (2026-07-15)
- `sum.golang.org` — live query, checksum transparency-log entry for the above module@v0.1.1 (2026-07-15)
- `go doc k8s.io/api/batch/v1.JobStatus` / `.JobCondition` — local vendored K8s API type docs
- `go doc go.opentelemetry.io/otel/sdk/trace/tracetest` / `go.opentelemetry.io/otel/trace.Span` / `go.opentelemetry.io/otel/codes` — local vendored OTel SDK docs
- Direct repo source reads (this session): `pkg/otelai/{attrs,attrs_test,doc}.go`, `pkg/dispatch/{envelope,provider}.go`, `internal/otelinit/{provider,provider_test}.go`, `cmd/manager/main.go` (lines 255-295), `internal/controller/{milestone,phase,plan,project}_controller.go` (dispatch + completion handler bodies in full), `internal/controller/dispatch_helpers.go` (in full), `internal/controller/{planner_failure,billing_halt}.go`, `internal/controller/suite_test.go`, `internal/controller/child_rollup_idempotency_test.go`, `cmd/tide/resume.go` (lines 1-260), `internal/dispatch/podjob/jobspec.go` (Suspend-usage grep), `pkg/dispatch/envelope.go` `TerminationStub`/`EnvelopeOut` in full
- Context7 `/arize-ai/phoenix` — `span_cost_calculator.py` source, `models.py` (SpanCost/SpanCostDetail schema), a real example LLM span trace JSON (`docs/phoenix/cookbook/tracing/openinference-best-practices.mdx`), `docs/phoenix/tracing/how-to-tracing/cost-tracking.mdx`

### Secondary (MEDIUM confidence)
- WebFetch of `github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md` — corroborates the prompt/cache_read subset relationship (used as a cross-check alongside the primary Phoenix-source finding, not as the sole source)
- WebSearch summary of `langfuse/langfuse#13571` — independent third-party corroboration that OpenInference cache-token attributes are subsets of `prompt`, not additions (a maintainer-confirmed bug-fix issue in a different observability platform hitting the exact same ambiguity)

### Tertiary (LOW confidence)
- slopcheck's SLOP verdict on the openinference-semantic-conventions module — included for transparency in the Package Legitimacy Audit, but explicitly treated as a false positive given three independent primary-source contradictions

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — module existence and exact constants verified via primary-source download, not secondhand summary
- Architecture (span synthesis + idempotency): HIGH — every claim grounded in direct, full reads of the actual four handler bodies and `resume.go`, not inference from the milestone-level architecture doc alone
- Pitfalls: HIGH — `JobStatus` field semantics and `nil`-completedJob path both verified against primary sources (K8s API godoc; an existing, passing test)
- D-08 token/cost formula: HIGH — triangulated across three independent sources (Phoenix cost-calculator source, a real example trace, an independent third-party issue) that all agree

**Research date:** 2026-07-15
**Valid until:** 30 days (stable Go module + stdlib-adjacent K8s API + already-shipped internal controller code — none of these move fast; the sole fast-moving item, the Phoenix Helm chart pin, is explicitly out of this phase's scope per CONTEXT.md's phase boundary)
