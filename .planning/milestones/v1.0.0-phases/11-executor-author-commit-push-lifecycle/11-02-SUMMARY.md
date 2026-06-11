---
phase: "11"
plan: "02"
subsystem: pkg/git
tags: [git, integration, worktree, merge, tdd]
dependency_graph:
  requires:
    - "pkg/git.EnsureRunBranch (B1, plan 11-01 / e880a5a)"
    - "pkg/git.AddWorktree (B2, f639340)"
    - "pkg/git.TaskBranchName (worktree.go)"
  provides:
    - "IntegrateTaskBranches(bareRepoPath, runBranch string, taskBranches []string) error"
  affects:
    - "cmd/tide-push/main.go (Plan 11-03: --integrate-task-branches flag calls this)"
    - "internal/controller (Plan 11-04: B6 wiring triggers integration per wave)"
tech_stack:
  added: []
  patterns:
    - "git CLI via exec.Command (same precedent as AddWorktree)"
    - "TDD: RED commit (1ee0285) then GREEN commit (bbba42f)"
key_files:
  created:
    - pkg/git/integrate.go
    - pkg/git/integrate_test.go
  modified: []
decisions:
  - "D-01 honored: git merge --no-ff via CLI, not go-git (go-git v5.19.0 only supports FastForwardMerge)"
  - "D-03 honored: bot identity TIDE Bot <tide-bot@tideproject.k8s> as fallback, TIDE_BOT_NAME/TIDE_BOT_EMAIL env override"
  - "Empty taskBranches list returns nil without provisioning an integration worktree (no side effects on no-op)"
  - "Conflict detection: git merge --no-ff exits non-zero on conflict; CombinedOutput captures stderr; error returned to caller"
  - "Idempotent worktree provisioning: checks for .git file; treats already-checked-out/already-exists as no-op"
metrics:
  duration: "~8 minutes"
  completed_date: "2026-06-09"
  tasks_completed: 1
  files_changed: 2
---

# Phase 11 Plan 02: IntegrateTaskBranches (B4) Summary

**One-liner:** `IntegrateTaskBranches` merges per-task worktree branches into the run branch via `git merge --no-ff` CLI inside a provisioned integration worktree, surfacing conflicts as errors.

## What Was Built

`pkg/git/integrate.go` exports `IntegrateTaskBranches(bareRepoPath, runBranch string, taskBranches []string) error`.

**Behavior:**
- Empty `taskBranches` returns nil immediately; no worktree is provisioned.
- Provisions integration worktree at `<bareRepoParent>/worktrees/run-<runBranch>/` using `git -C bareRepoPath worktree add <dir> <runBranch>` (no `-b` flag — run branch already exists via `EnsureRunBranch`).
- Worktree provisioning is idempotent: checks for `.git` file first; treats "already checked out"/"already exists" as no-op.
- For each task branch: `git -c user.name=... -c user.email=... -C <integrationDir> merge --no-ff <taskBranch> -m "tide: integrate <taskBranch>"`.
- `--no-ff` ensures a merge commit even when fast-forward is possible — makes wave-parallelism topology explicit in the commit graph.
- Merge conflict: `git merge` exits non-zero; `CombinedOutput()` captures stderr; error is returned to caller.
- Bot identity: `TIDE_BOT_NAME` / `TIDE_BOT_EMAIL` env with hardcoded fallback `TIDE Bot <tide-bot@tideproject.k8s>` (D-03).

**Integration worktree path agreement:** `filepath.Join(filepath.Dir(bareRepoPath), "worktrees", "run-"+runBranch)` — the same path `tide-push --mode=push` expects for `gogit.PlainOpen`.

## TDD Gate Compliance

| Gate | Commit | Status |
|------|--------|--------|
| RED (test commit) | 1ee0285 | test(11-02): add failing tests for IntegrateTaskBranches |
| GREEN (impl commit) | bbba42f | feat(11-02): implement IntegrateTaskBranches via git merge --no-ff CLI |

## Test Results

```
=== RUN   TestIntegrateTaskBranches
--- PASS: TestIntegrateTaskBranches (0.33s)
=== RUN   TestIntegrateTaskBranchesIdempotent
--- PASS: TestIntegrateTaskBranchesIdempotent (0.20s)
=== RUN   TestIntegrateTaskBranchesEmptyList
--- PASS: TestIntegrateTaskBranchesEmptyList (0.04s)
=== RUN   TestIntegrateTaskBranchesConflictFails
--- PASS: TestIntegrateTaskBranchesConflictFails (0.30s)
PASS
ok  github.com/jsquirrelz/tide/pkg/git  3.159s  (25/25 pass)
```

Full `pkg/git` suite: 25/25 pass, no regressions.

## Commits

| Hash | Type | Description |
|------|------|-------------|
| 1ee0285 | test | RED: failing tests for IntegrateTaskBranches |
| bbba42f | feat | GREEN: IntegrateTaskBranches implementation |

## Deviations from Plan

None — plan executed exactly as written.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes. `IntegrateTaskBranches` touches the local filesystem (PVC-backed bare repo) only. exec.Command args are positional slice elements (no shell string) — no injection path (T-11-02-01). Merge conflict surfaces as error (T-11-02-02). Cross-project collision structurally prevented by unique per-project run branch name (T-11-02-03 accepted).

## Known Stubs

None.

## Self-Check: PASSED

- pkg/git/integrate.go: FOUND
- pkg/git/integrate_test.go: FOUND
- Commit 1ee0285 (RED): FOUND
- Commit bbba42f (GREEN): FOUND
- All 4 TestIntegrate* tests: PASS
- Full pkg/git suite (25 tests): PASS
