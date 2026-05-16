// Command claude-subagent — Phase 3 real-Claude swap of cmd/stub-subagent.
// Thin shim: load EnvelopeIn → anthropic.Run → write EnvelopeOut.
// Real Claude images MUST ignore env.Dev (PATTERNS.md line 442 / D-F1).
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

	"github.com/jsquirrelz/tide/internal/harness"
	"github.com/jsquirrelz/tide/internal/subagent/anthropic"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// anthropicRunner is the test-seam surface (matches pkg/dispatch.Subagent).
type anthropicRunner interface {
	Run(ctx context.Context, in pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error)
}

// newSubagent is the constructor seam — tests override it to inject a
// fake-exec anthropic Subagent via the package's NewWithExec helper.
var newSubagent = func(claudeBinary, workspaceRoot string) anthropicRunner {
	return anthropic.New(anthropic.Options{
		ClaudeBinary:  claudeBinary,
		WorkspaceRoot: workspaceRoot,
	})
}

func main() {
	fs := flag.NewFlagSet("claude-subagent", flag.ExitOnError)
	envelopePath := fs.String("envelope-path", "/workspace/envelopes/in.json", "path to EnvelopeIn JSON")
	workspaceRoot := fs.String("workspace-root", "/workspace", "PVC mount root")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	os.Exit(run(ctx, *envelopePath, *workspaceRoot, os.Stdout, os.Stderr))
}

// run is the testable entry point. It does NOT branch on the envelope's
// Dev.TestMode field — that behavior is stub-only (PATTERNS.md line 442).
func run(ctx context.Context, envelopePath, workspaceRoot string, stdout, stderr io.Writer) int {
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	env, err := harness.ReadEnvelopeIn(envelopePath)
	if err != nil {
		fmt.Fprintf(stderr, "claude-subagent: %v\n", err)
		_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
			APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeOut,
			ExitCode: 2, Result: "invalid-envelope", Reason: err.Error(),
			CompletedAt: time.Now().UTC(),
		})
		return 2
	}
	sa := newSubagent("claude", workspaceRoot)
	out, runErr := sa.Run(ctx, env)
	if runErr != nil {
		// Wrap dispatch-level error (vendor mismatch, template load, parse) as
		// failure EnvelopeOut so the controller surfaces a structured Reason.
		out = pkgdispatch.EnvelopeOut{
			APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeOut,
			TaskUID: env.TaskUID, ExitCode: 1, Result: "subagent-error", Reason: runErr.Error(),
			CompletedAt: time.Now().UTC(),
		}
		fmt.Fprintf(stderr, "claude-subagent: %v\n", runErr)
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "claude-subagent: write out.json: %v\n", err)
		return 2
	}
	return out.ExitCode
}

// writeEnvelope persists EnvelopeOut to PVC and the K8s termination-message
// path (Phase 02.2 cascade-10 PodStatusEnvelopeReader fallback).
func writeEnvelope(path string, out pkgdispatch.EnvelopeOut) error {
	if err := harness.WriteEnvelopeOut(path, out); err != nil {
		return err
	}
	data, _ := json.Marshal(out)
	tp := os.Getenv("TIDE_TERMINATION_MESSAGE_PATH")
	if tp == "" {
		tp = "/dev/termination-log"
	}
	_ = os.WriteFile(tp, data, 0o644)
	return nil
}
