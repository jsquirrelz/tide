---
phase: 17
slug: address-tech-debt-plan-label-backfill-gate-hardening
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-12
---

# Phase 17 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2 + Gomega (envtest) for controllers; `go test` for CLI/unit |
| **Config file** | `Makefile` targets (`test`, `test-int`); envtest via setup-envtest |
| **Quick run command** | `go test ./internal/controller/... -run <Reconciler> -count=1` |
| **Full suite command** | `make test` (envtest) then `make test-int` (kind integration) |
| **Estimated runtime** | ~60–120s for targeted controller envtest; minutes for `make test-int` |

---

## Sampling Rate

- **After every task commit:** Run targeted `go test ./internal/controller/... -run <ReconcilerUnderEdit> -count=1`
- **After every plan wave:** Run `make test` (full envtest)
- **Before `/gsd:verify-work`:** `make test` green AND `make test-int` MAKE_EXIT=0 (read the echoed exit + grep for `^--- FAIL|^FAIL\s` per CLAUDE.md — Ginkgo green ≠ make green)
- **Max feedback latency:** ~120 seconds (targeted envtest)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 17-01-xx | 01 | 1 | DEBT-01 (Plan label backfill) | — | Pre-existing unlabeled Plan CR gets `tideproject.k8s/project` stamped on reconcile, becomes visible to `tide approve`/`tide resume` label selectors | unit (envtest) | `go test ./internal/controller/... -run PlanReconciler -count=1` | ❌ W0 (new spec) | ⬜ pending |
| 17-02-xx | 02 | 1 | DEBT-02 (WR-10 reject short-circuit, milestone+phase) | — | Rejected Project does NOT spawn a new reporter Job at milestone/phase level after reject | unit (envtest) | `go test ./internal/controller/... -run 'MilestoneReconciler|PhaseReconciler' -count=1` | ❌ W0 (new spec) | ⬜ pending |
| 17-03-xx | 03 | 1 | DEBT-03 (WR-06 approve guard) | — | Approve guard scope matches chosen semantics (narrowed to the approved level OR applied to `--wave`); strict failure profile honored | unit | `go test ./cmd/tide/... -run Approve -count=1` | ❌ W0 (new spec) | ⬜ pending |
| 17-04-xx | 04 | 1 | DEBT-04 (CR-01 envelope-read parity) | — | Plan envelope-read transient error → non-fatal requeue (not terminal `Failed`), matching milestone/phase Pitfall-1 | unit (envtest) | `go test ./internal/controller/... -run PlanReconciler -count=1` | ❌ W0 (new spec) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] New envtest spec rows in `internal/controller/plan_controller_test.go` (backfill + envelope-read parity) — mirror `phase_controller_test.go` backfill spec
- [ ] New envtest spec rows in `internal/controller/milestone_controller_test.go` and `phase_controller_test.go` (reject-before-reporter-spawn)
- [ ] New `cmd/tide/approve_test.go` row(s) for the chosen WR-06 semantics
- [ ] 15-WR-03 Project-parent test row (if 15-WR-03 folded in) in the reporter-edge test table

*Existing Ginkgo/envtest + `go test` infrastructure covers all phase requirements; no framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Upgrade-path backfill against a real pre-v1.0.1 cluster with unlabeled Plan CRs | DEBT-01 | True upgrade scenario needs a cluster seeded with legacy unlabeled CRs; envtest proves the reconciler logic, not the migration timeline | Seed a kind cluster with a Plan CR lacking the project label, apply the new binary, confirm `tide approve`/`tide resume --retry-failed` discovers it |

*All core reconciler/CLI behaviors have automated envtest/unit verification; only the end-to-end upgrade migration is manual.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-13 (all 4 DEBT envtest/unit specs green; 17-VERIFICATION passed)
