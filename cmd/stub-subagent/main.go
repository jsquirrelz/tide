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
//	exceed-output-paths — writes /workspace/escape/leak.txt + failure out.json; exit 1.
//	wait-for-signal     — polls /workspace/envelopes/{task-uid}/release every
//	                      500ms (Phase 3 D-D3); on signal-file appearance writes
//	                      canned success out.json; on ctx cancel returns 0.
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

	"github.com/jsquirrelz/tide/internal/harness"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"k8s.io/apimachinery/pkg/runtime"
)

// workspaceRoot is the PVC mount point under which envelope/signal files
// live. Production stub pods mount this at the canonical "/workspace" path
// (Phase 2 D-G2 layout); tests override it via withWorkspaceRoot in
// main_test.go to redirect signal polling into a tempdir.
var workspaceRoot = "/workspace"

// leakEscapePath is the deliberate out-of-bounds write path used by the
// exceed-output-paths test mode (HARN-05). Shared with main_test.go.
const leakEscapePath = "/workspace/escape/leak.txt"

// wait-for-signal polling cadence (D-D3) is locked at 500ms per CONTEXT.md /
// RESEARCH §"Open Questions Q4 RESOLVED". The literal is inlined at
// time.NewTicker call sites so the plan-acceptance grep
// (`time\.NewTicker\(500\s*\*\s*time\.Millisecond\)`) catches drift.

func main() {
	fs := flag.NewFlagSet("stub-subagent", flag.ExitOnError)
	envelopePath := fs.String("envelope", "",
		"path to EnvelopeIn JSON (default: $TIDE_ENVELOPE_PATH or /workspace/envelopes/$TIDE_TASK_UID/in.json)")
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
//
//nolint:unparam // stdout is part of the shared subagent-binary (stdout, stderr) entry-point contract
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

	// Executor git contract (mirrors cmd/claude-subagent/main.go): the real
	// subagent calls harness.EnsureWorktree before running the model, so a
	// Succeeded executor Task always leaves a tide/wt-<uid> branch behind.
	// Phase 34's wave-integration gate verifies exactly that — every
	// Succeeded task's branch must merge into the run branch — so a stub
	// that skips worktree provisioning can never satisfy a git-configured
	// Layer B flow (medium_http spec 3, integration_miss). Non-git fixtures
	// set no spec.git → BranchName never stamped → envelope Branch empty →
	// this block is skipped entirely (prior behavior preserved).
	if env.Role == "executor" && env.Branch != "" {
		if code := ensureExecutorWorktree(ctx, env, outPath, stderr); code != 0 {
			return code
		}
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

	case "wait-for-signal":
		return dispatchWaitForSignal(ctx, env, outPath, stderr)

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

// repoWaitTimeout bounds the executor's wait for the clone Job to land
// /workspace/repo.git on the shared PVC. Stub tasks can be dispatched within
// the same second the Project is created (no LLM latency), racing the clone
// Job — the real flow's planner latency masks this window.
//
// A BOUNDED wait with a NON-GIT FALLBACK, because envelope Branch presence
// does not imply a git-configured Project: reconcilePhase3Lifecycle Step 1
// stamps Status.Git.BranchName unconditionally (no Spec.Git guard), so
// gitless fixtures (chaos-resume, bare-project) dispatch later-wave tasks
// with Branch set and NO clone ever coming — failing hard there (the real
// claude-subagent's behavior) would fail every such task after the wait.
// 30s covers a kind-loaded image + tiny fixture clone with margin, while
// staying well inside chaos-resume's 4-minute pre-kill/pillar budgets.
const repoWaitTimeout = 30 * time.Second

// ensureExecutorWorktree engages the real executor's git contract
// (harness.EnsureWorktree — the identical call cmd/claude-subagent makes
// before running the model) when the workspace repo is present, waiting up
// to repoWaitTimeout for the clone Job to land it. Repo never appears →
// non-git fallback (prior stub behavior; gitless fixtures). Returns 0 to
// continue; on a real EnsureWorktree failure (repo present but worktree
// provisioning broke) writes the same "worktree-setup-failed" envelope
// shape as cmd/claude-subagent and returns 1.
func ensureExecutorWorktree(ctx context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	repoPath := filepath.Join(workspaceRoot, "repo.git")
	deadline := time.Now().Add(repoWaitTimeout)
	for {
		if _, err := os.Stat(repoPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(stderr,
				"stub-subagent: workspace repo %s not present after %s; proceeding without git\n",
				repoPath, repoWaitTimeout)
			return 0
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(2 * time.Second):
		}
	}
	if err := harness.EnsureWorktree(env, workspaceRoot, env.Branch); err != nil {
		fmt.Fprintf(stderr, "stub-subagent: %v\n", err)
		_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
			APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeOut,
			TaskUID: env.TaskUID, ExitCode: 1, Result: "worktree-setup-failed", Reason: err.Error(),
			CompletedAt: time.Now().UTC(),
		})
		return 1
	}
	return 0
}

// commitExecutorWorktree writes a distinct stub artifact into the Task's
// worktree and commits it — the real executor's harness.CommitWorktree
// effect — returning the GitOutput to stamp on the success envelope.
// Returns (nil, nil) when no worktree was provisioned (the
// ensureExecutorWorktree non-git fallback): the success envelope then
// carries no Git output, exactly like the pre-git stub. The stub file
// guarantees a non-empty diff so the commit always lands (the real
// subagent treats an empty diff as a Task failure; the stub must not
// manufacture that failure class).
func commitExecutorWorktree(env pkgdispatch.EnvelopeIn, stderr io.Writer) (*pkgdispatch.GitOutput, error) {
	worktreeDir := filepath.Join(workspaceRoot, "worktrees", env.TaskUID)
	if _, err := os.Stat(filepath.Join(worktreeDir, ".git")); err != nil {
		// Non-git fallback path: no worktree was provisioned.
		return nil, nil
	}
	stubFile := filepath.Join(worktreeDir, "stub-task-"+env.TaskUID+".txt")
	if err := os.WriteFile(stubFile, []byte("stub success\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write stub artifact into worktree: %w", err)
	}
	hash, isEmpty, err := harness.CommitWorktree(worktreeDir, env.TaskUID)
	if err != nil {
		return nil, fmt.Errorf("commit worktree: %w", err)
	}
	if isEmpty {
		return nil, fmt.Errorf("commit worktree: unexpectedly empty diff after writing %s", stubFile)
	}
	fmt.Fprintf(stderr, "stub-subagent: committed worktree %s @ %s\n", worktreeDir, hash.String())
	return &pkgdispatch.GitOutput{HeadSHA: hash.String()}, nil
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

// writeEnvelope marshals out and writes it to path (the PVC out.json — audit
// artifact + reporter Job input), creating parent dirs as needed. The
// termination message carries only the tiny TerminationStub (<4KB), not the
// full envelope. Errors are ignored by callers using the "_" discard pattern.
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
	// Write only the tiny TerminationStub to the termination message path.
	// The full envelope (with ChildCRDs) stays on the PVC; the stub is the
	// cross-namespace tiny-status carrier read by the Manager (#11 fix).
	writeTerminationMessage(pkgdispatch.NewTerminationStub(out))
	return nil
}

func writeTerminationMessage(stub pkgdispatch.TerminationStub) {
	path := os.Getenv("TIDE_TERMINATION_MESSAGE_PATH")
	if path == "" {
		path = "/dev/termination-log"
	}
	data, _ := json.Marshal(stub)
	_ = os.WriteFile(path, data, 0o644)
}

// dispatchPlannerSuccess handles planner-mode dispatch (env.Role == "planner").
// It switches on env.Level to emit exactly one typed ChildCRDSpec per level:
//
//	project   → Milestone "stub-milestone-1" with Spec {"projectRef": parentName}
//	milestone → Phase     "stub-phase-1"     with Spec {"milestoneRef": parentName}
//	phase     → Plan      "stub-plan-1"      with Spec {"phaseRef": parentName}
//	plan      → Task      "stub-task-1"      Spec {planRef, filesTouched, declaredOutputPaths, dev.testMode}
//	task (leaf/unknown)   → empty ChildCRDs, exit 0
//
// parentName is read from env.Dispatch.ParentName; falls back to
// "stub-parent" if absent (REQ-3 / 07-03-PLAN.md task 1).
//
// Wave CRDs are intentionally NOT emitted — waves are derived by PlanReconciler
// (CLAUDE.md constraint: "Waves are derived, not declared").
func dispatchPlannerSuccess(_ context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	parentName := "stub-parent"
	if env.Dispatch != nil && env.Dispatch.ParentName != "" {
		parentName = env.Dispatch.ParentName
	}

	var children []pkgdispatch.ChildCRDSpec

	switch env.Level {
	case "project":
		raw, err := json.Marshal(map[string]string{"projectRef": parentName})
		if err != nil {
			fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: marshal Milestone spec: %v\n", err)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion:  pkgdispatch.APIVersionV1Alpha1,
				Kind:        pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:     env.TaskUID,
				ExitCode:    2,
				Result:      "internal-error",
				Reason:      err.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 2
		}
		children = append(children, pkgdispatch.ChildCRDSpec{
			Kind: "Milestone",
			Name: "stub-milestone-1",
			Spec: runtime.RawExtension{Raw: raw},
		})

	case "milestone":
		raw, err := json.Marshal(map[string]string{"milestoneRef": parentName})
		if err != nil {
			fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: marshal Phase spec: %v\n", err)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion:  pkgdispatch.APIVersionV1Alpha1,
				Kind:        pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:     env.TaskUID,
				ExitCode:    2,
				Result:      "internal-error",
				Reason:      err.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 2
		}
		children = append(children, pkgdispatch.ChildCRDSpec{
			Kind: "Phase",
			Name: "stub-phase-1",
			Spec: runtime.RawExtension{Raw: raw},
		})

	case "phase":
		raw, err := json.Marshal(map[string]string{"phaseRef": parentName})
		if err != nil {
			fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: marshal Plan spec: %v\n", err)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion:  pkgdispatch.APIVersionV1Alpha1,
				Kind:        pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:     env.TaskUID,
				ExitCode:    2,
				Result:      "internal-error",
				Reason:      err.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 2
		}
		children = append(children, pkgdispatch.ChildCRDSpec{
			Kind: "Plan",
			Name: "stub-plan-1",
			Spec: runtime.RawExtension{Raw: raw},
		})

	case "plan":
		type devSpec struct {
			TestMode string `json:"testMode"`
		}
		type taskSpec struct {
			PlanRef             string   `json:"planRef"`
			FilesTouched        []string `json:"filesTouched"`
			DeclaredOutputPaths []string `json:"declaredOutputPaths"`
			DependsOn           []string `json:"dependsOn"`
			Dev                 devSpec  `json:"dev"`
		}
		raw, err := json.Marshal(taskSpec{
			PlanRef:             parentName,
			FilesTouched:        []string{"stub-output.txt"},
			DeclaredOutputPaths: []string{"/workspace/artifacts/stub"},
			DependsOn:           []string{},
			Dev:                 devSpec{TestMode: "success"},
		})
		if err != nil {
			fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: marshal Task spec: %v\n", err)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion:  pkgdispatch.APIVersionV1Alpha1,
				Kind:        pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:     env.TaskUID,
				ExitCode:    2,
				Result:      "internal-error",
				Reason:      err.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 2
		}
		// Mirror the real runner's children/<name>.json convention (anthropic/subagent.go:427):
		// write the Task spec as a prompt artifact file so the executor can read it at dispatch
		// time, and stamp SourcePath so the materializer can satisfy Task.Spec.PromptPath
		// (MinLength=1 — defect #10b / T-09-04 mitigation).
		const taskChildName = "stub-task-1.json"
		const taskSourcePath = "children/" + taskChildName
		childrenDir := filepath.Join(workspaceRoot, "children")
		if mkErr := os.MkdirAll(childrenDir, 0o755); mkErr != nil {
			fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: mkdir children: %v\n", mkErr)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion:  pkgdispatch.APIVersionV1Alpha1,
				Kind:        pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:     env.TaskUID,
				ExitCode:    2,
				Result:      "internal-error",
				Reason:      mkErr.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 2
		}
		if wErr := os.WriteFile(filepath.Join(childrenDir, taskChildName), raw, 0o644); wErr != nil {
			fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: write children/stub-task-1.json: %v\n", wErr)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion:  pkgdispatch.APIVersionV1Alpha1,
				Kind:        pkgdispatch.KindTaskEnvelopeOut,
				TaskUID:     env.TaskUID,
				ExitCode:    2,
				Result:      "internal-error",
				Reason:      wErr.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 2
		}
		children = append(children, pkgdispatch.ChildCRDSpec{
			Kind:       "Task",
			Name:       "stub-task-1",
			SourcePath: taskSourcePath,
			Spec:       runtime.RawExtension{Raw: raw},
		})

	default:
		// "task" level (leaf) or any unknown level: no children.
		children = []pkgdispatch.ChildCRDSpec{}
	}

	out := pkgdispatch.EnvelopeOut{
		APIVersion:  pkgdispatch.APIVersionV1Alpha1,
		Kind:        pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:     env.TaskUID,
		ExitCode:    0,
		Result:      "success",
		Reason:      "planner stub success",
		ChildCRDs:   children,
		CompletedAt: time.Now().UTC(),
	}
	if err := writeEnvelope(outPath, out); err != nil {
		fmt.Fprintf(stderr, "stub-subagent: dispatchPlannerSuccess: write out.json: %v\n", err)
		return 2
	}
	return 0
}

// dispatchSuccess handles testMode == "success" (or empty). It writes a canned
// result.txt under the first DeclaredOutputPath and then writes out.json.
func dispatchSuccess(ctx context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	// Branch on Role: planner-mode dispatch emits ChildCRDs, executor-mode
	// dispatch writes artifact files. This must be the first check so the
	// planner path is exercised even when Dev is nil (planner Jobs don't set
	// Dev.TestMode — they branch on Role, not testMode).
	if env.Role == "planner" {
		return dispatchPlannerSuccess(ctx, env, outPath, stderr)
	}

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

	// Executor git contract, success half (mirrors cmd/claude-subagent's
	// post-Run CommitWorktree): commit the worktree provisioned by
	// ensureExecutorWorktree and stamp HeadSHA. Git-configured flows only —
	// Branch empty means run() skipped worktree setup and there is nothing
	// to commit.
	if env.Role == "executor" && env.Branch != "" {
		gitOut, gErr := commitExecutorWorktree(env, stderr)
		if gErr != nil {
			fmt.Fprintf(stderr, "stub-subagent: %v\n", gErr)
			_ = writeEnvelope(outPath, pkgdispatch.EnvelopeOut{
				APIVersion: pkgdispatch.APIVersionV1Alpha1, Kind: pkgdispatch.KindTaskEnvelopeOut,
				TaskUID: env.TaskUID, ExitCode: 1, Result: "commit-failed", Reason: gErr.Error(),
				CompletedAt: time.Now().UTC(),
			})
			return 1
		}
		out.Git = gitOut
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
	leakPath := leakEscapePath
	_ = os.MkdirAll(filepath.Dir(leakPath), 0o755)
	_ = os.WriteFile(leakPath, []byte("leaked"), 0o644)

	out := pkgdispatch.EnvelopeOut{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:    env.TaskUID,
		ExitCode:   1,
		Result:     "output-paths-violation",
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
	return 1
}

// dispatchWaitForSignal handles testMode == "wait-for-signal" (Phase 3 D-D3).
// Polls <workspaceRoot>/envelopes/{TaskUID}/release every signalPollInterval
// (500ms). On signal-file appearance, delegates to dispatchSuccess to write
// the canned success envelope at outPath and exits 0. On context cancellation
// (e.g., the orchestrator pod being killed mid-wave during chaos-resume),
// returns 0 without writing out.json — matches dispatchHang's ctx-cancel
// contract so the new leader observes the Task as still Running.
//
// Used by the chaos-resume Layer B integration test (plan 03-10) to pin
// Tasks at Running long enough for pod-kill + leader-handoff to observe,
// then release them post-restart by touching the signal file.
func dispatchWaitForSignal(ctx context.Context, env pkgdispatch.EnvelopeIn, outPath string, stderr io.Writer) int {
	signalPath := filepath.Join(workspaceRoot, "envelopes", env.TaskUID, "release")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			if _, err := os.Stat(signalPath); err == nil {
				// Signal arrived — emit canned success envelope.
				return dispatchSuccess(ctx, env, outPath, stderr)
			}
		}
	}
}
