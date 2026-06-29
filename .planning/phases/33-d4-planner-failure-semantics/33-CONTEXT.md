# Phase 33: D4 — Planner Failure Semantics - Context

**Gathered:** 2026-06-29
**Status:** Ready for planning

<domain>
## Phase Boundary

Insert a planner-failure check **before** the succeed check at the **phase** and **milestone** controllers: a planner that exits nonzero with zero children (`envReadOK && out.ExitCode != 0 && out.ChildCount == 0`) is marked `Failed`, not `Succeeded`, via a shared `isPlannerFailure` helper applied identically at both levels. This stops a failed planner from corrupting the planning DAG by falsely advancing its parent. Recovery is the existing `tide resume --retry-failed` verb — the guard patches a permanent `Failed` condition (status patch), never returns a Go error, so no controller-side retry storm fires.

**In scope:** PLANFAIL-01..04 — the fail-before-succeed guard at phase + milestone, the shared helper, new `patchPhaseFailed`/`patchMilestoneFailed` helpers (only `patchPlanFailed` exists today), the leaf non-regression (exitCode==0, childCount==0 still Succeeds), and recoverability via the existing `--retry-failed` walker. Plus the carried-in D3 sizing-policy doc-consistency decision (see D-04).

**Out of scope (other phases / explicitly excluded levels):**
- **Plan and project levels** are structurally NOT exposed to this false-succeed (see D-02) and are excluded by deliberate decision (see D-01). No code change at those two levels.
- D2/D1 adoption seam (Phase 31, complete), D3 dispatch concurrency cap mechanism (Phase 32, complete — only its sizing-policy doc debt is carried in here).
- Milestone constraint (v1.0.6): no new CRDs, no new go.mod dependencies, no new persistence surface.
</domain>

<decisions>
## Implementation Decisions

### Scope: which levels get the guard (DISCUSSED — load-bearing)

- **D-01 (phase + milestone ONLY):** Apply `isPlannerFailure` at the **phase** and **milestone** controllers only. Plan and project are deliberately excluded. The fix is added with an inline comment at the excluded levels (or in the shared helper's doc) documenting *why* they are excluded (the `matched > 0` protection in D-02), so a future reader doesn't "complete the set" by reflex.

- **D-02 (evidence — why phase + milestone are the only exposed levels):** The false-succeed exists because phase and milestone take a **direct `expected == 0 → patchXSucceeded` shortcut** that bypasses `gates.BoundaryDetected`, ignoring `out.ExitCode`:
  - `internal/controller/phase_controller.go:637` — `if expected == 0 { return r.patchPhaseSucceeded(...) }`
  - `internal/controller/milestone_controller.go:718` — `if expected == 0 { return r.patchMilestoneSucceeded(...) }`

  Plan and project succeed **only via `gates.BoundaryDetected`** (`internal/gates/boundary.go:66`), which returns `matched > 0` — i.e. **`false` for zero children**. So a zero-child planner can never drive plan/project to `Succeeded`:
  - Plan succeeds via `reconcileWaveMaterialization` (needs Tasks to Succeed; zero Tasks → `BoundaryDetected("Task")` false).
  - Project succeeds via `checkProjectComplete` → `BoundaryDetected("Milestone")` false; and `setBillingHaltIfNeeded` already fires at `project_controller.go:1387` on `exitCode != 0`.

  The roadmap's "mirroring the Phase-30 plan-level guard" / "across both controllers" wording is therefore exactly right: **both** = phase + milestone.

- **D-03 (the known residual at plan/project is a latent HUNG-RUNNING, not a false-succeed):** A zero-child *failed* planner at plan/project will leave the level stuck `Running` (no children ever appear, `BoundaryDetected` stays false). User's call: **do NOT fix this in Phase 33.** Rationale: (a) it does not corrupt the planning DAG (project is the root → no parent to falsely advance; plan's children are execution Tasks, not planning-DAG nodes), (b) it is visible/recoverable, not silent, and (c) project already has `setBillingHaltIfNeeded` covering the cost-failure case. Captured as a deferred idea, not phase scope.

### Carried-in D3 sizing-policy debt (DE-SELECTED by user → Claude's discretion, see below)

- **D-04 (recommendation — soften the chart wording + document the tradeoff; do NOT raise the default):** 32-REVIEW flagged that the D3 default `plannerConcurrency=4` is narrower than the chart comment's "size ≥ widest expected wave (≈6)" guidance. The inconsistency is in the **doc wording**, not the value — `4` was *deliberately* chosen in Phase 32 for single-node-kind safety (each planner pod = subagent + credproxy sidecar; leave headroom for the executor pool + system pods). Raising to 6 would undo that deliberate safety choice. **Recommended fix:** soften the chart's "≥ widest wave" comment in `charts/tide/values.yaml` to a per-workload tuning note, and add a one-line note that the single-node default intentionally trades throughput for safety (a wide milestone serializes — degraded throughput, NOT a deadlock; single-shot planner Jobs drain). The planner should confirm the exact comment text against the current `values.yaml`. This is a docs/comment-only change; no behavior change.

### Claude's Discretion (de-selected gray areas — recommendations the planner may refine)

- **D-05 (failure vocabulary):** Add a `Reason` constant for the planner-failure condition (e.g. `ReasonPlannerFailed` in `api/v1alpha2/shared_types.go`, alongside `ReasonWaveIntegrationFailed`) and new `patchPhaseFailed` / `patchMilestoneFailed` helpers mirroring the existing `patchPlanFailed` (`plan_controller.go:887`). Operator-facing message should name the cause concretely, e.g. *"planner exited nonzero (exitCode=N) with zero children; marked Failed to prevent false succession"*. Keep the `Failed` condition permanent (the recovery story is `--retry-failed`, not auto-retry).

- **D-06 (shared helper shape):** A package-level `isPlannerFailure(out <plannerEnvelopeStatus>, envReadOK bool) bool` in a shared controller file (mirrors the existing shared-helper pattern: `depgraph.go`, `failure_halt.go`, `billing_halt.go`), called identically at both the phase and milestone succession sites. The check is `envReadOK && out.ExitCode != 0 && out.ChildCount == 0`. **Ordering is load-bearing (PLANFAIL-03):** the fail-check must sit *before* the `expected == 0 → patchXSucceeded` branch so a genuine leaf (exitCode==0, childCount==0) still Succeeds.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 33: D4 — Planner Failure Semantics" — goal, success criteria, and the carried-in D3 sizing-policy debt block.
- `.planning/REQUIREMENTS.md` §"Planner Failure Semantics (D4)" — PLANFAIL-01..04 verbatim.
- `.planning/phases/32-d3-dispatch-concurrency-cap/32-REVIEW.md` — the sizing-policy advisory carried in as D-04 (and confirmation the other two Phase-32 advisories were already fixed at commit `91f7499`).

### Load-bearing code (the bug + the fix sites)
- `internal/controller/phase_controller.go:634-641` — the exposed direct `expected == 0 → patchPhaseSucceeded` shortcut; insert the fail-check before line 637.
- `internal/controller/milestone_controller.go:715-721` — the exposed direct `expected == 0 → patchMilestoneSucceeded` shortcut; insert the fail-check before line 718.
- `internal/controller/plan_controller.go:887` — existing `patchPlanFailed` helper to mirror for the two new `patchPhaseFailed`/`patchMilestoneFailed` helpers; `:794` `patchPlanSucceeded` for the success-side pattern.
- `internal/gates/boundary.go:66` — `BoundaryDetected` returns `matched > 0` (the `false`-on-zero-children property that protects plan/project — D-02). Do not change.
- `internal/controller/project_controller.go:1387,1404` — project-level succession via `checkProjectComplete`/`BoundaryDetected` + existing `setBillingHaltIfNeeded` (why project is not exposed — D-02).
- `cmd/tide/resume.go:184` — `retryFailedLevels` already walks Milestone/Phase/Plan/Task and resets any `Status.Phase=="Failed"`; PLANFAIL-04 recovery needs NO new code, just the guard to set `Status.Phase=Failed` via status patch. `cmd/tide/resume_test.go` for coverage shape.
- `api/v1alpha2/shared_types.go:199-203` — `Reason*` condition constants (where `ReasonPlannerFailed` would live, per D-05).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `patchPlanFailed` (`plan_controller.go:887`) — exact template for the two new `patchPhaseFailed`/`patchMilestoneFailed` helpers (sets `Status.Phase="Failed"` + `ConditionFailed`, returns `ctrl.Result` not error).
- `tide resume --retry-failed` (`cmd/tide/resume.go` `retryFailedLevels`) — already resets Failed Phases & Milestones; reuse as-is for PLANFAIL-04.
- Shared-helper file convention (`depgraph.go`, `failure_halt.go`, `billing_halt.go`) — home for the package-level `isPlannerFailure`.

### Established Patterns
- Both exposed sites share one shape (`if envReadOK { expected := out.ChildCount; if expected == 0 { return patchXSucceeded } ... }`). A single shared `isPlannerFailure(out, envReadOK)` called identically at both keeps the change DRY and symmetric (PLANFAIL-02).
- Permanent-`Failed`-via-status-patch (not Go error) recovery model — mirrors Phase 25's `failure_halt.go` and the plan-level `patchPlanFailed` (no auto-retry; operator-gated recovery).

### Integration Points
- The guard inserts at the `expected == 0` branch in each of `phase_controller.go` handleJobCompletion and `milestone_controller.go` handleJobCompletion, ordered *before* the succeed return (PLANFAIL-03).
- `setBillingHaltIfNeeded` already runs earlier in both handlers on `exitCode != 0`; the new guard is independent of and ordered after it (billing-halt classification stays; the new guard handles the zero-child terminal state).

</code_context>

<specifics>
## Specific Ideas

The bug shape was confirmed by direct code read, not a live experiment: phase/milestone short-circuit to `patchXSucceeded` on `expected == 0` regardless of `out.ExitCode`, while plan/project route through `BoundaryDetected` (which is `matched > 0`, hence safe on zero children). The planner may still add a kubectl/envtest observation as a verification gate, but the scope is settled by source.

Envtest matrix to lock the contract (mirrors the success-criteria table):
- phase: `exitCode=1, childCount=0` → `Failed` (PLANFAIL-01)
- milestone: `exitCode=1, childCount=0` → `Failed` (PLANFAIL-02)
- both: `exitCode=0, childCount=0` → still `Succeeded` (PLANFAIL-03, no regression)
- recovery: a Failed phase/milestone is cleared by `resumeRun(retryFailed=true)` (PLANFAIL-04)
</specifics>

<deferred>
## Deferred Ideas

- **Plan/project hung-Running on a zero-child failed planner (D-03):** convert the latent "stuck Running, never succeeds, never fails" state at plan/project into an explicit `Failed` (belt-and-suspenders, all 4 levels fail fast). Not a planning-DAG corruption, so out of scope for D4. Candidate for a future hardening pass if dogfood surfaces a real hung-Running stall.
- **Raising `plannerConcurrency` default beyond single-node safety** — only if/when TIDE targets multi-node clusters (tracked with the vNext OpenAI/multi-node infra milestone, which the ROADMAP already gates on "adequate multi-node infrastructure").

</deferred>

---

*Phase: 33-d4-planner-failure-semantics*
*Context gathered: 2026-06-29*
