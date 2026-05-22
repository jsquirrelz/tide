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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// initBareRepo creates an empty bare git repo at <workspaceRoot>/repo.git
// with a single commit on the requested branch so the AddWorktree
// PlainClone path has a valid reference to check out. Tests that exercise
// the real pkggit.AddWorktree codepath (Test 1 + Test 3) call this; tests
// that only exercise the planner short-circuit (Test 2) or the missing-
// bare-repo error (Test 4) skip it.
func initBareRepo(t *testing.T, workspaceRoot, branch string) {
	t.Helper()
	bare := filepath.Join(workspaceRoot, "repo.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir bare: %v", err)
	}
	// A bare repo with a branch pointing at a real commit is easiest to
	// produce by first creating a normal repo with a commit, then cloning
	// it bare into the target path.
	tmp := t.TempDir()
	cmds := [][]string{
		{"git", "-C", tmp, "init", "-q", "-b", branch},
		{"git", "-C", tmp, "config", "user.email", "test@tide.local"},
		{"git", "-C", tmp, "config", "user.name", "tide-test"},
		{"git", "-C", tmp, "commit", "-q", "--allow-empty", "-m", "initial"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("setup cmd %v: %v\n%s", c, err, out)
		}
	}
	// Replace the bare dir with a bare clone of the source repo.
	if err := os.RemoveAll(bare); err != nil {
		t.Fatalf("rm bare: %v", err)
	}
	if out, err := exec.Command("git", "clone", "-q", "--bare", tmp, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
}

// TestEnsureWorktree_ExecutorRoleCreatesWorktree asserts that an executor
// Role triggers a real pkggit.AddWorktree call, producing a valid working
// tree at <workspaceRoot>/worktrees/<TaskUID>/. (Plan 03-07 Task 3 Test 1.)
func TestEnsureWorktree_ExecutorRoleCreatesWorktree(t *testing.T) {
	tmp := t.TempDir()
	branch := "tide/run-test"
	initBareRepo(t, tmp, branch)

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-abc",
		Role:       "executor",
		Level:      "task",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	if err := EnsureWorktree(in, tmp, branch); err != nil {
		t.Fatalf("EnsureWorktree: %v", err)
	}
	wt := filepath.Join(tmp, "worktrees", "task-abc")
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Fatalf(".git in worktree: %v", err)
	}
}

// TestEnsureWorktree_PlannerRoleShortCircuits asserts that a planner Role
// returns nil without invoking pkggit.AddWorktree. We assert via a test
// seam (addWorktreeFunc) that records call count. (Plan 03-07 Task 3
// Test 2 — D-A4 invariant.)
func TestEnsureWorktree_PlannerRoleShortCircuits(t *testing.T) {
	tmp := t.TempDir()
	// Note: NO bare repo on disk. If the planner-short-circuit fails, the
	// fallback path would also fail at the bare-repo existence check —
	// either way we'd notice. But the test seam is the cleaner signal.
	calls := 0
	orig := addWorktreeFunc
	t.Cleanup(func() { addWorktreeFunc = orig })
	addWorktreeFunc = func(repoPath, taskUID, branch string) (string, error) {
		calls++
		return filepath.Join(filepath.Dir(repoPath), "worktrees", taskUID), nil
	}

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "m-001",
		Role:       "planner",
		Level:      "milestone",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	if err := EnsureWorktree(in, tmp, "tide/run-test"); err != nil {
		t.Fatalf("EnsureWorktree(planner) returned error: %v", err)
	}
	if calls != 0 {
		t.Errorf("addWorktreeFunc invocations: got %d, want 0 (planner Role must short-circuit per D-A4)", calls)
	}
}

// TestEnsureWorktree_ExecutorIdempotentReCall asserts that calling
// EnsureWorktree twice with the same TaskUID does not error; the second
// call detects the existing worktree dir and short-circuits without
// re-cloning. (Plan 03-07 Task 3 Test 3.)
func TestEnsureWorktree_ExecutorIdempotentReCall(t *testing.T) {
	tmp := t.TempDir()
	branch := "tide/run-idem"
	initBareRepo(t, tmp, branch)

	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-idem",
		Role:       "executor",
		Level:      "task",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	if err := EnsureWorktree(in, tmp, branch); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Replace the real AddWorktree with a seam that would fail loudly if
	// called — proving the idempotent re-call path short-circuits before
	// reaching it.
	orig := addWorktreeFunc
	t.Cleanup(func() { addWorktreeFunc = orig })
	called := false
	addWorktreeFunc = func(repoPath, taskUID, branch string) (string, error) {
		called = true
		return "", nil
	}
	if err := EnsureWorktree(in, tmp, branch); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if called {
		t.Errorf("addWorktreeFunc should NOT be invoked on re-call when worktree dir already has a valid .git")
	}
}

// TestEnsureWorktree_ExecutorMissingBareRepo asserts the executor path
// returns a wrapped error containing "bare repo" or "repo.git" when the
// shared bare clone is missing. (Plan 03-07 Task 3 Test 4.)
func TestEnsureWorktree_ExecutorMissingBareRepo(t *testing.T) {
	tmp := t.TempDir() // no repo.git created
	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-no-bare",
		Role:       "executor",
		Level:      "task",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	err := EnsureWorktree(in, tmp, "tide/run-missing")
	if err == nil {
		t.Fatal("expected error on missing bare repo, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "bare repo") && !strings.Contains(msg, "repo.git") {
		t.Errorf("error should mention 'bare repo' or 'repo.git'; got %q", err.Error())
	}
}
