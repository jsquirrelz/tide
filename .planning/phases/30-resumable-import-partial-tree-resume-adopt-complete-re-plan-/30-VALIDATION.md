---
phase: 30
slug: resumable-import-partial-tree-resume-adopt-complete-re-plan
status: approved
nyquist_compliant: true
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

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | Status |
|---------|------|------|-------------|-----------|-------------------|--------|
| 30-01-01 | 30-01 | 1 | RESUME-PARTIAL-01 | unit | `go test ./pkg/dispatch/... ./cmd/tide-import/...` | ⬜ pending |
| 30-01-02 | 30-01 | 1 | RESUME-PARTIAL-01 | unit | `go test ./cmd/tide/... -run "Export\|SeedManifest\|BuildSeed"` | ⬜ pending |
| 30-01-03 | 30-01 | 1 | RESUME-PARTIAL-01/04 | envtest | `go test ./internal/controller/ -run "Import"` | ⬜ pending |
| 30-02-01 | 30-02 | 1 | RESUME-PARTIAL-02 | compile | `go build ./... && go vet ./internal/controller/` | ⬜ pending |
| 30-02-02 | 30-02 | 1 | RESUME-PARTIAL-02 | envtest | `go test ./internal/controller/ -run "Project"` | ⬜ pending |
| 30-03-01 | 30-03 | 2 | RESUME-PARTIAL-03 | fixture | shell file-existence + grep assertion | ⬜ pending |
| 30-03-02 | 30-03 | 2 | RESUME-PARTIAL-03 | kind E2E | `set -o pipefail; go test ./test/integration/kind/ -run "TestKind"` (see latency note) | ⬜ pending |
| 30-03-03 | 30-03 | 2 | RESUME-PARTIAL-03 | checkpoint:human-verify | N/A | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Latency tolerance (30-03-02):** the Tier-c kind E2E exceeds the 30s feedback threshold (8-minute
timeout) — accepted because no faster behavioral substitute exists for an end-to-end kind import, and
it is guarded by `testing.Short()` + `skipIfCRDsOnlyMode()` so cluster-less CI passes skip it cleanly.
The real-cluster green run is gated by the blocking human checkpoint 30-03-03 (requires `MAKE_EXIT=0`
and proof Tier c *ran*, not Skipped). `set -o pipefail` was added so a failing `go test` is not masked
by the `grep` exit code.

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
