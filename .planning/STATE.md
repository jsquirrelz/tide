---
gsd_state_version: 1.0
milestone: v1.0.8
milestone_name: Phoenix Rising — OpenInference Trace Emission + Self-Hosted Phoenix
status: Awaiting next milestone
stopped_at: Milestone v1.0.8 complete — archived 2026-07-17; public tag/release pending the rc-gated process
last_updated: "2026-07-17T22:29:21.967Z"
last_activity: 2026-07-17 — Milestone v1.0.8 completed and archived
progress:
  total_phases: 6
  completed_phases: 6
  total_plans: 32
  completed_plans: 32
  percent: 100
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-17)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Between milestones — v1.0.8 "Phoenix Rising" complete + archived; public tag/release (rc-gated) and next-milestone scoping pending.

## Current Position

Phase: Milestone v1.0.8 complete
Plan: —
Status: Awaiting next milestone
Last activity: 2026-07-17 — Milestone v1.0.8 completed and archived

## Performance Metrics

**Velocity (recent milestones):**

- v1.0.8: 32 plans across 6 phases in ~3 days (2026-07-15 → 2026-07-17) · 240 commits · +34.8k/−343 LOC
- v1.0.7: 51 plans across 8 phases in ~12 days (2026-07-03 → 2026-07-15)
- v1.0.6: 8 plans across 3 phases in ~2 days (2026-06-28 → 2026-06-29)
- v1.0.5: 3 plans, 1 phase (2026-06-27)
- Total plans completed v1.0.0–v1.0.8: ~380+

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

- **v1.0.8 not yet released.** The milestone is complete + archived, but the public `v1.0.8` git tag + images/OCI charts are NOT built. Follow the rc-gated release recipe — **bump chart/appVersion as step one** (else the rc dry-run reuses prior published images → version skew).
- **Cross-pod clock skew (Pitfall 5) remains unverified** — single-node kind can't surface child-span-outside-parent-window rendering; documented as a known limitation at Phase 47 close, revisit on a multi-node cluster.
- (Resolved this milestone: Phase 44 `events.jsonl` schema + D-O5 byte threshold — researched and implemented; Phase 47 chart-pin — re-verified live at `10.0.1`/`18.1.0`.)

### Roadmap Evolution

- v1.0.8 roadmap defined 2026-07-15: Phases 42–47, 19 requirements (TRACE-01..03, ATTR-01..03, PROP-01..02, MSG-01..03, ADAPT-01, PHX-01..02, OBS-01..04, PROOF-01), 100% mapped. Phase numbering continues from v1.0.7 (Phase 41 was the last phase); Phase 42 is the first v1.0.8 phase.
- Strict dependency chain 42→43→44→45→46→47 (research's suggested 5-phase shape treated as a strong prior, split into 6 to keep per-phase requirement counts and success-criteria counts coherent): 42 lays trace-context helpers + attribute-complete spans for the 4 planner levels (ATTR-01..03); 43 closes Task-level parity + wires `traceparent` propagation, completing TRACE-01/02 and PROP-01/02; 44 (`research: true`) adds LLM message-array spans + the D-O5 redaction/size boundary + TRACE-03's flush discipline (the reporter's first TracerProvider call site); 45 wraps 44's synthesizer in the runtime-neutral adapter seam (ADAPT-01); 46 enriches all spans (sampler default, session.id, metadata/tags) and adds the dashboard deep link (OBS-01..04, depends on 43's PROP-02 + 44's message spans); 47 (`research: true` — chart-pin re-check) documents the self-hosted Phoenix install and captures the live end-to-end proof (PHX-01/02, PROOF-01) — its docs may draft in parallel with 42–46 but the live proof gates on 46.

## Deferred Items

Items acknowledged and deferred at v1.0.8 close (2026-07-17) — 30 carried-forward, none blocking. Phase 47's two PROOF-01 human items were **resolved** (signed off), not deferred.

| Category | Count | Notes |
|----------|-------|-------|
| quick_tasks | 24 | SUMMARY frontmatter `status:` field missing/unknown — audit-scanner bookkeeping only; the work itself shipped (same class carried since v1.0.7) |
| todos | 4 | signed-commits-verified-badge (GPG scope, Future Requirements) · project-dispatch-missing-failurehalt-gate + task-dispatch-gate-order-divergence (audit W-2 dispatch-gate correctness, next-milestone candidates) · cache-f1-direct-sdk-cross-pod-caching (vNext+) |
| debug_sessions | 2 | knowledge-base.md (a KB file, not a session) · layer-a-envtest-flakes-pr9 [investigating] — CI-side Layer A envtest flakes; local envtest runs green |

**Resolved at close (not deferred):** `47-HUMAN-UAT.md` + `47-VERIFICATION.md` — both PROOF-01 human items signed off 2026-07-17 (the `audit-open` uat_gaps=1 line is a benign 0-open-scenario, status `passed` file).

Tech-debt still carried forward: W-2 FailureHalt/gate-order divergences (todos above), W-4 agentName/agentEmail CRD pattern locks not re-established post-crank, Phase 36 residual 'bot' vocabulary (7 comment/fixture refs), 37-REVIEW advisory warnings (secrets RBAC blast radius, gitfetch timeouts, settings-match determinism, Job-name coupling) + GIT_PAT fetch-path allowance.

## Session Continuity

Last session: 2026-07-17T22:20:26Z
Stopped at: v1.0.8 milestone complete + archived; public tag/release (rc-gated) pending
Resume file: — (start the next milestone with /gsd:new-milestone, or run the rc-gated release for v1.0.8)

## Operator Next Steps

- Start the next milestone with /gsd-new-milestone
