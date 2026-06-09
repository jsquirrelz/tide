# Phase 11: Executor Author→Commit→Push Lifecycle — Research

**Researched:** 2026-06-09
**Domain:** Go git library (go-git v5), per-task worktree commit lifecycle, per-run branch integration, tide-push clone/push mode wiring
**Confidence:** HIGH

## Summary

Phase 11 closes the final seam that prevented the medium sample from reaching a legitimate `Project=Complete` with a pushed `tide/run-*` branch. Phase 10 proved the full planning cascade and executor launch, but stopped at `git worktree add` because the run branch ref was never created (naming it in `project_controller.go:418` but never writing the ref), the per-run worktree at `worktrees/run-<branch>` that `tide-push --mode=push` expects was never provisioned, and there was no code that committed executor worktree changes after the claude CLI exited.

Foundation B1 (`pkg/git.EnsureRunBranch`, e880a5a) and B2 (`pkg/git.AddWorktree` via `git worktree add -b`, f639340) are already on `main`. The remaining work is four components: B3 (executor commit step in the harness + `cmd/claude-subagent` wiring), B4 (per-task branch integration into the run branch in DAG order), B5 (`tide-push` provisions the run branch + per-run worktree in clone mode; pushes via push mode), and B6 (controller wires the run-branch name to the clone job, triggers integration before the final push).

**Primary recommendation:** Implement B3–B6 in strict dependency order across two or three plans. Wire B3 (harness commit step) and B5-clone (EnsureRunBranch call in runClone) in parallel (Wave 1), then B4 (IntegrateTaskBranches via `git merge --no-ff` CLI in pkg/git) and B5-push worktree open path (already exists, add provisioning guard) (Wave 2), then B6 controller wiring + DoD re-run (Wave 3).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Per-task worktree commit | Executor pod (harness post-run step) | — | The harness owns the post-Claude lifecycle; the agent runs inside the worktree |
| Run-branch ref creation | Clone Job (tide-push --mode=clone) | — | The clone job is the first actor with PVC access; EnsureRunBranch is a pure git storer op |
| Per-task branch integration | Push Job (tide-push --mode=push) or dedicated integration step | Controller trigger | All task branches for a wave exist on the PVC; integration must see them before push |
| Push to remote | Push Job (tide-push --mode=push) | — | ART-04 — push from orchestrator-side job, not subagent pod |
| Run-branch name threading | ProjectReconciler (Status.Git.BranchName) → EnvelopeIn.Branch | TaskReconciler reads + threads | Already partially wired (09-09 landed); B6 completes the clone-job leg |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| go-git/v5 | v5.19.0 (pinned in go.mod) | Pure-Go git: bare repo open, ref set, commit, push | Already in use; EnsureRunBranch uses it |
| os/exec + git CLI | system git (in claude-subagent + tide-push images) | `git worktree add`, `git add -A`, `git commit`, `git merge` | go-git v5.19.0 merge only supports FastForwardMerge; real three-way / octopus merge requires git CLI |
| pkg/git (internal) | current HEAD | AddWorktree, EnsureRunBranch, Commit, AddPath, Push | Package-level seam already used by B1+B2 |

### Key Version Constraint

go-git v5.19.0's `Repository.Merge()` supports **only** `FastForwardMerge` and returns `ErrUnsupportedMergeStrategy` for any other strategy. [VERIFIED: /Users/justinsearles/go/pkg/mod/github.com/go-git/go-git/v5@v5.19.0/repository.go:1789-1791]. Since per-task branches that independently authored code are NOT generally fast-forward of each other, the integration step (B4) must use the `git merge` CLI (already present in both images). This is the same precedent as AddWorktree (B2), which chose `git worktree add` CLI over go-git for the same reason.

## Architecture Patterns

### System Architecture Diagram

```
Clone Job (tide-push --mode=clone)
  │  1. PlainClone → /workspace/repo.git  [go-git, unchanged]
  │  2. EnsureRunBranch(repo.git, BranchName)  [NEW — B5-clone]
  └──→ BranchName ref exists in bare repo

Executor Pod (claude-subagent) per Task
  │  1. EnsureWorktree(env, workspace, Branch)  [harness, unchanged]
  │     └──→ worktrees/<TaskUID>/ on branch tide/wt-<TaskUID>
  │  2. claude -p ...  [anthropic runner, unchanged]
  │  3. CommitWorktree(worktreeDir, TIDE identity)  [NEW — B3]
  │     └──→ HeadSHA in EnvelopeOut.Git.HeadSHA
  └──→ tide/wt-<TaskUID> branch has authored commit(s)

Wave controller (between waves, or at plan completion)
  │  1. CollectTaskSHAs(completedTasks)  [NEW — B6]
  │  2. IntegrateTaskBranches(repo.git, runBranch, taskBranches, DAG order)  [NEW — B4]
  │     └──→ run branch has all task commits merged in

Push Job (tide-push --mode=push)
  │  1. Open worktrees/run-<branch>/ (must exist)  [exists; B5-clone provisions it]
  │  2. Stage planner artifacts + commit + gitleaks  [unchanged]
  │  3. pkg/git.Push with force-with-lease  [unchanged]
  └──→ tide/run-* branch on remote with real authored code
```

### Recommended Project Structure

No new top-level directories. Changes are incremental within existing packages:

```
pkg/git/
├── branch.go          # EnsureRunBranch (B1, done)
├── integrate.go       # IntegrateTaskBranches (B4, NEW)
├── integrate_test.go  # B4 tests (NEW)
├── worktree.go        # AddWorktree (B2, done)
└── commit.go          # AddPath, Commit (existing)

internal/harness/
├── worktree.go        # EnsureWorktree (existing)
├── commit.go          # CommitWorktree (B3, NEW) — separate file, not in harness.go
└── commit_test.go     # B3 tests (NEW)

cmd/claude-subagent/
└── main.go            # add CommitWorktree call after anthropic.Run (B3 wiring)

cmd/tide-push/
└── main.go            # runClone: add EnsureRunBranch + provision run worktree (B5)

internal/controller/
├── push_helpers.go    # buildCloneJob: add --run-branch arg (B6)
└── project_controller.go  # trigger integration job before final push (B6)
```

### Pattern 1: B3 — Executor Commit Step (harness/commit.go)

**What:** After `anthropic.Run()` returns (exit 0), the harness reads `git status` in the linked worktree. If there are staged or unstaged changes, it runs `git add -A` + `git commit` with a TIDE identity. Returns `(plumbing.Hash, bool, error)` — the SHA, an `isEmpty` flag, and an error.

**Source touchpoints:**
- `internal/harness/commit.go` — new file, `CommitWorktree(worktreeDir string, identity GitIdentity) (plumbing.Hash, bool, error)`
- `cmd/claude-subagent/main.go:run()` — add call between `newSubagent(...).Run(ctx, env)` and `writeEnvelope`; if `isEmpty=true`, populate `out.Result = "empty-diff"` and a non-zero exit code (B3 requirement: empty diff = explicit result, not false success)
- `pkg/dispatch/envelope.go` — `EnvelopeOut.Git` field already exists; populate `out.Git = &pkgdispatch.GitOutput{HeadSHA: hash.String()}` when non-empty

**Empty-diff policy (fork c answer):** If the worktree has zero changes after claude exits (`git diff-index --quiet HEAD --`), the harness must NOT create an empty commit. It should return `isEmpty=true`. `cmd/claude-subagent` translates this into `ExitCode=1, Result="empty-diff", Reason="executor produced no changes"`. This surfaces to the controller as a task failure (retriable). An empty executor result is not "success" — success requires at least one committed file change.

**Implementation note:** Use the git CLI (`git -C <worktreeDir> add -A && git -C <worktreeDir> commit -m "tide: task <taskUID> authored" --author="TIDE Bot <tide-bot@tideproject.k8s>"`) rather than go-git for the commit step. The per-task worktree was created by `git worktree add` CLI; operating on it with go-git's `PlainOpen` works but has caused `dubious ownership` issues (cascade D in Phase 10). Since git is already installed in the claude-subagent image (cascade C fix), using the CLI is safer and consistent with AddWorktree's CLI-first precedent.

**Identity source (fork c answer):** Hardcode `TIDE Bot <tide-bot@tideproject.k8s>` in the harness, same as `tideBotSignature()` in `cmd/tide-push/main.go`. The commit identity is not a user-facing configuration concern at v1 (no Helm value for it; no user should be customizing bot identity per task). If needed, thread via env var `TIDE_BOT_NAME` / `TIDE_BOT_EMAIL` so both tide-push and the executor harness can pick it up from the pod spec without a Helm values addition.

**Example (pseudocode pattern):**

```go
// internal/harness/commit.go
// Source: based on pkg/git/worktree.go pattern (exec.Command git CLI)

func CommitWorktree(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
    // Step 1: Check for changes.
    out, err := exec.Command("git", "-C", worktreeDir, "status", "--porcelain").Output()
    if err != nil {
        return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git status: %w", err)
    }
    if len(strings.TrimSpace(string(out))) == 0 {
        return plumbing.ZeroHash, true, nil // empty-diff
    }
    // Step 2: Stage all changes.
    if out, err := exec.Command("git", "-C", worktreeDir, "add", "-A").CombinedOutput(); err != nil {
        return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git add -A: %w: %s", err, string(out))
    }
    // Step 3: Commit with TIDE identity.
    msg := fmt.Sprintf("tide: task %s authored", taskUID)
    args := []string{"-C", worktreeDir, "-c", "user.name=TIDE Bot", "-c",
        "user.email=tide-bot@tideproject.k8s", "commit", "-m", msg}
    if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
        return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git commit: %w: %s", err, string(out))
    }
    // Step 4: Read HEAD SHA from the worktree.
    sha, err := exec.Command("git", "-C", worktreeDir, "rev-parse", "HEAD").Output()
    if err != nil {
        return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: rev-parse HEAD: %w", err)
    }
    return plumbing.NewHash(strings.TrimSpace(string(sha))), false, nil
}
```

### Pattern 2: B4 — IntegrateTaskBranches (pkg/git/integrate.go)

**What:** Given a bare repo path, a run-branch name, and an ordered list of per-task branch names (ordered by DAG completion: wave-by-wave, within a wave in any stable order), fast-forward or merge each task branch into the run branch in sequence.

**Why CLI:** go-git v5.19.0 `Repository.Merge()` only supports `FastForwardMerge` and returns `ErrUnsupportedMergeStrategy` for anything else. [VERIFIED: repository.go:1789-1812]. Two sibling tasks in the same wave that each authored to DIFFERENT files can be merged sequentially (each is a fast-forward of the previous result IF they touched different trees). Two tasks that touched overlapping files require a real three-way merge or cherry-pick, which go-git cannot do. Use `git merge --no-ff` via CLI inside a temporary worktree opened on the run branch.

**Example:**

```go
// pkg/git/integrate.go
// Source: pattern from worktree.go (exec.Command git CLI)

// IntegrateTaskBranches merges each branch in taskBranches into runBranch,
// in the provided order, inside the bare repo at bareRepoPath.
// Uses a throw-away worktree at <bareRepoPath>/../worktrees/run-<runBranch>/
// (same path tide-push --mode=push expects).
func IntegrateTaskBranches(bareRepoPath, runBranch string, taskBranches []string) error {
    integrationDir := filepath.Join(filepath.Dir(bareRepoPath), "worktrees", "run-"+runBranch)
    // Provision the integration worktree if not present (idempotent).
    if _, err := os.Stat(filepath.Join(integrationDir, ".git")); err != nil {
        if _, err := exec.Command("git", "-C", bareRepoPath, "worktree", "add",
            integrationDir, runBranch).CombinedOutput(); err != nil {
            // Note: use of existing branch (not -b) — run branch already created by EnsureRunBranch.
            ...
        }
    }
    for _, tb := range taskBranches {
        args := []string{"-C", integrationDir,
            "-c", "user.name=TIDE Bot", "-c", "user.email=tide-bot@tideproject.k8s",
            "merge", "--no-ff", tb, "-m", "tide: integrate " + tb}
        if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
            return fmt.Errorf("integrate %s → %s: %w: %s", tb, runBranch, err, string(out))
        }
    }
    return nil
}
```

**Note on `--no-ff` vs `--ff-only`:** Use `--no-ff` (creates a merge commit even when FF is possible). This produces a clear commit-graph showing exactly which task branches contributed. `--ff-only` fails when the history is non-linear (which it will be after the first non-empty task merge). Cherry-pick is simpler but loses the "parallel siblings authored in the same wave" topology from the commit graph — prefer merge commits.

### Pattern 3: B5 — tide-push clone mode provisions run-branch + run worktree

**What:** After `pkg/git.Clone()` succeeds in `runClone()`, call `pkg/git.EnsureRunBranch(destDir, runBranch)` to create the ref. Also call `git worktree add <worktrees/run-<branch>> <runBranch>` (same as IntegrateTaskBranches provisions) so `tide-push --mode=push`'s `gogit.PlainOpen(worktreeDir)` succeeds.

**Current gap in `cmd/tide-push/main.go:runClone()`** [VERIFIED: lines 206-223]: it calls `pkggit.Clone()` and returns. No EnsureRunBranch call. No worktree provisioning.

**New flag:** Add `--run-branch string` to the tide-push CLI args. `buildCloneJob` (B6) passes this arg. `runClone()` reads it; if non-empty, calls EnsureRunBranch + `git worktree add ... worktrees/run-<branch> <branch>`.

**Existing push path (runPush, line 229):** It already calls `gogit.PlainOpen(worktreeDir)` where `worktreeDir = filepath.Join(cfg.Workspace, "worktrees", "run-"+cfg.Branch)`. If the worktree was not provisioned by clone mode, this PlainOpen fails with "repository does not exist". B5 fixes this by ensuring clone mode provisions the worktree before any executor runs.

### Pattern 4: B6 — Controller wiring

**What:**
1. `buildCloneJob` passes `--run-branch=<project.Status.Git.BranchName>` to the tide-push container. [File: `internal/controller/push_helpers.go:buildCloneJob`, currently lines 262-325 — does NOT pass a branch arg to clone mode]
2. Before triggering the final level-boundary push Job (`triggerBoundaryPush`), the controller triggers integration. Integration can run as a step inside the push Job (simplest) or as a separate Job (more atomic). The simplest approach for v1: add an `--integrate-task-branches=<CSV>` flag to `tide-push --mode=push` that runs IntegrateTaskBranches before staging planner artifacts.

**Alternative B6 approach:** Run integration inside the push Job rather than as a controller-triggered side Job. Rationale: the push Job already has PVC access and the git CLI; adding an integration pre-step keeps the atomic "integrate+commit+push" unit in one Job and avoids a new Job type.

**Controller trigger point:** After all Tasks in the final wave complete (all `Task.status.phase == Succeeded`), before dispatching `triggerBoundaryPush`. The controller must collect `HeadSHA` from each Task's reporter CRD (via `task.Status.Git.HeadSHA` — this field needs to be plumbed: see section "Open Questions #1").

### Anti-Patterns to Avoid

- **Empty commit on empty diff:** Do NOT call `git commit` when `git status --porcelain` is clean. Empty commits are confusing in the history and violate the "empty-diff = explicit failure" requirement.
- **go-git Merge for non-FF case:** go-git v5.19.0 will return `ErrUnsupportedMergeStrategy` for any strategy other than FastForwardMerge; use git CLI for all merge operations.
- **Merging into run branch from the executor pod:** Executor pods have no git credentials (ART-04). Integration happens in the push Job, not in the subagent pod.
- **Sharing a single worktree across tasks in the same wave:** Each task gets its own `worktrees/<TaskUID>/` (B2). The integration worktree `worktrees/run-<branch>/` is separate and owned by the push Job.

## Open Fork Recommendations

### Fork (a): Integration Mechanism

**Recommendation: `git merge --no-ff` per task branch in the run worktree, via git CLI.**

**Why:** go-git v5.19.0 only supports fast-forward merge (`Repository.Merge()`, lines 1789-1815 — `ErrUnsupportedMergeStrategy` for anything else). [VERIFIED: repository.go]. Per-task branches authored by two tasks in the same wave that touched overlapping paths require a three-way merge, which go-git cannot perform. The git CLI is already present in both tide-push and claude-subagent images (cascade C fix, commit 314afd8). `--no-ff` produces a merge commit that makes the integration topology explicit in the log.

**Concrete touchpoints:**
- New function `pkg/git.IntegrateTaskBranches(bareRepoPath, runBranch string, taskBranches []string) error`
- Uses `exec.Command("git", "-C", integrationDir, "merge", "--no-ff", taskBranch, ...)` inside a per-run worktree at `worktrees/run-<branch>/`

**Rejected alternatives:**
- *go-git fast-forward only:* fails if any two siblings touched the same file tree. Only safe when TIDE guarantees non-overlapping `FilesTouched` sets per wave. Not safe in general.
- *Cherry-pick in DAG order:* produces a linear history, which is cleaner, but cherry-pick loses the wave-parallelism topology signal. Also, if a task made 10 micro-commits before the harness committed them as one, cherry-pick would need to replay each one. The harness commit step (B3) collapses per-task changes into a single commit, so cherry-pick is equivalent to merge at that point — prefer merge for the clearer merge-base tracking.
- *Ref-level fast-forward (no tree merge):* This is what go-git `Repository.Merge(FastForwardMerge)` does — it just moves the HEAD ref pointer. Only works when the run branch is strictly behind the task branch (linear history). Fails the moment two tasks have diverged from the same run-branch tip.

### Fork (b): Integration Timing

**Recommendation: per-wave integration, triggered immediately after each wave's tasks complete.**

**Why:** SC-3 in the Phase 11 ROADMAP entry requires "dependents see their dependencies' commits" [VERIFIED: .planning/ROADMAP.md:465]. A task in wave k+1 that `dependsOn` a task in wave k must find the wave-k task's authored changes already on the run branch when its worktree is created (`AddWorktree` forks from `runBranch`). If integration is deferred to the push boundary, the wave-k task branches are NOT yet in the run branch when wave-k+1 executors run `EnsureWorktree`, so dependents cannot see their dependencies' code.

**Implementation implication:** The controller must trigger integration at wave completion, not only at plan completion. The wave reconciler (`WaveReconciler`) or a post-wave hook in the plan reconciler is the natural trigger point. The integration step writes to the PVC (no K8s API calls) and can run inside the push Job or as a lightweight pre-step inside tide-push --mode=push with an `--integrate-task-branches` flag.

**Rejected alternative — all-at-push-boundary:** Simpler to implement (no per-wave trigger needed), but violates the dependency visibility contract. A downstream task in wave 2 that starts with `git checkout tide/wt-task2-uid` (forking from runBranch) would not see wave-1 changes unless the run branch was integrated after wave 1. The ROADMAP explicitly states this requirement.

**Practical note for medium sample:** The medium sample's plan planner generates 3-5 tasks with `dependsOn` edges. If all tasks are in a single wave (no inter-task dependencies), all-at-push-boundary is fine. But the requirement is general, and the sample is intended to exercise the real model. Implement per-wave for correctness.

### Fork (c): Commit Identity Source and Empty-Diff Handling

**Commit identity recommendation:** Hardcode `TIDE Bot <tide-bot@tideproject.k8s>` in the harness commit step, matching `tideBotSignature()` in `cmd/tide-push/main.go`. [VERIFIED: cmd/tide-push/main.go:127-128]. Optionally read `TIDE_BOT_NAME` / `TIDE_BOT_EMAIL` env vars with this as the fallback, so the values can be overridden via pod env without a Helm values addition. Do NOT add a new Helm values key at v1 (not user-facing, adds noise to chart).

**Empty-diff handling recommendation:** If `git status --porcelain` in the executor worktree is empty after claude exits, the harness must NOT create a commit. Return `isEmpty=true`. The claude-subagent shim translates this into `ExitCode=1, Result="empty-diff", Reason="executor produced no changes in worktree"`. The task controller marks the task Failed (retriable). This is the correct behavior: a task that authored nothing is an execution failure (the prompt was supposed to produce code changes), not a success.

**Concrete touchpoints:**
- `internal/harness/commit.go` (new file): `CommitWorktree(worktreeDir, taskUID string) (plumbing.Hash, bool, error)` where `bool = isEmpty`
- `cmd/claude-subagent/main.go:run()`: after `newSubagent(...).Run(ctx, env)` succeeds (ExitCode=0), call `CommitWorktree`; if isEmpty, override out.ExitCode=1 and out.Result="empty-diff"
- `pkg/dispatch/envelope.go` `EnvelopeOut.Git` field: already exists; populate with `&pkgdispatch.GitOutput{HeadSHA: hash.String()}`

**Rejected alternatives:**
- *Treat empty diff as success:* violates the SC-2 requirement ("a task that authored nothing surfaces an explicit empty-diff result rather than a false success")
- *Create an empty commit:* pollutes the git history; the run branch has merge commits pointing at empty commits; semantically wrong
- *Read identity from Helm env via a new configmap:* unnecessary complexity for v1; the bot identity is not a user-customization concern

### Fork (d): tide-push Planner-Artifact Boundary Commit vs Executor Run Branch

**Recommendation: Keep the two commit streams SEPARATE and UNIFIED into one final push.**

**What the two streams are:**
- *Planner stream:* `tide-push --mode=push` stages `ArtifactPaths` (MILESTONE.md, phase briefs, PLAN.md files), creates a boundary commit with the W11 message ("tide: plan X authored + executed"), and pushes. This is triggered by `triggerBoundaryPush` (boundary_push.go).
- *Executor stream:* per-task branches (`tide/wt-<taskUID>`) are merged into the run branch by B4 before the push job runs; the push job's final push sends BOTH the integrated executor commits AND the planner boundary commit in one push.

**How to unify:** The `runPush` function in `cmd/tide-push/main.go` currently stages artifact paths and creates one boundary commit. After B5, the run worktree already has the integrated executor commits (merged by the pre-push integration step). The boundary commit is appended on top of those. One push sends everything: executor authored commits + planner boundary commit.

**Are the streams independent?** The planner boundary commit is authored by `tide-push` (not the claude agent). The executor commits are authored by the claude agent via the harness commit step. They are logically independent but both go to the same run branch. The simplest wiring: the push job (1) runs IntegrateTaskBranches first (if `--integrate-task-branches` is set), (2) then stages + commits planner artifacts, (3) then pushes. The run branch log will show: `[run branch root] → [task1 authored] → [task2 authored] → [merge commit sibling A+B] → [tide: plan X authored + executed]`.

**Rejected alternative — separate push jobs for executor vs planner streams:** Adds a second push job, introduces race on `--force-with-lease`, and complicates the controller wiring. Not needed.

**Rejected alternative — unify by having `tide-push` commit executor artifacts directly (bypassing per-task branches):** Breaks the per-task worktree model; the whole point of B2 is isolated per-task commit histories.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-task branch creation | Custom ref creation code | `git worktree add -b` CLI (AddWorktree, B2 — done) | go-git linked-worktree API incomplete |
| Three-way/octopus merge | Custom merge algorithm | `git merge --no-ff` CLI | go-git v5.19.0 only supports FF merge |
| Run-branch ref creation | Manual hash-ref write | `EnsureRunBranch` (B1 — done) | Already implemented, idempotent, tested |
| Push with lease | Custom remote protocol | `pkg/git.Push` (existing) | Already wired with ForceWithLease |

**Key insight:** The git CLI is already installed in the executor image (cascade C fix). Where go-git falls short (linked worktrees, non-FF merge), use the CLI as AddWorktree already demonstrates.

## Runtime State Inventory

This is not a rename/refactor phase. No runtime state inventory required.

## Common Pitfalls

### Pitfall 1: EnsureRunBranch Not Called Before AddWorktree

**What goes wrong:** If `runClone` doesn't call `EnsureRunBranch`, the `AddWorktree` call in the harness's `EnsureWorktree` fails with "couldn't find remote ref tide/run-..." (this is exactly the cascade that Phase 10 blocked on).
**Why it happens:** B1 is implemented but has no production caller yet. [VERIFIED: grep confirms no production call site for EnsureRunBranch outside tests]
**How to avoid:** B5 adds the EnsureRunBranch call in `cmd/tide-push/main.go:runClone()` immediately after the Clone succeeds.
**Warning signs:** Task log shows `git worktree add` error "couldn't find remote ref" or "pathspec '...' did not match".

### Pitfall 2: Run Worktree Not Provisioned Before Push

**What goes wrong:** `tide-push --mode=push` calls `gogit.PlainOpen(worktrees/run-<branch>/)` at line 259. If the directory doesn't exist, this returns "repository does not exist" and the push job fails.
**Why it happens:** The run worktree (`worktrees/run-<branch>`) is distinct from per-task worktrees (`worktrees/<taskUID>`). Clone mode currently provisions neither.
**How to avoid:** B5 adds `git worktree add worktrees/run-<branch> <runBranch>` in clone mode (after EnsureRunBranch ensures the branch exists).
**Warning signs:** Push job fails immediately with "repository does not exist" or "PlainOpen ... failed".

### Pitfall 3: Per-Task Worktree Commit Not Reflected in Reporter CRD

**What goes wrong:** The B3 commit step writes `HeadSHA` to `EnvelopeOut.Git.HeadSHA`, but the reporter CRD schema and the controller's `task.Status.Git.HeadSHA` field may not be wired to carry it. Integration (B4) needs to know which branch names to merge; the caller uses `TaskBranchName(taskUID)` directly, not HeadSHA — but the controller still needs a signal that the task committed successfully.
**Why it happens:** `GitOutput.HeadSHA` exists in `pkg/dispatch/envelope.go:226` but `Task.status` may not have a corresponding `Git.HeadSHA` field (need to verify).
**How to avoid:** Check `api/v1alpha1/task_types.go` for a `Status.Git` field before B6 wiring; add it if absent. The integration step can use `git/TaskBranchName(taskUID)` directly without HeadSHA for branch resolution.

### Pitfall 4: go-git Merge on Diverged Branches

**What goes wrong:** Calling `repo.Merge(taskBranchRef, MergeOptions{Strategy: FastForwardMerge})` when the task branch and run branch have diverged returns `ErrFastForwardMergeNotPossible`. [VERIFIED: repository.go:1812]
**Why it happens:** After the first task is merged into the run branch (which was at the clone tip), the run branch is ahead of all other task branches that also started from the same clone tip. Merging the second task branch is no longer a fast-forward.
**How to avoid:** Always use the `git merge` CLI, not go-git `Repository.Merge()`.
**Warning signs:** Integration step fails with "not possible to fast-forward merge changes".

### Pitfall 5: Empty-Diff Task Treated as Success

**What goes wrong:** If the B3 commit step is absent or skips the empty-diff check, a task where claude authoring produced no file changes exits 0 (claude exit 0), the harness returns ExitCode=0, and the reporter marks the task Succeeded. The push job then attempts to push a run branch with no executor content.
**Why it happens:** The claude exit code reflects whether the claude process itself errored, not whether it wrote any files.
**How to avoid:** Always check `git status --porcelain` before committing; return `isEmpty=true` and translate to `ExitCode=1, Result="empty-diff"` in the shim.

### Pitfall 6: Integration Before Wave k+1 Starts — Ordering Constraint

**What goes wrong:** If integration is triggered only at plan completion (not per-wave), a task in wave k+1 that runs `EnsureWorktree` forks its worktree from the run branch tip at clone time (before any wave-k commits). The wave-k authored changes are NOT in the worktree.
**Why it happens:** `AddWorktree` forks the new worktree from the current run-branch HEAD. If wave-k tasks haven't been integrated yet, the run branch head is still the bare clone tip.
**How to avoid:** Trigger integration after wave-k completes, before dispatching any wave-k+1 executor. The controller must treat integration as a precondition for dispatching the next wave's executors.

### Pitfall 7: Rebuild Images After B3 Change

**What goes wrong:** The B3 commit step is in `cmd/claude-subagent/main.go` and `internal/harness/`. If images are not rebuilt after landing B3, the in-cluster executor still runs the old shim without the commit step.
**Why it happens:** Already noted in 10-VERIFICATION.md: "The B2 worktree change is committed-only — rebuild claude-subagent before the next run."
**How to avoid:** The DoD plan (final wave) must include image rebuild steps: `make build-images` and `minikube image load` (or equivalent) for `controller`, `tide-push`, and `claude-subagent`.

## Code Examples

### EnsureRunBranch (already exists, B1 done)

```go
// Source: /Users/justinsearles/Projects/tide/pkg/git/branch.go:40
func EnsureRunBranch(bareRepoPath, branch string) error {
    // ... pure go-git storer op; idempotent; already tested
}
```

### AddWorktree (already exists, B2 done)

```go
// Source: /Users/justinsearles/Projects/tide/pkg/git/worktree.go:59
func AddWorktree(repoPath, taskUID, runBranch string) (string, error) {
    // ... git worktree add -b tide/wt-<uid> <path> <runBranch>
}
```

### TaskBranchName (already exists)

```go
// Source: /Users/justinsearles/Projects/tide/pkg/git/worktree.go:30
func TaskBranchName(taskUID string) string {
    return "tide/wt-" + taskUID
}
```

### EnvelopeOut.Git (already exists, plumbing for B3 HeadSHA)

```go
// Source: /Users/justinsearles/Projects/tide/pkg/dispatch/envelope.go:222
type GitOutput struct {
    HeadSHA string `json:"headSHA"`
}
// EnvelopeOut.Git *GitOutput `json:"git,omitempty"`
```

### tide-push runClone (exists, needs B5 additions at line 223)

```go
// Source: /Users/justinsearles/Projects/tide/cmd/tide-push/main.go:206
// After pkggit.Clone() succeeds, ADD:
//   pkggit.EnsureRunBranch(destDir, cfg.RunBranch)
//   exec.Command("git", "-C", destDir, "worktree", "add", worktreeDir, cfg.RunBranch)
```

### tide-push runPush (exists, worktree path at line 258)

```go
// Source: /Users/justinsearles/Projects/tide/cmd/tide-push/main.go:258
worktreeDir := filepath.Join(cfg.Workspace, "worktrees", "run-"+cfg.Branch)
repo, err := gogit.PlainOpen(worktreeDir)
// This PlainOpen will succeed ONLY if B5 provisioned the worktree in clone mode.
```

### buildCloneJob (exists, needs B6 addition for --run-branch)

```go
// Source: /Users/justinsearles/Projects/tide/internal/controller/push_helpers.go:262
// buildCloneJob currently passes: --mode=clone --repo-url=... --workspace=...
// ADD: "--run-branch=" + project.Status.Git.BranchName
```

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| git CLI | B3 CommitWorktree, B4 IntegrateTaskBranches, B5 worktree provision | Confirmed in claude-subagent image (cascade C, commit 314afd8) and tide-push image | system git | None required; git already installed |
| go-git v5 | EnsureRunBranch (B1), Clone, Push | Confirmed in go.mod | v5.19.0 | — |
| minikube (local) | DoD re-run | Available (parked state from Phase 10) | current | kind cluster |

**Note on parked cluster state:** Phase 10 VERIFICATION.md records `minikube context minikube; images rebuilt into the in-cluster daemon this session: controller d0fcfa26, tide-push 5ac1c39f, claude-subagent 6bc7dadd`. The B2 worktree change is on `main` but the claude-subagent image in the cluster does NOT yet include it. The DoD plan must rebuild and reload all three images after B3–B6 land.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go testing (standard) + Ginkgo v2 (integration) |
| Config file | `Makefile` targets `make test`, `make test-int` |
| Quick run command | `go test ./pkg/git/... ./internal/harness/... ./cmd/claude-subagent/... ./cmd/tide-push/... -count=1 -timeout 60s` |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind, ~355s inner wall) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SC-1 (B1 wired) | clone job calls EnsureRunBranch; run branch ref exists before executors | unit | `go test ./cmd/tide-push/... -run TestRunClone` | ❌ Wave 0 (new) |
| SC-2 (B3) | executor harness commits worktree changes; HeadSHA in EnvelopeOut | unit | `go test ./internal/harness/... -run TestCommitWorktree` | ❌ Wave 0 |
| SC-2 empty-diff | empty worktree → ExitCode=1 Result=empty-diff | unit | `go test ./internal/harness/... -run TestCommitWorktreeEmpty` | ❌ Wave 0 |
| SC-3 (B4) | IntegrateTaskBranches merges task branches into run branch | unit | `go test ./pkg/git/... -run TestIntegrateTaskBranches` | ❌ Wave 0 |
| SC-4 (B5) | tide-push clone mode provisions run worktree; push mode opens it | unit | `go test ./cmd/tide-push/... -run TestRunCloneProvisions` | ❌ Wave 0 |
| SC-5 (B6) | buildCloneJob passes --run-branch; integration triggered before final push | unit | `go test ./internal/controller/... -run TestBuildCloneJob` | ❌ Wave 0 |
| SC-6 | legitimate medium Complete + push (DoD) | live e2e (minikube) | `make acceptance-v1-smoke` or manual | ❌ Wave final |

### Wave 0 Gaps

- [ ] `pkg/git/integrate.go` + `pkg/git/integrate_test.go` — covers SC-3
- [ ] `internal/harness/commit.go` + `internal/harness/commit_test.go` — covers SC-2
- [ ] `cmd/tide-push` tests for B5 clone provisioning — covers SC-4
- [ ] `internal/controller/push_helpers_test.go` additions for buildCloneJob --run-branch arg — covers SC-6

*(All new; existing test infrastructure (`Makefile` targets, test suites) covers the framework — no framework install needed)*

### Sampling Rate

- **Per task commit:** `go test ./pkg/git/... ./internal/harness/... -count=1 -timeout 60s`
- **Per wave merge:** `make test` (unit only, <30s)
- **Phase gate:** full `make test-int` green before `/gsd-verify-work`

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | yes | Executor pod has no git creds (ART-04); only push Job has GIT_PAT |
| V5 Input Validation | yes | Commit message is orchestrator-generated (not user input); identity is hardcoded |
| V6 Cryptography | no | — |

### Known Threat Patterns for this Phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Executor pod attempting to push | Elevation of privilege | ART-04 enforcement: executor pod spec has no git-creds Secret mounted; B3 commit step uses local git with no remote auth |
| Empty commit leaking previous content in diff | Information disclosure | Empty-diff check before commit (B3 requirement) |
| Merge conflict poisoning (two tasks touch same file) | Tampering | `git merge --no-ff` fails on conflict, which surfaces as integration job failure and blocks the push; the controller marks the plan Failed |
| Stale run-branch in repeated runs | Tampering | EnsureRunBranch is idempotent — existing branch is untouched (no reset); `--force-with-lease` on push protects against unexpected remote state |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `Task.status` does not yet have a `Git.HeadSHA` field | Pitfall 3 | B6 wiring simpler if the field exists already; research verified by grep but didn't read full task_types.go |
| A2 | The medium sample's plan planner will generate tasks with `dependsOn` edges (not all in one wave) | Fork (b) integration timing | If all tasks are in one wave, per-wave vs at-push-boundary distinction doesn't matter for medium DoD; but the implementation must be correct for multi-wave plans |

**If this table is empty after planner review of A1:** All claims in this research were verified or cited — no user confirmation needed.

## Open Questions (RESOLVED)

1. **Does `Task.status` have a `Git.HeadSHA` field?**
   - **RESOLVED (2026-06-09, orchestrator grep): NO.** `api/v1alpha1/task_types.go` has no `Status.Git` field. `api/v1alpha1/project_types.go` has `GitStatus{BranchName, LastPushedSHA}` on `Project.Status.Git` (the push lease + run-branch source). Integration (B4) resolves branches via `TaskBranchName(taskUID)` directly; B6 may add `Task.Status.Git.HeadSHA` only if needed (currently unnecessary).
   - Original context: `EnvelopeOut.Git *GitOutput` carries `HeadSHA` [VERIFIED: pkg/dispatch/envelope.go:203]; `TerminationStub.HeadSHA` populated [VERIFIED: pkg/dispatch/envelope.go:346].

2. **Integration job vs integration step inside push job?**
   - **RESOLVED (2026-06-09, D-04 + per-wave fork fix): BOTH — step inside push job AND a per-wave trigger.** Per D-04 the push job runs `IntegrateTaskBranches` as a `--integrate-task-branches` pre-step before the planner-artifact boundary commit. Per D-02 (locked per-wave, user-confirmed thorough fix 2026-06-09) the PlanReconciler ALSO triggers integration of wave k-1's branches before wave k+1 task materialization, tracked via an `IntegratedWaves`-style status field so it doesn't re-fire every reconcile. Both reuse the same `IntegrateTaskBranches` + push-job infrastructure.
   - Original context: `triggerBoundaryPush` dispatches one Job per plan boundary; the push job already has PVC access + git CLI.

## Sources

### Primary (HIGH confidence)

- `/Users/justinsearles/Projects/tide/pkg/git/branch.go` — EnsureRunBranch implementation (B1, done)
- `/Users/justinsearles/Projects/tide/pkg/git/worktree.go` — AddWorktree implementation (B2, done)
- `/Users/justinsearles/Projects/tide/pkg/git/commit.go` — AddPath + Commit (existing)
- `/Users/justinsearles/Projects/tide/pkg/git/push.go` — Push with ForceWithLease (existing)
- `/Users/justinsearles/Projects/tide/cmd/tide-push/main.go` — runClone and runPush implementations
- `/Users/justinsearles/Projects/tide/internal/harness/worktree.go` — EnsureWorktree implementation
- `/Users/justinsearles/Projects/tide/cmd/claude-subagent/main.go` — shim wiring
- `/Users/justinsearles/Projects/tide/pkg/dispatch/envelope.go` — EnvelopeIn.Branch, EnvelopeOut.Git.HeadSHA
- `/Users/justinsearles/Projects/tide/internal/controller/push_helpers.go` — buildCloneJob, buildPushJob
- `/Users/justinsearles/Projects/tide/internal/controller/project_controller.go` — Status.Git.BranchName, clone job dispatch
- `/Users/justinsearles/Projects/tide/.planning/ROADMAP.md` — Phase 11 section, SC-3 requirement
- `/Users/justinsearles/Projects/tide/.planning/phases/10-task-execution-reliability-clone-idempotency-per-run-workspa/10-VERIFICATION.md` — B1–B6 component breakdown, cascade E analysis

### Secondary (MEDIUM confidence)

- `/Users/justinsearles/go/pkg/mod/github.com/go-git/go-git/v5@v5.19.0/repository.go:1789-1815` — Repository.Merge only supports FastForwardMerge, ErrUnsupportedMergeStrategy [VERIFIED in module cache]
- `/Users/justinsearles/go/pkg/mod/github.com/go-git/go-git/v5@v5.19.0/options.go:101-116` — MergeOptions, MergeStrategy, FastForwardMerge definition [VERIFIED in module cache]

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries confirmed from go.mod + module cache
- Architecture: HIGH — all component locations verified via grep + file reads on actual codebase
- Pitfalls: HIGH — cascade A/B/C/D/E from Phase 10 VERIFICATION.md are real runtime failures; go-git merge limitation verified from source
- Open forks: HIGH — each recommendation grounded in code evidence; no recommendations based solely on training data

**Research date:** 2026-06-09
**Valid until:** 2026-07-09 (go-git v5 is stable; medium-pace changes in this phase's territory)
