---
phase: 13
slug: dispatch-image-resolution-provider-halt
status: ready
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-11
updated: 2026-06-11
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
| 13-01/T1 | 13-01 | 1 | DISPATCH-01 | image-injection via Project spec | resolveImage walks Levels.Image → Spec.Subagent.Image → helm default | unit (RED-first) | `go test ./internal/controller/ -run 'TestResolveImage' -count=1 -timeout 120s` | ✅ in-task | ⬜ pending |
| 13-01/T2 | 13-01 | 1 | DISPATCH-01 | — | all six dispatch sites consume resolveImage; main.go shim keeps Layer B green | suite + build | `go test ./internal/controller/... ./internal/dispatch/... -count=1 -timeout 600s && go build ./...` | ✅ existing suites | ⬜ pending |
| 13-02/T1 | 13-02 | 1 | HALT-01 | halt-bypass | BillingHalt condition vocab + helpers (set/check/classify) | unit (RED-first) | `go test ./internal/controller/ -run 'TestIsBillingFailureReason\|TestCheckBillingHalt\|TestSetBillingHalt' -count=1 -timeout 120s` | ✅ in-task | ⬜ pending |
| 13-02/T2 | 13-02 | 1 | HALT-01 | credproxy tampering | fail-fast latch: zero upstream calls after first 400; no controller-runtime import | unit (RED-first) | `go test ./internal/credproxy/... -count=1 -timeout 120s` | ✅ in-task | ⬜ pending |
| 13-02/T3 | 13-02 | 1 | HALT-01 | — | tide resume clears BillingHalt unconditionally; no auto-probe | unit (RED-first) | `go test ./cmd/tide/... -count=1 -timeout 120s` | ✅ in-task | ⬜ pending |
| 13-03/T1 | 13-03 | 2 | DISPATCH-02 | — | chart drops --subagent-image; CLAUDE_SUBAGENT_IMAGE from subagent.defaults.image | helm render gate | `go test ./test/integration/kind/ -run 'TestHelmDeploymentTemplate' -count=1 -timeout 120s && helm template tide charts/tide \| grep -c 'subagent-image=' \| grep -qx 0` | ✅ in-task | ⬜ pending |
| 13-03/T2 | 13-03 | 2 | DISPATCH-02 | — | kind + acceptance opt into stub explicitly; full suite green after chart change | make test-int | `make test-int` (MAKE_EXIT + FAIL-line grep discipline) | ✅ existing harness | ⬜ pending |
| 13-04/T1 | 13-04 | 2 | HALT-01 | halt-bypass | third dispatch-entry hold at all five levels; park-not-fail, 30s requeue | suite (RED-first) | `go test ./internal/controller/... -count=1 -timeout 600s` | ✅ in-task | ⬜ pending |
| 13-04/T2 | 13-04 | 2 | HALT-01 | — | envelope backstop at five sites; run-1 regression: halt → zero sibling Jobs → resume → dispatch | envtest Layer A | `go test ./internal/controller/... -count=1 -timeout 600s && make test-int-fast` (exit discipline) | ✅ in-task | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] RED-first regression stubs inside fix tasks (tdd) — resolveImage chain unit specs; billing-classify unit specs; envtest BillingHalt dispatch-hold spec; helm render assert for the dropped flag
- [x] Existing envtest/kind harness covers infrastructure — no new framework install

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Released-chart install dispatches pinned image | DISPATCH-02 | Full helm-install + live dispatch path | kind install with chart defaults + Project pinning real image; `kubectl get job -o yaml` shows pinned image; no `stub-*` children / `"planner stub success"` termination message |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 300s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-11 (orchestrator, post-checker fixes)
