---
phase: 28-plan-import-core
plan: "03"
subsystem: tide-import-binary
tags: [import, envelope, rekey, schema-conversion, firewall, tdd]
dependency_graph:
  requires: ["28-02"]
  provides: ["tide-import binary (cmd/tide-import)", "images/tide-import/Dockerfile"]
  affects: ["28-04 ImportController dispatch surface"]
tech_stack:
  added: []
  patterns:
    - "tide-reporter structural mirror (config struct, run seam, flag.NewFlagSet)"
    - "cp -n no-clobber + atomic os.Rename partial-write safety (D-12)"
    - "containedJoin filepath.Clean + HasPrefix traversal defense (mirrors backend.go:116-127)"
    - "convertSpecRaw unmarshal/marshal typed round-trip through api/v1alpha2 structs"
    - "childKindAllowlist local set {Milestone,Phase,Plan,Task} — Wave excluded (D-09)"
    - "isEnvelopeComplete: exitCode==0 AND len(ChildCRDs)==ChildCount (IMPORT-02)"
    - "importReport JSON stdout: {copied,skipped,converted,incomplete}"
key_files:
  created:
    - cmd/tide-import/main.go
    - cmd/tide-import/main_test.go
    - images/tide-import/Dockerfile
  modified: []
decisions:
  - "out.json uses no-clobber semantics too (idempotent re-run D-12): if dst exists, skip"
  - "copyDirNoClobberExcluding: copy all files except out.json separately for atomic write"
  - "ChildCount mismatch: envelope with len(ChildCRDs)!=ChildCount → Incomplete, not fatal"
  - "Missing out.json (plan-level budget halt): treated as Incomplete, skip without error"
metrics:
  duration: "22 minutes"
  completed: "2026-06-18T19:08:00Z"
  tasks_completed: 2
  files_created: 3
  files_modified: 0
---

# Phase 28 Plan 03: tide-import Binary Summary

One-liner: tide-import in-pod binary copies salvaged UID-keyed envelope trees to new-UID paths with cp-n/atomic-rename safety, converts child Spec.Raw through typed v1alpha2 structs (strips objective/wave/filesTouched), rejects incomplete envelopes and non-allowlisted Kinds, and refuses path traversal — all proven offline via TDD.

## What Was Built

### Task 1: tide-import binary — copy + rekey + atomic TaskUID rewrite + path containment

`cmd/tide-import/main.go` (458 lines): the in-pod binary mirroring `cmd/tide-reporter` structure.

**Core capabilities:**

- `importConfig` struct: `OldWorkspace`/`NewWorkspace` flags (defaults `/old-workspace`/`/new-workspace`)
- `rekeyEntry` struct: `{fqName, oldUID, newUID}` — FQ-name keys the rekey table (D-07, T-28-03-04)
- `importReport` stdout JSON: `{copied, skipped, converted, incomplete}`
- `containedJoin`: `filepath.Clean` + `HasPrefix` path-traversal defense (mirrors `backend.go:116-127`, T-28-03-01)
- `isEnvelopeComplete`: `exitCode==0 AND len(ChildCRDs)==ChildCount` completeness guard (IMPORT-02, T-28-03-03)
- `convertSpecRaw`: unmarshal/marshal through typed v1alpha2 structs (`MilestoneSpec`/`PhaseSpec`/`PlanSpec`/`TaskSpec`) — strips `objective`/`wave`/`filesTouched`; Wave excluded per D-09
- `childKindAllowlist`: local `{Milestone, Phase, Plan, Task}` set (mirrors T-308; Wave excluded D-09)
- `copyDirNoClobberExcluding`: recursive cp-n for all files except `out.json`
- `copyFileNoClobber`: write `.tmp` + `os.Rename` for partial-write safety (D-12)
- `atomicWriteJSON`: atomic write for converted `out.json`; no-clobber (if dst exists, skip)

**Stdin protocol:** JSON slice of `rekeyEntry` (fqName→oldUID→newUID). The FQ-name includes the full parent chain (e.g. `milestone-02/phase-03/plan-01-foo`) guaranteeing no short-name aliasing (D-07, T-28-03-04).

**Exit codes:** 0 (success), 1 (generic I/O failure), 2 (invariant: bad stdin JSON, path traversal, Kind not in allowlist).

**TDD commits:**
- RED: `742f678` — `test(28-03): add failing tests for tide-import binary`
- GREEN: `87e72e0` — `feat(28-03): implement tide-import binary`

### Task 2: Dockerfile

`images/tide-import/Dockerfile`: multi-stage distroless build mirroring `images/tide-reporter/Dockerfile`.

- Builder: `golang:1.26-alpine@sha256:a6a091...` (same digest as tide-reporter)
- Runtime: `distroless/static:nonroot@sha256:963fa6...` (same digest as tide-reporter)
- COPY closure: `api/`, `pkg/dispatch/`, `cmd/tide-import/` — **no** `internal/reporter`, **no** `internal/owner`
- Docker build verified: `tide-import:verify` built successfully

**Commit:** `5e5f2e2` — `feat(28-03): add tide-import Dockerfile`

## Test Coverage (all 11 cases green)

| Case | Test | Requirement |
|------|------|-------------|
| (a) happy copy | TestRunHappyCopy | old-UID tree copied, TaskUID=newUID |
| (b) no-clobber | TestNoClobber | pre-existing dst file preserved, skipped++ |
| (c) atomic rewrite | TestAtomicTaskUIDRewrite | stale TaskUID rewritten, no .tmp leftover |
| (d) path traversal oldUID | TestPathTraversal | exit 2, nothing written to new-workspace |
| (d) path traversal newUID | TestPathTraversalNewUID | exit 2 |
| (e) FQ-name no-aliasing | TestFQNameNoAliasing | two same-short-name entries → distinct dirs |
| (f) conversion no-op | TestConversionNoOp | objective/wave/filesTouched dropped, phaseRef survives |
| (g) Kind allowlist Secret | TestKindAllowlistReject/Secret | exit 2 |
| (g) Kind allowlist Wave | TestKindAllowlistReject/Wave | exit 2 (D-09) |
| (h) completeness reject exitCode | TestCompletenessRejectExitCode | exitCode=1 → incomplete, not adopted |
| (i) completeness reject mismatch | TestCompletenessRejectChildCountMismatch | childCount!=len → incomplete, not adopted |

`go test ./cmd/tide-import/... -count=1`: PASS (0.5s)
`make verify-import-firewall`: OK (no provider/internal/controller-runtime leakage)
`docker build -f images/tide-import/Dockerfile -t tide-import:verify .`: DONE

## Requirements Coverage

| Req ID | Status | Evidence |
|--------|--------|----------|
| IMPORT-02 | Covered | isEnvelopeComplete + convertSpecRaw strict-validate; tests (f)(g)(h)(i) |
| IMPORT-03 | Covered | FQ-name rekey table; containedJoin traversal defense; tests (d)(e) |
| IMPORT-05 | Covered | containedJoin path containment (T-28-03-01); test (d) proves exit 2 + zero writes |

## Deviations from Plan

### Auto-fixed Issues

None — plan executed as written with one implementation clarification:

**Design clarification: out.json uses no-clobber semantics (idempotency)**

The plan specified "copy using cp -n semantics" and described `atomicWriteJSON` as separate from `copyFileNoClobber`. During implementation, the behavior of out.json on an idempotent re-run required a decision: the test `TestNoClobber` pre-populates the destination `out.json` and expects it to NOT be overwritten. This means out.json also uses cp-n: if the destination already exists, skip. This is correct per D-12 (ImportComplete condition is the first-step idempotency guard — if the import ran before and wrote out.json, a retry skips it). The implementation uses `os.Stat(outDst)` check before `atomicWriteJSON`, consistent with D-12.

**Missing out.json (plan-level budget halt):** The salvage fixture has 39 plan-level envelopes all with `exitCode=1`. The implementation treats a missing `out.json` (ReadFile error) as incomplete (not fatal), increments `incomplete`, and continues — matching the RESEARCH note that "levels with missing/failed envelopes re-plan fresh."

## Threat Surface Scan

No new threat surface beyond what's in the plan's `<threat_model>`. All four mitigations (T-28-03-01 through T-28-03-04) are implemented and test-proven:

- T-28-03-01 (path traversal): `containedJoin`, tests (d)
- T-28-03-02 (Spec.Raw tampering): `childKindAllowlist` + `convertSpecRaw`, tests (f)(g)
- T-28-03-03 (partial/corrupt envelope): `isEnvelopeComplete`, tests (h)(i)
- T-28-03-04 (FQ-name aliasing): `rekeyEntry.FQName`, test (e)

## TDD Gate Compliance

- RED gate: commit `742f678` (`test(28-03): ...`) — 11 failing tests
- GREEN gate: commit `87e72e0` (`feat(28-03): ...`) — all 11 pass

## Self-Check: PASSED

Files exist:
- `cmd/tide-import/main.go`: FOUND
- `cmd/tide-import/main_test.go`: FOUND
- `images/tide-import/Dockerfile`: FOUND

Commits exist:
- `742f678` (test RED): FOUND
- `87e72e0` (feat GREEN): FOUND
- `5e5f2e2` (feat Dockerfile): FOUND

Test result: PASS (11/11)
Firewall: OK
Docker build: DONE
