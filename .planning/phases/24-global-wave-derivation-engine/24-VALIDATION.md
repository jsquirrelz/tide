---
phase: 24
slug: global-wave-derivation-engine
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-16
---

# Phase 24 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2 + Gomega (envtest suite) + plain `go test` for `pkg/dag` |
| **Config file** | `test/integration/envtest/suite_test.go` |
| **Quick run command** | `go test ./pkg/dag/... ./internal/controller/... -count=1 -timeout 60s` |
| **Full suite command** | `make test-int` |
| **Estimated runtime** | ~90 seconds (quick) / several minutes (`make test-int` on constrained VM) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/dag/... ./internal/controller/... -count=1 -timeout 60s`
- **After every plan wave:** Run `make test-int` (full envtest suite)
- **Before `/gsd:verify-work`:** `make test-int` green AND `make verify-no-aggregates verify-dag-imports verify-no-sqlite-dep` green
- **Max feedback latency:** ~90 seconds (quick), full suite at wave boundaries

> ⚠ `make test-int` exit ≠ "Ginkgo green" — the package bundles plain go-tests (helm-template contracts) alongside Ginkgo specs. Read `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'` the log, not just the Ginkgo summary (CLAUDE.md verification protocol).

---

## Per-Task Verification Map

| Req ID | Behavior | Wave | Test Type | Automated Command | File Exists | Status |
|--------|----------|------|-----------|-------------------|-------------|--------|
| EXEC-01 | `ProjectReconciler` assembles ONE global DAG of all Tasks across multiple Plans/Phases/Milestones | 0→derivation | integration (envtest) | `go test ./test/integration/envtest/... -run GlobalDag -v` | ❌ W0 | ⬜ pending |
| EXEC-02 | Global monotonic wave indices; Wave CRs named `tide-wave-<project>-<N>` (not per-plan) | derivation | integration (envtest) | `go test ./test/integration/envtest/... -run GlobalWaveIndex -v` | ❌ W0 | ⬜ pending |
| EXEC-03 | README:54 bidirectional — task→wave label present; wave→tasks label-selector lists | derivation | integration (envtest) | `go test ./test/integration/envtest/... -run BidirectionalIndex -v` | ❌ W0 | ⬜ pending |
| EXEC-04 | Adding/completing a Task re-derives whole schedule O(V+E); no cached schedule in `.status` | derivation | integration (envtest) | `go test ./test/integration/envtest/... -run WaveRederivation -v` | ❌ W0 | ⬜ pending |
| DEPS-03 (regress) | Adversarial cross-scope `dependsOn` cycle surfaces `CycleDetected`, no dispatch | gate | integration (envtest) | `go test ./test/integration/envtest/... -run GlobalCycle -v` | ✓ (extend) | ⬜ pending |
| guards | `verify-no-aggregates` / `verify-dag-imports` / `verify-no-sqlite-dep` stay green through the change | static | `make verify-no-aggregates verify-dag-imports verify-no-sqlite-dep` | ✓ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `test/integration/envtest/global_wave_derivation_test.go` — covers EXEC-01..04 using multi-plan / cross-phase fixtures conforming to the README worked example (tasks α…θ across Plans; assert wave-0/1/2 labels + `tide-wave-<project>-<N>` Wave CRs + bidirectional resolution).
- [ ] Extend shared fixtures: `createSimplePlan` to accept a `phaseRef` (or add `createSimplePhase`/`createSimpleMilestone` helpers) so cross-phase/cross-milestone hierarchy can be declared in tests.
- [ ] Reuse existing helpers in `test/integration/envtest/indegree_test.go` (`makeTask`, `makeTaskWithWaveLabel`) — do not duplicate.

*Existing envtest infrastructure covers harness + most fixtures; the gap is the global-derivation test file and cross-scope fixture helpers.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live re-derivation on a real cluster (Wave CR set updates as Tasks complete) | EXEC-02/04 | Full end-to-end on `kind-tide-dogfood` is heavier than envtest; not required for the gate | Apply a multi-plan Project, `kubectl get waves -n <ns> -w`, complete tasks, observe Wave CR set re-derive |

*All gate-blocking behaviors have automated envtest verification; the live-cluster check is confirmatory only.*

---

## Validation Sign-Off

- [ ] All tasks have automated verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (global-derivation test file + cross-scope fixtures)
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (quick suite)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
