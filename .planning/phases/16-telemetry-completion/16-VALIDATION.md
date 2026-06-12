---
phase: 16
slug: telemetry-completion
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-12
---

# Phase 16 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (envtest/Ginkgo for controllers) + Vitest (dashboard/web) + helm render gates (hack/helm) |
| **Config file** | Makefile (Go tiers) / dashboard/web/vitest config |
| **Quick run command** | `go test ./internal/metrics/... ./cmd/dashboard/...` and `npm test --prefix dashboard/web` |
| **Full suite command** | `make test` + `npm test --prefix dashboard/web` + `make helm-assert` (new target this phase) |
| **Estimated runtime** | ~120 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched surface (Go or Vitest)
- **After every plan wave:** Run `make test` + dashboard Vitest suite
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (filled by planner) | — | — | TELEM-01..06 | — | — | — | — | ⬜ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Existing infrastructure covers all phase requirements (go test, Vitest, helm render gates already in place; new `helm-assert` target is itself a deliverable).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Helm value → live endpoint change | TELEM-01 | End-to-end helm install observation | `helm upgrade --set prometheus.endpoint=... ; kubectl exec` curl the proxy and observe upstream change |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
