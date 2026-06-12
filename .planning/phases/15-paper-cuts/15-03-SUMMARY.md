---
phase: 15-paper-cuts
plan: 03
subsystem: cli
tags: [go, kubernetes, pod, pvc, artifact, inspector-pod, log-stream, busybox]

# Dependency graph
requires:
  - phase: 15-paper-cuts
    provides: Context, patterns, and seam idioms (tail.go function-var pattern)
provides:
  - Real inspector-pod execution for tide artifact-get (busybox + PVC subPath + log stream)
  - Race-free readiness wait via in-pod sh stat-stability loop (D-11)
  - --timeout flag (5m default) bounding the full create/wait/stream window
  - Path validation rejecting traversal (..) and shell metacharacters (T-15-08)
  - Function-var seam (inspectorPodRunner) for testable injection without live apiserver
  - Finding-3 regression test pinning run-1 symptom (no pod-spec YAML on stdout)
affects: [15-paper-cuts, phase-16-telemetry]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Inspector-pod log-stream: create Pod + Follow:true GetLogs + io.Copy to stdout (D-10)"
    - "Race-free artifact wait: in-pod sh loop with stat stability check, not client-side polling"
    - "Function-var seam: var inspectorPodRunner = defaultInspectorPodRunner (mirrors tail.go)"
    - "Deferred pod delete with context.Background() covers all exit paths (T-15-09)"
    - "Path via env var ARTIFACT_PATH, never fmt.Sprintf into sh -c (T-15-08 defense-in-depth)"

key-files:
  created:
    - cmd/tide/artifact_get_run_test.go
  modified:
    - cmd/tide/artifact_get_run.go
    - cmd/tide/artifact_get.go
    - cmd/tide/runners.go
    - cmd/tide/describe_budget_test.go

key-decisions:
  - "D-09: Bare inspector Pod + log stream — no Job/TTL indirection; simpler, direct"
  - "D-10: Raw bytes to stdout, status to stderr — pipeable: tide artifact-get ns/proj/PLAN.md > plan.md"
  - "D-11: Race-free wait inside the pod via sh stat stability loop (not client-side polling)"
  - "D-12: Plain non-zero error only after timeout window exhausted"
  - "T-15-08: Path delivered via env var, not string-interpolated into sh -c"
  - "T-15-09: defer Delete(context.Background()) covers all exit paths including timeout"
  - "T-15-11: image fixed to busybox:1.36, command shape fixed (wait+stat+cat)"

patterns-established:
  - "Inspector-pod seam: function-var pattern from tail.go applied to artifact-get runner"
  - "Shell metacharacter rejection via switch-case (not raw-string constant with \t/\n ambiguity)"

requirements-completed: [CUTS-04]

# Metrics
duration: 25min
completed: 2026-06-12
---

# Phase 15 Plan 03: Paper Cuts (CUTS-04 artifact-get) Summary

**Real inspector Pod replaces dry-run stub: busybox + PVC UID-subPath + Follow log stream with race-free in-pod readiness wait and deferred delete**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-06-12T00:00:00Z
- **Completed:** 2026-06-12T00:25:00Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments

- Replaced the v1.0 dry-run stub (artifactGetDryRun) with a real inspector-pod execution path that creates, streams, and deletes a busybox Pod against the live cluster
- Implemented race-free in-pod readiness wait using a sh stat-stability loop (two consecutive equal size samples 2s apart) — guards against half-written artifacts
- Added --timeout (5m default) and --pvc flags; stdout carries raw artifact bytes only (D-10 pipeable contract); status/progress to stderr
- Pinned the run-1 finding-3 regression: TestArtifactGetFinding3Regression asserts no pod-spec YAML markers appear on stdout with the new implementation

## Task Commits

1. **Task 1: Real inspector Pod — create, readiness wait, stream, delete** - `dea6b00` (feat)
2. **Task 2: Tests via fake seam — finding-3 regression + lifecycle assertions** - `81da46a` (test)

## Files Created/Modified

- `cmd/tide/artifact_get_run.go` - Real implementation: parseArtifactRef unchanged; new validateArtifactPath + artifactGetRun + defaultInspectorPodRunner + waitForPodRunning + function-var seam
- `cmd/tide/artifact_get.go` - --timeout (5m) and --pvc flags; updated long help with RBAC note; runArtifactGet moved here with full context.WithTimeout wiring
- `cmd/tide/artifact_get_run_test.go` - 8 tests covering finding-3 regression, D-10 raw bytes, D-12 timeout + T-15-09 delete, path validation, ref parsing
- `cmd/tide/runners.go` - Removed old runArtifactGet stub (moved to artifact_get.go)
- `cmd/tide/describe_budget_test.go` - Removed TestArtifactGetDryRunPrintsPodSpec (tests removed dry-run behavior)

## Decisions Made

- Shell metacharacter validation uses a switch-case rather than a raw-string constant to avoid `\t`/`\n` escape ambiguity in backtick literals (fixed a test failure where `t` in `out.bin` matched the literal `\t` in the constant).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed shell metacharacter set false-positive**
- **Found during:** Task 2 (test run)
- **Issue:** `validateArtifactPath` used a backtick-string constant `$;&| \t\n` where `\t` and `\n` are literal backslash+letter, not tab/newline. This caused `out.bin` to fail validation because `t` was in the set.
- **Fix:** Replaced with an explicit switch-case on specific rune values (`'\t'`, `'\n'`, `'\r'`, etc.) to correctly match only the intended characters.
- **Files modified:** `cmd/tide/artifact_get_run.go`
- **Verification:** `go test ./cmd/tide/... -run ArtifactGet` exits 0; `TestArtifactGetRawBytesContract` passes with `out.bin`
- **Committed in:** `81da46a` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 — bug in metacharacter constant)
**Impact on plan:** Necessary correctness fix; no scope creep.

## Issues Encountered

- `runArtifactGet` in `runners.go` called `artifactGetDryRun` with the old signature. Moved the real implementation to `artifact_get.go` and removed the stub from `runners.go` cleanly.

## User Setup Required

None — no external service configuration required. The live-cluster path requires RBAC (pods: create/get/delete, pods/log: get) but this is operator-side configuration, not a setup step.

## Next Phase Readiness

- CUTS-04 is closed: `tide artifact-get` now executes a real inspector pod and streams artifact bytes to stdout
- The function-var seam (inspectorPodRunner) is in place for kind-harness integration tests in a future plan
- Manual verification against the live kind cluster (tide artifact-get ns/proj/MILESTONE.md) is a phase gate per 15-VALIDATION.md but not automated in this plan

## Stub Tracking

None — no stubs remain. The implementation is real (not dry-run). The inspectorPodRunner seam is a test injection point, not a behavioral stub; defaultInspectorPodRunner is the production path.

## Threat Flags

No new threat surface beyond the mitigations applied:
- T-15-08 (path injection): mitigated via env var delivery + validateArtifactPath
- T-15-09 (pod leak): mitigated via deferred Delete(context.Background())
- T-15-10/T-15-11: accepted/mitigated per plan threat register

---
*Phase: 15-paper-cuts*
*Completed: 2026-06-12*
