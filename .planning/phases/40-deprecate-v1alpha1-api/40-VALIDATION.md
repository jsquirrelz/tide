---
phase: 40
slug: deprecate-v1alpha1-api
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-06
---

# Phase 40 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test + Ginkgo v2.28 / Gomega (envtest Layer A; kind Layer B) |
| **Config file** | Makefile (test targets), test/integration/kind for Layer B |
| **Quick run command** | `go build ./... && make test` (unit + envtest tier) |
| **Full suite command** | `make test-int` (read MAKE_EXIT + grep '^--- FAIL', not just Ginkgo summary) |
| **Estimated runtime** | ~120 seconds (quick) / ~15+ min (full kind suite, one heavy run at a time) |

---

## Sampling Rate

- **After every task commit:** Run `go build ./... && make test`
- **After every plan wave:** Run envtest suite; `make test-int` at phase boundary only (constrained-VM recipe: fresh kind cluster per heavy run)
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner from PLAN.md task breakdown) | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Existing envtest + kind infrastructure covers the crank; no new framework needed
- [ ] End-state grep assertion harness: `grep -rE 'v1alpha1|v1alpha2'` outside `docs/migration/` and `.planning/` returns 0 (add as verification task, not new tooling)

*Existing infrastructure covers all phase requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Reinstall path on a live cluster | TBD (minted at plan time) | Requires a real kind cluster reinstall cycle | Fresh kind cluster → install old CRDs + sample Project → upgrade to v1alpha3 chart via documented reinstall recipe → RequiresReinstall guard fires for stale-shape objects; re-applied v1alpha3 Project reconciles |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
