/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
*/

// Plan 04-08 Task 1 — RED tests for `tide reject`. Asserts the verb writes
// `tideproject.k8s/reject: <reason>` on the Project via client.MergeFrom.
package main

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

func TestRejectWritesAnnotationWithReason(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := rejectRun(context.Background(), c, "default", "my-project", "operator stopped"); err != nil {
		t.Fatalf("rejectRun: %v", err)
	}
	var got tidev1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if v := got.Annotations["tideproject.k8s/reject"]; v != "operator stopped" {
		t.Errorf("expected reject annotation value 'operator stopped'; got %q (annotations=%v)", v, got.Annotations)
	}
}

func TestRejectDefaultsReasonWhenEmpty(t *testing.T) {
	p := makeProject("my-project")
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := rejectRun(context.Background(), c, "default", "my-project", ""); err != nil {
		t.Fatalf("rejectRun: %v", err)
	}
	var got tidev1alpha1.Project
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got)
	if v := got.Annotations["tideproject.k8s/reject"]; v != "rejected by operator" {
		t.Errorf("expected default reason 'rejected by operator'; got %q", v)
	}
}

func TestRejectProjectNotFound(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if err := rejectRun(context.Background(), c, "default", "missing", "x"); err == nil {
		t.Fatal("expected not-found error; got nil")
	}
}

func TestRejectPreservesOtherAnnotations(t *testing.T) {
	p := makeProject("my-project")
	p.Annotations = map[string]string{"unrelated.example.com/key": "keep"}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(p).Build()

	if err := rejectRun(context.Background(), c, "default", "my-project", "halt now"); err != nil {
		t.Fatalf("rejectRun: %v", err)
	}
	var got tidev1alpha1.Project
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got)
	if got.Annotations["unrelated.example.com/key"] != "keep" {
		t.Errorf("MergeFrom should preserve unrelated annotation; got %v", got.Annotations)
	}
	if got.Annotations["tideproject.k8s/reject"] != "halt now" {
		t.Errorf("reject annotation missing; annotations=%v", got.Annotations)
	}
}
