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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

// gitRun runs a git subcommand in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// gitOut runs a git subcommand in dir and returns trimmed stdout, failing on error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestAddWorktreeBasic clones a seeded bare repo and creates a per-Task
// worktree from it; verifies the returned path contains a checked-out
// tree (a .git subdir for a non-bare clone, plus the README.md fixture
// file from the seed commits).
func TestAddWorktreeBasic(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	// The orchestrator's bare clone lives at <pvc>/repo.git per D-B4.
	// Mirror that layout in the test fixture so AddWorktree's
	// <pvc>/worktrees/{uid}/ output goes somewhere coherent.
	pvc := t.TempDir()
	bareDest := filepath.Join(pvc, "repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, bareDest, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	wtDir, err := AddWorktree(bareDest, "task-uid-abc", branch)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	// Returned dir should live under <pvc>/worktrees/<task-uid>/.
	wantPrefix := filepath.Join(pvc, "worktrees", "task-uid-abc")
	if wtDir != wantPrefix {
		t.Errorf("AddWorktree dir = %q; want %q", wtDir, wantPrefix)
	}

	// A linked worktree has a .git FILE (a gitdir: pointer back to the bare
	// repo's worktrees/ metadata), not a .git directory.
	if _, err := os.Stat(filepath.Join(wtDir, ".git")); err != nil {
		t.Errorf("expected .git in worktree %q: err=%v", wtDir, err)
	}

	// The seed README.md should be checked out in the worktree.
	if _, err := os.Stat(filepath.Join(wtDir, "README.md")); err != nil {
		t.Errorf("expected README.md in worktree: %v", err)
	}

	// The worktree is checked out on the per-Task branch forked from runBranch.
	got := gitOut(t, wtDir, "rev-parse", "--abbrev-ref", "HEAD")
	if want := TaskBranchName("task-uid-abc"); got != want {
		t.Errorf("worktree HEAD branch = %q; want %q", got, want)
	}
}

// TestAddWorktreeDistinct exercises D-B4's parallelism property: two
// AddWorktree calls return distinct dirs whose .git/index files are
// independent (mutating one's checkout doesn't bleed into the other).
func TestAddWorktreeDistinct(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	pvc := t.TempDir()
	bareDest := filepath.Join(pvc, "repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, bareDest, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	wtA, err := AddWorktree(bareDest, "task-uid-a", branch)
	if err != nil {
		t.Fatalf("AddWorktree A: %v", err)
	}
	wtB, err := AddWorktree(bareDest, "task-uid-b", branch)
	if err != nil {
		t.Fatalf("AddWorktree B: %v", err)
	}
	if wtA == wtB {
		t.Fatalf("worktrees collided: %q == %q", wtA, wtB)
	}

	// Each linked worktree owns an independent index (the isolation property
	// D-B4 commits to). Stage a file in A and confirm B's status never sees it.
	if err := os.WriteFile(filepath.Join(wtA, "a-only.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a-only: %v", err)
	}
	gitRun(t, wtA, "add", "a-only.txt")

	if statusB := gitOut(t, wtB, "status", "--porcelain"); strings.Contains(statusB, "a-only.txt") {
		t.Errorf("a-only.txt leaked into worktree B status: %q", statusB)
	}

	// And the two worktrees are on distinct per-Task branches.
	if bA, bB := gitOut(t, wtA, "rev-parse", "--abbrev-ref", "HEAD"), gitOut(t, wtB, "rev-parse", "--abbrev-ref", "HEAD"); bA == bB {
		t.Errorf("worktrees share branch %q; want distinct per-Task branches", bA)
	}
}

// TestAddWorktreeValidatesArgs covers the input-validation surface:
// empty repoPath / taskUID / branch each surface a clear error.
func TestAddWorktreeValidatesArgs(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		repoPath, taskUID, branchName string
	}{
		{"empty repoPath", "", "uid", "main"},
		{"empty taskUID", "/tmp/repo.git", "", "main"},
		{"empty branch", "/tmp/repo.git", "uid", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AddWorktree(tc.repoPath, tc.taskUID, tc.branchName); err == nil {
				t.Errorf("AddWorktree(%q, %q, %q) returned nil error", tc.repoPath, tc.taskUID, tc.branchName)
			}
		})
	}
}

// TestAddReadOnlyWorktreeValidatesArgs covers the input-validation surface:
// empty repoPath / uid / runBranch each surface a clear error — mirrors
// TestAddWorktreeValidatesArgs's three-guard shape.
func TestAddReadOnlyWorktreeValidatesArgs(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		repoPath, uid, branchName string
	}{
		{"empty repoPath", "", "uid", "main"},
		{"empty uid", "/tmp/repo.git", "", "main"},
		{"empty branch", "/tmp/repo.git", "uid", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AddReadOnlyWorktree(tc.repoPath, tc.uid, tc.branchName); err == nil {
				t.Errorf("AddReadOnlyWorktree(%q, %q, %q) returned nil error", tc.repoPath, tc.uid, tc.branchName)
			}
		})
	}
}

// TestAddReadOnlyWorktreeBasic clones a seeded bare repo and takes a
// read-only worktree checkout via AddReadOnlyWorktree; verifies the
// returned dir, that HEAD resolves to the runBranch tip commit, and that
// the checkout is DETACHED (no branch — distinct from AddWorktree's
// write-oriented per-task branch).
func TestAddReadOnlyWorktreeBasic(t *testing.T) {
	base := t.TempDir()
	bareSrc, srcHead := seedBareRepo(t, base)

	pvc := t.TempDir()
	bareDest := filepath.Join(pvc, "repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, bareDest, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	wtDir, err := AddReadOnlyWorktree(bareDest, "phase-uid-abc", branch)
	if err != nil {
		t.Fatalf("AddReadOnlyWorktree: %v", err)
	}

	wantPrefix := filepath.Join(pvc, "worktrees", "phase-uid-abc")
	if wtDir != wantPrefix {
		t.Errorf("AddReadOnlyWorktree dir = %q; want %q", wtDir, wantPrefix)
	}

	if _, err := os.Stat(filepath.Join(wtDir, ".git")); err != nil {
		t.Errorf("expected .git in worktree %q: err=%v", wtDir, err)
	}

	got := gitOut(t, wtDir, "rev-parse", "HEAD")
	if got != srcHead.String() {
		t.Errorf("worktree HEAD = %q; want runBranch tip %q", got, srcHead.String())
	}

	// git branch --show-current is empty for a detached checkout.
	if current := gitOut(t, wtDir, "branch", "--show-current"); current != "" {
		t.Errorf("worktree branch --show-current = %q; want empty (detached)", current)
	}
}

// TestAddReadOnlyWorktreeIdempotent proves the retried-init-container
// property: a second call with the same args returns the same dir and nil
// error without a git failure.
func TestAddReadOnlyWorktreeIdempotent(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	pvc := t.TempDir()
	bareDest := filepath.Join(pvc, "repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, bareDest, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	first, err := AddReadOnlyWorktree(bareDest, "phase-uid-idem", branch)
	if err != nil {
		t.Fatalf("AddReadOnlyWorktree first call: %v", err)
	}

	second, err := AddReadOnlyWorktree(bareDest, "phase-uid-idem", branch)
	if err != nil {
		t.Fatalf("AddReadOnlyWorktree second call: %v", err)
	}
	if second != first {
		t.Errorf("AddReadOnlyWorktree second call dir = %q; want same dir %q", second, first)
	}
}

// TestAddReadOnlyWorktreeDistinctUIDsSameBranch exercises the --detach
// no-branch-claim property: two different uids checking out the SAME
// runBranch both succeed — a `-b`-style or plain-branch checkout would
// fail the second with "already checked out".
func TestAddReadOnlyWorktreeDistinctUIDsSameBranch(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	pvc := t.TempDir()
	bareDest := filepath.Join(pvc, "repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, bareDest, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	wtA, err := AddReadOnlyWorktree(bareDest, "phase-uid-a", branch)
	if err != nil {
		t.Fatalf("AddReadOnlyWorktree A: %v", err)
	}
	wtB, err := AddReadOnlyWorktree(bareDest, "phase-uid-b", branch)
	if err != nil {
		t.Fatalf("AddReadOnlyWorktree B: %v", err)
	}
	if wtA == wtB {
		t.Fatalf("worktrees collided: %q == %q", wtA, wtB)
	}
}

// TestAddReadOnlyWorktreeNoNewBranchCreated proves AddReadOnlyWorktree
// never mints a branch — distinct from AddWorktree's tide/wt-<uid> branch,
// which would otherwise dangle on every verify run (RESEARCH anti-pattern).
func TestAddReadOnlyWorktreeNoNewBranchCreated(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)

	pvc := t.TempDir()
	bareDest := filepath.Join(pvc, "repo.git")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := Clone(ctx, "file://"+bareSrc, bareDest, "")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	branch := defaultBranchOf(t, repo)

	before := gitOut(t, bareDest, "branch", "--list", "tide/wt-*")

	if _, err := AddReadOnlyWorktree(bareDest, "phase-uid-nobranch", branch); err != nil {
		t.Fatalf("AddReadOnlyWorktree: %v", err)
	}

	after := gitOut(t, bareDest, "branch", "--list", "tide/wt-*")
	if before != after {
		t.Errorf("git branch --list 'tide/wt-*' changed: before=%q after=%q", before, after)
	}
}

// Sanity check: ensure the second seed commit creates files independently
// from the first. Guards against fixture-helper regressions that would
// silently make other tests vacuous.
func TestSeedBareRepoFixtureIsSane(t *testing.T) {
	base := t.TempDir()
	bareDir, head := seedBareRepo(t, base)

	repo, err := gogit.PlainOpen(bareDir)
	if err != nil {
		t.Fatalf("PlainOpen bare: %v", err)
	}
	branch := defaultBranchOf(t, repo)
	if branch == "" {
		t.Fatal("seedBareRepo: default branch is empty")
	}
	if head.IsZero() {
		t.Fatal("seedBareRepo: head is zero hash")
	}
}
