---
phase: 27-budget-bypass-resume-correctness
verified: 2026-06-18T20:30:00Z
status: passed
gate_decision: APPROVED
score: 5/5 must-haves verified
overrides_applied: 0
requirements_coverage:
  BYPASS-01: satisfied
  BYPASS-02: satisfied
  BYPASS-03: satisfied
  BYPASS-04: satisfied
  BYPASS-05: satisfied
review_fixes_verified:
  CR-01: present   # tally.go MaybeResetWindow zeroes BypassBaselineCents + CR-01 envtest
  WR-01: present   # BYPASS-03 test deletes reporter Job between calls (marker is sole guard)
  WR-02: present   # BYPASS-04 resume-sticks re-drives Reconcile inside Consistently
  WR-03: present   # terminal-failed clone Job re-dispatches via ConditionCloneFailed
deferred_findings: [IN-01, IN-03, IN-04]  # non-blocking robustness follow-ups
---

# Phase 27: Budget-Bypass Resume Correctness — Verification Report

**Phase Goal:** A budget-halted Project resumes at `Running` without re-initializing the workspace or double-counting planning cost, cap-raise ergonomics no longer require raising both caps in lockstep, and the `2a5e0dc` planner-completion ordering fix has regression coverage — all without touching the import path.
**Verified:** 2026-06-18T20:30:00Z
**Status:** passed — **gate_decision: APPROVED**
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (success criterion) | Status | Evidence |
|---|---------------------------|--------|----------|
| 1 | Clearing a budget halt resumes at `Running` (not `Pending`) — no re-init/re-clone when `BranchName` is set | ✓ VERIFIED | `project_controller.go:1333-1337` branches to `PhaseRunning` iff `Status.Git.BranchName != ""`; init-Job guard at `:345`. Test `project_controller_test.go:565` asserts `Phase==Running` after bypass on initialized project. |
| 2 | Resume never re-dispatches the clone Job — guard is durable `CloneComplete`, not Job existence | ✓ VERIFIED | `project_controller.go:567` dispatch gated on `!CloneComplete && IsNotFound`; `:590` sets `CloneComplete=true` ONLY on `Succeeded>0` (never at dispatch). Tests: clone-idempotency Spec 1 (`:66` no re-clone when `CloneComplete=true`) + Spec 2 (`:134` flips true on Succeeded). |
| 3 | Planning cost rolls up exactly once across halt→resume — durable `PlannerRolledUpUID` prevents double-count after reporter GC | ✓ VERIFIED | `project_controller.go:1239-1253` gates rollup on `PlannerRolledUpUID != plannerJobName`, marker set only after successful `RollUpUsage`, never cleared on bypass. Test `project_planner_completion_test.go:142-181` deletes the reporter Job between the two `handleProjectJobCompletion` calls (WR-01) so the marker is the SOLE guard. |
| 4 | Raising the absolute cap alone clears a halt without the rolling-window cap immediately re-halting | ✓ VERIFIED | `project_controller.go:1338` records `BypassBaselineCents=CostSpentCents` at bypass; `:1362-1363` `newSpendSinceBypass` guard suppresses re-halt on already-incurred spend. CR-01 fix: `tally.go:140` zeroes `BypassBaselineCents` on window reset (prevents stale-baseline halt suppression). Tests: `project_controller_test.go:653` (raise-cap resume stays Running, WR-02 re-drives Reconcile) + `:831` CR-01 (window reset → new-window overspend re-halts). |
| 5 | Envtest locks the `2a5e0dc` ordering fix: reporter spawns AND planner cost rolls up while planner Job still exists | ✓ VERIFIED | `project_planner_completion_test.go:277-351` (QQH-01 primary): makes planner Job terminal WITHOUT deleting it (`:315`), reconciles, asserts (a) `tide-reporter-<uid>` spawns AND (b) `CostSpentCents >= plannerCost`. Control spec at `:355` proves a still-Running Job leaves reporter absent + budget 0. |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `api/v1alpha2/project_types.go` | 3 durable status fields | ✓ VERIFIED | `CloneComplete` (`:255`), `PlannerRolledUpUID` (`:280`), `BypassBaselineCents` (`:286`), all `+optional`/`omitempty`, no version bump |
| `api/v1alpha2/shared_types.go` | `CloneComplete` on GitStatus | ✓ VERIFIED | Field added (7-line additive change) |
| `config/crd/bases/...projects.yaml` | regenerated CRD | ✓ VERIFIED | `cloneComplete` (`:1106`), `plannerRolledUpUID` (`:1018`), `bypassBaselineCents` (`:1006`) |
| `internal/controller/project_controller.go` | bypass/clone/rollup guards | ✓ VERIFIED | All four guards present + WR-03 terminal-failed clone arm (`:606-624`) |
| `internal/budget/tally.go` | CR-01 baseline reset | ✓ VERIFIED | `MaybeResetWindow` zeroes `BypassBaselineCents` (`:140`) |
| `internal/budget/cap.go` | `IsCapExceeded` unchanged + doc note | ✓ VERIFIED | Logic untouched (ORs both caps `:54,58`); only doc note added |
| Test files (4) | RED-first regression coverage | ✓ VERIFIED | All specs present and GREEN (see Behavioral Spot-Checks) |

### Key Link Verification

| From | To | Via | Status |
|------|----|----|--------|
| `handleBudgetGate` bypass-clear | `Status.Git.BranchName` | conditional target phase | ✓ WIRED (`:1333`) |
| clone dispatch | `Status.Git.CloneComplete` | durable dispatch guard | ✓ WIRED (`:567` / set `:590`) |
| `handleProjectJobCompletion` | `Status.Budget.PlannerRolledUpUID` | rollup idempotency guard | ✓ WIRED (`:1241`) |
| `handleBudgetGate` re-halt | `Status.Budget.BypassBaselineCents` | `newSpendSinceBypass` + reset on window roll | ✓ WIRED (`:1362` / reset `tally.go:140`) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Build | `go build ./...` | exit 0 | ✓ PASS |
| Vet | `go vet ./internal/controller/... ./internal/budget/... ./api/v1alpha2/...` | exit 0 | ✓ PASS |
| Budget envtests | `go test ./internal/budget/...` | `ok` | ✓ PASS |
| Controller envtests | `go test ./internal/controller/...` | `ok 79.764s` | ✓ PASS |
| Ginkgo suite | controller `-v` summary | `Ran 147 of 147 Specs ... SUCCESS! — 147 Passed | 0 Failed | 0 Pending | 0 Skipped` | ✓ PASS |
| api/v1alpha2 (regression) | `go test ./api/v1alpha2/...` | `ok 0.563s` | ✓ PASS |

The `TestDogfoodManifests_*` failure flagged in the verification method as pre-existing did **not** reproduce in the committed tree (`api/v1alpha2` is `ok`); the offending dogfood fixture lives only in the untracked working-tree dir `examples/projects/dogfood/salvage-20260618/` and is unrelated to Phase 27. Phase 27's only `api/v1alpha2` changes are the 3 additive status fields. Not counted against this phase.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| BYPASS-01 | 27-02 | Resume at Running, no re-init/re-clone | ✓ SATISFIED | Truth #1 |
| BYPASS-02 | 27-01/02 | Durable CloneComplete sentinel (TTL-safe) | ✓ SATISFIED | Truth #2 |
| BYPASS-03 | 27-01/03 | Planner Usage rolls up once; durable marker | ✓ SATISFIED | Truth #3 |
| BYPASS-04 | 27-04 | Single resume / raise-abs-cap clears halt | ✓ SATISFIED | Truth #4 + CR-01 fix |
| BYPASS-05 | 27-01/03 | Ordering regression coverage (2a5e0dc) | ✓ SATISFIED | Truth #5 |

All 5 declared requirement IDs map to satisfied truths. No orphaned requirements (REQUIREMENTS.md `:59-63` lists exactly BYPASS-01..05 for Phase 27).

### Code-Review Fixes Verified In Code

| Finding | Severity | Fix present? | Evidence |
|---------|----------|--------------|----------|
| CR-01 stale `BypassBaselineCents` defeats halt | BLOCKER | ✓ | `tally.go:140` zeroes baseline on reset; CR-01 envtest `project_controller_test.go:831` proves new-window overspend re-halts |
| WR-01 BYPASS-03 test didn't exercise marker | WARNING | ✓ | Test deletes reporter Job between calls (`project_planner_completion_test.go:153-165`) |
| WR-02 resume-sticks used static `Consistently` | WARNING | ✓ | `project_controller_test.go:729-731` re-drives `Reconcile` per sample |
| WR-03 terminal-failed clone stalls on TTL | WARNING | ✓ | `project_controller.go:606-624` deletes failed Job + `ConditionCloneFailed`, requeues |

Fix commits confirmed on `main`: `3a42a0e` (CR-01), `5d04779` (WR-01), `ffb091b` (WR-02), `9f5cad7` (WR-03), `06dc6eb` (review-resolved).

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
|------|---------|----------|--------|
| (none) | No `TBD`/`FIXME`/`XXX` markers in any modified non-test file | — | Clean |

Deferred Info findings IN-01 (fixed-width prefix matching in test cleanup), IN-03 (patch-pattern fragility note), IN-04 (rollup marker still nested in `isFirstCompletion` rather than the sole gate) are non-blocking robustness follow-ups acknowledged in 27-REVIEW.md. IN-04 is the only one that touches the rollup-once invariant: the marker prevents the *double-count* (criterion #3, which is what was required and is tested); the theoretical *missed* rollup it describes is a pre-existing `isFirstCompletion` behavior the plan deliberately preserved, and does not regress this phase's goal.

### Goal Constraint: "without touching the import path"

✓ Honored. `git diff b48522c..HEAD` touched only `api/v1alpha2/`, `config/crd/`, `internal/controller/`, `internal/budget/`, and planning docs — no `internal/subagent/`, prompt-template, or module-import-path files changed.

### Human Verification Required

None. All five success criteria are mechanically verifiable in code and locked by GREEN envtests; no visual/runtime/external-service behavior is in scope for this corrective phase.

### Gaps Summary

No gaps. All 5 success criteria are met in the actual codebase and each is locked by a passing test that genuinely exercises the durable-flag guard it protects (not a tautology that would pass with the fix deleted). The one BLOCKER surfaced during the phase (CR-01) is fixed at root in `tally.go` and proven by a dedicated CR-01 envtest. Build/vet clean; controller + budget envtest packages GREEN (147/147 Ginkgo specs, 0 failed/pending/skipped).

---

_Verified: 2026-06-18T20:30:00Z_
_Verifier: Claude (gsd-verifier)_
