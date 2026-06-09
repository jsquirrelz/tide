---
phase: "11"
plan: "01"
subsystem: executor-commit-lifecycle
tags: [harness, git, commit, worktree, executor, empty-diff, D-03, SC-2]
dependency_graph:
  requires: [B1-EnsureRunBranch, B2-AddWorktree]
  provides: [CommitWorktree, executor-commit-step]
  affects: [cmd/claude-subagent, internal/harness]
tech_stack:
  added: []
  patterns: [git-cli-over-go-git, test-seam-var, tdd-red-green]
key_files:
  created:
    - internal/harness/commit.go
    - internal/harness/commit_test.go
    - cmd/claude-subagent/commit_test.go
  modified:
    - cmd/claude-subagent/main.go
    - cmd/claude-subagent/main_test.go
decisions:
  - "CommitWorktree uses git CLI (not go-git) — consistent with AddWorktree precedent; avoids dubious-ownership issues with linked worktrees (per 11-RESEARCH Pitfall 8 + B2 precedent)"
  - "TIDE_BOT_NAME / TIDE_BOT_EMAIL env vars override hardcoded identity without a new Helm key (D-03)"
  - "Empty diff returns (ZeroHash, true, nil) — caller translates to ExitCode=1 Result=empty-diff (D-03 / SC-2)"
  - "commitWorktreeFunc var seam follows ensureWorktreeFunc pattern for test isolation"
metrics:
  duration: "~15min"
  completed: "2026-06-09"
  tasks: 2
  files: 5
---

# Phase 11 Plan 01: Executor Commit Step (B3) Summary

**One-liner:** CommitWorktree function in internal/harness commits per-task worktree changes under TIDE Bot identity via git CLI; empty diff surfaces as explicit ExitCode=1 failure wired through claude-subagent run() executor path.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | CommitWorktree failing tests | 1745c84 | internal/harness/commit_test.go |
| 1 (GREEN) | CommitWorktree implementation | 04524f0 | internal/harness/commit.go |
| 2 (RED) | Executor commit-step wiring tests | 4fc0a92 | cmd/claude-subagent/commit_test.go |
| 2 (GREEN) | Wire CommitWorktree into run() | 18592d0 | cmd/claude-subagent/main.go, main_test.go |

## What Was Built

### internal/harness/commit.go

Exports `CommitWorktree(worktreeDir, taskUID string) (plumbing.Hash, bool, error)`:

1. Runs `git -C worktreeDir status --porcelain` — empty output returns `(ZeroHash, true, nil)` without creating a commit.
2. Runs `git -C worktreeDir add -A` to stage all changes.
3. Reads identity from `TIDE_BOT_NAME` / `TIDE_BOT_EMAIL` env (fallback: `TIDE Bot` / `tide-bot@tideproject.k8s`). Passes `-c user.name=... -c user.email=...` BEFORE `-C worktreeDir` in the git args (required ordering per plan note).
4. Runs `git commit -m "tide: task <taskUID> authored"`.
5. Reads HEAD SHA via `git rev-parse HEAD` and returns as `plumbing.Hash`.

### cmd/claude-subagent/main.go

Added `commitWorktreeFunc` package-level test seam (mirrors `ensureWorktreeFunc` pattern).

In `run()`, after successful `newSubagent(...).Run()` and only when `env.Role == "executor"`:
- Calls `commitWorktreeFunc(filepath.Join(workspaceRoot, "worktrees", env.TaskUID), env.TaskUID)`.
- `commitErr != nil` → `failEnvelope(..., "commit-failed")`.
- `isEmpty == true` → `out.ExitCode = 1`, `out.Result = "empty-diff"`, `out.Reason = "executor produced no changes in worktree"`.
- Success → `out.Git = &pkgdispatch.GitOutput{HeadSHA: hash.String()}`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Pre-existing executor tests broke after commitWorktreeFunc was added**
- **Found during:** Task 2 GREEN phase, full test suite run
- **Issue:** `TestClaudeSubagentMain_InvokesEnsureWorktreeBeforeRun` and `TestClaudeSubagentMain_PassesEnvBranchToWorktree` both set `Role: "executor"` but don't set up a real git worktree dir. After adding the executor commit step, `commitWorktreeFunc` ran on a nonexistent directory and returned an error, causing exit code 1.
- **Fix:** Added `commitWorktreeFunc` seam override to both tests (returns a fake non-empty hash), following the same pattern already used for `ensureWorktreeFunc`. This keeps the tests focused on their original assertions (call ordering, branch passing) without requiring a real git directory.
- **Files modified:** `cmd/claude-subagent/main_test.go`
- **Commit:** 18592d0

## Test Coverage

| Test | Behavior Covered |
|------|-----------------|
| TestCommitWorktree | Untracked file committed; hash non-zero; isEmpty=false; git HEAD matches |
| TestCommitWorktreeEmpty | Clean worktree → (ZeroHash, true, nil); no commit created |
| TestCommitWorktreeEnvIdentity | TIDE_BOT_NAME/TIDE_BOT_EMAIL override author in git log |
| TestCommitWorktreeModifiedFile | Modified tracked file staged and committed via git add -A |
| TestRunCommitsExecutorWorktree | executor + non-empty diff → ExitCode=0, HeadSHA populated |
| TestRunEmptyDiffOverridesExitCode | executor + empty diff → ExitCode=1, Result="empty-diff" |
| TestRunPlannerSkipsCommit | planner role → commitWorktreeFunc not called; ExitCode=0 preserved |

## Verification

```
go test ./internal/harness/... ./cmd/claude-subagent/... -count=1 -timeout 60s
```

Output:
```
ok  github.com/jsquirrelz/tide/internal/harness       1.171s
ok  github.com/jsquirrelz/tide/internal/harness/redact 0.692s
ok  github.com/jsquirrelz/tide/cmd/claude-subagent     1.301s
```

## TDD Gate Compliance

- RED gate: `test(11-01)` commits exist for both tasks (1745c84, 4fc0a92)
- GREEN gate: `feat(11-01)` commits exist after RED (04524f0, 18592d0)
- No REFACTOR needed — code is clean as committed

## Known Stubs

None. The commit step is fully wired — CommitWorktree runs real git CLI operations and HeadSHA is a real SHA from the worktree.

## Threat Flags

No new network endpoints, auth paths, or schema changes introduced. `CommitWorktree` is a local git operation only — T-11-01-03 mitigation confirmed (no push, no remote credentials).

## Self-Check: PASSED
