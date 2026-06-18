---
phase: 28
slug: plan-import-core
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-18
---

# Phase 28 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + Ginkgo v2.28 / Gomega (envtest Layer A); plain `go test` for unit |
| **Config file** | `Makefile` targets; `test/integration/kind` for Layer B |
| **Quick run command** | `make test` (unit + envtest Layer A) |
| **Full suite command** | `make test-int` (kind Layer B — env-gated, heavy) |
| **Estimated runtime** | Layer A ~60–120s; Layer B several minutes (kind) |

---

## Sampling Rate

- **After every task commit:** Run `make test` (Layer A — envtest covers the controllers/import path)
- **After every plan wave:** Run `make test` + relevant `go test ./internal/controller/... ./cmd/tide-import/... ./pkg/...`
- **Before `/gsd:verify-work`:** Layer A green; `make verify-import-firewall` / `make verify-dispatch-imports` green (no provider leakage); Layer B (`make test-int`) for the end-to-end import path if env permits
- **Max feedback latency:** ~120 seconds (Layer A)

> ⚠ Per CLAUDE.md: `make test-int` bundles plain go-tests (helm-template contract tests) alongside Ginkgo — read `MAKE_EXIT` and grep `^--- FAIL|^FAIL\s`, not just the Ginkgo summary. A known pre-existing kind `medium_http` fixture flake (MAKE_EXIT=2) is unrelated to import code if zero `test/integration/kind/` files were touched — confirm by `git diff`.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 28-01-01 | 01 | 1 | IMPORT-01 | T-28-01-01 | Chart declares images.tideImport before any binary reads it (FIXED contract) | helm | `helm template charts/tide --set images.tideImport.tag=test \| grep tide-import:test` | ✅ | ⬜ pending |
| 28-01-02 | 01 | 1 | IMPORT-01 | T-28-01-01 | Manager Deployment injects TIDE_IMPORT_IMAGE from chart value w/ AppVersion fallback | helm | `helm template charts/tide --set images.tideImport.tag=test \| grep TIDE_IMPORT_IMAGE` | ✅ | ⬜ pending |
| 28-02-01 | 02 | 1 | IMPORT-01, IMPORT-03 | T-28-02-01/02 | ImportSource field + ImportComplete condition vocabulary exported; MinLength on subfields | source | `go build ./api/... && grep ImportSourceRef api/v1alpha2/import_types.go` | ❌ W0 | ⬜ pending |
| 28-02-02 | 02 | 1 | IMPORT-01 | T-28-02-02 | CRD schema round-trips importSource (minLength validation enforced server-side) | source | `make generate manifests && grep importSource config/crd/bases/tideproject.k8s_projects.yaml` | ❌ W0 | ⬜ pending |
| 28-03-01 | 03 | 2 | IMPORT-03, IMPORT-05 | T-28-03-01/04 | FQ-name rekey, no cross-object aliasing; path traversal → exit 2, writes nothing; atomic rename | unit | `go test ./cmd/tide-import/... -run 'TestRun\|TestRekey\|TestCopy' -count=1` | ❌ W0 | ⬜ pending |
| 28-03-02 | 03 | 2 | IMPORT-02, IMPORT-05 | T-28-03-02/03 | convertSpecRaw strips unknown fields; non-allowlisted Kind/unconvertible spec fail closed (exit 2); incomplete envelope (exitCode!=0 or ChildCount mismatch) rejected not adopted | unit | `go test ./cmd/tide-import/... -count=1 && make verify-import-firewall` | ❌ W0 | ⬜ pending |
| 28-04-01 | 04 | 2 | IMPORT-03, IMPORT-04, IMPORT-05 | T-28-04-01/02/03/04 | dag.ComputeWaves before any client.Create; cyclic/unresolved → ReasonCyclicPlanDetected, zero CRs; PVC mounted at 2 same-ns subPaths only; Wave never created; AlreadyExists=success idempotent | source/firewall | `go build ./internal/controller/... && grep ComputeWaves internal/controller/import_controller.go && make verify-dispatch-imports` | ❌ W0 | ⬜ pending |
| 28-04-02 | 04 | 2 | IMPORT-01, IMPORT-04, IMPORT-05 | T-28-04-01/02/04 | envtest: adoption (new-UID CRs + rekey CM + ImportComplete=True); cycle reject with no partial CRs (Consistently); Kind rejection; idempotent re-run | envtest | `go test ./internal/controller/... -run Import -count=1` | ❌ W0 | ⬜ pending |
| 28-05-01 | 05 | 3 | IMPORT-01 | T-28-05-01/02/03 | All 5 sites park before pool acquire on import-pending (no slot leak); budget rollup suppressed for imported envelope | source | `go build ./internal/controller/... && for f in project milestone phase plan task; do grep -q ConditionImportComplete internal/controller/$f_controller.go; done` | ❌ W0 | ⬜ pending |
| 28-05-02 | 05 | 3 | IMPORT-01 | T-28-05-01/04 | envtest: park-on-pending acquires no slot; clear-on-complete proceeds; non-import Projects unaffected; ImportController registered w/ TIDE_IMPORT_IMAGE | envtest | `go build ./cmd/... && go test ./internal/controller/... -run Guard -count=1` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Planner/Nyquist auditor fills this map: one row per task, every IMPORT-01..05 requirement covered, each row an automated `go test`/envtest assertion (cycle-detection-before-create, completeness-reject, FQ-name no-aliasing, trust-gate containment, schema-conversion no-op round-trip).*

---

## Wave 0 Requirements

- [x] Envtest harness already exists (`internal/controller/suite_test.go`) — reuse for ImportController specs
- [x] Fixture access: `examples/projects/dogfood/salvage-20260618/` (seed YAMLs + `pvc-envelopes.tgz`) available to tests
- [x] `cmd/tide-import/` table-driven unit tests (copy + rekey + conversion round-trip) — no kind required

*Existing infrastructure covers most phase requirements; no new framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Zero planner Pods for adopted levels on a real kind run | IMPORT-01 | Requires a live cluster + PVC + Job dispatch | Belongs to Phase 29 E2E (`test/integration/kind`) against the salvage fixture; Phase 28 asserts the mechanism via envtest. |

*Phase 28's per-criterion proofs are automatable in envtest; the full live "zero planner Pods" proof is the Phase 29 kind E2E (TOOL-02).*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner-filled 2026-06-18 (every IMPORT-01..05 has an automated envtest/go-test/helm proof)
