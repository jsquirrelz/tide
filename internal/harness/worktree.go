package harness

import (
	"fmt"
	"os"
	"path/filepath"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// addWorktreeFunc is the package-level test seam — production code resolves
// it to pkggit.AddWorktree (D-B4 worktrees-per-Task). Tests override the var
// to record invocation counts without spinning up a real bare repo.
var addWorktreeFunc = pkggit.AddWorktree

// EnsureWorktree prepares the per-Task working tree on the shared per-Project
// PVC before the subagent's Run() executes its prompt (which assumes a
// `cd /workspace/worktrees/<task-uid>/` step succeeds).
//
// Role-based dispatch (D-A4 / D-B4):
//
//   - Planner Tasks (Role != "executor") emit artifacts to
//     /workspace/artifacts/... only; they never touch the working repo, so
//     they do NOT need a worktree. EnsureWorktree short-circuits returning
//     nil for these — preserving the Phase 2 D-A4 invariant that planners
//     have zero K8s verbs and zero repo-write surface.
//   - Executor Tasks (Role == "executor") get an isolated working tree at
//     <workspaceRoot>/worktrees/<TaskUID>/ via pkggit.AddWorktree (D-B4),
//     so two executors in the same wave can commit concurrently without
//     racing on a shared .git/index.
//
// EnsureWorktree is idempotent: a re-call for a TaskUID that already has a
// valid .git in its worktree directory returns nil without re-cloning. This
// keeps the shim's pre-Run hook safe to invoke from a re-spawned subagent
// Pod after a chaos-resume restart.
//
// On the executor path, EnsureWorktree fails fast if <workspaceRoot>/repo.git
// is missing — the shared bare clone is a precondition that the Project
// reconciler establishes before any Task Job dispatches; its absence is a
// surface-able error (D-B6 first-push or initial-clone failure).
func EnsureWorktree(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error {
	// D-A4 planner short-circuit. Anything not Role=="executor" (planner,
	// hypothetical reviewer, etc.) returns immediately.
	if in.Role != "executor" {
		return nil
	}

	bareRepoPath := filepath.Join(workspaceRoot, "repo.git")
	if _, err := os.Stat(bareRepoPath); err != nil {
		return fmt.Errorf("EnsureWorktree: bare repo at %s missing — Project clone must complete first: %w", bareRepoPath, err)
	}

	worktreeDir := filepath.Join(workspaceRoot, "worktrees", in.TaskUID)
	// Idempotent re-call: if the worktree dir already carries a valid
	// .git (file for go-git PlainClone non-bare, or dir for git-CLI), do
	// nothing. The .git path exists for both go-git PlainClone (writes a
	// .git directory in a non-bare clone) and CLI git worktree (writes a
	// .git file pointing at the bare repo's worktrees/ subdir).
	if _, err := os.Stat(filepath.Join(worktreeDir, ".git")); err == nil {
		return nil
	}

	if _, err := addWorktreeFunc(bareRepoPath, in.TaskUID, branch); err != nil {
		return fmt.Errorf("EnsureWorktree: add worktree taskUID=%s branch=%s: %w", in.TaskUID, branch, err)
	}
	return nil
}
