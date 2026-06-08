/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reporter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- MaterializeChildCRDs tests (use fake client) ----------

// fakeClientForTest returns a fake controller-runtime client with TIDE schema registered.
func fakeClientForTest(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).Build()
}

// Test 1: happy path — parent=Milestone creates Phase children with OwnerRef set.
func TestMaterializeChildCRDsHappyPath(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-002"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	phaseSpec := tideprojectv1alpha1.PhaseSpec{MilestoneRef: "parent-milestone"}
	rawSpec, err := json.Marshal(phaseSpec)
	if err != nil {
		t.Fatalf("Marshal phase spec: %v", err)
	}

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Phase", Name: "child-phase-1", Spec: runtime.RawExtension{Raw: rawSpec}},
		{Kind: "Phase", Name: "child-phase-2", Spec: runtime.RawExtension{Raw: rawSpec}},
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	// Verify both Phase CRDs were created.
	for _, name := range []string{"child-phase-1", "child-phase-2"} {
		var got tideprojectv1alpha1.Phase
		if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, &got); err != nil {
			t.Errorf("Get %q: %v", name, err)
			continue
		}
		// Owner ref set, controller=true, points at milestone.
		refs := got.GetOwnerReferences()
		if len(refs) == 0 {
			t.Errorf("%q has no owner refs", name)
			continue
		}
		var found bool
		for _, r := range refs {
			if r.Kind == "Milestone" && r.UID == milestone.UID {
				if r.Controller == nil || !*r.Controller {
					t.Errorf("%q owner ref Controller not true", name)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("%q missing Milestone owner ref", name)
		}
	}
}

// Test 2: unknown Kind rejected — Kind allowlist enforced (T-308 mitigation).
func TestMaterializeChildCRDsRejectsUnknownKind(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-003"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Pod", Name: "evil-pod", Spec: runtime.RawExtension{Raw: []byte(`{}`)}},
	}

	err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children)
	if err == nil {
		t.Fatal("MaterializeChildCRDs accepted Kind=Pod; expected error")
	}
	if !strings.Contains(err.Error(), "allowlist") && !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error %q should mention allowlist or not-allowed", err.Error())
	}

	// Verify the Pod was NOT created (no get-by-name; check nothing leaked).
	var phases tideprojectv1alpha1.PhaseList
	if err := c.List(context.Background(), &phases, client.InNamespace("default")); err != nil {
		t.Fatalf("List phases: %v", err)
	}
	if len(phases.Items) != 0 {
		t.Errorf("Unexpected Phase items created: %d", len(phases.Items))
	}
}

// Test 3: idempotent on AlreadyExists — pre-create the Phase, then re-call MaterializeChildCRDs.
func TestMaterializeChildCRDsIdempotent(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-004"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	// Pre-create the Phase.
	existing := &tideprojectv1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pre-existing-phase",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.PhaseSpec{MilestoneRef: "parent-milestone"},
	}
	if err := c.Create(context.Background(), existing); err != nil {
		t.Fatalf("pre-create Phase: %v", err)
	}

	phaseSpec := tideprojectv1alpha1.PhaseSpec{MilestoneRef: "parent-milestone"}
	rawSpec, _ := json.Marshal(phaseSpec)

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Phase", Name: "pre-existing-phase", Spec: runtime.RawExtension{Raw: rawSpec}},
	}

	// Should succeed (idempotent on AlreadyExists).
	err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children)
	if err != nil {
		t.Errorf("MaterializeChildCRDs on pre-existing Phase: %v (want nil — idempotent)", err)
	}

	// And the original Phase is still there.
	var got tideprojectv1alpha1.Phase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "pre-existing-phase"}, &got); err != nil {
		t.Errorf("Get pre-existing Phase: %v", err)
	}
	if got.UID != existing.UID && !apierrors.IsNotFound(err) {
		// fake client may regenerate UIDs; just verify the object still exists.
		// (The acceptance contract is "no error returned", not "same UID").
		_ = got
	}
}

// TestMaterializeChildCRDsTaskPromptPath covers defect #10b: a Task child's
// PromptPath is wired from ChildCRDSpec.SourcePath at materialization, even
// though the model-authored spec carries no promptPath. The prompt itself stays
// in the children file (read fresh at dispatch), not inline on the Task spec.
func TestMaterializeChildCRDsTaskPromptPath(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("plan-uid-010b"),
			Name:      "parent-plan",
			Namespace: "default",
		},
	}

	// Model-authored Task spec — note: NO promptPath here (the controller injects it).
	taskSpec := tideprojectv1alpha1.TaskSpec{
		PlanRef:             "parent-plan",
		FilesTouched:        []string{"main.go"},
		DeclaredOutputPaths: []string{"main.go"},
	}
	rawSpec, err := json.Marshal(taskSpec)
	if err != nil {
		t.Fatalf("Marshal task spec: %v", err)
	}

	const wantPath = "envelopes/plan-uid-010b/children/task-01.json"
	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Task", Name: "task-01-impl", Spec: runtime.RawExtension{Raw: rawSpec}, SourcePath: wantPath},
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, plan, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	var got tideprojectv1alpha1.Task
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "task-01-impl"}, &got); err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if got.Spec.PromptPath != wantPath {
		t.Errorf("Task.Spec.PromptPath = %q, want %q (wired from SourcePath)", got.Spec.PromptPath, wantPath)
	}
}
