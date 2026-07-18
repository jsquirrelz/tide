---
phase: 48-langgraph-evaluator-image-credproxy-tls-spike
plan: 02
subsystem: infra
tags: [k8s, jobspec, security, readonly-mount, credentials, tdd]

# Dependency graph
requires:
  - phase: 48-01
    provides: cmd/tide-langgraph-verifier Python scaffold + pinned deps (unrelated surface; no code dependency, same phase)
provides:
  - "ReadOnly bool field on internal/dispatch/podjob/BuildOptions"
  - "Boolean-gated branch in BuildJobSpec: /workspace ReadOnly mount + verifier-scratch emptyDir at /scratch + ReadOnlyRootFilesystem true"
  - "Regression test proving git-write/push credentials never appear in any BuildJobSpec container, regardless of ReadOnly"
  - "VolumeVerifierScratch exported const (\"verifier-scratch\")"
affects: [phase-51-task-loop-dispatch]

# Tech tracking
tech-stack:
  added: []
  patterns: ["single-field boolean-gated branch extension of an existing builder function, mirroring the credproxyEnabled idiom already in jobspec.go"]

key-files:
  created:
    - internal/dispatch/podjob/jobspec_readonly_test.go
  modified:
    - internal/dispatch/podjob/jobspec.go

key-decisions:
  - "Implemented D-08 as a single ReadOnly bool field on the existing BuildOptions/BuildJobSpec, per RESEARCH Pattern 2 — no forked buildVerifierJobSpec, no JobKindVerifier const."
  - "Credential omission proven as a regression TEST (TestBuildJobSpec_Verifier_NoGitCredsInAnyContainer), not new omission logic — git-write/push creds were already isolated to the separate tide-push Job (push_helpers.go) before this plan."
  - "Added an exported VolumeVerifierScratch const rather than an inline string literal, so the test file and jobspec.go share one source of truth for the volume/mount name."
  - "Field doc comment carries the Phase-51 forward-note: envelope write-back (/workspace/envelopes/<uid>/out.json) will need a separate read-write envelopes/ subPath mount, since out.json cannot be written through a ReadOnly /workspace mount — explicitly deferred to Phase 51's dispatch wiring."

patterns-established:
  - "D-08 read-only jobspec variant: ReadOnly bool → subagent /workspace mount ReadOnly + verifier-scratch emptyDir/scratch mount + ReadOnlyRootFilesystem true, following the same boolean-branch idiom as credproxyEnabled."

requirements-completed: [EVAL-01]

# Metrics
duration: 8min
completed: 2026-07-18
---

# Phase 48 Plan 02: Read-Only Verifier Jobspec Variant Summary

**Added a single `ReadOnly bool` field to `BuildOptions`/`BuildJobSpec` that structurally enforces D-08's read-only verifier contract (ReadOnly mount, scratch emptyDir, ReadOnlyRootFilesystem) and proves — via a new regression test — that git-write/push credentials never reach any Job this function builds, unit-tested but not yet dispatched by any reconciler.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-07-18T14:24:00-04:00
- **Completed:** 2026-07-18T14:27:03-04:00
- **Tasks:** 2/2 completed
- **Files modified:** 2 (1 created, 1 modified)

## Accomplishments
- Authored 5 `TestBuildJobSpec_Verifier_*` assertions against the not-yet-existing `ReadOnly` field, confirmed RED via a genuine compile failure.
- Implemented the `ReadOnly bool` field + boolean-gated branch in `BuildJobSpec`, mirroring the existing `credproxyEnabled` idiom exactly (RESEARCH Pattern 2) — all 5 new assertions plus the full pre-existing `podjob` suite pass GREEN.
- Proved (not assumed) that git-write/push credentials are absent from every container built by `BuildJobSpec`, for both `ReadOnly: true` and `ReadOnly: false`, closing the D-08 credential-omission requirement as a pinned regression test.

## Task Commits

1. **Task 1: Write failing TestBuildJobSpec_Verifier_* assertions (RED)** - `04f6efb` (test)
2. **Task 2: Implement the ReadOnly BuildOptions field + branch (GREEN)** - `0ea76e8` (feat)

**Plan metadata:** (this commit, following SUMMARY.md write)

## Files Created/Modified
- `internal/dispatch/podjob/jobspec_readonly_test.go` - New `TestBuildJobSpec_Verifier_*` family: workspace-mount ReadOnly, verifier-scratch emptyDir + `/scratch` mount, `ReadOnlyRootFilesystem`, git-credential absence (both ReadOnly values), and the `ReadOnly:false`/zero-value non-regression path.
- `internal/dispatch/podjob/jobspec.go` - Added `VolumeVerifierScratch` const, `ReadOnly bool` field on `BuildOptions` (with the D-08 doc comment + Phase-51 envelopes-subPath forward-note), and the boolean-gated branch in `BuildJobSpec`: subagent `/workspace` mount `ReadOnly: opts.ReadOnly`, conditional `verifier-scratch` emptyDir volume + `/scratch` mount, and `ReadOnlyRootFilesystem: new(opts.ReadOnly)` (was hardcoded `new(false)`).

## Decisions Made
- Followed RESEARCH Pattern 2 exactly: one field, one function, boolean branch — confirmed no forked `BuildVerifierJobSpec` and no `JobKindVerifier` const were introduced (RESEARCH Open Question 2 resolution).
- Exported `VolumeVerifierScratch` as a package-level const (rather than leaving the volume name as an inline string) so the test file references the same identifier as the implementation — avoids string-literal drift between test and source.

## Deviations from Plan

None — plan executed exactly as written. Both tasks' acceptance criteria were verified directly:
- Task 1: `go test ./internal/dispatch/podjob/ -run TestBuildJobSpec_Verifier` failed with a compile error (`opts.ReadOnly undefined`) — RED confirmed; `grep -c 'func TestBuildJobSpec_Verifier'` = 5; the credential-absence test's `Spec.Git.CredsSecretRef` construction confirmed via grep.
- Task 2: `go test ./internal/dispatch/podjob/ -run TestBuildJobSpec_Verifier` and `go test ./internal/dispatch/podjob/...` both pass; `go build ./...` and `make verify-dispatch-imports` both clean; `git diff --stat pkg/dispatch/` is empty; no reconciler constructs `BuildOptions{ReadOnly: true}` (grep returns 0); the field's doc comment contains the `envelopes` forward-note (grep returns ≥1, at line 201-202).

## Verification

- `go test ./internal/dispatch/podjob/...` — PASS (14.7s, full suite including the 5 new + all pre-existing `TestBuildJobSpec_*` tests)
- `go build ./...` — clean
- `make verify-dispatch-imports` — `OK: pkg/dispatch imports are clean`
- `go vet ./internal/dispatch/podjob/...` — clean
- `git diff --stat pkg/dispatch/` — empty (seam untouched, as required)
- No reconciler dispatches the `ReadOnly` variant this phase (grep confirms 0 matches in `internal/controller/`)

## Self-Check

- `internal/dispatch/podjob/jobspec_readonly_test.go` — FOUND
- `internal/dispatch/podjob/jobspec.go` (modified, `ReadOnly` field present) — FOUND (`grep -c 'ReadOnly bool' jobspec.go` = 1)
- Commit `04f6efb` — FOUND in `git log --oneline`
- Commit `0ea76e8` — FOUND in `git log --oneline`

## Self-Check: PASSED
