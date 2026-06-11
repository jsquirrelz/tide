---
phase: 14
slug: budget-enforcement-pricing
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-11
---

# Phase 14 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing + Ginkgo v2 + Gomega (envtest) |
| **Config file** | `internal/controller/suite_test.go` (Ginkgo bootstrap) |
| **Quick run command** | `go test ./internal/budget/... ./internal/subagent/anthropic/... -count=1` |
| **Full suite command** | `make test-int-fast` |
| **Estimated runtime** | ~60 seconds (quick) / ~10 min (full) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/budget/... ./internal/subagent/anthropic/... -count=1`
- **After every plan wave:** Run `make test-int-fast`
- **Before `/gsd:verify-work`:** Full suite must be green (`make test` + `make test-int-fast`)
- **Max feedback latency:** 600 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | — | — | BUDGET-01 | — | N/A | unit | `go test ./internal/subagent/anthropic/... -run TestEstimatedCostCents -count=1` | ❌ W0 | ⬜ pending |
| TBD | — | — | BUDGET-02 | — | N/A | envtest | `go test ./test/integration/envtest/... --ginkgo.label-filter='phase14,budget-blocked' -timeout=10m` | ❌ W0 | ⬜ pending |
| TBD | — | — | BUDGET-03 | — | N/A | unit | `go test ./internal/budget/... -run TestReservation -count=1` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*(Planner fills concrete Task IDs when PLAN.md files are authored; behavior rows derived from RESEARCH.md "Phase Requirements → Test Map".)*

---

## Wave 0 Requirements

- [ ] Extend `internal/subagent/anthropic/pricing_test.go` — cases for new model IDs + corrected opus-4-7 price + conservativeTier reassignment
- [ ] `internal/budget/reservation_test.go` — unit tests for `ReservationStore` (Reserve/TotalReserved/Settle/Release, HasHeadroom, restart rederivation)
- [ ] `internal/controller/budget_blocked_regression_test.go` — envtest run-1 regression (cap $100 trips → BudgetBlocked on Project)
- [ ] `internal/controller/budget_blocked_test.go` — unit tests for `checkBudgetBlocked` / `setBudgetBlockedIfNeeded`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| BudgetBlocked condition visible on dashboard project node | BUDGET-02 | Dashboard renders Project conditions; visual check only (no chip-state changes this phase) | Trip cap in kind cluster, open dashboard, confirm condition surfaces on project node |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 600s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
