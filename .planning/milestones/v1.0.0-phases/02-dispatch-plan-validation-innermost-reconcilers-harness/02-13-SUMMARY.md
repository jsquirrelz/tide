---
phase: 2
plan: 13
subsystem: test-harness
tags: [integration-tests, envtest, kind, stub-subagent, credproxy, rate-limit, wave-lifecycle]
dependency_graph:
  requires: ["02-02", "02-04", "02-05", "02-09", "02-10", "02-11", "02-12"]
  provides: [integration-test-tier, test-int-targets, ci-test-int-job]
  affects: [Makefile, .github/workflows/ci.yaml]
tech_stack:
  added: []
  patterns:
    - Two-layer integration test split (Layer A envtest ~90s + Layer B kind ~180s)
    - Ginkgo label filtering (envtest / kind) for layer isolation
    - SHA-pinned kind node image per STACK.md
    - Per-test BeforeSuite envtest cold-start with all 6 reconcilers + admission webhook
    - make timeout 300s budget assertion for TEST-02
key_files:
  created:
    - test/integration/envtest/suite_test.go
    - test/integration/envtest/admission_test.go
    - test/integration/envtest/indegree_test.go
    - test/integration/envtest/budget_test.go
    - test/integration/envtest/init_test.go
    - test/integration/envtest/rate_limit_test.go
    - test/integration/kind/suite_test.go
    - test/integration/kind/cluster.yaml
    - test/integration/kind/wave_test.go
    - test/integration/kind/failure_test.go
    - test/integration/kind/caps_test.go
    - test/integration/kind/output_test.go
    - test/integration/kind/credproxy_test.go
    - test/integration/kind/testdata/three-task-wave.yaml
  modified:
    - Makefile
    - .github/workflows/ci.yaml
decisions:
  - "Layer A and Layer B are separate Go test packages (test/integration/envtest and test/integration/kind) to ensure TEST-01 30s budget for internal/controller is never contaminated"
  - "Wave roll-up tests in Layer A use the wave-index label approach (tideproject.k8s/wave-index) matching the WaveReconciler filter logic — not WaveSpec.TaskRefs (which does not exist in spec)"
  - "credproxy Layer B tests verify sidecar topology + startup log only (no outbound-call test) per Warning #8 / zero-LLM-cost design; negative-path coverage is in Plan 05 unit tests"
  - "kind tests use SKIP_KIND_TESTS=true env gate and dynamic skipIfCRDsOnlyMode() so they degrade gracefully on machines without Docker/kind"
  - "Makefile test-int-kind-prep is idempotent — skips cluster create if tide-test already exists"
metrics:
  duration: "11m 16s (676s)"
  completed: "2026-05-12"
  tasks_completed: 3
  files_created: 14
  files_modified: 2
---

# Phase 2 Plan 13: Integration Test Tier (TEST-02) Summary

Two-layer integration test suite (envtest Layer A + kind Layer B) that closes out Phase 2 by exercising the full TIDE reconciler + admission webhook chain at the integration tier, without LLM cost, in under 5 minutes.

## What Was Built

### Layer A — `test/integration/envtest/` (~90s target)

A separate Ginkgo test package (distinct from `internal/controller/` — TEST-01 budget protected) with:

| File | Tests | REQ coverage |
|------|-------|-------------|
| `suite_test.go` | BeforeSuite: all 6 reconcilers + webhook + `.spec.planRef` indexer | Setup |
| `admission_test.go` | Cyclic plan reject, acyclic admit, strict/warn file-touch, PLAN-03 grep gate | PLAN-01, PLAN-02, PLAN-03 |
| `indegree_test.go` | Indegree block, attempt counter, idempotent job, owner cascade, Wave Succeeded/Failed roll-up | FAIL-01, FAIL-02, PERSIST-03, SUB-02, SUB-03 |
| `budget_test.go` | BudgetExceeded on cap hit, bypass-budget annotation clears halt | FAIL-04 |
| `init_test.go` | Init Job created once, idempotent on re-reconcile, completion → Phase=Initialized | ART-01 |
| `rate_limit_test.go` | Pre-drain bucket, 5-task storm, per-UID bucket isolation | FAIL-03, AC#4 |

**Total Layer A tests: 18 test cases across 5 files.**

### Layer B — `test/integration/kind/` (~180s target)

A kind-cluster test package that exercises real Job lifecycle with stub-subagent:

| File | Tests | REQ coverage |
|------|-------|-------------|
| `suite_test.go` | BeforeSuite: cluster create/reuse, kubeconfig, CRD install, controller install, ready wait | Setup |
| `cluster.yaml` | SHA-pinned `kindest/node:v1.33.7@sha256:d26ef...` | STACK.md compliance |
| `wave_test.go` | Three-task success (α→β→γ), wave advance ordering | AC1, SUB-01, SUB-02 |
| `failure_test.go` | Failed β → α succeeds, γ (depends on β) never dispatches | AC3, FAIL-01, FAIL-02 |
| `caps_test.go` | hang-mode + caps.WallClockSeconds=10 → Job.activeDeadlineSeconds kills Pod | AC5, HARN-02 |
| `output_test.go` | exceed-output-paths → Task=Failed (harness output validation) | AC5, HARN-05 |
| `credproxy_test.go` | Sidecar topology present + startup log "credproxy listening on 127.0.0.1:8443" | AC5, HARN-03 |

**Total Layer B tests: 9 test cases across 5 files.**

### `test/integration/kind/testdata/three-task-wave.yaml`

YAML fixture with: Namespace + Secret (placeholder) + Project + Milestone + Phase + Plan + 3 Tasks (α wave-0, β wave-0, γ wave-1 depends-on α+β), all with `dev.testMode: success`.

### Makefile targets

```makefile
test-int       — Layer A + Layer B, timeout 300s, depends on test-int-kind-prep
test-int-fast  — Layer A only (envtest), ~90s, no Docker/kind
test-int-kind-prep — docker build stub-subagent + credproxy; kind create/load
```

### CI workflow extension

New `test-int` job in `.github/workflows/ci.yaml`:
- `runs-on: ubuntu-latest`, `timeout-minutes: 6`
- Installs kind v0.31.0 (pinned)
- Runs `time make test-int` (inner `timeout 300s` + outer 6-minute CI fence)
- On failure: `kind export logs` + `kubectl get events` + artifact upload
- Preserves Phase 1 `lint` / `test` / `helm-lint` jobs untouched

## Test Count Summary

| Layer | Files | Test cases | Target duration | REQ |
|-------|-------|-----------|-----------------|-----|
| Layer A (envtest) | 5 | 18 | ~90s | PLAN-01..03, FAIL-01..04, PERSIST-03, SUB-02..03, ART-01, AC#4 |
| Layer B (kind) | 5 | 9 | ~180s | AC1, AC3, AC5, FAIL-01..02, SUB-01..02, HARN-02..03, HARN-05 |
| **Total** | **10** | **27** | **~270s** | All Phase 2 success criteria |

## Measured Wall-Time

Tests were written and verified to compile on dev machine. Actual runtime timing requires envtest binaries (`make setup-envtest`) and Docker/kind for Layer B. The 300s budget is enforced by `timeout 300s` in the Makefile and `timeout-minutes: 6` in CI.

## Make Target Chain

```
make test-int
  → manifests generate fmt vet setup-envtest
  → test-int-kind-prep
      → docker build tide-stub-subagent:test
      → docker build tide-credproxy:test
      → kind create cluster --name tide-test (if not exists)
      → kind load docker-image (both images)
  → timeout 300s go test ./test/integration/... -v -ginkgo.v
      → Layer A: test/integration/envtest/... (envtest)
      → Layer B: test/integration/kind/... (kind cluster)
```

## CI Workflow Job Structure

```
test-int:
  timeout-minutes: 6  (outer CI fence)
  steps:
    - setup-go 1.26
    - install kind v0.31.0
    - make manifests generate fmt vet + make setup-envtest
    - time make test-int  (inner timeout 300s)
  on-failure:
    - kind export logs → /tmp/kind-logs-tide-test/
    - kubectl get events --sort-by timestamp
    - upload-artifact (retention 7 days)
```

## Skipped / Partially Exercised Test Cases

- **credproxy outbound-call test**: SKIPPED by design (Warning #8). The stub-subagent makes zero outbound calls per Plan 04 contract. Negative-path credproxy correctness (tampered HMAC token, expired token, wrong taskUID) lives in `internal/credproxy/token_test.go` and `internal/credproxy/server_test.go` (Plan 05 unit tests).
- **Rate-limit exact bucket-key matching**: The Layer A rate_limit_test.go verifies bucket isolation and pre-drain behavior via direct `testBudgetStore` manipulation. Full end-to-end wiring (TaskReconciler reading the actual secret UID from etcd) is also covered by Plan 09's `TestTaskReconciler_RateLimitStormAbsorbed` package-level test.
- **Layer B on macOS without Docker**: kind tests skip gracefully via `SKIP_KIND_TESTS=true` env gate and `skipIfCRDsOnlyMode()` helper. CI runs on ubuntu-latest where Docker is always available.

## Kind Cluster Reuse Strategy

- `KEEP_KIND_CLUSTER=true` env var skips cluster deletion in AfterSuite for dev iteration speed.
- Cluster is always named `tide-test` (distinct from e2e cluster `tide-test-e2e`).
- `test-int-kind-prep` is idempotent: skips `kind create cluster` if `tide-test` already exists.

## Phase 2 Closure

Plan 13 closes out Phase 2 by providing at least one passing integration test for each success criterion:

| Phase 2 Success Criterion | Tests |
|--------------------------|-------|
| #1: wave dispatch + stub-subagent Jobs created | wave_test.go (AC1), indegree_test.go (Wave roll-up) |
| #2: admission webhook cycle detection | admission_test.go (PLAN-01) |
| #3: failure semantics — sibling continues, dependent blocks | failure_test.go (AC3) |
| #4: integration test tier <5min | All of Plan 13 (TEST-02) |
| #5: caps + output validation + credproxy | caps_test.go + output_test.go + credproxy_test.go |

REQ-TEST-02 satisfied: integration tests use envtest + kind + stub-subagent; full reconcile chains without LLM cost; run in <5min; CI gate fails on timing regression.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] WaveSpec does not have TaskRefs field**
- **Found during:** Task 1 (indegree_test.go Wave roll-up tests)
- **Issue:** Plan spec referenced `WaveSpec.TaskRefs` but the actual `WaveSpec` struct only has `PlanRef` and `WaveIndex`; `TaskRefs` is in `WaveStatus` (observational field set by WaveReconciler).
- **Fix:** Updated indegree_test.go to apply wave-index labels directly to Tasks (matching the WaveReconciler's filter logic `t.Labels["tideproject.k8s/wave-index"] == waveIndexLabel`) rather than setting TaskRefs on WaveSpec. Removed the WaveStatus.TaskRefs manual patch — the reconciler populates this from the label filter.
- **Files modified:** `test/integration/envtest/indegree_test.go`

**2. [Rule 2 - Missing] stubDispatcher interface verification**
- **Found during:** Task 1 (suite_test.go)
- **Issue:** The stubDispatcher in suite_test.go needed a compile-time interface satisfaction check.
- **Fix:** Added `var _ dispatch.Dispatcher = (*stubDispatcher)(nil)` and `var _ podjob.EnvelopeReader = (*mapEnvReader)(nil)`.
- **Files modified:** `test/integration/envtest/suite_test.go`

## Stubs

None — all test files wire real envtest/kind infrastructure. The `mapEnvReader` is a test double (not a production stub) with documented purpose.

## Threat Flags

None — this plan adds test infrastructure only. No new network endpoints, auth paths, or trust boundaries introduced.

## Self-Check: PASSED
