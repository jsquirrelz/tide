---
created: 2026-07-03T18:40:00.000Z
title: Wave-parallel task integrate step skipped; Complete does not gate on unintegrated worktree branches
area: git
resolves_phase: 34
files:
  - internal/harness/commit.go
  - pkg/git/worktree.go
---

## Problem

On the first external-repo run (2026-07-03),
wave 1 ran two tasks in parallel (`task-02-logging-docs` uid a877adf3,
`task-03-logging-tests` uid e088c86c). Both Task CRs reached Succeeded and
both worktree branches got their `tide: task <uid> authored` commit — but the
run branch only received integrate commits for tasks 01 and 02. Task 03's
`tide/wt-e088c86c-…` branch (commit d7d2234, +60-line
`server/tests/test_logging_settings.py`) was never merged.

Downstream: the boundary push shipped the run branch missing a declared
`filesTouched` deliverable, and the Project stamped Complete. Two contract
violations stack:

1. The integrate-into-run-branch step can be skipped/lost for one of two
   same-wave parallel tasks (smells like a race — RWO PVC, two integrations
   near-simultaneously).
2. Nothing gates Succeeded/Complete on "every succeeded task's wt branch is
   merged into the run branch" — the miss was silent. Also observed:
   `status.git.lastPushedSHA` never set even though BoundaryPushed=True
   (Pushed) — the lease bookkeeping may share the same gap.

Recovered manually via `git format-patch` from the PVC bare repo; evidence
preserved in the run's project namespace (local minikube) and on the pushed
run branch (which carried only 2 of 3 tasks' work).

## Solution

TBD — reproduce with a 2-parallel-task wave in the kind suite, then: make
integration per-task-serialized (or retry-on-lost merge), and add a
completeness check before boundary push / Complete: every Succeeded task UID
must have a merge commit reachable from the run branch (or an explicit
empty-diff marker). Verify lastPushedSHA stamping while in there.

Framing note: the completeness check is the degenerate (mechanical,
non-LLM) case of an Adversarial Verification stage — TIDE has no verify
step of any kind between "all tasks Succeeded" and Complete. The general
answer is the verify-level subagent seed
(`.planning/seeds/verify-level-subagent.md`); this todo's mechanical gate
should land regardless and first.
