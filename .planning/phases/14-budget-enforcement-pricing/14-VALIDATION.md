---
phase: 14
slug: budget-enforcement-pricing
status: planned
nyquist_compliant: true
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
| **Quick run command** | `go test ./internal/budget/... ./internal/subagent/anthropic/... ./pkg/dispatch/... -count=1` |
| **Full suite command** | `make test` (covers internal/controller envtest suite) + `make test-int-fast` |
| **Estimated runtime** | ~60 seconds (quick) / ~10 min (full) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/budget/... ./internal/subagent/anthropic/... ./pkg/dispatch/... -count=1`
- **After every plan wave:** Run `make test` (the BudgetBlocked regression envtest lives in `internal/controller/`, which runs under `make test`, NOT under `make test-int-fast`)
- **Before `/gsd:verify-work`:** Full suite green (`make test` + `make test-int-fast`) AND `go test ./test/integration/kind/ -run TestHelmDeploymentTemplate -count=1` (Phase-7 lesson: plain go-tests in that package fail `make test-int` even when Ginkgo is green)
- **Max feedback latency:** 600 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 14-01 T1 | 14-01 | 1 | BUDGET-01 | — | N/A | unit | `go test ./internal/subagent/anthropic/... -run TestEstimatedCostCents -count=1` | ❌ W0 (extends pricing_test.go in-task) | ⬜ pending |
| 14-01 T2 | 14-01 | 1 | BUDGET-01 | T-14-01 | rejects zero/negative cent overrides | unit | `go test ./pkg/dispatch/... -run TestParsePricingOverrides -count=1` | ❌ W0 (created in-task) | ⬜ pending |
| 14-01 T3 | 14-01 | 1 | BUDGET-01 | T-14-02 | per-instance clone, no package-var mutation | unit + race | `go test -race ./internal/subagent/anthropic/... ./internal/dispatch/podjob/... -count=1` | ❌ W0 (extends in-task) | ⬜ pending |
| 14-02 T1 | 14-02 | 1 | BUDGET-02 | — | N/A | build | `go build ./api/... && make verify-no-aggregates` | ✅ | ⬜ pending |
| 14-02 T2 | 14-02 | 1 | BUDGET-03 | T-14-06 | in-process only, label-rederivable, nil-safe | unit + race | `go test -race ./internal/budget/... -run 'TestReservation\|TestRederiveReservations' -count=1` | ❌ W0 (created in-task) | ⬜ pending |
| 14-02 T3 | 14-02 | 1 | BUDGET-02 | T-14-08 | bidirectional set/clear prevents permanent park | unit | `go test ./internal/controller/ -run 'TestCheckBudgetBlocked\|TestSetBudgetBlocked' -count=1` | ❌ W0 (created in-task) | ⬜ pending |
| 14-03 T1 | 14-03 | 2 | BUDGET-03 | — | N/A | unit | `go test ./internal/dispatch/podjob/... -count=1` | ✅ (extends jobspec_test.go) | ⬜ pending |
| 14-03 T2 | 14-03 | 2 | BUDGET-02, BUDGET-03 | T-14-06 | gate parks, never fails; bypass honored | unit | `go test ./internal/controller/ -short -count=1 -timeout 300s` | ✅ | ⬜ pending |
| 14-03 T3 | 14-03 | 2 | BUDGET-02, BUDGET-03 | T-14-08 | run-1 regression + bounded overshoot + cap-raise recovery | envtest | `make test` | ❌ W0 (created in-task) | ⬜ pending |
| 14-04 T1 | 14-04 | 2 | BUDGET-01 | — | fetch-failure ≠ drift (exit 2 vs 1) | script | `bash -n hack/check-pricing-drift.sh && ./hack/check-pricing-drift.sh` | ❌ W0 (created in-task) | ⬜ pending |
| 14-04 T2 | 14-04 | 2 | BUDGET-01 | T-14-09, T-14-10 | env-passed issue body, minimal token scope | yaml-lint | `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/pricing-drift.yaml'))"` | ❌ W0 (created in-task) | ⬜ pending |
| 14-05 T1 | 14-05 | 3 | BUDGET-02 | — | hold after billing halt at all 4 planner sites | unit | `go test ./internal/controller/ -short -count=1 -timeout 300s` | ✅ | ⬜ pending |
| 14-05 T2 | 14-05 | 3 | BUDGET-01, BUDGET-03 | T-14-01 | startup fail-fast on invalid overrides | build | `go build ./cmd/manager && go vet ./cmd/manager` | ✅ | ⬜ pending |
| 14-05 T3 | 14-05 | 3 | BUDGET-01, BUDGET-03 | — | additive chart contract pinned | helm-template go-test | `go test ./test/integration/kind/ -run TestHelmDeploymentTemplate -count=1` | ✅ (extends projects_pvc_test.go) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

All Wave-0 test scaffolds are created inside the task that needs them (test + implementation in the same commit, tests authored first where the task says so):

- [ ] Extend `internal/subagent/anthropic/pricing_test.go` — new model IDs + corrected opus-4-7 + conservativeTier + override merge (14-01 T1/T3)
- [ ] `pkg/dispatch/pricing_test.go` — ParsePricingOverrides validation (14-01 T2)
- [ ] `internal/budget/reservation_test.go` — ReservationStore + rederivation + nil-receiver (14-02 T2)
- [ ] `internal/controller/budget_blocked_test.go` — checkBudgetBlocked / setBudgetBlockedIfNeeded incl. clear path (14-02 T3)
- [ ] `internal/controller/budget_blocked_regression_test.go` — envtest run-1 regression (14-03 T3)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| BudgetBlocked condition visible on dashboard project node | BUDGET-02 | Dashboard renders Project conditions; visual check only (no chip-state changes this phase — chip mapping is Phase 15) | Trip cap in kind cluster, open dashboard, confirm condition surfaces on project node |
| pricing-drift workflow opens a deduped issue | BUDGET-01 (D-03) | Requires GitHub Actions execution + issues API | After merge: `gh workflow run pricing-drift` and confirm issue behavior on a forced drift |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 600s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner sign-off 2026-06-11 (plans 14-01..14-05)
