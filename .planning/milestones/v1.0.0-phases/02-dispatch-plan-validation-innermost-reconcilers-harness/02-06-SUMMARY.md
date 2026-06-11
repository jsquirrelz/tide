---
phase: 2
plan: 6
subsystem: harness
tags: [security, caps, redaction, output-validation, harness, tdd]
dependency_graph:
  requires: ["02-01"]
  provides: ["internal/harness", "internal/harness/redact"]
  affects: ["02-09"]
tech_stack:
  added: []
  patterns:
    - "tail-keep buffer (maxPatternLen=2048) for split-token redaction"
    - "filepath.EvalSymlinks + filepath.Rel for output-path scope enforcement"
    - "context.WithTimeout for wall-clock cap enforcement"
    - "Runtime interface seam for Phase 3 Claude Code swap"
key_files:
  created:
    - internal/harness/doc.go
    - internal/harness/harness.go
    - internal/harness/caps.go
    - internal/harness/outputs.go
    - internal/harness/envelope_io.go
    - internal/harness/redact/doc.go
    - internal/harness/redact/patterns.go
    - internal/harness/redact/redact.go
    - internal/harness/redact/redact_test.go
    - internal/harness/caps_test.go
    - internal/harness/outputs_test.go
    - internal/harness/harness_test.go
    - internal/harness/envelope_io_test.go
  modified: []
decisions:
  - "Anthropic key regex requires {20,} suffix chars — corrected test fixture from 19 to 21 chars (aBcDeFgHiJkLmNoPqRsTuV)"
  - "fakeRuntime.Execute signature uses io.Writer (not unnamed interface literal) to satisfy Runtime interface"
  - "WriteEnvelopeIn added as a helper alongside ReadEnvelopeIn to enable round-trip test and future orchestrator use"
metrics:
  duration: "7min"
  completed: "2026-05-13"
  tasks: 4
  files: 13
---

# Phase 2 Plan 6: harness (HARN-01..06) Summary

In-pod harness package with cap enforcement, secret-pattern redaction, output-path validation, and the Runtime interface seam for Phase 3.

## What Was Built

### redact sub-package (Task 1)

`internal/harness/redact/patterns.go` — compiled `SecretPatterns` slice with 6 regex patterns: `sk-ant-api03-[A-Za-z0-9\-_]{20,}`, `sk-[A-Za-z0-9]{20,}`, `gh[ps]_[A-Za-z0-9]{36}`, `xox[abp]-[A-Za-z0-9\-]+`, `AKIA[A-Z0-9]{16}`, `eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+` (JWT). `maxPatternLen = 2048` upper bounds the tail-keep buffer.

`internal/harness/redact/redact.go` — `RedactingWriter` wraps `io.Writer` with a tail-keep buffer. Each `Write(p)` prepends `w.tail` to `p`, applies all patterns via `ReplaceAll`, holds the last 2048 bytes in the new `w.tail`, and flushes the rest to dst. `Close()` drains the remaining tail through a final redaction pass. Returns `len(p)` always (io.Writer contract honored even when fewer bytes are flushed immediately).

The Pitfall A defense is tested explicitly by `TestRedactingWriter_RedactsTokenSplitAcrossWrites`: the full Anthropic key `sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV` is split across two Write calls; the combined output contains `[REDACTED]` and no key fragment.

### caps (Task 2)

`internal/harness/caps.go` — `CapHitError{Reason string}` with `errors.As` support. `CheckCaps(caps, usage)` checks iterations, input-tokens, output-tokens in priority order; returns nil if all are zero (zero = unconstrained). Wall-clock enforcement is intentionally absent here — it lives in `Harness.Run` via `context.WithTimeout`.

### output-path validation (Task 3)

`internal/harness/outputs.go` — `Validate(workspaceRoot, runStart, declared []string)` walks `workspaceRoot` via `filepath.WalkDir`. For each non-directory file with `ModTime() >= runStart`: resolves symlinks via `filepath.EvalSymlinks`, then checks `filepath.Rel(declaredAbs, real)` — any result with `../` prefix or no match is a violation. Pre-resolves declared paths with `EvalSymlinks` too (falls back to abs form for not-yet-existing paths per D-G2).

The symlink-out-of-scope test (`TestValidate_RejectsSymlinkToOutOfScope`) is the load-bearing Pitfall-7 defense: a symlink inside `artifacts/M-001/P-001/L-001/` pointing to `escape/target.txt` is flagged because `EvalSymlinks` resolves to the escape dir, which is outside the declared scope.

### harness orchestrator (Task 4)

`internal/harness/harness.go` — `Runtime` interface (HARN-06 seam): `Execute(ctx, EnvelopeIn, stdout, stderr io.Writer) (Usage, error)`. `Harness` struct: `Envelope`, `Workspace`, `Runtime`, `StdoutDest`, `StderrDest`, `StartedAt`. `Run(ctx)` flow:

1. Set `StartedAt` if zero.
2. `context.WithTimeout(ctx, WallClockSeconds)` if cap > 0.
3. `Runtime.Execute(capsCtx, ...)`.
4. `context.DeadlineExceeded` → `cap-hit/wall-clock/exit=1`.
5. Non-deadline error → `error/msg/exit=1`.
6. `CheckCaps` → `cap-hit/{reason}/exit=1`.
7. `Validate` violations → `output-paths-violation/listing/exit=1`.
8. Success → `success/""/exit=0`.

`internal/harness/envelope_io.go` — `ReadEnvelopeIn`: open + decode + `ValidateAPIVersionKind` (T-02-06-04 mitigate). `WriteEnvelopeOut`: `os.MkdirAll` ancestor dirs + `json.Marshal` + `os.WriteFile` (D-G2 lazy mkdir). `WriteEnvelopeIn` added as a helper enabling round-trip testing and future orchestrator use.

## Phase 3 Swap Path (HARN-06)

The `Runtime` interface is the seam. Phase 2's stub-subagent binary implements it (conceptually — in Phase 2, the K8s Job runs stub-subagent directly as its own entrypoint, not wrapped by the harness). Phase 3's swap: the Job spec's container `command` changes from `stub-subagent` to `harness`; the harness binary populates its `Runtime` field with a `ClaudeCodeRuntime` (which calls `claude -p ... --output-format stream-json`). No harness code changes required.

## Wall-Clock Enforcement Path

Two layers (defense-in-depth per CONTEXT.md):
1. **Harness layer**: `context.WithTimeout(ctx, WallClockSeconds*time.Second)` → passed as `capsCtx` to `Runtime.Execute`. If runtime respects cancellation and returns `context.DeadlineExceeded`, harness writes `cap-hit/wall-clock`. Activated in Phase 3 when harness becomes the container entrypoint.
2. **K8s layer**: `Job.spec.activeDeadlineSeconds = WallClockSeconds + 30s` (grace window) set by Plan 08's PodJobBackend. Activates in Phase 2 already — if the Pod runs past the deadline, kubelet SIGTERMs then SIGKILLs.

## Output-Path Violation Reporting

`Validate` returns `[]string` of absolute resolved paths of violating writes. `Harness.Run` joins them with `"; "` as the `Reason` field of `EnvelopeOut`. The controller's `handleJobCompletion` (Plan 09) reads `EnvelopeOut.Result == "output-paths-violation"` and sets `Task.Status.Phase = Failed` with the paths in `Task.Status.Conditions[].Message`.

In Phase 2, `outputs.Validate` is called from the controller side (Plan 09 `handleJobCompletion`) because the harness is not yet the container entrypoint. The library ships the full API now; the invocation site changes in Phase 3.

## Stub vs Harness Execution Split

- **Phase 2 integration tests (kind Layer B)**: K8s Job runs `stub-subagent` binary directly as the container command. The stub reads `EnvelopeIn` from PVC, writes `EnvelopeOut` to PVC, exits. Controller's `handleJobCompletion` reads the out-envelope. `outputs.Validate` is called controller-side.
- **Phase 3 production path**: K8s Job runs `harness` binary. Harness reads `EnvelopeIn`, populates `Harness{Runtime: ClaudeCodeRuntime{...}}`, calls `h.Run()`, writes `EnvelopeOut` to PVC via `WriteEnvelopeOut`. The harness owns redaction, wall-clock, caps, and output-path validation entirely in-pod.

Plan 12 wires the image selection: `Project.Spec.subagentImage` selects between `stub-subagent` and `harness` per-Project.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Anthropic key regex requires {20,} suffix chars**
- **Found during:** Task 1 implementation (GREEN phase test run)
- **Issue:** Test fixture used `sk-ant-api03-aBcDeFgHiJkLmNoPqRs` (19 char suffix) which the `{20,}` quantifier does not match.
- **Fix:** Updated test key to `sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV` (21 chars); split-token test uses matching split of same key.
- **Files modified:** `internal/harness/redact/redact_test.go`
- **Note:** The regex pattern itself is correct per RESEARCH.md spec (20+ chars); the test fixture was too short.

**2. [Rule 1 - Bug] fakeRuntime.Execute used unnamed interface instead of io.Writer**
- **Found during:** Task 4 implementation (GREEN phase test run)
- **Issue:** `fakeRuntime.Execute(_, _, interface{Write([]byte)(int,error)}, ...)` does not satisfy `Runtime.Execute(..., io.Writer, io.Writer)` even though they are structurally equivalent — Go's interface satisfaction requires the named type.
- **Fix:** Changed test-file signature to use `io.Writer` directly.
- **Files modified:** `internal/harness/harness_test.go`

**3. [Rule 2 - Missing] WriteEnvelopeIn added to envelope_io.go**
- **Found during:** Task 4 test writing
- **Issue:** Round-trip test `TestReadEnvelopeIn_RoundTrip` needs to write a fixture before reading it back. Without `WriteEnvelopeIn`, the test must hand-craft JSON strings (fragile).
- **Fix:** Added `WriteEnvelopeIn(path, env)` alongside `ReadEnvelopeIn` and `WriteEnvelopeOut`. This function is also a natural future helper for the controller writing envelopes to the PVC.
- **Files modified:** `internal/harness/envelope_io.go`, `internal/harness/envelope_io_test.go`

## Self-Check: PASSED
