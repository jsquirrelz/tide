package git

import (
	"fmt"
	"path/filepath"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// AddWorktree creates a per-Task working tree rooted at the shared bare
// repo at repoPath, branched at branch, and returns the absolute filesystem
// path of the new working tree.
//
// D-B4 (CONTEXT.md): each executor Task pod's harness needs an independent
// working tree so two Tasks in the same wave can commit concurrently
// without racing on a shared .git/index. The naive approach — calling
// CLI `git worktree add` — is not exposed by go-git/v5 v5.19.0
// (RESEARCH.md §"Pitfall 1: go-git/v5 worktree API gap" — verified). The
// workaround is to PlainClone the local bare repo (with a file:// URL,
// auth-free) into a per-Task subdirectory. Each Task's working tree is
// thus a fresh non-bare clone of the bare repo on the same PVC: cheap
// (filesystem-local), correct (independent index), at the price of a
// non-shared object directory. On the per-Project RWX PVC, disk is the
// abundant resource — see PATTERNS.md / D-B4 PVC layout.
//
// taskUID is used as the worktree subdirectory name. repoPath is expected
// to be the absolute path of a bare repo (e.g. /workspace/repo.git);
// worktrees are placed at <parent>/worktrees/<taskUID>, mirroring the PVC
// layout committed to in CONTEXT.md D-B4:
//
//	/workspace/repo.git/                # shared bare clone
//	/workspace/worktrees/{task-uid}/    # per-Task working tree (this fn's output)
//
// branch is the ref to check out in the new working tree. The branch must
// already exist on the bare repo (created by an earlier orchestrator step,
// typically D-B6's tide/run-<project>-<unix> branch creation at the first
// push or initial commit).
func AddWorktree(repoPath, taskUID, branch string) (string, error) {
	if repoPath == "" {
		return "", fmt.Errorf("git worktree: empty repoPath")
	}
	if taskUID == "" {
		return "", fmt.Errorf("git worktree: empty taskUID")
	}
	if branch == "" {
		return "", fmt.Errorf("git worktree: empty branch")
	}

	worktreeDir := filepath.Join(filepath.Dir(repoPath), "worktrees", taskUID)

	opts := &gogit.CloneOptions{
		URL:          "file://" + repoPath,
		SingleBranch: true,
	}
	// Only set ReferenceName when caller specifies a branch other than the
	// remote's HEAD default; passing an empty/invalid ref name would cause
	// PlainClone to error before it can fall back to remote HEAD.
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
	}

	if _, err := gogit.PlainClone(worktreeDir, false /* not bare */, opts); err != nil {
		return "", fmt.Errorf("git worktree add %s @ %s: %w", taskUID, branch, err)
	}

	return worktreeDir, nil
}
