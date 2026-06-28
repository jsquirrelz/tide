---
phase: 31-d2-d1-adoption-lifecycle-seam
verified: 2026-06-28T19:20:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
human_verification: []
---

# Phase 31: D2+D1 — Adoption Lifecycle Seam Verification Report

**Phase Goal:** Close the D2 (adoption lifecycle advance) and D1 (cost rollup idempotency) code-level defects on TIDE's adoption path — an adopted Project advances Initialized→Running with zero project-planner Jobs, accrues budget correctly through child planners, the budget cap halts it, and rollup is exactly-once across reporter-Job TTL-GC; the normal lifecycle is unchanged and the suppressed project-planner never re-dispatches on cache miss.
**Verified:** 2026-06-28T19:20:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (ROADMAP success criteria + plan must-haves) | Status | Evidence |
|---|----|----|----|
| 1 | ADOPT-01: adopted Project (ImportComplete=True, owned Milestone) advances Initialized→Running with **zero** role=project-planner Jobs | ✓ VERIFIED | `project_controller.go:1129-1168` first-time stamp arm sets `Phase=Running` + `ConditionProjectPlannerSuppressed=True` in one `Status().Patch(MergeFrom)` and returns before `PlannerPool.Acquire`. Envtest `adoption_lifecycle_test.go:232-260` asserts Phase=Running, suppression True/Reason=AdoptionComplete, `listPlannerJobsForProject(...).To(BeEmpty())`. Verifier-run: PASS. |
| 2 | ADOPT-02: adopted Project accrues CostSpentCents/TokensSpent as child planners complete (rollup fires under adoption path) | ✓ VERIFIED | Child rollups at `milestone_controller.go:593-607`, `phase_controller.go:524-538`, `plan_controller.go:599-613` are unconditional w.r.t. `ImportSource` (no skip-guard; the three `ImportSource` checks are dispatch-hold only — confirmed by grep). Envtest `child_rollup_idempotency_test.go` asserts CostSpentCents/TokensSpent increase at all three levels. Verifier-run: PASS. |
| 3 | ADOPT-03: `budget.absoluteCapCents` halt enforces on a Running adopted Project — over-cap drives the planner dispatch gate to refuse, zero new planner Jobs | ✓ VERIFIED | Test drives the **real** enforcement path: seeds `ConditionBudgetBlocked=True` via Status().Patch on a Running adopted Project, reconciles dispatch, asserts zero new planner Jobs (`adoption_lifecycle_test.go:409+`). Correctly does NOT assert on Phase==BudgetExceeded (the secondary signal). Verifier-run: PASS. |
| 4 | ADOPT-04: rollup is **exactly-once** at every child level across reporter-Job TTL-GC | ✓ VERIFIED | Each child rollup gated by its durable `.status` marker (`MilestoneRolledUpUID`/`PhaseRolledUpUID`/`PlanRolledUpUID`) checked before `budget.RollUpUsage`, stamped only after a nil-error rollup. Envtest sets `ReporterImage=""` so `isFirstCompletion=true` on every call (faithful TTL-GC simulation), drives `handleJobCompletion` a second time with marker set, asserts `Consistently(2s)` CostSpentCents unchanged. Verifier-run: PASS. See WR-02/WR-03 discussion below — genuinely met, not incidentally satisfied. |
| 5 | ADOPT-05: normal (non-import) lifecycle unchanged AND suppressed planner never re-dispatches on cold-cache restart | ✓ VERIFIED | Durable short-circuit `project_controller.go:1093-1103` reads `ConditionProjectPlannerSuppressed` BEFORE the live `r.List` (L1134) and BEFORE `PlannerPool.Acquire` (L1179) — cache-independent. Envtest: cold-cache re-reconcile keeps zero Jobs/Phase=Running; no-regression spec advances a normal Project to Running with suppression NOT set. Verifier-run: PASS. |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----|----|----|----|
| `api/v1alpha2/shared_types.go` | ConditionProjectPlannerSuppressed + ReasonAdoptionComplete | ✓ VERIFIED | L300/L305, doc-commented, in the BillingHalt/BudgetBlocked/ImportComplete family. |
| `api/v1alpha2/milestone_types.go` | MilestoneRolledUpUID marker | ✓ VERIFIED | L67, `json:"milestoneRolledUpUID,omitempty"`. |
| `api/v1alpha2/phase_types.go` | PhaseRolledUpUID marker | ✓ VERIFIED | L63. |
| `api/v1alpha2/plan_types.go` | PlanRolledUpUID marker (D-03a new) | ✓ VERIFIED | L88. |
| CRD manifests (3) | level-specific status props | ✓ VERIFIED | `milestoneRolledUpUID`/`phaseRolledUpUID`/`planRolledUpUID` present in respective `config/crd/bases/*.yaml`; deepcopy needs no explicit copy (scalar string by value); `go build` + `go vet` clean. |
| `internal/controller/project_controller.go` | suppression short-circuit + single-patch advance | ✓ VERIFIED | L1093-1103 (short-circuit), L1149-1167 (single-patch arm). |
| `internal/controller/{milestone,phase,plan}_controller.go` | marker-gated rollup | ✓ VERIFIED | All three gate `RollUpUsage` on the level marker, stamp after success. |
| `adoption_lifecycle_test.go` / `child_rollup_idempotency_test.go` | ADOPT-01..05 envtest | ✓ VERIFIED | 7 specs; verifier ran them green against local envtest assets. |

### Key Link Verification

| From | To | Via | Status |
|----|----|----|----|
| reconcileProjectPlannerDispatch | Phase=Running + ConditionProjectPlannerSuppressed | single `Status().Patch(MergeFrom)` before Acquire | ✓ WIRED (project_controller.go:1154-1167) |
| dispatch short-circuit | FindStatusCondition(ProjectPlannerSuppressed) | before r.List of owned Milestones | ✓ WIRED (L1093-1103 < L1134) |
| checkBudgetBlocked | planner dispatch refusal | ConditionBudgetBlocked gate | ✓ WIRED (exercised by ADOPT-03 spec) |
| milestone/phase/plan handleJobCompletion | budget.RollUpUsage | gated by `<Level>RolledUpUID != jobName` | ✓ WIRED (all three controllers) |
| post-rollup | `<level>.Status.<Level>RolledUpUID = jobName` | Status().Patch(MergeFrom) after nil-error rollup | ✓ WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----|----|----|----|----|
| Project budget | CostSpentCents / TokensSpent | `budget.RollUpUsage` re-fetches Project, increments, MergeFromWithOptimisticLock patch (tally.go:56-89) | Yes — real increment with conflict retry | ✓ FLOWING |
| child marker | `<Level>RolledUpUID` | stamped from `fmt.Sprintf("tide-<level>-%s-1", uid)` after rollup | Yes | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----|----|----|----|
| api+controller compile | `go build ./api/... ./internal/... ./cmd/manager/...` | exit 0 | ✓ PASS |
| controller vet | `go vet ./internal/controller/` | exit 0 | ✓ PASS |
| phase-31 envtests | `go test TestControllers -ginkgo.focus='ChildRollupIdempotency\|Adoption'` (local envtest assets) | Ran 7 of 162; 7 Passed, 0 Failed | ✓ PASS |
| full controller suite (regression) | `go test ./internal/controller/ -run TestControllers -count=1` | ok, 129.2s, exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Status | Evidence |
|----|----|----|----|
| ADOPT-01 | 31-01, 31-02 | ✓ SATISFIED | Truth #1 |
| ADOPT-02 | 31-03 | ✓ SATISFIED | Truth #2 |
| ADOPT-03 | 31-02 | ✓ SATISFIED | Truth #3 |
| ADOPT-04 | 31-01, 31-03 | ✓ SATISFIED | Truth #4 (see WR-02/03 below) |
| ADOPT-05 | 31-01, 31-02 | ✓ SATISFIED | Truth #5 |

All 5 phase requirement IDs map to plan frontmatter and to REQUIREMENTS.md §"Adoption Lifecycle & Cost Rollup". No orphaned requirements (REQUIREMENTS.md maps exactly ADOPT-01..05 to Phase 31).

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
|----|----|----|----|
| (none) | TBD/FIXME/XXX scan of phase-31 modified files | — | No unreferenced debt markers. The `WR-01..04`/`T-31-*` references in comments are formal review/threat IDs, not debt markers. |

D-09 (no new Status().Update): project_controller.go=7, unchanged. D-10 (project PlannerRolledUpUID): present (5), preserved. D-11 (values.yaml): untouched across phase-31 commits. No generic `PlannerRolledUpUID` leaked onto child controllers.

### The Load-Bearing ADOPT-04 Question (WR-02 / WR-03)

**Verdict: ADOPT-04 is genuinely met, not only incidentally satisfied. WARNING (hardening), not a blocker.**

Traced the marker-gating and accrual paths directly:

1. `budget.RollUpUsage(ctx, r.Client, project, ...)` re-fetches and patches the **Project** object with `MergeFromWithOptimisticLock` + `RetryOnConflict` (tally.go:57-84). It **never** touches the level object (`ms`/`ph`/`plan`).
2. Between the top of `handleJobCompletion` (where the level object is received) and the marker stamp, there is **no** `Status().Patch` on the level object — only the Project rollup and a reporter-Job create. Therefore the level object's ResourceVersion is current at the marker stamp; WR-02's "stale object after RollUpUsage patched a different object" condition is **not active** today. The review itself concedes the current code is safe.
3. The exactly-once guarantee rests on the **durable `.status` marker**, which is checked *before* `RollUpUsage` and persisted *after* a nil-error rollup. The verifier-run ADOPT-04 specs prove the marker is the SOLE guard across the simulated 300s TTL-GC window (`isFirstCompletion` forced true on every call) and CostSpentCents stays byte-for-byte unchanged.

The residual WR-03 gap is a genuine but **degenerate-failure-path** window: marker-patch fails (logged non-fatal) AND the reporter Job then TTL-GCs AND a reconcile re-fires — only then could a second rollup occur. This window:
- is the **same best-effort non-fatal marker-patch pattern as the shipped project-level prior art** (project_controller.go:1351-1361, Phase 27, preserved as D-10) — the phase propagated the accepted pattern downward exactly as the plan (D-03) mandated; it did not introduce a new weaker one;
- was classified by the code review as a **WARNING with 0 criticals**;
- does not undermine the normal-path exactly-once guarantee that the phase goal and ADOPT-04 envtest assert.

This is recorded as a hardening WARNING (suggested fix: `RetryOnConflict` + re-fetch on the marker stamp, made blocking on retry-budget exhaustion). It does not block phase closure.

### Warnings (non-blocking — from 31-REVIEW.md, confirmed against code)

- **WR-02 / WR-03** (above): marker stamp is a best-effort non-fatal `MergeFrom` patch. Currently safe (level object not re-fetched/mutated by RollUpUsage); residual crash-window for exactly-once. Mirrors accepted project-level pattern. Hardening opportunity for a follow-up.
- **WR-01** (project_controller.go:1163-1165): the suppression patch uses plain `MergeFrom` (last-write-wins, cannot Conflict) but the inline comment claims "Conflict is retryable." Comment encodes a false invariant; behavior benign for the two fields written. Cosmetic/correctness-of-comment.
- **WR-04** (adoption_lifecycle_test.go): the D-07 single-patch atomicity invariant is asserted in comments but not test-covered — a regression splitting it into two sequential patches would pass existing assertions. The end-state (Phase=Running + suppression both present, zero Jobs) IS asserted.
- **Minor:** the ADOPT-05 no-regression spec asserts `Phase=Running` (a valid proxy — the normal path patches Phase=Running only at the dispatch tail after Job creation) and asserts suppression NOT set, but does not explicitly assert the project-planner Job object exists. Coverage is adequate via the proxy; an explicit `HaveLen(1)` Job assertion would close the gap.

### Gaps Summary

No blocking gaps. All 5 must-have truths are verified in the actual codebase and proven by verifier-run envtests (7 phase-31 specs green; full 162-spec controller suite green, no regressions). The four review WARNINGS are correctness-at-the-margins / verification-gap items — none prevents the phase goal. The load-bearing ADOPT-04 exactly-once guarantee is genuinely achieved via the durable per-level markers, with a known degenerate-failure-path hardening opportunity (WR-03) that matches the accepted project-level prior art and is appropriate for a follow-up rather than a phase blocker.

---

_Verified: 2026-06-28T19:20:00Z_
_Verifier: Claude (gsd-verifier)_
