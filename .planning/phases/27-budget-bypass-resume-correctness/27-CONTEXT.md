# Phase 27: Budget-Bypass Resume Correctness - Context

**Gathered:** 2026-06-18
**Status:** Ready for planning
**Source:** ROADMAP success criteria (locked) + one fix-shape decision resolved with the operator

<domain>
## Phase Boundary

A budget-halted `Project` must resume cleanly: clearing a budget halt resumes at
`Running` (not `Pending`), no workspace re-init / re-clone fires when the workspace is
already initialized, planning cost rolls up exactly once across a halt→resume cycle,
and raising the absolute cap alone clears the halt without the rolling-window cap
immediately re-halting. The `2a5e0dc` planner-completion ordering fix gets regression
coverage.

**Out of scope (do NOT touch):** the plan/envelope import path — Phase 28 owns it.
`charts/tide/values.yaml` is a FIXED contract. No CRD schema *version* bump (additive
`+optional` status fields only).
</domain>

<decisions>
## Implementation Decisions

### BYPASS-01 — Bypass resumes at Running
- **D-01:** Clearing a budget halt sets `Status.Phase = PhaseRunning` (not `PhasePending`).
  Root cause: `project_controller.go:1257`. `PhasePending` re-enters `reconcileProjectPhase2`
  Step 3 init-Job dispatch (TTL-GC'd init Job → always `IsNotFound` → workspace-wiping re-init).
  Belt-and-suspenders: guard the Step 3 init-Job dispatch on `Status.Git.BranchName == ""`.

### BYPASS-02 — Durable clone-idempotency guard
- **D-02:** Add a durable `Status.Git.CloneComplete bool` (`+optional`, `omitempty`) status
  field. Gate clone-Job dispatch (`project_controller.go:549-571`) on `!CloneComplete` rather
  than reporter/clone-Job presence (TTL = 300s, so Job-presence is not GC-safe). Set
  `CloneComplete=true` at clone-success detection (first task must grep `existingClone.Status`
  after line 571 to locate the exact success site — RESEARCH open question A3).

### BYPASS-03 — Durable rollup-once marker
- **D-03:** Add a durable `Status.Budget.PlannerRolledUpUID string` (`+optional`, `omitempty`)
  status field. In `handleProjectJobCompletion` (`project_controller.go:1145-1182`), check
  `PlannerRolledUpUID != jobName` before `budget.RollUpUsage`, and patch it to `jobName`
  after. Replaces the reporter-Job-existence `isFirstCompletion` signal (TTL-GC unreliable).
  Marker key = deterministic planner job name `tide-project-<uid>-1` (`project_controller.go:955`).
  **Never clear `PlannerRolledUpUID` on bypass** — it must persist across halt→resume or the
  next resume double-counts.

### BYPASS-04 — Bypass acknowledges spend (OPERATOR-CHOSEN, thorough fix)
- **D-04:** A budget bypass records the spend at bypass time as a baseline; re-halt fires
  only on **new** spend that crosses a cap *after* the bypass — not on the already-incurred
  cost that triggered the original halt. This literally satisfies the criterion: raising the
  absolute cap alone (or just resuming) makes the resume stick; the rolling-window cap does
  not immediately re-halt on the same already-spent amount.
  - **This OVERRIDES RESEARCH.md Pattern 4 / assumption A2** (the "Fix A: TTL-bypass-form +
    docs only, no logic change" recommendation). The operator explicitly chose the
    root-cause behavior fix over the documentation/ergonomic workaround.
  - Implementation must audit every `budget.IsCapExceeded` call-site (`internal/budget/cap.go:44-57`;
    also called by TaskReconciler) so the acknowledged-spend baseline is scoped to the
    bypass/resume path and does not silently change global cap semantics.
  - Carry the observability improvement along: the re-halt condition / Event message must name
    WHICH cap fired (`AbsoluteCapReached` vs `RollingWindowCapReached`) with current spend +
    both cap values — `project_controller.go:1280-1284` currently only cites `AbsoluteCapCents`.

### BYPASS-05 — Ordering regression coverage
- **D-05:** Verify the existing QQH-01 envtest in `project_planner_completion_test.go`
  (committed in `2a5e0dc`) is GREEN — it asserts reporter-Job spawn AND planner cost rollup
  while the planner Job still exists. Add a companion TTL-GC scenario (reporter Job absent /
  GC'd) proving the durable `PlannerRolledUpUID` marker (D-03) still rolls up exactly once.

### CRD regeneration
- **D-06:** Adding the two new status fields (D-02, D-03) to `api/v1alpha2/project_types.go`
  requires `make manifests && make generate`. Fields are `+optional` with `omitempty` —
  backward-compatible, zero-value defaults are safe (pre-fix projects fall back to current
  behavior). No CRD schema version bump.

### Claude's Discretion
- Test file organization (new file vs new `Describe` block in existing files).
- Exact baseline representation for D-04 (e.g. a `Status.Budget.BypassBaselineCents` field
  vs reusing an existing spend snapshot) — choose the minimal durable representation
  consistent with CRD-`.status`-only persistence.
- Whether the BYPASS-01 init-Job `BranchName` guard is a separate task or folded into D-01.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Root-cause code paths
- `internal/controller/project_controller.go` — `handleBudgetGate` (~1237-1290, bug at :1257),
  `reconcileProjectPhase2` (309-365), clone dispatch (549-571), `handleProjectJobCompletion`
  (1145-1182), planner job name (:955)
- `internal/budget/cap.go` — `IsCapExceeded` / `IsBypassed` / `ConsumeBypass` (1-106)
- `api/v1alpha2/project_types.go` — `GitStatus` (234-250), `BudgetStatus` (252-269)

### Test conventions / helpers
- `internal/controller/project_planner_completion_test.go` — QQH-01 (BYPASS-05 baseline)
- `internal/controller/project_controller_test.go:505-560` — existing bypass test (extend for BYPASS-01)
- `internal/controller/budget_blocked_regression_test.go:51` — `stampBudgetSpend` helper
- `internal/controller/milestone_controller_test.go` — `makeFakeJobTerminal` helper
- `internal/controller/suite_test.go` — envtest wiring

### Project rules
- `CLAUDE.md` — anti-patterns, verification protocols, `values.yaml` fixed contract
- `README.md` — failure/resumption semantics (resumption = indegree map + completed-set)
- `.planning/phases/27-.../27-RESEARCH.md` — full code investigation + Validation Architecture
</canonical_refs>

<specifics>
## Specific Ideas

- Planner job name marker key: `tide-project-<uid>-1` (deterministic).
- TTLs that make Job-presence guards unreliable: clone/reporter = 300s, planner = 600s.
- BYPASS-05 must first *confirm GREEN* before adding the TTL-GC companion scenario.
</specifics>

<deferred>
## Deferred Ideas

- Plan/envelope import-path correctness — Phase 28 (IMPORT-01..03). Explicitly NOT this phase.
</deferred>

---

*Phase: 27-budget-bypass-resume-correctness*
*Context gathered: 2026-06-18 — ROADMAP-locked criteria + BYPASS-04 fix-shape decision*
