# Phase 11: Executor Author→Commit→Push Lifecycle - Context

**Gathered:** 2026-06-09
**Status:** Ready for planning
**Source:** Research-first + fork checkpoint (4 open forks resolved with user sign-off; no discuss-phase)

<domain>
## Phase Boundary

Close the seam between the two parallel models built in Phase 3 but never connected end-to-end (artifact-file vs worktree-authoring). Make a Task executor's authored code reach the remote. Approach is **Option B** (proper end-state, decided 2026-06-08). Foundation B1 (`pkg/git.EnsureRunBranch`, e880a5a) + B2 (real linked worktrees via `git worktree add -b`, f639340) already landed on `main`. This phase executes B3–B6 and owns the legitimate medium Complete + push + the v1.0.0 retag unblock.

**Out of scope:** schema-constrained MCP `emit_child` airtight child-CRD fix (later phase, blocked by `claude --bare`); editable/re-appliable "envelopes as first-class artifacts" DX.
</domain>

<decisions>
## Implementation Decisions

These four decisions resolve the ROADMAP's "Open forks for planning/research" (a–d). All four locked with user sign-off 2026-06-09 after code-grounded research (11-RESEARCH.md). Every plan touching the relevant component MUST cite the matching D-ID in its `must_haves`/`truths`.

### D-01 — Integration mechanism: `git merge --no-ff` via git CLI (fork a)
B4 integration uses `git merge --no-ff <taskBranch>` through the **git CLI** inside a per-run worktree, NOT go-git. Forced: go-git v5.19.0 `Repository.Merge()` only supports `FastForwardMerge` (`ErrUnsupportedMergeStrategy` otherwise) — two same-wave siblings touching overlapping paths need a three-way merge it cannot perform. The git CLI is already in both the `tide-push` and `claude-subagent` images (cascade C, 314afd8). New func: `pkg/git.IntegrateTaskBranches(bareRepoPath, runBranch string, taskBranches []string) error`. `--no-ff` keeps the wave-parallelism topology explicit in the log.

### D-02 — Integration timing: per-wave, before next wave dispatches (fork b)
Integration runs after each wave's tasks complete, BEFORE any wave k+1 executor is dispatched. Forced by SC-3 ("dependents see their dependencies' commits"): `AddWorktree` forks the per-task worktree from the run-branch tip, so wave-k commits must already be on the run branch when wave-(k+1) worktrees are created. Controller treats integration as a precondition for dispatching the next wave. Rejected: all-at-push-boundary (violates dependency visibility — downstream worktree forks from the bare clone tip and never sees upstream code).

### D-03 — Commit identity + empty-diff handling (fork c)
Harness commit step (B3) commits with identity `TIDE Bot <tide-bot@tideproject.k8s>`, matching existing `tideBotSignature()` in `cmd/tide-push/main.go`. Optionally read `TIDE_BOT_NAME` / `TIDE_BOT_EMAIL` env (this hardcoded pair as fallback) so it is pod-env-overridable WITHOUT a new Helm values key. Empty-diff: if `git status --porcelain` is empty after claude exits, the harness creates NO commit and returns `isEmpty=true`; the `claude-subagent` shim translates to `ExitCode=1, Result="empty-diff", Reason="executor produced no changes in worktree"` → task marked Failed (retriable). SC-2: a task that authored nothing is an explicit failure, never a false success. No empty commits.

### D-04 — Commit streams: separate, unified in one final push (fork d)
Keep the planner-artifact stream and the executor run-branch stream logically SEPARATE but UNIFIED into one push. The push job (`tide-push --mode=push`, gated by an `--integrate-task-branches` flag): (1) runs `IntegrateTaskBranches` to merge per-task branches into the run worktree, (2) stages + commits planner artifacts as the boundary commit on top, (3) pushes everything once with the D-B6 `--force-with-lease` lease against `Status.Git.LastPushedSHA`. Rejected: separate push jobs (lease race) and `tide-push` committing executor artifacts directly (breaks the per-task worktree model).

### Claude's Discretion
- Exact trigger wiring for per-wave integration (wave reconciler post-wave hook vs plan reconciler) — planner picks, grounded in existing controller structure.
- Whether to add a `Task.Status.Git.HeadSHA` field (currently absent; integration resolves branches via `TaskBranchName(taskUID)` so it is optional) — planner decides during B6.
- Internal file layout (e.g. `internal/harness/commit.go` vs existing harness package) and helper signatures.
- DoD re-run sequencing details (which sample, log capture paths).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec & gate evidence
- `.planning/ROADMAP.md` (Phase 11 section) — Option B, SC-1..SC-6, B1–B6 breakdown, scope boundaries
- `.planning/phases/11-executor-author-commit-push-lifecycle/11-RESEARCH.md` — code-grounded analysis, fork recs, 7 pitfalls, exact file:line touchpoints
- `.planning/phases/10-task-execution-reliability-clone-idempotency-per-run-workspa/10-VERIFICATION.md` — BLOCKED gate, 4-cascade analysis, executor-lifecycle design, image-rebuild note

### Code touchpoints (verified to exist)
- `pkg/git/branch.go` — `EnsureRunBranch` (B1, done; zero production callers yet — B5 adds the call in `runClone`)
- `pkg/git/worktree.go` — `AddWorktree` (B2, done), `TaskBranchName(taskUID)`
- `pkg/dispatch/envelope.go` — `EnvelopeOut.Git *GitOutput` / `GitOutput.HeadSHA` (plumbing for B3)
- `cmd/claude-subagent/main.go` — shim `run()` (B3 commit-step + empty-diff wiring)
- `internal/harness/` — harness commit step (B3)
- `cmd/tide-push/main.go` — `runClone` (~L206, B5 adds EnsureRunBranch + run-worktree provision), `runPush` (~L258 PlainOpen of `worktrees/run-<branch>/`), `tideBotSignature()` (~L127)
- `internal/controller/push_helpers.go` — `buildCloneJob` (~L262, B6 adds `--run-branch=` from `Project.Status.Git.BranchName`)
- `api/v1alpha1/project_types.go` — `GitStatus{BranchName, LastPushedSHA}` (B5 lease source, B6 run-branch source); `Task.Status` has NO Git field (B6 may add `HeadSHA`, optional)
</canonical_refs>

<specifics>
## Specific Ideas

- go-git v5.19.0 merge ceiling is the load-bearing constraint behind D-01 — do not attempt go-git `Repository.Merge()` for non-FF integration.
- Image staleness is a release-blocker, not an aside: the in-cluster `claude-subagent` predates B2 (f639340). The DoD plan (final wave) MUST rebuild + reload `controller`, `tide-push`, `claude-subagent` (minikube image load or equivalent) before the medium re-run.
- DoD success = all descendants Succeeded, `tide/run-*` pushed to in-cluster `http://` remote with real authored code, `costSpentCents > 0` under cap → flips Phase 8 SC-2 DEFERRED→PASS and UNBLOCKS v1.0.0 retag (retag/push stays user-gated, confirm-only per MEMORY.md 2026-06-03 option a).
</specifics>

<deferred>
## Deferred Ideas

- Schema-constrained MCP `emit_child` airtight child-CRD fix — separate later phase (blocked by `claude --bare`).
- Editable/re-appliable "envelopes as first-class artifacts" DX.
- New Helm values key for bot identity — intentionally avoided at v1 (D-03 uses env override with hardcoded fallback instead).
</deferred>

---

*Phase: 11-executor-author-commit-push-lifecycle*
*Context: research-first + fork checkpoint, 2026-06-09*
