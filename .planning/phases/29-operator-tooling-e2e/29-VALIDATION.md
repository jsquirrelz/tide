---
phase: 29
slug: operator-tooling-e2e
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-22
---

# Phase 29 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` (table-driven units; Ginkgo/Gomega for kind integration) |
| **Config file** | none — existing `Makefile` targets |
| **Quick run command** | `go test ./cmd/tide/...` |
| **Full suite command** | `make test` (unit/envtest); `make test-int` (kind integration, gated) |
| **Estimated runtime** | ~30s unit; kind E2E minutes (long-test gated) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./cmd/tide/...` (and the touched package)
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** `make test` green; kind E2E (`make test-int` or tagged run) green
- **Max feedback latency:** ~30 seconds (unit); kind paths gated behind `testing.Short()` so they don't gate the inner loop

---

## Per-Task Verification Map

> Plans/tasks are authored by gsd-planner; this map seeds the requirement→test linkage the
> planner must honor. Each CLI verb gets offline table tests via the `xxxRun` func-var seam
> (mirroring `artifactGetRun`); the E2E proof is the kind integration test.

| Capability | Requirement | Test Type | Automated Command | Notes |
|------------|-------------|-----------|-------------------|-------|
| `export-envelopes` bundle round-trip (tgz + `--dir`) | TOOL-01 | unit | `go test ./cmd/tide/ -run Export` | func-var seam stubs the inspector pod; assert bundle shape mirrors fixture (D-02) |
| Export stamps `childCount` forward when absent (D-16a) | TOOL-01 | unit | `go test ./cmd/tide/ -run Export.*ChildCount` | input out.json w/o childCount → bundled out.json has `childCount==len(childCRDs)` |
| Export emits seed manifest (CR specs + name→oldUID + sha256) (D-03/D-04) | TOOL-01 | unit | `go test ./cmd/tide/ -run Export.*Seed` | seed schema matches `seedEntry/seedManifest` (import_controller.go) |
| `import-envelopes` stage-only (loader pod + ConfigMap + surface project.yaml) (D-05/D-06) | TOOL-01 | unit | `go test ./cmd/tide/ -run Import` | func-var seam stubs loader pod; assert no Project apply |
| `--dry-run` offline validation + per-level table + `--output json` (D-07/D-08) | TOOL-01 | unit | `go test ./cmd/tide/ -run DryRun` | reuses ValidateAPIVersionKind + completeness + sha256 + ComputeWaves; no cluster |
| `--dry-run` cycle hard-reject shows edges (D-09) | TOOL-01 | unit | `go test ./cmd/tide/ -run DryRun.*Cycle` | crafted cyclic bundle → whole-import would-fail + edge list |
| Salvage fixture one-time childCount patch (D-16b) | TOOL-02 | unit/data | `go test ./test/integration/kind/ -run ...Salvage` (gated) | patched fixture out.json import-valid |
| E2E small fixture → all-Milestones-Succeeded via real CLI (D-10/D-11a) | TOOL-02 | integration (kind) | `make test-int` (long-test gated) | `exec.Command` the built `tide` binary for export+import round-trip |
| E2E salvage adoption: 0 planner Jobs `{milestone,phase}` + $0 re-paid (D-11b/D-17) | TOOL-02 | integration (kind) | `make test-int` (long-test gated) | JobList `role=planner,level in {milestone,phase}`→0; budget sampled before plan dispatch |

*Status: ✅ plans authored (29-01..29-05); every task carries an automated `<verify>`*

---

## Wave 0 Requirements

- [x] Small purpose-built E2E fixture (down-to-Plan seed + minimal complete envelope tree) that
      drains to all-Milestones-`Succeeded` with stub subagents (D-11a) — new `testdata/` tree. **Delivered by 29-04 Task 2.**
- [x] One-time childCount patch applied to committed `salvage-20260618` `out.json` files (D-16b). **Delivered by 29-04 Task 1.**
- [x] Test helper to `exec.Command` the built `tide` binary for the CLI round-trip (D-10) —
      `make test-int-kind-prep` builds `bin/tide`; 29-05 resolves it via `TIDE_BINARY`/`LookPath`. **Delivered by 29-04 Task 2 (build) + 29-05 (resolution).**

*Existing kind harness (`suite_test.go`, `cluster.yaml`, stub-subagent image load) otherwise covers infrastructure.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Bundle transports across a real cluster teardown/rebuild | TOOL-01 | True cross-cluster portability is environmental; kind E2E approximates with a fresh namespace/cluster | Export from cluster A, `kind delete`, import bundle into fresh cluster B, `tide apply`, confirm adoption |

*All other phase behaviors have automated verification.*

---

## Validation Sign-Off

- [x] All tasks have automated verify or Wave 0 dependencies — every `<task>` across 29-01..29-05 carries an `<automated>` verify
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers small fixture (29-04 T2) + fixture patch (29-04 T1) + CLI-exec helper (29-04 T2 build / 29-05 resolution)
- [x] No watch-mode flags
- [x] Feedback latency < 30s (unit loop)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved
