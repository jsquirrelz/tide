# Phase 46: Observability Enrichment + Dashboard Deep Link - Research

**Researched:** 2026-07-17
**Domain:** OpenTelemetry sampler semantics (Go SDK v1.43.0), OpenInference session/metadata/tag conventions, Arize Phoenix cost-rollup + trace-URL contract, TIDE dashboard API/SPA plumbing
**Confidence:** HIGH — all three priority questions resolved against either the pinned SDK's vendored source code or official Phoenix/OpenInference documentation (Context7 `/arize-ai/phoenix`), not training-data recall

## Summary

All three of CONTEXT.md's priority research questions have direct, code/doc-grounded answers, and two of them change the shape of the work described in D-02/D-03/D-12.

**D-03 (double-count) is CONFIRMED real, and — critically — it is a Task-level-only problem.** Phoenix computes one `SpanCost` row per span whenever that span carries `llm.token_count.*` + a matchable `llm.model_name`/`llm.provider` pair, with **no `openinference.span.kind` gate** (verified against Phoenix's own `span_cost_calculator.py`/`models.py` source, and independently confirmed by a real upstream bug report — `Arize-ai/openinference#3164` — describing exactly TIDE's shape: an AGENT span with rolled-up totals plus child LLM spans, summed 2x, fixed upstream by dropping token counts from the wrapper span). TIDE's four planner levels (Milestone/Phase/Plan) have **no sibling LLM-kind spans today** — only Task spawns the Phase-44 trace-only reporter that emits per-call LLM spans (`reporter_jobspec.go`'s `TraceOnly` shape is spawned "for completed Task dispatch Jobs" only). So the fix is narrow: drop `otelai.TokenCount(...)` from the Task-level call to `synthesizePlannerSpan` only; the four planner-level AGENT spans keep their token counts unchanged (they are the sole source at those levels).

**D-12 (deep-link URL) has a better answer than the research prior.** Phoenix ≥ 14.2.0 ships a documented "Shareable Project URLs" redirect surface: `GET /redirects/traces/{trace_id}` and `GET /redirects/spans/{span_id}`, both keyed on **raw OpenTelemetry hex IDs with no project segment at all** — no lookup of Phoenix's internal project ID, no dependency on setting `openinference.project.name`. This supersedes the milestone research's `{baseURL}/projects/{project}/traces/{trace_id}` prior. TIDE should build `{phoenixBaseURL}/redirects/traces/{trace_id}` (floor) and `{phoenixBaseURL}/redirects/spans/{span_id}` (span-anchoring enhancement) — zero new resource attributes needed.

**D-02 (sampled-flag coherence) is CONFIRMED real, with a precise mechanism and a right-sized fix.** Reading the pinned `go.opentelemetry.io/otel/sdk@v1.43.0` source directly: `ParentBased`'s default `remoteParentSampled` option is `AlwaysSample()`. Because `synthesizePlannerSpan` always reconstructs each level's parent `SpanContext` with `TraceFlags: trace.FlagsSampled` hardcoded true, **every span below the Project root is unconditionally sampled regardless of the configured ratio** — the `parentbased_traceidratio` sampler only ever gets a real say for the Project-level root span (whose reconstructed parent SpanContext is invalid/zero, so `ParentBased` falls through to its `root` sampler, the real ratio). At OBS-01's new 1.0 default this is invisible (`TraceIDRatioBased(1.0)` is a `predeterminedSampler` — always samples, no computation). At opt-down ratios it means: the ratio gates whether a run's root trace appears in Phoenix at all, but once it does, every descendant level's spans export in full, and if the root doesn't get sampled, its descendants still do (an orphaned trace missing its own root). A cheap, schema-free fix exists for the one place this currently matters in practice (Task-level reporter spawn — the only sibling-LLM-span case): thread the just-emitted span's real `IsSampled()` bit through the same-reconcile call to `traceparentForLevel`/`FormatTraceparent` instead of the hardcoded `true`. The deeper cross-level cascade (Project→Milestone→Phase→Plan→Task) would need a new persisted per-level "sampled" status field to survive cross-controller/cross-reconcile reads — a real schema change, disproportionate to the bug's now-irrelevant-at-default-1.0 impact. Recommend the narrow fix + honest opt-down documentation, not the full schema change.

**Primary recommendation:** Ship OBS-01/02/03/04 largely as CONTEXT.md's decisions describe, with three corrections this research surfaces: (1) drop Task-level-only `llm.token_count.*` from the AGENT span rather than a blanket "adjust all levels" reading of D-03; (2) use Phoenix's `/redirects/traces/{trace_id}` + `/redirects/spans/{span_id}` route family for D-12, not the `/projects/{project}/traces/{trace_id}` prior; (3) fix D-02 narrowly at the Task-level reporter-spawn call site (same-reconcile, no schema change) and document the deeper cross-level limitation rather than attempting a full fix this phase. A fourth correction: **CONTEXT.md's D-09 claim that all five node types share `NodeDetailPanel` is not accurate** — Task nodes render in a separate `TaskDetailDrawer` component; the deep link must be added in both places (see Common Pitfalls).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Sampler default + opt-down docs (OBS-01) | API / Backend (chart values → env) | — | Env-driven sampler read by the OTel SDK inside the manager/dashboard processes; zero application code |
| Sampled-flag coherence fix (D-02) | API / Backend (manager reconciler) | — | Lives entirely inside `internal/controller/span_emission.go` — SpanContext construction is manager-internal state |
| `session.id` / metadata / tags attribute emission (OBS-02/03) | API / Backend (manager + reporter Job) | — | Both span-emitting binaries (manager reconcilers, `tide-reporter`) set attributes at span-creation time; no browser involvement |
| Reporter-side value transport (D-05) | API / Backend (Job spec / CLI args) | — | Manager-authored `ReporterOptions` → Job args → reporter flag parse; the Job spec is the trust boundary, not a client-writable channel |
| Phoenix cost/session-rollup double-count fix (D-03) | API / Backend (span attribute selection) | External service (Phoenix's own aggregation) | TIDE controls only which attributes it emits; Phoenix's SQL aggregation is out of TIDE's control and treated as a fixed external contract |
| Deep-link URL construction (OBS-04) | Frontend Server / API (Go dashboard backend) | Browser / Client (SPA) | Backend resolves deterministic TraceID + per-level SpanID + assembles/streams the URL parts; SPA renders an anchor — URL-shape logic must live in exactly one of these two places (D-11), not both |
| `phoenix.baseURL` config plumbing (D-10) | API / Backend (`cmd/dashboard/main.go` → `ConfigHandler`) | Browser / Client (SPA disabled-state) | Mirrors the existing `telemetryEnabled` chain exactly — server resolves env, exposes via `/api/v1/config`, SPA reads and renders/hides |
| NodeDetailPanel / TaskDetailDrawer link affordance (D-09) | Browser / Client (React components) | — | Pure presentational addition; no new data-fetching beyond what the existing detail payloads already carry once trace identity rides in them |

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**OBS-01 — sampler default flip**
- D-01: `values.yaml` `otel.tracesSamplerArg: "0.1"` → `"1.0"`; sampler stays env-driven (`OTEL_TRACES_SAMPLER(_ARG)`); never `WithSampler(...)` in code (`TestNoWithSamplerInSource` guard untouched). Opt-down documented at `values.yaml` comment block (~lines 407–414) and `docs/observability.md`. Planner must find and deliberately update every surface pinning `"0.1"`.
- D-02 (opt-down honesty): `span_emission.go:246` hardcodes the traceparent sampled flag to `true`. Research verifies actual semantics and either threads the real sampling decision or documents the behavior honestly.

**OBS-02 — session.id + the routed double-count decision**
- D-03 (locked invariant, research pins mechanism): Phoenix's session/trace cost views must not double-count a run. Research pins whether Phoenix sums only `openinference.span.kind=LLM` spans or every span carrying `llm.token_count.*`. Decision rule if double-counting is confirmed: per-call LLM counts are authoritative; AGENT-level totals adjusted/dropped per research recommendation.
- D-04: `session.id` = `string(project.UID)` on every span via a new `pkg/otelai` helper backed by the semconv module's `SessionID` const. Manager-side sites have `project` in hand already. Do NOT conflate with `events.jsonl`'s own `session_id` field.
- D-05 (reporter transport): session.id and OBS-03 values reach the reporter as manager-authored `ReporterOptions` fields → CLI args (the `TraceParent`/`SkipMessageSpans` precedent). Never derived from pod-writable PVC data.

**OBS-03 — metadata / tag.tags split**
- D-06: `metadata` (JSON-encoded map) carries level kind, level name, wave index, gate profile, failure-halt state. `tag.tags` (list) carries only low-cardinality categorical filterables: level kind, gate profile, failure-halt marker. Wave index is metadata-only.
- D-07 (wave index — verified): Task CRs carry `tideproject.k8s/wave-index` label (stamped by `project_controller.go`'s global-wave materialization). Task spans read it from the label; planner-level spans omit wave index unless already in hand.
- D-08: Gate profile = the Project's per-level `Gates` policy (`auto|approve|pause`) for the emitting level; failure-halt state = the project-wide FailureHalt condition presence plus `Spec.FailureProfile`. Exact string encoding is planner's discretion — stable, lowercase, documented.

**OBS-04 — dashboard deep link**
- D-09 (placement): renders in NodeDetailPanel (claimed as "the existing click-through surface all five node types share") — **research found this claim inaccurate; see Common Pitfalls.**
- D-10 (config plumbing): new chart value `phoenix.baseURL` (default `""`) → `PHOENIX_BASE_URL` env on `dashboard-deployment.yaml` → `main.go` resolves → `ConfigHandler` → `GET /api/v1/config` gains `phoenixBaseURL`. Empty/absent → SPA renders no link.
- D-11 (trace identity to SPA): dashboard API payloads must carry per-level trace identity — zero `TraceSpanID` references exist in `cmd/dashboard/` today. TraceID is deterministic (`otelai.TraceIDFromUID`); span IDs come from the five `{Level}TraceSpanID` status fields. Pre-built URL vs. parts-for-SPA-assembly is planner's discretion, but URL assembly logic must live in exactly one place.
- D-12 (URL shape — research pins it): see Summary — resolved to Phoenix's `/redirects/traces/{trace_id}` + `/redirects/spans/{span_id}` shareable-URL family, not the `/projects/{project}/traces/{trace_id}` prior.

### Claude's Discretion
- Names/shapes of new `pkg/otelai` helpers (e.g. `SessionID(uid)`, `Metadata(map)`, `Tags(...)`) and exact metadata JSON key names.
- `ReporterOptions` field/arg names for the new transport values, matching existing Args conventions.
- Whether planner-level spans carry wave index.
- NodeDetailPanel/TaskDetailDrawer link styling/labeling, and where `phoenixBaseURL` trailing-slash normalization happens (server or SPA) — one place only.
- Whether the sampled-flag fix (D-02) is a this-phase code change or a documented limitation, per research's finding (**research recommends: narrow Task-level code fix + honest docs for the deeper cascade — see Summary/Pitfall 3**).

### Deferred Ideas (OUT OF SCOPE)
- Data-minimization toggle (`otel.redactMessageContent`) — fast-follow, not this milestone.
- Span-level deep-link anchoring, if Phoenix's URL contract didn't support it cleanly — **research found it DOES work cleanly (`/redirects/spans/{span_id}`), so this is now in scope as the D-12 enhancement tier, not deferred.**
- Node-shell link affordances (icons directly on DAG nodes) — NodeDetailPanel/TaskDetailDrawer only.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OBS-01 | Chart trace-sampler default 0.1 → 1.0, opt-down documented | Exact surface inventory below (values.yaml, hack/helm mirror, docs/observability.md table, otelinit doc comments); D-02 finding reshapes what "opt-down documented" must honestly say |
| OBS-02 | `session.id` = Project UID on every span for Phoenix's independent cost cross-check | `semconv.SessionID` const verified present; D-03 finding means the cross-check is only trustworthy once the Task-level double-count is fixed — otherwise the "independent" rollup is silently wrong |
| OBS-03 | `metadata`/`tag.tags` enrichment for Phoenix DSL filtering | `semconv.Metadata`/`semconv.TagTags` consts + exact value-encoding verified against the official OpenInference spec (JSON string vs. native string list) |
| OBS-04 | Dashboard deep-links DAG nodes to Phoenix traces, gated on `phoenix.baseURL` | D-12 URL contract resolved with HIGH confidence; dashboard payload/component survey identifies the TWO render surfaces (NodeDetailPanel + TaskDetailDrawer) and the config-chain precedent to copy |

</phase_requirements>

## Standard Stack

### Core

No new dependencies. This phase is additive attribute emission + two small Go/TS surfaces on top of an already-pinned stack.

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.opentelemetry.io/otel/sdk` | v1.43.0 (pinned, unchanged) | Sampler/SpanContext mechanics this phase's D-02 fix touches | Already the project's pinned OTel SDK; verified via vendored source read (`sampling.go`, `sampler_env.go`) |
| `github.com/Arize-ai/openinference/go/openinference-semantic-conventions` | v0.1.1 (pinned, unchanged) | `SessionID`, `Metadata`, `TagTags` consts | Already imported by `pkg/otelai` (ATTR-03); all three new consts verified present in the vendored module — no `go get` needed |

### Supporting

No supporting-library additions. `lucide-react` (already a `dashboard/web/package.json` dependency, `^1.16.0`) supplies whatever external-link icon the NodeDetailPanel/TaskDetailDrawer affordance wants (`ExternalLink` is a standard icon in that set) — no new npm package.

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `/redirects/traces/{trace_id}` (Phoenix ≥14.2.0 shareable URL) | `/projects/{project}/traces/{trace_id}` (the milestone research's prior, internal-ID-based route) | The internal-ID route requires knowing/setting a project name or resolving Phoenix's numeric project ID — a lookup TIDE has no clean way to do server-side without querying Phoenix's own API. The redirect route needs neither; strictly simpler and more robust. |
| Dropping Task-level AGENT `llm.token_count.*` | Dropping `llm.model_name`/`llm.provider` instead (keep token counts, break Phoenix's model match) | Rejected: `pkg/otelai`'s own `LLMIdentity` doc comment (Pitfall 5) already establishes the convention that an empty/absent model reads as "no cost data" — deliberately breaking model identification to dodge a cost rollup would violate that existing convention and leave a confusing half-populated span. Dropping the token-count triad directly is the maintainer-endorsed fix pattern (`Arize-ai/openinference#3164`). |
| Full cross-level sampled-flag propagation (new `{Level}TraceSampled` status field) | Narrow same-reconcile fix at the Task-level reporter spawn only | Rejected for this phase: requires a v1alpha3 schema addition (5 CRD types) to fix a class of bug that is fully invisible at OBS-01's new 1.0 default and only matters for opt-down installs — disproportionate risk/effort. Document the limitation instead; revisit if an opt-down install actually needs coherent per-level sampling. |

**Installation:** none — no `go get`, no `npm install`. All three semconv consts and the SDK sampler internals are already present in the pinned dependency tree.

**Version verification:**
```
$ go list -m all | grep "go.opentelemetry.io/otel/sdk "
go.opentelemetry.io/otel/sdk v1.43.0
$ go list -m all | grep -i openinference
github.com/Arize-ai/openinference/go/openinference-semantic-conventions v0.1.1
```
Both confirmed via `go list -m` against the actual `go.mod`/module cache during this research session — not training-data recall.

## Package Legitimacy Audit

**No new external packages this phase.** OBS-01..04 are implemented entirely with already-pinned, already-vetted dependencies (`go.opentelemetry.io/otel/sdk` v1.43.0, `openinference-semantic-conventions` v0.1.1, `lucide-react` ^1.16.0 in the SPA). The Package Legitimacy Gate protocol (slopcheck / registry verification) is not applicable — there is nothing new to audit.

## Architecture Patterns

### System Architecture Diagram

```
                         ┌─────────────────────────────────────────────┐
                         │  Manager reconciler (5 level completions)    │
                         │  synthesizePlannerSpan(level, project, ...)  │
                         │                                               │
Job completes ──────────▶│  1. build remote parent SpanContext          │
(batchv1.Job terminal)   │     (TraceFlags: FlagsSampled ─ D-02 site)   │
                         │  2. tracer.Start → AGENT span                │
                         │  3. SetAttributes: AgentInvocation,          │
                         │     LLMIdentity, TokenCount* (D-03: DROP     │
                         │     for level=="task" only), SessionID (new),│
                         │     Metadata (new), Tags (new)               │
                         │  4. span.End()                               │
                         │  5. persist {Level}TraceSpanID to .status    │
                         └───────────────┬───────────────────────────────┘
                                         │ same reconcile
                                         ▼
                         ┌─────────────────────────────────────────────┐
                         │  traceparentForLevel(project, spanIDHex,     │
                         │    sampled) ── D-02 fix: thread REAL         │
                         │    IsSampled() bit here, not hardcoded true  │
                         └───────────────┬───────────────────────────────┘
                                         │ ReporterOptions{TraceParent,
                                         │   SessionID, Metadata, Tags (new,
                                         │   Args-based, D-05)}
                                         ▼
                         ┌─────────────────────────────────────────────┐
                         │  tide-reporter Job (per-level spawn)         │
                         │  cmd/tide-reporter/main.go                   │
                         │    materialize.go (child-CR path, 4 levels)  │
                         │    tracesynth.go EmitSpans (Task only,       │
                         │      per-call LLM-kind spans; carries the    │
                         │      SAME SessionID/Metadata/Tags via new    │
                         │      CLI flags)                              │
                         └───────────────┬───────────────────────────────┘
                                         │ OTLP gRPC (env-driven sampler,
                                         │  parentbased_traceidratio)
                                         ▼
                         ┌─────────────────────────────────────────────┐
                         │  Self-hosted Phoenix (external, Phase 47)    │
                         │  - SpanCostCalculator: 1 SpanCost row per    │
                         │    span carrying llm.token_count.* (no       │
                         │    span.kind gate — D-03 finding)            │
                         │  - ProjectSession rollup: SUM(SpanCost)      │
                         │    grouped by session.id                    │
                         │  - /redirects/traces/{trace_id}              │
                         │  - /redirects/spans/{span_id}                │
                         └───────────────▲───────────────────────────────┘
                                         │ deep link (GET, new tab)
                         ┌───────────────┴───────────────────────────────┐
                         │  Browser: NodeDetailPanel (project/milestone/ │
                         │  phase/plan) + TaskDetailDrawer (task —       │
                         │  SEPARATE component, D-09 correction)         │
                         │  ── reads phoenixBaseURL from                │
                         │     GET /api/v1/config (D-10)                │
                         │  ── reads TraceID/SpanID from dashboard API   │
                         │     payloads (D-11 — currently absent)        │
                         └───────────────▲───────────────────────────────┘
                                         │ GET /api/v1/{projects,plans,tasks}
                         ┌───────────────┴───────────────────────────────┐
                         │  cmd/dashboard (Go backend)                   │
                         │  otelai.TraceIDFromUID(project.UID) (no K8s   │
                         │  import needed) + {Level}TraceSpanID read     │
                         │  from the already-cached CRD status           │
                         └────────────────────────────────────────────────┘
```

### Recommended Project Structure

No new files/directories — every change is additive to existing files:

```
pkg/otelai/attrs.go             # + SessionID(), Metadata(), Tags() helpers
internal/controller/span_emission.go   # attribute block + D-02 sampled-bit threading
internal/controller/reporter_jobspec.go # ReporterOptions + arg assembly
internal/controller/{dispatch_helpers,project,milestone,phase,plan,task}_controller.go
                                 # call-site wiring (gate profile / failure-halt lookups)
internal/reporter/tracesynth.go # EmitSpans attribute block (D-05 transport consumption)
cmd/tide-reporter/main.go       # new CLI flags for session/metadata/tags
charts/tide/values.yaml         # tracesSamplerArg "1.0" + phoenix.baseURL block
charts/tide/templates/{deployment,dashboard-deployment}.yaml # PHOENIX_BASE_URL env
cmd/dashboard/main.go           # phoenixBaseURL env resolution
cmd/dashboard/api/config.go     # ConfigHandler + configResponse field
cmd/dashboard/api/{projects,plans,tasks}.go # trace-identity fields on payload structs
dashboard/web/src/lib/          # phoenix URL assembly (ONE place — D-11)
dashboard/web/src/components/NodeDetailPanel.tsx  # link render (4 kinds)
dashboard/web/src/components/TaskDetailDrawer.tsx # link render (task — D-09 correction)
docs/observability.md           # sampler table + opt-down honesty + phoenix.baseURL doc
```

### Pattern 1: Env-driven sampler, resolved from the SDK's own `samplerFromEnv`

**What:** `sdktrace.NewTracerProvider` (no explicit `WithSampler`) auto-resolves `OTEL_TRACES_SAMPLER`/`OTEL_TRACES_SAMPLER_ARG` via `samplerFromEnv()`. For `parentbased_traceidratio`, this constructs `ParentBased(TraceIDRatioBased(ratio))` with the package defaults: `remoteParentSampled: AlwaysSample()`, `remoteParentNotSampled: NeverSample()`, `localParentSampled: AlwaysSample()`, `localParentNotSampled: NeverSample()`.

**When to use:** Already in effect; this phase does not touch `internal/otelinit`. Cited here because the D-02 fix's correctness depends on this exact default-options behavior.

**Example:**
```go
// Source: go.opentelemetry.io/otel/sdk@v1.43.0/trace/sampling.go (vendored, read directly)
func (pb parentBased) ShouldSample(p SamplingParameters) SamplingResult {
	psc := trace.SpanContextFromContext(p.ParentContext)
	if psc.IsValid() {
		if psc.IsRemote() {
			if psc.IsSampled() {
				return pb.config.remoteParentSampled.ShouldSample(p) // AlwaysSample() by default
			}
			return pb.config.remoteParentNotSampled.ShouldSample(p) // NeverSample() by default
		}
		// ...local-parent branches, unused by TIDE (parents are always Remote:true)
	}
	return pb.root.ShouldSample(p) // only reached when psc is invalid — TIDE's Project-root case
}
```

### Pattern 2: Manager-authored transport, never pod-writable data (D-05)

**What:** New session.id/metadata/tags values ride the same channel as `TraceParent`/`SkipMessageSpans` — computed by the reconciler (which has `project`, `Gates`, `FailureProfile`, the `wave-index` label all in hand), placed on `ReporterOptions`, rendered as `BuildReporterJob` CLI args (never Env, per this file's established "100% Args" convention for per-invocation values — `OTLPEndpoint` is the one exception, and it targets the reporter's OWN TracerProvider bootstrap, not per-span attribute values).

**When to use:** Any new per-span attribute value the reporter's `tracesynth.go` needs to apply to its own LLM-kind spans, so a Task's AGENT span and its per-call LLM spans carry identical session.id/metadata/tags (Phoenix's `ProjectSession` groups purely on the `session.id` string match — inconsistent values across sibling spans would fragment the session view).

**Example:**
```go
// Source: internal/controller/reporter_jobspec.go (existing pattern, read directly)
if opts.TraceParent != "" {
    args = append(args, "--traceparent="+opts.TraceParent)
}
if opts.SkipMessageSpans {
    args = append(args, "--skip-message-spans")
}
// New (this phase): mirror the same shape —
// args = append(args, "--session-id="+opts.SessionID)
// args = append(args, "--metadata="+opts.MetadataJSON)   // pre-JSON-encoded by the manager
// args = append(args, "--tags="+strings.Join(opts.Tags, ",")) // reporter splits on comma
```

### Pattern 3: The `telemetryEnabled` config chain, applied verbatim to `phoenixBaseURL`

**What:** Chart value → conditional env render → `main.go` resolution function → `Dependencies`/handler struct field → `/api/v1/config` JSON field → SPA reads once, gates rendering.

**When to use:** OBS-04's D-10. This is a byte-for-byte precedent, not just a similar shape — reuse the SAME "empty env → self-degrade, no error" posture `PROM_ENDPOINT`/`telemetryEnabledFromEnv` already establish.

**Example:**
```yaml
# Source: charts/tide/templates/dashboard-deployment.yaml (existing PROM_ENDPOINT pattern)
{{- if .Values.prometheus.endpoint }}
- name: PROM_ENDPOINT
  value: {{ quote .Values.prometheus.endpoint }}
{{- end }}
# New (this phase), same shape:
# {{- if .Values.phoenix.baseURL }}
# - name: PHOENIX_BASE_URL
#   value: {{ quote .Values.phoenix.baseURL }}
# {{- end }}
```
```go
// Source: cmd/dashboard/main.go (existing telemetryEnabledFromEnv pattern)
func telemetryEnabledFromEnv() bool {
	switch os.Getenv("PROMETHEUS_ENABLED") {
	case "true":
		return true
	case "false":
		return false
	default:
		return os.Getenv("PROM_ENDPOINT") != ""
	}
}
// New (this phase): phoenixBaseURLFromEnv() string { return os.Getenv("PHOENIX_BASE_URL") }
// Empty string IS the "no link" sentinel — no separate bool needed, unlike telemetryEnabled's
// three-state (true/false/legacy-fallback) logic, because there's no legacy chart to fall back for.
```

### Anti-Patterns to Avoid

- **Hand-rolling `openinference.project.name` to make the deep link "more correct":** Not needed. The `/redirects/traces/{trace_id}` route works against Phoenix's default project with zero additional resource attributes. Adding a project-name attribute is optional scope CONTEXT.md explicitly permits only "if a project-name resource attribute turns out to be required" — research found it is NOT required. Do not add it.
- **Building the Phoenix URL in more than one place:** D-11 already flags this. Concretely: do not compute `${baseURL}/redirects/traces/${traceId}` independently inside both `NodeDetailPanel.tsx` and `TaskDetailDrawer.tsx`. Put it in one `dashboard/web/src/lib/` helper (e.g. `phoenixLink.ts`) both components import.
- **Dropping token counts from ALL five levels' AGENT spans to "fix" double-counting:** Only Task has a sibling LLM-kind span source today. Dropping TokenCount from Milestone/Phase/Plan/Project would silently zero out Phoenix's ONLY token/cost signal at those levels — a regression, not a fix.
- **Forwarding `OTEL_TRACES_SAMPLER`/`_ARG` env vars to the reporter Job to "fix" D-02:** Unnecessary and does not fix the actual bug. The reporter's own `internal/otelinit` already defaults to `ParentBased(AlwaysSample())` when no sampler env is set (per `provider.go`'s doc comment); once the *incoming* `--traceparent` carries the correct sampled bit, `ParentBased`'s default `remoteParentNotSampled: NeverSample()` correctly propagates the drop without any new env wiring on the reporter side.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Phoenix trace/span URL construction | A bespoke URL scheme assuming project-name-in-path | `{phoenixBaseURL}/redirects/traces/{trace_id}` / `/redirects/spans/{span_id}` | Official, versioned (14.2.0+), stable, needs no project lookup — reinventing this risks breaking on a Phoenix internal-ID/project-name edge case that redirects were built specifically to avoid |
| `metadata`/`tag.tags` attribute keys | Hand-rolled string literals `"metadata"`/`"tag.tags"` | `semconv.Metadata`, `semconv.TagTags` (already-imported v0.1.1 module) | ATTR-03's existing policy — ANY new attribute key in this phase must resolve from the semconv module per `TestKeysUseSemconvModule`; these two already exist, so there is no reason to hand-roll |
| Cost double-count avoidance | A TIDE-side deduplication pass reading Phoenix's own stored costs back out | Don't emit the duplicate span-level signal in the first place (drop Task AGENT's TokenCount) | Phoenix's aggregation is a fixed external contract TIDE cannot special-case; the only lever TIDE has is which attributes it emits, so fix it at emission time, not by trying to post-process Phoenix's database |

**Key insight:** every one of this phase's four requirements is an attribute-emission or read-path change on top of infrastructure the prior five phases already built (span emission, propagation, message spans, adapter seam). The temptation is to treat OBS-02/03 as "just add attributes," but D-03's routed double-count decision means OBS-02 is actually a correctness fix disguised as an enrichment task — get the Task-level TokenCount removal right or the "independent cross-check" OBS-02 promises is actively misleading.

## Common Pitfalls

### Pitfall 1: D-09's "NodeDetailPanel is shared by all five node types" is false — Task uses `TaskDetailDrawer`

**What goes wrong:** Implementing the deep link only inside `NodeDetailPanel.tsx` (per CONTEXT.md D-09's literal text) silently omits Task nodes — the single most common node an operator clicks to ask "why did this fail," and the level where the deep link matters most (it's the only level with message-content spans behind it).

**Why it happens:** `NodeDetailPanel.tsx`'s own type is `PlanningNodeKind = "project" | "milestone" | "phase" | "plan"` — Task is not a member. Verified: `App.tsx` renders `<TaskDetailDrawer taskName={selectedTask} task={taskDetail} .../>` as a completely separate mounted component alongside `<NodeDetailPanel>`. `TaskDetailDrawer.tsx`'s own doc comment describes distinct chrome (slide-in from right, "Metadata grid" section with `podName`/`exitCode`/`waveIndex`/`envelopePath`) — it is not a specialization of `NodeDetailPanel`, it predates it and was never unified.

**How to avoid:** Treat this as a two-mount-point, one-logic-source task: extract the Phoenix-link URL assembly + rendering into a small shared component or hook (e.g. `<PhoenixTraceLink traceId spanId baseURL />`), then mount it in (a) `NodeDetailPanel`'s content branch for project/milestone/phase/plan (`App.tsx`'s `nodePanelContent` construction, ~line 616) and (b) `TaskDetailDrawer`'s metadata grid. This satisfies D-09's actual intent (no per-node-shell clutter, one link *implementation*) without the false premise of one render surface.

**Warning signs:** A plan that only touches `NodeDetailPanel.tsx` and its test file, with no `TaskDetailDrawer.tsx` diff, has silently dropped Task-level deep links.

### Pitfall 2: Emitting `session.id` before fixing the Task-level double-count makes the "independent cross-check" actively wrong

**What goes wrong:** OBS-02's stated purpose is a rollup an operator can trust as an independent check against TIDE's own budget tally. If shipped with the Task-level AGENT span still carrying `llm.token_count.*` alongside the reporter's per-call LLM spans, Phoenix's `ProjectSession.tokenUsage`/`costSummary` will read roughly 2x actual for any run where Tasks ran (which is every run) — an operator "catching" a discrepancy against TIDE's own tally would be chasing a phantom bug in the wrong system.

**Why it happens:** OBS-02 and OBS-03 read, on the surface, like independent enrichment tasks ("add session.id," "add metadata"). D-03 explicitly routes the double-count fix through OBS-02, but a plan that treats OBS-02 as "one attribute, done" will ship the session grouping without the TokenCount removal it depends on for correctness.

**How to avoid:** Sequence (or at minimum, land in the same wave/commit): (1) Task-level `synthesizePlannerSpan` call site drops `otelai.TokenCount(...)` when `level == "task"`, (2) `session.id` lands on all spans. Do not ship (2) without (1) — CONTEXT.md's decisions imply this ordering but a plan built from OBS-02's success-criterion text alone could miss it.

**Warning signs:** A wave/plan diff that adds `SessionID` to `span_emission.go`'s attribute block but does not touch the `if envReadOK { span.SetAttributes(otelai.TokenCount(...)) }` block for the task/executor role.

### Pitfall 3: D-02's fix only makes sense narrowly — don't over-scope it into a schema change

**What goes wrong:** A literal reading of D-02 ("thread the manager's real sampling decision into the traceparent flag") could be scoped as "make ALL five levels' sampled-flag propagation coherent," which requires a new persisted status field (the sampled bit does not survive a cross-controller/cross-reconcile read via the existing `{Level}TraceSpanID` hex-only status field) — a v1alpha3 schema addition across all 5 CRD types, disproportionate to a bug that is fully inert at OBS-01's new 1.0 default.

**Why it happens:** The bug has two layers that look similar but have very different fix costs: (a) the OUTBOUND reporter traceparent at `span_emission.go:246` (`traceparentForLevel`/`FormatTraceparent`) — fixable same-reconcile, zero schema change, because span emission and reporter spawn are sequential calls within the same reconciler function; (b) the INBOUND cross-level parent reconstruction inside `synthesizePlannerSpan` itself (`sc := trace.NewSpanContext(...)` ~line 155, hardcoding `TraceFlags: trace.FlagsSampled` when rebuilding a PARENT level's SpanContext from its persisted status hex) — this crosses reconciler/controller boundaries (e.g. Milestone's reconcile reads `project.Status.ProjectTraceSpanID`, set by a prior, separate Project reconcile) and cannot be fixed without persisting the sampled bit somewhere durable.

**How to avoid:** Scope the code fix to (a) only — thread the real `IsSampled()` bit from the just-emitted span into that SAME level's outbound reporter-spawn traceparent call (currently matters only at the Task level, since Task is the only level spawning a sibling reporter with LLM spans). For (b), write the honest limitation into `docs/observability.md`'s opt-down section instead of a code fix: at ratios below 1.0, only the Project-level root span is gated by the ratio sampler; once a run is sampled, every descendant level's AGENT spans export in full (the ratio controls run-level visibility, not per-span volume), and if the root is NOT sampled, descendant spans still export as an orphaned/rootless trace fragment in Phoenix.

**Warning signs:** A plan task titled "add TraceSampled status field" or touching all five `api/v1alpha3/*_types.go` files for this phase — that is the over-scoped version.

### Pitfall 4: `tag.tags` as a JSON string instead of a native OTel string-slice attribute

**What goes wrong:** Setting `tag.tags` via `attribute.String(semconv.TagTags, `["phase","conservative"]`)` (JSON-encoded, matching the `metadata` pattern) instead of `attribute.StringSlice(semconv.TagTags, []string{"phase", "conservative"})` renders as an opaque string in Phoenix's UI/filter DSL rather than a queryable tag list — CONTEXT.md's own "Also verify" bullet flags exactly this risk.

**Why it happens:** `metadata` and `tag.tags` look like siblings in the semconv module's doc comments, but the OpenInference spec documents them with genuinely different encodings: `metadata` is `"JSON String"` (e.g. `"{'author': 'John Doe'}"`), `tag.tags` is `"List of strings"` (e.g. `["shopping", "travel"]`) — a native array-valued attribute, which in Go SDK terms is `attribute.StringSlice`, not `attribute.String`.

**How to avoid:** `Metadata(map[string]string) attribute.KeyValue` should `json.Marshal` and return `attribute.String(semconv.Metadata, string(jsonBytes))`. `Tags([]string) attribute.KeyValue` should return `attribute.StringSlice(semconv.TagTags, tags)` directly — no JSON marshaling. Verified via `go.opentelemetry.io/otel@v1.43.0/attribute/kv.go`: `func StringSlice(k string, v []string) KeyValue` exists and is the standard OTel Go idiom for array-valued attributes.

**Warning signs:** A `Tags()` helper that calls `json.Marshal` on its input, or a unit test asserting the tag attribute's `.Type()` is `attribute.STRING` rather than `attribute.STRINGSLICE`.

### Pitfall 5: `Gates` has no Project-level field

**What goes wrong:** D-08's "gate profile = the Project's per-level `Gates` policy... for the emitting level" has no natural value when `level == "project"` — `Gates{Milestone, Phase, Plan, Task GatePolicy; PauseBetweenWaves bool}` (verified, `api/v1alpha3/project_types.go:40-49`) has no `Project` field, because gates apply to child-level dispatch approval, not the Project's own top-level execution.

**How to avoid:** Document the Project-level span's metadata as either omitting the gate-profile key entirely, or using a documented sentinel (e.g. `"n/a"` or `"root"`) — pick one and put it in the helper's doc comment (D-08 already requires the encoding be documented so Phoenix DSL queries are writable from docs alone). Don't let this null case silently become an empty-string attribute value, which reads in Phoenix as "gate profile: (blank)" rather than "not applicable."

## Code Examples

### Session ID helper (D-04)

```go
// Source: pattern matches existing pkg/otelai/attrs.go helpers (AgentInvocation, LLMIdentity)
// semconv.SessionID verified present: "session.id" (openinference-semantic-conventions v0.1.1)
func SessionID(projectUID string) attribute.KeyValue {
	return attribute.String(semconv.SessionID, projectUID)
}
```

### Metadata helper — JSON-encoded string (D-06, Pitfall 4)

```go
// Source: OpenInference spec (arize-ai.github.io/openinference/spec/semantic_conventions.html)
// — "metadata" documented as "JSON String", e.g. `"{'author': 'John Doe', 'date': '2023-09-09'}"`
func Metadata(m map[string]string) (attribute.KeyValue, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return attribute.KeyValue{}, err
	}
	return attribute.String(semconv.Metadata, string(b)), nil
}
```

### Tags helper — native string slice (D-06, Pitfall 4)

```go
// Source: OpenInference spec — "tag.tags" documented as "List of strings", e.g. ["shopping","travel"]
// attribute.StringSlice verified present: go.opentelemetry.io/otel@v1.43.0/attribute/kv.go
func Tags(tags ...string) attribute.KeyValue {
	return attribute.StringSlice(semconv.TagTags, tags)
}
```

### Phoenix deep-link URL assembly (D-11/D-12) — the ONE place

```ts
// Source: dashboard/web/src/lib/ (new file) — pattern per D-11 ("URL assembly logic must
// live in exactly one place"); route format per Phoenix docs (constructing-urls.mdx,
// release-notes/04-2026/04-10-2026-shareable-url-redirects.mdx, Phoenix >= 14.2.0)
export function phoenixTraceURL(baseURL: string, traceId: string): string {
  return `${baseURL.replace(/\/$/, "")}/redirects/traces/${traceId}`;
}

export function phoenixSpanURL(baseURL: string, spanId: string): string {
  return `${baseURL.replace(/\/$/, "")}/redirects/spans/${spanId}`;
}
```

### D-02 fix sketch — thread the real sampled bit (Task level, same-reconcile)

```go
// Source: internal/controller/span_emission.go + task_controller.go (existing shapes, modified)
// synthesizePlannerSpan already returns (trace.SpanID, bool); extend to also return the
// span's real IsSampled() bit so the SAME reconcile's reporter-spawn call can use it —
// no new persisted field needed for this narrow (Task-only) fix.
thisSpanID, sampled, emitted := synthesizePlannerSpan(ctx, "task", project, ..., parentSpanID)
// ...
traceParent := traceparentForLevel(project, task.Status.TaskTraceSpanID, sampled) // was: hardcoded true
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| Phoenix trace URLs via `/projects/{project}/traces/{trace_id}` (internal project ID/name in path) | `/redirects/traces/{trace_id}` + `/redirects/spans/{span_id}` (shareable, project-agnostic) | Phoenix ≥ 14.2.0 (release note dated 2026-04-10) | TIDE never needs to resolve or set a Phoenix project name for the deep link to work — simpler, more robust than the milestone research's original prior |

**Deprecated/outdated:** The milestone research's FEATURES.md deep-link URL prior (`{phoenix-url}/projects/{project}/traces/{trace_id}`) is superseded by the redirect-URL family above; it was written before (or without finding) the 14.2.0 shareable-URL feature.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The currently-deployed/documented Phoenix Helm chart's app version (last verified at 18.0.0 by the milestone's STACK.md research) is ≥ 14.2.0, so `/redirects/traces/{trace_id}` is available. Not re-verified against a live chart pull in this research session (Phase 47 owns the "re-fetch Chart.yaml fresh" obligation per STATE.md's blocker note). | D-12 / Standard Stack | If a much older Phoenix is deployed (pre-14.2.0), the redirect endpoints 404 and the deep link floor breaks. Low risk given the 18.0.0 app-version data point, but Phase 47's fresh chart-pin check should explicitly confirm ≥14.2.0 before this phase's deep link ships as documented-working. |
| A2 | Phoenix's `SpanCostCalculator.calculate_cost` has no `openinference.span.kind` gate — confirmed via Context7-served excerpts of the actual `span_cost_calculator.py`/`models.py` source and corroborated by a real, closed upstream bug (`Arize-ai/openinference#3164`) describing the identical double-count shape. The exact body of `SpanCostDetailsCalculator.calculate_details` (which attributes it reads to decide "has tokens") was not directly fetched — inferred from the calculator's stated required-attribute set (`llm.token_count.*`) in Phoenix's own cost-tracking docs. | D-03 / Summary | If `calculate_details` DOES gate on span.kind==LLM somewhere not surfaced by these excerpts, dropping Task-level AGENT TokenCount would be an unnecessary (but harmless) change — it would just remove a currently-inert duplicate. Low downside either way. |

**If this table is empty:** N/A — two assumptions logged above, both low-risk with a stated verification path.

## Open Questions

1. **Does Phoenix's session-level `tokenUsage`/`costSummary` GraphQL rollup (`ProjectSession`) use the exact same `SpanCost`-summed-by-`trace_rowid`-then-by-`session`-join path as the project-level `SpanCostSummaryByProjectDataLoader` shown in this research, or a separate session-specific aggregation query?**
   - What we know: the project-level aggregation SQL (verified, shown in Summary/Architecture) sums `SpanCost.total_tokens` grouped by `trace.project_rowid`, with an optional `session_filter_condition` narrowing by `project_session_rowid` — this strongly implies the session view is the SAME per-span-cost-row aggregation, just grouped/filtered by session instead of (or in addition to) project.
   - What's unclear: the exact GraphQL resolver backing `ProjectSession.costSummary`/`tokenUsage` wasn't independently fetched — only its field shape (`sessions.md`).
   - Recommendation: treat as HIGH confidence given the shared `SpanCost` table is the only cost-storage mechanism Phoenix has (verified via `models.py`); the fix (don't create a duplicate `SpanCost` row for the Task AGENT span) is correct regardless of which specific query reads that table.

2. **Is Phoenix's default project literally named `"default"`, and does that matter for anything in this phase?**
   - What we know: "If a project is not specified, all traces are sent to a default project" (Phoenix docs, verified) and TIDE sets no `openinference.project.name` resource attribute today.
   - What's unclear: exact literal string Phoenix uses for the default project name.
   - Recommendation: irrelevant to this phase's implementation — the `/redirects/traces/{trace_id}` route needs no project name at all. Worth a one-line footnote in `docs/observability.md` for an operator who goes looking for their traces by browsing Phoenix's project list rather than following TIDE's deep link.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Go framework | Go `testing` (table-driven), no Ginkgo needed for this phase's unit-level work (envtest/Ginkgo only if a controller-level integration test is added) |
| Frontend framework | Vitest (`dashboard/web/package.json` `"test": "vitest run"`) |
| Config file | `dashboard/web/vitest.config.ts` (existing) |
| Quick run command (Go) | `go test ./pkg/otelai/... ./internal/controller/... ./internal/reporter/... -run <TestName> -v` |
| Quick run command (frontend) | `cd dashboard/web && npx vitest run src/components/__tests__/node-panel-integration.test.tsx` |
| Full suite command | `make test` (Go unit tier) + `cd dashboard/web && npm test` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| OBS-01 | `values.yaml` default is `"1.0"`; docs/comment surfaces updated | unit / static grep | `helm template charts/tide \| grep -A1 OTEL_TRACES_SAMPLER_ARG` | ✅ chart renders today; assertion is new |
| OBS-02 | `SessionID` attribute present on every span; Task-level TokenCount dropped | unit (Go) | `go test ./pkg/otelai/... -run TestSessionID` / `go test ./internal/controller/... -run TestSynthesizePlannerSpan_TaskDropsTokenCount` | ❌ Wave 0 — new test names, existing `span_emission_test.go`/`attrs_test.go` files to extend |
| OBS-03 | `Metadata`/`Tags` helpers produce correctly-typed attributes (JSON string vs. string-slice) | unit (Go) | `go test ./pkg/otelai/... -run TestMetadata\|TestTags` | ❌ Wave 0 — extend `pkg/otelai/attrs_test.go` |
| OBS-04 | Dashboard renders/hides the Phoenix link based on config; URL assembly correct | unit (frontend) | `npx vitest run src/lib/__tests__/phoenixLink.test.ts` + extend `node-panel-integration.test.tsx` | ❌ Wave 0 — new `phoenixLink.ts`/test; existing node-panel-integration test file to extend |

### Sampling Rate
- **Per task commit:** the scoped `go test ./pkg/otelai/... ./internal/controller/...` / `npx vitest run <file>` for the touched package.
- **Per wave merge:** `make test` (Go unit tier) + `cd dashboard/web && npm test`.
- **Phase gate:** full `make test` + `npm test` green, plus a `helm template` render diff confirming `tracesSamplerArg: "1.0"` and (if `phoenix.baseURL` set) `PHOENIX_BASE_URL` present, before `/gsd:verify-work`.

### Wave 0 Gaps
- [ ] `pkg/otelai/attrs_test.go` — extend with `TestSessionID`, `TestMetadata` (asserts JSON-string encoding + `attribute.STRING` type), `TestTags` (asserts `attribute.STRINGSLICE` type — Pitfall 4's regression guard)
- [ ] `internal/controller/span_emission_test.go` — extend with a case asserting `level=="task"` omits `TokenCount` while `level=="milestone"/"phase"/"plan"/"project"` retain it (Pitfall 2's regression guard)
- [ ] `internal/reporter/tracesynth_test.go` — extend `EmitSpans` tests to assert the new session/metadata/tags CLI-sourced values land on emitted LLM spans
- [ ] `dashboard/web/src/lib/__tests__/phoenixLink.test.ts` — new file, asserts `phoenixTraceURL`/`phoenixSpanURL` shape + trailing-slash normalization
- [ ] `dashboard/web/src/components/__tests__/node-panel-integration.test.tsx` — extend for the NodeDetailPanel link render/hide-on-empty-config cases
- [ ] A new/extended `TaskDetailDrawer.test.tsx` (or equivalent) covering the same link render/hide cases for Task nodes — **required per Pitfall 1's correction; absent from CONTEXT.md's file list**

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | This phase adds no auth surface |
| V3 Session Management | No | OpenInference `session.id` is an observability/grouping concept, not an auth session — no token/cookie handling involved |
| V4 Access Control | No | `phoenixBaseURL` is a read-only config value exposed via the existing `/api/v1/config` GET-only surface (DASH-05 zero-mutation contract, unchanged) |
| V5 Input Validation | Yes | `phoenix.baseURL` chart value is operator-supplied and rendered into an `<a href>` — must be treated as untrusted-ish config, not user input from the browser |
| V6 Cryptography | No | No new crypto surface |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| `phoenixBaseURL` used to construct a link an operator clicks — a misconfigured or malicious value could point to a lookalike/phishing domain | Spoofing | This is an operator-controlled Helm value (same trust tier as `otel.exporter.endpoint`, already operator-controlled) — no new trust boundary crossed. Standard mitigation: render as a visible external-link affordance (not disguised text) so the destination domain is inspectable before clicking, matching the existing `ExternalLink`-icon convention other outbound links in the dashboard likely already use. No sanitization beyond standard React JSX auto-escaping is needed since the value never becomes `dangerouslySetInnerHTML` or is `eval`'d. |
| Trace/span ID values reflected into a URL | Tampering / Injection | Trace/span IDs are server-derived (`otelai.TraceIDFromUID`, `{Level}TraceSpanID` status fields) — never raw user input — but the URL-assembly helper should still use `encodeURIComponent` on any interpolated segment defensively, since hex IDs are safe today but the helper shouldn't assume that forever. |
| `--session-id`/`--metadata`/`--tags` CLI args on the reporter Job | Tampering | Already covered by D-05's trust posture: these are manager-authored Job-spec args (immutable once the Job is created), not pod-writable PVC data — no new attack surface versus the existing `--traceparent` precedent. |

## Sources

### Primary (HIGH confidence)
- `go.opentelemetry.io/otel/sdk@v1.43.0/trace/{sampling.go,sampler_env.go}` — read directly from the local Go module cache (`go list -m -f '{{.Dir}}'`); confirms `ParentBased` default option behavior and `samplerFromEnv()` parsing of `OTEL_TRACES_SAMPLER(_ARG)`
- `go.opentelemetry.io/otel@v1.43.0/attribute/kv.go` — confirms `attribute.StringSlice` exists
- `github.com/Arize-ai/openinference/go/openinference-semantic-conventions@v0.1.1/attributes.go` + `semconv_test.go` — confirms `SessionID`/`Metadata`/`TagTags` const values, read directly from the module cache
- Context7 `/arize-ai/phoenix` — `src/phoenix/server/daemons/span_cost_calculator.py`, `src/phoenix/db/models.py`, `src/phoenix/server/api/dataloaders/span_cost_summary_by_project.py` (Phoenix's own source, served via Context7's GitHub-indexed docs) — cost-computation and aggregation mechanics
- Context7 `/arize-ai/phoenix` + WebFetch `docs/phoenix/tracing/how-to-tracing/advanced/constructing-urls.mdx` + `docs/phoenix/release-notes/04-2026/04-10-2026-shareable-url-redirects.mdx` — exact redirect-URL route formats, Phoenix ≥14.2.0
- Context7 `/arize-ai/phoenix` — `docs/phoenix/tracing/concepts-tracing/otel-openinference/resource.mdx`, `docs/phoenix/tracing/how-to-tracing/setup-tracing/setup-projects.mdx` — `openinference.project.name` resource attribute semantics, default-project behavior
- `Arize-ai/openinference#3164` (GitHub issue, fetched via WebFetch) — real-world confirmation of the exact AGENT+child-LLM double-count shape and the maintainer-endorsed fix pattern (drop token counts from wrapper spans)
- TIDE source (direct read, this session): `internal/controller/span_emission.go`, `internal/controller/reporter_jobspec.go`, `internal/controller/dispatch_helpers.go`, `internal/controller/{milestone,phase,plan,project,task}_controller.go`, `internal/reporter/tracesynth.go`, `cmd/tide-reporter/main.go`, `pkg/otelai/{attrs.go,tracecontext.go}`, `internal/otelinit/{doc.go,provider.go}`, `api/v1alpha3/{project,milestone,phase,plan,task}_types.go`, `api/v1alpha3/shared_types.go`, `cmd/dashboard/{main.go,router.go}`, `cmd/dashboard/api/{config,projects,plans,tasks,waves,execution_dag}.go`, `dashboard/web/src/components/{NodeDetailPanel,NodeClickContext,TaskDetailDrawer}.tsx`, `dashboard/web/src/App.tsx`, `charts/tide/values.yaml`, `charts/tide/templates/{deployment,dashboard-deployment}.yaml`, `docs/observability.md`

### Secondary (MEDIUM confidence)
- `.planning/research/{SUMMARY,FEATURES,PITFALLS,ARCHITECTURE,STACK}.md` (prior milestone research) — used as a baseline/prior, cross-checked against fresh evidence above; the deep-link URL row was found superseded (see State of the Art)

### Tertiary (LOW confidence)
- None — every claim above traces to either vendored source code or official Phoenix/OpenInference documentation.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies, both existing pinned packages verified present via `go list -m`
- Architecture: HIGH — every call site cited was read directly from TIDE's current source this session
- Pitfalls: HIGH — D-02/D-03 mechanisms verified against pinned SDK source and Phoenix's own source/issue tracker, not inferred from documentation prose alone

**Research date:** 2026-07-17
**Valid until:** 30 days (stable — this phase touches no fast-moving external surface except the Phoenix chart version pin, which Phase 47 explicitly re-verifies immediately before use)
