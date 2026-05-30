---
phase: "06"
plan: "01"
subsystem: helm-chart-tags
tags: [chart, helm, tags, hygiene, troubleshooting]
dependency_graph:
  requires: []
  provides: [CHART-01, HYG-01, A7]
  affects: [charts/tide/values.yaml, hack/helm/tide-values.yaml, examples/projects/small/project.yaml, .gitignore, docs/troubleshooting.md]
tech_stack:
  added: []
  patterns: [helm-sot-pattern, augment-tide-chart-sh]
key_files:
  created: []
  modified:
    - hack/helm/tide-values.yaml
    - charts/tide/values.yaml
    - examples/projects/small/project.yaml
    - .gitignore
    - docs/troubleshooting.md
decisions:
  - CHART-01: 5 component tags in SOT changed v0.1.0-dev → "" (empty string) so `| default .Chart.AppVersion` resolves to 1.0.0; busybox 1.36 preserved
  - A7: examples/projects/small/project.yaml stub-subagent tag v1.0.0 → 1.0.0 (no "v") to match appVersion-resolved chart tag
  - HYG-01: .acceptance-runs/ git-ignored; new troubleshooting row for pre-publish ImagePullBackOff scenario
metrics:
  duration: "~8 min"
  completed: "2026-05-30"
  tasks_completed: 2
  files_modified: 5
---

# Phase 06 Plan 01: CHART-01 + HYG-01 Summary

**One-liner:** Killed 5 dead `v0.1.0-dev` chart tag pins by blanking them in the SOT so Helm's `| default .Chart.AppVersion` resolves all 6 TIDE component images to `1.0.0`; fixed A7 stub tag inconsistency; added .acceptance-runs/ gitignore and ImagePullBackOff pre-publish troubleshooting row.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | CHART-01 — edit SOT and regenerate chart; fix A7 stub tag | bee1be8 | hack/helm/tide-values.yaml, charts/tide/values.yaml, charts/tide/templates/deployment.yaml, charts/tide/templates/validating-webhook-configuration.yaml, examples/projects/small/project.yaml |
| 2 | HYG-01 — add .acceptance-runs/ to .gitignore and ImagePullBackOff row to troubleshooting.md | 73ea9c9 | .gitignore, docs/troubleshooting.md |

## What Changed

### Task 1: CHART-01 + A7

**SOT edits (hack/helm/tide-values.yaml):**
- Line 39: `tag: v0.1.0-dev` → `tag: ""` — controllerManager.manager.image.tag
- Line 140: `tag: v0.1.0-dev` → `tag: ""` — images.stubSubagent.tag
- Line 144: `tag: v0.1.0-dev` → `tag: ""` — images.credProxy.tag
- Line 155: `tag: v0.1.0-dev` → `tag: ""` — images.tidePush.tag
- Line 165: `tag: v0.1.0-dev` → `tag: ""` — images.claudeSubagent.tag
- Line 148: `tag: "1.36"` — busybox third-party tag PRESERVED unchanged
- Comment at line 161 updated: removed the `v0.1.0-dev` mention to satisfy the strict `grep -cE 'v0\.1\.0-dev' == 0` count check

**Chart regenerated:** `make helm` (bash hack/helm/augment-tide-chart.sh) propagated SOT to charts/tide/values.yaml — charts/tide/values.yaml was never hand-edited.

**A7 fix (examples/projects/small/project.yaml):**
- `image: ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0` → `image: ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0`
- Comment on the line above also updated to remove `v1.0.0` reference (to satisfy the strict `grep 'tide-stub-subagent:v1\.0\.0' == 0` check)

### Task 2: HYG-01

**.gitignore:** Appended after `cmd/tide-demo-init/fixture/` entry:
```
# Phase 6 ACC-01 — acceptance run evidence archives (maintainer-local, never committed)
.acceptance-runs/
```

**docs/troubleshooting.md:** New D-C4 table row added after the existing ImagePullBackoff credential-missing row:
- Symptom: pod stuck in ImagePullBackOff immediately after `helm install` (pre-publish scenario)
- Cause: images not yet published to ghcr.io, or chart tag pin doesn't match published tag
- Recipe: `docker manifest inspect` check + `make acceptance-v1-smoke` + Phase 6 chart tag verification

## Verification Results

All plan `must_haves.truths` confirmed:

| Check | Result |
|-------|--------|
| `grep -cE 'v0\.1\.0-dev' charts/tide/values.yaml hack/helm/tide-values.yaml` == 0 | PASS |
| `helm template charts/tide \| grep -E 'image:' \| grep -v '1\.0\.0\|1\.36'` returns empty | PASS |
| busybox `tag: "1.36"` preserved in SOT | PASS (count=1) |
| `tide-stub-subagent:1.0.0` in project.yaml (no `v` prefix) | PASS |
| `.acceptance-runs/` git-ignored | PASS |
| `grep -ciE 'ImagePullBackOff' docs/troubleshooting.md` >= 2 | PASS (count=2) |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Comment in SOT and project.yaml contained v0.1.0-dev / v1.0.0 literal strings**
- **Found during:** Task 1 verification
- **Issue:** `grep -cE 'v0\.1\.0-dev'` acceptance check counts ALL occurrences including comments; SOT line 161 had `"tag-based pulls of "v0.1.0-dev""` and project.yaml comment had `ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0` in a comment
- **Fix:** Updated both comments to remove the version string references; re-ran `make helm` to propagate the SOT comment fix
- **Files modified:** hack/helm/tide-values.yaml, charts/tide/values.yaml (regenerated), examples/projects/small/project.yaml
- **Commit:** bee1be8 (included in Task 1 commit)

## Known Stubs

None. All 5 empty-string tags resolve via `| default .Chart.AppVersion` to the real `1.0.0` appVersion declared in charts/tide/Chart.yaml.

## Threat Flags

None beyond those already tracked in the plan's threat model (T-06-01-01 mitigated by the SOT-only edit pattern; T-06-01-02 mitigated by the .gitignore entry now added).

## Self-Check: PASSED

- `hack/helm/tide-values.yaml` exists and has 5 `tag: ""` entries for TIDE components
- `charts/tide/values.yaml` exists and was regenerated from SOT
- `examples/projects/small/project.yaml` line 49 (image field) references `1.0.0` without `v` prefix
- `.gitignore` contains `.acceptance-runs/` entry
- `docs/troubleshooting.md` has 2 ImagePullBackOff rows
- Commits bee1be8 and 73ea9c9 confirmed in git log
