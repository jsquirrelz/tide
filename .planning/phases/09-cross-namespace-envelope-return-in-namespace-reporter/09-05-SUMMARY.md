---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: "05"
subsystem: reporter-binary
tags: [reporter, cmd-binary, docker, envtest, materialize, idempotency]
dependency_graph:
  requires: [09-04]
  provides: [REQ-09-01]
  affects: [Makefile, test/integration/envtest]
tech_stack:
  added:
    - "cmd/tide-reporter: Go cmd-main binary (plan's testable-seam pattern)"
    - "images/tide-reporter/Dockerfile: distroless/static:nonroot multi-stage image"
  patterns:
    - "cfg-by-value + run(ctx, cfg, stdout, stderr) int testable seam (mirrors cmd/tide-push)"
    - "in-cluster controller-runtime client via config.GetConfig() + client.New (mirrors cmd/manager)"
    - "runWithClient injectable seam for tests (fake.Client injected, nil triggers in-cluster)"
    - "Ginkgo envtest Label('reporter-materialize') test suite"
key_files:
  created:
    - cmd/tide-reporter/main.go
    - cmd/tide-reporter/main_test.go
    - images/tide-reporter/Dockerfile
    - test/integration/envtest/reporter_materialize_test.go
  modified:
    - Makefile
    - .gitignore
decisions:
  - "runWithClient injectable seam over run() injection avoids exported test helper: tests call runWithClient directly with fake.Client; main() wires nil -> in-cluster"
  - "resolveParent switches on --parent-kind to typed Get: avoids runtime type assertions; returns metav1.Object for MaterializeChildCRDs"
  - "TC-2 uses manual List+filter instead of MatchingFields: .spec.milestoneRef field indexer is registered on mgrClient (SetupWithManager) but not k8sClient in envtest package; MatchingFields returns error (not just empty list) when indexer absent"
  - ".gitignore: added /tide-reporter alongside /tide-push for repo-root go build artifacts"
metrics:
  duration: "~15min"
  completed: "2026-06-08"
  tasks: 3
  files: 6
---

# Phase 09 Plan 05: Reporter Binary + Dockerfile + envtest Summary

Reporter binary (cmd/tide-reporter) + distroless image + Makefile kind preload + envtest integration tests for the in-namespace child-CRD materialization path.

## What Was Built

**Task 1 — cmd/tide-reporter binary (7481063)**

The Option-C reader Job binary. `reporterConfig` struct by value, `run()` → `runWithClient()` testable seam (mirrors cmd/tide-push:79-201). `runWithClient` accepts an injectable `client.Client` (nil triggers `config.GetConfig()` + `client.New` in-cluster wiring, matching cmd/manager:32-45 scheme registration). Flow:
1. Validate required flags (--task-uid, --parent-name, --parent-namespace, --parent-kind)
2. Build K8s client with `clientgoscheme + tidev1alpha1` scheme
3. Read `out.json` from `filepath.Join(workspace, "envelopes", taskUID, "out.json")` — same-namespace PVC (#11/#12 fix)
4. `resolveParent()` switches on --parent-kind → typed `c.Get` → `metav1.Object`
5. `reporter.ChildrenAlreadyMaterialized()` idempotency guard
6. `reporter.MaterializeChildCRDs()` creates child CRs via K8s API
7. Exit codes: 0=success, 1=generic K8s error, 2=invariant/bad-args

`main_test.go` drives `runWithClient()` directly with `fake.Client` covering:
- Happy path: 2 Phase children created with controller ownerRef + milestoneRef
- Idempotent re-run: ChildrenAlreadyMaterialized short-circuits, exit 0
- Missing out.json: non-zero exit
- Disallowed Kind (Pod): non-zero exit, no children created
- Parent not found: non-zero exit
- Missing required flags: non-zero exit (4 subtests)

**Task 2 — tide-reporter image + Makefile (552a6c9)**

`images/tide-reporter/Dockerfile`: golang:1.26-alpine builder, copies `api/ internal/reporter/ internal/owner/ pkg/dispatch/ cmd/tide-reporter/`, `CGO_ENABLED=0 -ldflags="-s -w"`, output `/out/tide-reporter`; runtime stage `gcr.io/distroless/static:nonroot`, `ENTRYPOINT ["/usr/local/bin/tide-reporter"]`. Docker build succeeds.

`Makefile` test-int-kind-prep: added build + `kind load` for `ghcr.io/jsquirrelz/tide-reporter:test` alongside existing images. `docker-buildx-snapshot` doc comment updated 6→7 images + tide-reporter line added.

**Task 3 — envtest reporter_materialize_test.go (a98a9d5)**

3 Ginkgo specs under `Label("envtest", "phase9", "reporter-materialize")`:
- TC-1: creates Phase with controller ownerRef (UID-matched Milestone) + `Spec.MilestoneRef` set
- TC-2: idempotent — `ChildrenAlreadyMaterialized` returns true after first materialize; second call does not duplicate (manual List filter, not `MatchingFields` — indexer not registered on k8sClient)
- TC-3: fixture compatibility — JSON fixture mimicking both stub-authored + real-authored `EnvelopeOut` deserializes + materializes 2 Phase children

All 3 pass under `make test-int-fast`. Pre-existing failures in other envtest specs unchanged.

## Verification Results

- `go test ./cmd/tide-reporter/... -count=1`: PASS (6 tests)
- `go build ./cmd/tide-reporter`: OK
- `docker build -f images/tide-reporter/Dockerfile .`: exit=0
- `make verify-import-firewall`: OK (reporter imports no LLM SDK)
- `make test-int-fast` reporter-materialize specs: 3/3 PASS

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TC-2 MatchingFields unsupported in envtest k8sClient**
- **Found during:** Task 3
- **Issue:** `client.MatchingFields{".spec.milestoneRef": milestoneName}` returns `field label not supported` error (not empty results) on `k8sClient` because the `.spec.milestoneRef` indexer is registered on `mgrClient` (via `SetupWithManager`) not on the plain `k8sClient`
- **Fix:** Replaced `MatchingFields` List with a manual `List(InNamespace) + for-range filter` — reliable against both the envtest apiserver and fake clients
- **Files modified:** `test/integration/envtest/reporter_materialize_test.go`
- **Commit:** a98a9d5

**2. [Rule 2 - Missing] /tide-reporter not in .gitignore**
- **Found during:** post-commit untracked file check
- **Issue:** `go build ./cmd/tide-reporter` in the verify step emits a repo-root binary; `/tide-push` and `/stub-subagent` are gitignored but `/tide-reporter` was not
- **Fix:** Added `/tide-reporter` to `.gitignore` alongside existing binary entries
- **Files modified:** `.gitignore`
- **Commit:** b16e0f6

## Threat Flags

No new trust boundaries introduced. The binary reads a local file and calls `reporter.MaterializeChildCRDs` — both already covered by T-09-10/T-09-11/T-09-12 in the plan's threat model.

## Self-Check: PASSED

Files verified:
- cmd/tide-reporter/main.go: FOUND
- cmd/tide-reporter/main_test.go: FOUND
- images/tide-reporter/Dockerfile: FOUND
- test/integration/envtest/reporter_materialize_test.go: FOUND
- Makefile tide-reporter lines: FOUND

Commits verified:
- 7481063 (Task 1): FOUND
- 552a6c9 (Task 2): FOUND
- a98a9d5 (Task 3): FOUND
- b16e0f6 (.gitignore): FOUND
