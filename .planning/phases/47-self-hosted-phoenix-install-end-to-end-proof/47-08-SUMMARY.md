---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 08
subsystem: infra
tags: [go-git, force-with-lease, tide-push, run-integrity, gap-closure]

# Dependency graph
requires:
  - phase: 47-04
    provides: "the boundary-push retry / bypass-annotation re-dispatch paths that re-assert Status.Git.LastPushedSHA"
provides:
  - "pkg/git.RemoteBranchTip — generic authenticated remote-tip read primitive"
  - "cmd/tide-push effective-lease derivation with an ancestry guard before every pkggit.Push call"
  - "regression proof that a stale --last-pushed-sha retried against an already-integrated remote advance self-heals, while external divergence still fails closed"
affects: [47-EVIDENCE, 47-06, 47-07, project_controller.go boundary-push reconcile loop]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Read-then-guard-then-write at the git binary boundary: re-read authoritative remote state inside the same process that performs the write, instead of trusting a caller-supplied anchor that can go stale between dispatch and execution."

key-files:
  created:
    - pkg/git/remote.go
    - pkg/git/remote_test.go
  modified:
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go

key-decisions:
  - "Ancestry-guard refresh policy lives entirely in cmd/tide-push, not pkg/git — RemoteBranchTip stays a policy-free read primitive matching Push/Fetch's own doc-comment convention (pkg/git ships generic primitives; caller policy lives at cmd/tide-push)."
  - "Remote-read failures and ancestry-check errors both conservatively fall back to the caller-supplied cfg.LastPushedSHA unchanged, rather than inventing a new failure mode — any real transport problem still surfaces through the existing classifyPushError path with no new envelope reason strings."
  - "One mechanism (re-read inside every push attempt) closes the gap for both the controller's bounded retries and the bypass-annotation re-dispatch path, so no project_controller.go change is needed or permitted (that file is owned by plans 47-06/47-07 in this wave set)."

patterns-established:
  - "Effective-lease derivation as a single pre-push chokepoint (deriveEffectiveLease) — every future push-mode code path that reaches pkggit.Push automatically inherits the refresh/fail-closed behavior."

requirements-completed: [PROOF-01]

# Metrics
duration: ~20min
completed: 2026-07-17
---

# Phase 47 Plan 08: Self-Refreshing Push Lease Summary

**tide-push now re-reads the actual remote branch tip via a new `pkg/git.RemoteBranchTip` primitive immediately before every `--force-with-lease` push, refreshing a stale anchor when the advance is already-integrated local history and still failing closed (exit 11 / lease-rejected) on genuine external divergence.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-17
- **Tasks:** 2/2
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- Closed verification gap #3 (47-EVIDENCE §6.1 Defect #1): `Status.Git.LastPushedSHA` is re-asserted verbatim on every retry today, so a wave-level push that already advanced the run branch makes every subsequent boundary-push retry fail identically — flapping `medium-project.status.phase` forever.
- Root-fixed at the binary (`cmd/tide-push`), not the controller: the fix runs inside every push attempt, so both the controller's bounded retries AND the bypass-annotation re-dispatch path (which clears state without refreshing the lease) are covered by one mechanism.
- D-B6's protection against overwriting external/manual work on the run branch is provably intact: a remote advance whose commit is absent from the local object DB, or present but not an ancestor of local HEAD, still fails closed on the exact same `exitLeaseFailed` / `"lease-rejected"` contract the controller already classifies on.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add pkg/git RemoteBranchTip primitive with unit tests** - `5c3e0c0` (feat)
2. **Task 2: Derive the effective lease with an ancestry guard in tide-push, plus regression tests** - `091350e` (fix)

_Note: no plan-metadata commit is created in worktree mode — the orchestrator commits SUMMARY.md/REQUIREMENTS.md after merge._

## Files Created/Modified

- `pkg/git/remote.go` - `RemoteBranchTip(ctx, repo, branch, pat) (plumbing.Hash, bool, error)`: authenticated `origin` ref listing via the same x-access-token BasicAuth shape as `Push`, returning the branch's tip hash with `found=true`, or `(ZeroHash, false, nil)` when the ref is absent. Policy-free per pkg/git's Push/Fetch convention.
- `pkg/git/remote_test.go` - Mirrors `fetch_test.go`'s local-bare-repo harness: present-branch, absent-branch, and invalid-input (nil repo / empty branch) cases against `seedBareRepo`/`defaultBranchOf` from `clone_test.go`.
- `cmd/tide-push/main.go` - New `deriveEffectiveLease` helper, called immediately before the `pkggit.Push` call in `runPush`. Re-reads the remote tip via `RemoteBranchTip`; on a differing tip, resolves `repo.CommitObject(remoteTip)` and checks `remoteTipCommit.IsAncestor(headCommit)` — ancestor (already-integrated) refreshes the lease to the observed tip, non-ancestor or missing-object writes the `lease-rejected` envelope and returns `exitLeaseFailed` without pushing. Read or ancestry-check errors fall back to `cfg.LastPushedSHA` unchanged.
- `cmd/tide-push/main_test.go` - Two new regression tests: `TestRunPushModeRefreshesStaleLeaseAfterIntegratedRemoteAdvance` (three sequential pushes from the same workspace — push 3 retries push 1's now-stale lease after push 2 already advanced the remote, and must still land) and `TestRunPushModeRejectsExternalRemoteAdvance` (the remote is advanced by a separate go-git clone/push so the commit never enters the workspace's object DB — a stale-lease retry must exit 11 with reason `lease-rejected` and the external commit must survive untouched).

## Decisions Made

- Kept the ancestry-guard POLICY entirely at `cmd/tide-push` per the plan's context note — `pkg/git` gains only the read primitive, matching the existing Push/Fetch policy-split doc comments.
- Applied the derivation uniformly, including when `cfg.LastPushedSHA` is empty: an existing remote ref whose tip is an ancestor of local HEAD gets a refreshed race-guarded lease (rather than Push's natural "omit the lease" first-push semantics), while a divergent existing ref hits the same `lease-rejected` outcome the natural non-fast-forward rejection already produced. This only changes behavior when a branch already has remote commits at the moment of what the caller believed was a first push — the existing `TestRunPushModeCleanFirstPush`/`TestRunPushModeGitleaksBlocksAnthropicKey` tests (both against branches with no prior remote ref) remain byte-identical and pass unmodified.

## Deviations from Plan

None - plan executed exactly as written. The two tasks matched the plan's `<action>` and `<acceptance_criteria>` blocks precisely; no Rule 1-4 auto-fixes were needed.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Gap #3 is closed at the root: `go test ./pkg/git/ ./cmd/tide-push/ -count=1` is fully green (including both new regression tests and all pre-existing lease/first-push/leak/scheme-guard tests).
- `git diff --name-only` for this plan touches only the four `files_modified` paths — `project_controller.go` is untouched, preserving the ownership boundary with plans 47-06/47-07 in this wave set.
- No new envelope reason strings were introduced (`grep` for `-rejected"`/`remote-read` patterns across `main.go` yields only the pre-existing `"lease-rejected"`), so the controller's existing exit-11 classification arm needs no change to consume this fix.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*

## Self-Check: PASSED

All created/modified files verified present (pkg/git/remote.go, pkg/git/remote_test.go, cmd/tide-push/main.go, cmd/tide-push/main_test.go, this SUMMARY.md); all three task/summary commits (`5c3e0c0`, `091350e`, `f5aa985`) verified present in `git log`.
