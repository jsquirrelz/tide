# Project Research Summary

**Project:** TIDE — v1.0.8 "Phoenix Rising"
**Domain:** OpenInference/OpenTelemetry trace emission for a Kubernetes-native hierarchical agent orchestrator, wired to a self-hosted Arize Phoenix
**Researched:** 2026-07-15
**Confidence:** HIGH

## Executive Summary

Phoenix Rising is an *additions* milestone, not a from-scratch integration: TIDE already made the OpenInference-on-OTel bet at project start (`pkg/otelai/` attribute helpers, env-driven `internal/otelinit` TracerProvider, a chart-exposed `otel.exporter.endpoint`), but nothing calls any of it — zero `tracer.Start` call sites exist in the reconcilers or the dispatch path today, so a self-hosted Phoenix stood up against current `main` would render completely empty. The work is to wire emission into five existing Job-completion call sites (retroactive span synthesis, not live/held-open spans), extend the already-battle-tested reporter-Job pattern to a level that has none today (Task), thread a W3C `traceparent` one hop at a time using `.status` as the durable carrier (mirroring TIDE's existing "level boundary = durable artifact" resumability philosophy), and document a self-hosted Phoenix install as a separate `helm install` — never a subchart — matching the TELEM-01 precedent already used for Prometheus. No new Go module is required for the mechanically hardest parts (W3C propagation, retroactive timestamps, remote-span-context reconstruction are all already in the pinned `go.opentelemetry.io/otel` v1.43.0 family); the only new dependency is a small, zero-transitive-dependency OpenInference Go semconv package to replace hand-rolled attribute-key strings.

The recommended approach threads three load-bearing design decisions through the whole milestone. First, **retroactive span synthesis**: because controller-runtime reconcilers are stateless and requeue-driven, spans must be created and closed in the same function call at Job-completion time using `completedJob.Status.{StartTime,CompletionTime}` — never held open across a `Reconcile()` return, which would silently leak/vanish on restart. Second, **deterministic trace IDs, chronologically-threaded span IDs**: deriving the trace ID from `Project.UID` eliminates any need to propagate it at all, and because nothing is ever dispatched before its parent's Job completes, no custom `IDGenerator` hack is needed — a level's span ID only needs to exist *after* completion, exactly when every consumer needs it. Third, **the D-O5 payload boundary is a real, unresolved design decision, not a rubber stamp**: the milestone's headline feature ("full LLM input/output message arrays") pushes directly against the existing `TestNoPayloadHelperOnPublicSurface` invariant and the OTLP gRPC 4 MB message ceiling, and — more seriously — `events.jsonl` is explicitly unredacted (comment-documented as "untouched" for exactly this future OpenInference-parsing purpose), so shipping the headline feature without a scrub pass reuses none of TIDE's existing secret-redaction machinery and risks shipping leaked credentials straight into Phoenix's default-infinite-retention store.

The biggest risks cluster around three things the milestone can get structurally right and still ship broken-looking or unsafe: (1) Phoenix's own chart defaults — ephemeral SQLite, infinite retention, no auth, 20Gi PVC sizing — collide badly with TIDE's already-documented 8 GiB dev-VM constraint and with a milestone that inlines full repo content into spans; (2) the chart's existing 10% trace sampler is a per-*run* coin flip under TIDE's design (one root span per entire multi-hour run), so an operator following the bare quickstart has a 90% chance of seeing an empty Phoenix on the very first try; (3) span creation has no natural idempotency the way Job creation does (`AlreadyExists`-is-success), so wiring span creation into a reconcile body without a state-transition guard risks duplicate/fragmented traces the first time a real dispatch requeues more than once. All three are cheap to prevent (a size/redaction threshold, explicit `tracesSamplerArg=1.0` override for the doc's quickstart and the milestone's own live proof, and gating span creation off the same status-condition edges that already gate Job creation) but easy to miss because each individually "looks done" after a single successful demo run.

## Key Findings

### Recommended Stack

The Go-side mechanics need **zero new go.mod dependencies** for the hardest parts: W3C `traceparent` inject/extract (`propagation.TraceContext{}` + `propagation.MapCarrier`), retroactive span timestamps (`trace.WithTimestamp`, which satisfies both `SpanStartOption` and `SpanEndOption`), and remote-parent reconstruction (`trace.NewSpanContext` + `trace.ContextWithRemoteSpanContext`) are all already present in the pinned `go.opentelemetry.io/otel` v1.43.0 family and verified directly against the vendored module cache. The two genuinely new additions are `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` v0.1.1 (canonical attribute-key constants, zero transitive deps, values already match `pkg/otelai`'s hand-rolled strings — a safe, behavior-preserving swap) and, separately, the self-hosted Phoenix Helm chart (`oci://registry-1.docker.io/arizephoenix/phoenix-helm`, chart 10.0.0 at research time but ships ~daily — re-verify the pin immediately before implementation).

**Core technologies:**
- `go.opentelemetry.io/otel/propagation` v1.43.0 (already pinned) — W3C `traceparent` inject/extract across manager → Job pod boundaries; zero version change
- `go.opentelemetry.io/otel/trace` `WithTimestamp`/`NewSpanContext` v1.43.0 (already pinned) — retroactive span synthesis and remote-parent reconstruction; the entire mechanism this milestone's Job-completion-triggered emission depends on
- `openinference-semantic-conventions` v0.1.1 — canonical, spec-generated attribute-key constants replacing hand-rolled strings in `pkg/otelai/attrs.go`; zero transitive deps, directly serves the runtime-neutrality requirement that Phoenix queries survive a future LangGraph migration
- Phoenix Helm chart `10.0.0` (appVersion `18.0.0`), installed via separate `helm install`, OTLP gRPC on port **4317** (not 6006/HTTP — TIDE's exporter is gRPC-only, no `otlptracehttp` in the codebase)
- `otel.exporter.endpoint` chart value MUST be bare `host:port`, no scheme — `provider.go` calls `otlptracegrpc.WithEndpoint` explicitly, which rejects the `http://` form the raw OTLP env-var spec otherwise allows

### Expected Features

**Must have (table stakes) — matches PROJECT.md's stated Target Features exactly:**
- Dispatch-chain AGENT span emission at all five hierarchy levels, using data (model, tokens, cost, duration) the manager already holds
- `llm.model_name` + `llm.provider` + `llm.token_count.total` attributes — a **confirmed gap**: without these, Phoenix's built-in cost/LLM-detail view silently renders blank, which reads as "broken," not "not configured"
- W3C `traceparent` propagation into both the Task Job env and the reporter Job env — what turns five levels of disconnected root spans into one navigable tree
- LLM message-array spans (reporter, from `events.jsonl`) with an explicit, documented payload-boundary decision (inline-bounded vs. reference) — the milestone's stated headline capability
- Self-hosted Phoenix install recipe (documented, separate `helm install`, zero TIDE code dependency, can run in parallel with Go-side work)
- Live end-to-end proof: one real run's trace tree visible and queryable in Phoenix

**Should have (competitive differentiators, not required for launch):**
- Dashboard → Phoenix trace URL deep link (requires a new, small CRD `.status` field)
- `session.id` = Project UID for cross-trace/cross-resumption cost rollup (strong semantic match to Phoenix's `ProjectSession` concept)
- `metadata`/`tag.tags` enrichment (phase/plan/wave/gate-profile) for Phoenix's filter DSL
- Sampling default reconsideration (10% → a TIDE-appropriate default) once the "coin flip" failure mode is confirmed live

**Defer (v2+):**
- Native LangGraph self-instrumentation adapter flag-flip logic (the *seam* must exist now; the flag has nothing to activate until the LangGraph beachhead milestone)
- OTel Collector middle-tier / tail-based sampling (no evidence TIDE's dispatch volume needs this)
- `graph.node.id`/`graph.node.parent_id` explicit DAG attributes (Phoenix has no first-class visualization beyond standard span nesting)

### Architecture Approach

Retroactive span synthesis at each of the five existing/extended Job-completion handlers is the core pattern: a level's dispatch span is created and closed in the same `handleJobCompletion` call using `completedJob.Status.{StartTime,CompletionTime}`, its resulting SpanID is patched into a new additive `.status.trace.spanID` CRD field, and that field is what a *later*, independent reconcile (dispatching the next level down) reads to format the `TRACEPARENT` it injects into the child's Job env. Because nothing is ever dispatched before its parent's Job completes, no custom IDGenerator is needed — every span ID is minted fresh, after the fact, and threaded forward. The reporter Job (already the sole in-namespace `events.jsonl`/PVC reader for planner-level child materialization) is extended with a trace-only mode to give the Task/executor level — currently the richest LLM conversation with zero observability hook — its first path to span emission.

**Major components:**
1. `pkg/otelai/tracecontext.go` (new, pure functions) — `TraceIDFromUID`, `FormatTraceparent`, `ExtractRemoteParent`; zero K8s deps, fully unit-testable, everything else depends on it — build this first
2. Five manager reconcilers' `handleJobCompletion` (Milestone/Phase/Plan/Project modified, Task net-new) — synthesize each level's own dispatch span and patch `.status.trace.spanID`
3. `internal/dispatch/podjob/jobspec.go` + `internal/controller/reporter_jobspec.go` — inject `TRACEPARENT` (and OTLP env for the reporter) into Job/reporter specs, extending existing env-injection and CLI-arg patterns already used for credproxy/parent-ref plumbing
4. `internal/reporter/tracesynth.go` (new file, kept separate from `materialize.go`) — parses `events.jsonl` into LLM-kind message-array spans, applies the D-O5 redaction pass, requires `cmd/tide-reporter` to gain its first `otelinit.NewTracerProvider` call site (and, critically, its first explicit shutdown-before-exit discipline)
5. `pkg/dispatch/vendor_capabilities.go` (new) — a manager-computed, data-not-code capability flag (`SelfInstruments(vendor)`) so the reporter knows whether to skip message-span synthesis for a future self-instrumenting runtime, without trusting the semi-trusted subagent pod to self-report

### Critical Pitfalls

1. **`events.jsonl` is deliberately unredacted** — the reporter is the only place that has both the raw payload and the outbound hop to Phoenix, and today nothing scrubs it. Reuse `redact.SecretPatterns` as a required pass over `Message.Content` before calling `LLMInputMessages`/`LLMOutputMessages`, treating this as the span-emission equivalent of credproxy's environment boundary.
2. **Full message-array inlining collides with OTLP/gRPC's 4 MB ceiling**, and a single oversized span can drop a whole batch (unrelated spans included), not just itself. Resolve D-O5 with an explicit byte threshold, not a binary always-inline choice — inline under the threshold, fall back to `ArtifactPath` above it.
3. **Span creation has no natural idempotency** — unlike Job `Create`'s `AlreadyExists`-is-success pattern, `tracer.Start()` mints a fresh span/trace ID every call. Gate span creation off the same state-transition edges that already gate Job creation (first-creation, terminal-state), not "did I already do this" checks against ephemeral in-memory state.
4. **Short-lived Jobs drop spans on exit if not explicitly flushed** — the manager's `defer`-based `Shutdown` discipline does not automatically extend to `tide-reporter`, which today is a bare `os.Exit`-driven one-shot binary. Any binary calling `otelinit.NewTracerProvider` must call the returned `ShutdownFunc` on every exit path, not just success.
5. **The chart's default 10% sampler is a per-run coin flip, not a per-span filter**, because TIDE collapses an entire multi-hour run into one root trace under `ParentBased` sampling. An operator following the bare quickstart has a 90% chance of seeing nothing. The install docs and the milestone's own live-proof run must explicitly override `tracesSamplerArg=1.0`.
6. **Phoenix's friendliest-looking defaults compound into a real exposure**: ephemeral SQLite (data lost on pod restart, likely on kind dev clusters), infinite retention (unbounded disk growth given full-message-array embedding), and no auth by default at the chart level — all three must be explicitly overridden in the documented install recipe, not left at bare defaults.

## Implications for Roadmap

Based on combined research, suggested phase structure follows the architecture research's "Suggested Build Order" closely, since it is the one section explicitly sequenced by technical dependency (each step's confidence and rationale are grounded in direct source reads of the actual reconciler call sites):

### Phase 1: Trace-Context Foundation + Dispatch-Chain Span Emission (planner levels)
**Rationale:** `pkg/otelai/tracecontext.go`'s pure helpers have zero K8s dependencies and are a hard prerequisite for every other step; landing dispatch-span synthesis at the four *existing* planner-level completion handlers (Milestone/Phase/Plan/Project) next is pure composition of that helper + already-held reconciler data (`completedJob.Status`, envelope `Usage`) — no new I/O, no new Job spawns. This is independently demoable: real (self-rooted) spans appear in Phoenix as soon as this lands, before propagation is wired.
**Delivers:** `pkg/otelai/tracecontext.go`, the additive `Status.Trace.SpanID` CRD field on Milestone/Phase/Plan/Project, real AGENT-kind spans for four of five levels.
**Addresses:** Dispatch-chain span emission (table stakes), `llm.model_name`/`llm.provider`/`llm.token_count.total` attribute gaps (table stakes).
**Avoids:** Pitfall 3 (reconcile-loop span duplication) by building the state-transition guard in from the start; Anti-Pattern 1/2 (held-open spans, forced SpanIDs) by construction.

### Phase 2: Task-Level Parity + Traceparent Propagation
**Rationale:** Closes the biggest current observability gap (the executor/Task level has zero PVC-side hook today) and threads the parent → child `TRACEPARENT` wiring across every dispatch call site. Sequenced after Phase 1 because it depends on `.status.trace.spanID` already being populated at the parent level.
**Delivers:** `TaskReconciler.handleJobCompletion` span synthesis (new call site), a trace-only reporter-spawn mode (Pattern 3 — reuses reporter scaffolding, skips child-materialization logic), `TRACEPARENT` env injection into `podjob.BuildJobSpec` and `BuildReporterJob`/`ReporterOptions`.
**Uses:** Existing reporter-Job scaffolding (PVC mount, least-privilege SA, deterministic naming); `pkg/otelai/tracecontext.go` from Phase 1.
**Implements:** Architecture Pattern 2 (deterministic TraceID + chronologically-threaded SpanID) and Pattern 3 (reporter as trace-only extension).

### Phase 3: LLM Message-Array Spans (reporter) + D-O5 Redaction/Size Boundary
**Rationale:** This is the milestone's headline feature and its highest-risk phase — it requires resolving the genuinely open D-O5 payload-boundary decision, adding `cmd/tide-reporter`'s first `otelinit.NewTracerProvider` call site (with correct shutdown discipline), and closing the unredacted-`events.jsonl` gap. Sequenced after Phase 2 because correct parenting depends on propagation already being wired.
**Delivers:** `internal/reporter/tracesynth.go`, a documented+enforced size threshold for inline vs. `ArtifactPath` fallback, a redaction pass reusing `redact.SecretPatterns`, explicit `Shutdown`-before-exit on every `tide-reporter` exit path.
**Addresses:** LLM message-array spans (table stakes, the stated headline capability).
**Avoids:** Pitfall 1 (unredacted secrets into Phoenix), Pitfall 2 (OTLP 4 MB ceiling), Pitfall 4 (dropped spans from unflushed short-lived Job).

### Phase 4: Self-Instrumenting Runtime Adapter Seam (forward-compat scaffolding)
**Rationale:** Pure forward-compatibility scaffolding with zero behavioral effect today (every current vendor returns `false` for self-instrumentation) — sequencing it last avoids blocking the higher-value phases on a capability table with no real second entry until the LangGraph milestone lands.
**Delivers:** `pkg/dispatch/vendor_capabilities.go`, `--emit-message-spans` wiring at all spawn sites, a lightweight contract test proving `TRACEPARENT` env-carrier extraction works end-to-end (so this isn't discovered for the first time when LangGraph actually lands).
**Avoids:** Pitfall 7 (double-instrumentation / orphaned traces from a future runtime not receiving propagated context) — proven now via stub-runtime test, not deferred entirely.

### Phase 5: Self-Hosted Phoenix Install Surface (docs) + End-to-End Proof
**Rationale:** Has no dependency on the Go-side phases beyond needing *something* emitting spans to prove against — can be authored/drafted in parallel with Phases 1–4, but the live end-to-end proof (the milestone's acceptance bar) is gated on at least Phase 2 (dispatch-chain tree) and ideally Phase 3 (message arrays) existing.
**Delivers:** `INSTALL.md`/`docs/observability.md` Phoenix recipe with explicit non-default overrides (PV-backed persistence sized for the 8 GiB dev VM, bounded `database.defaultRetentionPolicyDays`, `PHOENIX_ENABLE_AUTH=true` + Secret-sourced credentials, `tracesSamplerArg=1.0` override for first-run verification), plus a captured live-run trace-tree screenshot as milestone-close proof.
**Delivers:** Self-hosted Phoenix surface (table stakes), live e2e proof (table stakes).
**Avoids:** Pitfall 6 (sampler coin-flip empty demo), Pitfall 8 (ephemeral/infinite-retention/no-auth defaults), Pitfall 9 (20Gi PVC + bundled Postgres colliding with the 8 GiB dev-VM budget), Pitfall 5 (cross-pod clock skew — validate on the actual proof run, clamp/document if observed).

### Phase Ordering Rationale

- Phases 1→2→3 follow a strict dependency chain identified in ARCHITECTURE.md's Suggested Build Order: pure helpers → planner-level synthesis (no new I/O) → Task-level parity + propagation (needs parent spanID) → message-array spans (needs correct parenting to land right). Each phase is independently demoable, which matters for a milestone whose acceptance bar is a live, inspectable proof.
- Phase 4 is deliberately sequenced last because it has zero real behavioral payload until a second runtime exists — but PITFALLS.md flags that skipping a contract test entirely (deferring 100% to the LangGraph milestone) is itself a risk, so a lightweight proof is pulled into scope now rather than fully deferred.
- Phase 5 (docs + Phoenix install) can run in parallel with the Go-side phases per ARCHITECTURE.md's explicit note ("Independent of steps 1–6 — can proceed in parallel on a separate plan/track"), but its *verification* (the live e2e proof) is a hard gate at the end, since PITFALLS.md's entire "Looks Done But Isn't" checklist is about things that only surface when a real trace tree is actually inspected, not from unit tests.
- Grouping the D-O5 redaction/size decision inside the message-array-spans phase (rather than as a separate phase) follows FEATURES.md's Dependency Notes: "the D-O5 payload-boundary decision is genuinely open, not a rubber stamp" — it's inseparable from the code that first makes the inline-vs-reference call, so splitting it out would create an artificial phase boundary around one design decision.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 3 (LLM message-array spans + D-O5 boundary):** the exact `events.jsonl` multi-turn schema and the concrete byte-threshold value are unverified/deferred per ARCHITECTURE.md's own confidence caveat — needs a research pass against real fixture files before implementation, not just the schema comment in `stream_parser.go`.
- **Phase 5 (Phoenix install docs):** the exact chart/appVersion pin is flagged MEDIUM confidence and ships near-daily (9 versions in ~9 days at research time) — re-verify the current chart version immediately before authoring INSTALL.md; do not trust the number recorded in STACK.md without a fresh check.
- **Phase 4 (self-instrumenting adapter):** the concrete `openinference-instrumentation-langchain` span-tree shape (which span kind sits at the graph-invocation root, how tool-call spans nest) was explicitly NOT verified in this research pass — flagged as an open question for the future LangGraph milestone's own research, not something to guess at now; today's Phase 4 only needs the generic env-carrier contract test, not LangGraph specifics.

Phases with standard patterns (skip research-phase):
- **Phase 1 (trace-context foundation + planner-level synthesis):** every primitive is HIGH-confidence, verified directly against the vendored `go.opentelemetry.io/otel` v1.43.0 source; the retroactive-synthesis pattern is fully worked out in ARCHITECTURE.md with code-level detail.
- **Phase 2 (Task parity + propagation):** HIGH confidence, grounded in direct reads of the actual reconciler call sites and the existing reporter-Job scaffolding being extended, not invented.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Every dependency claim verified against the locally-vendored `go.opentelemetry.io/otel*` v1.43.0 module cache, live-fetched Phoenix Helm chart `Chart.yaml`/`values.yaml`, and the OpenInference Go module's `go.mod`/pkg.go.dev page. Two items flagged MEDIUM in STACK.md itself (exact chart/app version, OpenInference Go module's pre-1.0 status) due to near-daily release cadence — re-verify immediately before implementation. |
| Features | HIGH (Phoenix/OpenInference specifics); MEDIUM (comparable-orchestrator landscape) | Phoenix-specific claims (cost-tracking required attributes, session rollups, span kinds) are Context7/official-docs verified. The LangGraph Platform / Argo / Dagster comparison table is thinner-sourced (WebSearch summaries), used only for competitive framing, not load-bearing for phase decisions. |
| Architecture | MEDIUM-HIGH | Retroactive-span mechanics and W3C propagation are HIGH (official OTel docs + direct source reads). The exact events.jsonl multi-turn schema and future LangGraph-side propagation code are explicitly flagged unverified/deferred in ARCHITECTURE.md itself. |
| Pitfalls | HIGH (codebase-grounded); MEDIUM (LangGraph self-instrumentation specifics) | Every codebase-grounded pitfall (redaction gap, OTLP ceiling, span idempotency, shutdown discipline) is verified via direct inspection of the actual TIDE source files cited. The double-instrumentation pitfall (7) is inferred from the general OTel env-carrier pattern, not observed, since no LangGraph adapter exists yet. |

**Overall confidence:** HIGH

### Gaps to Address

- **Exact `events.jsonl` multi-turn schema and a concrete byte-threshold constant** for the D-O5 inline-vs-`ArtifactPath` decision — all four research files agree this is a real, unresolved design call, not a formality; Phase 3 planning should pull real fixture files and pick a specific number, not defer to "some threshold."
- **Phoenix chart/appVersion pin freshness** — STACK.md records `10.0.0`/`18.0.0` as of 2026-07-15 but explicitly warns the chart ships near-daily; Phase 5 planning must re-fetch `Chart.yaml` fresh rather than trusting the pinned number in this research.
- **Cross-pod clock skew in retroactive span synthesis (Pitfall 5)** — only exercisable on a multi-node cluster; the milestone's dev/proof environment (kind, single-node) cannot surface this class of bug. Document as a known limitation rather than treating a clean single-node proof run as full verification.
- **LangGraph's actual span-tree shape** — deliberately out of scope for this milestone (no LangGraph adapter exists), but Phase 4's contract test should be written generically enough (env-carrier extraction only) that it doesn't need to guess at LangGraph-specific span structure it can't yet verify.
- **BatchSpanProcessor queue sizing under bursty wave completion** — PITFALLS.md flags this as unverified ("no test asserts a bound on concurrent in-flight spans per wave"); low priority at today's dispatch volumes but worth a note for whoever plans Phase 1/2's test coverage.

## Sources

### Primary (HIGH confidence)
- `go.opentelemetry.io/otel@v1.43.0` / `go.opentelemetry.io/otel/trace@v1.43.0` local module cache — propagation, `WithTimestamp`, `NewSpanContext`, `ContextWithRemoteSpanContext` — verified directly
- Context7 `/open-telemetry/opentelemetry-go` and `/arize-ai/phoenix` — exporter config, span kinds, cost-tracking attributes, persistence/auth defaults, Helm chart mechanics
- Phoenix official docs (arize.com/docs/phoenix/*) and live-fetched `helm/Chart.yaml`/`helm/values.yaml` from the Phoenix GitHub repo (2026-07-15)
- OpenInference spec (`github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md`) and Go module README/go.mod, pkg.go.dev listings
- Direct TIDE repo source reads: `pkg/otelai/{attrs,doc}.go`, `internal/otelinit/provider.go`, `internal/subagent/anthropic/{subagent,stream_parser}.go`, `internal/harness/redact/`, `internal/credproxy/doc.go`, `cmd/tide-reporter/main.go`, `internal/controller/{reporter_jobspec,dispatch_helpers,task_controller,milestone_controller,phase_controller,plan_controller,project_controller}.go`, `internal/dispatch/podjob/{backend,jobspec}.go`, `internal/reporter/materialize.go`, `pkg/dispatch/{envelope,provider,subagent}.go`, `charts/tide/values.yaml`, `docs/observability.md`, `.planning/PROJECT.md`
- OpenTelemetry spec — Environment Variables as Context Propagation Carriers (`opentelemetry.io/docs/specs/otel/context/env-carriers/`)

### Secondary (MEDIUM confidence)
- `openinference-instrumentation-anthropic-sdk-go` version-floor claim (WebSearch-summarized, not primary-fetched)
- Comparable-orchestrator landscape (LangGraph Platform/LangSmith, Argo Workflows, Dagster OTel/observability posture) — WebSearch summaries, used for competitive framing only
- `openinference-instrumentation-langchain` concrete span-tree shape — package existence and general hook mechanism confirmed; exact structure NOT verified
- BatchSpanProcessor default values (`DefaultScheduleDelay`, `DefaultMaxQueueSize`) — cross-referenced across multiple write-ups, not directly re-fetched from SDK godoc in this pass
- Resource-sizing guidance for self-hosted Phoenix (RAM/disk practitioner reports) — no single authoritative source found

### Tertiary (LOW confidence)
- None flagged at LOW in the underlying research files — all four files' source lists cap at MEDIUM for their least-certain claims.

---
*Research completed: 2026-07-15*
*Ready for roadmap: yes*
