# Phase 42: Trace-Context Foundation + Planner-Level Span Emission - Context

**Gathered:** 2026-07-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Pure, K8s-independent trace-context primitives in `pkg/otelai/tracecontext.go` (deterministic TraceID from Project UID, W3C `traceparent` formatting/extraction, retroactive `trace.WithTimestamp` span synthesis) wired into the four **existing** planner-level Job-completion handlers — `handleProjectJobCompletion` (project_controller.go:1779), `MilestoneReconciler.handleJobCompletion` (milestone_controller.go:501), `PhaseReconciler.handleJobCompletion` (phase_controller.go:454), `PlanReconciler.handlePlannerJobCompletion` (plan_controller.go:488) — so real, attribute-complete AGENT spans appear for Project/Milestone/Phase/Plan using only data the manager already holds (model, cost, duration, token counts from envelope/status).

**Requirements:** ATTR-01 (`llm.model_name` + `llm.provider` on every span), ATTR-02 (`llm.token_count.total` alongside the existing splits), ATTR-03 (attribute keys backed by the official `openinference-semantic-conventions` Go module).

**Explicitly NOT this phase (Phase 43):** Task-level span emission, W3C `traceparent` injection into Job/reporter env (PROP-01), the `.status.trace` CRD field and durable ID persistence (PROP-02), parenting all levels into one connected tree (TRACE-02). Phase 42's spans stand alone; 43 threads them.

</domain>

<decisions>
## Implementation Decisions

### Failure-path span coverage
- **D-01:** Spans emit for **succeeded AND failed** planner-Job completions. Failed completions carry OTel span status `Error`. ATTR-01/02 success criteria formally bind only succeeded levels, so attribute-degraded failure spans do not violate the phase gate.
- **D-02:** **One span per Job attempt**, not per level. Retries (reject/re-plan, `resume --retry-failed`) each produce their own span with that attempt's real `Job.Status.{StartTime,CompletionTime}` — retries are visible in the trace timeline. This aligns with the locked idempotency rule: span creation gates on the same state-transition edges that gate Job creation, which fire once per attempt.
- **D-03:** Failure detail rides as **span status + reason attributes**: status `Error` with the classified Reason as status description, PLUS the envelope's `ExitCode`/`Reason` as span attributes when the envelope is readable — failure class stays queryable in Phoenix's filter DSL.
- **D-04:** When the envelope is unreadable (`envReadOK=false` — possible even on succeeded Jobs), **emit a degraded span anyway**: usage attributes simply absent, plus a marker attribute noting the degradation. Observability must not gate on envelope health. Researcher must pin down a non-envelope source for the resolved model (spec/status) so `llm.model_name` survives degradation where possible — note `plan_controller.go:427`'s comment that the resolved model historically lived only in the PVC envelope.

### ATTR-03 custom-key policy
- **D-05:** Every spec-backed attribute key resolves from the official `openinference-semantic-conventions` Go module. TIDE-custom keys with no spec counterpart (`gen_ai.artifact_path`, `agent.invocation.level`, and any others research identifies) are **renamed into an explicit `tide.*` namespace** (e.g. `tide.invocation.level`) — nothing masquerades as spec vocabulary, and the `gen_ai.*` squat on the rejected OTel GenAI namespace dies. Renames are free now: zero production call sites, zero consumers. The researcher determines which bucket each existing key falls into (module-defined vs custom).
- **D-06:** The module is pinned **exactly at v0.1.1 — no drift-guard test** (user explicitly declined the drift test). `go.mod` freezes the version; bumps are deliberate PRs reviewed by diff.

### Attribute value semantics
- **D-07:** `llm.provider` (and `llm.system`) values are **derived from dispatch data** — the provider identity the manager already knows per dispatch (Project spec / provider abstraction) — replacing the hardcoded `llmSystem = "anthropic"` constant in `attrs.go`. One less site to hunt down when the OpenAI backend or LangGraph runtime lands; matches the runtime-neutrality lock.
- **D-08:** Token-count encoding goes **spec-exact, with license to re-map the existing four-way split**. TIDE's current helper encodes `llm.token_count.prompt` as *uncached-only* tokens (disjoint from `prompt_details.cache_read`/`cache_write`); if research confirms Phoenix/OpenInference treat `prompt_details.*` as subsets OF the prompt count, Phase 42 re-maps the split at the emission layer and computes `llm.token_count.total` per Phoenix's documented formula. Correct cost math beats a minimal diff — and the semantics change is free with zero call sites. Researcher must verify the exact Phoenix cost formula against its docs.

### Claude's Discretion
- **Mid-milestone trace shape** (user chose not to discuss): whether Phase 42's four planner spans already share the deterministic Project-UID TraceID (grouped but unparented in Phoenix until 43) or stay independent roots until Phase 43 threads them. Planner picks whichever composes cleanest with 43's parenting work; research's "self-rooted spans appear in Phoenix as soon as this lands" framing is the prior.
- Span/`agent.name` naming (existing docstring convention `tide.dispatch.<level>` is the prior), exact model-ID form (exact resolved ID per the v1.0.7 exact-ID pricing precedent), and how module constants surface inside `pkg/otelai` (direct use vs local re-export).
- Whether `agent.role`/`agent.name` are module-backed or fall under the D-05 `tide.*` rename — a research fact, not a user decision; apply D-05's policy to whatever research finds.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.0.8 Phoenix Rising, committed `c817f95`/`99d12bd`)
- `.planning/research/SUMMARY.md` — synthesis; Phase 42 = research "Phase 1" (HIGH confidence, explicitly flagged skip-research: every primitive verified against vendored otel v1.43.0 source)
- `.planning/research/ARCHITECTURE.md` — retroactive-synthesis pattern worked out at code level; `tracecontext.go` component spec (`TraceIDFromUID`, `FormatTraceparent`, `ExtractRemoteParent`); Suggested Build Order
- `.planning/research/PITFALLS.md` — Pitfall 3 (span-creation idempotency via state-transition edges) is Phase 42's load-bearing pitfall; also BatchSpanProcessor queue sizing under bursty wave completion (unverified, worth a test-coverage note)
- `.planning/research/STACK.md` — `openinference-semantic-conventions` v0.1.1 module rationale (zero transitive deps, values match current hand-rolled strings)

### Requirements and constraints
- `.planning/REQUIREMENTS.md` — ATTR-01/02/03 exact text (this phase); TRACE-01/02, PROP-01/02 text (Phase 43 boundary — do not pull forward)
- `.planning/PROJECT.md` §"Runtime-neutrality constraints" — the 2026-07-15 lock: trace-context contract is the durable seam; conventions follow OpenInference semconv exactly
- `.planning/STATE.md` §"v1.0.8 binding constraints" — retroactive synthesis same-call rule, deterministic TraceID / fresh span IDs / no custom IDGenerator, state-transition-edge gating

### Existing code (the surfaces this phase touches)
- `pkg/otelai/attrs.go` — existing helpers (`AgentInvocation`, `TokenCount`, `ArtifactPath`, message-array flatteners); the hand-rolled key constants D-05 replaces; the deliberate `.total` omission D-08 reverses
- `pkg/otelai/attrs_test.go` — existing test conventions for the attribute helpers
- `internal/otelinit/provider.go` — env-driven TracerProvider (no-op without `OTEL_EXPORTER_OTLP_ENDPOINT`); Pitfall-24 no-`WithSampler` rule enforced by `TestNoWithSamplerInSource`
- `internal/controller/project_controller.go:1779`, `internal/controller/milestone_controller.go:501`, `internal/controller/phase_controller.go:454`, `internal/controller/plan_controller.go:488` — the four completion handlers gaining span synthesis
- `pkg/dispatch/envelope.go` — `Usage` struct (tokens + `EstimatedCostCents`), tiny-status subset; the data source TRACE-01 mandates ("same envelope/status data the budget tally already uses — never recomputed")

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/otelai/` attribute helpers: complete but with **zero production call sites** — this phase creates the first callers. `AgentInvocation(name, role, level)` already models the AGENT-span shape (span.kind=AGENT, llm.system, agent.name/role/level).
- `internal/otelinit.NewTracerProvider`: already wired in `cmd/manager/main.go` with correct shutdown discipline; reconcilers calling `otel.Tracer(...)` resolve the right provider automatically (no-op without endpoint — zero overhead on plain clusters).
- `go.opentelemetry.io/otel` v1.43.0 (pinned): `trace.WithTimestamp`, `propagation.TraceContext`, `trace.NewSpanContext` all present — zero new go.mod deps for the mechanics; only new dep is the semconv module.

### Established Patterns
- **State-transition-edge gating** (Job-creation idempotency pattern): span creation must gate on the same status-condition edges — never in-memory "did I already do this" state.
- **envReadOK two-phase envelope handling** (`plan_controller.go:512`): existing degraded-read discipline that D-04's degraded spans compose with.
- **Guard/ratchet tests as house style**: `TestNoWithSamplerInSource`, `TestNoPayloadHelperOnPublicSurface`, DAG-import firewall — but note D-06 explicitly declines a module-drift test.
- **Envelope as status optimization, not success authority** (Pitfall-1 parity comments) — span emission must respect the same rule.

### Integration Points
- The four planner-level `handleJobCompletion` functions are the ONLY production call sites this phase adds; each already receives `completedJob *batchv1.Job` (timestamps) and reads the envelope (usage/model/reason) — everything a retroactive span needs is in scope at that line.
- `cmd/manager/main.go` TracerProvider bootstrap is already in place; no binary-level changes expected (reporter/one-shot flush discipline is Phase 44's TRACE-03, not here).

</code_context>

<specifics>
## Specific Ideas

- `gen_ai.artifact_path` squatting on the rejected OTel GenAI namespace was called out and the rename (D-05) chosen specifically because the zero-call-site window makes it free — do the rename now, in this phase, not later.
- User declined the recommended drift-guard test for the semconv module pin (D-06) — do not add one "for safety"; exact pin + PR review is the chosen posture.

</specifics>

<deferred>
## Deferred Ideas

### Reviewed Todos (not folded)
- `2026-07-03-signed-commits-verified-badge.md` (GPG signing / SIGN-02..04) — keyword false-positive; git-identity scope, no tracing overlap.
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` (W-2 candidate finding) — dispatch-gate ordering concern in the same controllers this phase touches, but a different concern; stays a next-milestone candidate.
- `2026-07-12-task-dispatch-gate-order-divergence.md` (W-2 sibling finding) — same disposition as above.
- `cache-f1-direct-sdk-cross-pod-caching.md` (CACHE-F1) — deferred vNext+; no overlap.

</deferred>

---

*Phase: 42-Trace-Context Foundation + Planner-Level Span Emission*
*Context gathered: 2026-07-15*
