---
phase: 52
slug: per-level-looppolicy-parameterization
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-20
---

# Phase 52 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from 52-RESEARCH.md §Validation Architecture. Task IDs filled at plan time (plans 52-01 … 52-11).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (plain unit tests) + Ginkgo v2.28/Gomega (envtest specs) |
| **Config file** | none — `internal/controller/suite_test.go` boots envtest programmatically |
| **Quick run command** | `go test ./internal/controller/... -run TestXxx` (plain-Go) / `go test ./api/v1alpha3/... -run TestXxx` (schema-only) |
| **Full suite command** | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='<Spec>'` (Ginkgo); `make test-int` for the kind-backed layer (grep `^--- FAIL\|^FAIL\s` — never trust the Ginkgo summary alone) |
| **Estimated runtime** | ~60–120s envtest suite; minutes for `make test-int` |

---

## Sampling Rate

- **After every task commit:** Run the relevant plain-Go unit test for the function touched (e.g. `go test ./internal/controller/... -run TestResolveLoopPolicy`)
- **After every plan wave:** Run `go test ./internal/controller/... -run TestControllers --ginkgo.focus='<this wave's specs>'`
- **Before `/gsd:verify-work`:** Full `make test-int` green (grep `^--- FAIL\|^FAIL\s`, and read MAKE_EXIT) + `make lint` (release-cascade lesson: ci.yaml-only gates run per phase)
- **Max feedback latency:** ~120 seconds (envtest layer)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 52-04 T1 | 52-04 | 2 | ESC-01 (resolver) | T-52-09 | Task>Plan>Project precedence + level defaults + P/M/P clamp + escalation-policy defaults | unit | `go test ./internal/controller/... -run TestResolveLoopPolicy` | ❌ W0 (created by 52-04 T1) | ⬜ pending |
| 52-07 T3 + 52-09 T2 | 52-07/52-09 | 4/5 | ESC-01 SC1 (plan-check own counter) | T-52-19 | REPAIRABLE → exactly one re-plan, then escalate; Verifying holds child dispatch | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='PlanCheck\|RePlan'` | ❌ W0 (created by 52-07 T3) | ⬜ pending |
| 52-09 T1 | 52-09 | 5 | ESC-01 SC1 (severity-weighted stall) | — | maxIterations:2 non-improving re-plan halts early | unit | `go test ./internal/controller/... -run 'TestReplanStalled\|TestSeverityScore\|TestPlanVerifyLoop'` | ❌ W0 (created by 52-09 T1) | ⬜ pending |
| 52-10 T2 | 52-10 | 5 | ESC-01 SC2 (maxIterations:0) | T-52-32 | Phase/Milestone/Project finding escalates (requireApproval default / escalate authored), never repairs | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='LevelVerify'` | ❌ W0 (created by 52-10 T2) | ⬜ pending |
| 52-04 T2 (+52-06 T1 removes exclusion) | 52-04/52-06 | 2/3 | ESC-01 SC3 (single resolver) | T-52-11 | No controller reads raw Spec.Verification policy knobs outside the resolver | static source-scan test | `go test ./internal/controller/... -run TestNoDirectVerificationPolicyReads` | ❌ W0 (created by 52-04 T2) | ⬜ pending |
| 52-09 T2 | 52-09 | 5 | D-04 (re-plan supersede) | T-52-27/28 | Rejected attempt's Tasks deleted before re-dispatch; stale Task never dispatches; deletion barrier kills the name-collision window | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='RePlan'` | ❌ W0 (created by 52-09 T2) | ⬜ pending |
| 52-06 T2 | 52-06 | 3 | D-08 (onExhaustion split, all levels) | T-52-15/16 | requireApproval → AwaitingApproval park → approve → Succeeded, no executor resurrect; escalate → ConditionVerifyHalt | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='Exhaust\|RequireApproval'` | ❌ W0 (created by 52-06 T2) | ⬜ pending |
| 52-10 T2 | 52-10 | 5 | ESC-03 (VerifyHalt ≠ Failed at new levels) | T-52-34 | Sibling/conservative-profile propagation untouched by a level VerifyHalt | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='VerifyHalt'` | extends verify_halt_test.go | ⬜ pending |
| 52-07 T3 (plan site) + 52-10 T2 (level site) | 52-07/52-10 | 4/5 | D-10 (rails at new sites) | T-52-21 | Counted by verifierInFlightCount + reserved via shared store + fail-closed ClassifyVerdict + EVALUATOR span per new site | envtest | `--ginkgo.focus='PlanCheck\|LevelVerify'` rails specs | ❌ W0 | ⬜ pending |
| 52-11 T1 | 52-11 | 6 | D-07 worktree provisioning (live) | T-52-12/35 | Init container provisions detached worktree at run-branch tip on a REAL PVC, non-billable | kind | `make test-int` (MAKE_EXIT + `grep '^--- FAIL\|^FAIL\s'`) | ❌ W0 (created by 52-11 T1) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Each gap is owned by the task that creates the test file alongside its implementation (test-with-the-change; TDD-ordered where flagged):

- [ ] `internal/controller/dispatch_helpers_loop_policy_test.go` — `TestResolveLoopPolicy*` + `TestNoDirectVerificationPolicyReads` → **52-04 T1/T2** (tests written first, tdd="true")
- [ ] `internal/controller/plan_verify_loop_test.go` — stall-detection math (plain-Go, mirrors task_verify_loop_test.go) → **52-09 T1** (tests written first, tdd="true")
- [ ] `internal/controller/plan_verify_dispatch_test.go` — PlanCheck + RePlan Ginkgo families → **52-07 T3**, extended by **52-09 T2**
- [ ] `internal/controller/level_verify_dispatch_test.go` + `level_verify_unit_test.go` — LevelVerify Ginkgo family + pure guards → **52-10 T2** / **52-08 T2**
- [ ] `pkg/git/worktree_test.go` extension — `TestAddReadOnlyWorktree` → **52-05 T1** (tests written first, tdd="true")
- [ ] Live kind proof for worktree provisioning (envtest cannot observe a real PVC) → **52-11 T1** (automated, non-billable) + **52-11 T2** (billable, operator-gated checkpoint)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Full billable loop proof: plan-check REPAIRABLE→re-plan→verdict + phase-level verify park→approve→Succeeded against the real API | ESC-01 SC1/SC2 end-to-end | Requires real ANTHROPIC key + operator spend approval (51-08 precedent: the live gate surfaced five defects green suites missed) | 52-11 T2 checkpoint runbook (kind cluster, ~/.tide/anthropic.key, fixture Projects per the how-to-verify steps) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (52-11 T2 is the sole `<human-check>`, by design — billable)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (each owned by a named task above)
- [x] No watch-mode flags
- [x] Feedback latency < 120s (envtest layer)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planned 2026-07-20 (11 plans, waves 1–6)
