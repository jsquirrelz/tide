---
phase: 12
slug: gate-semantics-reject-resume
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-11
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
| **Full suite command** | `make test` (unit tier; envtest) — kind Layer B via `make test-int` for boundary gates |
| **Estimated runtime** | ~120 s quick · ~5 min `make test` · ~30 min `make test-int` (one heavy run at a time on the 7.65 GiB VM) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/... ./internal/gates/... -timeout 120s`
- **After every plan wave:** Run `make test` (read MAKE_EXIT and grep `^--- FAIL|^FAIL\s` — not just the Ginkgo summary)
- **Before `/gsd:verify-work`:** `make test` green; gate-flow Layer B specs green if touched
- **Max feedback latency:** 300 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (populated by planner) | | | GATE-01..04, RESUME-01 | — | approval never escalates past children; reject cannot destroy state | envtest + kind | `make test` | ⬜ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Regression stubs in existing gate test files (`internal/controller/milestone_gates_test.go`, envtest `gates_test.go`) for the run-1 finding-7 and finding-1 symptoms
- [ ] Existing envtest/kind harness covers the rest — no new framework install

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Run-1 CR repro in live kind cluster `tide` | GATE-01 | Live cluster state from dogfood run 1 is a one-off artifact | Observe parked Milestone + 5 Phases; apply fixed controller image; `tide approve`; verify Milestone returns to Running with ApprovedByUser condition, not Succeeded |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 300s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
