---
phase: 28-plan-import-core
plan: "01"
subsystem: chart
tags: [helm, values, deployment, image-contract, import]
dependency_graph:
  requires: []
  provides: [images.tideImport-value-block, TIDE_IMPORT_IMAGE-env-injection]
  affects: [charts/tide/values.yaml, charts/tide/templates/deployment.yaml]
tech_stack:
  added: []
  patterns: [mirror-tideReporter-block, AppVersion-fallback-env]
key_files:
  created: []
  modified:
    - charts/tide/values.yaml
    - charts/tide/templates/deployment.yaml
decisions:
  - "Placed tideImport block immediately after tideReporter block (before subagent.defaults comment) to cluster image declarations together"
  - "Sentinel comment phase28-import-env-injected mirrors phase9-reporter-env-injected pattern for traceability"
metrics:
  duration: "~5 minutes"
  completed: "2026-06-18"
---

# Phase 28 Plan 01: Chart Contract — images.tideImport + TIDE_IMPORT_IMAGE Summary

Chart FIXED contract landed — `images.tideImport` value block and `TIDE_IMPORT_IMAGE` env injection on the manager Deployment, mirroring the `tideReporter`/`TIDE_REPORTER_IMAGE` pattern exactly, additive only with zero existing values modified.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add images.tideImport block to chart values | ef5f2fc | charts/tide/values.yaml |
| 2 | Inject TIDE_IMPORT_IMAGE env on manager Deployment | 133d49a | charts/tide/templates/deployment.yaml |

## What Was Built

**Task 1 — `charts/tide/values.yaml`:** Added a `tideImport` entry to the `images:` map, placed immediately after the `tideReporter` block (before the `subagent.defaults` comment). Fields mirror `tideReporter` exactly: `repository: ghcr.io/jsquirrelz/tide-import`, `tag: ""`, `pullPolicy: IfNotPresent`. A leading comment explains the Phase 28 (IMPORT-01) purpose and the empty-tag skip behavior.

**Task 2 — `charts/tide/templates/deployment.yaml`:** Added a `TIDE_IMPORT_IMAGE` env entry to the manager container's `env:` list, immediately after the `TIDE_REPORTER_IMAGE` entry. Value template: `"{{ .Values.images.tideImport.repository }}:{{ .Values.images.tideImport.tag | default .Chart.AppVersion }}"`. Sentinel comment `# phase28-import-env-injected` added (mirrors `# phase9-reporter-env-injected`).

## Verification

- `helm lint charts/tide` — 1 chart linted, 0 failures
- `helm template charts/tide --set images.tideImport.tag=test` renders `TIDE_IMPORT_IMAGE: ghcr.io/jsquirrelz/tide-import:test`
- `helm template charts/tide` (no tag) renders `TIDE_IMPORT_IMAGE: ghcr.io/jsquirrelz/tide-import:1.0.1` (AppVersion fallback)
- `go test ./test/integration/kind/ -run TestHelm` — all helm contract tests pass
- `git diff` on both files is additions-only (no existing lines modified)

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — this plan is additive chart YAML; no data flows to UI, no placeholder values.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: operator-controlled-image | charts/tide/values.yaml | images.tideImport.repository/tag is operator-controlled at install time — same trust model as existing tideReporter/tidePush (T-28-01-01: accept, no new exposure beyond existing image value blocks) |

## Self-Check: PASSED

- `charts/tide/values.yaml` modified and verified via grep
- `charts/tide/templates/deployment.yaml` modified and verified via helm template
- Commit ef5f2fc exists: `git log --oneline | grep ef5f2fc` — found
- Commit 133d49a exists: `git log --oneline | grep 133d49a` — found
