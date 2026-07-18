# Phase 46: Observability Enrichment + Dashboard Deep Link - Context

**Gathered:** 2026-07-17 (--auto mode: all gray areas auto-resolved to the research-recommended option; every selection logged in 46-DISCUSSION-LOG.md)
**Status:** Ready for planning

<domain>
## Phase Boundary

Make the now-complete five-level trace tree operator-useful inside Phoenix and inside TIDE's own dashboard:

1. **OBS-01 (sampler default):** the chart's trace-sampler default flips 0.1 → 1.0 (`charts/tide/values.yaml:414`), with the opt-down for high-volume installs documented — under `parentbased_traceidratio`, TIDE's one-root-trace-per-run shape makes 0.1 a per-*run* coin flip (Pitfall 6), so a demo run had a 90% chance of an empty Phoenix.
2. **OBS-02 (session identity):** every span — the five AGENT dispatch spans AND the reporter's LLM message-array spans — carries `session.id` = Project UID, so Phoenix's session view computes an independent per-run token/cost rollup an operator can cross-check against TIDE's budget tally. This requirement also owns the **Phoenix cost double-count risk Phase 44 explicitly routed here** (see D-03).
3. **OBS-03 (filterable enrichment):** spans carry `metadata` / `tag.tags` enrichment (level kind + name, wave index, gate profile, failure-halt state) so an operator can filter Phoenix's DSL by "every span from Phase N" or "every conservative-profile run" without leaving Phoenix.
4. **OBS-04 (dashboard deep link):** each Planning/Execution DAG node in TIDE's dashboard deep-links to its Phoenix trace, reading per-level span IDs from the `{Level}TraceSpanID` status fields (PROP-02, verified present on all five `api/v1alpha3/*_types.go`) plus the deterministic TraceID, gated on a new `phoenix.baseURL` chart value — absent config renders no link (no dead buttons).

**Requirements:** OBS-01..04 (ROADMAP.md Phase 46 section, 4 success criteria). Depends on Phase 43 (PROP-02 status fields) and Phase 44 (both span families exist). ROADMAP `UI hint: yes` — OBS-04 has a real (small) frontend surface.

**Explicitly NOT this phase:** Phase 47's Phoenix install docs + live proof (PHX-01/02, PROOF-01); any new span family or change to Phase 44's redaction/size bounds (D-01..D-12 stand as shipped); a bespoke trace-viewer UI in TIDE's dashboard (REQUIREMENTS.md Out of Scope — link out, don't rebuild); OTel Collector / tail sampling (Out of Scope); metrics migration (Prometheus stays the metrics train).

</domain>

<decisions>
## Implementation Decisions

### OBS-01 — sampler default flip
- **D-01:** `values.yaml` `otel.tracesSamplerArg: "0.1"` → `"1.0"`; sampler stays **env-driven** (`OTEL_TRACES_SAMPLER(_ARG)` on the manager + dashboard Deployments) — never `WithSampler(...)` in code; `internal/otelinit`'s `TestNoWithSamplerInSource` guard (Pitfall 24 in `otelinit/doc.go`) is untouched. The opt-down for high-volume installs is documented at both the `values.yaml` comment block (lines ~407–414) and `docs/observability.md`. Planner must find and **deliberately update** every surface pinning `"0.1"` (helm-template contract tests, chart comments, docs) — a silent leftover reading "defaults to 10%" is a doc bug.
- **D-02 (opt-down honesty — sampled-flag coherence):** `span_emission.go:246` builds the reporter's traceparent via `FormatTraceparent(traceID, spanID, true)` — the sampled flag is **hardcoded true**, so reporter-side LLM spans follow the parent flag and always emit, even if the manager's own ratio sampler dropped the AGENT span. Invisible at the new 1.0 default; real at opt-down ratios (orphaned LLM spans under dropped parents). Research/planner verifies the actual semantics and either threads the manager's real sampling decision into the traceparent flag or documents the behavior honestly in the opt-down text. Do not ship an opt-down doc that implies coherent ratio sampling if the flag stays hardcoded.

### OBS-02 — session.id + the routed double-count decision
- **D-03 (locked invariant, research pins the mechanism):** Today AGENT spans carry rolled-up `TokenCount` totals (`span_emission.go:194`, Phase 42 D-08 pre-sum semantics) AND LLM message spans carry per-call `TokenCount` (`tracesynth.go` EmitSpans) — turning on `session.id` makes Phoenix's session cost rollup sum across all of them. **Locked invariant: Phoenix's session/trace cost views must not double-count a run** — otherwise OBS-02's "independent cross-check against TIDE's budget tally" is worse than useless (a wrong number that looks authoritative). Research must pin Phoenix's actual aggregation semantics (does its cost/token rollup sum only `openinference.span.kind=LLM` spans, or every span carrying `llm.token_count.*`?). Decision rule if double-counting is confirmed: **per-call LLM counts are authoritative** (they are what `openinference-instrumentation-langchain` emits natively — runtime-neutrality survival), and the AGENT-level totals are adjusted/dropped per the research recommendation. If Phoenix only sums LLM-kind spans, no change is needed — document the verified finding either way.
- **D-04:** `session.id` = `string(project.UID)` on **every** span, emitted via a new `pkg/otelai` helper backed by the semconv module's `SessionID` const (verified present in `openinference-semantic-conventions@v0.1.1` alongside `Metadata` and `TagTags`) — ATTR-03 pattern, no hand-rolled key strings. Manager-side sites (the 5 `synthesizePlannerSpan`/Task emission call sites) have `project` in hand already. Do NOT conflate with the `session_id` field inside `events.jsonl` (that is Claude Code's own session identifier, not TIDE's run identity).
- **D-05 (reporter transport):** session.id and the OBS-03 enrichment values reach the reporter as **manager-authored `ReporterOptions` fields → CLI args** (the `TraceParent` / `SkipMessageSpans` precedent — `reporter_jobspec.go`'s 100% Args-based convention). Never derived from pod-writable PVC data (`in.json`) — Phase 45 D-02's trust posture applies identically: the Job spec is the tamper-resistant channel.

### OBS-03 — metadata / tag.tags split
- **D-06:** `metadata` (JSON-encoded map) carries the full structured set: level kind, level name, wave index (where meaningful), gate profile, failure-halt state. `tag.tags` (list) carries only the **low-cardinality categorical filterables**: level kind, gate profile, failure-halt marker — the one-click Phoenix filter surface. Wave index goes metadata-only (unbounded cardinality doesn't belong in tags). Both keys backed by the semconv module consts; TIDE-specific values keep plain names inside the metadata map (the `tide.*` namespace convention from Phase 42 governs top-level custom span attributes, not keys inside the metadata JSON).
- **D-07 (wave index availability — verified):** Task CRs carry the `tideproject.k8s/wave-index` label (stamped exclusively by the project controller's global-wave materialization, EXEC-03 — see `project_controller.go:2456`). Task spans read it from the label; planner-level spans omit wave index unless the planner finds it already in hand at the emission site — do not add new derivation machinery for a metadata nicety.
- **D-08:** Gate profile = the Project's per-level `Gates` policy (`auto|approve|pause`, `project_types.go:34-48`) for the emitting level; failure-halt state = the project-wide FailureHalt condition presence (plus `Spec.FailureProfile` as the profile value). Exact encoding (strings in metadata/tags) is planner's discretion — values must be stable, lowercase, and documented in the helper's doc comment so Phoenix DSL queries are writable from docs alone.

### OBS-04 — dashboard deep link
- **D-09 (placement):** The deep link renders in **NodeDetailPanel** (the existing click-through surface all five node types share) — one implementation point, no per-node clutter, and the panel is where an operator already is when asking "why did this node do that?". No link affordance on the node shells themselves this phase.
- **D-10 (config plumbing — the telemetryEnabled precedent, applied verbatim):** new chart value `phoenix.baseURL` (default `""`) → `PHOENIX_BASE_URL` env on `dashboard-deployment.yaml` → `cmd/dashboard/main.go` resolves it → `ConfigHandler` → `GET /api/v1/config` gains `phoenixBaseURL` beside the locked `telemetryEnabled` wire contract (`cmd/dashboard/api/config.go`). Empty/absent → the SPA renders no link at all (requirement letter: no dead buttons — mirror the Telemetry view's disabled-by-config handling).
- **D-11 (trace identity to the SPA):** the dashboard API payloads must carry per-level trace identity to the SPA — **zero `TraceSpanID` references exist in `cmd/dashboard/` today** (verified). TraceID is deterministic (`otelai.TraceIDFromUID(project.UID)` — the package is K8s-import-free, the dashboard server may import it) and span IDs come from the five `{Level}TraceSpanID` status fields. Whether the server sends a pre-built URL, or trace-ID + span-ID parts the SPA assembles, is planner's discretion — but URL assembly logic must live in exactly one place.
- **D-12 (URL shape — research pins it):** research prior is `{phoenixBaseURL}/projects/{project}/traces/{trace_id}` (FEATURES.md differentiator row); the ROADMAP's `{phoenixBaseURL}/…/{trace_id}` ellipsis acknowledges the middle segment is unverified. Research must pin against current Phoenix docs: the exact trace-URL route, what the `{project}` segment is (Phoenix project *name* vs internal ID — TIDE sets no OpenInference project-name resource attribute today, so spans land in Phoenix's **default** project), and whether span-level anchoring (e.g. a `selectedSpanNodeId` query param) works with raw OTel span-ID hex. **Floor: link to the trace root; span-level anchoring is an enhancement if Phoenix's URL contract supports it.** If a project-name resource attribute turns out to be required for a stable URL, adding it is in scope (small, documented) — but prefer the zero-new-config default-project path if it works.

### Claude's Discretion
- Names/shapes of the new `pkg/otelai` helpers (e.g. `SessionID(uid)`, `Metadata(map)`, `Tags(...)`) and exact metadata JSON key names.
- `ReporterOptions` field/arg names for the new transport values (D-05), matching the existing Args conventions.
- Whether planner-level spans carry wave index (D-07's "only if free" rule).
- NodeDetailPanel link styling/labeling (external-link affordance), and where `phoenixBaseURL` trailing-slash normalization happens (server or SPA) — one place only.
- Whether the sampled-flag fix (D-02) is a this-phase code change or a documented limitation, per research's finding.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.0.8 Phoenix Rising)
- `.planning/research/SUMMARY.md` — key-decision 5 (sampler = per-run coin flip; the OBS-01 basis); differentiator list lines ~40-42 (deep link, session.id ↔ Phoenix `ProjectSession`, metadata/tags for the filter DSL)
- `.planning/research/PITFALLS.md` — **Pitfall 6 (parentbased_traceidratio @ 0.1 is a per-run coin flip — OBS-01's exact rationale)**; Pitfall 3 (span idempotency — enrichment attributes ride existing marker-gated emission, no new emission sites)
- `.planning/research/FEATURES.md` — deep-link row (URL-shape prior `{phoenix-url}/projects/{project}/traces/{trace_id}`, the "16 bytes in `.status` is fine" sizing note); session.id row (Phoenix session GraphQL computes `tokenUsage`/`costSummary` rollups free — the OBS-02 cross-check mechanism); line ~96 (Phoenix cost display silently fails without the ATTR attributes — shipped in Phase 42)
- `.planning/research/ARCHITECTURE.md` — component-table row for `reporter_jobspec.go` (research anticipated `OTEL_TRACES_SAMPLER(_ARG)` forwarding to reporter Jobs — verify what Phase 44 actually shipped vs this; only OTLP endpoint + batch-size env exist today)

### Requirements and constraints
- `.planning/REQUIREMENTS.md` §"Observability Add-Ons (OBS)" — OBS-01..04 exact text; §Out of Scope (no bespoke trace viewer, no collector, no subchart)
- `.planning/ROADMAP.md` §"Phase 46: Observability Enrichment + Dashboard Deep Link" — goal, depends-on, 4 success criteria, UI hint
- `.planning/PROJECT.md` §"Runtime-neutrality constraints (2026-07-15)" — conventions must match what `openinference-instrumentation-langchain` emits natively (governs D-03's "per-call counts are authoritative" rule); §Current focus — "Phoenix cost double-count risk routed to Phase 46 (OBS-02)"
- `.planning/STATE.md` §"v1.0.8 binding constraints" — observability-never-gates posture; keep-CRDs-small rule (status fields already shipped in Phase 43)

### Prior phase context (decisions this phase composes with)
- `.planning/phases/44-llm-message-array-spans-d-o5-redaction-size-boundary/44-CONTEXT.md` — D-04 (per-call token counts + the explicit "research pins which level keeps counts" flag this phase now resolves); D-10 (exit-0 posture — enrichment must not add a failure class)
- `.planning/phases/43-task-level-parity-trace-context-propagation/43-CONTEXT.md` — D-03/D-04 (the five `{Level}TraceSpanID` fields OBS-04 reads; separate from `{Level}SpanEmittedUID` markers)
- `.planning/phases/45-runtime-neutral-adapter-seam/45-CONTEXT.md` — D-02 (trust posture: manager-authored Job spec, never pod-writable PVC data — D-05 extends it); D-04 (Args-based ReporterOptions convention)
- `.planning/phases/42-trace-context-foundation-planner-level-span-emission/42-CONTEXT.md` — ATTR-03 semconv-module-backed keys policy (D-04/D-06 extend it); `tide.*` custom-attribute namespace

### Existing code (surfaces this phase touches)
- `charts/tide/values.yaml:407-414` — `otel.tracesSampler` / `tracesSamplerArg: "0.1"` (the OBS-01 flip site) + the comment block documenting the default
- `charts/tide/templates/deployment.yaml:86-94` + `charts/tide/templates/dashboard-deployment.yaml:48-54` — the env-driven sampler/OTLP wiring pattern; dashboard-deployment gains `PHOENIX_BASE_URL`
- `internal/otelinit/doc.go` + `provider_test.go` (`TestNoWithSamplerInSource`) — the Pitfall 24 env-driven-sampler guard that must stay intact
- `internal/controller/span_emission.go` — `synthesizePlannerSpan` (line 136; attribute block at 188-208 gains session.id/metadata/tags); `FormatTraceparent(..., true)` at line 246 (the D-02 sampled-flag site)
- `internal/reporter/tracesynth.go` — `EmitSpans` (line 587; per-call `TokenCount` + the attribute block gaining session.id/metadata/tags via the D-05 transport)
- `internal/controller/reporter_jobspec.go` — `ReporterOptions` (line 74: `TraceParent`/`OTLPEndpoint`/`TraceOnly`/`SkipMessageSpans` precedents) + `BuildReporterJob` arg assembly
- `internal/controller/task_controller.go` — Task span-emission site (session.id/metadata/tags at the fifth level; reads the wave-index label)
- `internal/controller/project_controller.go:2450-2530` — global-wave materialization stamping `tideproject.k8s/wave-index` on Tasks (D-07's data source)
- `api/v1alpha3/{project,milestone,phase,plan,task}_types.go` — `{Level}TraceSpanID` status fields (all five, verified); `Gates`/`GatePolicy` (`project_types.go:34-48`); `FailureProfile` (`project_types.go:441-450`)
- `pkg/otelai/attrs.go` + `doc.go` — the helper surface gaining SessionID/Metadata/Tags helpers (semconv module import already present); `pkg/otelai/tracecontext.go` — `TraceIDFromUID` (K8s-free; the dashboard server's TraceID source)
- `cmd/dashboard/api/config.go` — `ConfigHandler` / the `telemetryEnabled` locked wire contract `phoenixBaseURL` lands beside; `cmd/dashboard/main.go:166-258` — env-resolution pattern (`PROM_ENDPOINT`/`PROMETHEUS_ENABLED`)
- `cmd/dashboard/api/{projects,plans,tasks,waves,execution_dag}.go` — the API payloads gaining trace identity (zero TraceSpanID refs today)
- `dashboard/web/src/components/NodeDetailPanel.tsx` (+ `NodeClickContext.tsx`, the five `*Node.tsx`, `dashboard/web/src/lib/`) — the D-09 render surface and the SPA config consumption path
- `docs/observability.md` — the opt-down documentation surface (OBS-01) and where the deep-link/config value gets operator docs
- Semconv module (pinned): `github.com/Arize-ai/openinference/go/openinference-semantic-conventions v0.1.1` — `SessionID` = `"session.id"`, `Metadata` = `"metadata"`, `TagTags` = `"tag.tags"` (all verified present in `attributes.go`)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The `telemetryEnabled` config chain (chart env → `main.go` resolution → `ConfigHandler` → SPA disabled-state) is the exact, already-tested template for `phoenixBaseURL` — including the "explicit false/empty wins" semantics OBS-04's no-dead-buttons criterion needs.
- `ReporterOptions` Args convention + the Phase 45 `SkipMessageSpans` wiring show precisely how a new manager-computed value rides to the reporter at all 5 spawn sites.
- `pkg/otelai` already imports the semconv module (ATTR-03) — the three new keys are consts away; helpers follow the existing `AgentInvocation`/`LLMIdentity` shapes.
- `otelai.TraceIDFromUID` is deliberately K8s-free — the dashboard server can compute the deterministic TraceID without touching status.
- `dashboard/web/src/components/__tests__/` (nodes, node-panel-integration, dag-views) — established SPA test conventions for the NodeDetailPanel change.

### Established Patterns
- **Env-driven sampler, never WithSampler** (Pitfall 24 guard test) — OBS-01 is a chart-values + docs change, zero Go sampler code.
- **Marker-gated at-most-once span emission** — enrichment attributes ride the existing emission sites; this phase adds no new emission sites and must not disturb the idempotency mechanics.
- **Observability never gates** (Phase 42 D-04 / 44 D-10) — new attributes and the deep link must not introduce failure classes; a missing wave-index label or empty baseURL degrades to absent-attribute/no-link, never an error.
- **Locked wire contracts on `/api/v1/config`** — additive JSON fields only; `telemetryEnabled` semantics untouched.

### Integration Points
- `span_emission.go` attribute block + `tracesynth.go` EmitSpans — the two span families gaining identical session/metadata/tag enrichment (one helper set, two call sites).
- `ReporterOptions` → `BuildReporterJob` → reporter flag parse — the D-05 transport for reporter-side attribute values.
- `values.yaml` → both Deployment templates (sampler flip + new PHOENIX_BASE_URL env) → helm-template contract tests in `test/integration/kind/`.
- Dashboard: API payload structs → SSE/REST handlers → SPA lib → NodeDetailPanel.

</code_context>

<specifics>
## Specific Ideas

No user-specific vision requests — this phase was auto-discussed (`--auto`); all five gray areas resolved to research-recommended options. Two framing notes for the planner:

- **OBS-02 is not "add one attribute."** Its real content is the double-count resolution Phase 44 explicitly routed here (PROJECT.md: "Phoenix cost double-count risk routed to Phase 46 (OBS-02)"). The session view is the surface that makes double-counting visible; D-03's invariant + research verification is the work.
- **OBS-04's blast radius is mostly plumbing, not UI.** The visible change is one link in NodeDetailPanel; the work is carrying trace identity through the dashboard API (currently absent) and the config chain. Treat the ROADMAP `UI hint: yes` accordingly — a full UI design contract is likely overkill for one panel row, but follow the house SPA test conventions.

</specifics>

<deferred>
## Deferred Ideas

- **Data-minimization toggle** (`otel.redactMessageContent` / Project-level ArtifactPath-only mode) — research recommended as a fast-follow, not v1.0.8 scope; the milestone commits to full message arrays as default.
- **Span-level deep-link anchoring** if research finds Phoenix's URL contract doesn't support it cleanly — trace-root linking is the requirement floor; revisit when Phoenix's URL surface stabilizes.
- **Node-shell link affordances** (link icons directly on DAG nodes) — NodeDetailPanel is the surface this phase; per-node affordances only if operators ask.

### Reviewed Todos (not folded)
Same four keyword matches as Phases 42–45, carried forward with the dispositions locked there (reviewed 2026-07-15/16; reasoning applies identically — no enrichment/dashboard-deep-link overlap):
- `2026-07-03-signed-commits-verified-badge.md` — git-identity/GPG scope, keyword false-positive.
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` — W-2 dispatch-gate ordering concern, next-milestone candidate. (OBS-03's failure-halt *attribute* reads the condition; the gate-ordering defect is untouched.)
- `2026-07-12-task-dispatch-gate-order-divergence.md` — W-2 sibling, same disposition.
- `cache-f1-direct-sdk-cross-pod-caching.md` — deferred vNext+, no overlap.

</deferred>

---

*Phase: 46-Observability Enrichment + Dashboard Deep Link*
*Context gathered: 2026-07-17*
