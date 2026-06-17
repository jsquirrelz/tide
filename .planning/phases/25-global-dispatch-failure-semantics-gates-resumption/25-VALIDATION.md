---
phase: 25
slug: global-dispatch-failure-semantics-gates-resumption
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-16
---

# Phase 25 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Ginkgo v2 + Gomega; envtest for controller integration) |
| **Config file** | none — controller-runtime envtest configured in suite_test.go |
| **Quick run command** | `go test ./internal/controller/... ./api/... ./pkg/dag/...` |
| **Full suite command** | `make test-int` |
| **Estimated runtime** | ~180 seconds (envtest integration); unit subset ~30s |

---

## Sampling Rate

- **After every task commit:** Run `{quick run command}`
- **After every plan wave:** Run `{full suite command}`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {N}-01-01 | 01 | 1 | REQ-{XX} | T-{N}-01 / — | {expected secure behavior or "N/A"} | unit | `{command}` | ✅ / ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/controller/global_dispatch_test.go` — stubs for DISP-01 (global indegree across plans/phases/milestones)
- [ ] `internal/controller/failure_halt_test.go` — stubs for DISP-02 (strict vs conservative failure contract)
- [ ] extend `internal/controller/resume_test.go` — RESUME-01 restart re-derivation regression
- [ ] reuse existing gate tests for DISP-03 (task-gate hold composes with global indegree)

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| {behavior} | REQ-{XX} | {reason} | {steps} |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** {pending / approved YYYY-MM-DD}
