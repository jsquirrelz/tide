---
phase: 53
slug: chart-config-dashboard-provenance-surfacing
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-21
---

# Phase 53 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (envtest via Ginkgo, helm-template contract tests, kind suite) + vitest (dashboard SPA) |
| **Config file** | Makefile targets (`test`, `test-int`, `lint`, `verify-dashboard-freshness`) / `dashboard/web/vitest` config |
| **Quick run command** | `go test ./internal/controller/... ./cmd/dashboard/... && (cd dashboard/web && npx vitest run)` |
| **Full suite command** | `make test && make lint && make verify-dashboard-freshness && make test-int` |
| **Estimated runtime** | quick ~90s · full ~15-25 min (kind suite dominates) |

---

## Sampling Rate

- **After every task commit:** Run the quick command scoped to the packages touched
- **After every plan wave:** Run `make test && make lint` (+ `verify-dashboard-freshness` when SPA files changed; + helm-template go tests when `hack/helm/` changed)
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner from PLAN.md tasks) | | | CFG-01 / CFG-02 / OBS-04 | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Helm-template render-pair test file (install vs `--is-upgrade`) — CFG-02 proof harness
- [ ] Component test skeleton asserting `VerifyHalted` presentation ≠ `Failed` — OBS-04 proof

*Existing infrastructure (envtest suite, kind suite, vitest, helm-template contract tests) covers the remainder.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Dashboard provenance renders legibly on a live cluster | OBS-04 | Visual quality judgment beyond DOM assertions | Deploy dashboard on kind, open a Task with loop iterations, inspect drawer + badges |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
