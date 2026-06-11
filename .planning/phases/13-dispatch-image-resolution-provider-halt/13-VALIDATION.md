---
phase: 13
slug: dispatch-image-resolution-provider-halt
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-11
---

# Phase 13 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (envtest via Ginkgo/Gomega) + helm template render gates + test/integration/kind Layer A/B |
| **Config file** | Makefile (`test`, `test-int-fast`, `test-int` targets) |
| **Quick run command** | `go test ./internal/... ./cmd/... -short -timeout 120s` |
| **Full suite command** | `make test` (unit tier) — Layer A via `make test-int-fast`; full `make test-int` at phase gate (chart change touches the harness install) |
| **Estimated runtime** | ~120 s quick · ~5 min `make test` · ~30 min `make test-int` (one heavy run at a time on the 7.65 GiB VM) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/... ./internal/credproxy/... ./cmd/... -timeout 600s`
- **After every plan wave:** Run `make test` (read MAKE_EXIT + grep `^--- FAIL|^FAIL\s`)
- **Before `/gsd:verify-work`:** `make test` green; `helm template` permutations green; `make test-int` if the harness chart-install changed
- **Max feedback latency:** 300 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (populated by planner) | | | DISPATCH-01..02, HALT-01 | — | image resolution honors Project pin; billing halt cannot be bypassed | unit + envtest + helm render | `make test` | ⬜ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] RED-first regression stubs inside fix tasks (tdd) — resolveImage chain unit specs; billing-classify unit specs; envtest BillingHalt dispatch-hold spec; helm render assert for the dropped flag
- [ ] Existing envtest/kind harness covers infrastructure — no new framework install

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Released-chart install dispatches pinned image | DISPATCH-02 | Full helm-install + live dispatch path | kind install with chart defaults + Project pinning real image; `kubectl get job -o yaml` shows pinned image; no `stub-*` children / `"planner stub success"` termination message |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 300s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
