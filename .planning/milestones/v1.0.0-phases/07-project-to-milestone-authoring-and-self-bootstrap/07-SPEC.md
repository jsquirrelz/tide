---
phase: 07-project-to-milestone-authoring-and-self-bootstrap
type: spec
status: complete
created: 2026-05-30
ambiguity_score: 0.19
requirements_locked: 7
tags: [cascade-7, project-milestone-authoring, self-bootstrap, ship-blocker, v1.0]
---

# Phase 7: Project-to-Milestone Authoring and Self-Bootstrap — Specification

**Created:** 2026-05-30
**Amended:** 2026-05-31 (post-research scope decision — see REQ 7)
**Ambiguity score:** 0.19 (gate: ≤ 0.20)
**Requirements:** 7 locked

## Goal

A bare `Project` CRD self-bootstraps the full five-level cascade — TIDE authors its `Milestone`, which drives `Phase → Plan → Task` — and reaches `Project status.phase=Complete` at `$0` (stub-driven, no API key), closing cascade-7, the v1.0 ship blocker.

## Background

Phase 6's `$0` BOOT-04 acceptance (`make acceptance-v1-smoke`) drove a bare Project to `status.phase=Initialized` and then stalled forever (`06-ACCEPTANCE-FINDINGS.md`, cascade-7). Verified against live code on 2026-05-30:

- **No Project→Milestone authoring.** After `Initialized`, `internal/controller/project_controller.go:271-380` runs only `reconcilePhase3Lifecycle` (git branch/clone/push, explicitly "skeletal"). `grep -rn 'Create(.*Milestone' internal/` returns zero non-test hits — no controller authors a Milestone.
- **The proven analog exists one level down.** `internal/controller/milestone_controller.go:184-432` (`reconcilePlannerDispatch` + `handleJobCompletion`) dispatches a planner Job, reads `EnvelopeOut`, and calls `MaterializeChildCRDs` to create child Phases. `internal/controller/dispatch_helpers.go` already exposes `BuildPlannerEnvelope(level, ...)` and a `MaterializeChildCRDs` whose Kind allowlist already includes `Milestone`. The Project needs this same pattern, one level up.
- **The stub emits no children.** `cmd/stub-subagent/main.go` — all five dispatch modes write a bare `KindTaskEnvelopeOut` with empty `ChildCRDs`. The `up-stack-project.yaml` fixture comment (lines 13-17) confirms the planner envelope round-trip was always deferred follow-up work at every level.
- **Nothing sets `PhaseComplete`.** `project_controller.go` only *reads* `PhaseComplete` (to fire the push Job); the comment at lines 453-457 defers "full level-boundary detection." No code transitions a Project to Complete based on Milestone status.
- **The gap survived to v1 because no test exercised it.** Every Layer B fixture (`up-stack-project`, `three-task-wave`, `chaos-resume-three-task`, `push-lease-project`) pre-applies a `kind: Milestone`, so the suite tests down-stack dispatch but never Project→Milestone authoring.

**Locked decisions (this phase's two fix-shape forks):**
- **Scope = full `$0` self-bootstrap** — a real minimal `Milestone → Phase → Plan → Task` tree to `Project=Complete`, not a vacuous top-edge.
- **Gates = no new gate at the Project→Milestone step** — the Project authors and proceeds; the existing `gates.Milestone` checkpoint stays at the milestone level (after Phases authored). The `$0` smoke fixture sets `gates.milestone=auto` to run unattended.

## Requirements

1. **Project→Milestone planner dispatch**: The ProjectReconciler dispatches a project-level planner Job after `Initialized`.
   - Current: After `Initialized`, the reconciler runs only `reconcilePhase3Lifecycle`; no planner Job, no `level=project` dispatch.
   - Target: After `Initialized` (clone Job present), the ProjectReconciler dispatches a planner Job (deterministic name `tide-project-<uid>-1`) via `podjob.BuildJobSpec(JobKindPlanner)` + `BuildPlannerEnvelope("project", project, ...)`, mirroring `milestone_controller.go:reconcilePlannerDispatch`; patches `Status.Phase=Running` with Condition `AuthoringPlanner=True`. Coexists with the existing clone/push lifecycle.
   - Acceptance: Applying a bare Project yields a Job labelled `tideproject.k8s/level=project,role=planner` owned by the Project (Controller=true); Project `Status.Phase` transitions `Initialized → Running`.

2. **Milestone materialization from planner envelope**: On planner Job completion, the Project creates the authored Milestone CR.
   - Current: No path reads a project-level `EnvelopeOut` or creates a Milestone; `MaterializeChildCRDs` is invoked only by down-stack reconcilers.
   - Target: A Project-level `handleJobCompletion` reads `EnvelopeOut` via the configured envelope reader (`PodStatusEnvelopeReader`) and calls `MaterializeChildCRDs(ctx, ..., project, envOut.ChildCRDs)`; AlreadyExists is idempotent success. No new gate is applied at this boundary (locked decision).
   - Acceptance: A bare Project produces exactly one Milestone CR with `ownerReferences` → Project (Controller=true) and `spec.projectRef` = Project name; a second reconcile creates no duplicate.

3. **Stub-subagent canned multi-level tree**: The stub emits `ChildCRDs` keyed by the envelope `Level` so a `$0` planner run authors real children.
   - Current: Every stub mode emits empty `ChildCRDs`; no planner level authors anything.
   - Target: In `success` mode, when `env.Role=="planner"`, the stub emits `EnvelopeOut.ChildCRDs` by `env.Level`: `project→[1 Milestone]`, `milestone→[1 Phase]`, `phase→[1 Plan]`, `plan→[1 Task]`; `task` keeps today's leaf behavior. Each child Spec carries its required parent `*Ref` field. Payload stays small enough for the termination-message envelope channel.
   - Acceptance: A unit test feeds the stub an `EnvelopeIn` at each planner level and asserts `out.json` carries exactly one `ChildCRD` of the expected Kind with a valid parent ref; `task`-level output carries zero children.

4. **Project Complete-detection**: The Project transitions to `Complete` when all owned Milestones have `Succeeded`.
   - Current: `PhaseComplete` is only read; nothing sets it.
   - Target: Using its existing `Owns(&Milestone{})` watch, the ProjectReconciler patches `Status.Phase=Complete` once ≥1 owned Milestone exists and all owned Milestones report `Status.Phase=Succeeded` (boundary detection analogous to `gates.BoundaryDetected(..., "Milestone")`).
   - Acceptance: In an integration test, when the sole child Milestone reaches `Succeeded`, the Project transitions `Running → Complete`; with zero Milestones or an unfinished Milestone it stays `Running`.

5. **Bare-Project Layer B integration test**: A new kind-based spec applies only a Project and asserts the full tree materializes.
   - Current: All Layer B fixtures pre-apply a `kind: Milestone`; Project→Milestone authoring is never exercised.
   - Target: A new fixture (bare Project, `gates.milestone=auto`, stub image) + spec asserts the cascade: Milestone (owner=Project) → Phase → Plan → Task all materialize, and the Project reaches `Complete`.
   - Acceptance: `make test-int` includes the new spec and it passes (full `Milestone→Phase→Plan→Task` tree materializes from a bare Project; Project=`Complete`).

6. **`$0` acceptance reaches Complete**: `make acceptance-v1-smoke` drives a bare Project to `Complete` with no API key and no acceptance-script changes.
   - Current: The smoke gate drives to `Initialized` then stalls (cascade-7).
   - Target: With Phase 7 wiring plus the smoke Project carrying `gates.milestone=auto`, the same `make acceptance-v1-smoke` proceeds `Initialized → Running → Complete` at `$0`, with **no edits** to `hack/scripts/acceptance-v1.sh`.
   - Acceptance: `make acceptance-v1-smoke` exits 0 and the Project reaches `status.phase=Complete`; the run log shows the `Milestone→Phase→Plan→Task` tree materialized; no `ANTHROPIC_API_KEY` is consumed (cost = `$0`).

7. **Down-stack cascade completion** (added post-research): The Phase→Plan→Task chain actually drives to all-`Succeeded` when the stub feeds children, so the full tree is real, not partially-materialized. *(Research `07-RESEARCH.md` refuted the assumption that the down-stack "already works" — two production gaps block the cascade.)*
   - Current: (a) `Plan.Status.ValidationState` is never set to `"Validated"` in production (only in tests), so `reconcileWaveMaterialization` (`plan_controller.go:535`) no-ops → Waves never materialize → Task executor Jobs never fire. (b) `PlanReconciler` has no `patchPlanSucceeded`, so `PhaseReconciler.handleJobCompletion`'s `BoundaryDetected(ph,"Plan")` never observes `Plan=Succeeded` → the Phase requeue-loops forever.
   - Target: `PlanReconciler` stamps `ValidationState="Validated"` in its planner-job-completion path (after materializing Task children) so Waves derive + Task executor Jobs dispatch; and gains a `patchPlanSucceeded` transition (Plan reaches `Succeeded` once its Wave/Tasks complete) so the Phase boundary detects and advances. These are the **only** two down-stack reconciler-logic edits permitted (the cascade-8/9 work the user accepted).
   - Acceptance: with the stub feeding a 1-1-1-1 tree, `Plan.Status.ValidationState=="Validated"`, a `Wave` materializes, the leaf `Task` executor Job runs and the Task reaches `Succeeded`, the `Plan`/`Phase` reach `Succeeded`; asserted by the bare-Project Layer B spec (REQ 5).

## Boundaries

**In scope:**
- ProjectReconciler `Initialized → author-Milestone` planner dispatch (mirror `milestone_controller.go:reconcilePlannerDispatch` + `handleJobCompletion`)
- `Create(Milestone)` from `EnvelopeOut` via the existing `MaterializeChildCRDs`
- Project `Complete`-detection from child Milestone status
- stub-subagent planner-mode multi-level canned `ChildCRDs` (project/milestone/phase/plan)
- New bare-Project Layer B integration test + fixture
- Smoke Project fixture carries `gates.milestone=auto`

**Out of scope:**
- Real Claude-backed authoring (live `acceptance-v1` `$25` path) — `$0` stub only; the live path is unchanged
- Changes to down-stack reconciler *logic* **except the two specific fixes in REQ 7** (`Plan.ValidationState="Validated"` stamp + `PlanReconciler.patchPlanSucceeded`). All other Milestone/Phase/Task reconciler logic is reused as-is — only the stub feeds them children. If a *third* down-stack cascade surfaces during execution, surface it (do not silently expand) per CLAUDE.md "don't predict chain terminator."
- A new `project` gate level or any change to `gates.Milestone` semantics — locked decision: no new gate; existing milestone gate unchanged
- Edits to `hack/scripts/acceptance-v1.sh` — cascades 1–6 already fixed; it drives correctly through `Initialized`
- Multi-Milestone Projects / project-level `dependsOn` ordering — single-Milestone bootstrap proves the edge; multi-milestone authoring deferred
- Push-result envelope schema / real `git push` semantics — Phase 3 follow-up, unchanged

## Constraints

- **Mirror the proven pattern; add no new dispatch abstraction.** Reuse `BuildPlannerEnvelope`, `podjob.BuildJobSpec(JobKindPlanner)`, `MaterializeChildCRDs`, `internal/gates`, `credproxy.Sign`, and `PodStatusEnvelopeReader`.
- **CRD `.status`-only persistence.** No schedule caching; resumption stays indegree-map + completed-set. Project `Complete` is derived from live child status, not cached.
- **Stub canned tree is minimal** (exactly 1 child per level) to fit the Pod termination-message envelope channel and keep `$0` runtime low.
- **Planner Jobs use the `JobKindPlanner` Caps floor** (600s wall-clock, 20-iteration default) — identical to the milestone level.
- **`$0` smoke fixture gate = `auto`** so the run completes unattended; the default-`approve` human-gate path remains covered by `test/e2e/gate_flow_test.go`.
- **No regression** of the established 7/7 Layer B + 18/18 Layer A green baseline (Phase 02.2 `chain_status: empirically_closed`).

## Acceptance Criteria

- [ ] Applying a bare Project (no Milestone) yields a `level=project,role=planner` Job owned by the Project; Project Phase `Initialized → Running`
- [ ] Exactly one Milestone CR materializes from the planner `EnvelopeOut`, owner-ref'd to the Project with `spec.projectRef` set; re-reconcile creates no duplicate
- [ ] stub-subagent emits one `ChildCRD` of the correct Kind at each planner level (project→Milestone, milestone→Phase, phase→Plan, plan→Task) and zero at `task`; unit test passes
- [ ] Project transitions `Running → Complete` when all child Milestones `Succeed`; stays `Running` with zero/unfinished Milestones
- [ ] New bare-Project Layer B spec passes under `make test-int` (full `Milestone→Phase→Plan→Task` tree materializes AND reaches `Succeeded`; Project=`Complete`)
- [ ] `Plan.Status.ValidationState=="Validated"` is stamped in production (not just tests); a `Wave` materializes and the leaf `Task` executor Job runs to `Succeeded`; `Plan`/`Phase` reach `Succeeded` (REQ 7 — down-stack cascade closed)
- [ ] `make acceptance-v1-smoke` exits 0 and reaches `Project status.phase=Complete` at `$0` with no edits to `hack/scripts/acceptance-v1.sh`
- [ ] Existing 7/7 Layer B + 18/18 Layer A specs remain green (no regression)

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                              |
|--------------------|-------|------|--------|--------------------------------------------------------------------|
| Goal Clarity       | 0.85  | 0.75 | ✓      | Measurable: bare Project → Complete at $0 via a real tree          |
| Boundary Clarity   | 0.80  | 0.70 | ✓      | Explicit in/out; reuse down-stack as-is; no acceptance-script edits |
| Constraint Clarity | 0.75  | 0.65 | ✓      | Mirror milestone pattern; CRD-status-only; minimal canned tree     |
| Acceptance Criteria| 0.80  | 0.70 | ✓      | 7 pass/fail checks incl. $0 smoke gate + no-regression             |
| **Ambiguity**      | 0.19  | ≤0.20| ✓      | Residual HOW (stub Level-switch shape, dispatch sequencing) → discuss-phase |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                                  | Decision locked                                                                 |
|-------|-----------------|---------------------------------------------------|---------------------------------------------------------------------------------|
| 1     | Researcher      | What exists today for Project→Milestone authoring? | Nothing — Project stalls at Initialized; milestone_controller is the proven analog |
| 2     | Boundary Keeper | Acceptance bar: how deep does Phase 7 go?         | **Full `$0` self-bootstrap** — real minimal Milestone→Phase→Plan→Task to Complete |
| 3     | Boundary Keeper | Which gate applies at Project→Milestone authoring? | **No new gate**; existing `gates.Milestone` stays at milestone level; smoke=auto |
| 4     | Failure Analyst | What blocks Complete at $0?                       | Stub emits no children + nothing sets Complete → both in scope (REQ 3, REQ 4)   |

---

*Phase: 07-project-to-milestone-authoring-and-self-bootstrap*
*Spec created: 2026-05-30*
*Next step: /gsd-discuss-phase 7 — implementation decisions (stub Level-switch shape, dispatch/clone sequencing, Complete-detection placement)*
