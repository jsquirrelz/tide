# Phase 12: Gate Semantics + Reject/Resume - Context

**Gathered:** 2026-06-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Gate passage, reject/resume recovery, and planner-failure recovery are correct across the four level reconcilers (Milestone/Phase/Plan/Task). Closes dogfood run-1 findings 7 (approve-consume run-killer), 1 (gate-before-descent — folded in by decision D-01), 5 (planner-failure/approval interplay), and 9a's reject/resume wedge. Requirements: GATE-01..04, RESUME-01. Every fix carries a regression test reproducing the run-1 symptom; the kind cluster `tide` still holds the run-1 CRs as the repro environment (never delete it; plans use it for repro only).

</domain>

<decisions>
## Implementation Decisions

### Approval semantics (gate seat)
- **D-01: Gate sits at descent.** `AwaitingApproval` parks the level after its artifact is authored and BLOCKS child dispatch. The operator reviews the authored artifact, approves, children dispatch, and the level auto-succeeds when all children complete — no second approval at completion. This is the docs' review-before-descent promise and closes run-1 finding 1's spend gap (5 × ~$0.64 of phase planners fired pre-approval in run 1). **Scope note:** this folds finding 1 into Phase 12 as GATE-04 (added to REQUIREMENTS.md + ROADMAP.md traceability).
- **D-02: Materialize children, hold dispatch.** Child CRs are created immediately by the reporter (operator sees the planned children in dashboard/kubectl while reviewing), but child reconcilers refuse to dispatch planner/executor Jobs while the parent is unapproved. Review with full DAG visibility, zero spend.
- **D-03: Approval never jumps a level to Succeeded.** Succession is exclusively children-gated (the ChildCount-gated succession logic in handleJobCompletion is the right shape; the bug is the approval path that bypasses it). The run-1 finding-7 regression test must prove: approving a Milestone with N incomplete children leaves it un-Succeeded until all N complete.

### Approved-state shape
- **D-04: `Running` + `ApprovedByUser` condition.** After approval the level's Status.Phase returns to Running; a Condition (Reason=`ApprovedByUser`, mirroring the existing `ResumedByUser` pattern in plan_controller.go) permanently records the human approval. NO new Status.Phase enum value (gates.md:99's `Approved` sketch is superseded — update that doc). No CRD enum/conversion churn; dashboard chips and CEL stay as-is.

### Reject/resume contract
- **D-05: Reject parks, never fail-marks.** `tide reject` sets a Rejected condition and halts dispatch; children pause where they are. No `Status.Phase=Failed` writes (today's patchPlanFailed cascade at plan_controller.go:478 goes away for the reject path). `Failed` is reserved for real failures. Docs' "preserves all state" becomes true rather than reworded away.
- **D-06: Resume lifts parks AND retries Failed (flagged).** `tide resume` undoes a reject park. With an explicit `--retry-failed` flag it also implements the run-1 kubectl recovery recipe as a sanctioned verb: clear status.phase + conditions → reconciler re-dispatches → `ResumedByUser` condition set. The flag is deliberate friction so legitimately dead work isn't resurrected by accident. This verb is also the recovery path HALT-01 (Phase 13) will point at.

### Planner-retry trigger (GATE-03 reworded)
- **D-07: Approve on a Failed level errors with a pointer to resume.** `tide approve` against a level whose planner Job failed prints the failure reason and directs to `tide resume --retry-failed`. Approval never doubles as a spend-retry (protects against re-firing into an empty credit balance — interacts with Phase 13's HALT-01). GATE-03 is reworded: "a failed-planner level is recoverable via `tide resume --retry-failed`, never wedged; `tide approve` against it gives an actionable error." Note: gate-at-descent structurally eliminates finding 5's original scenario (children can't have failed planner attempts while the parent is parked — they haven't dispatched).

### Claude's Discretion
- Regression-test vehicle split: envtest vs kind Layer B per scenario (success criteria require the run-1 symptoms reproduced; the kind cluster `tide` is available for repro during development but automated tests should be self-contained).
- Exact condition type/reason naming (follow existing condition conventions in api/v1alpha1 + controllers).
- Migration/cleanup handling for run-1 CRs already fail-marked in the live cluster (document the recipe or have resume --retry-failed handle them — planner's call).
- Whether reject cancels in-flight Jobs or lets them drain (pick the simpler-correct option; no state loss either way).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Gate semantics
- `docs/gates.md` — current gate flow doc; step 5 (§"End-to-end approve flow") ENCODES the finding-7 bug ("…patches `Succeeded`") and line ~99 sketches an `Approved` phase value superseded by D-04. This doc changes WITH the fix (GATE-02).
- `internal/gates/annotation.go` — ConsumeApprove/CheckApprove primitives (purity contract, T-04-G2 replay note).
- `internal/controller/milestone_controller.go` (~430–530) — gate-policy hook inside handleJobCompletion + ChildCount-gated succession (Plan 09-08 Defect B shape); the four ConsumeApprove sites are milestone_controller.go:474, phase_controller.go:387, plan_controller.go:487, task_controller.go:387.

### Reject/resume
- `internal/controller/plan_controller.go` — patchPlanFailed (~:478), Failed early-exit (~:222), ResumedByUser condition sites (:523, :552, :1016, comment :923).
- Run-1 recovery recipe (the behavioral spec for `resume --retry-failed`): `kubectl patch <kind> <name> --subresource=status --type=merge -p '{"status":{"phase":"","conditions":[]}}'` — reconciler then re-dispatches and sets ResumedByUser.

### Background
- Memory file `project_dogfood_run1_findings.md` (findings 1, 5, 7, 9a — symptoms and spend numbers for regression-test assertions).
- `.planning/REQUIREMENTS.md` — GATE-01..04, RESUME-01 (GATE-03 reworded + GATE-04 added per D-01/D-07).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `gates.EvaluatePolicy` / `gates.CheckApprove` / `gates.ConsumeApprove` (internal/gates/annotation.go): annotation primitives are sound; the bug is WHERE the consume result routes, not the primitives.
- ChildCount-gated succession (milestone_controller.go handleJobCompletion, Plan 09-08 Defect B): `expected == 0 → leaf-succeed; observed < expected → requeue; all-Succeeded → boundary push + succeed`. This is the succession logic D-03 makes authoritative.
- `ResumedByUser` condition pattern (plan_controller.go): template for the new `ApprovedByUser` condition (D-04) and the resume --retry-failed reset path (D-06).
- `spawnReporterIfNeeded` (idempotent, T-09-13): children materialize via reporter — D-02's "materialize children" half already exists; the new part is the dispatch hold in child reconcilers.

### Established Patterns
- Gate policy is read from `Project.Spec.Gates` per level via `gates.EvaluatePolicy` — keep human-gate policy out of the controller (CLAUDE.md): the descent-hold must be driven by the parent's gate config, not hardcoded.
- Conditions use `tideprojectv1alpha1.Reason*` constants; status writes via MergeFrom patches on the status subresource.
- The four reconcilers mirror each other's gate hooks — the fix lands symmetrically at all four sites.

### Integration Points
- Child reconcilers' dispatch sites need a parent-approval check (D-02) — likely a shared helper (parent lookup + gate policy + ApprovedByUser/AwaitingApproval state).
- `cmd/tide` approve/reject/resume verbs (CLI) change behavior per D-05/D-06/D-07.
- Dashboard status-chip mapping is Phase 15 (CUTS-05) — D-04's no-new-enum decision deliberately avoids creating new chip states.
- Phase 13's HALT-01 will point recovery at `tide resume --retry-failed` (D-06) — keep the flag's semantics general (any Failed level), not reject-specific.

</code_context>

<specifics>
## Specific Ideas

- The run-1 finding-7 symptom is the canonical regression scenario: Milestone gated `approve`, 5 Phase children materialized and incomplete; approving must NOT produce Milestone=Succeeded/Project=Complete.
- Run-1 finding-1 symptom for GATE-04: phase planners dispatched ~1s after the milestone hit AwaitingApproval, ~$0.64 each — the regression test asserts zero child planner Jobs exist while the parent is parked.
- gates.md step 5's exact sentence "advances the level to Succeeded" must be gone after GATE-02 (the verification grep from run-1 review discipline).

</specifics>

<deferred>
## Deferred Ideas

- Approval UX in dashboard (approve button beyond copy-to-clipboard) — out of scope; dashboard stays read-only per PROJECT.md.
- Per-level gate timeout / auto-approve-after-N-hours — new capability, not in any v1.0.1 requirement.

</deferred>

---

*Phase: 12-Gate Semantics + Reject/Resume*
*Context gathered: 2026-06-11*
