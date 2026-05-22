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
	"strings"
	"syscall"
	"time"

	"github.com/jsquirrelz/tide/internal/harness"
	"github.com/jsquirrelz/tide/internal/subagent/anthropic"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

type anthropicRunner interface {
	Run(ctx context.Context, in pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error)
}

var newSubagent = func(claudeBinary, workspaceRoot string) anthropicRunner {
	return anthropic.New(anthropic.Options{ClaudeBinary: claudeBinary, WorkspaceRoot: workspaceRoot})
}
var ensureWorktreeFunc = harness.EnsureWorktree

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

// run is the testable entry point. Does NOT branch on env.Dev.TestMode.
func run(ctx context.Context, envelopePath, workspaceRoot string, stdout, stderr io.Writer) int {
	outPath := filepath.Join(filepath.Dir(envelopePath), "out.json")
	env, err := harness.ReadEnvelopeIn(envelopePath)
	if err != nil {
		return failOut(stderr, outPath, "", err, 2, "invalid-envelope")
	}
	if err := ensureWorktreeFunc(env, workspaceRoot, readBranch(envelopePath)); err != nil {
		return failOut(stderr, outPath, env.TaskUID, err, 1, "worktree-setup-failed")
	}
	out, runErr := newSubagent("claude", workspaceRoot).Run(ctx, env)
	if runErr != nil {
		fmt.Fprintf(stderr, "claude-subagent: %v\n", runErr)
		out = failEnvelope(env.TaskUID, runErr, 1, "subagent-error")
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "claude-subagent: write out.json: %v\n", err)
		return 2
	}
	return out.ExitCode
}

func readBranch(envelopePath string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(envelopePath), "branch.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\r\n")
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
