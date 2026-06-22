---
phase: "29"
plan: "03"
subsystem: "cli"
tags: [cli, import, spdy, kind-integration, tdd]
dependency_graph:
  requires: [29-02]
  provides: [TOOL-01]
  affects: [cmd/tide, test/integration/kind]
tech_stack:
  added: []
  patterns:
    - func-var seam (loaderPodRunner) for offline unit testing of live mode
    - SPDY exec via SubResource("exec") + VersionedParams + NewSPDYExecutor
    - cobra command with --dry-run (offline) / live-stage-only separation (D-05/D-07)
key_files:
  created:
    - cmd/tide/import_envelopes.go
    - cmd/tide/import_envelopes_run.go
    - cmd/tide/import_envelopes_test.go
    - test/integration/kind/loader_exec_smoke_test.go
  modified:
    - cmd/tide/subcommands.go
    - go.mod
    - go.sum
decisions:
  - func-var seam (loaderPodRunner) to keep live-mode testable without K8s; production impl calls RESTConfig() internally
  - emptyDir (not PVC) for smoke test pod — proves URL/verb/codec against real apiserver; PVC binding tested in 29-05
  - verifyLoaderSmokeMarker as supplementary non-fatal check — primary A1/A2 proof is StreamWithContext success + pod Succeeded
  - AlreadyExists swallowed in ConfigMap create to make re-runs idempotent
  - namespace falls back to project.yaml metadata.namespace when --namespace flag is not provided
metrics:
  duration: "~120 min (across 2 sessions)"
  completed: "2026-06-22T06:15:25Z"
  tasks: 3
  files: 6
---

# Phase 29 Plan 03: tide import-envelopes verb + SPDY loader exec smoke Summary

`tide import-envelopes <bundle>` with offline `--dry-run` report (D-07/D-08/D-09), live stage-only mode (D-05/D-06) via SPDY loader pod, and a kind integration smoke test gating the A1/A2 SPDY assumptions.

## Tasks Completed

| # | Name | Commit | Files |
|---|------|--------|-------|
| 1 (RED) | TDD: failing tests for import-envelopes | 9e69cd1 | cmd/tide/import_envelopes_test.go (11 tests) |
| 2 (GREEN) | import-envelopes implementation | 9e69cd1 | cmd/tide/import_envelopes.go, import_envelopes_run.go, subcommands.go, go.mod |
| 3 | LIVE kind SPDY loader-exec smoke | 7d29932 | test/integration/kind/loader_exec_smoke_test.go |

Note: Tasks 1 and 2 were committed together as RED+GREEN in a single `feat(29-03)` commit because the test file had compile-time dependencies on the implementation types (fakeLoaderRunner, injectLoaderRunner) making a pure-RED commit non-buildable without stubs.

## What Was Built

### `cmd/tide/import_envelopes.go`
Cobra command constructor with flags: `--namespace/-n`, `--dry-run` (bool), `--pvc` (default `tide-projects`), `--timeout` (default 5m). RunE adapter routes to `importEnvelopesDryRun` or `importEnvelopesRun` based on `--dry-run`.

### `cmd/tide/import_envelopes_run.go`
Core implementation:

**Offline dry-run** (`--dry-run`): calls `pkg/bundle.OpenBundleDir` + `ValidateBundle`. Hard-rejects cyclic DAG (D-09) by printing edges and returning error. Renders per-level table (D-08) or `--output json` report.

**Live stage-only mode**: reads `project.yaml` from bundle to obtain `seedCMName` and `oldUID`; creates seed ConfigMap (AlreadyExists swallowed for idempotency); calls `loaderPodRunner` (func-var seam, see below); writes `project.yaml` to CWD; prints `tide apply project.yaml` to errOut. **Never applies the Project CR (D-05)**.

**Func-var seam**: `var loaderPodRunner = defaultLoaderPodRunner` where production `defaultLoaderPodRunner` calls `RESTConfig()` internally then delegates to `execLoaderPod`. Tests inject `fakeLoaderRunner.run` to avoid K8s.

**SPDY loader pod** (`execLoaderPod`): busybox:1.36, RestartPolicy Never, Stdin=true, `tar xzf - -C /workspace`, VolumeMount SubPath `<oldUID>/workspace` (RW). Exec URL: `cs.CoreV1().RESTClient().Post().Resource("pods").Name(pod).Namespace(ns).SubResource("exec").VersionedParams(&PodExecOptions{...}, ParameterCodec).URL()`. Streams tgz via `remotecommand.NewSPDYExecutor(restCfg, "POST", url).StreamWithContext(ctx, StreamOptions{Stdin: tgzFile, ...})`.

### `test/integration/kind/loader_exec_smoke_test.go`
Kind integration smoke that gates A1/A2 SPDY exec assumptions:
- Creates busybox:1.36 pod with emptyDir + Stdin=true + `tar xzf - -C /workspace`
- Waits Running; builds exec URL via `SubResource("exec")` + `VersionedParams` + `ParameterCodec`
- `remotecommand.NewSPDYExecutor(restCfg, "POST", execURL).StreamWithContext(ctx, StreamOptions{Stdin: markerTgzReader})`
- A1/A2 proof: StreamWithContext success + pod Succeeded (tar exit 0 = valid tgz)
- Supplementary: `verifyLoaderSmokeMarker` exec confirms mechanism works for read-back (non-fatal)
- Gating: `Label("kind", "long")` + `testing.Short()` skip + `skipIfCRDsOnlyMode()`

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `APIVersionV1Alpha2` does not exist in pkg/dispatch**
- **Found during:** Task 1 test authoring
- **Issue:** Test helper `makeValidEnvelopeBytes` initially referenced `pkgdispatch.APIVersionV1Alpha2` which is not exported
- **Fix:** Changed to `pkgdispatch.APIVersionV1Alpha1` (the only exported constant)
- **Files modified:** cmd/tide/import_envelopes_test.go
- **Commit:** 9e69cd1

**2. [Rule 1 - Bug] `ChildCRDSpec` missing required `Kind` field**
- **Found during:** Task 1 test authoring
- **Issue:** `ChildCRDSpec{Name: "child"}` missing `Kind` field, causing compile error
- **Fix:** Changed to `ChildCRDSpec{Kind: "Task", Name: "child"}`
- **Files modified:** cmd/tide/import_envelopes_test.go
- **Commit:** 9e69cd1

**3. [Rule 3 - Blocking] Missing go.sum entries for remotecommand transitive deps**
- **Found during:** Task 2 build (`go build ./cmd/tide/`)
- **Issue:** `gorilla/websocket`, `k8s.io/streaming`, `moby/spdystream` were transitive deps of `k8s.io/client-go/tools/remotecommand` but not in go.sum
- **Fix:** Ran `go get k8s.io/client-go/tools/remotecommand@v0.36.1` to populate go.sum; then reverted go.mod to keep them as `// indirect` without version pin (already pinned via client-go)
- **Files modified:** go.mod, go.sum
- **Commit:** 9e69cd1

**4. [Rule 1 - Bug] Stray `cmd/tide/project.yaml` written to CWD during unit tests**
- **Found during:** Task 2 test run
- **Issue:** `importEnvelopesRun` writes `project.yaml` to `os.Getwd()`, leaving a file in the test's CWD
- **Fix:** Added `defer func() { _ = os.Remove("project.yaml") }()` in 4 live-mode test functions
- **Files modified:** cmd/tide/import_envelopes_test.go
- **Commit:** 9e69cd1

**5. [Rule 1 - Bug] loader_exec_smoke_test.go: `interface{ Host string }` invalid as parameter type**
- **Found during:** Task 3 authoring (first version)
- **Issue:** First version used an anonymous interface type in a function signature, which is syntactically invalid in Go at the file level
- **Fix:** Completely rewrote the file to use `*rest.Config` directly throughout, obtaining it via `clientcmd.BuildConfigFromFlags("", kubeconfigPath)` inside the test
- **Files modified:** test/integration/kind/loader_exec_smoke_test.go
- **Commit:** 7d29932

## Known Stubs

None — all paths are wired to real implementations. The `loaderPodRunner` func-var is production-wired; tests inject `fakeLoaderRunner`.

## Threat Flags

None — no new network endpoints. The SPDY exec path uses the existing K8s apiserver connection. RBAC requirements (`pods: create,get,delete` + `pods/exec: create`) are documented in plan but chart RBAC is managed by Phase 29-05.

## Self-Check: PASSED

- cmd/tide/import_envelopes.go: EXISTS (committed 9e69cd1)
- cmd/tide/import_envelopes_run.go: EXISTS (committed 9e69cd1)
- cmd/tide/import_envelopes_test.go: EXISTS (committed 9e69cd1), 11/11 tests PASS
- test/integration/kind/loader_exec_smoke_test.go: EXISTS (committed 7d29932), compiles + go vet PASS
- grep 'SubResource("exec")': FOUND in loader_exec_smoke_test.go
- grep 'remotecommand': FOUND in loader_exec_smoke_test.go
- go build ./...: SUCCESS
- go test ./cmd/tide/ -run 'TestImportEnvelopes|TestDryRun': 11/11 PASS
