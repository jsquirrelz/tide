---
phase: 30
slug: resumable-import-partial-tree-resume-adopt-complete-re-plan
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-25
---

# Phase 30 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Ginkgo v2 + Gomega for controller/envtest; plain go test for kind tier) |
| **Config file** | none — existing Makefile targets |
| **Quick run command** | `go test ./internal/controller/... ./cmd/tide/... ./cmd/tide-import/...` |
| **Full suite command** | `make test-int` |
| **Estimated runtime** | ~quick: 60-120s · full (kind): several min |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched package(s)
- **After every plan wave:** Run `make test-int`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** ~120 seconds (quick), full as final gate

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | — | — | TBD | — | N/A | unit/envtest/kind | TBD | ❌ W0 | ⬜ pending |

*Planner fills this map per the derived requirements. Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] New partial-tree fixture (mixed complete/incomplete envelopes) — `testdata/import-partial-fixture/` or analog
- [ ] New Tier-c kind test driving a partial import to `Project=Complete` (analog: `test/integration/kind/import_resume_test.go`)

*Existing controller envtest infrastructure covers the materialization-branch and project-guard assertions.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live partial-salvage resume on `kind-tide-dogfood` | TBD | Real cluster + LLM dispatch ($-metered); the carried-in `salvage-20260618` bundle | Apply run-2 project, observe adopt-complete + re-plan-incomplete drives Project to Complete |

*Automated Tier-c covers the partial-tree-to-completion outcome with stub/fake envelopes; the live run is the dogfood re-attempt (separate, post-phase).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
