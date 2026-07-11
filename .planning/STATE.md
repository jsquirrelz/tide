---
gsd_state_version: 1.0
milestone: v1.0.7
milestone_name: "— First-Run Paper Cuts: Run Integrity & Operator Ergonomics"
current_phase: 35
current_phase_name: Git Base Ref
status: executing
stopped_at: Completed 36-03-PLAN.md (agent-identity Job-env injection — both builders, all six subagent sites + both push sites; D-03 chain reaches runtime)
last_updated: "2026-07-11T17:17:39.537Z"
last_activity: 2026-07-11
last_activity_desc: Phase 34 complete, transitioned to Phase 35
progress:
  total_phases: 5
  completed_phases: 5
  total_plans: 33
  completed_plans: 33
  percent: 100
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-03)

**Core value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.
**Current focus:** Phase 38 — small-independents-pricing-accuracy-promptfile-telemetry-nud

## Current Position

Phase: 35 — Git Base Ref
Plan: Not started
Status: Executing Phase 38
Last activity: 2026-07-11 — Phase 34 complete, transitioned to Phase 35

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
| 34. Run Integrity — Integration-Miss Gate + lastPushedSHA | 6/6 | Complete (verify-close pending) |
| 35. Git Base Ref | 4/4 | Complete |
| 36. Signed Commits + Bot Identity | 4/4 | Complete|
| 37. Dashboard Surfaces — Artifact View, Project View, Log-Drawer States | 12/12 | Complete |
| 38. Small Independents — Pricing, promptFile, Telemetry Nudge, Tech-Debt Carry | 7 | Planned, not executed |
| Phase 36 P04 | 14min | 3 tasks | 8 files |

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
- [Phase ?]: D-06 batched bump applied in 36-04: chart was still 1.0.6, single 1.0.6->1.0.7 bump across chart + crds-chart

### Roadmap Evolution

- v1.0.7 roadmap defined 2026-07-03: Phases 34–38, 26 requirements (INTEG-01..05, COST-01..03, BASE-01..03, SIGN-01..04, PROMPT-01, DASH-01..04, TELEM-01..03, DEBT-01..03), 100% mapped.
- Phase numbering continues from v1.0.6 (Phase 33 was the last phase). Phase 34 is the first v1.0.7 phase.
- Phase 34 (run integrity) is the headline and must land before Phase 36 — signing touches the same three commit sites the integration fix stabilizes.
- Phase 36 carries `research: true` (gpg-shim vs plumbing spike) and an ASK-FIRST key-exposure scope decision.
- Phases 35, 37, 38 are order-independent; Phase 38 items can interleave anywhere.
- **Phase 36 descoped 2026-07-03 (discussion):** SIGN-02/03/04 (GPG signing) deferred out of v1.0.7 — 26 → 23 active requirements. Phase 36 = SIGN-01 agent identity only (`spec.git.agentName`/`agentEmail` → chart → compiled-in `TIDE Agent <tide-agent@tideproject.k8s>`; full bot→agent rename). The `research: true` flag and ASK-FIRST decision above are void; the Phase 34 → 36 sequencing constraint no longer applies (Phase 35 batching stays).
- Phase 40 added 2026-07-06; **rescoped same day by discussion (40-CONTEXT.md) into a full version-lifecycle turn:** introduce v1alpha3 (carrying the folded `subagent.levels` rename + user-approved batchable schema fixes), then remove v1alpha1 AND v1alpha2 — end state v1alpha3 sole served+storage version. Reinstall-only migration (D-09-consistent), SchemaRevision guard generalized, owner-ref dual-accepts dropped, deep docs/samples sweep, envelope contract decoupled to `dispatch.tideproject.k8s/v1alpha1`. Scouting found v1alpha1 already served:false since Phase 23 and the INSTALL.md/gates.md quickstart examples broken today (v1alpha1 apiVersion rejected).
- **Phases 40 + 41 appended to v1.0.7 (2026-07-11, operator decision):** Phase 40 = v1alpha1/v1alpha2 code removal + subagent.levels semantic rename (breaking migration; authoritative planning artifacts pending operator import from the other machine — wait for import, do not re-plan fresh). Phase 41 = 12-item non-breaking refactoring review (seed in-repo, file:line-verified), sequenced after 40.
- **Phase 40 import LANDED (2026-07-11):** the authoritative planning artifacts (40-CONTEXT/RESEARCH/PATTERNS/VALIDATION/DISCUSSION-LOG + 7 plans, CRANK-01..07) were rebased onto origin/main from the planning machine; the 2026-07-11 roadmap stub and its seed todo (`2026-07-09-phase-40-v1alpha-removal-semantic-rename.md`) are superseded — the seed's scope items all map into plans 40-03 (guard re-expression, owner-walk, scheme comment) and 40-04 (rename).

### Pending Todos

- All ten 2026-07-03 first-run todos are now covered by v1.0.7 requirements (see REQUIREMENTS.md traceability); their files remain under `.planning/todos/pending/` until their phases close.
- `subagent.levels` semantic rename (DECIDED — breaking, needs SchemaRevision/v1alpha3) — **FOLDED into Phase 40 (2026-07-06 discussion; supersedes "own milestone" routing)** — `.planning/todos/pending/2026-07-03-project-level-subagent-override-slot.md`.
- CACHE-F1 direct-SDK cross-pod caching backend — `.planning/todos/pending/cache-f1-direct-sdk-cross-pod-caching.md` (deferred; vNext or later).

### Blockers/Concerns

- **Phase 38 empirical gate:** COST-03 — verify the `claude` CLI's cache-write TTL (5m 1.25× vs 1h 2×) via one teed credproxy request before the pricing rows ship.
- **Repro evidence perishable:** the integration-miss evidence lives on the minikube `tide-projects` PVC; export before any namespace/cluster cleanup (Phase 34 kind-suite repro reduces dependence on it).
- ~~**Phase 40 execution HELD until last in v1.0.7 (user decision 2026-07-06)**~~ **RESOLVED 2026-07-11:** the hold condition is satisfied — phases 34–38 all executed and landed on origin/main (PRs #3–#10), and the authoritative Phase 40 planning artifacts were imported from the planning machine via rebase (11 commits; remote's Phase 40 stub superseded by the full 7-plan/4-wave/CRANK-01..07 plan). Phase 40 is UNBLOCKED and executes next, then Phase 41. Before executing: plans cite pre-34–38 line numbers (e.g. project_controller.go sites) — the enumeration-driven tasks absorb this, but treat cited line numbers as hints, not anchors.
- **Shared-.git hazard while the pr3-debug worktree session is live (2026-07-06):** something in that session's kind/fixture runs intermittently flips the shared `.git/config` `core.bare` to `true`, breaking ALL commits repo-wide ("fatal: this operation must be run in a work tree"). Flipped 3× on 2026-07-06 and again by 2026-07-11 (4th). Containment: `git config core.bare false` and retry. Root cause not yet identified — if it recurs after the pr3 session ends, open a `/gsd:debug` session.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260708-tv5 | Fix Defect E (DASH-02) — boundary push supersedes a stale-subset artifact Job so milestone/phase/plan artifacts stage on the run branch (envtest-green; Layer-B confirmation deferred) | 2026-07-09 | 6a65f4e | [260708-tv5-fix-defect-e-dash-02-follow-up-milestone](./quick/260708-tv5-fix-defect-e-dash-02-follow-up-milestone/) |
| 260709-lint37 | Clear 7 golangci-lint findings (lll/modernize.stringscut/modernize.rangeint/prealloc) in cmd/tide-push + cmd/dashboard/gitfetch — no behavior change, `make lint` green — to unblock Phases 36–37 ship PR | 2026-07-09 | f71bdd7 | — |
| 260710-g2r | Fix main RED — raise Layer B kind suite timeout budget across every layer (Ginkgo 25m→45m, go-test 40m→50m, outer 45m→55m, kind-sensitive step 35m→60m/job→70m, nightly kind step 25m→60m/job→110m) so make test-int completes; Phases 36/37 (agent-identity + artifact-staging DASH-02 live cascade) outgrew the 35m step. No spec trimmed | 2026-07-10 | 8930c3f | [260710-g2r-raise-layer-b-kind-integration-suite-tim](./quick/260710-g2r-raise-layer-b-kind-integration-suite-tim/) |

## Deferred Items

Items acknowledged and carried forward at v1.0.6 milestone close (2026-06-29):

| Category | Count | Notes |
|----------|-------|-------|
| quick_tasks | 20 | Stale historical capture-log entries (260521–260625 era), long resolved |
| todos | 1 | historical |
| uat_gaps | 1 | partial-status historical entry |

v1.0.6 tech-debt carried INTO this milestone as requirements: W1 → DEBT-01, W2 → DEBT-02, envtest tier split → DEBT-03 (all Phase 38).

## Session Continuity

Last session: 2026-07-06T16:44:10.929Z
Stopped at: Phase 40 planned (7 plans, gate-passed); execution HELD until last in v1.0.7
Resume file: .planning/phases/40-deprecate-v1alpha1-api/40-01-PLAN.md

## Operator Next Steps

- Plan the first phase: `/gsd-plan-phase 34`
