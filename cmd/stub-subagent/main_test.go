// Package main tests for stub-subagent (SUB-04 / D-F1..F3).
// Uses stdlib testing only — no Ginkgo/Gomega; this is a lean cmd package.
// Tests exercise the in-process run() helper with tmpdir fixture envelopes
// to avoid shelling out to the binary.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// makeEnvelope writes a fixture EnvelopeIn JSON to dir/in.json and returns
// the path. The caller controls TestMode via the Dev field.
func makeEnvelope(t *testing.T, dir string, testMode string, outputPaths []string) string {
	t.Helper()
	env := pkgdispatch.EnvelopeIn{
		APIVersion:          pkgdispatch.APIVersionV1Alpha1,
		Kind:                pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:             "test-uid-1234",
		Role:                "executor",
		Level:               "task",
		Prompt:              "test prompt",
		FilesTouched:        []string{"result.txt"},
		DeclaredOutputPaths: outputPaths,
		Caps: pkgdispatch.Caps{
			WallClockSeconds: 300,
			Iterations:       10,
			InputTokens:      10000,
			OutputTokens:     5000,
		},
		ProxyEndpoint: "https://localhost:8443",
		SignedToken:   "test-signed-token",
	}
	if testMode != "" {
		env.Dev = &pkgdispatch.Dev{TestMode: testMode}
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, data, 0o644); err != nil {
		t.Fatalf("write in.json: %v", err)
	}
	return inPath
}

// readOutEnvelope reads and unmarshals the out.json written by the stub.
func readOutEnvelope(t *testing.T, dir string) pkgdispatch.EnvelopeOut {
	t.Helper()
	outPath := filepath.Join(dir, "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out.json: %v", err)
	}
	var out pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal out.json: %v", err)
	}
	return out
}

func TestStub_SuccessMode(t *testing.T) {
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	inPath := makeEnvelope(t, dir, "success", []string{artifactDir})

	ctx := context.Background()
	code := run(ctx, inPath, os.Stdout, os.Stderr)
	if code != 0 {
		t.Fatalf("success mode: want exit 0, got %d", code)
	}

	out := readOutEnvelope(t, dir)

	if out.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", out.ExitCode)
	}
	if out.Result != "success" {
		t.Errorf("Result = %q, want %q", out.Result, "success")
	}
	if out.Usage.InputTokens != 100 {
		t.Errorf("Usage.InputTokens = %d, want 100", out.Usage.InputTokens)
	}
	if out.Usage.OutputTokens != 200 {
		t.Errorf("Usage.OutputTokens = %d, want 200", out.Usage.OutputTokens)
	}
	if out.Usage.Iterations != 1 {
		t.Errorf("Usage.Iterations = %d, want 1", out.Usage.Iterations)
	}
	if len(out.Artifacts) == 0 {
		t.Errorf("Artifacts is empty, want at least one artifact")
	}
	if out.CompletedAt.IsZero() {
		t.Errorf("CompletedAt is zero")
	}
	// Verify result.txt was actually written
	resultPath := filepath.Join(artifactDir, "result.txt")
	if _, err := os.Stat(resultPath); err != nil {
		t.Errorf("result.txt not written at %s: %v", resultPath, err)
	}
}

func TestWriteEnvelopeAlsoWritesTerminationMessage(t *testing.T) {
	dir := t.TempDir()
	terminationPath := filepath.Join(dir, "termination.log")
	t.Setenv("TIDE_TERMINATION_MESSAGE_PATH", terminationPath)

	want := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    "task-termination",
		ExitCode:   0,
		Result:     "success",
	}
	if err := writeEnvelope(filepath.Join(dir, "out.json"), want); err != nil {
		t.Fatalf("writeEnvelope: %v", err)
	}

	data, err := os.ReadFile(terminationPath)
	if err != nil {
		t.Fatalf("read termination message: %v", err)
	}
	var got pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal termination message: %v", err)
	}
	if got.TaskUID != want.TaskUID || got.Result != want.Result || got.ExitCode != want.ExitCode {
		t.Fatalf("termination message = %#v, want %#v", got, want)
	}
}

func TestStub_SuccessMode_EmptyTestMode(t *testing.T) {
	// Dev == nil (no testMode) should behave identically to "success"
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	inPath := makeEnvelope(t, dir, "", []string{artifactDir})

	ctx := context.Background()
	code := run(ctx, inPath, os.Stdout, os.Stderr)
	if code != 0 {
		t.Fatalf("empty testMode: want exit 0, got %d", code)
	}

	out := readOutEnvelope(t, dir)
	if out.Result != "success" {
		t.Errorf("Result = %q, want %q", out.Result, "success")
	}
}

func TestStub_FailExit1Mode(t *testing.T) {
	dir := t.TempDir()
	inPath := makeEnvelope(t, dir, "fail-exit-1", []string{})

	ctx := context.Background()
	code := run(ctx, inPath, os.Stdout, os.Stderr)
	if code != 1 {
		t.Fatalf("fail-exit-1 mode: want exit 1, got %d", code)
	}

	out := readOutEnvelope(t, dir)
	if out.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", out.ExitCode)
	}
	if out.Result != "forced-failure" {
		t.Errorf("Result = %q, want %q", out.Result, "forced-failure")
	}
	if out.Reason != "stub testMode=fail-exit-1" {
		t.Errorf("Reason = %q, want %q", out.Reason, "stub testMode=fail-exit-1")
	}
	if out.CompletedAt.IsZero() {
		t.Errorf("CompletedAt is zero")
	}
}

func TestStub_HangMode(t *testing.T) {
	// Hang mode: run() blocks until ctx is cancelled. Use a short timeout
	// context to avoid blocking the test.
	dir := t.TempDir()
	inPath := makeEnvelope(t, dir, "hang", []string{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan int, 1)
	go func() {
		code := run(ctx, inPath, os.Stdout, os.Stderr)
		done <- code
	}()

	select {
	case code := <-done:
		// Context cancelled — stub should exit cleanly (0)
		if code != 0 {
			t.Errorf("hang mode on ctx cancel: want exit 0, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hang mode: run() did not return after context cancellation")
	}

	// out.json should NOT exist for hang mode (stub never writes it)
	outPath := filepath.Join(dir, "out.json")
	if _, err := os.Stat(outPath); err == nil {
		t.Errorf("hang mode: out.json should not be written")
	}
}

func TestStub_ExceedOutputPathsMode(t *testing.T) {
	dir := t.TempDir()
	// DeclaredOutputPaths does not include /workspace/escape
	inPath := makeEnvelope(t, dir, "exceed-output-paths", []string{filepath.Join(dir, "safe")})

	ctx := context.Background()
	code := run(ctx, inPath, os.Stdout, os.Stderr)
	if code != 1 {
		t.Fatalf("exceed-output-paths mode: want exit 1, got %d", code)
	}

	out := readOutEnvelope(t, dir)
	if out.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", out.ExitCode)
	}
	if out.Result != "output-paths-violation" {
		t.Errorf("Result = %q, want %q", out.Result, "output-paths-violation")
	}

	// Verify the leak file was written at /workspace/escape/leak.txt
	// (In tests, /workspace/escape may or may not exist, but the stub attempts to write there)
	leakPath := "/workspace/escape/leak.txt"
	if data, err := os.ReadFile(leakPath); err == nil {
		if string(data) != "leaked" {
			t.Errorf("leak.txt content = %q, want %q", string(data), "leaked")
		}
	}
	// The artifact list should include /workspace/escape/leak.txt
	if len(out.Artifacts) == 0 {
		t.Errorf("exceed-output-paths mode: Artifacts should not be empty")
	}
	foundLeak := false
	for _, a := range out.Artifacts {
		if a == "/workspace/escape/leak.txt" {
			foundLeak = true
		}
	}
	if !foundLeak {
		t.Errorf("exceed-output-paths mode: /workspace/escape/leak.txt not in Artifacts, got %v", out.Artifacts)
	}
}

func TestStub_InvalidEnvelope_BadAPIVersion(t *testing.T) {
	dir := t.TempDir()
	// Write an envelope with wrong apiVersion
	badEnv := map[string]interface{}{
		"apiVersion": "wrong.group/v1",
		"kind":       pkgdispatch.KindTaskEnvelopeIn,
		"taskUID":    "bad-uid",
	}
	data, _ := json.Marshal(badEnv)
	inPath := filepath.Join(dir, "in.json")
	if err := os.WriteFile(inPath, data, 0o644); err != nil {
		t.Fatalf("write in.json: %v", err)
	}

	ctx := context.Background()
	code := run(ctx, inPath, os.Stdout, os.Stderr)
	if code != 2 {
		t.Errorf("bad apiVersion: want exit 2, got %d", code)
	}
}

func TestStub_InvalidEnvelope_MissingFile(t *testing.T) {
	ctx := context.Background()
	code := run(ctx, "/nonexistent/path/in.json", os.Stdout, os.Stderr)
	if code != 2 {
		t.Errorf("missing file: want exit 2, got %d", code)
	}
}

func TestStub_UnknownTestMode(t *testing.T) {
	dir := t.TempDir()
	inPath := makeEnvelope(t, dir, "unknown-mode-xyz", []string{})

	ctx := context.Background()
	code := run(ctx, inPath, os.Stdout, os.Stderr)
	if code != 2 {
		t.Errorf("unknown testMode: want exit 2, got %d", code)
	}
}
