---
gsd_state_version: 1.0
milestone: v1.0.7
milestone_name: "— First-Run Paper Cuts: Run Integrity & Operator Ergonomics"
current_phase: 34
current_phase_name: Run Integrity — Integration-Miss Gate + lastPushedSHA
status: planning
stopped_at: Phase 38 context gathered
last_updated: "2026-07-08T05:59:44.560Z"
last_activity: 2026-07-03
last_activity_desc: v1.0.7 roadmap created (Phases 34–38, 26/26 requirements mapped)
progress:
  total_phases: 5
  completed_phases: 0
  total_plans: 31
  completed_plans: 3
  percent: 10
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-03)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 34 — Run Integrity: Integration-Miss Gate + lastPushedSHA

## Current Position

Phase: 34 of 34–39 (Run Integrity — Integration-Miss Gate + lastPushedSHA)
Plan: — (not yet planned)
Status: Ready to plan
Last activity: 2026-07-03 — v1.0.7 roadmap created (Phases 34–38, 26/26 requirements mapped)

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity (recent milestones):**

- v1.0.6: 8 plans across 3 phases in ~2 days (2026-06-28 → 2026-06-29)
- v1.0.5: 3 plans, 1 phase (2026-06-27)
- Total plans completed v1.0.1–v1.0.6: ~85+

**v1.0.7 Phase Tracking:**

| Phase | Plans | Status |
|-------|-------|--------|
| 39. Pre-flight Tech-Debt Hardening | 2/2 | Complete|
| 34. Run Integrity — Integration-Miss Gate + lastPushedSHA | TBD | Not started |
| 35. Git Base Ref | TBD | Not started |
| 36. Signed Commits + Bot Identity | 3/4 | In progress |
| 37. Dashboard Surfaces — Artifact View, Project View, Log-Drawer States | TBD | Not started |
| 38. Small Independents — Pricing, promptFile, Telemetry Nudge, Tech-Debt Carry | TBD | Not started |

## Accumulated Context

### Session Continuity (2026-07-03 — first external-repo run)

Context beyond what the 2026-07-03 todos + the `verify-level-subagent` seed carry
(run details deliberately kept out of this public repo — the operator has them):

- **Run evidence is live but perishable.** The completed run's envelopes, bare repo,
  and worktree branches (incl. the never-integrated `tide/wt-e088c86c-…`) live on the
  `tide-projects` PVC in the run's project namespace on the operator's local
  **minikube** cluster. Deleting the namespace or cluster destroys the
  integration-miss repro evidence — export before cleanup if the namespace must go.

- **Real-vs-tallied spend:** dashboard/status said $10.86; Anthropic console said
  $3.84. Use console numbers when sizing budget caps until the pricing table is
  fixed (Phase 38 / COST-01).

- **Downstream state:** two PRs on the target repo were open and CI-green at session
  end; the pushed run branch carried one hand-recovered commit (the integration-miss
  deliverable) plus two human cleanup commits.

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.

**v1.0.7 binding constraints (from research + requirements):**

- Gate the *boundary push* on integration completeness, not `Complete` directly (preserves the #13b decision); the completeness verdict is always recomputed from git (`merge-base --is-ancestor`), never cached in `.status`.
- Tasks stay parallel; only run-branch merges serialize. No lockfile-existence protocols on the PVC — kernel `flock(2)` only, as belt-and-braces behind control-plane serialization.
- `charts/tide/values.yaml` is a FIXED contract — Phase 35's CRD change and Phase 36's agent-identity CRD/chart config batch into one chart version bump.
- No `+kubebuilder:default` on `baseRef` — absent means current HEAD behavior, one encoding.
- GPG signing is DESCOPED from v1.0.7 (2026-07-03, Phase 36 discussion) — SIGN-02/03/04 moved to REQUIREMENTS.md Future Requirements; Phase 36 delivers agent identity (SIGN-01) only. The gpg-shim spike and key-exposure analysis are preserved in `36-CONTEXT.md` `<deferred>`.
- Artifact ConfigMaps are a size-capped display cache (owner-ref'd, ~512 KiB, truncation markers); PVC/git remain source of truth. The manager cannot mount project PVCs.
- Dashboard stays read-only — no reader pods, no mutation surfaces.
- [Phase ?]: 36-02: resolveAgentIdentity is pure (no os.Getenv); resolver owns D-03 defaulting, manager transports chart tier via empty-is-unset ProviderDefaults
- [Phase 36]: 36-03: agent identity injected UNCONDITIONALLY into both Job builders (subagent executor+planner, push boundary+wave-integration); podjob mirrors resolveAgentIdentity inline to avoid a controller import cycle. D-03 chain now reaches runtime end-to-end.

### Roadmap Evolution

- v1.0.7 roadmap defined 2026-07-03: Phases 34–38, 26 requirements (INTEG-01..05, COST-01..03, BASE-01..03, SIGN-01..04, PROMPT-01, DASH-01..04, TELEM-01..03, DEBT-01..03), 100% mapped.
- Phase numbering continues from v1.0.6 (Phase 33 was the last phase). Phase 34 is the first v1.0.7 phase.
- Phase 34 (run integrity) is the headline and must land before Phase 36 — signing touches the same three commit sites the integration fix stabilizes.
- Phase 36 carries `research: true` (gpg-shim vs plumbing spike) and an ASK-FIRST key-exposure scope decision.
- Phases 35, 37, 38 are order-independent; Phase 38 items can interleave anywhere.
- **Phase 36 descoped 2026-07-03 (discussion):** SIGN-02/03/04 (GPG signing) deferred out of v1.0.7 — 26 → 23 active requirements. Phase 36 = SIGN-01 agent identity only (`spec.git.agentName`/`agentEmail` → chart → compiled-in `TIDE Agent <tide-agent@tideproject.k8s>`; full bot→agent rename). The `research: true` flag and ASK-FIRST decision above are void; the Phase 34 → 36 sequencing constraint no longer applies (Phase 35 batching stays).

### Pending Todos

- All ten 2026-07-03 first-run todos are now covered by v1.0.7 requirements (see REQUIREMENTS.md traceability); their files remain under `.planning/todos/pending/` until their phases close.
- `subagent.levels` semantic rename (DECIDED — breaking, needs SchemaRevision/v1alpha3; own milestone) — `.planning/todos/pending/2026-07-03-project-level-subagent-override-slot.md`.
- CACHE-F1 direct-SDK cross-pod caching backend — `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md` (deferred; vNext or later).

### Blockers/Concerns

- **Phase 38 empirical gate:** COST-03 — verify the `claude` CLI's cache-write TTL (5m 1.25× vs 1h 2×) via one teed credproxy request before the pricing rows ship.
- **Repro evidence perishable:** the integration-miss evidence lives on the minikube `tide-projects` PVC; export before any namespace/cluster cleanup (Phase 34 kind-suite repro reduces dependence on it).

## Deferred Items

Items acknowledged and carried forward at v1.0.6 milestone close (2026-06-29):

| Category | Count | Notes |
|----------|-------|-------|
| quick_tasks | 20 | Stale historical capture-log entries (260521–260625 era), long resolved |
| todos | 1 | historical |
| uat_gaps | 1 | partial-status historical entry |

v1.0.6 tech-debt carried INTO this milestone as requirements: W1 → DEBT-01, W2 → DEBT-02, envtest tier split → DEBT-03 (all Phase 38).

## Session Continuity

Last session: 2026-07-08T05:59:44.560Z
Stopped at: Completed 36-03-PLAN.md (agent-identity Job-env injection — both builders, all six subagent sites + both push sites; D-03 chain reaches runtime)
Resume file: None

## Operator Next Steps

- Plan the first phase: `/gsd-plan-phase 34`
