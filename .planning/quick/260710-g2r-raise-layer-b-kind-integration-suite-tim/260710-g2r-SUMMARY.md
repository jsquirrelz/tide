---
id: 260710-g2r
slug: raise-layer-b-kind-integration-suite-tim
type: quick
status: complete
completed: 2026-07-10
commits:
  - 8930c3f  # fix(test-int): internal Ginkgo/go-test/outer budgets
  - c6fe34c  # ci(kind): CI step/job timeout-minutes
files_modified:
  - test/integration/kind/suite_test.go
  - Makefile
  - .github/workflows/kind-sensitive.yml
  - .github/workflows/nightly-integration.yml
---

# Quick Task 260710-g2r — Summary

## What changed

Main was RED: the Layer B kind suite (`make test-int`) outgrew every timeout
layer after Phases 36/37 (agent-identity chart spec + the artifact-staging
DASH-02 live 4-level planner cascade, a ~12m tail). Root cause was the **CI
step-level `timeout-minutes`**, not the internal Ginkgo ctx — the step killed
`make test-int` mid-suite (~30m in on kind-sensitive; 21m54s of Layer B on
nightly). Raised every layer consistently so the innermost fires loudly first.

| Layer | Before | After | File |
|---|---|---|---|
| Ginkgo ctx `kindTestTimeout` | 25m | **45m** | `suite_test.go:117` |
| go-test `KIND_GO_TEST_TIMEOUT` | 40m | **50m** | `Makefile:22` |
| outer shell `INTEGRATION_TIMEOUT` | 2700s | **3300s** (55m) | `Makefile:21` |
| kind-sensitive step | 35m | **60m** | `kind-sensitive.yml` |
| kind-sensitive job | 42m | **70m** | `kind-sensitive.yml` |
| nightly kind step | 25m | **60m** | `nightly-integration.yml` |
| nightly job | 45m | **110m** | `nightly-integration.yml` |

Final nesting: `Ginkgo 45m < go-test 50m < outer 55m < CI step 60m < job`.

## Explicitly NOT done

- Did **not** trim the artifact-staging spec's `Eventually`s — they are the real
  correctness budget for a live cascade; trimming risks flakes. (Task said this
  was optional; skipped by design.)

## Verification

- `go test -run '^$' ./test/integration/kind/...` → `ok [no tests to run]` (compiles).
- Both workflow YAMLs parse (`yaml.safe_load`); step < job in each.
- `gofmt -l suite_test.go` clean.
- **CI green confirmation:** pending — the kind-sensitive check runs on the PR /
  push and must go green before merge (it touches `test/integration/kind/**` and
  `.github/workflows/kind-sensitive.yml`, both in the `paths:` gate).
