---
created: 2026-07-03T18:08:15.231Z
title: Add spec.git.baseRef so runs can branch off a non-default ref
area: git
resolves_phase: 35
files:
  - pkg/git/branch.go:40
  - pkg/git/clone.go:44
  - api/v1alpha2/project_types.go:205
---

## Problem

`EnsureRunBranch` (pkg/git/branch.go:40) always creates the
`tide/run-<project>-<ts>` branch at the bare clone's HEAD — the remote's
default branch. There is no way to base a run on any other ref.

Real need from the first external-repo run (2026-07-03): the operator wanted
the run based on an unmerged hotfix branch that touched the same
files the run would modify. The only workable answer was "merge the hotfix to
main first" — fine that time, but not always possible (long-lived release
branches, stacked work, repos where main is protected and slow to merge).

## Solution

Add optional `GitConfig.BaseRef` (branch name or SHA) to v1alpha2:

- CRD field + CEL/defaulting (empty = HEAD, current behavior).
- Plumb through the clone Job env → `EnsureRunBranch` resolves the given ref
  (`refs/heads/<baseRef>`, falling back to tag/SHA resolution) instead of
  `repo.Head()`.
- Bare clone already fetches all branches, so no CloneOptions change needed
  for branch refs; SHA support may need a fetch-depth check.
- Reject unresolvable refs at reconcile with a clear condition rather than a
  cryptic worktree-add failure.

Chart is the FIXED contract — schema change rides a chart version bump per
the usual binary-catches-up-to-chart rule.
