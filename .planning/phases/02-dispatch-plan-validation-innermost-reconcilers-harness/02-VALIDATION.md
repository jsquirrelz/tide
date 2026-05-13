---
phase: 2
slug: dispatch-plan-validation-innermost-reconcilers-harness
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-12
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega + envtest (Phase 1 already wired); stdlib `testing` for `pkg/dag`-style leaf packages |
| **Config file** | None (Ginkgo discovers via `_test.go`); kind cluster spec at `test/integration/kind/cluster.yaml` (Wave 0) |
| **Quick run command** | `make test-int-fast` (Layer A envtest only, ~90s) |
| **Full suite command** | `make test-int` (Layer A + Layer B, ~5 min) |
| **Estimated runtime** | Unit `make test` <30s · Layer A `make test-int-fast` ~90s · Layer A+B `make test-int` ~270s + 30s buffer = ≤5 min |

---

## Sampling Rate

- **After every task commit:** Run `make test` (unit, <30s)
- **After every plan wave:** Run `make test-int-fast` (Layer A envtest, ~90s)
- **Before `/gsd-verify-work`:** `make test-int` green + `make lint` green + `make verify-rbac-marker-discipline` green + `make verify-no-aggregates` green + `make verify-dag-imports` green + `make verify-no-sqlite-dep` green
- **Max feedback latency:** 30s (unit suite)

---

## Per-Task Verification Map

> Task IDs use the form `{padded_phase}-{plan_id}-{task_n}`. Plan IDs and wave numbers populate when PLAN.md files land in step 8.

| REQ-ID | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|----------|-----------|-------------------|-------------|--------|
| SUB-01 | Envelope round-trip + apiVersion/kind validation | unit | `go test ./pkg/dispatch/...` | ❌ W0 | ⬜ pending |
| SUB-02 | PodJobBackend builds 2-container Job with sidecar topology | unit + kind | `go test ./internal/dispatch/podjob/...` · `ginkgo --label-filter=kind ./test/integration/kind` | ❌ W0 | ⬜ pending |
| SUB-03 | Deterministic Job name; AlreadyExists is success | unit + envtest | `go test ./internal/controller/...` · `ginkgo --label-filter=envtest` | ❌ W0 | ⬜ pending |
| SUB-04 | Stub-subagent canned envelopes per testMode | unit + kind | `go test ./cmd/stub-subagent/...` · `ginkgo --label-filter=kind -focus=stub_modes` | ❌ W0 | ⬜ pending |
| SUB-05 | Provider-firewall lint rule blocks Anthropic SDK imports | unit + CI | `go test ./tools/analyzers/providerfirewall/...` · `make lint` | ❌ W0 | ⬜ pending |
| HARN-01 | Envelope role/level drives prompt selection | unit | `go test ./internal/harness/...` | ❌ W0 | ⬜ pending |
| HARN-02 | Wall-clock + iteration + token caps fire | unit + kind | `go test ./internal/harness/caps_test.go` · `ginkgo --label-filter=kind -focus=caps` | ❌ W0 | ⬜ pending |
| HARN-03 | Signed-token verify rejects tampered/expired | unit + kind | `go test ./internal/credproxy/...` · `ginkgo --label-filter=kind -focus=credproxy` | ❌ W0 | ⬜ pending |
| HARN-04 | Secret-pattern redaction (incl. boundary-straddle) | unit | `go test ./internal/harness/redact/...` | ❌ W0 | ⬜ pending |
| HARN-05 | Output-path validator rejects out-of-scope writes | unit + kind | `go test ./internal/harness/...` · `ginkgo --label-filter=kind -focus=output_paths` | ❌ W0 | ⬜ pending |
| HARN-06 | Claude Code headless contract — stubbed in Phase 2 | manual review | (stub satisfies the seam; Phase 3 swaps in real image) | n/a Phase 2 | ⬜ manual |
| PLAN-01 | Admission rejects cyclic Plan via pkg/dag.ComputeWaves | envtest | `ginkgo --label-filter=envtest -focus=cycle` | ❌ W0 | ⬜ pending |
| PLAN-02 | File-touch reconciliation strict/warn modes | envtest | `ginkgo --label-filter=envtest -focus=file-touch` | ❌ W0 | ⬜ pending |
| PLAN-03 | Cycle "recovery" out of scope (verified by absence) | code review | grep for `// recover` or `if cycle` recovery branches — must return zero | n/a | ⬜ manual |
| FAIL-01 | Sibling Tasks in same wave continue on one failure | envtest + kind | `ginkgo -focus=siblings_continue` | ❌ W0 | ⬜ pending |
| FAIL-02 | Per-task indegree decrement (not per-wave) | envtest | `ginkgo -focus=indegree_recomputed` | ❌ W0 | ⬜ pending |
| FAIL-03 | 429 storm absorbed; counter increments | envtest + unit | `ginkgo -focus=429` · `go test ./internal/budget/...` | ❌ W0 | ⬜ pending |
| FAIL-04 | Per-Project budget cap halts dispatch | envtest | `ginkgo -focus=budget_exceeded` | ❌ W0 | ⬜ pending |
| PERSIST-03 | Waves re-derived per reconcile (no cache) | envtest + analyzer | `ginkgo -focus=wave_status` · Phase 1's `make verify-no-aggregates` still passes | ❌ W0 (analyzer already CI-gated) | ⬜ pending |
| ART-01 | PVC layout created by init Job | envtest + kind | `ginkgo -focus=init_job` · kind test reads /workspace from inside subagent | ❌ W0 | ⬜ pending |
| TEST-02 | Full integration tier under 5 min | CI timing | `time make test-int` (CI captures duration, fails on >300s) | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Wave 0 must establish test infrastructure that doesn't exist yet. Without these, no acceptance criterion can be exercised.

- [ ] `pkg/dispatch/envelope_test.go` — round-trip JSON, apiVersion/kind validation (depends on `pkg/dispatch/envelope.go` ship)
- [ ] `test/integration/envtest/suite_test.go` — Ginkgo BeforeSuite spins up envtest, registers CRDs (extends Phase 1's `internal/controller/suite_test.go` pattern)
- [ ] `test/integration/kind/suite_test.go` — Ginkgo BeforeSuite spins up kind cluster, loads images, applies CRDs
- [ ] `test/integration/kind/cluster.yaml` — kind config pinning `kindest/node:v1.33.7@sha256:d26ef333bdb2cbe9862a0f7c3803ecc7b4303d8cea8e814b481b09949d353040`
- [ ] `Makefile` — add `test-int`, `test-int-fast`, `test-int-kind-prep` (build images, kind load) targets
- [ ] `images/stub-subagent/Dockerfile` — multi-stage Go build producing static binary
- [ ] `images/credproxy/Dockerfile` — same
- [ ] `tools/analyzers/providerfirewall/analyzer.go` + `testdata/` — prerequisite for SUB-05 CI gate; multichecker flip in `cmd/tide-lint/main.go` is single-line change
- [ ] `internal/budget/bucket_test.go` — fixtures for rate-limit assertion
- [ ] `internal/credproxy/token_test.go` — HMAC fixture set (tampered, expired, valid)
- [ ] `internal/harness/redact/redact_test.go` — secret-pattern fixtures incl. boundary-straddle case

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Claude Code headless contract satisfied by stub | HARN-06 | Real Claude image swap-in is Phase 3 (REQ-HARN-06 second half). Phase 2 only verifies the seam exists. | Review `internal/harness/` exposes a single `Subagent` interface point; the stub-subagent calls into it; document that swapping the binary is the Phase 3 contract. |
| Cycle "recovery" features absent from codebase | PLAN-03 | Verified by absence — no automated test can prove a feature wasn't added. | `git grep -nE "(recoverCycle|cycleRecover|fix.*cycle|skip.*cycle)" -- ':!*test*'` returns zero hits in non-test code. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags (Ginkgo `--watch` would defeat the 5-min budget)
- [ ] Feedback latency < 30s (unit suite)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
