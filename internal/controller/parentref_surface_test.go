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

// Defect #17 defense-in-depth tests: when a level controller resolves its
// direct parent via the spec parent-ref and gets NotFound, it must SURFACE the
// stall (status condition + requeue) rather than silently requeue forever. Pure
// Go fake-client tests (no envtest/Ginkgo) so they run fast.
package controller

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// TestPhaseReconciler_ParentRefNotFound_Surfaces verifies that a Phase whose
// spec.milestoneRef names a non-existent Milestone requeues AND surfaces
// ConditionParentUnresolved (the silent-requeue bug from defect #17). The
// finalizer is pre-set so reconcile reaches step 4 (parent-ref resolution).
func TestPhaseReconciler_ParentRefNotFound_Surfaces(t *testing.T) {
	s := fakeSchemeWithAll(t)
	phase := &tideprojectv1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "phase-01",
			Namespace:  "default",
			UID:        types.UID("phase-uid-17"),
			Finalizers: []string{phaseFinalizer},
		},
		Spec: tideprojectv1alpha1.PhaseSpec{
			// Mismatched parent-ref: no such Milestone exists.
			MilestoneRef: "milestone-02-does-not-exist",
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(phase).
		WithStatusSubresource(&tideprojectv1alpha1.Phase{}).
		Build()
	r := &PhaseReconciler{Client: c, Scheme: s}

	res, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "phase-01"},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	//nolint:staticcheck // SA1019: asserting the controller preserves the legacy Requeue field set by step-4.
	if !res.Requeue {
		t.Errorf("expected Requeue=true so it self-heals when the parent appears, got %+v", res)
	}

	var got tideprojectv1alpha1.Phase
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "phase-01"}, &got); err != nil {
		t.Fatalf("Get phase: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionParentUnresolved)
	if cond == nil {
		t.Fatalf("expected ConditionParentUnresolved to be set; conditions=%+v", got.Status.Conditions)
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("condition Status = %q, want False", cond.Status)
	}
	if cond.Reason != tideprojectv1alpha1.ReasonParentRefNotFound {
		t.Errorf("condition Reason = %q, want %q", cond.Reason, tideprojectv1alpha1.ReasonParentRefNotFound)
	}
	if !strings.Contains(cond.Message, "milestone-02-does-not-exist") {
		t.Errorf("condition Message %q should name the missing parent-ref", cond.Message)
	}
}

// TestMilestoneReconciler_ParentRefNotFound_Surfaces is the symmetric check one
// level up: a Milestone whose spec.projectRef names a non-existent Project
// requeues AND surfaces ConditionParentUnresolved.
func TestMilestoneReconciler_ParentRefNotFound_Surfaces(t *testing.T) {
	s := fakeSchemeWithAll(t)
	ms := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "milestone-01",
			Namespace:  "default",
			UID:        types.UID("ms-uid-17"),
			Finalizers: []string{milestoneFinalizer},
		},
		Spec: tideprojectv1alpha1.MilestoneSpec{
			ProjectRef: "project-99-does-not-exist",
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(ms).
		WithStatusSubresource(&tideprojectv1alpha1.Milestone{}).
		Build()
	r := &MilestoneReconciler{Client: c, Scheme: s}

	res, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "milestone-01"},
	})
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	//nolint:staticcheck // SA1019: asserting the controller preserves the legacy Requeue field set by step-4.
	if !res.Requeue {
		t.Errorf("expected Requeue=true, got %+v", res)
	}

	var got tideprojectv1alpha1.Milestone
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "milestone-01"}, &got); err != nil {
		t.Fatalf("Get milestone: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionParentUnresolved)
	if cond == nil {
		t.Fatalf("expected ConditionParentUnresolved to be set; conditions=%+v", got.Status.Conditions)
	}
	if cond.Reason != tideprojectv1alpha1.ReasonParentRefNotFound {
		t.Errorf("condition Reason = %q, want %q", cond.Reason, tideprojectv1alpha1.ReasonParentRefNotFound)
	}
	if !strings.Contains(cond.Message, "project-99-does-not-exist") {
		t.Errorf("condition Message %q should name the missing parent-ref", cond.Message)
	}
}
