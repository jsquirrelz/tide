---
phase: 14-budget-enforcement-pricing
verified: 2026-06-12T01:01:51Z
status: gaps_found
score: 2/3 success criteria verified (criterion 2 partial — kubectl met, dashboard unmet)
overrides_applied: 0
gaps:
  - truth: "When the project budget cap is reached, a BudgetBlocked condition appears on the Project CR — visible via kubectl get project -o yaml AND reflected on the dashboard project node"
    status: partial
    reason: "The kubectl/CR half is fully implemented, wired at all five dispatch sites, and proven by passing envtest. The dashboard half has NO implementation — Phase 14 touched no dashboard file. ProjectNode.tsx renders only a StatusValue chip (no BudgetBlocked state in the enum); the dashboard conditions API (cmd/dashboard/api/tasks.go) surfaces Task.Status.Conditions only, never Project conditions, so a BudgetBlocked condition on the Project CR has no code path to the dashboard project node. The Phase 13 BillingHalt condition has the same pre-existing gap; this is not regression, but Phase 14 added nothing toward the dashboard half of its own success criterion."
    artifacts:
      - path: "dashboard/web/src/components/ProjectNode.tsx"
        issue: "Renders status: StatusValue only; no condition surfacing. StatusValue enum (StatusBadge.tsx:28-39) has no BudgetBlocked/blocked state."
      - path: "cmd/dashboard/api/tasks.go"
        issue: "Conditions API is Task-scoped (Task.Status.Conditions); the Project node receives no condition data."
    missing:
      - "A dashboard surface (project-node badge/chip or detail field) that reads the Project CR's BudgetBlocked condition and displays the blocked state."
      - "Backend exposure of Project.Status.Conditions to the project node (analogous to the existing Task conditions API), or extension of ProjectNodeData to carry a blocked flag."
human_verification:
  - test: "Install the chart in a cluster, drive a Project past its AbsoluteCapCents, then open the dashboard and inspect the project node."
    expected: "The project node visibly reflects the BudgetBlocked state (success criterion 2, dashboard half)."
    why_human: "Visual dashboard rendering cannot be verified programmatically; also confirms the end-to-end SSE/render path beyond the static-code gap already found."
  - test: "On a live cluster, run a planner+executor wave under a Project whose cap is about to trip; watch kubectl get jobs --all-namespaces after the cap breach is detected."
    expected: "No new Jobs dispatch after BudgetBlocked=True; in-flight overshoot is at most one estimate per concurrent reconcile, not wave-wide (criterion 3, runtime confirmation)."
    why_human: "Envtest proves the gate logic; real-cluster concurrent-reconcile timing (and the WR-06/WR-07/WR-09 accuracy windows) is best confirmed against a live manager with MaxConcurrentReconciles=16."
---

# Phase 14: Budget Enforcement + Pricing Verification Report

**Phase Goal:** The pricing table resolves current model IDs without warnings, budget-cap exhaustion is visible on the Project and dashboard, and in-flight overshoot past the cap is bounded
**Verified:** 2026-06-12T01:01:51Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Sessions on claude-opus-4-8, claude-fable-5, and other current model IDs log no `pricing: unknown model` lines — table covers all v1.0.1 IDs | ✓ VERIFIED | `internal/subagent/anthropic/pricing.go` carries a 6-entry table (fable-5, opus-4-8/4-7/4-6, sonnet-4-6, haiku-4-5) at D-01 cent values. Old wrong opus-4-7 `7500` rate gone (`grep -c 7500` = 0). `conservativeTier = priceTable["claude-fable-5"]` (line 117). The `pricing: unknown model` warning is single-sourced (pricing.go:137) and only fires on a table miss — known IDs never trip it. `go test ./internal/subagent/anthropic/... -run TestEstimatedCostCents` = ok; `go test ./pkg/dispatch/... -run TestParsePricingOverrides` = ok (override merge over `maps.Clone`, package var immutable). |
| 2 | Cap reached → `BudgetBlocked` condition on the Project CR, visible via kubectl AND reflected on the dashboard project node | ✗ FAILED (partial) | **kubectl half VERIFIED:** `ConditionBudgetBlocked`/`ReasonBudgetCapReached`/`ReasonBudgetCapCleared` constants exist; `setBudgetBlockedIfNeeded` (budget_blocked.go) is bidirectional (set on `IsCapExceeded`, clear on recovery); wired at 2 call sites in task_controller.go (gate line 373, post-RollUpUsage line 931) and `checkBudgetBlocked` holds at all 5 dispatch sites (milestone/phase/plan/project/task — `grep -l` = 5). Run-1 regression envtest asserts BudgetBlocked=True with Reason=BudgetCapReached via `meta.FindStatusCondition` and passes (full controller suite = ok, 57.9s). **dashboard half FAILED:** no dashboard file touched this phase; ProjectNode renders only a status chip, the conditions API is Task-scoped. See Gaps. |
| 3 | In-flight overshoot bounded to ≤ one wave's already-dispatched sessions — no new Jobs after cap breach detected | ✓ VERIFIED | Headroom gate (`HasHeadroom`, reservation.go) blocks at `spent+reserved+estimate >= cap`; nil-safe. Hold #4 returns `ctrl.Result{RequeueAfter: 30s}` and creates no Job. Executor Jobs carry `tideproject.k8s/estimated-cost` for restart rederivation (`RederiveReservations`, registered as a mgr.Add runnable at main.go:587). Regression envtest "reservation bound" proves a second task parks when 9000+600+600 ≥ 10000, and "no Job created" asserts via Consistently(3s) that no Job appears while blocked. Suite passes. (Accuracy caveats WR-06/07/08/09 below — bounding holds; margins are follow-up.) |

**Score:** 2/3 success criteria fully verified; criterion 2 partial (1 of 2 halves).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/subagent/anthropic/pricing.go` | 6-entry corrected table, conservativeTier=fable-5 | ✓ VERIFIED | All D-01 values present; 7500 gone; warning single-sourced |
| `pkg/dispatch/pricing.go` | Provider-agnostic PriceOverride + ParsePricingOverrides | ✓ VERIFIED | Exports present; validation rejects ≤0 input/output; tests green |
| `internal/budget/reservation.go` | Nil-safe ReservationStore + RederiveReservations | ✓ VERIFIED | sync.Map, ≥4 nil guards, label-rederive; PERSIST-02 honored |
| `internal/controller/budget_blocked.go` | check + bidirectional set/clear | ✓ VERIFIED | Set + idempotent + clear branch (ReasonBudgetCapCleared) all tested |
| `internal/controller/task_controller.go` | Hold #4 + headroom + reserve/settle wiring | ✓ VERIFIED | 2 setBudgetBlockedIfNeeded sites, 2 Reserve, 2 Settle/Release, 30s requeue |
| `internal/dispatch/podjob/jobspec.go` | EstimatedCostCents → estimated-cost label; PricingOverridesJSON env | ✓ VERIFIED | Label matches reservedCostLabel; jobspec tests green |
| `internal/controller/budget_blocked_regression_test.go` | run-1 envtest, ≥100 lines | ✓ VERIFIED | 347 lines, 3 labeled Describe blocks (cap-trip, reservation-bound, cap-raise recovery) |
| `cmd/manager/main.go` | Flags, startup validation, rederive runnable, Deps wiring | ✓ VERIFIED | Both flags; ParsePricingOverrides→os.Exit(1); RederiveReservations as mgr.Add runnable; Deps set |
| `charts/tide/values.yaml` | pricing.overrides + budget.reservePerDispatchCents | ✓ VERIFIED | `pricing:` key + `reservePerDispatchCents: 100` under single `budget:` (grep `^budget:` = 1) |
| `charts/tide/templates/deployment.yaml` | Both new manager args | ✓ VERIFIED | `--budget-reserve-per-dispatch-cents` + conditional `--pricing-overrides-json`; helm contract test green |
| `hack/check-pricing-drift.sh` | Fetch+parse+diff, 3 exit paths | ✓ EXISTS (with WR-01/02/03/IN-04 fragilities) | Script present, executable; advisory drift-quality warnings open |
| `.github/workflows/pricing-drift.yaml` | Weekly deduped issue, no auto-PR | ✓ VERIFIED (CR-01 fixed) | `DRIFT_EXIT=$?` capture corrected in commit 0fba8ca; `process.env` injection guard; WR-04 delimiter open (advisory) |
| `docs/releasing.md` | check-pricing-drift checklist line | ✓ EXISTS (WR-10 open) | Contains the line; step-4 grep check is mis-specified (advisory) |
| dashboard project-node BudgetBlocked surface | Reflect BudgetBlocked on project node | ✗ MISSING | No dashboard artifact created or modified this phase |

### Key Link Verification

| From | To | Via | Status |
|------|----|----|--------|
| 5 controllers | budget_blocked.go | checkBudgetBlocked hold after checkBillingHalt | ✓ WIRED (5/5 sites) |
| task_controller createDispatchJob/ensureJob | reservation.go | Reservations.Reserve at Job create | ✓ WIRED (2 sites) |
| task_controller handleJobCompletion | budget_blocked.go | setBudgetBlockedIfNeeded after RollUpUsage | ✓ WIRED (line 931) |
| deployment.yaml | main.go | --pricing-overrides-json / --budget-reserve-per-dispatch-cents | ✓ WIRED |
| main.go | reservation.go | RederiveReservations as mgr.Add runnable | ✓ WIRED (line 587) |
| pricing-drift.yaml | check-pricing-drift.sh | workflow step runs script, captures exit code | ✓ WIRED (CR-01 fixed) |
| Project CR BudgetBlocked condition | dashboard project node | (no link) | ✗ NOT_WIRED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Pricing table resolves all IDs, no unknown-model warning | `go test ./internal/subagent/anthropic/... -run TestEstimatedCostCents` | ok | ✓ PASS |
| Override parse/validation | `go test ./pkg/dispatch/... -run TestParsePricingOverrides` | ok | ✓ PASS |
| ReservationStore + rederive | `go test ./internal/budget/...` | ok | ✓ PASS |
| BudgetBlocked helpers (set/idempotent/clear) | `go test ./internal/controller/ -run 'TestCheckBudgetBlocked\|TestSetBudgetBlocked'` | ok | ✓ PASS |
| estimated-cost Job label | `go test ./internal/dispatch/podjob/...` | ok | ✓ PASS |
| run-1 regression envtest (criteria 2-kubectl + 3) | `go test ./internal/controller/ -run TestControllers` | ok (57.9s) | ✓ PASS |
| helm renders both flags | `go test ./test/integration/kind/ -run TestHelmDeploymentTemplate` | ok | ✓ PASS |
| No aggregate schedule fields (PERSIST-02) | `make verify-no-aggregates` | OK | ✓ PASS |
| Build sanity | `go build ./...` | clean | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|-------------|--------|----------|
| BUDGET-01 | 14-01, 14-04, 14-05 | Pricing table resolves current model IDs without `pricing: unknown model` | ✓ SATISFIED | 6-entry table, drift automation (CR-01 fixed), override path; tests green |
| BUDGET-02 | 14-02, 14-03, 14-05 | BudgetBlocked surfaces on Project, kubectl + dashboard visible | ⚠️ PARTIAL | kubectl/CR fully implemented + envtest-proven; dashboard surface absent |
| BUDGET-03 | 14-02, 14-03, 14-05 | In-flight overshoot bounded | ✓ SATISFIED | Headroom gate + reservation pre-charge + 30s park; regression envtest |

All three Phase-14 requirement IDs are claimed by plans and accounted for. No orphaned IDs (REQUIREMENTS.md maps BUDGET-01/02/03 → Phase 14 exclusively).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No TBD/FIXME/XXX debt markers, no TODO/HACK/PLACEHOLDER in modified Go files | ℹ️ Info | Completion is auditable. The `XXXXXX` mktemp template (drift script) is a literal, not a debt marker — covered by WR-01. |

### Code-Review Findings Weighed Against Success Criteria

- **CR-01 (critical, drift automation dead):** FIXED in commit 0fba8ca and verified present (`DRIFT_EXIT=$?` capture corrected). Not a gap.
- **WR-06 (settle-before-rollup), WR-07 (failure branches skip roll-up), WR-08 (HasHeadroom ignores rolling-window cap), WR-09 (RollUpUsage non-atomic):** Real, confirmed against code. These narrow the *accuracy* of cap accounting at concurrent-reconcile and failure-heavy margins, but do NOT negate the enforcement that satisfies criterion 3 — the BudgetBlocked condition + headroom gate + 30s park demonstrably prevent new-Job dispatch after cap breach (regression envtest). The run-1 wave-wide overshoot class is closed; residual overshoot is bounded to estimate-error-plus-window. Treated as acceptable follow-up (advisory), consistent with REVIEW.md's open-warning disposition.
- **WR-01/02/03/IN-04 (drift script fragilities), WR-04 (heredoc delimiter), WR-05 (per-Task condition never cleared — cosmetic), WR-10 (releasing.md grep mis-specified):** Advisory; none block a success criterion.

### Gaps Summary

Phase 14's core mechanics are sound and well-tested: the corrected 6-entry price table closes BUDGET-01, the bidirectional BudgetBlocked condition is stamped at all five dispatch sites and is kubectl-visible (BUDGET-02's first half), and the reservation pre-charge + headroom gate bound in-flight overshoot (BUDGET-03), all proven by a passing envtest regression that reproduces and closes the run-1 symptom. The CR-01 critical is fixed.

The single gap is the **dashboard half of success criterion 2**: "reflected on the dashboard project node." No Phase-14 plan listed a dashboard file in `files_modified`, and the codebase confirms there is no path from a Project CR's `BudgetBlocked` condition to the dashboard project node (ProjectNode renders a status chip only; the conditions API is Task-scoped). This is not addressed by any later milestone phase — Phase 15's CUTS-05 dashboard work only corrects the Complete/Pending status-chip mapping, not condition surfacing — so it is a real gap, not a deferral. Two human-verification items are also raised to confirm the runtime dashboard render and live-cluster overshoot bounding.

---

_Verified: 2026-06-12T01:01:51Z_
_Verifier: Claude (gsd-verifier)_
