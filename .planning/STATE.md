---
gsd_state_version: 1.0
milestone: v1.0.8
milestone_name: Phoenix Rising — OpenInference Trace Emission + Self-Hosted Phoenix
status: planning
last_updated: "2026-07-15T00:00:00.000Z"
last_activity: 2026-07-15
progress:
  total_phases: 6
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-15)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 42 — Trace-Context Foundation + Planner-Level Span Emission (v1.0.8 roadmap just created; ready to plan)

## Current Position

Phase: 42 of 47 (Trace-Context Foundation + Planner-Level Span Emission)
Plan: — (not yet planned)
Status: Ready to plan
Last activity: 2026-07-15 — ROADMAP.md created for v1.0.8 (Phases 42–47, 19 requirements, 100% mapped)

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity (recent milestones):**

- v1.0.7: 51 plans across 8 phases in ~12 days (2026-07-03 → 2026-07-15)
- v1.0.6: 8 plans across 3 phases in ~2 days (2026-06-28 → 2026-06-29)
- v1.0.5: 3 plans, 1 phase (2026-06-27)
- Total plans completed v1.0.0–v1.0.7: ~350+

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

**v1.0.8 binding constraints (from research + requirements + runtime-neutrality lock):**

- Retroactive span synthesis only — spans are created and closed in the same `handleJobCompletion` call using `completedJob.Status.{StartTime,CompletionTime}`, never held open across a `Reconcile()` return.
- Deterministic TraceID from `Project.UID`; span IDs mint fresh, after the fact, at each level's completion — no custom `IDGenerator`.
- D-O5 payload-boundary decision (message-array inline-vs-`ArtifactPath`) is a real open design call, not a rubber stamp — resolved in Phase 44 with an explicit byte threshold, not a binary always-inline choice.
- `events.jsonl` is deliberately unredacted at the source; the reporter's redaction pass (`internal/harness/redact.SecretPatterns`) is mandatory before any message content reaches a span (MSG-02).
- Span creation has no natural idempotency (unlike Job `Create`) — gate span creation off the same state-transition edges that already gate Job creation, not in-memory "did I already do this" checks.
- Phoenix is a separate `helm install`, never a TIDE chart subchart or bundled manifest (documented-install posture, TELEM-01 precedent) — no version coupling to Arize's near-daily chart releases.
- Runtime-neutrality (locked 2026-07-15 in PROJECT.md): trace-context contract (manager-injected `traceparent`) is the durable seam; the events.jsonl parser is a per-runtime adapter behind the Subagent seam with a self-instrumenting capability flag (ADAPT-01); attribute/span-kind conventions follow OpenInference semconv exactly so Phoenix queries survive the future LangGraph migration.

### Pending Todos

- CACHE-F1 direct-SDK cross-pod caching backend — `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md` (deferred; vNext or later).
- `subagent.levels` semantic rename — CLOSED, folded into v1.0.7 Phase 40 (CRANK-04).

### Blockers/Concerns

- **Phase 44 research gap:** exact `events.jsonl` multi-turn schema and the D-O5 byte threshold are unverified beyond the schema comment in `stream_parser.go` — needs a research pass against real fixture files before planning, not just the comment.
- **Phase 47 chart-pin freshness:** Phoenix Helm chart ships near-daily (9 versions in ~9 days at research time); re-fetch `Chart.yaml` fresh immediately before authoring INSTALL.md — do not trust the `10.0.0`/`18.0.0` pin recorded in research/STACK.md.
- **Cross-pod clock skew (Pitfall 5):** only exercisable on a multi-node cluster; the dev/proof environment (kind, single-node) cannot surface this class of bug — document as a known limitation at Phase 47 close, not treated as fully verified.

### Roadmap Evolution

- v1.0.8 roadmap defined 2026-07-15: Phases 42–47, 19 requirements (TRACE-01..03, ATTR-01..03, PROP-01..02, MSG-01..03, ADAPT-01, PHX-01..02, OBS-01..04, PROOF-01), 100% mapped. Phase numbering continues from v1.0.7 (Phase 41 was the last phase); Phase 42 is the first v1.0.8 phase.
- Strict dependency chain 42→43→44→45→46→47 (research's suggested 5-phase shape treated as a strong prior, split into 6 to keep per-phase requirement counts and success-criteria counts coherent): 42 lays trace-context helpers + attribute-complete spans for the 4 planner levels (ATTR-01..03); 43 closes Task-level parity + wires `traceparent` propagation, completing TRACE-01/02 and PROP-01/02; 44 (`research: true`) adds LLM message-array spans + the D-O5 redaction/size boundary + TRACE-03's flush discipline (the reporter's first TracerProvider call site); 45 wraps 44's synthesizer in the runtime-neutral adapter seam (ADAPT-01); 46 enriches all spans (sampler default, session.id, metadata/tags) and adds the dashboard deep link (OBS-01..04, depends on 43's PROP-02 + 44's message spans); 47 (`research: true` — chart-pin re-check) documents the self-hosted Phoenix install and captures the live end-to-end proof (PHX-01/02, PROOF-01) — its docs may draft in parallel with 42–46 but the live proof gates on 46.

## Deferred Items

Items acknowledged and carried forward from v1.0.7 close (2026-07-15) — full detail in [milestones/v1.0.7-MILESTONE-AUDIT.md](milestones/v1.0.7-MILESTONE-AUDIT.md):

| Category | Count | Notes |
|----------|-------|-------|
| debug_sessions | 1 | layer-a-envtest-flakes-pr9 [investigating] — CI-side Layer A envtest flakes; local envtest runs green, so the repro is CI-specific |
| quick_tasks | 24 | SUMMARY frontmatter `status:` field missing/unknown — audit-scanner bookkeeping only; the work itself shipped |
| todos | 4 | signed-commits-verified-badge (GPG scope, Future Requirements) · project-dispatch-missing-failurehalt-gate + task-dispatch-gate-order-divergence (audit W-2, next-milestone candidates) · cache-f1-direct-sdk-cross-pod-caching (vNext+) |
| uat_gaps | 1 | scanner false-positive: 37-UAT.md is status `passed` with 0 open scenarios |

Tech-debt carried into v1.0.8 window: W-2 FailureHalt/gate-order divergences (todos above), W-4 agentName/agentEmail CRD pattern locks not re-established post-crank, Phase 36 residual 'bot' vocabulary (7 comment/fixture refs), 37-REVIEW advisory warnings (secrets RBAC blast radius, gitfetch timeouts, settings-match determinism, Job-name coupling) + GIT_PAT fetch-path allowance.

## Session Continuity

Last session: 2026-07-15
Stopped at: v1.0.8 ROADMAP.md created (Phases 42–47); REQUIREMENTS.md traceability filled
Resume file: None

## Operator Next Steps

- Run `/gsd:plan-phase 42` to begin planning the first v1.0.8 phase (Trace-Context Foundation + Planner-Level Span Emission).
