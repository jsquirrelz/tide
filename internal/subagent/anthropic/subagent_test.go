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

package anthropic

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// fixtureStreamJSON is the canned stream-json content used by the happy-path
// test. It mirrors RESEARCH Pattern 5: a system/init, one stream_event delta,
// and a final result event with all 4 usage fields.
const fixtureStreamJSON = `{"type":"system/init","session_id":"sess-1"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}}
{"type":"result","result":"hello world","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":30,"cache_creation_input_tokens":20},"total_cost_usd":0.001}
`

// TestRun_VendorMismatch asserts the Anthropic subagent refuses an envelope
// whose Provider.Vendor is not "anthropic" — without exec'ing claude. This is
// the fail-fast defense per pkg/dispatch/provider.go godoc: image-tag-vs-
// envelope drift catches at startup instead of mid-flight.
func TestRun_VendorMismatch(t *testing.T) {
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude", // would explode if ever exec'd
		WorkspaceRoot: t.TempDir(),
	})
	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-vendor-mismatch",
		Role:       "executor",
		Level:      "task",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "openai",
			Model:  "gpt-4",
		},
	}
	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("expected vendor-mismatch error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "vendor") || !strings.Contains(msg, "anthropic") {
		t.Errorf("error should mention vendor and anthropic; got %q", msg)
	}
}

// TestRun_UnknownParam asserts Q3 RESOLVED params allow-list — unknown keys
// (anything outside {temperature, thinking_budget, top_p, top_k}) cause a
// fail-fast error before exec'ing claude. Matches 03-RESEARCH §"Open
// Questions Q3 RESOLVED" — reject-unknown at subagent startup.
func TestRun_UnknownParam(t *testing.T) {
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: t.TempDir(),
	})
	in := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    "uid-bad-param",
		Role:       "executor",
		Level:      "task",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
			Params: map[string]string{
				"unknown_key": "value",
			},
		},
	}
	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("expected unknown-param error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "unknown") && !strings.Contains(strings.ToLower(msg), "param") {
		t.Errorf("error should mention 'unknown' or 'param'; got %q", msg)
	}
}

// TestRun_AllowedParams asserts each key in the Q3 allow-list (temperature,
// thinking_budget, top_p, top_k) passes the params check. This is a unit-test
// negation of TestRun_UnknownParam: with only allowed keys, the params gate
// must not fire. We use a fake exec that feeds the fixture stream-json so
// the test runs end-to-end without claude on PATH.
func TestRun_AllowedParams(t *testing.T) {
	tmp := t.TempDir()
	a := newFakeExecAnthropic(t, tmp, fixtureStreamJSON)
	in := envelopeFixture("uid-allowed-params")
	in.Provider.Params = map[string]string{
		"temperature":     "0.7",
		"thinking_budget": "20000",
		"top_p":           "0.95",
		"top_k":           "40",
	}
	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run with allowed params: %v", err)
	}
	if out.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", out.ExitCode)
	}
}

// TestRun_HappyPath asserts the end-to-end exec → stream-json parse →
// EnvelopeOut path. Uses a fake exec that runs `bash -c 'cat <fixture>'`
// so the test does not depend on the claude CLI being installed.
// Validates that:
//   - EnvelopeOut.Usage carries the parsed token tally for all 4 fields.
//   - EnvelopeOut.Result carries the final assistant text.
//   - events.jsonl is written under WorkspaceRoot/envelopes/{TaskUID}/.
//   - EnvelopeOut.APIVersion / Kind / TaskUID are set per pkg/dispatch.
func TestRun_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	a := newFakeExecAnthropic(t, tmp, fixtureStreamJSON)
	in := envelopeFixture("uid-happy-path")
	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if out.APIVersion != pkgdispatch.APIVersionV1Alpha1 {
		t.Errorf("APIVersion: got %q, want %q", out.APIVersion, pkgdispatch.APIVersionV1Alpha1)
	}
	if out.Kind != pkgdispatch.KindTaskEnvelopeOut {
		t.Errorf("Kind: got %q, want %q", out.Kind, pkgdispatch.KindTaskEnvelopeOut)
	}
	if out.TaskUID != in.TaskUID {
		t.Errorf("TaskUID: got %q, want %q", out.TaskUID, in.TaskUID)
	}
	if out.ExitCode != 0 {
		t.Errorf("ExitCode: got %d, want 0", out.ExitCode)
	}
	if out.Result != "hello world" {
		t.Errorf("Result: got %q, want %q", out.Result, "hello world")
	}
	if out.Usage.InputTokens != 100 || out.Usage.OutputTokens != 50 {
		t.Errorf("Usage tokens: got %+v, want In=100 Out=50", out.Usage)
	}
	if out.Usage.CacheReadTokens != 30 || out.Usage.CacheCreationTokens != 20 {
		t.Errorf("Usage cache tokens: got %+v, want CR=30 CC=20", out.Usage)
	}

	// events.jsonl must exist under WorkspaceRoot/envelopes/{TaskUID}/
	eventsPath := filepath.Join(tmp, "envelopes", in.TaskUID, "events.jsonl")
	data, rerr := os.ReadFile(eventsPath)
	if rerr != nil {
		t.Fatalf("events.jsonl not written at %s: %v", eventsPath, rerr)
	}
	if !strings.Contains(string(data), `"type":"result"`) {
		t.Errorf("events.jsonl missing result event; got:\n%s", string(data))
	}
}

// TestRun_ExecFailure asserts that when the underlying exec.Cmd exits
// non-zero, Run() surfaces a non-zero ExitCode and a populated Reason in
// EnvelopeOut. Dispatch-level success vs task-level failure are different
// concerns (pkg/dispatch/subagent.go godoc) — we return (EnvelopeOut, nil)
// for task failure, not (EnvelopeOut, err).
func TestRun_ExecFailure(t *testing.T) {
	tmp := t.TempDir()
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude", // unused — overridden below
		WorkspaceRoot: tmp,
	})
	// Override execFunc to invoke `false` (always exits 1) — simulates a
	// claude crash. The harness ParseStream still runs against empty stdout;
	// the non-zero exit drives ExitCode/Reason.
	a.execFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "false")
	}

	in := envelopeFixture("uid-exec-fail")
	out, err := a.Run(context.Background(), in)
	if err != nil {
		// Per pkg/dispatch contract, dispatch-level (network/I/O) failures
		// return non-nil err. exec failing to start IS a dispatch-level fail,
		// but exit 1 from the CLI is a task-level fail. `false` exits cleanly
		// non-zero so we expect (out, nil) here with ExitCode != 0.
		t.Fatalf("Run returned dispatch-level error for task-level fail: %v", err)
	}
	if out.ExitCode == 0 {
		t.Error("expected ExitCode != 0, got 0")
	}
	if out.Reason == "" {
		t.Error("expected non-empty Reason on exec failure")
	}
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// envelopeFixture returns a populated EnvelopeIn for tests that need a
// well-formed input. Provider.Vendor is "anthropic" so the vendor fail-fast
// does not fire.
func envelopeFixture(uid string) pkgdispatch.EnvelopeIn {
	return pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    uid,
		Role:       "executor",
		Level:      "task",
		Prompt:     "fixture prompt",
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "anthropic",
			Model:  "claude-sonnet-4-6",
		},
		ProxyEndpoint: "https://127.0.0.1:8443",
		SignedToken:   "fixture-signed-token",
	}
}

// newFakeExecAnthropic returns an *Anthropic whose execFunc invokes
// `bash -c 'cat <fixture-file>'` so the test exercises the parse path
// without needing the claude CLI on PATH. The fixture content is written to
// a temp file under workspaceRoot.
func newFakeExecAnthropic(t *testing.T, workspaceRoot, fixtureContent string) *Anthropic {
	t.Helper()
	fixturePath := filepath.Join(workspaceRoot, "fixture.jsonl")
	if err := os.WriteFile(fixturePath, []byte(fixtureContent), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude", // overridden via execFunc
		WorkspaceRoot: workspaceRoot,
	})
	a.execFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		// Ignore name/args — we replace them with a deterministic cat-the-fixture.
		return exec.CommandContext(ctx, "bash", "-c", "cat "+fixturePath)
	}
	return a
}

// TestRun_PromptViaStdinAndPermissionFlags locks in the defect #7 + #8 fixes:
//   - #8: the rendered prompt is NOT passed as the `-p` positional argv entry
//     (which risks Linux MAX_ARG_STRLEN for large planner prompts) — it is
//     delivered via STDIN. So renderedPrompt must be absent from cmd.Args and
//     present on cmd.Stdin.
//   - #7: the invocation grants the headless Write/Edit capability scoped to
//     the per-Task events dir, i.e. `--permission-mode acceptEdits` and
//     `--add-dir <eventsDir>` appear in cmd.Args, while `--bare` is retained.
//
// The fake exec captures the args, drains stdin into a temp file (proving the
// prompt was delivered there), then emits the canned stream-json on stdout so
// the parse path still completes.
func TestRun_PromptViaStdinAndPermissionFlags(t *testing.T) {
	tmp := t.TempDir()

	fixturePath := filepath.Join(tmp, "fixture.jsonl")
	if err := os.WriteFile(fixturePath, []byte(fixtureStreamJSON), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stdinCapturePath := filepath.Join(tmp, "captured-stdin.txt")

	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude", // overridden via execFunc
		WorkspaceRoot: tmp,
	})

	var capturedArgs []string
	a.execFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = append([]string(nil), args...)
		// Drain stdin -> capture file, then emit the fixture on stdout so the
		// parser still sees a well-formed stream. Run() sets cmd.Stdin after
		// this returns, so the spawned shell receives the rendered prompt.
		script := "cat > " + stdinCapturePath + "; cat " + fixturePath
		return exec.CommandContext(ctx, "bash", "-c", script)
	}

	in := envelopeFixture("uid-stdin-flags")
	const wantPrompt = "THIS-IS-THE-RENDERED-PROMPT-SENTINEL"
	in.Prompt = wantPrompt // task executor template echoes nothing structural; sentinel just needs to render

	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("ExitCode: got %d want 0 (reason=%q)", out.ExitCode, out.Reason)
	}

	// (a) renderedPrompt must NOT be a positional argv entry. The bare "-p"
	// flag is present, but no element of Args may carry the prompt sentinel.
	joined := strings.Join(capturedArgs, "\x00")
	if strings.Contains(joined, wantPrompt) {
		t.Errorf("rendered prompt leaked into cmd.Args (defect #8 regression): %v", capturedArgs)
	}
	if !containsArg(capturedArgs, "-p") {
		t.Errorf("expected bare -p flag in args, got %v", capturedArgs)
	}

	// (b) the prompt must have been delivered via stdin.
	stdinData, rerr := os.ReadFile(stdinCapturePath)
	if rerr != nil {
		t.Fatalf("read captured stdin: %v", rerr)
	}
	if !strings.Contains(string(stdinData), wantPrompt) {
		t.Errorf("rendered prompt not delivered via stdin; captured=%q", string(stdinData))
	}

	// (c) headless write capability flags, scoped to the per-Task events dir,
	// with --bare retained.
	wantEventsDir := filepath.Join(tmp, "envelopes", in.TaskUID)
	assertFlagPair(t, capturedArgs, "--permission-mode", "acceptEdits")
	assertFlagPair(t, capturedArgs, "--add-dir", wantEventsDir)
	if !containsArg(capturedArgs, "--bare") {
		t.Errorf("--bare flag dropped (hermeticity regression): %v", capturedArgs)
	}
}

// TestRun_PricingFallbackModel covers the D-02 provider side (Phase 38
// COST-02): when the dispatch model misses the effective price table even
// after the -YYYYMMDD normalizer, Run stamps Usage.PricingFallbackModel with
// the unmatched model ID so the controller can roll it up into the
// PricingFallbackActive condition and Prometheus counter. Priced models —
// exact or date-suffixed — leave the field empty.
func TestRun_PricingFallbackModel(t *testing.T) {
	t.Run("unknown_model_stamped", func(t *testing.T) {
		tmp := t.TempDir()
		a := newFakeExecAnthropic(t, tmp, fixtureStreamJSON)
		in := envelopeFixture("uid-fallback-unknown")
		in.Provider.Model = "claude-nova-9"
		out, err := a.Run(context.Background(), in)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if out.Usage.PricingFallbackModel != "claude-nova-9" {
			t.Errorf("PricingFallbackModel: got %q, want %q (unknown model must be stamped)",
				out.Usage.PricingFallbackModel, "claude-nova-9")
		}
	})

	t.Run("known_model_empty", func(t *testing.T) {
		tmp := t.TempDir()
		a := newFakeExecAnthropic(t, tmp, fixtureStreamJSON)
		in := envelopeFixture("uid-fallback-known") // claude-sonnet-4-6 — in the table
		out, err := a.Run(context.Background(), in)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if out.Usage.PricingFallbackModel != "" {
			t.Errorf("PricingFallbackModel: got %q, want empty for a priced model",
				out.Usage.PricingFallbackModel)
		}
	})

	t.Run("dated_known_model_empty", func(t *testing.T) {
		// A normalizer hit is NOT a fallback — the date-suffixed ID resolved to
		// a real row (D-01), so no flag.
		tmp := t.TempDir()
		a := newFakeExecAnthropic(t, tmp, fixtureStreamJSON)
		in := envelopeFixture("uid-fallback-dated")
		in.Provider.Model = "claude-sonnet-5-20260514"
		out, err := a.Run(context.Background(), in)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if out.Usage.PricingFallbackModel != "" {
			t.Errorf("PricingFallbackModel: got %q, want empty for a normalizer-resolved model",
				out.Usage.PricingFallbackModel)
		}
	})
}

// containsArg reports whether want appears as a standalone element of args.
func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}

// assertFlagPair asserts that flag is immediately followed by value in args.
func assertFlagPair(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag {
			if i+1 < len(args) && args[i+1] == value {
				return
			}
			t.Errorf("flag %q present but not followed by %q (args=%v)", flag, value, args)
			return
		}
	}
	t.Errorf("flag %q missing from args=%v", flag, args)
}

// writeChildPromptJSON writes a prompt artifact in the children JSON format at
// the given path. The artifact matches childPromptFile: {"spec":{"prompt":"<p>"}}.
func writeChildPromptJSON(t *testing.T, path, prompt string) {
	t.Helper()
	data := []byte(`{"kind":"Task","name":"task-01","spec":{"prompt":"` + prompt + `"}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for prompt artifact: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write prompt artifact: %v", err)
	}
}

// TestPromptPath_HappyPath asserts that when in.PromptPath is set and the artifact
// exists under WorkspaceRoot, Run reads the artifact and renders {{.Prompt}} from it.
func TestPromptPath_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	const wantPrompt = "implement FormattedNow as specified"

	// Write the prompt artifact at <WorkspaceRoot>/envelopes/planner-uid/children/task-01.json
	artifactPath := filepath.Join(tmp, "envelopes", "planner-uid", "children", "task-01.json")
	writeChildPromptJSON(t, artifactPath, wantPrompt)

	stdinCapturePath := filepath.Join(tmp, "captured-stdin.txt")
	fixturePath := filepath.Join(tmp, "fixture.jsonl")
	if err := os.WriteFile(fixturePath, []byte(fixtureStreamJSON), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: tmp,
	})
	// Capture stdin so we can verify the prompt was populated from the artifact.
	script := "cat > " + stdinCapturePath + "; cat " + fixturePath
	a.execFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "bash", "-c", script)
	}

	in := envelopeFixture("uid-promptpath-happy")
	in.Prompt = ""                                                // empty — must be populated from PromptPath
	in.PromptPath = "envelopes/planner-uid/children/task-01.json" // workspace-relative

	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("ExitCode: got %d want 0 (reason=%q)", out.ExitCode, out.Reason)
	}
	// The artifact's .spec.prompt must have been delivered via stdin.
	stdinData, rerr := os.ReadFile(stdinCapturePath)
	if rerr != nil {
		t.Fatalf("read captured stdin: %v", rerr)
	}
	if !strings.Contains(string(stdinData), wantPrompt) {
		t.Errorf("artifact prompt not delivered via stdin; captured=%q", string(stdinData))
	}
}

// TestPromptPath_AbsoluteRejected asserts that an absolute PromptPath is rejected
// before any file read (traversal defense T-09-05).
func TestPromptPath_AbsoluteRejected(t *testing.T) {
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: t.TempDir(),
	})
	in := envelopeFixture("uid-abs")
	in.Prompt = ""
	in.PromptPath = "/etc/passwd"

	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("want error for absolute PromptPath, got nil")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should mention 'absolute'; got %q", err.Error())
	}
}

// TestPromptPath_TraversalRejected asserts that a "../escape" PromptPath is
// rejected (traversal defense T-09-05).
func TestPromptPath_TraversalRejected(t *testing.T) {
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: t.TempDir(),
	})
	in := envelopeFixture("uid-traversal")
	in.Prompt = ""
	in.PromptPath = "../../../../etc/passwd"

	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("want error for traversal PromptPath, got nil")
	}
	if !strings.Contains(err.Error(), "..") && !strings.Contains(err.Error(), "traversal") && !strings.Contains(err.Error(), "workspace") {
		t.Errorf("error should indicate traversal rejection; got %q", err.Error())
	}
}

// TestPromptPath_OutsideRootRejected asserts that a resolved path outside
// WorkspaceRoot is rejected (second-line traversal defense).
func TestPromptPath_OutsideRootRejected(t *testing.T) {
	tmp := t.TempDir()
	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: filepath.Join(tmp, "workspace"),
	})
	in := envelopeFixture("uid-outside")
	in.Prompt = ""
	// A clean relative path that happens to resolve outside the workspace root
	// because WorkspaceRoot is a sub-dir of tmp: sibling/ escapes workspace/.
	in.PromptPath = "../sibling/evil.json"

	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("want error for out-of-root PromptPath, got nil")
	}
}

// TestPromptPath_EmptySpecPromptRejected asserts that an artifact with an
// empty .spec.prompt causes a hard error (no silent empty-prompt dispatch, #4 class).
func TestPromptPath_EmptySpecPromptRejected(t *testing.T) {
	tmp := t.TempDir()
	// Write artifact with empty spec.prompt.
	artifactPath := filepath.Join(tmp, "envelopes", "empty", "task.json")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte(`{"spec":{"prompt":""}}`), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: tmp,
	})
	in := envelopeFixture("uid-emptyspec")
	in.Prompt = ""
	in.PromptPath = "envelopes/empty/task.json"

	_, err := a.Run(context.Background(), in)
	if err == nil {
		t.Fatal("want error for empty .spec.prompt, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty'; got %q", err.Error())
	}
}

// TestPromptPath_EmptyPath_PlannerPath asserts that when PromptPath is empty
// and Prompt is non-empty (planner path), Run uses in.Prompt unchanged.
func TestPromptPath_EmptyPath_PlannerPath(t *testing.T) {
	tmp := t.TempDir()
	fixturePath := filepath.Join(tmp, "fixture.jsonl")
	if err := os.WriteFile(fixturePath, []byte(fixtureStreamJSON), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stdinCapturePath := filepath.Join(tmp, "stdin.txt")

	a := New(Options{
		ClaudeBinary:  "/nonexistent/claude",
		WorkspaceRoot: tmp,
	})
	script := "cat > " + stdinCapturePath + "; cat " + fixturePath
	a.execFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "bash", "-c", script)
	}

	const plannerPrompt = "PLANNER-OUTCOME-SENTINEL"
	in := envelopeFixture("uid-planner-path")
	in.Prompt = plannerPrompt
	in.PromptPath = "" // planner path: PromptPath empty, Prompt set directly

	out, err := a.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.ExitCode != 0 {
		t.Fatalf("ExitCode: got %d want 0", out.ExitCode)
	}
	stdinData, rerr := os.ReadFile(stdinCapturePath)
	if rerr != nil {
		t.Fatalf("read captured stdin: %v", rerr)
	}
	if !strings.Contains(string(stdinData), plannerPrompt) {
		t.Errorf("planner prompt not in stdin; captured=%q", string(stdinData))
	}
}
