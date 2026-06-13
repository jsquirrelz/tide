---
phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
verified: 2026-06-13T01:10:00Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
---

# Phase 17: Address tech debt: Plan label backfill + gate hardening — Verification Report

**Phase Goal:** PlanReconciler self-heals the `tideproject.k8s/project` label like milestone/phase already do, and the reject/approve/envelope-read gate paths are brought into consistency with their shipped sibling patterns — every fix mirrors an in-tree template and carries a regression test
**Verified:** 2026-06-13T01:10:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (ROADMAP Success Criterion) | Status | Evidence |
| --- | --- | --- | --- |
| 1 | DEBT-01: a pre-v1.0.1 Plan with no project label gets it stamped idempotently on next reconcile; Project→Milestone reporter edge stamps at create-site | ✓ VERIFIED | `plan_controller.go:182-193` backfill block (label-absent guard → `resolveProjectName` → `MergeFrom` patch), ordered before `r.Dispatcher != nil` (:199), orphan-safe via `err == nil && name != ""`. `materialize.go:260-264` `*Project` type-switch resolves `parent.GetName()` before `StampProjectLabel`. Specs: `plan_controller_test.go:499` (stamp + RV-unchanged idempotency), `materialize_test.go:410` (create-site stamp). 7-spec envtest run + materialize unit test PASS. |
| 2 | DEBT-02: a rejected Project's completing planner Job parks Milestone/Phase Rejected WITHOUT spawning a NEW reporter Job, never deleting an in-flight Job | ✓ VERIFIED | Phase: `gates.CheckRejected` :458 < `ReadOut` :475 < `spawnReporterIfNeeded` :491 (reject is first statement after project resolution). Milestone: :515 < :534 < :556. No `Delete(` on either reject path. Regression specs `phase_controller_test.go:279` + `milestone_controller_test.go:515` assert RejectedByUser park AND zero `tide-reporter-<uid>` Job (assert-none-created, not deletion). envtest PASS. |
| 3 | DEBT-03: `tide approve` refuses only when the approval TARGET is itself Failed, not when an unrelated sibling is Failed; `--wave` semantics documented | ✓ VERIFIED | `approve.go:163` `approveLevel` discovers AwaitingApproval target FIRST (findAwaiting* chain :175-194), `findFailedLevel` (:199) demoted to a no-target UX fallback (no longer an unconditional pre-guard). `approveLevelTarget` (:219) checks only the discovered target's `Status.Phase`. Option-A `--wave` comment at :33-35, :79-85. 3 new tests (`approve_test.go:321,348,375`) PASS — unrelated-Failed-doesn't-block (asserts approval SUCCEEDS + annotation written), Failed-target-refused, `--wave` bypass. |
| 4 | DEBT-04: a transient envelope-read error in the Plan completion handler is non-fatal — defers to children-based succession instead of terminal Failed | ✓ VERIFIED | `EnvelopeReadFailed` removed (0 grep hits). `plan_controller.go:504-526` introduces `envReadOK`/`envReaderPresent`; read error logs non-fatally (:523) and falls through — no `Status.Phase="Failed"`. Children-based fallback :661-676 requeues 5s instead of wedging. nil-EnvReader fallback (:506-514, clears `Phase=""`) preserved. Spec `plan_controller_test.go:599` stubs read error (:653), asserts `Status.Phase != "Failed"` (:671). envtest PASS. |

**Score:** 4/4 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| --- | --- | --- | --- |
| `internal/controller/plan_controller.go` | Backfill block (DEBT-01) + non-fatal envelope read (DEBT-04) coexisting | ✓ VERIFIED | Backfill :182-193; sentinels :504-505; both present, package builds, envtest green |
| `internal/reporter/materialize.go` | `*Project` create-site stamp | ✓ VERIFIED | Type-switch :261, `projectName` resolved before `StampProjectLabel` :264 |
| `internal/controller/phase_controller.go` | Reject short-circuit before reporter spawn | ✓ VERIFIED | `gates.CheckRejected` :458 ahead of spawn :491; no Job deletion |
| `internal/controller/milestone_controller.go` | Reject short-circuit before read + spawn | ✓ VERIFIED | :515 < :534 < :556 (landed Phase 12 per SUMMARY deviation; regression guard added this phase) |
| `cmd/tide/approve.go` | D-07 guard narrowed to target (Option A) | ✓ VERIFIED | `approveLevel` discovers-first; `approveLevelTarget` targeted check; `--wave` documented |
| Regression specs (5 net-new) | One test per fix referencing the right behavior | ✓ VERIFIED | plan backfill, materialize create-site, phase reject, milestone reject, plan non-fatal — all exist, substantive, and pass when run |

### Key Link Verification

| From | To | Via | Status | Details |
| --- | --- | --- | --- | --- |
| `PlanReconciler.Reconcile` | `resolveProjectName` | label-absent guard before dispatch | ✓ WIRED | :182-189, reuses existing resolver |
| `MaterializeChildCRDs` | `owner.StampProjectLabel` | `*Project` type-switch → `parent.GetName()` | ✓ WIRED | :261-264 |
| `PhaseReconciler.handleJobCompletion` | `patchPhaseRejected` | reject before `spawnReporterIfNeeded` | ✓ WIRED | :458 ahead of :491 |
| `MilestoneReconciler.handleJobCompletion` | `patchMilestoneRejected` | reject before read + spawn | ✓ WIRED | :515 ahead of :534/:556 |
| `approveLevel` | `findAwaiting*` discovery | discover target first, refuse only if THAT object Failed | ✓ WIRED | :175-194 precede :199 |
| `PlanReconciler.handlePlannerJobCompletion` | children-based succession | non-fatal read path (no terminal Failed) | ✓ WIRED | :518-526, :661-676 |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Four touched packages compile | `go build ./internal/controller/... ./internal/reporter/... ./cmd/tide/...` | BUILD_EXIT=0 | ✓ PASS |
| DEBT-03 approve tests run + pass | `go test ./cmd/tide/... -run 'TestApprove(Unrelated…|FailedTarget…|Wave…)' -v` | 3/3 PASS | ✓ PASS |
| 15-WR-03 create-site stamp test | `go test ./internal/reporter/... -run …ProjectParentStampsLabelAtCreateSite -v` | PASS | ✓ PASS |
| DEBT-01/02/04 envtest regression specs | `KUBEBUILDER_ASSETS=… go test ./internal/controller/... -ginkgo.focus='DEBT-0[24]|backfill|reject short-circuit|non-fatal'` | Ran 7 of 143 — 7 Passed, 0 Failed | ✓ PASS |
| Debt-marker scan on 5 modified files | `grep -nE 'TBD|FIXME|XXX|HACK|PLACEHOLDER'` | 0 hits | ✓ PASS |

Note: controller envtest required `dangerouslyDisableSandbox` — the macOS sandbox blocked `etcd`/`kube-apiserver` exec (`fork/exec … no such file or directory` despite the binaries being present and executable in `bin/k8s/1.33.0-darwin-amd64`). This is a sandbox restriction, not a code or asset failure; specs passed once the control plane could fork.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| --- | --- | --- | --- | --- |
| DEBT-01 | 17-01 | Plan label backfill + Project→Milestone create-site stamp | ✓ SATISFIED | Truth 1 |
| DEBT-02 | 17-02 | Reject short-circuit ahead of reporter spawn (milestone+phase) | ✓ SATISFIED | Truth 2 |
| DEBT-03 | 17-03 | Narrow D-07 approve guard to target (Option A) | ✓ SATISFIED | Truth 3 |
| DEBT-04 | 17-04 | Plan envelope-read non-fatal | ✓ SATISFIED | Truth 4 |

All 4 requirement IDs declared in PLAN frontmatter are present in REQUIREMENTS.md (lines 59-62) and mapped to Phase 17 (lines 114-117). No orphaned requirements. Full traceability.

### Anti-Patterns Found

None. No `TBD`/`FIXME`/`XXX`/`HACK`/`PLACEHOLDER` in any of the 5 modified production files. The `cmd/tide-demo-init` build issue noted in 17-03-SUMMARY is pre-existing, unrelated to Phase 17 (no phase-17 file touches it), and the fixture files (`go.mod.txt`, `go.sum.txt`, `main.go`) are present.

### Gaps Summary

None. Every fix mirrors its in-tree sibling template (per 17-PATTERNS.md), carries a substantive regression test that references the correct behavior, and the goal's "consistency with shipped sibling patterns" is observable in source. Independent code review (17-REVIEW.md) is clean (0 critical / 0 warning / 3 info-advisory). The three Info items are advisory only and do not block goal achievement:
- IN-01: `approveLevelTarget`'s belt-and-suspenders `targetPhase == "Failed"` branch is currently inert (discovery returns only AwaitingApproval objects, no re-Get) — safe, cannot wrongly approve; the target-Failed refusal is still covered by `TestApproveFailedLevelNoAnnotationWritten` and the fallback hint path.
- IN-02: reject skips budget rollup of already-consumed planner spend — an established cross-controller tradeoff (plan/milestone identical), not a Phase 17 regression.
- IN-03: `buildFailureDetail` `Conditions[0]` fallback can surface a stale condition — cosmetic error-message quality only.

The two minor SUMMARY deviations were both confirmed accurate against source: (1) milestone reject was already correctly ordered from Phase 12 commit `be82c7e`, so 17-02 Task 2 was spec-only — verified the milestone reject IS ahead of read+spawn; (2) `TestApproveFailedTargetStillRefused` asserts `err != nil` rather than the specific message because its fixture routes through the fallback hint path — disclosed and acceptable.

---

_Verified: 2026-06-13T01:10:00Z_
_Verifier: Claude (gsd-verifier)_
