---
phase: 15
slug: paper-cuts
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-12
---

# Phase 15 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + Ginkgo v2.28/Gomega (envtest), Vitest (dashboard/web) |
| **Config file** | Makefile (test targets), dashboard/web/vitest.config.ts |
| **Quick run command** | `go test ./internal/... ./cmd/...` (Go) / `npm test --prefix dashboard/web` (dashboard) |
| **Full suite command** | `make test` (unit+envtest); `make test-int` (kind Layer B, heavy — read MAKE_EXIT + grep '^--- FAIL', not just Ginkgo summary) |
| **Estimated runtime** | ~120s unit/envtest; kind suite minutes-scale (one heavy run at a time on the 7.65 GiB VM) |

---

## Sampling Rate

- **After every task commit:** Run the affected package's `go test ./internal/<pkg>/...` or `npm test --prefix dashboard/web -- --run <file>`
- **After every plan wave:** Run `make test` (full unit/envtest); `make test-int` only at phase-final gate
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds (unit/envtest layer)

---

## Per-Task Verification Map

*To be filled by planner — one row per task. Run-1 regression symmetry: each CUTS requirement maps to a regression test reproducing the run-1 symptom.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | — | — | CUTS-01..07 | — | — | unit/envtest/vitest | TBD | ⬜ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — envtest suites exist for all four controllers, cmd/tide has table-driven CLI tests, dashboard/web has Vitest + Testing Library. No new framework installs needed.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `tide artifact-get` against a live cluster streams real artifact bytes | CUTS-04 | End-to-end PVC + pod-exec path needs a real kind cluster with a populated workspace PVC | Run a stub project to seed the PVC, then `tide artifact-get <ns>/<proj>/MILESTONE.md` and compare bytes |
| Dashboard running-waves view renders live | CUTS-06 | Visual SSE behavior on a live cluster | Open dashboard with ≥2 plans running; verify wave cards + click-through |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
