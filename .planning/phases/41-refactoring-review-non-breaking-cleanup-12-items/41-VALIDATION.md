---
phase: 41
slug: refactoring-review-non-breaking-cleanup-12-items
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-11
---

# Phase 41 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + Ginkgo v2/Gomega envtest tiers |
| **Config file** | Makefile (test/test-only/test-int-fast targets) |
| **Quick run command** | `go build ./... && go test ./internal/controller/... ./cmd/...` |
| **Full suite command** | `make test` (unit tier incl. envtest Layer A) |
| **Estimated runtime** | quick ~60s · full ~6-8 min |

---

## Sampling Rate

- **After every task commit:** Run the item's per-seed Verify command (see map) — each seed item carries its own targeted `go test -run` / `rg` assertion
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** `make test` green + `make test-int-fast` (envtest 56 specs) green
- **Max feedback latency:** ~480 seconds (full unit tier)

---

## Per-Task Verification Map

Populated from the seed's per-item Verify lines (RESEARCH.md re-anchored them against HEAD). Task IDs land at plan time; the item→check mapping is fixed:

| Item | Requirement | Test Type | Automated Command | File Exists | Status |
|------|-------------|-----------|-------------------|-------------|--------|
| 1 typed Phase constants | REFAC (mint at plan) | build+grep | `go build ./... && rg '"Succeeded"' internal/controller cmd --count-matches` (target: only constant defs) | ✅ | ⬜ pending |
| 2 IsStatusConditionTrue | REFAC | unit | `go test ./internal/controller/... -run 'Halt\|Budget'` | ✅ | ⬜ pending |
| 4 dead code deletion | REFAC | build+vet | `go build ./... && go vet ./... && go test ./internal/controller/... ./cmd/manager/...` | ✅ | ⬜ pending |
| 5 mojibake | REFAC | grep | `rg 'â' internal/controller/dispatch_helpers.go internal/subagent/anthropic/subagent.go --count` returns nothing; `go build ./...` | ✅ | ⬜ pending |
| 6 test-helper unification | REFAC | test (full pkg) | `go test ./internal/controller/...` (FULL package — OQ-2: no `-run` narrowing) | ✅ | ⬜ pending |
| 7 dispatch-holds extraction | REFAC | envtest | `go test ./internal/controller/... -run 'Gates\|Halt\|Budget\|Import'` + `make test-int-fast` per migrated controller | ✅ | ⬜ pending |
| 8 PlannerDeps carrier | REFAC | wiring tests | `go test ./cmd/manager/... ./internal/controller/...` (wiring_test.go + wave_dispatcher_wiring_test.go extended) | ✅ | ⬜ pending |
| 9 polarity normalization | REFAC | unit+grep | `go test ./internal/controller/... -run 'Parent'` + `rg -l ConditionParentUnresolved cmd/dashboard internal test` swept | ✅ | ⬜ pending |
| 10 status-helper extraction | REFAC | envtest | `go test ./internal/controller/... -run 'Gates\|Approve\|Boundary'` | ✅ | ⬜ pending |
| 11 magic-literal centralization | REFAC | grep+unit | `rg '"tide-projects"' internal \| grep -v _test` returns only constant def; `go test ./internal/controller/...` | ✅ | ⬜ pending |
| 12 log-style (AGENTS.md amendment) | REFAC | grep | `rg -l 'dispatch held\|creating job' internal test .planning` unchanged before/after (doc-only change) | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements — the controller test suite, envtest Layer A, and the Makefile verify-gates already exist; no new framework or fixtures needed. Behavioral invariance is the contract: existing tests must stay green through every refactor.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| None | — | — | All phase behaviors have automated verification (refactor-invariance = existing suites green + per-item greps). |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (none)
- [ ] No watch-mode flags
- [ ] Feedback latency < 480s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
