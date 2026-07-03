---
title: Lifecycle-stage subagents beyond authoring — verify, research, review, debug, tournament, learnings
trigger_condition: Next milestone scoping (post-v1.0.6), OR when planning the wave-parallel-integration-miss fix, OR before the first unattended (all-auto-gates) real-repo run
planted_date: 2026-07-03
---

# Lifecycle-stage subagents beyond authoring — verify, research, review, debug, tournament, learnings

TIDE's five compiled-in templates (`internal/subagent/common/templates/`:
project/milestone/phase/plan planners + task_executor) are all
**authoring/executing** — there is no checking, verifying, reviewing, or
debugging surface anywhere in the pipeline. The first real-repo run
(2026-07-03) shipped `Complete` with a declared deliverable
missing from the pushed branch, and the outcome prompt's pass criterion
("pytest green") was never executed by anything in-cluster — the only
verifier in the loop was the human operator diffing `filesTouched` against
the branch by hand.

## Gap map 1 — six agentic workflow patterns vs TIDE

| Pattern | TIDE today |
| --- | --- |
| Classify-And-Act | faint (per-level model ladder routes work by kind) |
| Fanout-And-Synthesize | core (planning waves + reporter materialization) |
| Adversarial Verification | **absent** |
| Generate-And-Filter | **absent** |
| Tournament | absent (plausible someday for candidate-plan judging; cost-gated) |
| Loop Until Done | partial (reconcile loop + blind `maxAttemptsPerTask` retries) |

## Gap map 2 — GSD phase lifecycle vs TIDE (the richer comparison)

TIDE deliberately re-implements GSD's hierarchy as CRDs, but only ported the
authoring half of GSD's lifecycle:

| GSD stage | GSD surface | TIDE analog | Gap |
| --- | --- | --- | --- |
| discuss (pre-planning context) | discuss-phase | outcomePrompt only; gates approve *after* authoring | gates are approve-shaped, not discuss-shaped |
| research/grounding | phase-researcher, pattern-mapper | planners self-ground inline (worked well on first external run) | partial; quality rides on planner model |
| plan | planner | 4 planner templates | covered |
| **plan check (pre-execution, goal-backward)** | plan-checker | `planAdmission` = mechanical file-touch match only | **no semantic plan verification** |
| execute | executor | task_executor | covered |
| **verify (post-execution, gate_decision)** | verifier → VERIFICATION.md APPROVED/BLOCKED | none — Succeeded/Complete stamp on JobComplete | **pass criteria never checked; the headline gap** |
| integration check | integration-checker | none | the wave-integration-miss bug is exactly this class |
| code review | code-reviewer | none | lower priority (host-side PR review exists) |
| UAT | verify-work | approve-gate annotations | partial; see dashboard artifact-view todo |
| debug on failure | debugger | blind re-dispatch ×3 then Failed | no diagnosis stage |
| extract learnings | extract-learnings | none | no cross-run learning persistence |
| security audit | secure-phase | gitleaks at push (leak class only) | partial |

## Proposed shape when this surfaces

A sixth template class, `verifier`, dispatched at two seams (both read-only
against the worktree — no authoring):

1. **Plan-check** — after plan authoring, before task dispatch: goal-backward
   "will these tasks achieve the phase objective?" + declared-vs-plausible
   file-touch sanity. Rejection routes back to the planner with findings
   (Generate-And-Filter for plans).
2. **Level verification** — after a level's children succeed, before the
   level stamps Succeeded / boundary push: check pass criteria actually hold
   (run the declared gate command in-worktree), every declared deliverable
   exists on the run branch, constraints (files NOT to touch) were honored.
   Emits a gate_decision the reconciler enforces.

## Additional endorsed stages (operator, 2026-07-03)

Beyond plan-check + level-verify, four more stages judged "very valuable":

3. **Research** — pre-planning dispatch producing a grounding artifact
   (RESEARCH.md-analog) the planner consumes, instead of every planner
   re-grounding inline. Observed inline grounding worked well on the
   first external run, so the wins here are consistency (grounding quality stops
   riding on the planner model tier) and cheaper planner dispatches
   (grounding once on a cheap model, planning on the expensive one).
4. **Integration check** — cross-child E2E verification at milestone/project
   boundaries, distinct from per-level verify (which checks one level's own
   deliverables): do sibling phases' outputs actually compose, does the full
   run branch build/test as a whole. The wave-integration-miss bug is this
   class caught one level down.
5. **Code review** — post-execution, pre-boundary-push review of the run
   branch diff producing severity-tagged findings. This is exactly the
   "future review/verify subagent" the repo CLAUDE.md already anticipates —
   its coverage-not-conservatism prompting note applies verbatim. Default
   report-only (findings artifact on the run branch / dashboard), optionally
   gate-blocking via config.
6. **Extract learnings** — post-Complete dispatch distilling what the run
   learned (planner mispredictions, task retry causes, constraint
   violations attempted). Two persistence scopes with distinct retrieval
   consumers (operator, 2026-07-03):
   - **Project-scoped** (project namespace — ConfigMap or committed to the
     target repo): retrieved by the project's own later dispatches —
     research grounds against prior-run learnings for the same targetRepo,
     verify checks previously-violated constraints first, debug starts from
     previously-diagnosed failure causes.
   - **Cluster-wide** (tide-system, across projects): may feed research too
     (cross-project patterns), but the primary consumer is likely the
     **human operator** — insights surfaced via the dashboard/reporting
     ("planner overestimates task counts on repo family X", "Haiku retry
     rate by task class"), not auto-injected into prompts.

   Watch PERSIST-02 — learnings are artifacts, not derived schedules, but
   the no-external-DB constraint still applies: ConfigMaps or git, not a
   new store.
7. **Tournament-style candidate-plan judging** — N independent planner
   dispatches for the same level, pairwise-judged, winner proceeds
   (optionally grafting runners-up ideas). Direct multiplier on planner
   cost, so strictly config-gated (per-level N, default 1 = off); most
   plausible at the plan level where authoring quality compounds into every
   task. Judges are verifier-class dispatches — reuses the same template
   machinery as plan-check.
8. **Debug-level subagent** — replaces blind `maxAttemptsPerTask` re-dispatch:
   on task failure, a diagnosis dispatch reads the failed attempt's
   envelope/logs/diff and either (a) annotates the retry prompt with the
   suspected cause, (b) reroutes to a stronger model for the retry, or
   (c) recommends Failed-fast with a human-readable reason. Consumes
   project-scoped learnings (prior failure causes) and writes new ones —
   the tightest retrieval loop of stage 6.

## Design constraints already on record

- Prompt for **coverage, not conservatism** (CLAUDE.md subagent-tuning note:
  Opus-family models honor "only high-severity" literally and drop real
  findings — find everything with confidence/severity tags, filter downstream).
- Gate policy stays in config, not the controller (existing principle);
  stage dispatch should be per-level configurable like gates/models —
  `auto` runs cost money, so default-off or milestone/phase-only is plausible
  per stage.
- Wave-boundary failure semantics must not be weakened — a BLOCKED verify is
  a new halt class, not a reinterpretation of task failure.
- The mechanical completeness gate in
  `2026-07-03-wave-parallel-integration-miss.md` lands first and regardless —
  it needs no LLM and closes the shipped bug.
- Suggested sequencing when this surfaces: verify (plan-check + level-verify
  + integration check) is the safety tier and comes first; research, code
  review, and debug are the quality tier (debug also closes the
  blind-retry gap); learnings is the compounding tier — project-scoped
  retrieval by research/verify/debug is what makes it non-write-only, and
  debug↔learnings is the tightest loop to build first; tournament is the
  cost-multiplier tier, last and config-gated.
