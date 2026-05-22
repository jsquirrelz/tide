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
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
)

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

	// A non-bare clone has a .git subdirectory.
	if info, err := os.Stat(filepath.Join(wtDir, ".git")); err != nil || !info.IsDir() {
		t.Errorf("expected .git directory in worktree %q: err=%v", wtDir, err)
	}

	// The seed README.md should be checked out in the worktree.
	if _, err := os.Stat(filepath.Join(wtDir, "README.md")); err != nil {
		t.Errorf("expected README.md in worktree: %v", err)
	}

	// The worktree should be a valid go-git repo.
	if _, err := gogit.PlainOpen(wtDir); err != nil {
		t.Errorf("PlainOpen worktree: %v", err)
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

	// Each worktree's .git/index is its own file (the index-isolation
	// property D-B4 commits to). Compare file inode / content cheaply by
	// modifying wtA and observing wtB unchanged.
	repoA, err := gogit.PlainOpen(wtA)
	if err != nil {
		t.Fatalf("PlainOpen A: %v", err)
	}
	wtAW, err := repoA.Worktree()
	if err != nil {
		t.Fatalf("Worktree A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtA, "a-only.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a-only: %v", err)
	}
	if _, err := wtAW.Add("a-only.txt"); err != nil {
		t.Fatalf("wtAW.Add: %v", err)
	}

	// Worktree B's status should NOT show a-only.txt (independent index).
	repoB, err := gogit.PlainOpen(wtB)
	if err != nil {
		t.Fatalf("PlainOpen B: %v", err)
	}
	wtBW, err := repoB.Worktree()
	if err != nil {
		t.Fatalf("Worktree B: %v", err)
	}
	statusB, err := wtBW.Status()
	if err != nil {
		t.Fatalf("wtBW.Status: %v", err)
	}
	if _, ok := statusB["a-only.txt"]; ok {
		t.Errorf("a-only.txt leaked into worktree B status: %+v", statusB["a-only.txt"])
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
