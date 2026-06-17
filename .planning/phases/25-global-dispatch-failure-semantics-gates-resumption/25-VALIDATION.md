---
phase: 25
slug: global-dispatch-failure-semantics-gates-resumption
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-16
---

# Phase 25 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Ginkgo v2 + Gomega; envtest for controller integration) |
| **Config file** | none — controller-runtime envtest configured in `test/integration/envtest/suite_test.go` |
| **Quick run command** | `go test ./internal/controller/... ./api/... ./pkg/dag/... ./cmd/tide/...` |
| **Full suite command** | `make test-int` |
| **Estimated runtime** | ~180 seconds (envtest integration); unit subset ~30s |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/... ./api/... ./pkg/dag/... ./cmd/tide/...`
- **After every plan wave:** Run `make test-int`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 25-01-01 | 01 | 1 | DISP-02 | T-25-01-01 / T-25-01-02 | `FailureProfile` CEL enum rejects out-of-enum values; default=strict means unset never silently becomes conservative | unit | `go build ./api/... && grep -q 'ConditionFailureHalt = "FailureHalt"' api/v1alpha2/shared_types.go && grep -q 'FailureProfile FailureProfileType' api/v1alpha2/project_types.go && make verify-no-aggregates` | ✅ (api/v1alpha2/*.go) | ⬜ pending |
| 25-01-02 | 01 | 1 | DISP-01, DISP-02, DISP-03, RESUME-01 | — | RED scaffolds precede implementation (Nyquist); fixtures grepped for coarse-ref deps (A1) | scaffold (RED) | `go vet ./internal/controller/... ./cmd/tide/... ; test -f internal/controller/failure_halt_test.go && test -f test/integration/envtest/global_dispatch_test.go && test -f cmd/tide/resume_failure_test.go && grep -q 'Label("envtest", "phase25")' test/integration/envtest/global_dispatch_test.go && grep -q 'func TestResumeRunClearsFailureHalt' cmd/tide/resume_failure_test.go` | ❌ W0 (created here) | ⬜ pending |
| 25-02-01 | 02 | 2 | DISP-01 | T-25-02-01 | Coarse-ref counts satisfied only when ALL member tasks Succeeded; one shared resolver = dispatch & wave map cannot disagree | unit | `go test ./internal/controller/... -run 'TestBuildScopeResolver\|TestResolveScope\|TestBuildGlobalEdges' -count=1 && make verify-dag-imports && make verify-no-aggregates` | ✅ (internal/controller/depgraph_test.go) | ⬜ pending |
| 25-02-02 | 02 | 2 | DISP-01, DISP-03, RESUME-01 | T-25-02-01 / T-25-02-02 / T-25-02-03 | Empty-projectName label selector rejected; self-enqueue guard; coarse-ref dependent IS re-enqueued (no resync stall) | unit + envtest | `go test ./internal/controller/... -run 'TestComputeGlobalIndegree\|TestGlobalIndegree\|TestGlobalDependentsMapper\|TestListProjectTasks' -count=1 && go test ./test/integration/envtest/... -run 'DISP-01\|DISP-03\|RESUME-01' -count=1` | ✅ (test/integration/envtest/global_dispatch_test.go) | ⬜ pending |
| 25-03-01 | 03 | 3 | DISP-02 | T-25-03-05 | Halt fires only on task EXECUTION failure under conservative profile; idempotent (no patch churn on concurrent failures) | unit | `go test ./internal/controller/... -run 'TestCheckFailureHalt\|TestSetFailureHaltIfNeeded' -count=1 && make verify-no-aggregates` | ✅ (internal/controller/failure_halt_test.go) | ⬜ pending |
| 25-03-02 | 03 | 3 | DISP-02, RESUME-01 | T-25-03-01 / T-25-03-02 / T-25-03-03 / T-25-03-04 | All FOUR execution sites gate (fail-closed); planner site stays ungated (execution-only); bare resume never clears; in-flight Wave never pruned | unit + envtest | `go test ./cmd/tide/... -run 'TestResumeRunClearsFailureHalt\|TestResumeWithoutRetryFailedLeavesFailureHalt' -count=1 && grep -c 'checkFailureHalt' internal/controller/{task,plan,phase,milestone}_controller.go && go test ./test/integration/envtest/... -run DISP-02 -count=1` | ✅ (cmd/tide/resume_failure_test.go) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Authored in 25-01 Task 2 (RED scaffolds — must precede implementation per Nyquist). Paths match the actual codebase layout (envtest specs live in `test/integration/envtest/`, not `internal/controller/`).

- [ ] `internal/controller/failure_halt_test.go` — seven RED unit cases for `checkFailureHalt`/`setFailureHaltIfNeeded` (DISP-02 conservative halt); turned GREEN by 25-03 Task 1.
- [ ] `test/integration/envtest/global_dispatch_test.go` (package `envtest_integration`, `Label("envtest", "phase25")`) — RED specs for:
  - DISP-01 (cross-plan + coarse-ref dependent stays non-Running until predecessors Succeed)
  - DISP-02 strict — later-wave non-dependent continues AND same-global-wave CROSS-PLAN independent sibling continues (ROADMAP success criterion 2)
  - DISP-02 conservative (first failure stamps `ConditionFailureHalt`; all new dispatch frozen)
  - DISP-03 (task gate `approve` holds a globally-ready task while a non-dependent flows)
  - RESUME-01 (A→B→C cross-plan; status-patch A,B Succeeded; C dispatches with no new persisted field)
  - Turned GREEN by 25-02 (DISP-01/03, RESUME-01) and 25-03 (DISP-02 strict + conservative).
- [ ] `cmd/tide/resume_failure_test.go` (package `main`) — `TestResumeRunClearsFailureHalt` + `TestResumeWithoutRetryFailedLeavesFailureHalt` (RESUME-01 / DISP-02 clear); turned GREEN by 25-03 Task 2.

> `internal/controller/depgraph_test.go` (25-02 Task 1) and the `globalDependentsMapper`/`computeGlobalIndegree` unit tests (25-02 Task 2) are authored inside their TDD tasks (RED→GREEN within the same task), so they are not Wave-0 scaffolds.

---

## Manual-Only Verifications

*All phase behaviors have automated verification.* (Global dispatch readiness, the strict/conservative failure contract, the task-gate hold, and restart re-derivation are all exercisable in envtest with status-subresource fixtures.)

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (paths corrected to `test/integration/envtest/` + `cmd/tide/`)
- [x] No watch-mode flags
- [x] Feedback latency < 180s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-16
