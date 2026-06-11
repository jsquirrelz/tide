---
phase: 12
slug: gate-semantics-reject-resume
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-11
updated: 2026-06-11
---

# Phase 12 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (envtest via Ginkgo/Gomega) + test/integration/kind Layer A/B |
| **Config file** | Makefile (`test`, `test-int-fast`, `test-int` targets) |
| **Quick run command** | `go test ./internal/... ./cmd/... -short -timeout 120s` |
| **Full suite command** | `make test` (unit tier; envtest) — Layer A via `make test-int-fast`; full `make test-int` at phase gate |
| **Estimated runtime** | ~120 s quick · ~5 min `make test` · ~30 min `make test-int` (one heavy run at a time on the 7.65 GiB VM) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/... ./internal/gates/... ./cmd/tide/... -timeout 600s`
- **After every plan wave:** Run `make test-int-fast` (read MAKE_EXIT and grep `^--- FAIL|^FAIL\s` — not just the Ginkgo summary)
- **Before `/gsd:verify-work`:** full `make test-int` green (MAKE_EXIT=0 + zero FAIL lines)
- **Max feedback latency:** 300 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 12-01/T1 | 12-01 | 1 | GATE-01 | T-12-01, T-12-02 | approval never escalates a level to Succeeded past incomplete children; ConsumeApprove stays one-shot | envtest (Ginkgo) | `go test ./internal/controller/... -timeout 600s` | ✅ milestone_gates_test.go (spec added in-task, RED first) | ⬜ pending |
| 12-01/T2 | 12-01 | 1 | GATE-01 | T-12-01 | phase parity: AwaitingApproval early-return; approve-then-wait-for-children end-to-end | envtest Layer A | `make test-int-fast` | ✅ gates_test.go TestGateApproveFlow (updated in-task) | ⬜ pending |
| 12-01/T3 | 12-01 | 1 | GATE-02 | — | doc no longer instructs the bypass | grep gate | `test "$(grep -c 'advances the level to' docs/gates.md)" -eq 0` | ✅ docs/gates.md | ⬜ pending |
| 12-02/T1 | 12-02 | 1 | RESUME-01 | T-12-04, T-12-06 | --retry-failed resets only Failed levels; ResumedByUser audit record | unit (fake client) | `go test ./cmd/tide/... -run 'TestResume' -timeout 120s` | ✅ resume_test.go (tests added in-task, RED first) | ⬜ pending |
| 12-02/T2 | 12-02 | 1 | GATE-03 | T-12-05 | approve never doubles as spend-retry; no annotation written on Failed level | unit (fake client) | `go test ./cmd/tide/... -run 'TestApprove' -timeout 120s` | ✅ approve_test.go (tests added in-task, RED first) | ⬜ pending |
| 12-03/T1 | 12-03 | 2 | GATE-04 | T-12-08, T-12-10 | child dispatch held while parent parked; NotFound never wedges | compile + suite | `go build ./... && go test ./internal/controller/... -timeout 600s` | ✅ existing suites | ⬜ pending |
| 12-03/T2 | 12-03 | 2 | GATE-04 | T-12-08 | finding-1 regression: 5 materialized children, zero Jobs until approval | envtest (Ginkgo) | `make test-int-fast` | ✅ phase_gates_test.go + gates_test.go (specs added in-task) | ⬜ pending |
| 12-04/T1 | 12-04 | 3 | RESUME-01 | T-12-11 | milestone/phase reject parks — no Failed write, recoverable | envtest (Ginkgo) | `go test ./internal/controller/... -timeout 600s` | ✅ milestone/phase_gates_test.go (updated RED first) | ⬜ pending |
| 12-04/T2 | 12-04 | 3 | RESUME-01 | T-12-11, T-12-12 | plan/task reject parks; rejected Project spends nothing new | envtest (Ginkgo) | `go test ./internal/controller/... -timeout 600s` | ✅ plan/task_gates_test.go (updated RED first) | ⬜ pending |
| 12-04/T3 | 12-04 | 3 | RESUME-01, GATE-03 | T-12-13 | retry-failed recipe re-dispatches; reject wedge structurally impossible | envtest Layer A | `make test-int-fast` | ✅ gates_test.go (TestRejectHalts updated + new spec) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] No separate Wave 0 — every regression test is created RED-first inside the task that fixes its symptom (`tdd="true"` on all code tasks); existing envtest/kind harness covers infrastructure, no new framework install

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Run-1 CR repro in live kind cluster `tide` | GATE-01 | Live cluster state from dogfood run 1 is a one-off artifact (NEVER delete) | Load fixed controller image; `tide approve` the parked run-1 Milestone; verify it returns to Running with ApprovedByUser condition, not Succeeded |
| Run-1 fail-marked CR recovery | RESUME-01 | Run-1 CRs lack the `tideproject.k8s/project` label (CUTS-01 ships in Phase 15), so the CLI verb can't discover them | Use the kubectl recipe documented in gates.md (12-01/T3); observe reconciler re-dispatch |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (RED-first in-task tests)
- [x] No watch-mode flags
- [x] Feedback latency < 300s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner 2026-06-11
