# Phase 46: Observability Enrichment + Dashboard Deep Link - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-17
**Phase:** 46-observability-enrichment-dashboard-deep-link
**Mode:** `--auto` — all gray areas auto-selected; each question resolved to the recommended option without user prompts. Every selection logged below for audit.
**Areas discussed:** Phoenix cost double-count resolution (OBS-02), Reporter data transport, Metadata vs tag.tags split (OBS-03), Deep-link placement + URL construction (OBS-04), Sampler-flip blast radius (OBS-01)

---

## Phoenix cost double-count resolution (OBS-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Lock the invariant, research pins the mechanism | Lock "Phoenix session/trace cost views must not double-count"; research verifies Phoenix's aggregation semantics (LLM-kind-only vs all `llm.token_count.*` spans) before any counts move; if double-count confirmed, per-call LLM counts win (LangGraph-native shape) | ✓ |
| Preemptively strip AGENT-span totals | Remove `TokenCount` totals from the five dispatch spans now | |
| Preemptively strip per-call LLM counts | Remove per-call counts from message spans, keep AGENT totals | |

**Auto-selected:** Lock invariant + research-verified mechanism (recommended default).
**Notes:** `[auto]` Phase 44 D-04 explicitly deferred this here ("research pins which level keeps counts"); PROJECT.md routes "Phoenix cost double-count risk" to OBS-02. Preemptive stripping in either direction risks acting on an unverified assumption about Phoenix's rollup semantics — evidence first. Runtime-neutrality (PROJECT.md lock) dictates the tiebreak: per-call LLM counts are what `openinference-instrumentation-langchain` emits natively.

---

## Reporter data transport (session.id + enrichment values)

| Option | Description | Selected |
|--------|-------------|----------|
| ReporterOptions fields → CLI args | Manager-authored Job spec carries the values (TraceParent / SkipMessageSpans precedent; tamper-resistant channel) | ✓ |
| Read from `in.json` on the PVC | Reporter derives values from envelope data already on disk | |

**Auto-selected:** ReporterOptions → Args (recommended default).
**Notes:** `[auto]` Phase 45 D-02 locked the trust posture: the PVC is pod-writable by the semi-trusted subagent; the Job spec is the manager-controlled channel. Also the `in.json` route couldn't carry gate profile / failure-halt state anyway (not envelope data).

---

## Metadata vs tag.tags split (OBS-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Metadata = full set; tags = low-cardinality filterables | `metadata` JSON map carries level kind+name, wave index, gate profile, failure-halt; `tag.tags` carries only categorical one-click filters (level kind, gate profile, failure-halt) | ✓ |
| Everything in tags | All enrichment values as tags | |
| Everything in metadata | No tags at all | |

**Auto-selected:** Split by cardinality (recommended default).
**Notes:** `[auto]` Tags are Phoenix's one-click filter surface — unbounded values (wave index, level names) would pollute it; the metadata map keeps everything DSL-queryable. Wave index sourced from the `tideproject.k8s/wave-index` Task label (verified stamped by project controller, EXEC-03); planner levels omit it unless free.

---

## Deep-link placement + URL construction (OBS-04)

| Option | Description | Selected |
|--------|-------------|----------|
| NodeDetailPanel link + config-chain precedent + research-pinned URL | Link renders in NodeDetailPanel only; `phoenix.baseURL` chart value rides the `telemetryEnabled` config chain (`PHOENIX_BASE_URL` env → `/api/v1/config`); Phoenix URL route/project-segment/span-anchoring pinned by research; trace-root link is the floor | ✓ |
| Link button on every DAG node shell | Per-node inline affordances on all five node types | |
| Server-rendered URL only, no SPA config | Dashboard server assembles full URLs into every payload | |

**Auto-selected:** NodeDetailPanel + telemetryEnabled-precedent config chain (recommended default).
**Notes:** `[auto]` The panel is the shared click-through surface for all five node types — one implementation point, no dead buttons (mirrors the Telemetry view's disabled-by-config state). Trace identity must be added to the dashboard API (zero `TraceSpanID` refs in `cmd/dashboard/` today — verified). URL-assembly location (server vs SPA) left to planner, single place only.

---

## Sampler-flip blast radius (OBS-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Chart flip + deliberate sweep + sampled-flag coherence check | `tracesSamplerArg` "0.1"→"1.0"; planner sweeps every surface pinning "0.1" (helm contract tests, comments, docs); research verifies the hardcoded `FormatTraceparent(..., true)` sampled flag vs opt-down honesty | ✓ |
| Chart flip only | Change values.yaml and stop | |

**Auto-selected:** Flip + sweep + coherence check (recommended default).
**Notes:** `[auto]` `span_emission.go:246` hardcodes sampled=true in the reporter's traceparent — reporter LLM spans ignore the ratio sampler entirely. Invisible at 1.0, real at opt-down (orphaned LLM spans under dropped parents). The opt-down doc must not imply coherent ratio sampling if the flag stays hardcoded.

---

## Claude's Discretion

- New `pkg/otelai` helper names/shapes and metadata JSON key names.
- `ReporterOptions` field/arg names for the new transport values.
- Whether planner-level spans carry wave index ("only if free" rule).
- NodeDetailPanel link styling; `phoenixBaseURL` trailing-slash normalization location (one place only).
- Whether the sampled-flag fix is a code change or documented limitation, per research's finding.

## Deferred Ideas

- Data-minimization toggle (`otel.redactMessageContent` / ArtifactPath-only mode) — research's fast-follow, not v1.0.8.
- Span-level deep-link anchoring if Phoenix's URL contract doesn't support it — trace-root link is the floor.
- Node-shell link affordances (icons on DAG nodes) — panel-only this phase.

## Todos (auto-reviewed)

`[auto]` `todo.match-phase 46` returned the same four keyword matches as Phases 42–45 (all score 0.6). Dispositions locked in those phases carry forward unchanged — none folded (git-identity/GPG, two W-2 dispatch-gate candidates, CACHE-F1): no enrichment or dashboard-deep-link overlap. Recorded in CONTEXT.md `<deferred>`.
