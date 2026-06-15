---
phase: 18
slug: eval-harness
status: approved
nyquist_compliant: true
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
| **Estimated runtime** | ~5–15 seconds (internal/eval unit tests, zero-network) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/eval/...`
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 18-01-01 | 01 | 1 | EVAL-01/03/06 | — | N/A | build | `grep -q "sebdah/goldie/v2" go.mod && go build ./internal/eval/... && grep -q "^package eval" internal/eval/doc.go` | ❌ W0 | ⬜ pending |
| 18-01-02 | 01 | 1 | EVAL-01/03/06 | — | N/A | unit | `go test ./internal/eval/... && ls internal/eval/testdata/goldie/*.golden \| wc -l \| grep -q 5 && ls internal/eval/testdata/ratchets/*.txt \| wc -l \| grep -q 5` | ❌ W0 | ⬜ pending |
| 18-02-01 | 02 | 2 | EVAL-04 | — | N/A | unit | `go test ./internal/subagent/anthropic/ -run 'TestCostParity' && git diff --quiet internal/subagent/anthropic/pricing.go` | ❌ W0 | ⬜ pending |
| 18-02-02 | 02 | 2 | EVAL-02 | — | N/A | unit | `go test ./internal/subagent/anthropic/ -run 'TestReadChildCRDs' && git diff --quiet internal/subagent/anthropic/subagent.go` | ❌ W0 | ⬜ pending |
| 18-02-03 | 02 | 2 | EVAL-02/04 | — | N/A | unit | `go test ./internal/eval/ -run 'TestDAG\|TestDeclaredOutputPaths\|TestCostReplay\|TestParseStream'` | ❌ W0 | ⬜ pending |
| 18-03-01 | 03 | 1 | EVAL-05 | T-18-03 | tide-eval fails closed; never logs token | build | `head -1 cmd/tide-eval/main.go \| grep -q "//go:build eval" && go vet -tags eval ./cmd/tide-eval/ && go build ./...` | ❌ W0 | ⬜ pending |
| 18-03-02 | 03 | 1 | EVAL-05 | T-18-03 | N/A | unit | `grep -qE '^eval:' Makefile && grep -q "go run -tags eval ./cmd/tide-eval/" Makefile && ! grep -qE '^test-unit:' Makefile` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*
*File Exists ❌ W0 = the eval package + testdata are created during execution (Plan 18-01 Task 2); no separate Wave-0 stub step needed.*

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

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (7/7 tasks)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (no separate Wave 0 — tests created inline by 18-01-02)
- [x] No watch-mode flags
- [x] Feedback latency < 30s (all unit tests + greps)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-06-15
