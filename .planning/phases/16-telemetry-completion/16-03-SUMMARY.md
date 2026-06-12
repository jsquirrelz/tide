---
phase: 16-telemetry-completion
plan: "03"
subsystem: build-and-ci
tags: [makefile, ci, helm, telemetry, telem-05]
dependency_graph:
  requires: []
  provides: [helm-telemetry-assert, helm-assert, ci-helm-assert-step]
  affects: [Makefile, .github/workflows/ci.yaml, hack/helm/assert-prometheus-env.py]
tech_stack:
  added: []
  patterns: [makefile-phony-target, github-actions-step]
key_files:
  created: []
  modified:
    - Makefile
    - .github/workflows/ci.yaml
    - hack/helm/assert-prometheus-env.py
decisions:
  - "helm-telemetry-assert runs three scripts: assert-prometheus-env.py --expect-absent, assert-prometheus-env.py --expect-endpoint, and assert-telemetry-render.sh"
  - "helm-assert is a pure prereq aggregate with no recipe body — both constituent targets run independently"
  - "No new ci.yaml setup steps needed — azure/setup-helm@v4 and python3 already present in helm-lint job"
metrics:
  duration: "~5 minutes"
  completed: "2026-06-12T21:00:43Z"
  tasks_completed: 2
  tasks_total: 2
  files_modified: 3
requirements: [TELEM-05]
---

# Phase 16 Plan 03: Helm Gate Wiring (TELEM-05) Summary

Wire orphaned `hack/helm` telemetry render gates into the Makefile (`helm-telemetry-assert` + `helm-assert`) and CI (`make helm-assert` step in helm-lint job), correcting the docstring that falsely claimed `make helm-rbac-assert` drove the telemetry scripts.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add helm-telemetry-assert + helm-assert Makefile targets and fix docstrings (D-13) | be1fede | Makefile, hack/helm/assert-prometheus-env.py |
| 2 | Add make helm-assert step to ci.yaml helm-lint job (D-14) | b01fd77 | .github/workflows/ci.yaml |

## What Was Built

**Task 1 — Makefile targets (D-13):**
- `helm-telemetry-assert` (.PHONY): runs `assert-prometheus-env.py --expect-absent`, `assert-prometheus-env.py --expect-endpoint http://prom:9090`, and `assert-telemetry-render.sh` — all six gate permutations pass locally
- `helm-assert` (.PHONY): aggregate prereq target (`helm-rbac-assert helm-telemetry-assert`) with no recipe body
- Docstring correction in `assert-prometheus-env.py`: "Driven by `make helm-rbac-assert`" corrected to "Driven by `make helm-telemetry-assert`"

**Task 2 — CI step (D-14):**
- New step appended to `helm-lint` job after "Verify chart tree is reproducible" step
- Step name: `Helm render gate assertions (TELEM-05 D-14)`; run: `make helm-assert`
- Zero added setup cost: `azure/setup-helm@v4` and `python3` already present in the job

## Deviations from Plan

None — plan executed exactly as written. The `awk` acceptance check in the plan spec (`awk '/^  helm-lint:/,/^  [a-z-]+:/'`) returns 0 matches only because `helm-lint` is the last job in ci.yaml and no subsequent job header terminates the range. Direct evidence confirms correct placement: `grep -n "make helm-assert" .github/workflows/ci.yaml` returns line 195, which falls inside the helm-lint job section (lines 141-196).

## Verification Results

All checks passed:

```
make helm-telemetry-assert:
  PASS: PROM_ENDPOINT env var is absent from dashboard container (expected)
  PASS: PROM_ENDPOINT='http://prom:9090' on dashboard container (expected)
  PASS [A]: default render exits 0; no PROM_ENDPOINT in output (graceful-degradation OK)
  PASS [B]: PROM_ENDPOINT env entry with value http://prom:9090 is present in rendered output
  PASS [C]: retentionTime=30d renders without error; values file documents storage.tsdb.retention.time
  PASS [D]: helm lint exits 0
  PASS: all 4 permutations passed — EC-7 render gate satisfied

make helm-assert: exits 0 (runs helm-rbac-assert + helm-telemetry-assert)
YAML parse: python3 yaml.safe_load exits 0
git status --porcelain charts/: empty (chart untouched)
```

## Known Stubs

None.

## Threat Flags

No new security surface. The new CI step runs `make helm-assert` (pure `helm template` render gates — no cluster credentials, no GITHUB_TOKEN scope expansion). Consistent with T-16-09 disposition in plan's threat register.

## Self-Check: PASSED

- Makefile: `grep -cE '^helm-telemetry-assert:' Makefile` = 1 ✓
- Makefile: `grep -cE '^helm-assert: helm-rbac-assert helm-telemetry-assert' Makefile` = 1 ✓
- Docstrings: `grep -rn "helm-rbac-assert" hack/helm/assert-prometheus-env.py hack/helm/assert-telemetry-render.sh` = 0 ✓
- Charts untouched: `git status --porcelain charts/` = empty ✓
- CI YAML parses: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yaml'))"` exits 0 ✓
- Commits: be1fede and b01fd77 both exist in git log ✓
