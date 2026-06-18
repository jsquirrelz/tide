---
phase: 27
slug: budget-bypass-resume-correctness
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-18
---

# Phase 27 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2 + Gomega (existing) |
| **Config file** | `internal/controller/suite_test.go` (BeforeSuite wires envtest) |
| **Quick run command** | `cd internal/controller && go test -run TestControllers -v -timeout 5m . -ginkgo.focus="BYPASS"` |
| **Full suite command** | `make test-int-fast` (Layer A envtest) |
| **Estimated runtime** | ~90 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick run command (focused on `BYPASS`)
- **After every plan wave:** Run `make test-int-fast`
- **Before `/gsd:verify-work`:** `make test-int-fast` must be green
- **Max feedback latency:** ~90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 27-XX | TBD | TBD | BYPASS-01 | — | Bypass sets `Phase=Running`, no new init Job when `BranchName` set | envtest | `make test-int-fast` | ✅ extend `project_controller_test.go:505` | ⬜ pending |
| 27-XX | TBD | TBD | BYPASS-02 | — | `CloneComplete=true` gates re-clone on resume (GC-safe) | envtest | `make test-int-fast` | ❌ W0 (new spec) | ⬜ pending |
| 27-XX | TBD | TBD | BYPASS-03 | — | `PlannerRolledUpUID` prevents double-count when reporter GC'd | envtest | `make test-int-fast` | ❌ W0 (new spec) | ⬜ pending |
| 27-XX | TBD | TBD | BYPASS-04 | — | Bypass acknowledges spend; re-halt only on new post-bypass spend; condition names which cap | unit + envtest | `go test ./internal/budget/ -run TestIsCapExceeded` + `make test-int-fast` | ❌ W0 (new spec) | ⬜ pending |
| 27-XX | TBD | TBD | BYPASS-05 | — | Reporter spawns + budget rolls up while planner Job still exists; TTL-GC companion | envtest | `make test-int-fast` | ✅ verify GREEN + add scenario | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] New spec for BYPASS-02 clone idempotency (`project_clone_idempotency_test.go` or new spec in `project_phase3_test.go`)
- [ ] New spec for BYPASS-03 TTL-GC double-count in `project_planner_completion_test.go`
- [ ] Extended assertion in existing bypass test (`project_controller_test.go:505`) — BYPASS-01 positive `Phase==Running` check
- [ ] New unit spec for BYPASS-04 acknowledged-spend baseline (`internal/budget/` test) + condition-message envtest
- [ ] TTL-GC companion scenario in `project_planner_completion_test.go` — BYPASS-05

*No framework install needed — envtest infrastructure already in place.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| — | — | All phase behaviors have automated envtest/unit coverage | — |

*All phase behaviors have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
