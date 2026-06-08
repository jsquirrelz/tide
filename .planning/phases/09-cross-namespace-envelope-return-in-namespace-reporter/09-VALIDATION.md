---
phase: 9
slug: cross-namespace-envelope-return-in-namespace-reporter
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-08
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + Ginkgo v2 / Gomega · envtest (Layer A) · kind (Layer B integration) |
| **Config file** | Makefile targets; `test/integration/kind/suite_test.go` |
| **Quick run command** | `go test ./internal/... ./cmd/... ./api/... ./pkg/... -short` |
| **Full suite command** | `make test-int` (Layer A envtest + Layer B kind) |
| **Estimated runtime** | ~30s quick; multi-min full (kind) |

---

## Sampling Rate

- **After every task commit:** `go test <touched packages> -short`
- **After every plan wave:** `make test-int-fast` (Layer A envtest) ; Layer B kind specs for cross-namespace CR-creation behavior
- **Before `/gsd-verify-work`:** full `make test-int` green AND the live medium-sample acceptance (real-Claude end-to-end Complete) per RESEARCH.md "## Validation Architecture"
- **Max feedback latency:** ~30s for unit; minutes for kind/live

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 01-1 | 09-01 | 1 | REQ-09-05 | T-09-01 | cost from price table; table-miss fails loud non-zero | unit | `go test ./internal/subagent/anthropic/ -run TestEstimatedCostCents -count=1` | ❌ Wave 0 | ⬜ pending |
| 01-2 | 09-01 | 1 | REQ-09-05 | T-09-01 | cost wired before EnvelopeOut assembly; rollup unchanged | unit | `go test ./internal/subagent/anthropic/... ./internal/budget/... -short -count=1` | ✅ extend | ⬜ pending |
| 02-1 | 09-02 | 1 | REQ-09-03 | T-09-03 | TerminationStub subset stays <4KB | unit | `go test ./pkg/dispatch/ -run TerminationStub -count=1` | ❌ Wave 0 | ⬜ pending |
| 02-2 | 09-02 | 1 | REQ-09-03 | T-09-03 | shims write stub not full envelope; full out.json on PVC | unit | `go test ./cmd/claude-subagent/... ./cmd/stub-subagent/... -short -count=1` | ✅ extend | ⬜ pending |
| 02-3 | 09-02 | 1 | REQ-09-03 | T-09-04 | stub Task children carry SourcePath (MinLength=1) | unit | `go test ./cmd/stub-subagent/... -short -count=1` | ✅ extend | ⬜ pending |
| 03-1 | 09-03 | 1 | REQ-09-02 | T-09-06 | Manager stops reading prompt cross-ns; EnvelopeIn.PromptPath set | unit/integration | `go test ./internal/controller/ -run 'EnvelopeIn|Task' -short -count=1` | ✅ extend | ⬜ pending |
| 03-2 | 09-03 | 1 | REQ-09-02 | T-09-05 | in-pod prompt read with traversal defense | unit | `go test ./internal/subagent/anthropic/ -run Prompt -count=1` | ❌ Wave 0 | ⬜ pending |
| 04-1 | 09-04 | 1 | REQ-09-01 | T-09-08, T-09-09 | materialize + spec-ref guard + allowlist in internal/reporter; no import cycle | unit | `go test ./internal/reporter/... ./internal/controller/... -short -count=1` | ✅ move | ⬜ pending |
| 04-2 | 09-04 | 1 | REQ-09-01 | T-09-07 | least-privilege tide-reporter Role (create+get on 5 Kinds) | render | `helm template charts/tide --set projectNamespaces='{tide-sample-medium}'` grep create-only Role | ❌ Wave 0 | ⬜ pending |
| 04-3 | 09-04 | 1 | REQ-09-01 | T-09-07 | medium sample provisions tide-reporter RBAC | render | `kubectl apply --dry-run=client -f examples/projects/medium/per-namespace-resources.yaml` | ✅ extend | ⬜ pending |
| 05-1 | 09-05 | 2 | REQ-09-01 | T-09-10, T-09-11 | reporter reads out.json local + materializes idempotently | unit | `go test ./cmd/tide-reporter/... -count=1` | ❌ Wave 0 | ⬜ pending |
| 05-2 | 09-05 | 2 | REQ-09-01 | T-09-12 | reporter image builds + kind preload | build | `docker build -f images/tide-reporter/Dockerfile .` | ❌ Wave 0 | ⬜ pending |
| 05-3 | 09-05 | 2 | REQ-09-01 | T-09-11 | envtest: child CR appears with ownerRef+specRef; idempotent | integration (envtest) | `make test-int-fast` | ❌ Wave 0 | ⬜ pending |
| 06-1 | 09-06 | 3 | REQ-09-01 | T-09-14 | buildReporterJob least-privilege SA + subPath + role label | unit | `go test ./internal/controller/ -run ReporterJob -count=1` | ❌ Wave 0 | ⬜ pending |
| 06-2 | 09-06 | 3 | REQ-09-01 | T-09-13, T-09-15 | handlers spawn reader Job; no inline materialize; tiny-status read retained | unit | `go test ./internal/controller/... -short -count=1` + `grep -c MaterializeChildCRDs ...` == 0 | ✅ extend | ⬜ pending |
| 06-3 | 09-06 | 3 | REQ-09-01 | T-09-13 | Layer B: Manager-spawns-reader-Job → children appear (stub path) | integration (kind) | `make test-int` | ❌ Wave 0 | ⬜ pending |
| 07-2 | 09-07 | 4 | REQ-09-04, REQ-09-06 | T-09-16, T-09-18 | medium → legitimate Complete, branch pushed, costSpentCents>0 | live (manual) | parked-minikube medium run (not CI-gated — cost) | ❌ manual gate | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

These test files / fixtures must exist before (or as the first task of) their plan:

- [ ] `internal/subagent/anthropic/pricing_test.go` — per-model cost computation + table-miss (plan 09-01 Task 1, TDD)
- [ ] `pkg/dispatch/envelope_test.go` — `TestNewTerminationStub_StaysSmall` (<4KB) + subset assertions (plan 09-02 Task 1, TDD; resurrect #11 writer-half)
- [ ] `internal/subagent/anthropic/subagent_test.go` — in-pod prompt read happy path + traversal-reject cases (plan 09-03 Task 2, TDD)
- [ ] `internal/reporter/materialize_test.go` — MOVE the existing MaterializeChildCRDs/childrenAlreadyMaterialized tests from `internal/controller/dispatch_helpers_test.go` (plan 09-04 Task 1)
- [ ] `cmd/tide-reporter/main_test.go` — drive `run()` with a fake client: read out.json → materialize → idempotent re-run; reject paths (plan 09-05 Task 1, TDD)
- [ ] `test/integration/envtest/reporter_materialize_test.go` — cross-ns create + ownerRef + specRef + idempotency (plan 09-05 Task 3)
- [ ] `internal/controller/reporter_jobspec_test.go` — buildReporterJob SA/subPath/args/ownerRef/role-label/securityContext (plan 09-06 Task 1, TDD)
- [ ] `test/integration/kind/reporter_pod_test.go` — Manager-spawns-reader-Job → children appear via watch on the stub path (plan 09-06 Task 3)
- [ ] Framework: existing Ginkgo/envtest/kind infra covers this — no install needed

*See RESEARCH.md "## Validation Architecture" for the cross-namespace test approach (envtest for the reader binary's create logic; kind Layer B for Manager-spawns-reader-Job → children-appear; live medium-sample for the real-Claude acceptance).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live real-Claude medium-sample end-to-end Complete (real branch pushed, costSpentCents>0) | REQ-09-04 / REQ-09-06 | Requires real ANTHROPIC_API_KEY + live cluster (parked minikube); not CI-gated (cost) | Plan 09-07: apply the medium sample in the documented order on the parked minikube repro; watch the full tree to Complete; assert all descendants Succeeded, a `tide/run-medium-project-*` branch pushed to the in-cluster http:// remote, and `status.budget.costSpentCents > 0` under the cap |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (the live acceptance is the documented manual gate)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 30s (unit)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planned (per-task map filled; Wave 0 enumerated)
