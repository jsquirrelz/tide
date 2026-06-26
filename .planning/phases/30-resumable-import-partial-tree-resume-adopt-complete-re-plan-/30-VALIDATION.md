---
phase: 30
slug: resumable-import-partial-tree-resume-adopt-complete-re-plan
status: verified
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-25
validated: 2026-06-26
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
| 30-01-01 | 30-01 | 1 | RESUME-PARTIAL-01 | unit | `go test ./pkg/dispatch/...` → `TestIsEnvelopeComplete` (5 behavior cases, envelope_test.go:849) | ✅ green |
| 30-01-02 | 30-01 | 1 | RESUME-PARTIAL-01 | unit | `go test ./cmd/tide/...` → `TestExportEnvelopesSeedManifest` (:426), `TestExportEnvelopesChildCountRepair` (:331), `TestSeedStatusFor_LegacyUnstampedEnvelopeIsRePlannableOnExport` (:755, WR-03 pin) | ✅ green |
| 30-01-03 | 30-01 | 1 | RESUME-PARTIAL-01/04 | envtest | `go test ./internal/controller/` → `Test 5 (RESUME-PARTIAL-04): partial-tree materialization` (import_controller_test.go:561 — complete→Running+Validated, incomplete→empty+fresh, both CRs exist w/ DependsOn) | ✅ green |
| 30-02-01 | 30-02 | 1 | RESUME-PARTIAL-02 | compile | `go build ./...` + `go vet` | ✅ green |
| 30-02-02 | 30-02 | 1 | RESUME-PARTIAL-02 | envtest | `go test ./internal/controller/` → `post-ImportComplete adoption guard` (project_controller_test.go:1172, Label phase30 — no-Job + WR-01 foreign-UID pin) | ✅ green |
| 30-03-01 | 30-03 | 2 | RESUME-PARTIAL-03 | fixture | `testdata/import-partial-fixture/` (project/milestones/phases/plans.yaml + seed-manifest.json + pvc-envelopes.tgz — 6 files present) | ✅ green |
| 30-03-02 | 30-03 | 2 | RESUME-PARTIAL-03 | kind E2E | `make test-int` → `Tier c: partial fixture drains to Project=Complete` (import_resume_test.go:436) | ✅ green |
| 30-03-03 | 30-03 | 2 | RESUME-PARTIAL-03 | checkpoint:human-verify | Human-verified: real kind run `MAKE_EXIT=0`, Tier c ran (50s, not Skipped), all 3 tiers green | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Verified 2026-06-26:** all packages green — `go build` 0, `make test` (unit+envtest) 0, `internal/controller` envtest 155/155, kind 3-tier `MAKE_EXIT=0` (post `deleteNamespaceAndWait` cross-tier-contention fix). Code-review gap-fixes (WR-01 owner-ref, WR-03 pre-stamp completeness) carry their own pinning tests, both green.

**Latency tolerance (30-03-02):** the Tier-c kind E2E exceeds the 30s feedback threshold (8-minute
timeout) — accepted because no faster behavioral substitute exists for an end-to-end kind import, and
it is guarded by `testing.Short()` + `skipIfCRDsOnlyMode()` so cluster-less CI passes skip it cleanly.
The real-cluster green run is gated by the blocking human checkpoint 30-03-03 (requires `MAKE_EXIT=0`
and proof Tier c *ran*, not Skipped). `set -o pipefail` was added so a failing `go test` is not masked
by the `grep` exit code.

---

## Wave 0 Requirements

- [x] New partial-tree fixture (mixed complete/incomplete envelopes) — `test/integration/kind/testdata/import-partial-fixture/` (commit `23e7ead`)
- [x] New Tier-c kind test driving a partial import to `Project=Complete` — `test/integration/kind/import_resume_test.go` (commit `85da26a`)

*Existing controller envtest infrastructure covers the materialization-branch and project-guard assertions.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live partial-salvage resume on `kind-tide-dogfood` | TBD | Real cluster + LLM dispatch ($-metered); the carried-in `salvage-20260618` bundle | Apply run-2 project, observe adopt-complete + re-plan-incomplete drives Project to Complete |

*Automated Tier-c covers the partial-tree-to-completion outcome with stub/fake envelopes; the live run is the dogfood re-attempt (separate, post-phase).*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s (quick layers; kind E2E latency-tolerated per note above)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** verified 2026-06-26

---

## Validation Audit 2026-06-26

| Metric | Count |
|--------|-------|
| Requirements | 4 (RESUME-PARTIAL-01..04) |
| Tasks mapped | 8 |
| Gaps found | 0 |
| Resolved | 0 (all COVERED at execution time) |
| Escalated | 0 |

State A audit (post-execution). Every requirement maps to a named automated test that ran green; no MISSING/PARTIAL gaps, so no `gsd-nyquist-auditor` spawn was needed. The one genuinely manual item (live partial-salvage resume on a real `$`-metered cluster) remains in the Manual-Only table — it is the dogfood run #2 re-attempt, deliberately out of automated scope. `nyquist_compliant: true`.
