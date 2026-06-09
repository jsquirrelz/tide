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

package harness

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
)

// CommitWorktree stages all changes in worktreeDir and commits them under a
// TIDE Bot identity. Returns the HEAD SHA after the commit, an isEmpty flag
// (true when there are no changes to commit), and any error.
//
// Identity is read from TIDE_BOT_NAME / TIDE_BOT_EMAIL env vars, falling back
// to "TIDE Bot" / "tide-bot@tideproject.k8s" (matching tideBotSignature() in
// cmd/tide-push/main.go, per D-03).
//
// Empty-diff policy (D-03 / SC-2): if git status --porcelain reports nothing,
// CommitWorktree returns (ZeroHash, true, nil) — no empty commit is created.
// The caller is expected to translate isEmpty=true into an explicit task
// failure (ExitCode=1, Result="empty-diff").
func CommitWorktree(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
	// Step 1: check for changes.
	statusOut, err := exec.Command("git", "-C", worktreeDir, "status", "--porcelain").Output()
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git status: %w", err)
	}
	if len(strings.TrimSpace(string(statusOut))) == 0 {
		return plumbing.ZeroHash, true, nil
	}

	// Step 2: stage all changes.
	if addOut, addErr := exec.Command("git", "-C", worktreeDir, "add", "-A").CombinedOutput(); addErr != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git add -A: %w: %s", addErr, addOut)
	}

	// Step 3: commit with TIDE identity.
	botName := os.Getenv("TIDE_BOT_NAME")
	if botName == "" {
		botName = "TIDE Bot"
	}
	botEmail := os.Getenv("TIDE_BOT_EMAIL")
	if botEmail == "" {
		botEmail = "tide-bot@tideproject.k8s"
	}
	msg := fmt.Sprintf("tide: task %s authored", taskUID)
	// Note: -c flags must precede -C and the subcommand.
	commitArgs := []string{
		"-c", "user.name=" + botName,
		"-c", "user.email=" + botEmail,
		"-C", worktreeDir,
		"commit", "-m", msg,
	}
	if commitOut, commitErr := exec.Command("git", commitArgs...).CombinedOutput(); commitErr != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: git commit: %w: %s", commitErr, commitOut)
	}

	// Step 4: read HEAD SHA.
	headOut, err := exec.Command("git", "-C", worktreeDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return plumbing.ZeroHash, false, fmt.Errorf("CommitWorktree: rev-parse HEAD: %w", err)
	}
	return plumbing.NewHash(strings.TrimSpace(string(headOut))), false, nil
}
