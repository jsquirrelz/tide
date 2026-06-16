---
phase: 21
slug: cost-cache-observability
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-15
---

# Phase 21 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (backend: metrics, pricing, dispatch, controller) · `vitest` (dashboard) |
| **Config file** | `dashboard/web/vitest.config.ts` (dashboard); none for Go |
| **Quick run command** | `go test ./internal/metrics/... ./internal/subagent/anthropic/... ./pkg/dispatch/...` |
| **Full suite command** | `go test ./internal/metrics/ ./internal/subagent/anthropic/ ./pkg/dispatch/ ./internal/controller/ -count=1 && make lint` · `cd dashboard/web && npm run test && npm run lint` · `make dashboard-frontend` |
| **Estimated runtime** | ~60 seconds (Go quick ~20s; vitest ~15s; full + lint ~60s) |

---

## Sampling Rate

- **After every task commit:** Run the relevant quick command (Go quick for 21-01 tasks; `cd dashboard/web && npm run test` for 21-02 tasks)
- **After every plan wave:** Run the full suite command (Go full + lint, dashboard full + lint)
- **Before `/gsd:verify-work`:** Full suite must be green AND `make lint` exits 0
- **Max feedback latency:** ~20 seconds (quick command per task)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 21-01-T1 | 01 | 1 | OBSV-02 | T-21-01-01/02/05 | Pricing/savings math stays behind provider firewall (D-C1); controller never imports pricing | unit (tdd) | `go test ./internal/subagent/anthropic/ -run TestCacheSavingsCents -v && go test ./pkg/dispatch/ -run TestNewTerminationStub_StaysSmall -v` | ✅ in-task TDD | ⬜ pending |
| 21-01-T2 | 01 | 1 | OBSV-01, OBSV-02 | T-21-01-03 | Locked 4-label set `{project,phase,plan,wave}`; no `task` label (metriccardinality analyzer) | unit (tdd) | `go test ./internal/metrics/ -v -run TestRegistry && go test ./internal/controller/ -count=1 && make lint` | ✅ in-task TDD | ⬜ pending |
| 21-02-T1 | 02 | 1 | OBSV-03 | T-21-02-02/04/05 | Read-only panel; graceful degradation; NaN → "—" | component (tdd, vitest) | `cd dashboard/web && npm run test -- --reporter=verbose && npm run lint` | ✅ in-task TDD | ⬜ pending |
| 21-02-T2 | 02 | 1 | OBSV-01, OBSV-03 | T-21-02-01/03 | `BreakdownKind` enum literal only — no free-form string interpolated into PromQL | component (tdd, vitest) | `cd dashboard/web && npm run test -- --reporter=verbose && npm run lint && make dashboard-frontend` | ✅ in-task TDD | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

All four phase tasks are `tdd="true"` and create their own tests in the RED step before implementation (RED → GREEN → REFACTOR). The test functions that RESEARCH.md §Wave 0 Gaps flagged as "not yet existing" are created **in-task** by the owning task, so no separate Wave 0 plan is required:

- `TestCacheSavingsCents` (`internal/subagent/anthropic/pricing_test.go`) — created in 21-01-T1
- `TestRegistry_CacheSavingsCentsLabelArity` + `AllMetricFamiliesPresent` seed/want updates (`internal/metrics/registry_test.go`) — created in 21-01-T2 (also the OBSV-01 audit guard)
- `TestNewTerminationStub_StaysSmall` regression (`pkg/dispatch/envelope_test.go`) — exists; re-run after the `CacheSavingsCents omitempty` field is added in 21-01-T1
- Cache-efficiency + level-selector describe blocks and the `toHaveLength(4)→(5)` assertion updates (`dashboard/web/src/components/__tests__/TelemetryView.test.tsx`) — created in 21-02-T1 and 21-02-T2

**Resolution: in-task TDD creation covers all Wave 0 gaps.** Each task's automated verify command targets the same test file the task itself creates, so feedback sampling is continuous from the first commit.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live cache-efficiency panel renders against a real Prometheus in a running cluster | OBSV-03 | Requires a deployed dashboard + Prometheus scrape; outside unit/component scope | Deploy to `kind-tide-dogfood`, run a dispatch, open TelemetryView, confirm the trio + sparkline render and the level selector slices by phase/plan/wave. (Deferred to phase verification, not gating per-task.) |

*All unit/component-level phase behaviors have automated verification.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (in-task TDD creation)
- [x] No watch-mode flags
- [x] Feedback latency < 20s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-15
