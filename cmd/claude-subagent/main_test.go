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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/jsquirrelz/tide/internal/subagent/anthropic"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// fixtureStreamJSON mirrors the anthropic subagent unit tests — a minimal
// stream-json transcript that resolves to a successful EnvelopeOut.
const fixtureStreamJSON = `{"type":"system/init","session_id":"sess-claude-subagent"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}}
{"type":"result","result":"shim-ok","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"total_cost_usd":0.0001}
`

// withFakeSubagent swaps the package-level newSubagent seam to construct an
// anthropic.Anthropic whose exec is a deterministic `bash -c 'cat <fixture>'`.
// Restores the seam on test cleanup.
func withFakeSubagent(t *testing.T, fixturePath, workspaceRoot string) {
	t.Helper()
	orig := newSubagent
	t.Cleanup(func() { newSubagent = orig })
	newSubagent = func(claudeBinary, wsRoot string, _ map[string]pkgdispatch.PriceOverride) anthropicRunner {
		a := anthropic.NewWithExec(
			anthropic.Options{ClaudeBinary: claudeBinary, WorkspaceRoot: wsRoot},
			func(ctx context.Context, name string, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, "bash", "-c", "cat "+fixturePath)
			},
		)
		_ = workspaceRoot // captured for shape consistency
		return a
	}
}

// writeFixture writes the canned stream-json transcript to <dir>/fixture.jsonl
// and returns the absolute path.
func writeFixture(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "fixture.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// writeEnvelopeInFile marshals env to JSON and drops it at envelopePath,
// creating parent dirs.
func writeEnvelopeInFile(t *testing.T, envelopePath string, env pkgdispatch.EnvelopeIn) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(envelopePath), 0o755); err != nil {
		t.Fatalf("mkdir envelope dir: %v", err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope-in: %v", err)
	}
	if err := os.WriteFile(envelopePath, data, 0o644); err != nil {
		t.Fatalf("write envelope-in: %v", err)
	}
}

// TestClaudeSubagentMain_HappyPath asserts the shim loads EnvelopeIn, runs
// anthropic via the fake exec, and writes a populated out.json — exit 0.
// (Plan 03-07 Task 1 Test 1.)
func TestClaudeSubagentMain_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	fixturePath := writeFixture(t, tmp, fixtureStreamJSON)
	withFakeSubagent(t, fixturePath, tmp)

	envelopePath := filepath.Join(tmp, "envelopes", "t-1", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "t-1",
		Role:       "planner",
		Level:      "milestone",
		Prompt:     "hello",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
		ProxyEndpoint: "https://127.0.0.1:8443",
		SignedToken:   "fixture-token",
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), envelopePath, tmp, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr=%s", code, stderr.String())
	}
	// Verify out.json exists at sibling path and contains a valid EnvelopeOut.
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	if got.TaskUID != "t-1" {
		t.Errorf("TaskUID: got %q, want %q", got.TaskUID, "t-1")
	}
	if got.ExitCode != 0 {
		t.Errorf("EnvelopeOut.ExitCode: got %d, want 0", got.ExitCode)
	}
	if got.Result == "" {
		t.Errorf("EnvelopeOut.Result is empty; want parsed result text")
	}
	if got.Usage.InputTokens != 10 || got.Usage.OutputTokens != 5 {
		t.Errorf("Usage: got %+v, want In=10 Out=5", got.Usage)
	}
}

// TestClaudeSubagentMain_EnvelopeLoadFailure asserts that a missing envelope
// path produces exit 2 and a stderr containing "envelope". (Plan 03-07 Task 1
// Test 2.)
func TestClaudeSubagentMain_EnvelopeLoadFailure(t *testing.T) {
	tmp := t.TempDir()
	// Make newSubagent panic if called — the shim must not invoke it when
	// envelope load fails.
	orig := newSubagent
	t.Cleanup(func() { newSubagent = orig })
	newSubagent = func(claudeBinary, wsRoot string, _ map[string]pkgdispatch.PriceOverride) anthropicRunner {
		t.Fatalf("newSubagent must not be invoked on envelope load failure")
		return nil
	}

	bogusPath := filepath.Join(tmp, "does-not-exist", "in.json")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), bogusPath, tmp, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code: got %d, want 2; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "envelope") {
		t.Errorf("stderr should mention envelope; got %q", stderr.String())
	}
}

// TestClaudeSubagentMain_VendorMismatch asserts that an envelope with
// Provider.Vendor != "anthropic" causes anthropic.Run() to return a
// dispatch-level error; the shim must wrap that error as a failure-shaped
// EnvelopeOut, persist it to out.json, and return a non-zero exit code.
// (Plan 03-07 Task 1 Test 3.)
func TestClaudeSubagentMain_VendorMismatch(t *testing.T) {
	tmp := t.TempDir()
	fixturePath := writeFixture(t, tmp, fixtureStreamJSON)
	withFakeSubagent(t, fixturePath, tmp)

	envelopePath := filepath.Join(tmp, "envelopes", "t-vendor", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "t-vendor",
		Role:       "planner",
		Level:      "milestone",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "openai", // intentional mismatch — anthropic refuses.
			Model:  "gpt-4",
		},
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), envelopePath, tmp, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero exit on vendor mismatch, got 0; stderr=%s", stderr.String())
	}
	// out.json must exist with a failure-shaped EnvelopeOut.
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	if got.ExitCode == 0 {
		t.Errorf("EnvelopeOut.ExitCode: got 0 on vendor mismatch, want != 0")
	}
	if got.Result == "" && got.Reason == "" {
		t.Errorf("EnvelopeOut should describe the failure in Result or Reason; got empty")
	}
}

// TestClaudeSubagentMain_InvokesEnsureWorktreeBeforeRun asserts the shim
// calls EnsureWorktree BEFORE the subagent's Run() — order matters because
// the subagent's prompt assumes the worktree dir already exists. We assert
// via a recorded call order: ensureWorktreeFunc bumps a counter to 1; the
// fake newSubagent sees counter==1 when its Run is invoked. (Plan 03-07
// Task 3 Test 5 — cross-file integration; ties Task 3 to Task 1's shim.)
func TestClaudeSubagentMain_InvokesEnsureWorktreeBeforeRun(t *testing.T) {
	tmp := t.TempDir()
	fixturePath := writeFixture(t, tmp, fixtureStreamJSON)
	// Mark the order seen at each call site.
	var order []string
	origEW := ensureWorktreeFunc
	t.Cleanup(func() { ensureWorktreeFunc = origEW })
	ensureWorktreeFunc = func(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error {
		order = append(order, "ensure-worktree")
		return nil
	}
	origSA := newSubagent
	t.Cleanup(func() { newSubagent = origSA })
	newSubagent = func(claudeBinary, wsRoot string, _ map[string]pkgdispatch.PriceOverride) anthropicRunner {
		return anthropic.NewWithExec(
			anthropic.Options{ClaudeBinary: claudeBinary, WorkspaceRoot: wsRoot},
			func(ctx context.Context, name string, args ...string) *exec.Cmd {
				order = append(order, "subagent-run")
				return exec.CommandContext(ctx, "bash", "-c", "cat "+fixturePath)
			},
		)
	}
	// Override the commit seam — this test exercises call ordering, not commit behavior.
	origCW := commitWorktreeFunc
	t.Cleanup(func() { commitWorktreeFunc = origCW })
	commitWorktreeFunc = func(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
		return plumbing.NewHash("aabbccdd" + strings.Repeat("0", 32)), false, nil
	}

	envelopePath := filepath.Join(tmp, "envelopes", "t-order", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "t-order",
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
	if len(order) < 2 {
		t.Fatalf("expected ≥2 ordered calls; got %v", order)
	}
	if order[0] != "ensure-worktree" {
		t.Errorf("call order: got %v, want ensure-worktree first", order)
	}
	if order[1] != "subagent-run" {
		t.Errorf("call order: got %v, want subagent-run second", order)
	}
}

// TestClaudeSubagentMain_PassesEnvBranchToWorktree is the 09-09 regression guard:
// the executor's worktree branch must come from EnvelopeIn.Branch (in.json), not the
// never-written branch.txt. A previous build passed an empty branch → EnsureWorktree
// failed with "git worktree: empty branch" on every real-Claude task.
func TestClaudeSubagentMain_PassesEnvBranchToWorktree(t *testing.T) {
	tmp := t.TempDir()
	fixturePath := writeFixture(t, tmp, fixtureStreamJSON)
	const wantBranch = "tide/run-medium-project-1780956333"

	var gotBranch string
	origEW := ensureWorktreeFunc
	t.Cleanup(func() { ensureWorktreeFunc = origEW })
	ensureWorktreeFunc = func(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error {
		gotBranch = branch
		return nil
	}
	origSA := newSubagent
	t.Cleanup(func() { newSubagent = origSA })
	newSubagent = func(claudeBinary, wsRoot string, _ map[string]pkgdispatch.PriceOverride) anthropicRunner {
		return anthropic.NewWithExec(
			anthropic.Options{ClaudeBinary: claudeBinary, WorkspaceRoot: wsRoot},
			func(ctx context.Context, name string, args ...string) *exec.Cmd {
				return exec.CommandContext(ctx, "bash", "-c", "cat "+fixturePath)
			},
		)
	}
	// Override the commit seam — this test exercises branch-passing, not commit behavior.
	origCW := commitWorktreeFunc
	t.Cleanup(func() { commitWorktreeFunc = origCW })
	commitWorktreeFunc = func(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
		return plumbing.NewHash("aabbccdd" + strings.Repeat("0", 32)), false, nil
	}

	envelopePath := filepath.Join(tmp, "envelopes", "t-branch", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "t-branch",
		Role:       "executor",
		Level:      "task",
		Branch:     wantBranch,
		Provider:   pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), envelopePath, tmp, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr=%s", code, stderr.String())
	}
	if gotBranch != wantBranch {
		t.Errorf("worktree branch = %q, want %q (must come from EnvelopeIn.Branch, not branch.txt)", gotBranch, wantBranch)
	}
}

// TestClaudeSubagentMain_IgnoresDevTestMode asserts that even when env.Dev is
// populated, the shim does NOT switch on Dev.TestMode — it always goes
// through anthropic.New().Run(). Behavior is identical to the happy-path.
// (Plan 03-07 Task 1 Test 4 — anti-pattern enforcement: real Claude image
// MUST ignore env.Dev entirely per PATTERNS.md line 442.)
func TestClaudeSubagentMain_IgnoresDevTestMode(t *testing.T) {
	tmp := t.TempDir()
	fixturePath := writeFixture(t, tmp, fixtureStreamJSON)
	withFakeSubagent(t, fixturePath, tmp)

	envelopePath := filepath.Join(tmp, "envelopes", "t-dev", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "t-dev",
		Role:       "planner",
		Level:      "milestone",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
		Dev: &pkgdispatch.Dev{TestMode: "success"},
	}
	writeEnvelopeInFile(t, envelopePath, env)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), envelopePath, tmp, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code: got %d, want 0; stderr=%s", code, stderr.String())
	}
	// Same shape as TestClaudeSubagentMain_HappyPath — proves Dev was ignored
	// and the fake-exec produced the canned anthropic stream.
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	if got.Result == "" {
		t.Errorf("EnvelopeOut.Result is empty; if the shim had switched on Dev.TestMode it might have synthesized success — we want the fake-exec result instead")
	}
}

// fakeErrRunner is a minimal anthropicRunner that always returns a
// dispatch-level error — used to exercise the "subagent-error" TerminalReason
// mapping without depending on the real anthropic package's error shapes.
type fakeErrRunner struct{ err error }

func (f fakeErrRunner) Run(_ context.Context, _ pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error) {
	return pkgdispatch.EnvelopeOut{}, f.err
}

// TestClaudeSubagentMain_TerminalReasonMapping is the 50-PATTERNS.md 6-row
// mapping-table proof (Task 2 Test 1): every claude-subagent exit class
// carries its mapped pkgdispatch.TerminalReason. The 6th row (success ->
// completed) is covered separately by
// TestClaudeSubagentMain_SuccessPathCompletesRunEvidence below, since it
// needs a real git worktree fixture, not a failure-path setup.
func TestClaudeSubagentMain_TerminalReasonMapping(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, tmp string) (envelopePath string)
		wantResult string
		wantReason pkgdispatch.TerminalReason
	}{
		{
			name: "invalid-envelope",
			setup: func(t *testing.T, tmp string) string {
				return filepath.Join(tmp, "does-not-exist", "in.json")
			},
			wantResult: "invalid-envelope",
			wantReason: pkgdispatch.TerminalReasonInvalidOutput,
		},
		{
			name: "worktree-setup-failed",
			setup: func(t *testing.T, tmp string) string {
				origEW := ensureWorktreeFunc
				t.Cleanup(func() { ensureWorktreeFunc = origEW })
				ensureWorktreeFunc = func(_ pkgdispatch.EnvelopeIn, _, _ string) error {
					return fmt.Errorf("worktree boom")
				}
				envelopePath := filepath.Join(tmp, "envelopes", "t-wt", "in.json")
				writeEnvelopeInFile(t, envelopePath, pkgdispatch.EnvelopeIn{
					APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeIn,
					TaskUID: "t-wt", Role: "executor", Level: "task",
					Provider: pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
				})
				return envelopePath
			},
			wantResult: "worktree-setup-failed",
			wantReason: pkgdispatch.TerminalReasonToolFailure,
		},
		{
			name: "subagent-error",
			setup: func(t *testing.T, tmp string) string {
				origSA := newSubagent
				t.Cleanup(func() { newSubagent = origSA })
				newSubagent = func(_, _ string, _ map[string]pkgdispatch.PriceOverride) anthropicRunner {
					return fakeErrRunner{err: fmt.Errorf("dispatch boom")}
				}
				origEW := ensureWorktreeFunc
				t.Cleanup(func() { ensureWorktreeFunc = origEW })
				ensureWorktreeFunc = func(_ pkgdispatch.EnvelopeIn, _, _ string) error { return nil }
				envelopePath := filepath.Join(tmp, "envelopes", "t-sa", "in.json")
				writeEnvelopeInFile(t, envelopePath, pkgdispatch.EnvelopeIn{
					APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeIn,
					TaskUID: "t-sa", Role: "planner", Level: "milestone",
					Provider: pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
				})
				return envelopePath
			},
			wantResult: "subagent-error",
			wantReason: pkgdispatch.TerminalReasonToolFailure,
		},
		{
			name: "commit-failed",
			setup: func(t *testing.T, tmp string) string {
				withFakeSubagentSuccess(t, tmp)
				origEW := ensureWorktreeFunc
				t.Cleanup(func() { ensureWorktreeFunc = origEW })
				ensureWorktreeFunc = func(_ pkgdispatch.EnvelopeIn, _, _ string) error { return nil }
				origCW := commitWorktreeFunc
				t.Cleanup(func() { commitWorktreeFunc = origCW })
				commitWorktreeFunc = func(_, _ string) (plumbing.Hash, bool, error) {
					return plumbing.ZeroHash, false, fmt.Errorf("commit boom")
				}
				envelopePath := filepath.Join(tmp, "envelopes", "t-commit", "in.json")
				writeEnvelopeInFile(t, envelopePath, pkgdispatch.EnvelopeIn{
					APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeIn,
					TaskUID: "t-commit", Role: "executor", Level: "task",
					Provider: pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
				})
				return envelopePath
			},
			wantResult: "commit-failed",
			wantReason: pkgdispatch.TerminalReasonToolFailure,
		},
		{
			name: "empty-diff",
			setup: func(t *testing.T, tmp string) string {
				worktreeDir := filepath.Join(tmp, "worktrees", "t-empty")
				if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
					t.Fatalf("mkdir worktreeDir: %v", err)
				}
				initGitWorktreeDir(t, worktreeDir)
				withFakeSubagentSuccess(t, tmp)
				origEW := ensureWorktreeFunc
				t.Cleanup(func() { ensureWorktreeFunc = origEW })
				ensureWorktreeFunc = func(_ pkgdispatch.EnvelopeIn, _, _ string) error { return nil }
				envelopePath := filepath.Join(tmp, "envelopes", "t-empty", "in.json")
				writeEnvelopeInFile(t, envelopePath, pkgdispatch.EnvelopeIn{
					APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeIn,
					TaskUID: "t-empty", Role: "executor", Level: "task",
					Provider: pkgdispatch.ProviderSpec{Vendor: "anthropic", Model: "claude-sonnet-4-6"},
				})
				return envelopePath
			},
			wantResult: "empty-diff",
			wantReason: pkgdispatch.TerminalReasonBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			envelopePath := tt.setup(t, tmp)
			var stdout, stderr bytes.Buffer
			_ = run(context.Background(), envelopePath, tmp, &stdout, &stderr)
			outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
			data, err := os.ReadFile(outPath)
			if err != nil {
				t.Fatalf("read out.json: %v", err)
			}
			var got pkgdispatch.EnvelopeOut
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal out.json: %v", err)
			}
			if got.Result != tt.wantResult {
				t.Errorf("Result: got %q, want %q", got.Result, tt.wantResult)
			}
			if got.TerminalReason != tt.wantReason {
				t.Errorf("TerminalReason: got %q, want %q", got.TerminalReason, tt.wantReason)
			}
		})
	}
}

// TestClaudeSubagentMain_SuccessPathCompletesRunEvidence asserts the D-03/
// EXEC-03 success-path contract (Task 2 Test 2): TerminalReason arrives as
// "completed" from anthropic.Run()'s base literal (not re-set by run()),
// Git.HeadSHA is set, and RunEvidence.ChangedFiles/ChangedFileTotal are
// populated from the real worktree's HEAD commit.
func TestClaudeSubagentMain_SuccessPathCompletesRunEvidence(t *testing.T) {
	tmp := t.TempDir()

	worktreeDir := filepath.Join(tmp, "worktrees", "t-evidence")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("mkdir worktreeDir: %v", err)
	}
	initGitWorktreeDir(t, worktreeDir)
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(worktreeDir, name), []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	withFakeSubagentSuccess(t, tmp)
	origEW := ensureWorktreeFunc
	t.Cleanup(func() { ensureWorktreeFunc = origEW })
	ensureWorktreeFunc = func(_ pkgdispatch.EnvelopeIn, _, _ string) error { return nil }

	envelopePath := filepath.Join(tmp, "envelopes", "t-evidence", "in.json")
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "t-evidence",
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

	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	if got.TerminalReason != pkgdispatch.TerminalReasonCompleted {
		t.Errorf("TerminalReason: got %q, want %q", got.TerminalReason, pkgdispatch.TerminalReasonCompleted)
	}
	if got.Git == nil || got.Git.HeadSHA == "" {
		t.Fatalf("Git.HeadSHA: got %+v, want a populated HeadSHA", got.Git)
	}
	if got.RunEvidence == nil {
		t.Fatal("RunEvidence: got nil, want populated")
	}
	if got.RunEvidence.ChangedFileTotal <= 0 {
		t.Errorf("RunEvidence.ChangedFileTotal: got %d, want > 0", got.RunEvidence.ChangedFileTotal)
	}
	if got.RunEvidence.ChangedFileTotal < len(got.RunEvidence.ChangedFiles) {
		t.Errorf("RunEvidence.ChangedFileTotal (%d) must be >= len(ChangedFiles) (%d)",
			got.RunEvidence.ChangedFileTotal, len(got.RunEvidence.ChangedFiles))
	}
}
