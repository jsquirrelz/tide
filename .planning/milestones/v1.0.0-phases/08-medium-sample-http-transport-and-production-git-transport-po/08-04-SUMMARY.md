---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
plan: "04"
subsystem: examples
tags: [image-tags, samples, alignment, cosmetic]

dependency_graph:
  requires: [08-01]
  provides: [aligned-image-tags-in-examples]
  affects: [examples/projects/medium, examples/projects/large, examples/projects/small]

tech_stack:
  added: []
  patterns: [canonical no-v image tag form matching chart appVersion SOT]

key_files:
  modified:
    - examples/projects/medium/demo-remote-init-job.yaml
    - examples/projects/medium/project.yaml
    - examples/projects/large/project.yaml
    - examples/projects/small/README.md

decisions:
  - "Aligned all v-prefix image tags (:v1.0.0) to canonical no-v form (:1.0.0) matching chart appVersion SOT"
  - "Added canonical-form comment in small/README.md near updated lines per plan instruction"

metrics:
  duration: "~3 minutes"
  completed_date: "2026-06-03"
  tasks_completed: 1
  tasks_total: 1
  files_modified: 4
---

# Phase 08 Plan 04: Image Tag Alignment Summary

Aligned four sample/demo files from v-prefix `:v1.0.0` to the canonical no-v `:1.0.0` form that matches the chart SOT (`hack/helm/tide-chart.yaml` `appVersion: "1.0.0"`), acceptance scripts, and load-images scripts.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Align image tags in medium and large sample manifests | 28c5f38 | examples/projects/medium/demo-remote-init-job.yaml, examples/projects/medium/project.yaml, examples/projects/large/project.yaml, examples/projects/small/README.md |

## Verification Results

- `grep -rn ':v1\.' examples/ | grep image:` returns 0 results (SC-6 gate: PASS)
- `grep -n 'tide-demo-init:1.0.0' examples/projects/medium/demo-remote-init-job.yaml` → line 42: PASS
- `grep -n 'tide-claude-subagent:1.0.0' examples/projects/medium/project.yaml` → line 90: PASS
- `grep -n 'tide-claude-subagent:1.0.0' examples/projects/large/project.yaml` → line 105: PASS
- `grep -n 'tide-stub-subagent:1.0.0' examples/projects/small/README.md` → lines 126, 132, 133: PASS
- `examples/projects/small/project.yaml` remains at `:1.0.0` (untouched): PASS

## Changes Made

Four surgical substitutions, no other fields altered:

1. `examples/projects/medium/demo-remote-init-job.yaml` line 42: `tide-demo-init:v1.0.0` → `tide-demo-init:1.0.0`
2. `examples/projects/medium/project.yaml` line 90: `tide-claude-subagent:v1.0.0` → `tide-claude-subagent:1.0.0`
3. `examples/projects/large/project.yaml` line 105: `tide-claude-subagent:v1.0.0` → `tide-claude-subagent:1.0.0`
4. `examples/projects/small/README.md` lines 126, 132, 133: all three `tide-stub-subagent:v1.0.0` → `tide-stub-subagent:1.0.0`; added canonical-form comment on line 127

## Deviations from Plan

None — plan executed exactly as written. The README had three occurrences of the v-prefix tag (not two as implied by "lines 130-131" in RESEARCH.md — line 126 also had one). All three were updated; gate passes.

## Known Stubs

None.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes introduced.

## Self-Check: PASSED

- examples/projects/medium/demo-remote-init-job.yaml: exists, contains `tide-demo-init:1.0.0`
- examples/projects/medium/project.yaml: exists, contains `tide-claude-subagent:1.0.0`
- examples/projects/large/project.yaml: exists, contains `tide-claude-subagent:1.0.0`
- examples/projects/small/README.md: exists, contains `tide-stub-subagent:1.0.0`
- Commit 28c5f38: verified in git log
- SC-6 gate: PASS (0 v-prefixed image tags in examples/)
