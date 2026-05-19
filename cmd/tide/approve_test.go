/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 1 — RED tests for `tide approve`. Asserts the verb writes
// the canonical annotation key set defined in internal/gates/annotation.go on
// either (a) the AwaitingApproval level discovered from Project Status
// Conditions, or (b) the Plan when --wave plan/N is provided.
package main

import (
	"context"
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
