---
phase: 52
slug: per-level-looppolicy-parameterization
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-20
---

# Phase 52 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from 52-RESEARCH.md §Validation Architecture.

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
- **Before `/gsd:verify-work`:** Full `make test-int` green (grep `^--- FAIL\|^FAIL\s`, and read MAKE_EXIT)
- **Max feedback latency:** ~120 seconds (envtest layer)

---

## Per-Task Verification Map

> Task IDs filled by the planner; requirement rows fixed from research.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | — | — | ESC-01 (resolver) | — | Task>Plan>Project precedence + level defaults | unit | `go test ./internal/controller/... -run TestResolveLoopPolicy` | ❌ W0 | ⬜ pending |
| TBD | — | — | ESC-01 SC1 (plan-check own counter) | — | REPAIRABLE → exactly one re-plan, then escalate | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='PlanCheck'` | ❌ W0 | ⬜ pending |
| TBD | — | — | ESC-01 SC1 (severity-weighted stall) | — | maxIterations:2 non-improving re-plan halts early | unit | `go test ./internal/controller/... -run TestPlanVerifyLoop_Stall` | ❌ W0 | ⬜ pending |
| TBD | — | — | ESC-01 SC2 (maxIterations:0) | — | Phase/Milestone/Project finding escalates, never repairs | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='LevelVerify'` | ❌ W0 | ⬜ pending |
| TBD | — | — | ESC-01 SC3 (single resolver) | — | No CRD-kind switch picks gate policy | static grep-proof | `grep -rn` kind-switch assertion (planner picks exact form) | ❌ W0 | ⬜ pending |
| TBD | — | — | D-04 (re-plan supersede) | — | Rejected attempt's Tasks deleted before re-dispatch; stale Task never Running | envtest | `go test ./internal/controller/... -run TestControllers --ginkgo.focus='RePlan'` | ❌ W0 | ⬜ pending |
| TBD | — | — | D-08 (onExhaustion split, all levels) | — | requireApproval → AwaitingApproval; escalate → ConditionVerifyHalt | unit + envtest | `go test ./internal/controller/... -run TestOnExhaustion` + Ginkgo focus | ❌ W0 | ⬜ pending |
| TBD | — | — | ESC-03 (VerifyHalt ≠ Failed at new levels) | — | Sibling/conservative-profile propagation untouched | envtest | `--ginkgo.focus='VerifyHalt.*Sibling'` (extends verify_halt_test.go) | ❌ W0 | ⬜ pending |
| TBD | — | — | D-10 (rails at new sites) | — | Counted + reserved + fail-closed + EVALUATOR span per new site | envtest + unit | extends existing verifier-count/span coverage | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/controller/dispatch_helpers_loop_policy_test.go` (or similar) — `TestResolveLoopPolicy` stubs (precedence + level defaults)
- [ ] `internal/controller/plan_verify_loop_test.go` — plan-check stall-detection math, mirroring `task_verify_loop_test.go`
- [ ] New Ginkgo `Describe`/`It` blocks (`plan_verify_dispatch_test.go` / `level_verify_dispatch_test.go` alongside `task_verify_dispatch_test.go`)
- [ ] Live kind proof for worktree provisioning (envtest cannot observe a real PVC/worktree — mirrors Phase 51's 51-08 checkpoint shape)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Worktree-provisioning init container checks out run-branch tip for a planner-level verify | ESC-01 SC2 / D-07 | envtest has no kubelet/PVC — only a kind cluster runs the init container for real | kind cluster: create a Phase-level contract, observe verifier pod init container + gate command execution (Phase-51 51-08 runbook shape) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
