/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 1 — RED tests for `tide approve`. Asserts the verb writes
// the canonical annotation key set defined in internal/gates/annotation.go on
// either (a) the AwaitingApproval level discovered from Project Status
// Conditions, or (b) the Plan when --wave plan/N is provided.
// Plan 12-02 Task 2 adds D-07 guard tests: approve against a Failed level
// must error with an actionable message naming the level and pointing to
// tide resume --retry-failed.
package main

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// makeProject is a Project fixture with optional AwaitingApproval condition.
func makeProject(name string) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: tidev1alpha1.ProjectSpec{
			TargetRepo: "https://example.com/repo.git",
		},
		Status: tidev1alpha1.ProjectStatus{Phase: "Running"},
	}
}

func makeMilestoneAwaiting(name, projectName string) *tidev1alpha1.Milestone {
	return &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tidev1alpha1.MilestoneSpec{ProjectRef: projectName},
		Status: tidev1alpha1.MilestoneStatus{Phase: "AwaitingApproval"},
	}
}

func makePlan(name, projectName string) *tidev1alpha1.Plan {
	return &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tidev1alpha1.PlanSpec{PhaseRef: "some-phase"},
	}
}

// makeFailedMilestone builds a Milestone fixture with Status.Phase="Failed"
// and optionally one condition for the D-07 reason/message extraction test.
func makeFailedMilestone(name, projectName string, conditions []metav1.Condition) *tidev1alpha1.Milestone {
	return &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tidev1alpha1.MilestoneSpec{ProjectRef: projectName},
		Status: tidev1alpha1.MilestoneStatus{Phase: "Failed", Conditions: conditions},
	}
}

func TestApproveLevelDiscoversAwaitingMilestone(t *testing.T) {
	p := makeProject("my-project")
	ms := makeMilestoneAwaiting("ms-alpha", "my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	if err := approveRun(context.Background(), c, "default", "my-project", "", nil); err != nil {
		t.Fatalf("approveRun: %v", err)
	}

	// Re-fetch and verify the approve-milestone annotation lands on the Milestone.
	var got tidev1alpha1.Milestone
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ms-alpha"}, &got); err != nil {
		t.Fatalf("get milestone: %v", err)
	}
	if v := got.Annotations["tideproject.k8s/approve-milestone"]; v != "true" {
		t.Errorf("expected approve-milestone=true on Milestone; got %q (annotations=%v)", v, got.Annotations)
	}
}

func TestApproveWaveFormatRejection(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	for _, bad := range []string{"my-plan", "my-plan/", "my-plan/abc", "/3", ""} {
		if err := approveRun(context.Background(), c, "default", "my-project", bad, nil); err == nil {
			t.Errorf("expected error for invalid --wave %q; got nil", bad)
		}
	}
}

func TestApproveWaveWritesAnnotationOnPlan(t *testing.T) {
	p := makeProject("my-project")
	pl := makePlan("my-plan", "my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, pl).Build()

	if err := approveRun(context.Background(), c, "default", "my-project", "my-plan/3", nil); err != nil {
		t.Fatalf("approveRun: %v", err)
	}
	var got tidev1alpha1.Plan
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-plan"}, &got); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if v := got.Annotations["tideproject.k8s/approve-wave-3"]; v != "true" {
		t.Errorf("expected approve-wave-3=true on Plan; got %q (annotations=%v)", v, got.Annotations)
	}
}

func TestApproveProjectNotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	err := approveRun(context.Background(), c, "default", "missing", "", nil)
	if err == nil {
		t.Fatal("expected not-found error; got nil")
	}
}

func TestApproveNoAwaitingLevel(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()
	err := approveRun(context.Background(), c, "default", "my-project", "", nil)
	if err == nil {
		t.Fatal("expected error when no level is AwaitingApproval; got nil")
	}
}

func TestApproveUsesMergeFromPatch(t *testing.T) {
	// Patch via client.MergeFrom preserves other annotations; this test seeds
	// the Milestone with an unrelated annotation and verifies it is not
	// stripped on approve.
	p := makeProject("my-project")
	ms := makeMilestoneAwaiting("ms-alpha", "my-project")
	ms.Annotations = map[string]string{"unrelated.example.com/key": "preserve-me"}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	if err := approveRun(context.Background(), c, "default", "my-project", "", nil); err != nil {
		t.Fatalf("approveRun: %v", err)
	}
	var got tidev1alpha1.Milestone
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "ms-alpha"}, &got); err != nil {
		t.Fatalf("get milestone: %v", err)
	}
	if v := got.Annotations["unrelated.example.com/key"]; v != "preserve-me" {
		t.Errorf("MergeFrom should preserve unrelated annotation; got %q", v)
	}
	if v := got.Annotations["tideproject.k8s/approve-milestone"]; v != "true" {
		t.Errorf("approve annotation missing on Milestone; annotations=%v", got.Annotations)
	}
}

// ---------------------------------------------------------------------------
// Plan 12-02 additions: D-07 guard — approve refuses Failed levels
// ---------------------------------------------------------------------------

// TestApproveRunFailedLevelError asserts that approveRun returns a non-nil
// error when a Failed Milestone exists, and the error text contains
// "retry-failed" and the level name "ms-alpha".
func TestApproveRunFailedLevelError(t *testing.T) {
	p := makeProject("my-project")
	ms := makeFailedMilestone("ms-alpha", "my-project", nil)

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	err := approveRun(context.Background(), c, "default", "my-project", "", nil)
	if err == nil {
		t.Fatal("expected error when a Failed level exists; got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "retry-failed") {
		t.Errorf("error should contain 'retry-failed'; got: %q", errStr)
	}
	if !strings.Contains(errStr, "ms-alpha") {
		t.Errorf("error should name the failed level 'ms-alpha'; got: %q", errStr)
	}
}

// TestApproveFailedLevelErrorIncludesReason asserts that when the Failed
// Milestone carries a condition with a Reason and Message, the error text
// includes that information (D-07 — print failure reason).
func TestApproveFailedLevelErrorIncludesReason(t *testing.T) {
	p := makeProject("my-project")
	ms := makeFailedMilestone("ms-alpha", "my-project", []metav1.Condition{
		{
			Type:               tidev1alpha1.ConditionWaveOrLevelPaused,
			Status:             metav1.ConditionTrue,
			Reason:             "PlannerJobFailed",
			Message:            "planner job exceeded backoffLimit",
			LastTransitionTime: metav1.Now(),
		},
	})

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	err := approveRun(context.Background(), c, "default", "my-project", "", nil)
	if err == nil {
		t.Fatal("expected error when a Failed level exists; got nil")
	}
	errStr := err.Error()
	if !strings.Contains(errStr, "planner job exceeded backoffLimit") {
		t.Errorf("error should include failure message; got: %q", errStr)
	}
}

// ---------------------------------------------------------------------------
// CUTS-01 finding-6 regression: label-filtered discovery (D-02)
// ---------------------------------------------------------------------------

// TestApproveUnlabeledMilestoneNotDiscovered is the SYMPTOM case for CUTS-01
// run-1 finding 6: a Milestone at AwaitingApproval WITHOUT the
// tideproject.k8s/project label is NOT discovered by findAwaitingMilestone —
// the caller gets "no level awaiting approval" despite a parked CR.
//
// D-02 locks `tide approve` discovery to label-filter-only; the test pins that
// contract so a future "helpful" OwnerRef fallback does not silently change
// the approved surface (T-15-01 mitigation).
func TestApproveUnlabeledMilestoneNotDiscovered(t *testing.T) {
	p := makeProject("proj-unlabeled")
	ms := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-unlabeled",
			Namespace: "default",
			// Labels intentionally absent — reproduces pre-Phase-15 reporter shape.
		},
		Spec:   tidev1alpha1.MilestoneSpec{ProjectRef: "proj-unlabeled"},
		Status: tidev1alpha1.MilestoneStatus{Phase: "AwaitingApproval"},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	err := approveRun(context.Background(), c, "default", "proj-unlabeled", "", nil)
	if err == nil {
		t.Fatal("expected 'no level awaiting approval' error for unlabeled Milestone; got nil")
	}
	if strings.Contains(err.Error(), "ms-unlabeled") {
		t.Errorf("unlabeled Milestone should NOT be discovered by label-filter; got error mentioning it: %q", err.Error())
	}
}

// TestApproveLabeledMilestoneDiscoveredFirstCall is the FIX case for CUTS-01
// run-1 finding 6: a Milestone at AwaitingApproval WITH the
// tideproject.k8s/project label (as MaterializeChildCRDs now stamps via D-01)
// IS discovered on the FIRST approveRun call — no "no level awaiting approval"
// false negative.
func TestApproveLabeledMilestoneDiscoveredFirstCall(t *testing.T) {
	p := makeProject("proj-labeled")
	ms := makeMilestoneAwaiting("ms-labeled", "proj-labeled")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	if err := approveRun(context.Background(), c, "default", "proj-labeled", "", nil); err != nil {
		t.Fatalf("approveRun on labeled Milestone: %v — should discover it on first call (CUTS-01 fix)", err)
	}

	var got tidev1alpha1.Milestone
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ms-labeled"}, &got); err != nil {
		t.Fatalf("get milestone: %v", err)
	}
	if v := got.Annotations["tideproject.k8s/approve-milestone"]; v != "true" {
		t.Errorf("approve annotation not written; annotations=%v", got.Annotations)
	}
}

// TestApproveFailedLevelNoAnnotationWritten asserts that when a Failed level
// blocks approval, no approve annotation is written on the Milestone —
// approval never doubles as a spend-retry (T-12-05).
func TestApproveFailedLevelNoAnnotationWritten(t *testing.T) {
	p := makeProject("my-project")
	ms := makeFailedMilestone("ms-alpha", "my-project", nil)

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, ms).Build()

	_ = approveRun(context.Background(), c, "default", "my-project", "", nil)

	var got tidev1alpha1.Milestone
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "ms-alpha"}, &got); err != nil {
		t.Fatalf("get milestone: %v", err)
	}
	if v := got.Annotations["tideproject.k8s/approve-milestone"]; v != "" {
		t.Errorf("expected no approve annotation written when Failed level blocks; got %q", v)
	}
}
