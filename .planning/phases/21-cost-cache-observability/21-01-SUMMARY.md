---
phase: 21-cost-cache-observability
plan: "01"
subsystem: metrics, anthropic-provider, dispatch
tags: [observability, prometheus, cache, savings, tdd]
dependency_graph:
  requires: []
  provides:
    - pkg/dispatch.Usage.CacheSavingsCents
    - internal/subagent/anthropic.cacheSavingsCents
    - internal/metrics.CacheSavingsCentsTotal
  affects:
    - internal/controller/task_controller.go (emitTaskMetrics)
tech_stack:
  added: []
  patterns:
    - TDD RED/GREEN for pricing helper and registry counter
    - Provider firewall D-C1: savings math stays in internal/subagent/anthropic/
    - Truncation division for savings (conservative, never over-reports)
    - Per-instance a.prices clone (T-14-02) used in cacheSavingsCents
key_files:
  created: []
  modified:
    - pkg/dispatch/envelope.go
    - internal/subagent/anthropic/pricing.go
    - internal/subagent/anthropic/subagent.go
    - internal/subagent/anthropic/pricing_test.go
    - internal/metrics/registry.go
    - internal/controller/task_controller.go
    - internal/metrics/registry_test.go
    - internal/eval/render_test.go
    - cmd/credproxy/main.go
decisions:
  - "cacheSavingsCents uses truncation division (not ceiling) — conservative for savings, floor vs ceil of estimatedCostCents"
  - "Silent fallback to conservativeTier on unknown model in cacheSavingsCents (stderr noise reserved for cost helper)"
  - "Pre-existing lint issues in eval/render_test.go and credproxy/main.go fixed as Rule 1 deviations to unblock make lint acceptance criterion"
metrics:
  duration: ~15 minutes
  tasks_completed: 2
  files_changed: 9
  completed_date: "2026-06-16"
requirements:
  - OBSV-01
  - OBSV-02
---

# Phase 21 Plan 01: Cache Savings Counter End-to-End Summary

**One-liner:** `tide_cache_savings_cents_total{project,phase,plan,wave}` counter added end-to-end — computed in the Anthropic provider via truncation division, carried on `Usage.CacheSavingsCents`, registered in the metrics registry, emitted in `emitTaskMetrics()` next to `CostCentsTotal`.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add CacheSavingsCents to Usage, pricing.go helper, and subagent.go call site | 847c56f | envelope.go, pricing.go, subagent.go, pricing_test.go |
| 2 | Register CacheSavingsCentsTotal counter + emit in emitTaskMetrics + registry tests | f724acc | registry.go, task_controller.go, registry_test.go, render_test.go, credproxy/main.go |

## Verification Results

```
go test ./internal/metrics/ ./internal/subagent/anthropic/ ./pkg/dispatch/ ./internal/controller/ -count=1
ok  internal/metrics                   0.989s
ok  internal/subagent/anthropic        0.534s
ok  pkg/dispatch                       1.352s
ok  internal/controller                60.892s

make lint
0 issues.
```

All 4 packages green. `make lint` exits 0 (metriccardinality analyzer confirmed no `"task"` label; providerfirewall analyzer confirmed no pricing import in controller).

## Source-Level Assertions (from plan `<verification>`)

| Assertion | Result |
|-----------|--------|
| `grep -c "tide_cache_savings_cents_total" internal/metrics/registry.go` | 1 |
| `grep -c "cacheSavingsCents,omitempty" pkg/dispatch/envelope.go` | 1 |
| `grep -c "func.*Anthropic.*cacheSavingsCents" internal/subagent/anthropic/pricing.go` | 1 |
| `grep -c "usage.CacheSavingsCents = a.cacheSavingsCents" internal/subagent/anthropic/subagent.go` | 1 |
| `grep -c "CacheSavingsCentsTotal" internal/controller/task_controller.go` | 1 |
| `grep -c "TestCacheSavingsCents" internal/subagent/anthropic/pricing_test.go` | 2 (comment + func decl) |
| `grep -c "CacheSavingsCentsLabelArity" internal/metrics/registry_test.go` | 2 (comment + func decl) |

## TDD Gate Compliance

**Task 1 (RED → GREEN):**
- RED: `TestCacheSavingsCents` added first — confirmed compile failure (`cacheSavingsCents undefined`)
- GREEN: `cacheSavingsCents` method added to `pricing.go` → all 5 sub-tests pass

**Task 2 (RED → GREEN):**
- RED: registry test seeds + arity test added — confirmed compile failure (`CacheSavingsCentsTotal undefined`)
- GREEN: `CacheSavingsCentsTotal` declared and registered in `registry.go` → all registry tests pass

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed pre-existing lint failures blocking `make lint` acceptance criterion**
- **Found during:** Task 2 verification (`make lint` exit 2)
- **Issue:** Two pre-existing lint issues from Phase 20 prevented `make lint` from exiting 0:
  - `internal/eval/render_test.go:172`: `tc := tc` loop-variable copy unnecessary in Go 1.26+ (`copyloopvar`)
  - `cmd/credproxy/main.go:70`: `flag.StringVar` line 205 chars, exceeds 120-char limit (`lll`)
- **Confirmed pre-existing:** `git stash` + `make lint` reproduced both errors before any plan changes
- **Fix:**
  - Removed `tc := tc` in `render_test.go` (Go 1.22+ loop semantics make it redundant)
  - Wrapped long `flag.StringVar` line in `credproxy/main.go` with string concatenation
- **Files modified:** `internal/eval/render_test.go`, `cmd/credproxy/main.go`
- **Commit:** f724acc (included in Task 2 commit)

## Known Stubs

None — all fields are computed from real API response data (CacheReadTokens populated by the Anthropic stream parser).

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries introduced. The new `Usage.CacheSavingsCents int64` field is an internal carry field; the new Prometheus counter adds to the existing telemetry surface already covered by the plan's threat model.

## Self-Check: PASSED
