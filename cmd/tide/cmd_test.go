/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Plan 04-07 Task 1 — RED tests for the cobra-based `tide` CLI skeleton.
// Tests cover: help output enumerates all D-C3 verbs, root command exposes the
// kubectl-aligned persistent flags via genericclioptions, --version prints,
// signal cancel propagates to long-running subcommands, and the stub-verb
// surface is wired so `tide --help` lists the post-04-08 verbs today.
package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// newTestRoot returns a fresh root command tree for isolated testing — each
// test mutates flags/args independently. The production binary uses the
// package-level rootCmd (see main.go); the test-only constructor calls the
// same init seam.
func newTestRoot(t *testing.T) *cobra.Command {
	t.Helper()
	return buildRootForTest()
}

func TestHelpListsAllVerbs(t *testing.T) {
	root := newTestRoot(t)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}

	got := out.String()
	wantVerbs := []string{
		"apply",
		"watch",
		"tail",
		"approve",
		"reject",
		"cancel",
		"resume",
		"inspect-wave",
		"artifact-get",
		"describe-budget",
		"completion",
	}
	for _, v := range wantVerbs {
		if !strings.Contains(got, v) {
			t.Errorf("--help missing verb %q\nfull output:\n%s", v, got)
		}
	}
}

func TestVersionFlagPrintsStableShape(t *testing.T) {
	root := newTestRoot(t)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--version returned error: %v", err)
	}
	got := out.String()
	// Default version is "dev" (ldflag-overridable). Cobra renders it as the
	// program name + space + version + newline.
	if !strings.Contains(got, "dev") {
		t.Errorf("expected --version output to contain default 'dev'; got %q", got)
	}
}

func TestPersistentFlagsAvailable(t *testing.T) {
	root := newTestRoot(t)
	for _, name := range []string{"kubeconfig", "context", "namespace", "output"} {
		f := root.PersistentFlags().Lookup(name)
		if f == nil {
			t.Errorf("expected persistent flag --%s on root command", name)
		}
	}
	// Ensure -o short form maps to --output (kubectl convention).
	if f := root.PersistentFlags().ShorthandLookup("o"); f == nil || f.Name != "output" {
		t.Errorf("expected -o shorthand to map to --output; got %+v", f)
	}
}

func TestSignalCancelPropagatesToContext(t *testing.T) {
	// Build a sentinel subcommand that blocks on the cobra context and
	// records observed cancellation. The production cmd/tide/main.go threads
	// signal.NotifyContext into rootCmd.ExecuteContext; the equivalent here
	// is an explicit ctx-cancel via context.WithCancel.
	root := newTestRoot(t)
	var observed bool
	sentinel := &cobra.Command{
		Use: "blocker",
		RunE: func(cmd *cobra.Command, args []string) error {
			<-cmd.Context().Done()
			observed = true
			return cmd.Context().Err()
		},
	}
	root.AddCommand(sentinel)

	ctx, cancel := context.WithCancel(context.Background())
	root.SetArgs([]string{"blocker"})

	done := make(chan error, 1)
	go func() {
		done <- root.ExecuteContext(ctx)
	}()

	// Give the goroutine a moment to enter the RunE block, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunE did not return within 1s of ctx cancel")
	}
	if !observed {
		t.Fatal("RunE returned without observing ctx.Done()")
	}
}

func TestUseResolvesFromArgv0(t *testing.T) {
	// Pitfall 27 — `Use` is `filepath.Base(os.Args[0])` so Krew-installed
	// `kubectl-tide` and direct `tide` invocations both show the right name
	// in help output. Cobra surfaces the resolved Use in Command.Use.
	root := newTestRoot(t)
	if root.Use == "" {
		t.Fatal("expected non-empty Use on root command")
	}
	// In the test binary the basename is the *.test executable; we only
	// assert non-emptiness here. The production main.go anchors the real
	// behaviour.
	_ = os.Args[0]
}

func TestApplyRequiresFFlag(t *testing.T) {
	root := newTestRoot(t)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"apply"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected `tide apply` (no -f) to error; got nil")
	}
}

// TestStubVerbsRequireArgs replaces the plan 04-07-era stub assertion. The
// plan-04-08 write-back verbs (approve / reject / cancel / resume / tail) are
// now real subcommands — they no longer return "not yet implemented" errors.
// Instead, each rejects an invocation without the required <project> / <task>
// arg via cobra's Args: ExactArgs(1) guard.
func TestStubVerbsRequireArgs(t *testing.T) {
	for _, verb := range []string{"tail", "approve", "reject", "cancel", "resume"} {
		root := newTestRoot(t)
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{verb})
		err := root.Execute()
		if err == nil {
			t.Errorf("expected %q (no args) to return error; got nil", verb)
		}
	}
}
