---
phase: 23
slug: schema-migration-cross-scope-dependency-model
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-16
---

# Phase 23 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega (envtest/integration), stdlib `go test` (unit) |
| **Config file** | none — `go test ./...` / `make test-int` |
| **Quick run command** | `go test ./api/v1alpha2/... ./internal/controller/... ./internal/metrics/... -count=1` |
| **Full suite command** | `make test-int` |
| **Estimated runtime** | ~90–300 seconds (quick); full envtest suite several minutes |

---

## Sampling Rate

- **After every task commit:** Run `go test ./api/v1alpha2/... ./internal/controller/... -count=1`
- **After every plan wave:** Run `make test-int`
- **Before `/gsd:verify-work`:** Full suite must be green AND `make verify-no-aggregates verify-dag-imports` pass
- **Max feedback latency:** ~300 seconds

---

## Per-Task Verification Map

> Task IDs are placeholders until the planner finalizes plan/wave structure. Every phase requirement maps to at least one automated check below; the planner MUST preserve this requirement→test coverage.

| Requirement | Behavior | Test Type | Automated Command | File Exists | Status |
|-------------|----------|-----------|-------------------|-------------|--------|
| SCHEMA-01 | `WaveSpec.ProjectRef` replaces `PlanRef`; `WaveIndex` is the global monotonic index | unit | `go test ./api/v1alpha2/... -run TestWaveSpec` | ❌ W0 | ⬜ pending |
| SCHEMA-02 | Metric `wave` label emits the global `WaveIndex`; label set stays `{project,phase,plan,wave}`; `task` label forbidden | unit | `go test ./internal/metrics/... -run TestWaveLabel` | ❌ W0 | ⬜ pending |
| SCHEMA-03 | Old v1alpha1-shape object rejected with a `RequiresReinstall`-style status condition (no silent corruption) | envtest | `go test ./internal/controller/... -run TestOldShapeRejection` | ❌ W0 | ⬜ pending |
| DEPS-01 | `Task.DependsOn` accepts cross-scope IDs; plan-local (D-F1) restriction absent | unit | `go test ./api/v1alpha2/... -run TestTaskDependsOn` | ❌ W0 | ⬜ pending |
| DEPS-02 | `Plan.DependsOn` (and generalized `Phase`/`Milestone` `DependsOn`) present, validate, accept any-level targets | unit | `go test ./api/v1alpha2/... -run TestPlanDependsOn` | ❌ W0 | ⬜ pending |
| DEPS-03 | Cross-scope cycle (task-level edges) detected at validation, surfaced as a Project status condition naming involved nodes; no run starts | envtest | `go test ./internal/controller/... -run TestGlobalCycleDetection` | ❌ W0 | ⬜ pending |
| (guard) | `verify-no-aggregates` passes over `api/v1alpha2` (no `Schedule`/`Waves[]`/`IndegreeMap`/cached schedule) | CI make target | `make verify-no-aggregates` | ⚠️ Makefile glob update needed | ⬜ pending |
| (guard) | `verify-dag-imports` passes (pkg/dag stays k8s-free) | CI make target | `make verify-dag-imports` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `api/v1alpha2/` package — all type files (`wave_types.go`, `task_types.go`, `plan_types.go`, `phase_types.go`, `milestone_types.go`, `project_types.go`, `shared_types.go`) + `groupversion_info.go`
- [ ] `api/v1alpha2/zz_generated.deepcopy.go` — generated via `make generate`
- [ ] `internal/controller/project_controller_cycle_test.go` — covers DEPS-03 (cross-scope cycle node-surfacing)
- [ ] `internal/controller/project_controller_v2_guard_test.go` — covers SCHEMA-03 old-object fail-closed rejection
- [ ] `internal/metrics/*_test.go` — covers SCHEMA-02 global-wave label resemantics
- [ ] Update `Makefile` `verify-no-aggregates` grep glob to include `api/v1alpha2/*_types.go`

*Migration doc (SCHEMA-03) is a manual artifact — see below.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Migration/conversion doc exists with version bump + reinstall steps | SCHEMA-03 | Documentation artifact, not executable | Confirm a migration doc names the breaking change, the version bump location, and the CRD delete+reapply reinstall procedure that avoids stranded etcd objects |
| Clean-break upgrade on `kind-tide-dogfood` does not silently corrupt | SCHEMA-03 | Requires a live cluster smoke test | Apply v1alpha2 CRDs over a cluster holding a v1alpha1 object; confirm the object is either rejected loudly or removed by the documented reinstall — never silently mis-run |

---

## Validation Sign-Off

- [ ] All requirements have an automated verify or a justified manual entry
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING test references
- [ ] No watch-mode flags
- [ ] Feedback latency < 300s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
