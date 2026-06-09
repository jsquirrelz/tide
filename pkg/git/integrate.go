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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IntegrateTaskBranches merges each branch in taskBranches into runBranch in
// the provided order, using `git merge --no-ff` via the git CLI inside a
// per-run integration worktree at <bareRepoParent>/worktrees/run-<runBranch>/.
//
// Design constraints (D-01, 11-CONTEXT.md):
//   - go-git v5.19.0 Repository.Merge() only supports FastForwardMerge and
//     returns ErrUnsupportedMergeStrategy for any other strategy. Two same-wave
//     task branches that independently authored content need a three-way merge
//     that go-git cannot perform. The git CLI handles this correctly.
//   - --no-ff produces a merge commit for every task branch, making the
//     wave-parallelism topology explicit in the commit graph.
//
// Precondition: EnsureRunBranch(bareRepoPath, runBranch) must be called before
// IntegrateTaskBranches so the run branch ref exists in the bare repo. If the
// run branch does not exist, git worktree add will fail.
//
// The integration worktree at worktrees/run-<runBranch>/ is the same directory
// that tide-push --mode=push opens via gogit.PlainOpen (cmd/tide-push/main.go).
// Both must agree on: filepath.Join(workspace, "worktrees", "run-"+branch)
// where workspace = filepath.Dir(bareRepoPath).
//
// If taskBranches is empty, IntegrateTaskBranches returns nil immediately and
// does not provision an integration worktree.
//
// A merge conflict surfaces as a non-nil error (git merge exits non-zero on
// conflict; CombinedOutput captures stderr; the error is returned to the
// caller). The controller marks the push job Failed, blocking the push.
//
// The bot identity used for merge commits defaults to:
//   - TIDE_BOT_NAME env (default "TIDE Bot")
//   - TIDE_BOT_EMAIL env (default "tide-bot@tideproject.k8s")
//
// These match tideBotSignature() in cmd/tide-push/main.go (D-03).
func IntegrateTaskBranches(bareRepoPath, runBranch string, taskBranches []string) error {
	if len(taskBranches) == 0 {
		return nil
	}

	integrationDir := filepath.Join(filepath.Dir(bareRepoPath), "worktrees", "run-"+runBranch)

	// Provision the integration worktree idempotently.
	// Check for .git file — a linked worktree has a .git FILE (not directory).
	if _, err := os.Stat(filepath.Join(integrationDir, ".git")); err != nil {
		out, werr := exec.Command("git", "-C", bareRepoPath, "worktree", "add",
			integrationDir, runBranch).CombinedOutput()
		if werr != nil {
			msg := string(out)
			// Treat "already checked out" or "already exists" as idempotent no-op;
			// a prior call or tide-push clone mode may have provisioned the worktree.
			if strings.Contains(msg, "already checked out") || strings.Contains(msg, "already exists") {
				// worktree exists — continue
			} else {
				return fmt.Errorf("IntegrateTaskBranches: provision integration worktree at %s: %w: %s",
					integrationDir, werr, msg)
			}
		}
	}

	botName := os.Getenv("TIDE_BOT_NAME")
	if botName == "" {
		botName = "TIDE Bot"
	}
	botEmail := os.Getenv("TIDE_BOT_EMAIL")
	if botEmail == "" {
		botEmail = "tide-bot@tideproject.k8s"
	}

	for _, taskBranch := range taskBranches {
		msg := fmt.Sprintf("tide: integrate %s", taskBranch)
		args := []string{
			"-c", "user.name=" + botName,
			"-c", "user.email=" + botEmail,
			"-C", integrationDir,
			"merge", "--no-ff", taskBranch, "-m", msg,
		}
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("IntegrateTaskBranches: merge %s → %s: %w: %s",
				taskBranch, runBranch, err, string(out))
		}
	}

	return nil
}
