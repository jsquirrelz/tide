---
phase: 10-task-execution-reliability-clone-idempotency-per-run-workspa
plan: 05
gate_decision: BLOCKED
date: 2026-06-08
dod_assertions_passed: 1
dod_assertions_total: 5
cascades_closed_this_session: 4
v1_0_0_retag: BLOCKED
---

# Phase 10 — DoD Re-run Gate (10-05)

**gate_decision: BLOCKED** — the medium Definition of Done (real Claude run reaching
`Complete`, pushing a `tide/run-*` branch with real authored code, `costSpentCents > 0`)
is **NOT met**. The run drove dramatically further than any prior attempt and four new
cascades were closed; it is now blocked on the executor author→commit→push lifecycle,
which was never wired end-to-end (the Phase-8 debug doc's "UNVERIFIED downstream" set).
v1.0.0 retag stays **BLOCKED**.

## Wave 1 — surgical fixes (LANDED, green)

All four merged to `main`, `make test` exit 0:

| Plan | SC | Fix | Commit(s) |
|------|----|-----|-----------|
| 10-01 | SC-1 | Clone idempotency (open+fetch on `ErrRepositoryAlreadyExists`) | merged 42ed8c9 |
| 10-02 | SC-2 | `FSGroup=1000` on clone/push pod specs | merged 161a747 |
| 10-03 | SC-4 | Child-CRD `json.Decoder` + per-file isolation + prompt | merged bd94fe3 |
| 10-04 | SC-7 | Dashboard cross-namespace project Get | merged 5a21f95 |

SC-1 confirmed at runtime: the clone Job `Complete 1/1` on a warm PVC every run.
SC-2 confirmed at runtime: the live push pod carries `podSC={"fsGroup":1000}`.

## Wave 2 — DoD run (10-05): cascades closed this session

The run now reliably executes **Project → Milestone → Phase → Plan → Tasks materialize →
executor launches → `git worktree add`**, with cost tracking working (defect #6 fixed:
`costSpentCents` reached 92–107¢ per run; ≤ $5 cap). Four new cascades fixed + committed:

| ID | Defect | Fix | Commit |
|----|--------|-----|--------|
| A | Plan planner emitted child-CRD JSON with literal newlines inside string values → `invalid character '\n' in string literal`, 0 tasks | `sanitizeJSONStringControls` escapes raw control chars inside string literals; prompt hardened | 8f8d62a |
| B | `tide-push` (uid 65532) `mkdir /workspace/envelopes/push: permission denied` on warm PVC (envelopes dir 0755 owned by uid 1000) | `mkdirSharedAll` makes the envelopes subtree group-writable+setgid | b148dd6 |
| C | Executor `git worktree add`: `exec: "git": not found` (claude-subagent image had no git) | install `git` in the image | 314afd8 |
| D | `git worktree add`: `detected dubious ownership` (repo owned by clone uid 65532, executor uid 1000) | `git config --system safe.directory '*'` in the image | 5b9d4ca |

## DoD assertions (1 / 5)

| # | Assertion | Result |
|---|-----------|--------|
| a | project `phase == Complete` | ❌ stuck `Running` (executor blocked) |
| b | all Tasks Succeeded | ❌ task-01 `Failed` at worktree-add |
| c | `tide/run-*` branch pushed with real code | ❌ never reached push |
| d | `costSpentCents > 0` | ✅ 92–107¢ (defect #6 fixed) |
| e | executor worktree branch non-empty | ⛔ N/A (blocked before authoring) |

## Blocking cascade (E) — executor author→commit→push lifecycle (design-level)

Two parallel models were each built and unit-tested in Phase 3 but never connected:

- **Artifact model (planners, works):** subagents write `/workspace/artifacts/…`; `tide-push`
  copies+commits+pushes them.
- **Worktree model (executors, incoherent):** per-Task worktrees `worktrees/<taskUID>` (D-B4),
  executor authors+commits in-worktree, push boundary in `tide-push`.

Missing seams: (1) the `tide/run-*` branch is named (`project_controller.go:418`) but never
created as a ref; (2) the per-run worktree `worktrees/run-<branch>` `tide-push` opens is never
provisioned; (3) no bridge from per-task worktree commits → the pushed run branch.

### Decision: Option B (chosen 2026-06-08) — proper end-state

Run-branch + per-task worktrees on their own branches + executor commit + integrate task
branches into the run branch + push. Implementation status:

| # | Component | Status | Where |
|---|-----------|--------|-------|
| B1 | `pkg/git.EnsureRunBranch` (create run-branch ref at default HEAD) | ✅ DONE, green | e880a5a |
| B2 | `AddWorktree` → real `git worktree add -b tide/wt-<uid> <path> <runBranch>` | ✅ DONE, green | f639340 |
| B3 | Executor commit step (git CLI add -A + commit, identity, HeadSHA) after authoring | ⏳ TODO | `internal/harness`, `cmd/claude-subagent` |
| B4 | Integration: merge per-task branches → run branch (wave/DAG order) | ⏳ TODO | `pkg/git` + caller |
| B5 | `tide-push`: clone-mode provisions run-branch (EnsureRunBranch) + run worktree; push-mode pushes run branch | ⏳ TODO | `cmd/tide-push` |
| B6 | Controller wiring: pass run-branch to clone Job; trigger integration before final push | ⏳ TODO | `internal/controller` |

Then rebuild claude-subagent + tide-push + controller images, re-run the medium DoD.

## Cluster / image state (parked)

minikube context `minikube`; images rebuilt into the in-cluster daemon this session:
controller `d0fcfa26`, tide-push `5ac1c39f`, claude-subagent `6bc7dadd` (git + safe.directory).
The B2 worktree change is NOT yet in a rebuilt image (committed to `main` only) — rebuild
claude-subagent before the next run. Do not rely on the parked failed medium-project run;
delete + re-apply after B3–B6 land.

## v1.0.0

Remains BLOCKED — unblocks only on a legitimate medium `Complete` + pushed `tide/run-*`
branch. Retag is user-gated confirm-only per MEMORY.md (2026-06-03 option a); NOT performed here.
