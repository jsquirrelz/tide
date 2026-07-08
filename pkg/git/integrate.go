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
// Precondition: EnsureRunBranch(bareRepoPath, runBranch, baseRef) must be
// called before IntegrateTaskBranches so the run branch ref exists in the bare
// repo. If the run branch does not exist, git worktree add will fail.
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
// The merge-commit identity comes from AgentIdentity() — read from
// TIDE_AGENT_NAME / TIDE_AGENT_EMAIL env vars, falling back to the compiled
// default "TIDE Agent <tide-agent@tideproject.k8s>". This is the same source
// the harness task commit and the tide-push boundary commit use (D-04 /
// SIGN-01).
//
// Phase 34 (D-09/D-10, Pitfall 1): on ANY merge failure, a defensive
// `git merge --abort` runs before returning so a lingering MERGE_HEAD never
// persists on the shared integration worktree (the worktree lives on the
// project PVC and is reused by every subsequent Job — a leftover
// in-progress merge would break the next retry with an unrelated "You have
// not concluded your merge" error, silently reclassifying a conflict as a
// generic failure). A genuine content conflict (verified marker strings,
// git 2.54) returns the typed *MergeConflictError so callers (cmd/tide-push)
// can classify conflict vs transient without re-parsing error text.
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

	// Defensive self-heal (Pitfall 1): a prior Job that crashed mid-merge may
	// have left MERGE_HEAD set on the shared worktree. Clear it before this
	// Job's own merges begin; failure here is expected+tolerated when there
	// was nothing to abort.
	_, _ = exec.Command("git", "-C", integrationDir, "merge", "--abort").CombinedOutput()

	agentName, agentEmail := AgentIdentity()

	for _, taskBranch := range taskBranches {
		msg := fmt.Sprintf("tide: integrate %s", taskBranch)
		args := []string{
			"-c", "user.name=" + agentName,
			"-c", "user.email=" + agentEmail,
			"-C", integrationDir,
			"merge", "--no-ff", taskBranch, "-m", msg,
		}
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			outStr := string(out)
			// ALWAYS abort before returning — a lingering in-progress merge
			// breaks every subsequent Job differently (Pitfall 1). Error
			// ignored: nothing to abort is the common non-conflict case.
			_, _ = exec.Command("git", "-C", integrationDir, "merge", "--abort").CombinedOutput()

			if isMergeConflictOutput(outStr) {
				return &MergeConflictError{
					TaskBranch: taskBranch,
					RunBranch:  runBranch,
					Output:     outStr,
				}
			}
			return fmt.Errorf("IntegrateTaskBranches: merge %s → %s: %w: %s",
				taskBranch, runBranch, err, outStr)
		}
	}

	return nil
}

// MergeConflictError is returned by IntegrateTaskBranches when `git merge
// --no-ff` fails with a genuine content conflict (as opposed to a transient
// infra failure — network, permissions, missing ref). Phase 34 D-09/D-10:
// callers classify on this type (errors.As) to distinguish "content problem,
// human needed" from "retry me" without re-parsing error text.
type MergeConflictError struct {
	// TaskBranch is the branch that failed to merge cleanly.
	TaskBranch string
	// RunBranch is the branch it was being merged into.
	RunBranch string
	// Output is the combined stdout+stderr of the failed `git merge` command.
	Output string
}

func (e *MergeConflictError) Error() string {
	return fmt.Sprintf("merge conflict integrating %s into %s: %s", e.TaskBranch, e.RunBranch, e.Output)
}

// isMergeConflictOutput conservatively string-matches git's conflict markers
// (verified on git 2.54, RESEARCH Pattern 5): "CONFLICT (" appears for every
// conflict type (content/rename/modify-delete/etc.), and "Automatic merge
// failed" is git's summary line printed alongside it. Matching either is
// sufficient and mirrors the existing classifyPushError conservative
// string-matching style in cmd/tide-push/main.go.
func isMergeConflictOutput(output string) bool {
	return strings.Contains(output, "CONFLICT (") || strings.Contains(output, "Automatic merge failed")
}
