---
phase: 04
plan: 01
subsystem: observability
tags: [metrics, prometheus, api-constants, gates-prep, w-1-prep, d-o2, d-w1, d-g2]
dependency_graph:
  requires: []
  provides:
    - "internal/metrics package — central Prometheus registry"
    - "api/v1alpha1.PhasePushLeakBlocked — W-1 phase constant"
    - "api/v1alpha1.ConditionWaveOrLevelPaused + 4 Reason constants"
    - "cmd/manager blank import triggering metric registration at boot"
  affects:
    - "04-02 (metriccardinality analyzer) — registry.go is the canonical 'clean' fixture"
    - "04-04 (TaskReconciler gates) — consumes ConditionWaveOrLevelPaused + Reason vocabulary"
    - "04-05 (up-stack reconciler gates) — same"
    - "04-06 (boundary push trigger) — increments PushJobsTotal + SecretLeakBlockedTotal + sets PhasePushLeakBlocked"
    - "All Phase 4 reconciler edits that Inc() a metric — single import path: internal/metrics"
tech_stack:
  added: []
  patterns:
    - "blank-import side-effect registration (mirrors internal/budget)"
    - "package-level *CounterVec / *HistogramVec exported variables"
    - "init()-time MustRegister on sigs.k8s.io/controller-runtime/pkg/metrics.Registry"
key_files:
  created:
    - internal/metrics/registry.go
    - internal/metrics/doc.go
    - internal/metrics/registry_test.go
    - api/v1alpha1/phase4_constants_test.go
    - cmd/manager/metrics_test.go
  modified:
    - api/v1alpha1/project_types.go
    - api/v1alpha1/shared_types.go
    - cmd/manager/main.go
    - go.mod
    - go.sum
decisions:
  - "ProviderRateLimitHitsTotal is re-exported (variable alias), NOT re-registered — duplicate MustRegister on controller-runtime registry panics. internal/budget retains the canonical registration; internal/metrics gives callers a single import path."
  - "TestMetricsBlankImportPresent uses static source-grep on cmd/manager/main.go instead of runtime registration check, because the test file's own internal/metrics import would always fire init() — masking a missing main.go wire-up."
  - "Counter families need at least one labeled child to emit from Gather(). Tests seed `.WithLabelValues(\"__seed__\", ...).Add(0)` to materialize the family without polluting any real bucket."
metrics:
  duration_minutes: 18
  completed_date: 2026-05-19
  tasks_completed: 3
  files_created: 5
  files_modified: 5
  commits: 6
---

# Phase 4 Plan 01: Central Prometheus Registry + API Constants Summary

Ship the central `internal/metrics` package registering all seven Phase 4 v1 metrics on the controller-runtime registry, plus the API constants downstream gate-policy and W-1 plans consume.

## What landed

### internal/metrics/registry.go — 7 metric registrations on `crmetrics.Registry`

| Metric                            | Type      | Labels                          | Arity | Notes                                       |
| --------------------------------- | --------- | ------------------------------- | ----- | ------------------------------------------- |
| `tide_waves_dispatched_total`     | counter   | `{project, phase, plan}`        | 3     | D-O2 — rollups at the Plan level            |
| `tide_tasks_completed_total`      | counter   | `{project, phase, plan}`        | 3     | D-O2 — no `task` label (Pitfall 17)         |
| `tide_tasks_failed_total`         | counter   | `{project, phase, plan, reason}` | 4    | reason ∈ {exit-1, gitleaks, lease, auth, internal, budget} |
| `tide_dispatch_latency_seconds`   | histogram | `{level}`                       | 1     | buckets `[0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800]` (D-Discretion §195) |
| `tide_secret_leak_blocked_total`  | counter   | `{project, phase, plan}`        | 3     | **W-1 lands here** (D-W1)                   |
| `tide_push_jobs_total`            | counter   | `{project, outcome}`            | 2     | outcome ∈ {success, leak, lease, auth, internal} |
| `tide_budget_overruns_total`      | counter   | `{project}`                     | 1     | tracks Phase 2 D-D2 cap-hit events           |

Plus `ProviderRateLimitHitsTotal` re-exported (NOT re-registered) from `internal/budget`. The variable alias gives Phase 4 callers a single import path; the registration stays in `internal/budget` to avoid duplicate-registration panic.

`internal/metrics/doc.go` documents the cardinality discipline (Pitfall 17), the re-export contract, and the reason/outcome/level enumerations for future contributors.

### API constants

`api/v1alpha1/project_types.go` — new Phase constant:

```go
PhasePushLeakBlocked = "PushLeakBlocked"  // D-W1 — distinct from PhasePushLeaseFailed
```

`api/v1alpha1/shared_types.go` — new Condition + 4 Reasons:

```go
ConditionWaveOrLevelPaused = "WaveOrLevelPaused"  // D-G2
ReasonAwaitingApproval     = "AwaitingApproval"   // gate=approve, no annotation yet
ReasonPausedAtBoundary     = "PausedAtBoundary"   // gate=pause OR PauseBetweenWaves
ReasonRejectedByUser       = "RejectedByUser"     // tide reject annotation set
ReasonResumedByUser        = "ResumedByUser"      // tide resume cleared reject
```

Plans 04-04, 04-05, 04-06 set `ConditionWaveOrLevelPaused` on `Status.Conditions` with one of these four Reasons at every gate-policy seam.

### cmd/manager/main.go — blank import

```go
// Phase 4 D-O2: blank-import the central metric registry so its init()
// registers all 7 Phase 4 counters/histograms on
// sigs.k8s.io/controller-runtime/pkg/metrics.Registry at Manager start.
_ "github.com/jsquirrelz/tide/internal/metrics"
```

Placed inline within the existing `github.com/jsquirrelz/tide/*` import group, preserving goimports ordering. No other manager wiring touched — OTel tracing lands in plan 04-03.

## Test coverage

- `api/v1alpha1/phase4_constants_test.go` — table-driven constant value assertions + distinctness check vs `PhasePushLeaseFailed`.
- `internal/metrics/registry_test.go` — registration (7 families on `crmetrics.Registry`), label arity per metric, bucket-slice source grep, no-`"task"`-label grep guard.
- `cmd/manager/metrics_test.go` — static source-grep for the blank-import wire-up (`TestMetricsBlankImportPresent`) + runtime `GatherAndCount` audit (`TestMetricsRegistered`).

```
ok  	github.com/jsquirrelz/tide/api/v1alpha1
ok  	github.com/jsquirrelz/tide/internal/metrics
ok  	github.com/jsquirrelz/tide/cmd/manager
```

All three packages pass with `-race`. `make tide-lint` clean.

## What downstream plans now consume

| Downstream plan | Consumes |
|----------------|----------|
| **04-02** (metriccardinality analyzer) | `internal/metrics/registry.go` as the canonical clean fixture (zero `task` labels, all `NewCounterVec` / `NewHistogramVec` calls match the expected AST shape) |
| **04-04** (TaskReconciler gates) | `ConditionWaveOrLevelPaused`, `ReasonAwaitingApproval`, `ReasonPausedAtBoundary` |
| **04-05** (up-stack reconciler gates) | `ConditionWaveOrLevelPaused`, all 4 Reasons (including reject/resume) |
| **04-06** (boundary push trigger + W-1) | `PhasePushLeakBlocked`, `SecretLeakBlockedTotal`, `PushJobsTotal` |
| All counter `.Inc()` sites in Phase 4 reconciler edits | Single import path: `tidemetrics "github.com/jsquirrelz/tide/internal/metrics"` |

## TDD Gate Compliance

All three tasks followed strict RED → GREEN cycles. Commit ledger:

| Task | Phase | Commit | Type | Subject |
|------|-------|--------|------|---------|
| 1 | RED   | `ba40316` | test  | failing tests for Phase 4 API constants |
| 1 | GREEN | `217c1fb` | feat  | Phase 4 API constants — D-W1 phase + D-G2 condition/reasons |
| 2 | RED   | `8a27035` | test  | failing registry tests for Phase 4 metrics |
| 2 | GREEN | `8b3128e` | feat  | central Prometheus registry for Phase 4 metrics |
| 3 | RED   | `7f90813` | test  | failing test for cmd/manager metrics wire-up |
| 3 | GREEN | `bf1baaa` | feat  | blank-import internal/metrics into cmd/manager (D-O2) |

Six commits. RED gates are real (verified by build failure / runtime test failure on RED; passing only after the GREEN implementation lands).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocker] Task 3 test that imports `internal/metrics` directly cannot prove `cmd/manager/main.go`'s blank import**

- **Found during:** Task 3 RED
- **Issue:** The first version of `cmd/manager/metrics_test.go` (which imported `tidemetrics` directly and called `GatherAndCount`) passed even WITHOUT the blank import in `main.go` — because the test file's own import fires `init()` at test-binary load time. That defeats the purpose of asserting the production binary loads the registry.
- **Fix:** Split into two tests: `TestMetricsBlankImportPresent` uses a static source-grep on `cmd/manager/main.go` for the exact blank-import literal (mirroring the pattern in `api/v1alpha1/aggregates_guard_test.go` and `cmd/manager/rbac_docs_test.go`). `TestMetricsRegistered` retains the runtime audit but documents that its purpose is the contract-shape check, not the wire-up check.
- **Files modified:** `cmd/manager/metrics_test.go`
- **Commit:** `7f90813` (final RED with the corrected test design)
- **Confirmed RED:** `TestMetricsBlankImportPresent` failed cleanly until the GREEN commit added the blank import.

**2. [Rule 1 - Bug] Counter families don't emit from `Gather()` until at least one labeled child exists**

- **Found during:** Task 2 GREEN test run
- **Issue:** `TestRegistry_AllMetricFamiliesPresent` initially failed because `*CounterVec` / `*HistogramVec` with zero `WithLabelValues` calls produce no entries in `Registry.Gather()` — a known Prometheus client_golang quirk, not a registration bug. The MustRegister had succeeded; only the post-Gather visibility check was wrong.
- **Fix:** Seed one labeled child per metric using `.WithLabelValues("__seed__", ...).Add(0)` (and `WithLabelValues("__seed__")` for the histogram — the call itself materializes the child). Documented the Prometheus semantics inline in the test.
- **Files modified:** `internal/metrics/registry_test.go`, `cmd/manager/metrics_test.go`
- **Confirmed:** test passes; the seed value (`Add(0)`) does not pollute real bucket aggregates because `0` is the additive identity.

### Architectural decisions auto-applied (no checkpoint)

- **Re-export `ProviderRateLimitHitsTotal` via variable alias** (not re-register). The plan's frontmatter says "re-export aliasing budget" and the task action says "do NOT duplicate it." I implemented the alias as a pointer assignment in `internal/metrics`' init() so callers can use either `budget.ProviderRateLimitHitsTotal` or `metrics.ProviderRateLimitHitsTotal` and write to the same `*CounterVec`. Duplicate `MustRegister` on the controller-runtime registry would panic, so this is the only consistent shape.

## Known Stubs

None. Every metric registered, every constant declared, every wire-up live.

## Threat Flags

None. The plan's `<threat_model>` (T-04-O2 DoS via unbounded labels, T-04-O5 info-disclosure via help text) is fully mitigated by the bounded label sets and grep test. No new threat surface introduced.

## Self-Check: PASSED

Files exist:
- ✅ `internal/metrics/registry.go`
- ✅ `internal/metrics/doc.go`
- ✅ `internal/metrics/registry_test.go`
- ✅ `api/v1alpha1/phase4_constants_test.go`
- ✅ `cmd/manager/metrics_test.go`

Commits exist on worktree branch (`git log --all`):
- ✅ `ba40316` test(04-01): RED — API constants
- ✅ `217c1fb` feat(04-01): GREEN — API constants
- ✅ `8a27035` test(04-01): RED — registry
- ✅ `8b3128e` feat(04-01): GREEN — registry
- ✅ `7f90813` test(04-01): RED — manager wire-up
- ✅ `bf1baaa` feat(04-01): GREEN — manager wire-up

Tests pass with `-race` on all three target packages. Plan verification block satisfied: grep counts ≥ 1 for both new constants, zero `"task"` literals in registry.go, `make tide-lint` clean.
