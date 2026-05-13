// Command stub-subagent implements the TIDE canned subagent image used in
// integration tests (SUB-04 / D-F1..F3). It reads an EnvelopeIn JSON from
// the path given by --envelope (or $TIDE_ENVELOPE_PATH, or the default
// /workspace/envelopes/$TIDE_TASK_UID/in.json), dispatches on Dev.TestMode,
// writes an EnvelopeOut to the sibling out.json, and exits with the
// appropriate code:
//
//	0 — task succeeded (testMode=success or empty)
//	1 — task failed   (testMode=fail-exit-1)
//	2 — envelope error (bad apiVersion/kind, parse failure, unknown testMode)
//
// The stub imports github.com/jsquirrelz/tide/pkg/dispatch directly; this
// proves the D-A1 public-contract claim — if the contract breaks the stub
// fails to compile.
//
// Invocation:
//
//	stub-subagent --envelope /workspace/envelopes/$TIDE_TASK_UID/in.json
//
// Test modes (Dev.TestMode):
//
//	success             — writes canned result.txt + out.json; exit 0.
//	fail-exit-1         — writes failure out.json; exit 1.
//	hang                — loops until SIGTERM/context cancellation; exit 0.
//	exceed-output-paths — writes /workspace/escape/leak.txt + out.json; exit 0.
//	(empty)             — treated as "success".
//
// Real Claude-backed subagent images MUST ignore the Dev field entirely
// (RESEARCH.md Pitfall 9 / D-F1).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

func main() {
	fs := flag.NewFlagSet("stub-subagent", flag.ExitOnError)
	envelopePath := fs.String("envelope", "", "path to EnvelopeIn JSON (default: $TIDE_ENVELOPE_PATH or /workspace/envelopes/$TIDE_TASK_UID/in.json)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "stub-subagent: flag parse: %v\n", err)
		os.Exit(2)
	}

	// Resolve envelope path: flag → env var → default.
	if *envelopePath == "" {
		if p := os.Getenv("TIDE_ENVELOPE_PATH"); p != "" {
			*envelopePath = p
		} else {
			uid := os.Getenv("TIDE_TASK_UID")
			*envelopePath = fmt.Sprintf("/workspace/envelopes/%s/in.json", uid)
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	os.Exit(run(ctx, *envelopePath, os.Stdout, os.Stderr))
}

// run is the testable in-process entry point. It reads the EnvelopeIn at
// envelopePath, dispatches on Dev.TestMode, writes out.json, and returns
// the process exit code (0, 1, or 2).
func run(ctx context.Context, envelopePath string, stdout, stderr io.Writer) int {
	// Derive the output path: replace "in.json" basename with "out.json".
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")

	// Open + decode the envelope.
	env, err := loadEnvelope(envelopePath)
	if err != nil {
		fmt.Fprintf(stderr, "stub-subagent: envelope load: %v\n", err)
		// Attempt a best-effort failure envelope if we can derive the outPath.
		_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
			APIVersion:  pkgdispatch.APIVersionV1Alpha1,
			Kind:        pkgdispatch.KindTaskEnvelopeOut,
			ExitCode:    2,
			Result:      "invalid-envelope",
			Reason:      err.Error(),
			CompletedAt: time.Now().UTC(),
		})
		return 2
	}

	// Dispatch on TestMode.
	testMode := ""
	if env.Dev != nil {
		testMode = env.Dev.TestMode
	}

	switch testMode {
	case "", "success":
		return dispatchSuccess(ctx, env, outPath, stderr)

	case "fail-exit-1":
		return dispatchFail(env, outPath, stderr)

	case "hang":
		return dispatchHang(ctx)

	case "exceed-output-paths":
		return dispatchExceedOutputPaths(env, outPath, stderr)

	default:
		fmt.Fprintf(stderr, "stub-subagent: unknown testMode %q\n", testMode)
		_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
			APIVersion:  pkgdispatch.APIVersionV1Alpha1,
			Kind:        pkgdispatch.KindTaskEnvelopeOut,
			TaskUID:     env.TaskUID,
			ExitCode:    2,
			Result:      "invalid-envelope",
			Reason:      fmt.Sprintf("unknown testMode %q", testMode),
			CompletedAt: time.Now().UTC(),
		})
		return 2
	}
}

// loadEnvelope opens path, parses it as JSON into EnvelopeIn, and validates
// the apiVersion + kind discriminators via pkgdispatch.ValidateAPIVersionKind.
func loadEnvelope(path string) (pkgdispatch.EnvelopeIn, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pkgdispatch.EnvelopeIn{}, fmt.Errorf("read %s: %w", path, err)
	}
	var env pkgdispatch.EnvelopeIn
	if err := json.Unmarshal(data, &env); err != nil {
		return pkgdispatch.EnvelopeIn{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := pkgdispatch.ValidateAPIVersionKind(env.APIVersion, env.Kind, pkgdispatch.KindTaskEnvelopeIn); err != nil {
		return pkgdispatch.EnvelopeIn{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return env, nil
}

// writeEnvelope marshals out and writes it to path, creating parent dirs as
// needed. Errors are ignored by callers using the "_" discard pattern; only
// the best-effort attempt on envelope-load failure uses this.
func writeEnvelope(path string, out pkgdispatch.EnvelopeOut) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal out.json: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// dispatchSuccess handles testMode == "success" (or empty). It writes a canned
// result.txt under the first DeclaredOutputPath and then writes out.json.
func dispatchSuccess(_ context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	artifacts := []string{}

	if len(env.DeclaredOutputPaths) > 0 {
		first := env.DeclaredOutputPaths[0]
		if err := os.MkdirAll(first, 0o755); err != nil {
			fmt.Fprintf(stderr, "stub-subagent: mkdir %s: %v\n", first, err)
		} else {
			resultPath := filepath.Join(first, "result.txt")
			if err := os.WriteFile(resultPath, []byte("stub success"), 0o644); err != nil {
				fmt.Fprintf(stderr, "stub-subagent: write result.txt: %v\n", err)
			} else {
				artifacts = append(artifacts, resultPath)
			}
		}
	}

	out := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    env.TaskUID,
		ExitCode:   0,
		Result:     "success",
		Reason:     "stub testMode=success",
		Usage: pkgdispatch.Usage{
			InputTokens:        100,
			OutputTokens:       200,
			EstimatedCostCents: 1,
			Iterations:         1,
		},
		Artifacts:   artifacts,
		CompletedAt: time.Now().UTC(),
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "stub-subagent: write out.json: %v\n", err)
		return 2
	}
	return 0
}

// dispatchFail handles testMode == "fail-exit-1". Writes a structured failure
// out.json and returns 1.
func dispatchFail(env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	out := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    env.TaskUID,
		ExitCode:   1,
		Result:     "forced-failure",
		Reason:     "stub testMode=fail-exit-1",
		Usage: pkgdispatch.Usage{
			InputTokens:        0,
			OutputTokens:       0,
			EstimatedCostCents: 0,
			Iterations:         0,
		},
		CompletedAt: time.Now().UTC(),
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "stub-subagent: write out.json: %v\n", err)
	}
	return 1
}

// dispatchHang handles testMode == "hang". Blocks until the context is
// cancelled (via SIGTERM/SIGINT installed by main, or ctx cancellation in
// tests). Never writes out.json. Returns 0 on clean cancellation.
func dispatchHang(ctx context.Context) int {
	// Loop with time.Sleep so the goroutine is interruptible via ctx.
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(time.Hour):
			// Keep looping — hang forever until killed.
		}
	}
}

// dispatchExceedOutputPaths handles testMode == "exceed-output-paths". Writes
// /workspace/escape/leak.txt (outside DeclaredOutputPaths — deliberate
// violation for HARN-05 harness tests), then writes a success out.json.
func dispatchExceedOutputPaths(env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	leakPath := "/workspace/escape/leak.txt"
	_ = os.MkdirAll(filepath.Dir(leakPath), 0o755)
	_ = os.WriteFile(leakPath, []byte("leaked"), 0o644)

	out := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    env.TaskUID,
		ExitCode:   0,
		Result:     "success",
		Reason:     "stub testMode=exceed-output-paths",
		Usage: pkgdispatch.Usage{
			InputTokens:        100,
			OutputTokens:       100,
			EstimatedCostCents: 1,
			Iterations:         1,
		},
		Artifacts:   []string{leakPath},
		CompletedAt: time.Now().UTC(),
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "stub-subagent: write out.json: %v\n", err)
		return 2
	}
	return 0
}
