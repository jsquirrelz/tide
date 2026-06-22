---
phase: "29-operator-tooling-e2e"
plan: "01"
subsystem: "pkg/bundle"
tags: ["bundle", "tgz", "dry-run", "sha256", "childCount", "zip-slip", "dag"]
dependency_graph:
  requires: []
  provides:
    - "pkg/bundle.BundleEntry / BundleManifest (seed schema superset)"
    - "pkg/bundle.WriteBundle / ReadBundle / ExtractBundle / OpenBundleDir (tgz codec)"
    - "pkg/bundle.ValidateBundle (offline dry-run validator)"
    - "pkg/bundle.computeEnvelopeSHA256 (integrity)"
    - "pkg/bundle.stampChildCount (D-16a legacy repair)"
    - "pkg/bundle.WritePVCEnvelopesTgz (pvc-envelopes helper)"
    - "pkg/bundle.MilestoneFQName / PhaseFQName / PlanFQName"
  affects: []
tech_stack:
  added: []
  patterns:
    - "TDD (RED test → GREEN impl) across all 3 tasks"
    - "crypto/sha256 + hex for per-envelope integrity (D-04)"
    - "archive/tar + compress/gzip (stdlib, no new deps)"
    - "filepath.Clean + HasPrefix(..) + dest-dir prefix guard (zip-slip T-29-01-01)"
    - "dag.ComputeWaves reuse for offline cycle detection (D-09)"
    - "dispatch.ValidateAPIVersionKind reuse for schema check (D-07)"
key_files:
  created:
    - pkg/bundle/seed.go
    - pkg/bundle/seed_test.go
    - pkg/bundle/bundle.go
    - pkg/bundle/bundle_test.go
    - pkg/bundle/dryrun.go
    - pkg/bundle/dryrun_test.go
  modified: []
decisions:
  - "BundleEntry re-declares seedEntry in pkg/bundle to avoid importing internal/controller from cmd/tide (D-07 offline constraint)"
  - "sha256 gate skipped (not failed) when BundleEntry.SHA256 is empty — tolerates hand-written test fixtures"
  - "WritePVCEnvelopesTgz exported (not unexported) so 29-02 export layer can produce the nested tgz"
  - "stampChildCount uses io.Discard in ValidateBundle (warnings suppressed at validate layer; CLI shows them during export)"
metrics:
  duration: "~10 minutes"
  completed: "2026-06-22T05:33:45Z"
  tasks_completed: 3
  files_created: 6
---

# Phase 29 Plan 01: pkg/bundle — Bundle Format + Offline Dry-Run Summary

Bundle codec + sha256 integrity + childCount-stamp repair + offline dry-run validator using stdlib-only deps with no cluster or controller-runtime imports.

## Tasks Completed

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1 | Seed schema + sha256 + childCount-stamp | 3f0b833 | seed.go, seed_test.go |
| 2 | Zip-slip-safe tgz writer/reader | a171719 | bundle.go, bundle_test.go |
| 3 | Offline dry-run validator | 9b937f5 | dryrun.go, dryrun_test.go |

## What Was Built

### pkg/bundle/seed.go
- `BundleEntry`: seedEntry superset — same 8 json-tagged fields byte-identical to `internal/controller.seedEntry` + `sha256 omitempty`. ImportController's `json.Unmarshal` accepts the same bytes and silently drops `sha256` (unknown field).
- `BundleManifest{Milestones,Phases,Plans []BundleEntry}` mirrors `seedManifest`.
- `computeEnvelopeSHA256([]byte) string`: lowercase hex sha256 via `crypto/sha256` stdlib (D-04).
- `stampChildCount([]byte, io.Writer) ([]byte, error)`: repairs legacy salvage envelopes where `ChildCount==0 AND len(ChildCRDs)>0` — sets `ChildCount=len(ChildCRDs)` and warns (D-16a). Returns bytes unchanged when already correct or children are absent.
- `MilestoneFQName / PhaseFQName / PlanFQName`: three-component FQ name builders.

### pkg/bundle/bundle.go
- `BundleFile*` constants: the seven canonical bundle entry names matching salvage-20260618 fixture shape.
- `WriteBundle(tgzPath, files)`: writes seven entries in deterministic order.
- `ReadBundle(tgzPath)`: returns file map.
- `ExtractBundle(tgzPath)`: extracts to temp dir + cleanup func. Zip-slip defense (T-29-01-01): `filepath.Clean` + `HasPrefix("..") || IsAbs` + dest-dir prefix confirm before any `os.Create`.
- `OpenBundleDir(bundlePath)`: accepts `.tgz` (extract) or directory (return as-is). Pitfall 6 / D-02.

### pkg/bundle/dryrun.go
- `ValidateBundle(bundleDir) (*ValidationResult, error)`: per-entry adopt/re-plan classification.
  - `stampChildCount` applied first (D-16a).
  - sha256 check (D-04); skipped when `BundleEntry.SHA256 == ""` to tolerate hand-written fixtures.
  - `dispatch.ValidateAPIVersionKind` schema check (D-07 — client-free).
  - `len(ChildCRDs)==ChildCount` completeness check (post-stamp).
  - `dag.ComputeWaves` cycle detection (D-09): cycle → `CycleRejected=true` with `*dag.CycleError` — hard-rejects entire import, no partial adoption.
- `ValidationRow{Level,Name,FQName,Verdict,Reason}` + `ValidationResult` (CLI renders in 29-03).
- `WritePVCEnvelopesTgz(files)`: writes arbitrary-named in-memory tgz for export + tests.
- `loadPVCEnvelopes` / `readPVCEnvelopesTgz` / `parseEnvelopePath`: internal helpers to locate `envelopes/<uid>/out.json` entries.

## Verification Results

```
go test ./pkg/bundle/ -count=1     → PASS (16 tests across 3 files)
go vet ./pkg/bundle/               → clean
go build ./...                     → clean
git diff --stat go.mod go.sum      → (no output — zero new deps)
```

## Deviations from Plan

### Auto-fixed Issues

None — plan executed exactly as written.

### Structural Adjustment: WritePVCEnvelopesTgz exported

The plan called for an internal `writePVCEnvelopesTgz` helper. It was exported as `WritePVCEnvelopesTgz` so that the 29-02 export layer can call it from a different package (`cmd/tide/`) without accessing unexported symbols. This is an additive change with no behavioral difference.

## Known Stubs

None — all functions are fully implemented.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes beyond what the plan's threat model already covers (T-29-01-01 through T-29-01-05). All four mitigated threats have corresponding tests:
- T-29-01-01 (zip-slip): `TestZipSlip/dot-dot_entry_rejected` + `TestZipSlip/absolute_path_entry_rejected`
- T-29-01-02 (sha256 tamper): `TestDryRun/checksum_mismatch`
- T-29-01-03 (cycle graph): `TestDryRunCycle`

## Self-Check: PASSED

- pkg/bundle/seed.go: FOUND
- pkg/bundle/bundle.go: FOUND
- pkg/bundle/dryrun.go: FOUND
- Commit 3f0b833: FOUND
- Commit a171719: FOUND
- Commit 9b937f5: FOUND
