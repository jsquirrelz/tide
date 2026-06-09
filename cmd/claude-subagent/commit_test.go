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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/jsquirrelz/tide/internal/subagent/anthropic"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// initGitWorktreeDir creates a minimal git repo at dir with a single commit
// so the executor's CommitWorktree step can run successfully (HEAD exists).
func initGitWorktreeDir(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "init", "-q"},
		{"git", "-C", dir, "config", "user.email", "test@tide.local"},
		{"git", "-C", dir, "config", "user.name", "tide-test"},
		{"git", "-C", dir, "commit", "-q", "--allow-empty", "-m", "initial"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %v\n%s", c, err, out)
		}
	}
}

// withFakeSubagentSuccess swaps the newSubagent seam to return a fake that
// always exits 0 using a canned transcript. Restores on test cleanup.
func withFakeSubagentSuccess(t *testing.T, tmp string) {
	t.Helper()
	fixturePath := writeFixture(t, tmp, fixtureStreamJSON)
	orig := newSubagent
	t.Cleanup(func() { newSubagent = orig })
	newSubagent = func(claudeBinary, wsRoot string) anthropicRunner {
		return anthropic.NewWithExec(
			anthropic.Options{ClaudeBinary: claudeBinary, WorkspaceRoot: wsRoot},
			func(ctx context.Context, name string, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, "bash", "-c", "cat "+fixturePath)
			},
		)
	}
}

// TestRunCommitsExecutorWorktree asserts that run() calls CommitWorktree after
// the subagent exits 0 when Role="executor", and out.Git.HeadSHA is non-empty.
func TestRunCommitsExecutorWorktree(t *testing.T) {
	tmp := t.TempDir()

	// Prepare the executor worktree directory with an uncommitted file.
	worktreeDir := filepath.Join(tmp, "worktrees", "task-exec-01")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("mkdir worktreeDir: %v", err)
	}
	initGitWorktreeDir(t, worktreeDir)
	if err := os.WriteFile(filepath.Join(worktreeDir, "authored.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	withFakeSubagentSuccess(t, tmp)

	// Skip the real EnsureWorktree; the worktree already exists.
	origEW := ensureWorktreeFunc
	t.Cleanup(func() { ensureWorktreeFunc = origEW })
	ensureWorktreeFunc = func(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error { return nil }

	envelopePath := filepath.Join(tmp, "envelopes", "task-exec-01", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-exec-01",
		Role:       "executor",
		Level:      "task",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), envelopePath, tmp, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr=%s", code, stderr.String())
	}

	// Read out.json and verify Git.HeadSHA is populated.
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	if got.Git == nil || got.Git.HeadSHA == "" {
		t.Errorf("out.Git.HeadSHA: got empty/nil, want a non-empty SHA (executor role + non-empty diff)")
	}
	if got.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", got.ExitCode)
	}
}

// TestRunEmptyDiffOverridesExitCode asserts that when the executor worktree
// has no changes after the subagent exits 0, run() returns exit code 1 and
// out.Result == "empty-diff" (D-03 / SC-2 policy).
func TestRunEmptyDiffOverridesExitCode(t *testing.T) {
	tmp := t.TempDir()

	// Executor worktree exists but has no uncommitted changes.
	worktreeDir := filepath.Join(tmp, "worktrees", "task-empty-01")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("mkdir worktreeDir: %v", err)
	}
	initGitWorktreeDir(t, worktreeDir)

	withFakeSubagentSuccess(t, tmp)

	origEW := ensureWorktreeFunc
	t.Cleanup(func() { ensureWorktreeFunc = origEW })
	ensureWorktreeFunc = func(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error { return nil }

	envelopePath := filepath.Join(tmp, "envelopes", "task-empty-01", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-empty-01",
		Role:       "executor",
		Level:      "task",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), envelopePath, tmp, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code: got %d, want 1 (empty-diff must be an explicit failure)", code)
	}

	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	if got.Result != "empty-diff" {
		t.Errorf("Result: got %q, want \"empty-diff\"", got.Result)
	}
	if got.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", got.ExitCode)
	}
}

// TestRunPlannerSkipsCommit asserts that CommitWorktree is NOT called when
// Role="planner", and that ExitCode=0 from the runner is preserved.
func TestRunPlannerSkipsCommit(t *testing.T) {
	tmp := t.TempDir()

	withFakeSubagentSuccess(t, tmp)

	origEW := ensureWorktreeFunc
	t.Cleanup(func() { ensureWorktreeFunc = origEW })
	ensureWorktreeFunc = func(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error { return nil }

	commitCalled := false
	origCW := commitWorktreeFunc
	t.Cleanup(func() { commitWorktreeFunc = origCW })
	commitWorktreeFunc = func(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
		commitCalled = true
		return plumbing.ZeroHash, false, nil
	}

	envelopePath := filepath.Join(tmp, "envelopes", "task-plan-01", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-plan-01",
		Role:       "planner",
		Level:      "milestone",
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), envelopePath, tmp, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr=%s", code, stderr.String())
	}
	if commitCalled {
		t.Error("commitWorktreeFunc should NOT be called for planner role")
	}
}
