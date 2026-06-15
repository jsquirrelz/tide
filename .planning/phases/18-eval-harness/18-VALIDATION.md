---
phase: 18
slug: eval-harness
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-15
---

# Phase 18 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (+ github.com/sebdah/goldie/v2 v2.8.0, test-only) |
| **Config file** | Makefile (`test` target → `go test ./...` excl. /e2e, /test/integration) |
| **Quick run command** | `go test ./internal/eval/...` |
| **Full suite command** | `make test` |
| **Estimated runtime** | ~{N} seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/eval/...`
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** {N} seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {N}-01-01 | 01 | 1 | EVAL-{XX} | — | N/A | unit | `{command}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/eval/` package created — stubs for EVAL-01..06
- [ ] `github.com/sebdah/goldie/v2 v2.8.0` added to go.mod (test-only)
- [ ] `testdata/goldie/` + ratchet snapshot fixtures scaffolded

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live `count_tokens` per-template counts | EVAL-05 | Needs network + creds + running credproxy; cannot run in zero-network `make test` | `make eval` (requires credproxy reachable) |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < {N}s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
