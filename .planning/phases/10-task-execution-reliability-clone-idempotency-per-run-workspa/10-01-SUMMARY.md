---
phase: 10-task-execution-reliability-clone-idempotency-per-run-workspa
plan: "01"
subsystem: pkg/git
tags: [git, clone, idempotency, tdd, reliability]

dependency_graph:
  requires: []
  provides:
    - pkg/git.Clone idempotent on ErrRepositoryAlreadyExists
    - pkg/git.Clone anonymous path (empty PAT skips Fetch)
  affects:
    - cmd/tide-push/main.go (Clone caller — unmodified, benefits automatically)

tech_stack:
  added: []
  patterns:
    - errors.Is sentinel detection for go-git ErrRepositoryAlreadyExists
    - PlainOpen + conditional Fetch as idempotent retry path

key_files:
  created: []
  modified:
    - pkg/git/clone.go
    - pkg/git/clone_test.go

decisions:
  - "Empty-PAT path skips Fetch entirely — avoids accidental BasicAuth header emission to anonymous servers (RESEARCH Pitfall 1); consistent with plan action step 2b."
  - "Fetch not called on first-clone success path — no behavior change for warm happy path."
  - "errors import added to clone.go; fetch.go unchanged (already handles NoErrAlreadyUpToDate)."

metrics:
  duration: "1 minute"
  completed_date: "2026-06-09"
  tasks_completed: 1
  files_modified: 2
---

# Phase 10 Plan 01: Clone Idempotency (SC-1) Summary

Idempotent `pkg/git.Clone` using `errors.Is(err, gogit.ErrRepositoryAlreadyExists)` sentinel detection — opens the existing bare repo and fetches instead of returning an error, eliminating clone Job retry failures on warm PVCs.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Add failing TestCloneIdempotent + TestFetchAnonymous | 83ada3f | pkg/git/clone_test.go |
| 1 (GREEN) | Implement idempotent Clone | 856258b | pkg/git/clone.go |

## What Was Built

`pkg/git/Clone` now handles `gogit.ErrRepositoryAlreadyExists` by:
1. Calling `gogit.PlainOpen(destDir)` to open the existing bare repo.
2. If `pat == ""` (anonymous remote): returning the repo directly — no `Fetch` call, no `BasicAuth` header emitted.
3. If `pat != ""`: calling `Fetch(ctx, repo, pat)` to refresh from remote, then returning.

All other errors still propagate as `fmt.Errorf("git clone %s: %w", repoURL, err)`.

The `errors` package was added to `clone.go` imports. `fetch.go` is unchanged — it already handles `NoErrAlreadyUpToDate` transparently.

## TDD Gate Compliance

RED gate (test commit) `83ada3f` preceded GREEN gate (feat commit) `856258b` in git log. Both gates satisfied.

- RED: `TestCloneIdempotent` failed with `"repository already exists"` (confirmed before implementation).
- GREEN: All three tests (`TestCloneIdempotent`, `TestFetchAnonymous`, `TestCloneSucceeds`) pass.
- Full `./pkg/git/...` suite green; `go vet ./pkg/git/...` clean.

## Deviations from Plan

None — plan executed exactly as written.

`TestFetchAnonymous` passed even before the implementation (file:// transport ignores auth). This is correct behavior — the test documents and guards the empty-PAT contract, not a bug in the existing code. The RED gate was satisfied by `TestCloneIdempotent` failing, which is the actual defect being fixed.

## Known Stubs

None.

## Threat Flags

No new network endpoints, auth paths, or trust-boundary surfaces introduced. The empty-PAT anonymous path explicitly prevents BasicAuth header emission (T-10-01-B mitigation from plan threat model).

## Self-Check

- [x] `pkg/git/clone.go` modified — exists at worktree path
- [x] `pkg/git/clone_test.go` modified — exists at worktree path
- [x] RED commit `83ada3f` — confirmed in git log
- [x] GREEN commit `856258b` — confirmed in git log
- [x] `grep -c "ErrRepositoryAlreadyExists" pkg/git/clone.go` returns 1
- [x] `go test ./pkg/git/... -count=1` exits 0
- [x] `go vet ./pkg/git/...` clean

## Self-Check: PASSED
