---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 11
subsystem: dashboard-api
tags: [dashboard, artifacts, git-auth, gap-closure, DASH-01]
gap_closure: true
requires:
  - cmd/dashboard/api/artifacts.go (resolveAuth, pre-existing)
  - cmd/tide-push/main.go resolveGitAuth (requirePAT rule, mirrored)
provides:
  - "resolveAuth: scheme-conditional empty-PAT handling (http:// anonymous, https://|git@ required)"
affects:
  - GET /api/v1/nodes/{kind}/{name}/artifacts
tech-stack:
  added: []
  patterns:
    - "Scheme-gated auth relaxation mirroring the push path (exact-prefix https://|git@ requires PAT)"
key-files:
  created: []
  modified:
    - cmd/dashboard/api/artifacts.go
    - cmd/dashboard/api/artifacts_test.go
decisions:
  - "Mirror tide-push resolveGitAuth's requirePAT prefix rule verbatim rather than invent a new gate — keeps read path and push path in lockstep"
  - "Missing-Secret path stays a loud error for every scheme; only the missing/empty GIT_PAT key is scheme-relaxed"
metrics:
  duration: ~8m
  completed: 2026-07-09
  tasks: 1
  files: 2
status: complete
---

# Phase 37 Plan 11: Anonymous http:// Artifact Auth (Gap 37-G1) Summary

Scheme-conditional `resolveAuth` — the dashboard artifact view now fetches anonymously (nil Auth) for `http://` remotes with an empty/absent `GIT_PAT`, while `https://`/`git@` remotes still require the PAT, mirroring `cmd/tide-push` `resolveGitAuth`.

## What Changed

`resolveAuth` in `cmd/dashboard/api/artifacts.go` gained a `repoURL string` parameter and, in the missing/empty-PAT branch (`!ok || len(pat) == 0`), now computes the same `requirePAT := strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "git@")` guard the push path uses. When `!requirePAT` (anonymous `http://`) it returns `(nil, "")` so the fetch proceeds anonymously; when `requirePAT` it returns the unchanged missing-data-key error. The single call site in `Get` now passes `proj.Spec.Git.RepoURL` (already in scope). The empty-`credsSecretRef` early return and the secret-Get-failure error path are untouched.

This closes Gap 37-G1, live-repro'd during the 37-10 UAT: an in-cluster anonymous http remote pushed fine but the artifact panel stayed `state:"error"` (`missing data key GIT_PAT`) until a dummy PAT was set.

## Tasks Completed

| Task | Name | Commits | Files |
| ---- | ---- | ------- | ----- |
| 1 | Scheme-conditional resolveAuth (TDD) | `9df7ee1` (test/RED), `9ac856b` (fix/GREEN) | cmd/dashboard/api/artifacts.go, cmd/dashboard/api/artifacts_test.go |

TDD cycle: RED commit added `httpGitProject()`, `emptyCredsSecret()`, `noKeyCredsSecret()` fixtures, extended `fakeFetcher` to record the handed `*gitfetch.Auth`, and three failing/guard tests. Two http:// cases failed as expected (`state:"error"` instead of `available`); the https:// guard already passed. GREEN commit made all green. No REFACTOR needed — the change is minimal.

## Verification

Command (plan's own verification): `go test ./cmd/dashboard/api/ -run TestArtifacts`

Result: PASS, exit 0. All 8 test functions green:
- New: `TestArtifactsHTTPEmptyPATAvailable` (asserts `state:available` AND recorded Fetch auth is `nil`), `TestArtifactsHTTPNoKeyPATAvailable`, `TestArtifactsHTTPSEmptyPATError` (asserts `state:error` + message names `GIT_PAT`).
- Pre-existing: `TestArtifactsNoGit`, `TestArtifactsAvailable`, `TestArtifactsAbsent`, `TestArtifactsError`, `TestArtifactsValidation` — all still green.

Supplementary: `go vet ./cmd/dashboard/api/` exit 0. `grep -c 'requirePAT' cmd/dashboard/api/artifacts.go` = 3 (>= 1). `grep -c 'repoURL string' cmd/dashboard/api/artifacts.go` = 1 (>= 1).

## Must-Haves Status

- http:// anonymous + empty/absent GIT_PAT → `state:"available"`, nil Auth: MET (Tests 1-3, nil-auth assertion via recorded Fetch arg).
- https://|git@ + empty/absent GIT_PAT → `state:"error"`, PAT requirement preserved: MET (Test 4, scheme-gated regression guard).
- PAT value never echoed in any error surface: MET (unchanged error strings; `TestArtifactsError` patSentinel-not-in-body assertion still green).

## Threat Model Status

- T-37-11-01 (auth downgrade): mitigated — exact-prefix scheme gate; `TestArtifactsHTTPSEmptyPATError` proves no authenticated remote is silently downgraded.
- T-37-11-02 (PAT disclosure): mitigated — no new credential logging; error strings carry only Secret NAME + missing-key reason.

## Deviations from Plan

None — plan executed exactly as written.

## Self-Check: PASSED

- FOUND: cmd/dashboard/api/artifacts.go (modified, requirePAT guard present)
- FOUND: cmd/dashboard/api/artifacts_test.go (modified, 3 new tests)
- FOUND: commit 9df7ee1 (test/RED)
- FOUND: commit 9ac856b (fix/GREEN)
- Verification `go test ./cmd/dashboard/api/ -run TestArtifacts` exit 0
