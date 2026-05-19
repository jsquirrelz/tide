/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 2 — RED tests for `tide cancel --force`. Asserts the
// destructive-confirmation gate, foreground cascade delete, dry-run preview,
// and friendly NotFound surface.
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

func cancelFixture(t *testing.T) client.Client {
	t.Helper()
	p := makeProject("my-project")
	// Child fixtures stamped with the canonical project label so --dry-run can
	// enumerate them for the operator.
	m1 := &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-alpha",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/project": "my-project"},
		},
		Spec: tidev1alpha1.MilestoneSpec{ProjectRef: "my-project"},
	}
	return fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p, m1).Build()
}

func TestCancelRequiresForce(t *testing.T) {
	c := cancelFixture(t)
	var stdout, stderr bytes.Buffer
	err := cancelRun(context.Background(), c, "default", "my-project", false /*force*/, false /*dryRun*/, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected --force-required error; got nil")
	}
	if !strings.Contains(err.Error(), "force") {
		t.Errorf("error should mention --force; got %q", err.Error())
	}
	// Project must still exist when --force is absent.
	var got tidev1alpha1.Project
	if gerr := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); gerr != nil {
		t.Errorf("project should still exist; got err=%v", gerr)
	}
}

func TestCancelForceDeletes(t *testing.T) {
	c := cancelFixture(t)
	var stdout, stderr bytes.Buffer
	if err := cancelRun(context.Background(), c, "default", "my-project", true /*force*/, false, &stdout, &stderr); err != nil {
		t.Fatalf("cancelRun: %v", err)
	}
	// Project must be gone.
	var got tidev1alpha1.Project
	gerr := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got)
	if gerr == nil || !apierrors.IsNotFound(gerr) {
		t.Errorf("expected project gone (NotFound); got err=%v", gerr)
	}
	// Confirmation banner on stderr.
	if !strings.Contains(stderr.String(), "Deleting project my-project") {
		t.Errorf("expected delete banner on stderr; got %q", stderr.String())
	}
}

func TestCancelMissingProjectFriendlyError(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	var stdout, stderr bytes.Buffer
	err := cancelRun(context.Background(), c, "default", "missing-project", true, false, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected NotFound error; got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error; got %q", err.Error())
	}
}

func TestCancelDryRunListsChildren(t *testing.T) {
	c := cancelFixture(t)
	var stdout, stderr bytes.Buffer
	if err := cancelRun(context.Background(), c, "default", "my-project", true /*force*/, true /*dry-run*/, &stdout, &stderr); err != nil {
		t.Fatalf("cancelRun dry-run: %v", err)
	}
	// Project still exists (dry-run does not delete).
	var got tidev1alpha1.Project
	if gerr := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); gerr != nil {
		t.Errorf("dry-run should not delete project; got err=%v", gerr)
	}
	// Output should list the child Milestone for operator review.
	combined := stdout.String() + stderr.String()
	for _, want := range []string{"my-project", "ms-alpha"} {
		if !strings.Contains(combined, want) {
			t.Errorf("dry-run output should mention %q; got:\n%s", want, combined)
		}
	}
}
