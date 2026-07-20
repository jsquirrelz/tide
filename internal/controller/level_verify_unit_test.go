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

// level_verify_unit_test.go — Phase 52 Plan 08 Task 2: pure-function unit
// tests for level_verify.go's dispatch-decision guard and envelope-shape
// construction. Plain testing.T functions, mirroring span_emission_unit_test.go's
// precedent (51-03's own decision log): internal/controller's sole Ginkgo
// entry point is TestControllers, so a Ginkgo Describe/It here would
// vacuously pass 0 specs under `go test -run TestLevelVerify`. These tests
// have no client.Client/context dependency, so the plain testing.T shape
// genuinely executes.
package controller

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// lockedVerificationSpec is a minimal active-and-Locked fixture spec shared
// by the decision-table subtests below.
func lockedVerificationSpec() tideprojectv1alpha3.VerificationSpec {
	return tideprojectv1alpha3.VerificationSpec{
		Phase:       verificationPhaseLocked,
		GateCommand: "make test-phase-gate",
		Commands:    []string{"make lint"},
	}
}

// TestLevelVerifyDecision covers the four mutually-exclusive branches
// levelVerifyDecision's pure guard evaluates (inactive / converged /
// engaging-needs-dispatch / engaging-already-verifying), matching the
// plan's required "inactive / converged / engaging" coverage.
func TestLevelVerifyDecision(t *testing.T) {
	t.Run("inactive_no_gate_command", func(t *testing.T) {
		spec := tideprojectv1alpha3.VerificationSpec{Phase: verificationPhaseLocked}
		got := levelVerifyDecision(spec, nil, "")
		if got != levelVerifyInactive {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyInactive", got)
		}
	})

	t.Run("inactive_not_locked", func(t *testing.T) {
		spec := tideprojectv1alpha3.VerificationSpec{Phase: "Draft", GateCommand: "make test"}
		got := levelVerifyDecision(spec, nil, "")
		if got != levelVerifyInactive {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyInactive (Draft contract must never activate)", got)
		}
	})

	t.Run("converged", func(t *testing.T) {
		spec := lockedVerificationSpec()
		ls := &tideprojectv1alpha3.LoopStatus{ExitReason: tideprojectv1alpha3.ExitApproved}
		got := levelVerifyDecision(spec, ls, tideprojectv1alpha3.LevelPhaseVerifying)
		if got != levelVerifyConverged {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyConverged (post-approval convergence guard, T-52-25)", got)
		}
	})

	t.Run("converged_escalated_still_never_redispatches", func(t *testing.T) {
		spec := lockedVerificationSpec()
		ls := &tideprojectv1alpha3.LoopStatus{ExitReason: tideprojectv1alpha3.ExitEscalated}
		got := levelVerifyDecision(spec, ls, tideprojectv1alpha3.LevelPhaseAwaitingApproval)
		if got != levelVerifyConverged {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyConverged", got)
		}
	})

	t.Run("engaging_needs_dispatch", func(t *testing.T) {
		spec := lockedVerificationSpec()
		ls := &tideprojectv1alpha3.LoopStatus{} // ExitReason empty: loop not yet concluded
		got := levelVerifyDecision(spec, ls, "")
		if got != levelVerifyNeedsDispatch {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyNeedsDispatch", got)
		}
	})

	t.Run("engaging_already_verifying", func(t *testing.T) {
		spec := lockedVerificationSpec()
		ls := &tideprojectv1alpha3.LoopStatus{}
		got := levelVerifyDecision(spec, ls, tideprojectv1alpha3.LevelPhaseVerifying)
		if got != levelVerifyAlreadyVerifying {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyAlreadyVerifying", got)
		}
	})

	t.Run("engaging_nil_loop_status_is_not_converged", func(t *testing.T) {
		spec := lockedVerificationSpec()
		got := levelVerifyDecision(spec, nil, "")
		if got != levelVerifyNeedsDispatch {
			t.Fatalf("levelVerifyDecision() = %v, want levelVerifyNeedsDispatch (nil LoopStatus must not be treated as converged)", got)
		}
	})
}

// TestLevelVerifyEnvelopeShape unit-tests buildLevelVerifierEnvelopeIn's
// construction shape: Branch populated from the caller-supplied run branch,
// Level stamped from target.Level, and the resolved Commands union ordered
// gate command first (D-01) — matches the plan's required "envelope shape"
// coverage.
func TestLevelVerifyEnvelopeShape(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", UID: types.UID("11111111-1111-1111-1111-111111111111")},
	}
	target := levelVerifyTarget{
		Obj: &tideprojectv1alpha3.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: "phase-01", Namespace: "default", UID: types.UID("22222222-2222-2222-2222-222222222222")},
		},
		Level: "phase",
		Goal:  "Ship the thing.",
	}
	spec := tideprojectv1alpha3.VerificationSpec{
		Phase:             verificationPhaseLocked,
		GateCommand:       "make test-phase-gate",
		Commands:          []string{"make lint", "make vet"},
		RequiredArtifacts: []string{"docs/OUTCOME.md"},
		Evaluator:         "default",
	}
	deps := PlannerReconcilerDeps{}

	envIn, data, err := buildLevelVerifierEnvelopeIn(deps, project, target, spec, "signed-token", "tide/run-proj-123")
	if err != nil {
		t.Fatalf("buildLevelVerifierEnvelopeIn() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("buildLevelVerifierEnvelopeIn() returned empty marshalled JSON")
	}

	if envIn.Branch != "tide/run-proj-123" {
		t.Errorf("envIn.Branch = %q, want %q (D-07 point 4: the run branch — Task's own verifier never sets this)", envIn.Branch, "tide/run-proj-123")
	}
	if envIn.Level != "phase" {
		t.Errorf("envIn.Level = %q, want %q", envIn.Level, "phase")
	}
	if envIn.Role != "verifier" {
		t.Errorf("envIn.Role = %q, want %q", envIn.Role, "verifier")
	}
	if envIn.Provider.Vendor != "langgraph" {
		t.Errorf("envIn.Provider.Vendor = %q, want %q (the verifier is a logically independent process, TASK-04)", envIn.Provider.Vendor, "langgraph")
	}
	if envIn.Verify == nil {
		t.Fatal("envIn.Verify is nil")
	}
	wantCommands := []string{"make test-phase-gate", "make lint", "make vet"}
	if len(envIn.Verify.Commands) != len(wantCommands) {
		t.Fatalf("envIn.Verify.Commands = %v, want %v", envIn.Verify.Commands, wantCommands)
	}
	for i, want := range wantCommands {
		if envIn.Verify.Commands[i] != want {
			t.Errorf("envIn.Verify.Commands[%d] = %q, want %q (D-01: gate command first, guaranteed executed)", i, envIn.Verify.Commands[i], want)
		}
	}
	if envIn.Verify.GateCommand != spec.GateCommand {
		t.Errorf("envIn.Verify.GateCommand = %q, want %q", envIn.Verify.GateCommand, spec.GateCommand)
	}
	if envIn.Prompt == "" {
		t.Error("envIn.Prompt is empty — the level verifier template did not render")
	}
}

// TestLevelVerifyEnvelopeShape_LevelGoalRendersIntoPrompt proves target.Goal
// reaches the rendered prompt via levelVerifierRenderData.LevelGoal
// (phase_verifier.tmpl's {{.LevelGoal}} placeholder, D-09).
func TestLevelVerifyEnvelopeShape_LevelGoalRendersIntoPrompt(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", UID: types.UID("11111111-1111-1111-1111-111111111111")},
	}
	const goal = "Ship v1.0.9 Slack Tide's per-level LoopPolicy parameterization."
	target := levelVerifyTarget{
		Obj: &tideprojectv1alpha3.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: "milestone-01", Namespace: "default", UID: types.UID("33333333-3333-3333-3333-333333333333")},
		},
		Level: "milestone",
		Goal:  goal,
	}
	spec := tideprojectv1alpha3.VerificationSpec{
		Phase:       verificationPhaseLocked,
		GateCommand: "make test-milestone-gate",
	}
	deps := PlannerReconcilerDeps{}

	envIn, _, err := buildLevelVerifierEnvelopeIn(deps, project, target, spec, "signed-token", "tide/run-proj-123")
	if err != nil {
		t.Fatalf("buildLevelVerifierEnvelopeIn() error = %v", err)
	}
	if !strings.Contains(envIn.Prompt, goal) {
		t.Errorf("rendered prompt does not contain LevelGoal %q", goal)
	}
}

// TestLevelVerifyNoEnvelopeOut proves synthesizeNoLevelVerifyEnvelopeOut
// preserves LoopRunID/AttemptID identity (fixed attempt=1) through a
// degraded/unreadable envelope, mirroring synthesizeNoEnvelopeOut's Task-level
// contract.
func TestLevelVerifyNoEnvelopeOut(t *testing.T) {
	obj := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", UID: types.UID("44444444-4444-4444-4444-444444444444")},
	}
	out := synthesizeNoLevelVerifyEnvelopeOut(obj, nil)
	if out.TaskUID != string(obj.UID) {
		t.Errorf("out.TaskUID = %q, want %q", out.TaskUID, obj.UID)
	}
	if out.LoopRunID != string(obj.UID) {
		t.Errorf("out.LoopRunID = %q, want %q", out.LoopRunID, obj.UID)
	}
	wantAttemptID := string(obj.UID) + "-1"
	if out.AttemptID != wantAttemptID {
		t.Errorf("out.AttemptID = %q, want %q (attempt fixed at 1)", out.AttemptID, wantAttemptID)
	}
}

// TestLevelVerifyApplyLevelLoopStatus proves applyLevelLoopStatus fixes
// Iteration at 1 (D-07: exactly one attempt ever, never a repair-driven
// counter) and correctly counts high-severity findings.
func TestLevelVerifyApplyLevelLoopStatus(t *testing.T) {
	ls := &tideprojectv1alpha3.LoopStatus{}
	out := pkgdispatch.EnvelopeOut{
		LoopRunID: "run-1",
		Verdict: &pkgdispatch.GateDecision{
			Verdict: pkgdispatch.VerdictBlocked,
			Findings: []pkgdispatch.Finding{
				{Severity: gateCommandFindingSeverity, Dimension: gateCommandFindingDimension},
				{Severity: "advisory"},
			},
		},
	}
	applyLevelLoopStatus(ls, out, tideprojectv1alpha3.ExitEscalated)

	if ls.Iteration != 1 {
		t.Errorf("ls.Iteration = %d, want 1", ls.Iteration)
	}
	if ls.ExitReason != tideprojectv1alpha3.ExitEscalated {
		t.Errorf("ls.ExitReason = %q, want %q", ls.ExitReason, tideprojectv1alpha3.ExitEscalated)
	}
	if ls.LastEvaluation == nil {
		t.Fatal("ls.LastEvaluation is nil")
	}
	if ls.LastEvaluation.FindingsCount != 2 {
		t.Errorf("ls.LastEvaluation.FindingsCount = %d, want 2", ls.LastEvaluation.FindingsCount)
	}
	if ls.LastEvaluation.HighSeverityCount != 1 {
		t.Errorf("ls.LastEvaluation.HighSeverityCount = %d, want 1", ls.LastEvaluation.HighSeverityCount)
	}

	// nil-safety: must not panic.
	applyLevelLoopStatus(nil, out, tideprojectv1alpha3.ExitEscalated)
}
