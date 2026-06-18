---
phase: 28
slug: plan-import-core
status: draft
nyquist_compliant: false
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
| {N}-01-01 | 01 | 1 | IMPORT-{0X} | T-{N}-01 / — | {expected secure behavior or "N/A"} | unit/envtest | `{command}` | ✅ / ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Planner/Nyquist auditor fills this map: one row per task, every IMPORT-01..05 requirement covered, each row an automated `go test`/envtest assertion (cycle-detection-before-create, completeness-reject, FQ-name no-aliasing, trust-gate containment, schema-conversion no-op round-trip).*

---

## Wave 0 Requirements

- [ ] Envtest harness already exists (`internal/controller/suite_test.go`) — reuse for ImportController specs
- [ ] Fixture access: `examples/projects/dogfood/salvage-20260618/` (seed YAMLs + `pvc-envelopes.tgz`) available to tests
- [ ] `cmd/tide-import/` table-driven unit tests (copy + rekey + conversion round-trip) — no kind required

*Existing infrastructure covers most phase requirements; no new framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Zero planner Pods for adopted levels on a real kind run | IMPORT-01 | Requires a live cluster + PVC + Job dispatch | Belongs to Phase 29 E2E (`test/integration/kind`) against the salvage fixture; Phase 28 asserts the mechanism via envtest. |

*Phase 28's per-criterion proofs are automatable in envtest; the full live "zero planner Pods" proof is the Phase 29 kind E2E (TOOL-02).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
