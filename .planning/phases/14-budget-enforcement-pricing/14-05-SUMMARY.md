---
phase: 14-budget-enforcement-pricing
plan: "05"
subsystem: controller
tags: [budget, helm, dispatch-gate, controller-runtime, go]
completed_date: "2026-06-12"
duration_seconds: 1800
tasks_completed: 3
tasks_total: 3
files_created: 0
files_modified: 8
dependency_graph:
  requires:
    - phase: 14-budget-enforcement-pricing
      provides: checkBudgetBlocked + setBudgetBlockedIfNeeded + ReservationStore from 14-02
    - phase: 14-budget-enforcement-pricing
      provides: TaskReconciler budget gate + reserve/settle lifecycle from 14-03
  provides:
    - internal/controller/{milestone,phase,plan,project}_controller.go: BudgetBlocked hold at all four planner dispatch sites
    - cmd/manager/main.go: --pricing-overrides-json and --budget-reserve-per-dispatch-cents flags + startup validation + RederiveReservations runnable + full Deps wiring
    - charts/tide/values.yaml: pricing.overrides stanza + budget.reservePerDispatchCents field
    - charts/tide/templates/deployment.yaml: --budget-reserve-per-dispatch-cents and conditional --pricing-overrides-json args
  affects:
    - All five dispatch sites (milestone/phase/plan/project/task reconcilers)
    - PodJobBackend.Run path
tech_stack:
  added: []
  patterns:
    - "BudgetBlocked hold mirrors checkBillingHalt insertion pattern exactly (after BillingHalt, same return arity per site)"
    - "Startup validation of pricing JSON before any component construction (fail-fast, T-14-01/ASVS V5)"
    - "RederiveReservations as manager.Runnable post-cache-warm (same NeedLeaderElection/cache-warm pattern as budget.PreCharge)"
    - "Conditional helm arg rendering: pricing.overrides flag only rendered when map is non-empty"
key_files:
  created: []
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/dispatch/podjob/backend.go
    - cmd/manager/main.go
    - charts/tide/values.yaml
    - charts/tide/templates/deployment.yaml
    - test/integration/kind/projects_pvc_test.go
decisions:
  - "PricingOverridesJSON added to each planner reconciler struct as a direct field (not a nested Deps struct) to mirror the existing HelmProviderDefaults/CredproxyImage precedent — no new abstraction layer needed"
  - "RederiveReservations registered as a manager.Runnable (not called synchronously) to mirror budget.PreCharge and ensure the informer cache is warm before the List call"
  - "pricing.overrides helm arg is conditional (if non-empty map) so the default empty-override case renders no flag, avoiding YAML-quoting churn — manager default is empty string meaning no overrides"
  - "budget.reservePerDispatchCents lives INSIDE the existing budget: block as a sibling of defaults: — chart ADDITIVE constraint preserved (grep -c '^budget:' returns 1)"
requirements:
  - BUDGET-01
  - BUDGET-02
  - BUDGET-03
---

# Phase 14 Plan 05: Planner dispatch gates + operator config wiring Summary

BudgetBlocked hold rolled out to all four planner dispatch sites; operator tuning via --pricing-overrides-json + --budget-reserve-per-dispatch-cents flags with startup validation; RederiveReservations runnable; additive Helm chart surface with helm-template contract tests.

## Tasks Completed

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1 | BudgetBlocked hold at four planner dispatch sites + PricingOverridesJSON | df117b5 | milestone/phase/plan/project controllers + backend.go |
| 2 | main.go flags, validation, RederiveReservations runnable, Deps wiring | 9cfcf68 | cmd/manager/main.go |
| 3 | Chart pricing/budget additive surface + helm-template contract tests | 4fb43fd | values.yaml + deployment.yaml + projects_pvc_test.go |

## What Was Built

### Task 1: BudgetBlocked hold at four planner dispatch sites

Inserted `checkBudgetBlocked(earlyProject) && !budget.IsBypassed(earlyProject, time.Now())` holds immediately after each existing `checkBillingHalt` block in:

- `milestone_controller.go` — returns `ctrl.Result{RequeueAfter: 30s}, nil`
- `phase_controller.go` — returns `ctrl.Result{RequeueAfter: 30s}, nil`
- `plan_controller.go` — returns `ctrl.Result{RequeueAfter: 30s}, true, nil` (three-value arity)
- `project_controller.go` — returns `ctrl.Result{RequeueAfter: 30s}, nil`

Each hold has the plan-specified doc comment confirming it is separate from BillingHalt. No per-level condition written — operator signal is the single Project BudgetBlocked condition.

Added `PricingOverridesJSON string` field to each of the four reconciler structs (`MilestoneReconciler`, `PhaseReconciler`, `PlanReconciler`, `ProjectReconciler`) and wired it into each reconciler's `BuildOptions` at the planner Job construction site.

Added `PricingOverridesJSON string` field to `PodJobBackend` and wired it in `Run()` BuildOptions.

### Task 2: main.go operator config wiring

Two new flags registered (exact names per plan interfaces block):
- `--pricing-overrides-json` (default `""`) — JSON map of model-ID to price overrides
- `--budget-reserve-per-dispatch-cents` (default `100`) — flat per-dispatch reservation estimate

Startup validation (T-14-01/ASVS V5): when `pricingOverridesJSON` is non-empty, calls `pkgdispatch.ParsePricingOverrides`; on error writes to stderr and calls `os.Exit(1)`. Also exits on negative `budgetReservePerDispatchCents`. Validation fires before any component construction — tested against the worktree binary.

`reservationStore := budget.NewReservationStore()` created; registered as a `manager.Runnable` (step 9.b) that calls `budget.RederiveReservations(ctx, mgr.GetClient(), reservationStore)` after cache sync. Logs `totalReservedCents` at startup. Mirrors the `budget.PreCharge` runnable pattern exactly.

`PricingOverridesJSON` wired into `PodJobBackend`, all four planner reconcilers, and `TaskReconcilerDeps` (alongside `Reservations` and `ReserveEstimateCents`).

### Task 3: Additive chart surface + contract tests

`values.yaml` (ADDITIVE ONLY per CLAUDE.md):
- New `pricing:` top-level stanza after `rateLimits:` with `overrides: {}` and full comment block explaining cents/MTok keys and --pricing-overrides-json transport
- `budget.reservePerDispatchCents: 100` added as sibling of `defaults:` inside the EXISTING `budget:` block (verified: `grep -c '^budget:' values.yaml` returns 1)

`deployment.yaml`:
- `--budget-reserve-per-dispatch-cents={{ .Values.budget.reservePerDispatchCents | default 100 }}` always rendered
- `--pricing-overrides-json={{ .Values.pricing.overrides | toJson }}` rendered conditionally inside `{{- if .Values.pricing.overrides }}`

`test/integration/kind/projects_pvc_test.go`: two new plain go-tests:
- `TestHelmDeploymentTemplateBudgetReserveDefaultArg` — asserts default render contains `--budget-reserve-per-dispatch-cents=100` and NOT `--pricing-overrides-json`
- `TestHelmDeploymentTemplatePricingOverridesArg` — asserts render with `--set pricing.overrides.claude-test-model.*` contains `--pricing-overrides-json` with `claude-test-model`

Both pass. All 6 `TestHelmDeploymentTemplate*` tests pass.

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — all fields carry real operator-sourced values from flags.

## Threat Flags

No new threat surface beyond what the plan's threat model documents (T-14-01 mitigated by startup validation, T-14-12/T-14-05/T-14-SC accepted). No new network endpoints, auth paths, or file access patterns introduced.

## Self-Check: PASSED
