package harness

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// fakeRuntime is a controllable Runtime for harness unit tests.
type fakeRuntime struct {
	usage pkgdispatch.Usage
	err   error
}

func (f *fakeRuntime) Execute(_ context.Context, _ pkgdispatch.EnvelopeIn, _, _ io.Writer) (pkgdispatch.Usage, error) {
	return f.usage, f.err
}

// buildTestHarness constructs a Harness wired to a fakeRuntime and a
// temporary workspace. The caller may optionally write fixture files into the
// returned workspaceDir before calling h.Run().
func buildTestHarness(t *testing.T, rt *fakeRuntime, env pkgdispatch.EnvelopeIn) (*Harness, string) {
	t.Helper()
	ws := t.TempDir()
	env.DeclaredOutputPaths = appendDeclared(env.DeclaredOutputPaths, ws)
	var stdout, stderr bytes.Buffer
	h := &Harness{
		Envelope:   env,
		Workspace:  ws,
		Runtime:    rt,
		StdoutDest: &stdout,
		StderrDest: &stderr,
		StartedAt:  time.Now(),
	}
	return h, ws
}

// appendDeclared ensures at least one declared path exists so tests that
// write files inside the workspace pass the output-path check by default.
func appendDeclared(existing []string, ws string) []string {
	if len(existing) > 0 {
		return existing
	}
	// Use workspace root as the declared path so any write within it is in-scope.
	return []string{"."}
}

// TestHarness_Run_Success verifies the happy path: fakeRuntime returns good
// usage, declared output paths satisfied, expect Result="success" ExitCode=0.
func TestHarness_Run_Success(t *testing.T) {
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-001",
		Role:       "executor",
		Level:      "task",
		Caps:       pkgdispatch.Caps{Iterations: 10},
	}
	rt := &fakeRuntime{usage: pkgdispatch.Usage{Iterations: 5, InputTokens: 100, OutputTokens: 50}}
	h, _ := buildTestHarness(t, rt, env)

	out, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if out.Result != "success" {
		t.Errorf("Result: got %q, want %q", out.Result, "success")
	}
	if out.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", out.ExitCode)
	}
	if out.TaskUID != "task-001" {
		t.Errorf("TaskUID: got %q, want %q", out.TaskUID, "task-001")
	}
}

// TestHarness_Run_WallClockExceeded verifies that when the runtime returns
// context.DeadlineExceeded, the harness produces Result="cap-hit"
// Reason="wall-clock" ExitCode=1.
func TestHarness_Run_WallClockExceeded(t *testing.T) {
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-002",
	}
	rt := &fakeRuntime{err: context.DeadlineExceeded}
	h, _ := buildTestHarness(t, rt, env)

	out, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if out.Result != "cap-hit" {
		t.Errorf("Result: got %q, want %q", out.Result, "cap-hit")
	}
	if out.Reason != "wall-clock" {
		t.Errorf("Reason: got %q, want %q", out.Reason, "wall-clock")
	}
	if out.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", out.ExitCode)
	}
}

// TestHarness_Run_IterationsCapHit verifies that when usage.Iterations exceeds
// caps.Iterations, the harness produces Result="cap-hit" Reason="iterations".
func TestHarness_Run_IterationsCapHit(t *testing.T) {
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-003",
		Caps:       pkgdispatch.Caps{Iterations: 10},
	}
	rt := &fakeRuntime{usage: pkgdispatch.Usage{Iterations: 11}}
	h, _ := buildTestHarness(t, rt, env)

	out, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if out.Result != "cap-hit" {
		t.Errorf("Result: got %q, want %q", out.Result, "cap-hit")
	}
	if out.Reason != "iterations" {
		t.Errorf("Reason: got %q, want %q", out.Reason, "iterations")
	}
	if out.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", out.ExitCode)
	}
}

// TestHarness_Run_RuntimeError verifies that when the runtime returns a
// non-deadline error, the harness produces Result="error" with the error text.
func TestHarness_Run_RuntimeError(t *testing.T) {
	env := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "task-004",
	}
	rt := &fakeRuntime{err: errors.New("boom")}
	h, _ := buildTestHarness(t, rt, env)

	out, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if out.Result != "error" {
		t.Errorf("Result: got %q, want %q", out.Result, "error")
	}
	if !strings.Contains(out.Reason, "boom") {
		t.Errorf("Reason should contain 'boom', got %q", out.Reason)
	}
	if out.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", out.ExitCode)
	}
}

// TestHarness_Run_OutputPathsViolation verifies that when a file is written
// outside declared output paths, the harness produces
// Result="output-paths-violation".
func TestHarness_Run_OutputPathsViolation(t *testing.T) {
	ws := t.TempDir()

	// The declared output path is a sub-directory of workspace.
	declaredSubdir := filepath.Join(ws, "allowed")
	if err := os.MkdirAll(declaredSubdir, 0o755); err != nil {
		t.Fatalf("mkdir declared: %v", err)
	}

	env := pkgdispatch.EnvelopeIn{
		APIVersion:          pkgdispatch.APIVersionV1Alpha1,
		Kind:                pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:             "task-005",
		DeclaredOutputPaths: []string{"allowed"},
	}

	var stdout, stderr bytes.Buffer
	startedAt := time.Now()
	time.Sleep(5 * time.Millisecond)

	// Write a file OUTSIDE the declared subdir (in the workspace root).
	escapedFile := filepath.Join(ws, "escape.txt")
	if err := os.WriteFile(escapedFile, []byte("bad"), 0o644); err != nil {
		t.Fatalf("WriteFile escape: %v", err)
	}

	rt := &fakeRuntime{usage: pkgdispatch.Usage{Iterations: 1}}
	h := &Harness{
		Envelope:   env,
		Workspace:  ws,
		Runtime:    rt,
		StdoutDest: &stdout,
		StderrDest: &stderr,
		StartedAt:  startedAt,
	}

	out, err := h.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if out.Result != "output-paths-violation" {
		t.Errorf("Result: got %q, want %q", out.Result, "output-paths-violation")
	}
	if out.ExitCode != 1 {
		t.Errorf("ExitCode: got %d, want 1", out.ExitCode)
	}
}

func TestValidateIgnoresWorkspaceEnvelopeTransportFiles(t *testing.T) {
	ws := t.TempDir()
	startedAt := time.Now()
	time.Sleep(5 * time.Millisecond)

	if err := os.MkdirAll(filepath.Join(ws, "alpha.go"), 0o755); err != nil {
		t.Fatalf("mkdir declared output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "alpha.go", "result.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write declared output: %v", err)
	}
	envelopeDir := filepath.Join(ws, "envelopes", "task-001")
	if err := os.MkdirAll(envelopeDir, 0o755); err != nil {
		t.Fatalf("mkdir envelope dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(envelopeDir, "in.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write envelope in: %v", err)
	}
	if err := os.WriteFile(filepath.Join(envelopeDir, "out.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write envelope out: %v", err)
	}

	violations, err := Validate(ws, startedAt, []string{"alpha.go"})
	if err != nil {
		t.Fatalf("Validate error: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("Validate violations = %v, want none", violations)
	}
}
