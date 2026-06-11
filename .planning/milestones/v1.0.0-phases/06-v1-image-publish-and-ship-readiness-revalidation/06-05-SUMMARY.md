---
phase: "06"
plan: "05"
subsystem: documentation
tags: [docs, install, image-publish, ghcr, maintainer, DOC-01]
dependency_graph:
  requires: [06-01, 06-03, 06-04]
  provides: [DOC-01]
  affects:
    - docs/INSTALL.md
tech_stack:
  added: []
  patterns:
    - docker-manifest-inspect-probe
    - ghcr-visibility-workflow
    - acceptance-v1-smoke-local-path
key_files:
  created: []
  modified:
    - docs/INSTALL.md
decisions:
  - "DOC-01: Maintainer image-publish section placed before Next steps — operators skip it, maintainers find it at document end without breaking operator reading flow"
  - "GHCR Pitfall 3 documented as an action item (navigate to package settings, set Public) rather than a script — no automated fix exists for first-push visibility"
  - "load-images-if-needed.sh mechanism described at the right level of detail (probe → build → kind load) without duplicating the script source"
metrics:
  duration: "~5 min"
  completed: "2026-05-30"
  tasks_completed: 1
  files_modified: 1
---

# Phase 06 Plan 05: Maintainer image-publish documentation (DOC-01) Summary

**One-liner:** Added `## Maintainer: image-publish pipeline` section to `docs/INSTALL.md` covering the 6-component `build-images` CI matrix, `make acceptance-v1-smoke` local pre-publish fallback, GHCR private-by-default pitfall, and `docker manifest inspect` existence check; zero premature ship-ready claims remain.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add Maintainer: image-publish section to docs/INSTALL.md | 11a5b21 | docs/INSTALL.md |

## What Changed

### Task 1: docs/INSTALL.md — new Maintainer: image-publish pipeline section

New `## Maintainer: image-publish pipeline` section added between `## Is TIDE right for me?` and `## Next steps`. Contains five subsections:

**What publishes:** Table of all 6 component images with their names (`ghcr.io/jsquirrelz/tide-controller`, `tide-dashboard`, `tide-stub-subagent`, `tide-credproxy`, `tide-push`, `tide-claude-subagent`) and source Dockerfiles.

**CI pipeline (IMG-01):** Describes the `build-images` matrix job in `.github/workflows/release.yaml` triggered on `v*` (non-rc) tags. Includes an ASCII run-order diagram (`helmify-verify → build-images + pre-flight → release → chart-publish`) and the image tag convention (`${GITHUB_REF_NAME#v}` strips the `v` prefix so `v1.0.0` → `1.0.0` matching chart `appVersion`).

**Local pre-publish path (IMG-LOAD-01):** Documents `make acceptance-v1-smoke` as the zero-cost pre-tag verification path. Explains the 4-step sequence: `load-images-if-needed.sh` probe + conditional local build + kind load + helm install + small-sample project apply. Notes that no `ANTHROPIC_API_KEY` is required.

**GHCR visibility (Pitfall 3):** Documents the private-by-default behavior on first push with the per-package URL pattern (`https://github.com/users/jsquirrelz/packages/container/<name>/settings`) and the exact UI action (Package visibility → Public). Notes the `401 Unauthorized` symptom and the `ImagePullBackOff` downstream effect.

**Existence check:** `docker manifest inspect ghcr.io/jsquirrelz/tide-controller:1.0.0` — exits 0 if published and public.

## Verification Results

All plan acceptance criteria confirmed:

| Check | Result |
|-------|--------|
| `grep -cE 'acceptance-v1-smoke' docs/INSTALL.md` >= 1 | PASS (count=2) |
| `grep -cE 'build-images' docs/INSTALL.md` >= 1 | PASS (count=6) |
| `grep -cE 'ghcr\.io/jsquirrelz' docs/INSTALL.md` >= 1 | PASS (count=13) |
| `grep -cE 'load-images-if-needed' docs/INSTALL.md` >= 1 | PASS (count=1) |
| `grep -cE 'Maintainer.*image.publish' docs/INSTALL.md` >= 1 | PASS (count=1) |
| `grep -cE 'docker manifest inspect' docs/INSTALL.md` >= 1 | PASS (count=3) |
| `grep -riE 'v0\.1\.0-dev' README.md docs/INSTALL.md` == 0 | PASS (count=0) |

## Deviations from Plan

None — plan executed exactly as written. The 2026-05-30 grep verification of no premature ship-ready claims was confirmed during the read step; no corrections were needed to README.md or INSTALL.md beyond adding the new section.

## Known Stubs

None. The section documents concrete artifacts that already exist: the `build-images` job (af081ed in Plan 06-03), the `load-images-if-needed.sh` helper (34ceb39 in Plan 06-04), the `acceptance-v1-smoke` Makefile target (9f833cb in Plan 06-04), and the 6 component image names from the chart's values.yaml.

## Threat Flags

None beyond the plan's threat model. T-06-05-01 (GHCR private-by-default information disclosure) is mitigated by the new GHCR visibility subsection. T-06-05-02 (premature ship claim tampering) accepted — no uncorrected claims found in live codebase.

## Self-Check: PASSED

- `docs/INSTALL.md` exists: confirmed
- Commit 11a5b21 exists: confirmed via `git rev-parse --short HEAD`
- Section heading `## Maintainer: image-publish pipeline` present: `grep -cE 'Maintainer.*image.publish'` → 1
- All 7 acceptance criteria checks passed (see Verification Results above)
