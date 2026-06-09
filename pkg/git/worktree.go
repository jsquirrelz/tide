/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// TaskBranchName returns the per-Task branch name for taskUID. Each executor
// Task authors on its OWN branch off the run branch; a distinct branch per
// task is required because a single branch cannot be checked out in two linked
// worktrees at once (D-B4 wave parallelism). The per-task branches are merged
// into the run branch at the integration/push boundary (Phase 10 Option B).
func TaskBranchName(taskUID string) string {
	return "tide/wt-" + taskUID
}

// AddWorktree creates a per-Task linked working tree rooted at the shared bare
// repo at repoPath, on a fresh per-Task branch ([TaskBranchName]) forked from
// runBranch, and returns the absolute filesystem path of the new working tree.
//
// D-B4 (CONTEXT.md): each executor Task pod's harness needs an independent
// working tree so two Tasks in the same wave can commit concurrently without
// racing on a shared .git/index. This uses the real `git worktree add -b` CLI:
// go-git/v5 does not expose linked worktrees (RESEARCH.md §"Pitfall 1"). A
// linked worktree shares repoPath's object database but owns an independent
// index and HEAD, so concurrent commits do not race. The earlier go-git
// PlainClone-from-file:// workaround was replaced in Phase 10 (Option B):
// PlainClone over file:// shells out to git-upload-pack anyway (the path that
// surfaced the git-missing / dubious-ownership / missing-ref cascade), and it
// cloned a (nonexistent) run-branch ref instead of creating a task branch.
//
// taskUID is the worktree subdirectory name. repoPath is the absolute path of a
// bare repo (e.g. /workspace/repo.git); worktrees are placed at
// <parent>/worktrees/<taskUID>, mirroring the PVC layout (CONTEXT.md D-B4):
//
//	/workspace/repo.git/                # shared bare clone
//	/workspace/worktrees/{task-uid}/    # per-Task working tree (this fn's output)
//
// runBranch must already exist on the bare repo (created by [EnsureRunBranch]
// in the clone Job — D-B6). The new working tree is checked out on a fresh
// branch TaskBranchName(taskUID) pointing at runBranch's tip.
func AddWorktree(repoPath, taskUID, runBranch string) (string, error) {
	if repoPath == "" {
		return "", fmt.Errorf("git worktree: empty repoPath")
	}
	if taskUID == "" {
		return "", fmt.Errorf("git worktree: empty taskUID")
	}
	if runBranch == "" {
		return "", fmt.Errorf("git worktree: empty branch")
	}

	worktreeDir := filepath.Join(filepath.Dir(repoPath), "worktrees", taskUID)
	taskBranch := TaskBranchName(taskUID)

	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "-b", taskBranch, worktreeDir, runBranch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add %s @ %s: %w: %s", taskUID, runBranch, err, string(out))
	}

	return worktreeDir, nil
}
