---
phase: 10
slug: task-execution-reliability-clone-idempotency-per-run-workspa
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-08
---

# Phase 10 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go testing + Ginkgo v2.28 (kind integration) |
| **Config file** | `Makefile` targets (`make test`, `make test-int`) |
| **Quick run command** | `go test ./pkg/git/... ./internal/subagent/anthropic/... ./cmd/dashboard/api/... ./internal/controller/...` |
| **Full suite command** | `make test` (unit) · `make test-int` (kind integration) |
| **Estimated runtime** | ~30s quick · several min full |

---

## Sampling Rate

- **After every task commit:** Run quick command for the touched package(s).
- **After every plan wave:** Run `make test`.
- **Before `/gsd-verify-work`:** `make test` green **and** a manual medium-sample DoD re-run (legitimate Complete + push + `costSpentCents > 0`).
- **Max feedback latency:** ~30 seconds (unit), bounded by the DoD run at the phase gate.

---

## Per-Requirement Verification Map

Phase 10 has no REQ-IDs (inserted reliability phase); Success Criteria (SC-N) serve as requirements. Per-task IDs are assigned by the planner — this maps each SC to its automated proof.

| SC | Behavior | Wave | Test Type | Automated Command | File Exists | Status |
|----|----------|------|-----------|-------------------|-------------|--------|
| SC-1 | Clone into an existing bare repo returns nil (idempotent) | early | unit | `go test ./pkg/git/ -run TestCloneIdempotent` | ❌ W0 | ⬜ pending |
| SC-1 | Clone-mode retry sequence succeeds | early | unit | `go test ./cmd/tide-push/ -run TestCloneMode` | ✅ extend | ⬜ pending |
| SC-2 | Push/clone Job pods carry `PodSecurityContext.FSGroup == 1000` | early | unit | `go test ./internal/controller/ -run 'TestBuild(Clone\|Push)JobFSGroup'` | ❌ W0 | ⬜ pending |
| SC-3 | Executor checks out `tide/run-*` worktree branch (already-merged fix; confirm) | DoD | manual/E2E | medium-sample run; `kubectl logs` executor shows non-empty branch | ✅ wired | ⬜ pending |
| SC-4 | Malformed child JSON does not abort valid siblings (per-file isolation) | mid | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_PartialParse` | ❌ W0 | ⬜ pending |
| SC-4 | Trailing prose after `}` tolerated (json.Decoder + dec.More) | mid | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_TrailingProse` | ❌ W0 | ⬜ pending |
| SC-4 | Existing malformed-JSON test updated to per-file behavior | mid | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_RejectsMalformedJSON` | ✅ update | ⬜ pending |
| SC-7 | `GET /api/v1/projects/{name}` w/o namespace finds cross-ns project (200) | any | unit | `go test ./cmd/dashboard/api/ -run TestGetProjectWithoutNamespace` | ❌ W0 | ⬜ pending |
| SC-5/6 | Legitimate medium Complete + push + `costSpentCents>0`; unblocks Phase 8 SC-2 + v1.0.0 retag | final | manual/E2E | fresh medium-sample DoD re-run on primed minikube | ✅ env | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/git/clone_test.go` — add `TestCloneIdempotent` (call `Clone` twice on same `destDir`, assert nil error on 2nd; assert remote refs refreshed). Add `TestFetchAnonymous` proxy (seedBareRepo + `file://`) for the empty-PAT fetch path.
- [ ] `internal/controller/push_helpers_test.go` — add `TestBuildCloneJobFSGroup` / `TestBuildPushJobFSGroup` asserting `PodSecurityContext.FSGroup == 1000`.
- [ ] `internal/subagent/anthropic/childcrd_read_test.go` — add `TestReadChildCRDs_PartialParse` (valid `task-01.json` + malformed `task-02.json` → task-01 returned + per-file error for task-02) and `TestReadChildCRDs_TrailingProse` (object + trailing sentence → parses); **update** `TestReadChildCRDs_RejectsMalformedJSON` to the new per-file-isolation contract.
- [ ] `cmd/dashboard/api/projects_test.go` — add `TestGetProjectWithoutNamespaceParamFindsAcrossNamespaces` (project in non-default namespace, no `?namespace=` param → 200 with detail).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Legitimate medium Complete + push to in-cluster `http://` remote | SC-5/SC-6 | Requires a live multi-namespace cluster + real Claude (Haiku) dispatch; not an envtest/unit concern | Apply medium-sample Project on primed minikube; watch tree to all-Succeeded; `git ls-remote` the in-cluster remote shows `tide/run-*`; `kubectl get project -o jsonpath` shows `costSpentCents>0` under cap |
| Executor worktree branch non-empty in-cluster | SC-3 | Confirms an already-merged fix under real dispatch | Executor pod logs show `EnsureWorktree ... branch=tide/run-<project>-<unix>` (not empty) |
| Dashboard tree + detail panels populate for live project | SC-7 | Visual confirmation beyond the unit test | Load dashboard against the live medium project; detail panel renders (no blank panels) |

---

## Validation Sign-Off

- [ ] All code-fix tasks have an `<automated>` verify or a Wave 0 test dependency
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING (❌) references above
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (unit layer)
- [ ] `nyquist_compliant: true` set in frontmatter (after planner wires tasks)

**Approval:** pending
