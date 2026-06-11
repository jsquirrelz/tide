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

// Command claude-subagent — Phase 3 real-Claude swap of stub-subagent. Thin
// shim: load EnvelopeIn → anthropic.Run → write EnvelopeOut. Ignores the
// envelope's Dev struct (PATTERNS.md line 442 / D-F1).
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

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/jsquirrelz/tide/internal/harness"
	"github.com/jsquirrelz/tide/internal/subagent/anthropic"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// parsePricingOverridesFromEnv reads TIDE_PRICING_OVERRIDES_JSON and parses it.
// An empty or missing env var returns a nil map (no overrides). An invalid JSON
// value logs a loud stderr warning and returns nil (bad override must not fail
// the session — Plan 14-05 validates at manager startup; this is defense in depth).
func parsePricingOverridesFromEnv(stderr io.Writer) map[string]pkgdispatch.PriceOverride {
	raw := os.Getenv("TIDE_PRICING_OVERRIDES_JSON")
	if raw == "" {
		return nil
	}
	overrides, err := pkgdispatch.ParsePricingOverrides(raw)
	if err != nil {
		fmt.Fprintf(stderr, "pricing: invalid TIDE_PRICING_OVERRIDES_JSON, ignoring overrides: %v\n", err)
		return nil
	}
	return overrides
}

type anthropicRunner interface {
	Run(ctx context.Context, in pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error)
}

var newSubagent = func(claudeBinary, workspaceRoot string, pricingOverrides map[string]pkgdispatch.PriceOverride) anthropicRunner {
	return anthropic.New(anthropic.Options{
		ClaudeBinary:     claudeBinary,
		WorkspaceRoot:    workspaceRoot,
		PricingOverrides: pricingOverrides,
	})
}
var ensureWorktreeFunc = harness.EnsureWorktree
var commitWorktreeFunc = func(worktreeDir, taskUID string) (plumbing.Hash, bool, error) {
	return harness.CommitWorktree(worktreeDir, taskUID)
}

func main() {
	fs := flag.NewFlagSet("claude-subagent", flag.ExitOnError)
	envelopePath := fs.String("envelope-path", "",
		"path to EnvelopeIn JSON (default: $TIDE_ENVELOPE_PATH or /workspace/envelopes/$TIDE_TASK_UID/in.json)")
	workspaceRoot := fs.String("workspace-root", "/workspace", "PVC mount root")
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	// Resolve envelope path: flag → $TIDE_ENVELOPE_PATH → per-task-uid default.
	// This MUST match stub-subagent (cmd/stub-subagent/main.go) and the
	// controller's write location, which is /workspace/envelopes/$TIDE_TASK_UID/in.json
	// (the controller passes no --envelope-path arg, so it relies on this default).
	// The previous hardcoded /workspace/envelopes/in.json default did not include
	// the per-task-uid subdirectory, so the real claude planner/executor could never
	// find its input envelope. Only surfaced in live real-Claude runs — CI exercises
	// the stub, whose default already matched.
	if *envelopePath == "" {
		if p := os.Getenv("TIDE_ENVELOPE_PATH"); p != "" {
			*envelopePath = p
		} else {
			*envelopePath = fmt.Sprintf("/workspace/envelopes/%s/in.json", os.Getenv("TIDE_TASK_UID"))
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	os.Exit(run(ctx, *envelopePath, *workspaceRoot, os.Stdout, os.Stderr))
}

// run is the testable entry point. Does NOT branch on env.Dev.TestMode.
//
//nolint:unparam // stdout is part of the shared subagent-binary (stdout, stderr) entry-point contract
func run(ctx context.Context, envelopePath, workspaceRoot string, stdout, stderr io.Writer) int {
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	env, err := harness.ReadEnvelopeIn(envelopePath)
	if err != nil {
		return failOut(stderr, outPath, "", err, 2, "invalid-envelope")
	}
	if err := ensureWorktreeFunc(env, workspaceRoot, env.Branch); err != nil {
		return failOut(stderr, outPath, env.TaskUID, err, 1, "worktree-setup-failed")
	}
	// D-02: read pricing overrides from TIDE_PRICING_OVERRIDES_JSON env.
	// Invalid JSON logs a loud warning and falls back to compiled table (defense in depth).
	pricingOverrides := parsePricingOverridesFromEnv(stderr)
	out, runErr := newSubagent("claude", workspaceRoot, pricingOverrides).Run(ctx, env)
	if runErr != nil {
		fmt.Fprintf(stderr, "claude-subagent: %v\n", runErr)
		out = failEnvelope(env.TaskUID, runErr, 1, "subagent-error")
	}
	if runErr == nil && env.Role == "executor" {
		worktreeDir := filepath.Join(workspaceRoot, "worktrees", env.TaskUID)
		hash, isEmpty, commitErr := commitWorktreeFunc(worktreeDir, env.TaskUID)
		if commitErr != nil {
			out = failEnvelope(env.TaskUID, commitErr, 1, "commit-failed")
		} else if isEmpty {
			out.ExitCode = 1
			out.Result = "empty-diff"
			out.Reason = "executor produced no changes in worktree"
		} else {
			out.Git = &pkgdispatch.GitOutput{HeadSHA: hash.String()}
		}
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "claude-subagent: write out.json: %v\n", err)
		return 2
	}
	return out.ExitCode
}

func failEnvelope(taskUID string, err error, exitCode int, result string) pkgdispatch.EnvelopeOut {
	return pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeOut,
		TaskUID: taskUID, ExitCode: exitCode, Result: result, Reason: err.Error(),
		CompletedAt: time.Now().UTC(),
	}
}

func failOut(stderr io.Writer, outPath, taskUID string, err error, exitCode int, result string) int {
	fmt.Fprintf(stderr, "claude-subagent: %v\n", err)
	_ = writeEnvelope(outPath, failEnvelope(taskUID, err, exitCode, result))
	return exitCode
}

func writeEnvelope(path string, out pkgdispatch.EnvelopeOut) error {
	// Write the full envelope to the PVC (audit artifact + reporter Job input).
	// This is UNCHANGED — out.json on the PVC carries ChildCRDs + verbose Result.
	if err := harness.WriteEnvelopeOut(path, out); err != nil {
		return err
	}
	// Write only the tiny TerminationStub to the termination message path.
	// The stub carries ExitCode/Reason/Usage/HeadSHA and stays <4KB regardless
	// of ChildCRDs or Result size (#11 root cause fix; T-09-03 mitigation).
	stub := pkgdispatch.NewTerminationStub(out)
	data, _ := json.Marshal(stub)
	tp := os.Getenv("TIDE_TERMINATION_MESSAGE_PATH")
	if tp == "" {
		tp = "/dev/termination-log"
	}
	_ = os.WriteFile(tp, data, 0o644)
	return nil
}
