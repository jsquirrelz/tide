/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 1 — RED tests for `tide resume`. Asserts the verb clears
// the `tideproject.k8s/reject` annotation via gates.ConsumeReject + Patch.
package main

import (
	"context"
	"testing"

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

	if err := resumeRun(context.Background(), c, "default", "my-project"); err != nil {
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

	if err := resumeRun(context.Background(), c, "default", "my-project"); err != nil {
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
	if err := resumeRun(context.Background(), c, "default", "missing"); err == nil {
		t.Fatal("expected not-found error; got nil")
	}
}

func TestResumeNoOpWhenNoReject(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()
	if err := resumeRun(context.Background(), c, "default", "my-project"); err != nil {
		t.Fatalf("resumeRun on un-rejected project should be no-op; got %v", err)
	}
}
