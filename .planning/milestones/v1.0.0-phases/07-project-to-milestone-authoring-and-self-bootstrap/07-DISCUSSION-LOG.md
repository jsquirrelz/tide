# Phase 7: Project-to-Milestone Authoring and Self-Bootstrap - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-30
**Phase:** 07-project-to-milestone-authoring-and-self-bootstrap
**Areas discussed:** Acceptance bar / scope depth, Gate semantics at the Project→Milestone boundary

> Note: the two scope-defining forks were surfaced during spec-phase (after grounding against live code), because they redefine the SPEC's boundaries and acceptance criteria. discuss-phase confirmed the remaining items are pure HOW determined by the "mirror `milestone_controller`" constraint plus facts read from the `$0` smoke fixture — no further user forks remained.

---

## Acceptance bar / scope depth

| Option | Description | Selected |
|--------|-------------|----------|
| Full `$0` self-bootstrap | Bare Project drives a real minimal `Milestone→Phase→Plan→Task` tree to `Project=Complete`; stub emits a canned multi-level tree; wire Project Complete-detection. Honest TIDE-on-TIDE bar; risks down-stack cascades 8/9. | ✓ |
| Top-edge only: vacuous Milestone | Project authors one Milestone that Succeeds with zero Phases → Complete. Less work / less risk, but proves only the top edge. | |
| Authoring only; defer Complete | Wire Project→Milestone authoring + a bare-Project test; defer Complete + multi-level round-trip to a Phase 8. Smallest; v1.0 "Complete at `$0`" slips. | |

**User's choice:** Full `$0` self-bootstrap.
**Notes:** Grounding revealed the findings under-stated scope — the stub emits no `ChildCRDs` at any level and nothing sets `Project=Complete`. Both became in-scope requirements (SPEC REQ 3, REQ 4). User accepted the cascade-8/9 risk as the cost of the honest acceptance bar.

---

## Gate semantics at the Project→Milestone boundary

| Option | Description | Selected |
|--------|-------------|----------|
| No new gate; author & proceed | Project authors the Milestone and proceeds; existing `gates.Milestone` stays at the milestone level. Smoke fixture runs unattended (`gates.milestone=auto`). | ✓ |
| Apply `gates.Milestone` at this boundary too | Project parks at AwaitingApproval after authoring the Milestone — mirrors milestone_controller exactly but double-applies the milestone gate (human approves twice per milestone). | |

**User's choice:** No new gate; author & proceed.
**Notes:** The `Gates` struct has no `project` level; the milestone checkpoint already lives at the milestone level. One human checkpoint per milestone, where it already is.

---

## Claude's Discretion

- Exact deterministic child names and placeholder Markdown artifact content (only the structured `ChildCRDs` is load-bearing at `$0`).
- Where Complete-detection slots into the Reconcile ordering relative to `reconcilePhase3Lifecycle`.
- Whether the project-level `handleJobCompletion` factors a shared helper with milestone's or stays a parallel method.

## Deferred Ideas

- Multi-Milestone Projects + project-level `dependsOn` ordering.
- Real `git push` at level boundaries on the `$0` path (needs push-result envelope schema).
- Real Claude-backed authoring (live `acceptance-v1` `$25` path).
