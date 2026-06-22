---
phase: "29-operator-tooling-e2e"
plan: "04"
subsystem: "test-data + build-wiring"
tags: ["childCount", "salvage-patch", "small-fixture", "seed-manifest", "makefile", "bin/tide"]
dependency_graph:
  requires:
    - "pkg/bundle.BundleManifest/BundleEntry schema (29-01)"
  provides:
    - "examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/ — childCount-stamped (D-16b)"
    - "scripts/check-salvage-childcount.sh — reusable invariant assertion"
    - "test/integration/kind/testdata/import-small-fixture/ — drain-to-Succeeded stub fixture (D-11a)"
    - "Makefile test-int-kind-prep — bin/tide build step (D-10)"
  affects:
    - "cmd/tide-import isEnvelopeComplete now satisfied for all 18 salvage complete envelopes"
    - "kind E2E (29-05) can exec the real tide binary via TIDE_BINARY or PATH"
tech_stack:
  added: []
  patterns:
    - "Python jq-equivalent patch: json.load → mutate → json.dump for in-repo fixture repair"
    - "gzip deterministic tarball (mtime=0) for reproducible pvc-envelopes.tgz"
    - "sha256 per-envelope in seed-manifest.json (D-04 integrity gate for dry-run)"
key_files:
  created:
    - scripts/check-salvage-childcount.sh
    - test/integration/kind/testdata/import-small-fixture/projects.yaml
    - test/integration/kind/testdata/import-small-fixture/milestones.yaml
    - test/integration/kind/testdata/import-small-fixture/phases.yaml
    - test/integration/kind/testdata/import-small-fixture/plans.yaml
    - test/integration/kind/testdata/import-small-fixture/seed-manifest.json
    - test/integration/kind/testdata/import-small-fixture/pvc-envelopes.tgz
    - test/integration/kind/testdata/import-small-fixture/project.yaml
  modified:
    - examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/ (18 out.json + repacked .tgz)
    - Makefile (test-int-kind-prep: prepend go build -o bin/tide ./cmd/tide)
decisions:
  - "Python patch script (not Go) for the one-time in-repo repair: no production-code change needed"
  - "All four planner levels included in pvc-envelopes.tgz (milestone + phase + plan-01 + plan-02) so import can adopt all levels"
  - "Seed manifest covers milestones/phases/plans (D-15 — down to Plan only; Tasks materialize from plan envelope children)"
  - "project.yaml seedManifestConfigMap name: 'tide-import-seed-import-small-test' (deterministic convention for 29-05 to reference)"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-22T05:57:00Z"
  tasks_completed: 2
  files_created: 8
  files_modified: 2
---

# Phase 29 Plan 04: Test Data + Build Wiring Summary

One-time childCount patch on 18 complete salvage envelopes + check script + small drain fixture (1 Project/1 Milestone/1 Phase/2 Plans) + Makefile bin/tide build wired into test-int-kind-prep.

## Tasks Completed

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1 | childCount patch of 18 complete salvage out.json (D-16b) | b75c73e | 18 out.json + pvc-envelopes.tgz + scripts/check-salvage-childcount.sh |
| 2 | Small drain fixture + Makefile bin/tide build | 2638fd5 | 7 fixture files + Makefile |

## What Was Built

### Task 1: D-16b salvage childCount patch

All 18 successful planner envelopes (exitCode==0, non-empty childCRDs) in
`examples/projects/dogfood/salvage-20260618/pvc-envelopes/envelopes/` now carry
`childCount == len(childCRDs)`. The patch was applied via a Python one-shot script
that only touches exitCode==0 envelopes with non-empty childCRDs.

**Count breakdown:**
- Patched: 18 envelopes (project: 1, milestone: 3, phase: 14)
- Untouched (failed, exitCode!=0): 39 envelopes — they must re-plan (D-17)
- Untouched (executor-level, 0 children): 0 envelopes in this fixture

The `pvc-envelopes.tgz` was repacked from the patched unpacked tree so both
forms agree (T-29-04-03 mitigated).

**scripts/check-salvage-childcount.sh:** exits 0 only when every complete
salvage envelope carries `childCount == len(childCRDs)`. Failed envelopes are
skipped. Can be run as part of CI or pre-merge checks.

### Task 2: Small drain-to-Succeeded fixture (D-11a)

**test/integration/kind/testdata/import-small-fixture/**

Minimal bundle for the E2E's drain-to-all-Milestones-Succeeded tier:

| Level | Name | Old UID |
|-------|------|---------|
| Project | import-small-test | aaaaaaaa-0001-0001-0001-aaaaaaaaaaaa |
| Milestone | milestone-01-stub | bbbbbbbb-0001-0001-0001-bbbbbbbbbbbb |
| Phase | phase-01-stub | cccccccc-0001-0001-0001-cccccccccccc |
| Plan | plan-01-stub | dddddddd-0001-0001-0001-dddddddddddd |
| Plan | plan-02-stub | eeeeeeee-0001-0001-0001-eeeeeeeeeeee |

**seed-manifest.json:** BundleManifest with milestones (1) / phases (1) / plans (2)
arrays. Each entry carries `name`, `fqName` (full parent chain), `oldUID`, `status`,
parent ref, and `sha256` (per-envelope integrity per D-04).

**pvc-envelopes.tgz:** Contains complete stub envelopes for all four planner UIDs:
- Milestone planner → authors 1 Phase child
- Phase planner → authors 2 Plan children
- Plan-01 planner → authors 1 Task child
- Plan-02 planner → authors 1 Task child

All envelopes carry `childCount == len(childCRDs)` (correct from authoring — no
repair needed for stub-authored envelopes, only legacy salvage required repair).

**project.yaml:** Applies after `tide import-envelopes` stages the seed ConfigMap.
References `importSource.seedManifestConfigMap = "tide-import-seed-import-small-test"`
and `importSource.salvagedPVCSubPath = "aaaaaaaa-0001-0001-0001-aaaaaaaaaaaa/workspace"`.
Budget: absoluteCapCents=0 (stub costs nothing).

### Task 2: Makefile bin/tide build wiring (D-10)

`test-int-kind-prep` now prepends:
```makefile
go build -o bin/tide ./cmd/tide
```

The kind E2E (29-05) resolves the binary via `os.Getenv("TIDE_BINARY")` falling
back to `exec.LookPath("tide")`. Build confirmed: `go build -o /tmp/tide ./cmd/tide`
exits 0; binary is 75 MB (full Go binary with all CLI subcommands).

## Verification Results

```
bash scripts/check-salvage-childcount.sh
  → check-salvage-childcount: all complete envelopes carry correct childCount

grep checks on seed-manifest.json
  → "milestones" found, "phases" found, "plans" found

Makefile grep
  → FOUND: bin/tide ./cmd/tide in test-int-kind-prep

go build -o /tmp/tide ./cmd/tide
  → BUILD OK (exit 0)

go build ./...
  → (no output — clean build, zero production-code changes)
```

## Deviations from Plan

None — plan executed exactly as written.

D-16b patch: Python script used (vs Go or jq), which is fine since this is a
one-shot in-repo data repair, not production code. The script produces valid JSON
with the exact same structure as the input (separators=(',', ':') compact form).

## Known Stubs

None — no data stubs. All childCount values are correct. All envelope sha256 values
in seed-manifest.json match the actual out.json bytes in pvc-envelopes.tgz.

## Threat Surface Scan

| Flag | File | Description |
|------|------|-------------|
| None | — | No new network endpoints, auth paths, or schema changes introduced |

T-29-04-01 (patch scope): Verified — only exitCode==0 + non-empty childCRDs
envelopes were patched; check script confirms invariant holds.

T-29-04-02 (fixture authenticity): Accepted — in-repo test data, git-tracked.

T-29-04-03 (.tgz vs unpacked drift): Mitigated — pvc-envelopes.tgz was repacked
from the patched unpacked tree; verified with `tar xOf` spot-check.

## Self-Check: PASSED

- scripts/check-salvage-childcount.sh: FOUND, exits 0
- test/integration/kind/testdata/import-small-fixture/seed-manifest.json: FOUND
- test/integration/kind/testdata/import-small-fixture/pvc-envelopes.tgz: FOUND
- test/integration/kind/testdata/import-small-fixture/project.yaml: FOUND
- Makefile has bin/tide build in test-int-kind-prep: FOUND
- Commit b75c73e: FOUND (Task 1)
- Commit 2638fd5: FOUND (Task 2)
