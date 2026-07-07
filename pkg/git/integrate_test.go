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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupIntegrateFixture builds a bare repo with EnsureRunBranch called and
// returns (bareDir, runBranch). The bare repo is seeded with two commits.
func setupIntegrateFixture(t *testing.T) (bareDir, runBranch string) {
	t.Helper()
	base := t.TempDir()
	bareDir, _ = seedBareRepo(t, base)

	runBranch = "tide/run-proj-test"
	if err := EnsureRunBranch(bareDir, runBranch); err != nil {
		t.Fatalf("EnsureRunBranch: %v", err)
	}
	return bareDir, runBranch
}

// addTaskBranch creates a per-task worktree from bareDir on runBranch, writes a
// file named filename with content to it, and commits. Returns the task branch
// name and the worktree directory path.
func addTaskBranch(t *testing.T, bareDir, runBranch, taskUID, filename, content string) (branchName, wtDir string) {
	t.Helper()
	var err error
	wtDir, err = AddWorktree(bareDir, taskUID, runBranch)
	if err != nil {
		t.Fatalf("AddWorktree(%s): %v", taskUID, err)
	}
	path := filepath.Join(wtDir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	gitRun(t, wtDir, "-c", "user.name=Test Author", "-c", "user.email=test@example.com", "add", filename)
	gitRun(t, wtDir, "-c", "user.name=Test Author", "-c", "user.email=test@example.com", "commit", "-m", "task: "+taskUID+" authored")
	return TaskBranchName(taskUID), wtDir
}

// TestIntegrateTaskBranches verifies the core SC-3 contract: two task branches
// touching different files both appear in the run-branch commit log after
// IntegrateTaskBranches is called.
func TestIntegrateTaskBranches(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	branchA, _ := addTaskBranch(t, bareDir, runBranch, "uid-task-a", "task-a.txt", "task a output\n")
	branchB, _ := addTaskBranch(t, bareDir, runBranch, "uid-task-b", "task-b.txt", "task b output\n")

	if err := IntegrateTaskBranches(bareDir, runBranch, []string{branchA, branchB}); err != nil {
		t.Fatalf("IntegrateTaskBranches: %v", err)
	}

	// Verify the run branch contains both task files.
	integrationDir := filepath.Join(filepath.Dir(bareDir), "worktrees", "run-"+runBranch)
	if _, err := os.Stat(filepath.Join(integrationDir, "task-a.txt")); err != nil {
		t.Errorf("task-a.txt missing from run branch after integration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(integrationDir, "task-b.txt")); err != nil {
		t.Errorf("task-b.txt missing from run branch after integration: %v", err)
	}

	// Verify the log contains both task commits and their merge commits.
	log := gitOut(t, integrationDir, "log", "--oneline")
	if !strings.Contains(log, "task: uid-task-a") {
		t.Errorf("run branch log does not contain task-a commit; log:\n%s", log)
	}
	if !strings.Contains(log, "task: uid-task-b") {
		t.Errorf("run branch log does not contain task-b commit; log:\n%s", log)
	}
	// --no-ff must produce at least one merge commit.
	if !strings.Contains(log, "tide: integrate") {
		t.Errorf("run branch log has no merge commits (--no-ff expected); log:\n%s", log)
	}
}

// TestIntegrateTaskBranchesIdempotent ensures a second call on already-integrated
// branches is a no-op and returns nil. The integration worktree already exists.
func TestIntegrateTaskBranchesIdempotent(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	branchA, _ := addTaskBranch(t, bareDir, runBranch, "uid-idempotent-a", "idem-a.txt", "idem a\n")

	// First call — integrates the branch.
	if err := IntegrateTaskBranches(bareDir, runBranch, []string{branchA}); err != nil {
		t.Fatalf("IntegrateTaskBranches (first): %v", err)
	}

	// Second call with the same list — already integrated; git merge emits
	// "Already up to date" and exits 0.
	if err := IntegrateTaskBranches(bareDir, runBranch, []string{branchA}); err != nil {
		t.Fatalf("IntegrateTaskBranches (second, idempotent): %v", err)
	}
}

// TestIntegrateTaskBranchesEmptyList verifies that an empty taskBranches list
// returns nil without touching the bare repo or provisioning any worktree.
func TestIntegrateTaskBranchesEmptyList(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	if err := IntegrateTaskBranches(bareDir, runBranch, []string{}); err != nil {
		t.Fatalf("IntegrateTaskBranches(empty): expected nil, got %v", err)
	}

	// No integration worktree should have been created.
	integrationDir := filepath.Join(filepath.Dir(bareDir), "worktrees", "run-"+runBranch)
	if _, err := os.Stat(integrationDir); err == nil {
		t.Errorf("integration worktree was created for empty taskBranches list: %s", integrationDir)
	}
}

// TestIntegrateTaskBranchesConflictFails verifies that two branches which each
// modified the same line in the same file surface as a non-nil error (the merge
// conflict is not silently discarded).
func TestIntegrateTaskBranchesConflictFails(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	// Both tasks write to the same file with conflicting content.
	branchA, _ := addTaskBranch(t, bareDir, runBranch, "uid-conflict-a", "conflict.txt", "side A\n")
	branchB, _ := addTaskBranch(t, bareDir, runBranch, "uid-conflict-b", "conflict.txt", "side B\n")

	err := IntegrateTaskBranches(bareDir, runBranch, []string{branchA, branchB})
	if err == nil {
		t.Fatal("IntegrateTaskBranches: expected non-nil error for conflicting branches, got nil")
	}
}

// TestIntegrateTaskBranchesConflictClassifiedAndWorktreeClean verifies Phase 34
// D-09/D-10/Pitfall 1: a conflict returns a *MergeConflictError naming both
// branches, AND the integration worktree is left with no in-progress merge
// (MERGE_HEAD cleared) so a subsequent IntegrateTaskBranches call on the same
// worktree does not fail with "You have not concluded your merge".
func TestIntegrateTaskBranchesConflictClassifiedAndWorktreeClean(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	branchA, _ := addTaskBranch(t, bareDir, runBranch, "uid-conflict-c1", "conflict2.txt", "side A\n")
	branchB, _ := addTaskBranch(t, bareDir, runBranch, "uid-conflict-c2", "conflict2.txt", "side B\n")

	err := IntegrateTaskBranches(bareDir, runBranch, []string{branchA, branchB})
	if err == nil {
		t.Fatal("expected non-nil error for conflicting branches")
	}

	var mce *MergeConflictError
	if !errors.As(err, &mce) {
		t.Fatalf("expected *MergeConflictError, got %T: %v", err, err)
	}
	if mce.TaskBranch != branchB {
		t.Errorf("MergeConflictError.TaskBranch = %q, want %q (the branch being merged when the conflict hit)", mce.TaskBranch, branchB)
	}
	if mce.RunBranch != runBranch {
		t.Errorf("MergeConflictError.RunBranch = %q, want %q", mce.RunBranch, runBranch)
	}

	// Worktree must be clean: a fresh IntegrateTaskBranches call reusing the
	// same worktree (a third, non-conflicting branch) must succeed — proving
	// no MERGE_HEAD lingered from the aborted conflict above.
	branchC, _ := addTaskBranch(t, bareDir, runBranch, "uid-conflict-c3", "unrelated.txt", "fine\n")
	if err := IntegrateTaskBranches(bareDir, runBranch, []string{branchC}); err != nil {
		t.Fatalf("IntegrateTaskBranches after conflict-abort: expected clean worktree to accept a new merge, got: %v", err)
	}
}

// TestIntegrateTaskBranchesSelfHealsLeftoverMerge verifies the defensive
// `merge --abort` at integration start (Pitfall 1): a manufactured
// in-progress merge state (leftover MERGE_HEAD, as if a prior Job crashed
// mid-merge) does not break a fresh IntegrateTaskBranches call.
func TestIntegrateTaskBranchesSelfHealsLeftoverMerge(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	branchA, _ := addTaskBranch(t, bareDir, runBranch, "uid-selfheal-a", "selfheal-a.txt", "a\n")
	branchB, _ := addTaskBranch(t, bareDir, runBranch, "uid-selfheal-b", "selfheal-b.txt", "b\n")

	// Provision the integration worktree directly (IntegrateTaskBranches with
	// an empty list short-circuits before provisioning anything).
	integrationDir := filepath.Join(filepath.Dir(bareDir), "worktrees", "run-"+runBranch)
	gitRun(t, bareDir, "worktree", "add", integrationDir, runBranch)

	// Start (but do not finish) a merge, leaving MERGE_HEAD behind — simulate
	// a crashed prior Job. --no-commit stops before the commit so MERGE_HEAD
	// persists even for a clean fast-forwardable merge.
	gitRun(t, integrationDir, "-c", "user.name=Test Author", "-c", "user.email=test@example.com",
		"merge", "--no-ff", "--no-commit", branchA)
	if _, err := os.Stat(filepath.Join(integrationDir, ".git")); err != nil {
		t.Fatalf("integration worktree missing: %v", err)
	}

	// A fresh IntegrateTaskBranches call must self-heal (defensive abort at
	// start) rather than failing with "you have not concluded your merge".
	if err := IntegrateTaskBranches(bareDir, runBranch, []string{branchB}); err != nil {
		t.Fatalf("IntegrateTaskBranches did not self-heal a leftover MERGE_HEAD: %v", err)
	}
}

// TestIntegrateTaskBranchesTransientFailureNotClassifiedAsConflict verifies a
// merge failure that is NOT a content conflict (a nonexistent branch ref)
// returns a plain wrapped error, not *MergeConflictError — D-09's
// classify-don't-retry split must not misclassify infra/ref failures.
func TestIntegrateTaskBranchesTransientFailureNotClassifiedAsConflict(t *testing.T) {
	bareDir, runBranch := setupIntegrateFixture(t)

	err := IntegrateTaskBranches(bareDir, runBranch, []string{"tide/wt-does-not-exist"})
	if err == nil {
		t.Fatal("expected non-nil error for a nonexistent branch")
	}
	var mce *MergeConflictError
	if errors.As(err, &mce) {
		t.Fatalf("nonexistent-branch failure misclassified as *MergeConflictError: %v", err)
	}
}
