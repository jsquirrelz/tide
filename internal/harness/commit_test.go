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

	"github.com/go-git/go-git/v5/plumbing"
)

// initWorktreeRepo initialises a plain (non-bare) git repo at dir suitable
// for CommitWorktree unit tests. Sets local user.email and user.name so that
// git commit won't fail due to a missing identity in CI environments.
func initWorktreeRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "init", "-q"},
		{"git", "-C", dir, "config", "user.email", "test@tide.local"},
		{"git", "-C", dir, "config", "user.name", "tide-test"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", c, err, out)
		}
	}
}

// TestCommitWorktree asserts that CommitWorktree commits an untracked file and
// returns a non-zero hash with isEmpty=false.
func TestCommitWorktree(t *testing.T) {
	dir := t.TempDir()
	initWorktreeRepo(t, dir)

	// Drop an untracked file.
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	hash, isEmpty, err := CommitWorktree(dir, "task-unit-01")
	if err != nil {
		t.Fatalf("CommitWorktree: %v", err)
	}
	if isEmpty {
		t.Error("isEmpty: got true, want false (file was staged)")
	}
	if hash == plumbing.ZeroHash {
		t.Error("hash: got ZeroHash, want a real commit SHA")
	}

	// HEAD in the worktree must match the returned hash.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	if strings.TrimSpace(string(out)) != hash.String() {
		t.Errorf("HEAD mismatch: git says %q, CommitWorktree returned %q",
			strings.TrimSpace(string(out)), hash.String())
	}
}

// TestCommitWorktreeEmpty asserts that CommitWorktree returns (ZeroHash, true, nil)
// when the worktree has no staged or unstaged changes.
func TestCommitWorktreeEmpty(t *testing.T) {
	dir := t.TempDir()
	initWorktreeRepo(t, dir)
	// No file written — worktree is clean.

	hash, isEmpty, err := CommitWorktree(dir, "task-empty-01")
	if err != nil {
		t.Fatalf("CommitWorktree (empty): %v", err)
	}
	if !isEmpty {
		t.Error("isEmpty: got false, want true (no changes in worktree)")
	}
	if hash != plumbing.ZeroHash {
		t.Errorf("hash: got %q, want ZeroHash on empty diff", hash.String())
	}
}

// TestCommitWorktreeEnvIdentity asserts that TIDE_BOT_NAME / TIDE_BOT_EMAIL
// override the default bot identity in the committed author line.
func TestCommitWorktreeEnvIdentity(t *testing.T) {
	dir := t.TempDir()
	initWorktreeRepo(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "id_check.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	t.Setenv("TIDE_BOT_NAME", "Custom Bot")
	t.Setenv("TIDE_BOT_EMAIL", "custom@example.com")

	hash, isEmpty, err := CommitWorktree(dir, "task-id-01")
	if err != nil {
		t.Fatalf("CommitWorktree: %v", err)
	}
	if isEmpty {
		t.Error("isEmpty: got true, want false")
	}
	if hash == plumbing.ZeroHash {
		t.Error("hash: got ZeroHash, want a real commit SHA")
	}

	// git log --format=%ae gives the author email of the most recent commit.
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%ae").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if strings.TrimSpace(string(out)) != "custom@example.com" {
		t.Errorf("author email: got %q, want custom@example.com", strings.TrimSpace(string(out)))
	}

	out, err = exec.Command("git", "-C", dir, "log", "-1", "--format=%an").Output()
	if err != nil {
		t.Fatalf("git log name: %v", err)
	}
	if strings.TrimSpace(string(out)) != "Custom Bot" {
		t.Errorf("author name: got %q, want Custom Bot", strings.TrimSpace(string(out)))
	}
}

// TestCommitWorktreeModifiedFile asserts that a modified tracked file (not just
// a new untracked file) is staged and committed by CommitWorktree.
func TestCommitWorktreeModifiedFile(t *testing.T) {
	dir := t.TempDir()
	initWorktreeRepo(t, dir)

	// Create an initial commit with a file so HEAD exists and the file is tracked.
	fpath := filepath.Join(dir, "existing.go")
	if err := os.WriteFile(fpath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	cmds := [][]string{
		{"git", "-C", dir, "add", "existing.go"},
		{"git", "-C", dir, "-c", "user.name=tide-test", "-c", "user.email=test@tide.local",
			"commit", "-m", "initial commit"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("setup commit %v: %v\n%s", c, err, out)
		}
	}

	// Modify the tracked file.
	if err := os.WriteFile(fpath, []byte("package main\n// modified\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}

	hash, isEmpty, err := CommitWorktree(dir, "task-mod-01")
	if err != nil {
		t.Fatalf("CommitWorktree (modified): %v", err)
	}
	if isEmpty {
		t.Error("isEmpty: got true, want false (modified file should be staged)")
	}
	if hash == plumbing.ZeroHash {
		t.Error("hash: got ZeroHash, want a real commit SHA")
	}
}
