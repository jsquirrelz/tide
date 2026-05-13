---
phase: 2
plan: 4
subsystem: stub-subagent
tags: [subagent, testing, docker, dispatch, envelope]
dependency_graph:
  requires: ["02-01"]
  provides: ["cmd/stub-subagent", "ghcr.io/jsquirrelz/tide-stub-subagent:test"]
  affects: ["02-06", "02-13"]
tech_stack:
  added: []
  patterns: ["multi-stage Docker build", "ctx-cancellation hang mode", "in-process testable run() helper"]
key_files:
  created:
    - cmd/stub-subagent/main.go
    - cmd/stub-subagent/main_test.go
    - images/stub-subagent/Dockerfile
    - images/stub-subagent/.dockerignore
  modified: []
decisions:
  - "Hang mode: ctx-cancellation (context.WithTimeout in tests, signal.NotifyContext in main) rather than real SIGTERM to avoid unreliable OS signal delivery in parallel test runs"
  - "Image base: gcr.io/distroless/static:nonroot over scratch — enforces USER 1000 cleanly across OCI runtimes without requiring /etc/passwd in scratch"
  - "No Apache header: matches cmd/tide-lint minimalism convention per 02-PATTERNS.md"
metrics:
  duration: "~12min"
  completed: "2026-05-12"
  tasks: 2
  files: 4
---

# Phase 2 Plan 4: Stub-Subagent Summary

**One-liner:** Static Go CLI image proving the pkg/dispatch EnvelopeIn/EnvelopeOut public contract with four deterministic test modes (success/fail-exit-1/hang/exceed-output-paths) and a 4.44 MB distroless/nonroot container image.

## What Was Built

### cmd/stub-subagent/main.go

The stub-subagent CLI (SUB-04 / D-F1..F3). Reads an EnvelopeIn JSON from `--envelope <path>` (or `$TIDE_ENVELOPE_PATH` env var fallback, then `/workspace/envelopes/$TIDE_TASK_UID/in.json`), validates via `pkgdispatch.ValidateAPIVersionKind`, dispatches on `Dev.TestMode`, writes an EnvelopeOut `out.json`, and exits with the appropriate code.

**Exit code semantics:**

| Code | Meaning |
|------|---------|
| 0 | Success (testMode=success or empty) |
| 1 | Task failure (testMode=fail-exit-1) |
| 2 | Envelope error — bad apiVersion/kind, parse failure, or unknown testMode |

**out.json shape per mode:**

| Mode | ExitCode | Result | Reason | Artifacts | Usage |
|------|----------|--------|--------|-----------|-------|
| `success` / `""` | 0 | `"success"` | `"stub testMode=success"` | `[<firstDeclaredPath>/result.txt]` | InputTokens=100, OutputTokens=200, Cost=1, Iter=1 |
| `fail-exit-1` | 1 | `"forced-failure"` | `"stub testMode=fail-exit-1"` | `[]` | all zeros |
| `hang` | (never returns normally) | — | — | out.json NOT written | — |
| `exceed-output-paths` | 0 | `"success"` | `"stub testMode=exceed-output-paths"` | `["/workspace/escape/leak.txt"]` | InputTokens=100, OutputTokens=100, Cost=1, Iter=1 |
| unknown mode | 2 | `"invalid-envelope"` | `"unknown testMode \"<mode>\""` | `[]` | all zeros |

**D-A1 public-contract proof:** `cmd/stub-subagent/main.go` imports `github.com/jsquirrelz/tide/pkg/dispatch` aliased as `pkgdispatch` and references `pkgdispatch.EnvelopeIn` in 8 locations. If the pkg/dispatch contract breaks, this file fails to compile — the earliest possible signal.

### Hang mode test strategy

Hang mode uses **ctx-cancellation** — the in-process `run()` helper accepts a `context.Context`; tests pass `context.WithTimeout(ctx, 200ms)`. In production, `main()` installs `signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)` so a real SIGTERM cancels the context and `dispatchHang()` returns 0.

This avoids sending real OS signals to the test process (which would kill all test goroutines on some platforms), while exercising the same code path that runs in the Job pod.

### Image

`images/stub-subagent/Dockerfile` — two-stage build:

- **Stage 1:** `golang:1.26-alpine` builder; `CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w"` produces a static Linux binary.
- **Stage 2:** `gcr.io/distroless/static:nonroot` runtime; `USER 1000` (D-G3 subagent UID); `ENTRYPOINT ["/usr/local/bin/stub-subagent"]`.

**Image base chosen:** `distroless/static:nonroot` over bare scratch. Distroless ships a minimal `/etc/passwd` so the container runtime enforces UID 1000 cleanly regardless of OCI runtime flavor. The stub makes zero outbound calls so no CA bundle is needed — distroless/static is adequate.

**Final image size:** 4.44 MB (well under the 20 MB target).

**Verification:**
- `docker run --rm ghcr.io/jsquirrelz/tide-stub-subagent:test --envelope /nonexistent` → exits 2 (envelope load failure path).
- `docker inspect ... --format '{{.Config.User}}'` → `1000`.

### Tests (cmd/stub-subagent/main_test.go)

8 stdlib subtests, all PASS:
- `TestStub_SuccessMode` — verifies exit 0, out.json shape, result.txt artifact written.
- `TestStub_SuccessMode_EmptyTestMode` — verifies nil Dev treated as success.
- `TestStub_FailExit1Mode` — verifies exit 1, ExitCode=1, Result="forced-failure".
- `TestStub_HangMode` — ctx-cancellation unblocks within 2s; out.json NOT written.
- `TestStub_ExceedOutputPathsMode` — exit 0; /workspace/escape/leak.txt in Artifacts.
- `TestStub_InvalidEnvelope_BadAPIVersion` — exit 2.
- `TestStub_InvalidEnvelope_MissingFile` — exit 2.
- `TestStub_UnknownTestMode` — exit 2.

## TDD Gate Compliance

- RED commit: `test(02-04): add failing tests for stub-subagent four test modes` (69932b0)
- GREEN commit: `feat(02-04): implement stub-subagent CLI with four test-mode dispatch` (8a4b619)

## Deviations from Plan

None — plan executed exactly as written. Hang mode test strategy (ctx-cancellation) was the recommended variant from the plan's `<action>` section.

## Threat Surface Scan

No new network endpoints, auth paths, or file access patterns introduced beyond what the plan's threat model covers (T-02-04-01 through T-02-04-05). The stub makes zero outbound calls. The exceed-output-paths mode writes to `/workspace/escape/leak.txt` intentionally as a HARN-05 violation generator — documented in the threat model.

## Self-Check

Files:
- [x] cmd/stub-subagent/main.go — FOUND
- [x] cmd/stub-subagent/main_test.go — FOUND
- [x] images/stub-subagent/Dockerfile — FOUND
- [x] images/stub-subagent/.dockerignore — FOUND

Commits:
- [x] 69932b0 — test(02-04): RED phase
- [x] 8a4b619 — feat(02-04): GREEN phase (main.go + .dockerignore)
- [x] 9670a61 — feat(02-04): Dockerfile

## Self-Check: PASSED
