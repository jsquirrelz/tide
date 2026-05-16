package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
	newSubagent = func(claudeBinary, wsRoot string) anthropicRunner {
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
	newSubagent = func(claudeBinary, wsRoot string) anthropicRunner {
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
