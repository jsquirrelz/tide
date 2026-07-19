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

// Plan 25-01 Task 2 — RED tests for `tide resume --retry-failed` clearing FailureHalt.
// Tests: TestResumeRunClearsFailureHalt, TestResumeWithoutRetryFailedLeavesFailureHalt.
// These tests are RED until 25-03 Task 2 adds the FailureHalt clear block to resume.go.
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// TestResumeRunClearsFailureHalt asserts that resumeRun with retryFailed=true
// clears a ConditionFailureHalt=True condition on the Project, and that the
// output mentions "FailureHalt" (operator feedback).
//
// RED: this test fails until 25-03 adds the FailureHalt clear block inside the
// retryFailed gate in cmd/tide/resume.go.
func TestResumeRunClearsFailureHalt(t *testing.T) {
	p := makeProject("my-project")
	// Stamp FailureHalt=True on the project (simulates a conservative-profile halt).
	p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionFailureHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonTaskFailedHalt,
		LastTransitionTime: metav1.Now(),
	})
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(&tidev1alpha3.Project{}, &tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	fhCond := meta.FindStatusCondition(got.Status.Conditions, tidev1alpha3.ConditionFailureHalt)
	if fhCond != nil && fhCond.Status == metav1.ConditionTrue {
		t.Errorf("expected ConditionFailureHalt cleared by retry-failed; still True")
	}
	if !strings.Contains(buf.String(), "FailureHalt") {
		t.Errorf("expected output to mention FailureHalt; got %q", buf.String())
	}
}

// TestResumeWithoutRetryFailedLeavesFailureHalt asserts that bare resume (no
// --retry-failed) does NOT clear ConditionFailureHalt — only --retry-failed clears it.
// FailureHalt is execution-failure-specific; it must be cleared together with
// the --retry-failed Task phase resets, not on bare resume.
//
// RED: this test fails until 25-03 adds the FailureHalt clear block only inside
// the retryFailed gate (not the unconditional BillingHalt clear path).
func TestResumeWithoutRetryFailedLeavesFailureHalt(t *testing.T) {
	p := makeProject("my-project-noflag")
	p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionFailureHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonTaskFailedHalt,
		LastTransitionTime: metav1.Now(),
	})
	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(&tidev1alpha3.Project{}, &tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	// retryFailed=false — bare resume must not clear FailureHalt.
	if err := resumeRun(context.Background(), c, "default", "my-project-noflag", false, nil); err != nil {
		t.Fatalf("resumeRun(retryFailed=false): %v", err)
	}

	var got tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project-noflag"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	fhCond := meta.FindStatusCondition(got.Status.Conditions, tidev1alpha3.ConditionFailureHalt)
	if fhCond == nil || fhCond.Status != metav1.ConditionTrue {
		t.Errorf("expected ConditionFailureHalt=True still present after bare resume (no --retry-failed); got %v", fhCond)
	}
}

// TestResumeRetryFailedRecoversConservativeHalt is the WR-03 regression test for
// CR-01/CR-02. It proves the full conservative-halt recovery path: a project with
// ConditionFailureHalt=True and a Failed Task is recovered by
// `tide resume --retry-failed` such that (a) the halt is cleared, (b) the
// previously-frozen Failed task is reset to "" for re-dispatch, and (c) the
// AnnotationFailureResumedAt fence is stamped so a straggler reconcile cannot
// re-freeze the project.
//
// This test would be RED on the pre-CR-01 code: that path cleared the halt
// BEFORE resetting the Failed task and never stamped AnnotationFailureResumedAt,
// so assertion (c) fails — the exact gap that let a reconcile re-stamp the halt
// after a "successful" resume.
func TestResumeRetryFailedRecoversConservativeHalt(t *testing.T) {
	p := makeProject("conservative-proj")
	p.Spec.FailureProfile = tidev1alpha3.FailureProfileConservative
	p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionFailureHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonTaskFailedHalt,
		LastTransitionTime: metav1.Now(),
	})

	// A previously-frozen task: ready (no deps) but stamped Failed under the halt.
	failed := makeTask("frozen-task", "conservative-proj", "0", "Failed", 1, 1)
	failed.Spec.PlanRef = "conservative-plan"

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, failed).
		WithStatusSubresource(&tidev1alpha3.Project{}, &tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "conservative-proj", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	// (a) FailureHalt cleared.
	var gotProj tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "conservative-proj"}, &gotProj); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if cond := meta.FindStatusCondition(gotProj.Status.Conditions, tidev1alpha3.ConditionFailureHalt); cond != nil && cond.Status == metav1.ConditionTrue {
		t.Errorf("expected ConditionFailureHalt cleared; still True")
	}

	// (b) The previously-frozen task is reset for re-dispatch.
	var gotTask tidev1alpha3.Task
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "frozen-task"}, &gotTask); err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status.Phase != "" {
		t.Errorf("expected frozen task phase reset to \"\"; got %q", gotTask.Status.Phase)
	}
	if cond := meta.FindStatusCondition(gotTask.Status.Conditions, tidev1alpha3.ConditionWaveOrLevelPaused); cond == nil || cond.Reason != tidev1alpha3.ReasonResumedByUser {
		t.Errorf("expected ResumedByUser condition on reset task; got %v", cond)
	}

	// (c) Resume fence stamped so setFailureHaltIfNeeded refuses to re-freeze on a
	//     pre-resume straggler reconcile. RED on pre-CR-01 code (never stamped).
	if _, ok := gotProj.Annotations[tidev1alpha3.AnnotationFailureResumedAt]; !ok {
		t.Errorf("expected AnnotationFailureResumedAt stamped on FailureHalt clear; annotations=%v", gotProj.Annotations)
	}
}

// TestResumeRecoversVerifyHalt (Phase 51 ESC-03/HI-01) proves plain `tide resume`
// — NOT --retry-failed — is the recovery path for a VerifyHalt: it (a) clears the
// project-wide ConditionVerifyHalt, (b) resets any VerifyHalted Task to "" for
// re-dispatch, and (c) stamps the AnnotationVerifyResumedAt fence so a pre-resume
// straggler verifier completion cannot re-freeze the project. Before HI-01, no CLI
// verb cleared ConditionVerifyHalt at all — a VerifyHalted project was stranded.
func TestResumeRecoversVerifyHalt(t *testing.T) {
	p := makeProject("verify-halt-proj")
	p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionVerifyHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonVerifyExhausted,
		LastTransitionTime: metav1.Now(),
	})

	// A VerifyHalted task: exhausted its verification loop under the halt.
	halted := makeTask("halted-task", "verify-halt-proj", "0", tidev1alpha3.LevelPhaseVerifyHalted, 3, 1)
	halted.Spec.PlanRef = "verify-plan"

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, halted).
		WithStatusSubresource(&tidev1alpha3.Project{}, &tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	var buf bytes.Buffer
	// retryFailed=false — plain resume owns VerifyHalt recovery (distinct class).
	if err := resumeRun(context.Background(), c, "default", "verify-halt-proj", false, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=false): %v", err)
	}

	// (a) VerifyHalt cleared.
	var gotProj tidev1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "verify-halt-proj"}, &gotProj); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if cond := meta.FindStatusCondition(gotProj.Status.Conditions, tidev1alpha3.ConditionVerifyHalt); cond != nil && cond.Status == metav1.ConditionTrue {
		t.Errorf("expected ConditionVerifyHalt cleared by plain resume; still True")
	}

	// (b) The VerifyHalted task is reset for re-dispatch.
	var gotTask tidev1alpha3.Task
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "halted-task"}, &gotTask); err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status.Phase != "" {
		t.Errorf("expected VerifyHalted task phase reset to \"\"; got %q", gotTask.Status.Phase)
	}

	// (c) Resume fence stamped.
	if _, ok := gotProj.Annotations[tidev1alpha3.AnnotationVerifyResumedAt]; !ok {
		t.Errorf("expected AnnotationVerifyResumedAt stamped on VerifyHalt clear; annotations=%v", gotProj.Annotations)
	}

	if !strings.Contains(buf.String(), "VerifyHalt") {
		t.Errorf("expected output to mention VerifyHalt; got %q", buf.String())
	}
}

// TestResumeLeavesFailedTaskUnderVerifyHaltRecovery proves the VerifyHalt recovery
// path is scoped to VerifyHalted tasks only — a plain resume must NOT reset a Failed
// task (that stays the --retry-failed job), preserving the distinct-halt-class contract.
func TestResumeLeavesFailedTaskUnderVerifyHaltRecovery(t *testing.T) {
	p := makeProject("mixed-halt-proj")
	p.Status.Conditions = append(p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionVerifyHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonVerifyExhausted,
		LastTransitionTime: metav1.Now(),
	})
	failed := makeTask("plain-failed-task", "mixed-halt-proj", "0", tidev1alpha3.LevelPhaseFailed, 1, 1)
	failed.Spec.PlanRef = "mixed-plan"

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, failed).
		WithStatusSubresource(&tidev1alpha3.Project{}, &tidev1alpha3.Milestone{}, &tidev1alpha3.Phase{}, &tidev1alpha3.Plan{}, &tidev1alpha3.Task{}).
		Build()

	if err := resumeRun(context.Background(), c, "default", "mixed-halt-proj", false, nil); err != nil {
		t.Fatalf("resumeRun(retryFailed=false): %v", err)
	}

	var gotTask tidev1alpha3.Task
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "plain-failed-task"}, &gotTask); err != nil {
		t.Fatalf("get task: %v", err)
	}
	if gotTask.Status.Phase != tidev1alpha3.LevelPhaseFailed {
		t.Errorf("expected Failed task untouched by plain resume (needs --retry-failed); got %q", gotTask.Status.Phase)
	}
}
