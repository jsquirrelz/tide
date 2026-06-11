/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 1 — RED tests for `tide resume`. Asserts the verb clears
// the `tideproject.k8s/reject` annotation via gates.ConsumeReject + Patch.
// Plan 12-02 Task 1 extends the suite: `--retry-failed` resets Status.Phase
// on Failed levels via the status subresource (all four level kinds), skips
// Running levels (Pitfall 3), and is a no-op when the flag is absent.
package main

import (
	"bytes"
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

func makeRejectedProject(name, reason string) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Annotations: map[string]string{"tideproject.k8s/reject": reason},
		},
		Spec:   tidev1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
		Status: tidev1alpha1.ProjectStatus{Phase: "Running"},
	}
}

func TestResumeClearsRejectAnnotation(t *testing.T) {
	p := makeRejectedProject("my-project", "stopped")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}
	var got tidev1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if v, ok := got.Annotations["tideproject.k8s/reject"]; ok {
		t.Errorf("expected reject annotation cleared; still present with value %q (annotations=%v)", v, got.Annotations)
	}
}

func TestResumePreservesOtherAnnotations(t *testing.T) {
	p := makeRejectedProject("my-project", "stopped")
	p.Annotations["other/key"] = "preserve-me"
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}
	var got tidev1alpha1.Project
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got)
	if got.Annotations["other/key"] != "preserve-me" {
		t.Errorf("expected other annotations preserved; got %v", got.Annotations)
	}
}

func TestResumeProjectNotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if err := resumeRun(context.Background(), c, "default", "missing", false, nil); err == nil {
		t.Fatal("expected not-found error; got nil")
	}
}

func TestResumeNoOpWhenNoReject(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()
	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun on un-rejected project should be no-op; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Plan 12-02 additions: --retry-failed behaviour
// ---------------------------------------------------------------------------

// makeFailedPlan builds a Plan fixture with Status.Phase="Failed" and the
// project label so the retry-failed walker can find it.
func makeFailedPlan(name, projectName string) *tidev1alpha1.Plan {
	return &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tidev1alpha1.PlanSpec{PhaseRef: "some-phase"},
		Status: tidev1alpha1.PlanStatus{Phase: "Failed"},
	}
}

// makeRunningPlan builds a Plan fixture with Status.Phase="Running".
func makeRunningPlan(name, projectName string) *tidev1alpha1.Plan {
	return &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tidev1alpha1.PlanSpec{PhaseRef: "some-phase"},
		Status: tidev1alpha1.PlanStatus{Phase: "Running"},
	}
}

// TestResumeRunRetryFailed asserts that resumeRun with retryFailed=true resets
// Status.Phase on a Failed Plan to "" and stamps a ResumedByUser condition.
func TestResumeRunRetryFailed(t *testing.T) {
	p := makeProject("my-project")
	pl := makeFailedPlan("pl-one", "my-project")

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl).
		WithStatusSubresource(&tidev1alpha1.Milestone{}, &tidev1alpha1.Phase{}, &tidev1alpha1.Plan{}, &tidev1alpha1.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var got tidev1alpha1.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-one"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.Phase == "Failed" {
		t.Errorf("expected Status.Phase cleared; still 'Failed'")
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tidev1alpha1.ConditionWaveOrLevelPaused)
	if cond == nil {
		t.Fatal("expected WaveOrLevelPaused condition stamped on reset Plan; got nil")
	}
	if cond.Reason != tidev1alpha1.ReasonResumedByUser {
		t.Errorf("expected Reason=ResumedByUser; got %q", cond.Reason)
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected condition Status=False; got %q", cond.Status)
	}

	// Output should mention the plan name.
	if out := buf.String(); out == "" {
		t.Errorf("expected per-level feedback written to out; got empty string")
	}
}

// TestResumeRetryFailedSkipsRunning asserts that a Running Plan is untouched
// when retryFailed=true (Pitfall 3 — no double-dispatch).
func TestResumeRetryFailedSkipsRunning(t *testing.T) {
	p := makeProject("my-project")
	pl := makeRunningPlan("pl-running", "my-project")

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl).
		WithStatusSubresource(&tidev1alpha1.Milestone{}, &tidev1alpha1.Phase{}, &tidev1alpha1.Plan{}, &tidev1alpha1.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var got tidev1alpha1.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-running"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.Phase != "Running" {
		t.Errorf("expected Running Plan untouched; Status.Phase=%q", got.Status.Phase)
	}

	// Nothing reset — should print the "no Failed levels found" message.
	if out := buf.String(); out == "" {
		t.Errorf("expected 'no Failed levels found' feedback; got empty string")
	}
}

// TestResumeWithoutFlagLeavesFailed asserts that retryFailed=false does NOT
// touch a Failed Plan — flag is deliberate friction (D-06).
func TestResumeWithoutFlagLeavesFailed(t *testing.T) {
	p := makeProject("my-project")
	pl := makeFailedPlan("pl-fail", "my-project")

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, pl).
		WithStatusSubresource(&tidev1alpha1.Milestone{}, &tidev1alpha1.Phase{}, &tidev1alpha1.Plan{}, &tidev1alpha1.Task{}).
		Build()

	if err := resumeRun(context.Background(), c, "default", "my-project", false, nil); err != nil {
		t.Fatalf("resumeRun(retryFailed=false): %v", err)
	}

	var got tidev1alpha1.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-fail"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.Status.Phase != "Failed" {
		t.Errorf("expected Failed Plan untouched when flag absent; Status.Phase=%q", got.Status.Phase)
	}
}

// TestResumeRetryFailedAllFourKinds asserts that a single retryFailed=true
// call resets one Failed object of each of the four level kinds — Milestone,
// Phase, Plan, Task — leaving no item still "Failed".
func TestResumeRetryFailedAllFourKinds(t *testing.T) {
	p := makeProject("my-project")

	ms := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha1.MilestoneSpec{ProjectRef: "my-project"},
		Status: tidev1alpha1.MilestoneStatus{Phase: "Failed"},
	}
	ph := &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ph-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha1.PhaseSpec{MilestoneRef: "ms-failed"},
		Status: tidev1alpha1.PhaseStatus{Phase: "Failed"},
	}
	pl := makeFailedPlan("pl-failed", "my-project")
	tk := &tidev1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tk-failed",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec:   tidev1alpha1.TaskSpec{PlanRef: "pl-failed"},
		Status: tidev1alpha1.TaskStatus{Phase: "Failed"},
	}

	c := fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(p, ms, ph, pl, tk).
		WithStatusSubresource(&tidev1alpha1.Milestone{}, &tidev1alpha1.Phase{}, &tidev1alpha1.Plan{}, &tidev1alpha1.Task{}).
		Build()

	var buf bytes.Buffer
	if err := resumeRun(context.Background(), c, "default", "my-project", true, &buf); err != nil {
		t.Fatalf("resumeRun(retryFailed=true): %v", err)
	}

	var gotMS tidev1alpha1.Milestone
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ms-failed"}, &gotMS)
	if gotMS.Status.Phase == "Failed" {
		t.Errorf("Milestone still Failed after retry-failed")
	}

	var gotPH tidev1alpha1.Phase
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ph-failed"}, &gotPH)
	if gotPH.Status.Phase == "Failed" {
		t.Errorf("Phase still Failed after retry-failed")
	}

	var gotPL tidev1alpha1.Plan
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "pl-failed"}, &gotPL)
	if gotPL.Status.Phase == "Failed" {
		t.Errorf("Plan still Failed after retry-failed")
	}

	var gotTK tidev1alpha1.Task
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "tk-failed"}, &gotTK)
	if gotTK.Status.Phase == "Failed" {
		t.Errorf("Task still Failed after retry-failed")
	}
}

// ---------------------------------------------------------------------------
// Plan 13-02 Task 3 — tide resume clears BillingHalt (D-06)
// ---------------------------------------------------------------------------

// makeBillingHaltedProject builds a Project fixture with BillingHalt=True.
func makeBillingHaltedProject(name string) *tidev1alpha1.Project {
	p := &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       tidev1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
		Status:     tidev1alpha1.ProjectStatus{Phase: "Running"},
	}
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha1.ConditionBillingHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha1.ReasonCreditBalanceTooLow,
		LastTransitionTime: metav1.Now(),
	})
	return p
}

// TestResumeClearsBillingHalt asserts that resumeRun clears the BillingHalt
// condition from a Project that has been halted for credit exhaustion.
func TestResumeClearsBillingHalt(t *testing.T) {
	p := makeBillingHaltedProject("halt-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	if err := resumeRun(context.Background(), c, "default", "halt-project", false, nil); err != nil {
		t.Fatalf("resumeRun: %v", err)
	}

	var got tidev1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "halt-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	for _, cond := range got.Status.Conditions {
		if cond.Type == tidev1alpha1.ConditionBillingHalt && cond.Status == metav1.ConditionTrue {
			t.Errorf("BillingHalt=True condition should be cleared after resume; still present: %+v", cond)
		}
	}
}

// TestResumeWithoutBillingHalt_StillSucceeds asserts that resumeRun on a
// Project with no BillingHalt condition still succeeds (no-op for billing).
func TestResumeWithoutBillingHalt_StillSucceeds(t *testing.T) {
	p := makeProject("plain-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).
		WithObjects(p).
		WithStatusSubresource(p).
		Build()

	if err := resumeRun(context.Background(), c, "default", "plain-project", false, nil); err != nil {
		t.Fatalf("resumeRun on project without BillingHalt should succeed; got %v", err)
	}
}
