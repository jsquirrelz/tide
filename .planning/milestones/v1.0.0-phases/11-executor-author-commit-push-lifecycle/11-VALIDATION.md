---
phase: 11
slug: executor-author-commit-push-lifecycle
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-09
---

# Phase 11 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from 11-RESEARCH.md "## Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing (standard) + Ginkgo v2 (integration) |
| **Config file** | `Makefile` targets `make test`, `make test-int` |
| **Quick run command** | `go test ./pkg/git/... ./internal/harness/... ./cmd/claude-subagent/... ./cmd/tide-push/... -count=1 -timeout 60s` |
| **Full suite command** | `make test-int` (Layer A envtest + Layer B kind, ~355s inner wall) |
| **Estimated runtime** | quick ~30s · full ~355s inner wall |

> **Phase 02.2 lesson (CLAUDE.md):** `make test-int` bundles plain go-tests (helm-template contract tests) alongside Ginkgo specs. Read the echoed `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'` — a green Ginkgo summary with one RED go-test still fails the package.

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/git/... ./internal/harness/... -count=1 -timeout 60s`
- **After every plan wave:** Run `make test` (unit only, <30s)
- **Before `/gsd-verify-work`:** Full `make test-int` must be green
- **Max feedback latency:** ~30 seconds (quick) · ~355 seconds (full gate)

---

## Per-Task Verification Map

Keyed to ROADMAP SC-1..SC-6. Plan/task IDs finalize when the planner authors B3–B6; each plan must map its tasks onto these SC rows.

| SC | Component | Wave | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|----|-----------|------|-----------------|-----------|-------------------|-------------|--------|
| SC-1 | B1 wired (clone calls EnsureRunBranch) | 1 | run-branch ref exists before any executor; no `couldn't find remote ref` | unit | `go test ./cmd/tide-push/... -run TestRunClone` | ❌ W0 | ⬜ pending |
| SC-2 | B3 commit step | 1 | worktree changes committed as `TIDE Bot`; `HeadSHA` in `EnvelopeOut.Git` | unit | `go test ./internal/harness/... -run TestCommitWorktree` | ❌ W0 | ⬜ pending |
| SC-2 | B3 empty-diff | 1 | empty worktree → `ExitCode=1 Result=empty-diff`, no commit, no false success | unit | `go test ./internal/harness/... -run TestCommitWorktreeEmpty` | ❌ W0 | ⬜ pending |
| SC-3 | B4 integration | 2 | `IntegrateTaskBranches` merges task branches into run branch (`git merge --no-ff`) without losing commits | unit | `go test ./pkg/git/... -run TestIntegrateTaskBranches` | ❌ W0 | ⬜ pending |
| SC-4 | B5 tide-push | 2 | clone mode provisions run worktree; push mode opens it + pushes with `--force-with-lease` | unit | `go test ./cmd/tide-push/... -run TestRunCloneProvisions` | ❌ W0 | ⬜ pending |
| SC-5 | B6 controller wiring | 2 | `buildCloneJob` passes `--run-branch`; integration triggered before final push | unit | `go test ./internal/controller/... -run TestBuildCloneJob` | ❌ W0 | ⬜ pending |
| SC-6 | DoD medium re-run | final | all descendants Succeeded; `tide/run-*` pushed with real code; `costSpentCents>0` under cap | live e2e (minikube) | manual DoD re-run (rebuild+reload 3 images first) | ❌ W-final | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/git/integrate.go` + `pkg/git/integrate_test.go` — covers SC-3 (`IntegrateTaskBranches`)
- [ ] `internal/harness/commit.go` + `internal/harness/commit_test.go` — covers SC-2 (commit step + empty-diff)
- [ ] `cmd/tide-push/*_test.go` additions for B5 clone provisioning — covers SC-4
- [ ] `internal/controller/push_helpers_test.go` additions for `buildCloneJob --run-branch` — covers SC-5

*Existing infrastructure (`Makefile` targets, Go/Ginkgo suites) covers the framework — no framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Legitimate medium Complete + push to in-cluster `http://` remote | SC-6 | Requires a live minikube cluster, real provider key, and a full planning→execute→push cascade; not reproducible in unit/envtest | Rebuild + reload `controller`, `tide-push`, `claude-subagent` images; run the medium DoD; assert `tide/run-*` pushed with authored code + `costSpentCents>0`; confirms Phase 8 SC-2 flip + v1.0.0 retag unblock |

---

## Validation Sign-Off

- [ ] All B3–B6 tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (integrate.go, commit.go, tide-push/controller test additions)
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (quick) / 355s (full gate)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
