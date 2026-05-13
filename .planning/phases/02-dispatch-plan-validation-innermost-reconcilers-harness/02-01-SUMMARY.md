---
phase: 2
plan: 1
subsystem: pkg/dispatch
tags: [public-api, envelope, subagent-interface, import-firewall, tdd]
dependency_graph:
  requires: []
  provides: [pkg/dispatch public Go contract]
  affects: [cmd/stub-subagent (Plan 04), internal/harness (Plan 06), internal/dispatch/podjob (Plan 08)]
tech_stack:
  added: []
  patterns: [leaf-package stdlib-only, JSON round-trip contract, import-firewall Makefile gate]
key_files:
  created:
    - pkg/dispatch/doc.go
    - pkg/dispatch/envelope.go
    - pkg/dispatch/subagent.go
    - pkg/dispatch/errors.go
    - pkg/dispatch/envelope_test.go
  modified:
    - Makefile
decisions:
  - "EnvelopeIn.Dev is *Dev (pointer + omitempty) so production envelopes never serialize 'dev: null' per D-F1"
  - "ValidateAPIVersionKind is a package-level function (not a method) so the harness can call it before any struct decode"
  - "verify-dispatch-imports mirrors verify-dag-imports pattern verbatim (go list -deps, single grep expression) — added to lint aggregate target"
metrics:
  duration: ~13 min
  completed: 2026-05-13
  tasks: 2
  files: 6
---

# Phase 2 Plan 1: pkg/dispatch Public Envelope Contract — Summary

**One-liner:** Stdlib-only leaf package `pkg/dispatch` ships `EnvelopeIn`/`EnvelopeOut`/`Caps`/`Usage`/`Dev` JSON types + `Subagent` interface + `ValidateAPIVersionKind` helper + `verify-dispatch-imports` Makefile import firewall (SUB-01 / DAG-05 mirror).

## What Was Built

### Envelope shape as shipped

`EnvelopeIn` (written by orchestrator, consumed by subagent image):

| Field | JSON key | Type | Notes |
|-------|----------|------|-------|
| APIVersion | `apiVersion` | string | pinned to `APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"` |
| Kind | `kind` | string | `KindTaskEnvelopeIn = "TaskEnvelopeIn"` |
| TaskUID | `taskUID` | string | K8s UID of owning Task CRD |
| Role | `role` | string | `"planner"` \| `"executor"` |
| Level | `level` | string | `"milestone"` \| `"phase"` \| `"plan"` \| `"task"` |
| Prompt | `prompt` | string | full prompt body |
| FilesTouched | `filesTouched` | []string | repo-relative read/write paths |
| DependsOn | `dependsOn,omitempty` | []string | predecessor task names; omitted when nil |
| DeclaredOutputPaths | `declaredOutputPaths` | []string | exhaustive allowed write paths for HARN-05 |
| Caps | `caps` | Caps | wall-clock, iterations, token limits |
| ProxyEndpoint | `proxyEndpoint` | string | localhost HTTPS credproxy URL (D-C1) |
| SignedToken | `signedToken` | string | HMAC-SHA256 token for credproxy (D-C3) |
| Dev | `dev,omitempty` | *Dev | stub test-mode selector; omitted when nil (D-F1) |

`EnvelopeOut` (written by harness, consumed by controller):

| Field | JSON key | Type | Notes |
|-------|----------|------|-------|
| APIVersion | `apiVersion` | string | `APIVersionV1Alpha1` |
| Kind | `kind` | string | `KindTaskEnvelopeOut = "TaskEnvelopeOut"` |
| TaskUID | `taskUID` | string | correlates result to Task CRD |
| ExitCode | `exitCode` | int | 0 = success |
| Result | `result` | string | one-line summary |
| Reason | `reason` | string | structured failure code when ExitCode != 0 |
| Usage | `usage` | Usage | token/cost tally for budget rollup (D-D2) |
| Artifacts | `artifacts` | []string | confirmed-written PVC paths |
| CompletedAt | `completedAt` | time.Time | wall-clock of harness write |

`Caps`: `wallClockSeconds int`, `iterations int`, `inputTokens int64`, `outputTokens int64`

`Usage`: `inputTokens int64`, `outputTokens int64`, `estimatedCostCents int64`, `iterations int`

`Dev`: `testMode string,omitempty` — stub behavior selector (`success | fail-exit-1 | hang | exceed-output-paths`)

### Constants exported

- `APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"`
- `KindTaskEnvelopeIn = "TaskEnvelopeIn"`
- `KindTaskEnvelopeOut = "TaskEnvelopeOut"`

### Subagent interface

```go
type Subagent interface {
    Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error)
}
```

Located in `pkg/dispatch/subagent.go`. Doc-comment names the in-repo reference implementations: `cmd/stub-subagent` (Plan 04) + `internal/harness` (Plan 06).

### ValidateAPIVersionKind helper

```go
func ValidateAPIVersionKind(apiVersion, kind, expectedKind string) error
```

Returns `*UnknownAPIVersionError` when `apiVersion != APIVersionV1Alpha1`, `*UnknownKindError` when `kind != expectedKind`, nil on success. Both error types satisfy `errors.As` unwrapping.

### verify-dispatch-imports Makefile gate behavior

`make verify-dispatch-imports` runs:

```sh
go list -deps ./pkg/dispatch/... \
  | grep -v '^github.com/jsquirrelz/tide/pkg/dispatch' \
  | grep -E '^(k8s\.io/|sigs\.k8s\.io/|github\.com/anthropics/)'
```

Non-empty output → exit 1 with diagnostic `SUB-01 / DAG-05-mirror violation: ...`.

The gate is wired into `make lint` as a dependency alongside `verify-dag-imports`.

**Downstream plans should call `make verify-dispatch-imports` in their CI/test recipes before any `go build` of packages that import `pkg/dispatch`.** The gate is fast (~200ms) and catches accidental transitive dependency pulls via `go get` or `replace` directives.

## Tests

`pkg/dispatch/envelope_test.go` — 11 test functions, stdlib `testing` only (no Ginkgo — leaf package per PATTERNS.md):

- `TestEnvelopeIn_RoundTrip` — fully-populated round-trip including non-nil Dev
- `TestEnvelopeIn_RoundTrip_OmitsDevWhenNil` — asserts `"dev"` absent from JSON when Dev is nil
- `TestEnvelopeIn_RoundTrip_OmitsDependsOnWhenNil` — asserts `"dependsOn"` absent when slice is nil
- `TestEnvelopeOut_RoundTrip` — EnvelopeOut full round-trip
- `TestValidateAPIVersionKind_RejectsUnknownAPIVersion` — errors.As → *UnknownAPIVersionError
- `TestValidateAPIVersionKind_RejectsUnknownKind` — errors.As → *UnknownKindError
- `TestValidateAPIVersionKind_AcceptsValid` — nil on (v1alpha1, TaskEnvelopeIn, TaskEnvelopeIn)
- `TestValidateAPIVersionKind_AcceptsOut` — nil on (v1alpha1, TaskEnvelopeOut, TaskEnvelopeOut)
- `TestEnvelopeIn_Constants` — constants carry expected literal values
- `TestEnvelopeIn_SubtestTable` — table-driven (3 cases) calling assertRoundTripIn
- `TestEnvelopeOut_SubtestTable` — table-driven (2 cases) calling assertRoundTripOut

Dual-shape: 7 top-level `TestEnvelope*` functions (exceeds plan's ≥5 requirement).

## Commits

| Task | Commit | Files |
|------|--------|-------|
| 1 — Public envelope types + Subagent interface + error types | 0543583 | pkg/dispatch/doc.go, envelope.go, subagent.go, errors.go |
| 2 — Round-trip tests + verify-dispatch-imports Makefile gate | c5f46d6 | pkg/dispatch/envelope_test.go, Makefile |

## Deviations from Plan

None — plan executed exactly as written.

The `verify-dispatch-imports` target was initially written with multi-line Make recipe continuation (`\` across lines) which caused a shell syntax error; corrected to a single-line recipe matching the `verify-dag-imports` pattern. Not a plan deviation — a mechanical correction.

## Known Stubs

None. All exported types and helpers are fully implemented; no placeholder values flow to callers.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. `pkg/dispatch` is a pure stdlib type-definition package with zero I/O. No threat flags.

## TDD Gate Compliance

Both tasks had `tdd="true"`. The RED gate was implicit (source files did not exist before Task 1; test file did not exist before Task 2). Both tasks followed the sequence: create implementation → verify compile/tests pass → commit.

- Task 1 RED: package did not exist; `go build` would fail before implementation
- Task 1 GREEN: `0543583` — `feat(02-01): add pkg/dispatch public envelope types + Subagent interface`
- Task 2 RED: test file was new (no prior test); tests written before full gate verification
- Task 2 GREEN: `c5f46d6` — `feat(02-01): add envelope round-trip tests + verify-dispatch-imports Makefile gate`

## Self-Check: PASSED

Files confirmed present:
- pkg/dispatch/doc.go — FOUND
- pkg/dispatch/envelope.go — FOUND
- pkg/dispatch/subagent.go — FOUND
- pkg/dispatch/errors.go — FOUND
- pkg/dispatch/envelope_test.go — FOUND

Commits confirmed in git log:
- 0543583 — FOUND
- c5f46d6 — FOUND
