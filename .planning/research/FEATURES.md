# Feature Research

**Domain:** LLM/agent trace observability integration (OpenInference spans → self-hosted Arize Phoenix) for a Kubernetes-native hierarchical agent orchestrator
**Researched:** 2026-07-15
**Confidence:** HIGH (Phoenix/OpenInference specifics grounded in Context7 + official docs; comparable-orchestrator landscape MEDIUM — thinner official documentation)

## Context: what already exists in TIDE

This is additive research — TIDE already has the seams this milestone needs to fill:

- `pkg/otelai/attrs.go` — five OpenInference attribute helpers (`LLMInputMessages`, `LLMOutputMessages`, `TokenCount`, `AgentInvocation`, `ArtifactPath`) exist and are unit-tested, but have **zero production call sites** (confirmed: no `tracer.Start` outside `internal/otelinit`).
- `internal/otelinit/provider.go` — env-driven `TracerProvider` construction; no-op when `otel.exporter.endpoint` is empty.
- `charts/tide/values.yaml` (`otel:` block) — OTLP gRPC endpoint, `OTEL_TRACES_SAMPLER`/`OTEL_TRACES_SAMPLER_ARG` (default `parentbased_traceidratio` @ 0.1), service name — all wired and chart-templated already. **Sampling controls are table stakes TIDE already ships**; this milestone doesn't need to build them.
- Per-Task `events.jsonl` — the raw Claude Code stream-json capture (comment in `stream_parser.go` confirms `session_id`, `model_usage`, durations "ride through... untouched for Phase 4 OpenInference parsing"). This is the reporter's raw material for LLM message-array spans.
- Dashboard Telemetry tab (`TelemetryView.tsx`) — Prometheus-backed cost/token/cache panels, polling every 60s, graceful degradation states. No trace/span awareness today — it queries `tide_*` counters, not OTel data.
- `Project.Spec.Subagent.Levels.<level>.Model` — model identifier is already known per-level at dispatch time (manager holds it before every Job creation).

## Feature Landscape

### Table Stakes (Users Expect These)

An operator who deploys "OpenTelemetry + self-hosted Phoenix" for a hierarchical orchestrator expects Phoenix to look and behave like it does for any other agent framework. Missing any of these makes the integration feel broken or half-wired, even though "some spans exist."

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Real parent/child span tree matching the M→P→P→T hierarchy | Phoenix's core UI *is* the nested trace tree (`AGENT → LLM/TOOL → ...`); a flat list of unrelated root spans is the single biggest "this doesn't work" complaint pattern for agent tracing | MEDIUM | W3C `traceparent` env-var propagation across pod boundaries (Job env / envelope) is a standard, well-established pattern — validates the milestone's locked design (manager creates dispatch span, injects `traceparent` into the Job env). Confirmed via OTel docs: env vars are the conventional carrier when processes, not in-proc calls, are the trust boundary. |
| Correct `openinference.span.kind` per dispatch site | Phoenix renders/queries differently per span kind; `AGENT` vs generic spans changes icon, grouping, and detail-panel layout | LOW | Already coded: `AgentInvocation()` hardcodes `spanKindAgent = "AGENT"`. OpenInference's own definition of AGENT ("higher-level reasoning that acts on tools using LLM guidance") matches every TIDE dispatch level, since each level *is* an LLM-backed subagent authoring an artifact — not just a workflow step. Keep AGENT for all five levels; do not introduce CHAIN as a "grouping folder" span unless a level genuinely delegates without LLM reasoning (none currently do). |
| `llm.model_name` + `llm.provider` attributes on every LLM/AGENT span | **Phoenix will not compute cost or populate its LLM detail view without these** — confirmed required alongside token counts (Context7: "Model information: `llm.model_name` ... `llm.provider`"). | LOW | **Gap found**: `pkg/otelai/attrs.go` emits `llm.system="anthropic"` (a different, non-cost-bearing attribute) but has no `llm.model_name`/`llm.provider` helper today. Must add before Phoenix's built-in cost rollup is usable — otherwise every span shows `$0.00`/blank cost, which reads as "broken," not "not configured." |
| `llm.token_count.total` attribute | Phoenix's documented required-attribute list for cost tracking includes `total`, not just `prompt`/`completion` | LOW | `TokenCount()` currently emits `prompt`/`completion`/`cache_read`/`cache_write` but no `total`. Cheap addition (sum at the call site or in the helper) — same call site as the model_name/provider gap above. |
| LLM input/output message arrays visible in Phoenix's trace detail view | This is the literal headline value proposition of the milestone ("real OpenTelemetry spans... including full LLM input/output message arrays") — an operator opening Phoenix and seeing empty/truncated message content is the single worst first impression | HIGH | Helpers exist (`LLMInputMessages`/`LLMOutputMessages`) but have zero call sites. The reporter Job is the only in-namespace place that can read `events.jsonl` off the PVC (manager cannot mount project PVCs — already an established TIDE constraint from the envelopes-as-artifacts decision). Requires the explicit D-O5 payload-boundary call named in the milestone: full message content as inline attribute VALUES is the whole point here, but very large multi-turn conversations (tool-heavy Claude Code sessions) can produce large attribute strings — this is an **OTLP-transport/Phoenix-render-time concern, not an etcd concern** (traces never touch CRD `.status`, so the project's own "keep CRDs small" persistence rule does not apply here — a different persistence path entirely). Recommend a pragmatic cap (e.g. truncate individual message content at some threshold with an explicit "truncated" marker) rather than an unbounded inline dump, to keep OTLP export batches and Phoenix's span-attribute table renderable. |
| Self-hosted Phoenix reachable at the chart's existing `otel.exporter.endpoint` | The chart already exposes this env-driven hook; users expect a documented recipe, not a from-scratch integration exercise | LOW | Phoenix ships an **official OCI Helm chart** (`oci://registry-1.docker.io/arizephoenix/phoenix-helm`) — `helm install phoenix $CHART_URL`. Installed **separately** from TIDE's chart (no subchart dependency, matches the milestone's locked constraint). Exposes OTLP gRPC on 4317 / HTTP on 4318, web UI on 6006. |
| Phoenix persistence that survives pod restarts | An operator who loses all traces on the first Phoenix pod restart will conclude self-hosting "doesn't work" | LOW–MEDIUM | Two supported paths: SQLite-on-PVC (fine for single-operator/dev, TIDE's kind/minikube posture) or Postgres (**bundled as part of Phoenix's own Helm chart** — this is Phoenix's subchart, not TIDE's, so it doesn't violate TIDE's "no subchart dependency" rule, which is about TIDE's *own* chart not vendoring Phoenix). Document both; default recommendation should match TIDE's existing dev-cluster posture (PVC-backed SQLite for kind, note Postgres for production). |
| Model/cost/duration attributes sourced from data the manager already holds | Re-deriving cost/duration independently in the tracing path risks a second, disagreeing number | LOW | Manager already computes cost via v1.0.7's exact-ID pricing engine and holds `Model` per level at dispatch time — the span attributes should be populated from these same values, not recomputed. |
| Trace context survives across the reporter Job (not just manager→executor) | The milestone explicitly requires LLM message-array spans to parent correctly under the manager's dispatch span — a disconnected reporter trace defeats the whole "one navigable tree" value prop | MEDIUM | Same `traceparent` carrier as the manager→Task Job hop, but now manager→reporter Job. Confirms as the same solved pattern, applied twice. |

### Differentiators (Competitive Advantage)

Not required for the milestone's stated Target Features, but exploit what TIDE already has (the dashboard, the CRD status model, the multi-level hierarchy) in ways most agent-tracing integrations don't bother with because most callers aren't Kubernetes orchestrators with a persistent control plane.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Dashboard → Phoenix trace URL deep link | Click a Task/Plan row in TIDE's dashboard, land directly on that span's Phoenix trace (`{phoenix-url}/projects/{project}/traces/{trace_id}`) — closes the loop between "what happened" (dashboard) and "why" (trace detail with full message content) | MEDIUM | Requires: (1) persisting `trace_id`/`span_id` somewhere the dashboard's API can read — a new CRD `.status` field per level is the natural fit (16 bytes for a trace ID, well within the "keep per-Task CRDs small" rule); (2) a configured Phoenix base URL in chart values for the dashboard to build the link from. Not in this milestone's stated Target Features (which stop at emission + install docs + one live proof) — recommend as the natural v1.x follow-up, not scope creep into this milestone. |
| `session.id` = Project UID for cross-trace/cross-resumption grouping | TIDE resumability means a single logical "run" can span multiple manager restarts or multiple root traces (e.g., if trace context isn't preserved across a manager crash/restart). Phoenix's `ProjectSession` concept rolls up token/cost/latency **across multiple traces** under one session — this is a close semantic match, and Phoenix's own session GraphQL API already computes `tokenUsage`/`costSummary` rollups for free. | LOW | `setSession()`/`session.id` attribute is a one-line addition once the AGENT span exists. Gives a second, independently-computed cost/token rollup (Phoenix's session view) that can cross-check TIDE's own Prometheus-based budget rollup — valuable for catching the class of accounting bugs the project has hit before (the Claude-5 budget overcount, the adoption-lifecycle rollup gap). |
| `metadata` / `tag.tags` carrying TIDE identifiers (phase/plan/task name, wave index, gate profile, failure-halt state) | Phoenix's filter DSL queries on `metadata`/`tag.tags` — lets an operator filter Phoenix's UI by "show me every AGENT span from Phase 12" or "every span from a conservative-profile run" without leaving Phoenix | LOW | Cheap: these are strings/JSON the manager already has in scope at span-creation time. High leverage for an operator debugging one wave among many. |
| Runtime-neutral adapter with self-instrumenting capability flag | Forward-compatible with the already-locked LangGraph beachhead: a future LangGraph subagent emits OpenInference spans natively (`openinference-instrumentation-langchain`), so the reporter's `events.jsonl` parser must be skippable per-runtime to avoid **double-emitting spans** for the same dispatch | MEDIUM | Already named as a locked constraint in `PROJECT.md`, not optional — listed here because it's a genuine differentiator versus bolt-on tracing integrations that don't anticipate a second, self-instrumenting runtime arriving later. The adapter boundary is what keeps this from becoming a rewrite when LangGraph lands. |
| `graph.node.id` / `graph.node.parent_id` explicit DAG attributes | OpenInference's spec defines these specifically for representing multi-agent/execution-graph structure independent of the OTel span-parent relationship | LOW | Optional/nice-to-have — TIDE's dispatch hierarchy is already a strict tree (each level has exactly one parent), so standard OTel span nesting already gives Phoenix's trace-tree view everything it needs. These attributes matter more for graphs with genuine fan-in (multiple parents) or cross-links, which TIDE's Execution DAG has at the *task* level within a plan (siblings, dependents) but Phoenix's span tree doesn't natively render non-tree graphs — low priority, would need its own visualization work Phoenix doesn't provide out of the box. |
| 100%-sample default (or an explicit low-volume override) instead of the chart's current 10% | TIDE's dispatch volume is nothing like a high-QPS web service — a single Milestone run might produce a few dozen to a few hundred spans total, not millions. At 10% `traceidratio` sampling, an operator watching one Milestone run has a 90% chance any given Phase/Plan/Task span never reaches Phoenix at all, which will read as "half the traces are missing," not "sampling is working as designed." | LOW | Not new code — this is a values.yaml default reconsideration, already fully wired. Flagging because "sampling controls" was explicitly named in the research question and the current default is tuned for the wrong traffic shape for this milestone's use case. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| Bespoke trace-viewer UI inside TIDE's own dashboard | "Why make the operator leave the dashboard?" | Phoenix already *is* a purpose-built trace UI (tree view, message detail, session rollups, filter DSL) — reimplementing any meaningful subset is wasted effort competing with a tool this milestone exists specifically to adopt | Dashboard → Phoenix deep link (see Differentiators) — link out, don't rebuild |
| An OTel Collector middle-tier (à la Argo Workflows' Collector+Tempo+Grafana+Prometheus stack) | "Best practice" pattern seen in generic K8s tracing writeups | Phoenix **already doubles as its own OTLP collector** (ingests gRPC/HTTP directly) — adding a Collector between the manager and Phoenix is an unnecessary hop for this milestone's scale, and tail-based sampling (the main reason to run a Collector) requires all spans of a trace to land on the same Collector instance, which is real operational complexity TIDE doesn't need yet | Point `otel.exporter.endpoint` straight at Phoenix's OTLP port; revisit a Collector only if trace volume across many concurrent Projects genuinely grows past what head-based sampling can manage |
| Vendoring/subcharting Arize's Phoenix Helm chart into `charts/tide/` | "One `helm install` for everything" convenience | Couples TIDE's release cadence to Arize's chart versions, contradicts the pattern already used successfully for Prometheus/Grafana (`prometheus.enabled=false` default, documented-install posture per TELEM-01), and locks operators who already run Phoenix for other apps into a second, TIDE-owned instance | Documented separate install (`INSTALL.md`/`observability.md` recipe using Phoenix's own official chart), `otel.exporter.endpoint` as the only coupling point — this is already the locked decision in `PROJECT.md`, confirmed correct against how Phoenix actually ships |
| Inlining full envelope/artifact PVC content into span attribute values | "More detail is always better" | Violates the already-code-enforced D-O5 boundary (`TestNoPayloadHelperOnPublicSurface` exists specifically to prevent this); large inlined payloads bloat OTLP export batches and make Phoenix's attribute table slow/unreadable; conflates two different persistence paths (artifact PVC vs span attribute) that TIDE deliberately kept separate | `ArtifactPath()` attribute (a reference) for large artifacts; reserve inline message-array attributes for the bounded conversational turns Phoenix's UI is actually built to render |
| Treating Phoenix's own cost rollup as authoritative / reconciling it against TIDE's budget engine | "One number to trust" instinct once both surfaces show a dollar figure | Phoenix computes cost from its own pricing table (Settings > Models, independently maintained) against whatever `llm.model_name` string TIDE's spans happen to carry; TIDE's own budget engine uses exact-ID pricing wired directly into the manager (the thing that gates spend). These two numbers **will** drift on pricing-table lag or model-string mismatches, and reconciling them is real, ongoing maintenance work with no operator-facing payoff | Populate `llm.model_name`/`llm.provider` accurately so Phoenix's number is *directionally* useful for spot-checking, but keep TIDE's CRD-status budget/cost rollup as the sole gate-enforcement source of truth — never let Phoenix's number block or unblock spend |

## Feature Dependencies

```
[Manager dispatch-chain span emission]
    └──requires──> [W3C traceparent propagation into Job env] (shared pattern, used twice)
                       └──requires──> [envelope/env carrier field already exists as a plumbing point on the Job spec]

[LLM message-array spans (reporter)]
    └──requires──> [events.jsonl capture] (already exists)
    └──requires──> [D-O5 payload-boundary decision: inline vs. truncate vs. ArtifactPath] (explicit open call this milestone must make)
    └──requires──> [W3C traceparent propagation into reporter Job env] (same mechanism as manager→Task)

[Phoenix built-in cost/token rollup]
    └──requires──> [llm.model_name attribute] (GAP — not in pkg/otelai today)
    └──requires──> [llm.provider attribute] (GAP — not in pkg/otelai today)
    └──requires──> [llm.token_count.total attribute] (GAP — TokenCount() omits it today)

[Self-hosted Phoenix surface]
    └──requires──> [chart otel.exporter.endpoint wiring] (already exists)
    └──requires──> [Phoenix official Helm chart / manifests, installed separately]

[Dashboard → Phoenix trace URL deep link] (differentiator, not this milestone's core scope)
    └──requires──> [trace_id/span_id persisted to CRD .status per level] (new field, small)
    └──requires──> [Phoenix base URL configured in chart values]
    └──enhances──> [Manager dispatch-chain span emission]

[session.id = Project UID grouping] (differentiator)
    └──enhances──> [Manager dispatch-chain span emission]
    └──conflicts with nothing──> additive attribute only

[Runtime-neutral adapter / self-instrumenting capability flag]
    └──requires──> [Subagent interface seam already exists]
    └──enhances──> [LLM message-array spans] (prevents double-emission once LangGraph lands)
```

### Dependency Notes

- **Phoenix cost/token rollup requires attribute additions TIDE doesn't have today** — this is the single most important dependency to surface for the roadmap: the milestone's plain-language goal ("traces are observable... with real spans") is achievable without touching `pkg/otelai/attrs.go`'s public surface, but Phoenix's *cost display specifically* silently fails (renders blank/zero) without `llm.model_name`/`llm.provider`/`llm.token_count.total`. Any phase that claims "cost is visible in Phoenix" as a deliverable needs this addition explicitly in scope, not assumed to fall out of `TokenCount()`/`AgentInvocation()` as they exist today.
- **The D-O5 payload-boundary decision is genuinely open, not a rubber stamp** — the existing `pkg/otelai` code enforces "no inline payload helper" as a hard invariant (a test fails the build if one is added), but this milestone's headline feature is precisely "full LLM input/output message arrays" in spans. Squaring these two requires either (a) a scoped exception for message-array content specifically (distinct from generic "payload"), or (b) a size-bounded inline strategy (truncate + reference ArtifactPath for overflow). This is a real design decision for the roadmap to schedule deliberately, not a mechanical wiring task.
- **Dashboard deep-linking depends on a new CRD status field** — small, but it's a schema change (v1alpha3 is the sole served version per v1.0.7's API lifecycle crank), so it should land in the same phase as any other v1alpha3 field additions this milestone might need, not as an afterthought bolted onto a later phase.
- **Self-hosted Phoenix has no dependency on TIDE code changes at all** — it's purely a docs + `otel.exporter.endpoint` values.yaml default deliverable, and can be authored/verified in parallel with the Go-side span emission work.

## MVP Definition

### Launch With (v1.0.8 — matches PROJECT.md's stated Target Features exactly)

- [ ] Dispatch-chain span emission (manager) at all five hierarchy levels — AGENT span kind, `agent.invocation.level`, cost/duration/token attributes sourced from data the manager already holds — *essential: this is the milestone's core value proposition, and the helper functions already exist, just unused*
- [ ] `llm.model_name` + `llm.provider` + `llm.token_count.total` attribute additions to `pkg/otelai` — *essential: without these, Phoenix's cost/LLM-detail view silently renders blank, which will read as "broken" not "working as designed"*
- [ ] W3C `traceparent` propagation into Task Job env AND reporter Job env — *essential: this is what makes the five-level hierarchy render as one navigable tree instead of disconnected root spans*
- [ ] LLM message-array spans (reporter, from `events.jsonl`) with an explicit, documented payload-boundary decision (inline-bounded vs. reference) — *essential: named explicitly in the milestone's Target Features as the headline capability*
- [ ] Self-hosted Phoenix install recipe (INSTALL.md/observability.md) using Phoenix's official OCI chart, `otel.exporter.endpoint` wiring, NOTES.txt nudge — *essential: stated Target Feature, zero TIDE code dependency, can run in parallel*
- [ ] Live end-to-end proof: one real run's trace tree visible and queryable in Phoenix — *essential: stated Target Feature, the acceptance bar for the milestone*

### Add After Validation (v1.x)

- [ ] Dashboard → Phoenix trace URL deep link — *trigger: once trace_id is flowing and stable, and once the dashboard's existing CRD-status read path has a natural field to extend*
- [ ] `session.id` = Project UID grouping for cross-trace/cross-resumption cost rollup — *trigger: once multi-restart resumption scenarios are actually exercised in dogfooding and the "does trace context survive a manager restart" question has a concrete answer*
- [ ] `metadata`/`tag.tags` enrichment (phase/plan/wave/gate-profile) for Phoenix filter-DSL queries — *trigger: cheap, could plausibly land in this milestone if time allows, but not load-bearing for the acceptance bar*
- [ ] Sampling default reconsideration (10% → 100% or a TIDE-appropriate default) — *trigger: after the first live proof run, to confirm the "half my spans are missing" failure mode is real at TIDE's actual dispatch volume*

### Future Consideration (v2+)

- [ ] Native LangGraph self-instrumentation + adapter skip-if-native flag — *defer: blocked on the LangGraph specialist beachhead itself, which is explicitly sequenced after this milestone; the adapter *seam* must exist now (already locked as a constraint) but the actual flag-flip logic has nothing to activate until LangGraph subagents exist*
- [ ] Direct-SDK subagent backend trace refinement (ties to CACHE-F1) — *defer: CACHE-F1 itself is an unscheduled follow-up; tracing refinements on a backend that doesn't exist yet are premature*
- [ ] OTel Collector middle-tier / tail-based sampling — *defer: no evidence TIDE's dispatch volume needs this; revisit only if many concurrent Projects genuinely produce trace volume head-based sampling can't manage*
- [ ] `graph.node.id`/`graph.node.parent_id` explicit DAG attributes for non-tree Execution DAG structure — *defer: Phoenix has no first-class visualization for this beyond standard span nesting; would require bespoke work on a tool this milestone is deliberately not trying to extend*

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|----------------------|----------|
| Dispatch-chain AGENT span emission (5 levels) | HIGH | MEDIUM | P1 |
| `llm.model_name`/`llm.provider`/`token_count.total` attrs | HIGH | LOW | P1 |
| `traceparent` propagation (Task + reporter Jobs) | HIGH | MEDIUM | P1 |
| LLM message-array spans + payload-boundary decision | HIGH | HIGH | P1 |
| Self-hosted Phoenix install docs | HIGH | LOW | P1 |
| Live e2e trace-tree proof | HIGH | LOW (proof, not build) | P1 |
| Dashboard → Phoenix trace URL deep link | MEDIUM | MEDIUM | P2 |
| `session.id` = Project UID grouping | MEDIUM | LOW | P2 |
| `metadata`/`tag.tags` enrichment | LOW–MEDIUM | LOW | P2 |
| Sampling default reconsideration | MEDIUM | LOW | P2 |
| Native LangGraph self-instrumentation adapter flag | HIGH (future) | MEDIUM | P3 |
| OTel Collector / tail-based sampling | LOW (now) | HIGH | P3 |
| `graph.node.*` DAG attributes | LOW | MEDIUM | P3 |

## Competitor / Comparable-Orchestrator Feature Analysis

| Capability | LangGraph Platform (LangSmith) | Argo Workflows | Dagster | TIDE's Approach |
|------------|--------------------------------|-----------------|---------|------------------|
| Native LLM/agent tracing | Yes — purpose-built, LangSmith SDK is the primary path (lower overhead than OTel per LangChain's own docs); **added full end-to-end OpenTelemetry support** as a secondary, standards-based path (send to LangSmith or any OTel backend) | No — Argo's own OTel support instruments the **workflow-controller and argoexec** (generic K8s workflow-step spans: DAG structure, step timing), not LLM-specific attributes; ships its own Collector+Tempo+Prometheus+Grafana stack for this | Thin — surfaces token/credit usage as **asset metadata** via its OpenAI integration, not OpenInference/GenAI-semconv spans; no first-class agent trace tree found in current docs | OpenInference-on-OTel spans emitted natively from the orchestrator's own dispatch/reconcile path — closer to LangGraph Platform's "standards-based OTel" posture than to Argo's generic-workflow-spans or Dagster's asset-metadata posture |
| Session/conversation grouping | Yes — LangSmith threads/sessions native | No — no session concept, workflows are the unit | No — assets are the unit, no session concept | Phoenix's `session.id`/`ProjectSession` is available and a strong semantic match for TIDE's own Project-as-a-run concept (differentiator, not yet built) |
| Self-hosted OSS trace backend | LangSmith is not self-hostable OSS at this tier; OTel path lets you point at any backend (Phoenix, Jaeger, etc.) instead | Self-hosted by construction (it's a K8s controller); ships its own observability stack as part of its OTel getting-started guide | Self-hosted by construction; observability story is thinner for LLM-specific data | Matches: OTel-standard emission + a documented, separately-installed self-hosted Phoenix — no vendor lock to a hosted SaaS trace backend |
| Hierarchical multi-agent trace tree | Yes — LangGraph's graph structure maps naturally to nested spans in LangSmith | Partial — DAG structure is visible as workflow steps, but not as LLM-aware AGENT/LLM/TOOL spans | No — asset lineage graph exists but isn't a trace/span concept | TIDE's five-level hierarchy (Milestone→Phase→Plan→Task→Wave) maps directly onto nested AGENT spans — this is the milestone's core bet, and it's a stronger structural fit than any of the three comparables, none of which have a fixed hierarchical-agent-dispatch model to begin with |

**Takeaway:** TIDE's problem shape (a fixed five-level agent-dispatch hierarchy, each level itself an LLM-backed subagent, running as isolated K8s Jobs) doesn't have a close analog among these three. LangGraph Platform is the closest in spirit (standards-based OTel emission, session grouping, self-hostable trace backend of choice) but its unit of work is an in-process graph, not cross-pod K8s Jobs — so its OTel support doesn't have to solve the `traceparent`-across-process-boundary problem TIDE's Job-per-dispatch model requires. Argo and Dagster's observability stories are generic-workflow or asset-lineage flavored, not agent/LLM-native — confirming that OpenInference-on-OTel is a genuine differentiator for a K8s-native orchestrator, not table stakes the ecosystem already provides for free.

## Sources

- Context7 `/arize-ai/phoenix` — span kinds, common attributes (`session.id`, `user.id`, `metadata`, `tag.tags`), session GraphQL rollups, cost-tracking required attributes, project-routing resource attributes (all HIGH confidence, official docs)
- [OpenInference Semantic Conventions spec](https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md) — span kind definitions (LLM/TOOL/AGENT/CHAIN/etc.), `graph.node.id`/`graph.node.parent_id` — HIGH confidence, canonical spec
- [Phoenix Self-Hosting — Kubernetes/Helm](https://arize.com/docs/phoenix/self-hosting/deployment-options/kubernetes-helm) — OCI chart reference, install commands, Postgres bundling — MEDIUM confidence (WebFetch summary; port/resource details not fully confirmed in fetched content)
- [Phoenix Cost Tracking docs](https://arize.com/docs/phoenix/tracing/how-to-tracing/cost-tracking) — required attributes for cost calc (`llm.model_name`, `llm.provider`, `llm.token_count.*`) — HIGH confidence via Context7
- [Phoenix Authentication docs](https://arize.com/docs/phoenix/self-hosting/features/authentication) — OSS self-hosted has no auth by default, opt-in via `PHOENIX_ENABLE_AUTH` — MEDIUM confidence (WebSearch summary)
- [Phoenix Sessions tutorial](https://github.com/arize-ai/phoenix/blob/main/docs/phoenix/tracing/tutorial/sessions.mdx) — `session.id` propagation pattern (Python `using_session`, TS `setSession`) — HIGH confidence via Context7
- TIDE repo ground-truth: `pkg/otelai/attrs.go`, `pkg/otelai/attrs_test.go`, `internal/otelinit/provider.go`, `charts/tide/values.yaml` (otel block), `internal/subagent/anthropic/stream_parser.go`, `dashboard/web/src/components/TelemetryView.tsx`, `.planning/PROJECT.md` — direct repo inspection, HIGH confidence
- [Red Hat: Distributed tracing for agentic workflows with OpenTelemetry](https://developers.redhat.com/articles/2026/04/06/distributed-tracing-agentic-workflows-opentelemetry) — general agent-tracing pattern context — MEDIUM confidence (single-source WebSearch summary)
- [Argo Workflows OpenTelemetry getting-started](https://argo-workflows.readthedocs.io/en/latest/telemetry-getting-started/) — Argo's Collector+Tempo+Prometheus+Grafana pattern, workflow-controller/argoexec span scope — MEDIUM confidence (WebSearch summary)
- [LangChain: Trace with OpenTelemetry](https://docs.langchain.com/langsmith/trace-with-opentelemetry) / [LangSmith OTel blog post](https://blog.langchain.com/end-to-end-opentelemetry-langsmith/) — LangSmith native vs. OTel tracing tradeoffs — MEDIUM confidence (WebSearch summary)
- [Dagster: Building AI Products That Scale](https://dagster.io/blog/building-ai-products-that-scale) — Dagster's asset-metadata approach to LLM cost/token visibility — LOW-MEDIUM confidence (thin official documentation found; no dedicated OpenLLMetry integration confirmed)
- [OneUpTime: Environment Variables as Context Propagation Carriers](https://oneuptime.com/blog/post/2026-02-06-environment-variables-context-propagation-carriers/view) — `TRACEPARENT`/`TRACESTATE` env-var carrier pattern for cross-process K8s workloads — MEDIUM confidence (WebSearch summary, pattern is well-established/uncontroversial)

---
*Feature research for: LLM/agent trace observability (OpenInference + self-hosted Phoenix) on TIDE, a Kubernetes-native hierarchical agent orchestrator*
*Researched: 2026-07-15*

