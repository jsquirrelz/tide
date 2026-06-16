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

// Plan 14-02 Task 3 — unit tests for BudgetBlocked condition helpers.
// Tests: checkBudgetBlocked, setBudgetBlockedIfNeeded (set, idempotent, clear, nil).
package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// ---------- checkBudgetBlocked ----------

func TestCheckBudgetBlocked_TrueWhenConditionPresent(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonBudgetCapReached,
		LastTransitionTime: metav1.Now(),
	})
	if !checkBudgetBlocked(project) {
		t.Error("expected checkBudgetBlocked=true when BudgetBlocked=True condition present")
	}
}

func TestCheckBudgetBlocked_FalseWhenConditionAbsent(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	if checkBudgetBlocked(project) {
		t.Error("expected checkBudgetBlocked=false when no conditions")
	}
}

func TestCheckBudgetBlocked_FalseWhenConditionFalse(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonBudgetCapCleared,
		LastTransitionTime: metav1.Now(),
	})
	if checkBudgetBlocked(project) {
		t.Error("expected checkBudgetBlocked=false when BudgetBlocked=False")
	}
}

func TestCheckBudgetBlocked_FalseForNilProject(t *testing.T) {
	if checkBudgetBlocked(nil) {
		t.Error("expected checkBudgetBlocked=false for nil project")
	}
}

// ---------- setBudgetBlockedIfNeeded ----------

// projectWithCap creates a Project with the given cap and spent values (for fake client).
func projectWithCap(name string, capCents, spentCents int64) *tideprojectv1alpha1.Project {
	return &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://example.com/repo.git",
			Budget:     tideprojectv1alpha1.BudgetConfig{AbsoluteCapCents: capCents},
		},
		Status: tideprojectv1alpha1.ProjectStatus{
			Budget: tideprojectv1alpha1.BudgetStatus{CostSpentCents: spentCents},
		},
	}
}

func TestSetBudgetBlockedIfNeeded_SetsCondition(t *testing.T) {
	s := fakeSchemeWithAll(t)
	// cap=1000, spent=1001 → IsCapExceeded=true
	project := projectWithCap("my-project", 1000, 1001)
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setBudgetBlockedIfNeeded(context.Background(), c, project, 50); err != nil {
		t.Fatalf("setBudgetBlockedIfNeeded: %v", err)
	}

	var got tideprojectv1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}

	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
	if cond == nil {
		t.Fatal("expected BudgetBlocked condition to be set; got nil")
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("expected BudgetBlocked=True; got %q", cond.Status)
	}
	if cond.Reason != tideprojectv1alpha1.ReasonBudgetCapReached {
		t.Errorf("expected Reason=%q; got %q", tideprojectv1alpha1.ReasonBudgetCapReached, cond.Reason)
	}
	if len(cond.Message) == 0 {
		t.Error("expected non-empty condition Message")
	}
	// Message must mention the reserved cents and cap amounts.
	if !containsStr(cond.Message, "1001") {
		t.Errorf("expected condition Message to mention spent cents (1001); got %q", cond.Message)
	}
	if !containsStr(cond.Message, "50") {
		t.Errorf("expected condition Message to mention reserved cents (50); got %q", cond.Message)
	}
}

func TestSetBudgetBlockedIfNeeded_CapNotExceeded_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	// cap=1000, spent=500 → IsCapExceeded=false
	project := projectWithCap("under-cap-project", 1000, 500)
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setBudgetBlockedIfNeeded(context.Background(), c, project, 0); err != nil {
		t.Fatalf("setBudgetBlockedIfNeeded: %v", err)
	}

	var got tideprojectv1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "under-cap-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
	if cond != nil {
		t.Errorf("expected no BudgetBlocked condition when cap not exceeded; got %+v", cond)
	}
}

func TestSetBudgetBlockedIfNeeded_Idempotent(t *testing.T) {
	s := fakeSchemeWithAll(t)
	// cap=1000, spent=1001 → cap exceeded; condition already set
	project := projectWithCap("already-blocked-project", 1000, 1001)
	// Pre-stamp the condition
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonBudgetCapReached,
		Message:            "already set",
		LastTransitionTime: metav1.Now(),
	})
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	// Call twice — second call must be a no-op (no patch).
	if err := setBudgetBlockedIfNeeded(context.Background(), c, project, 0); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := setBudgetBlockedIfNeeded(context.Background(), c, project, 0); err != nil {
		t.Fatalf("second call (idempotent): %v", err)
	}

	var got tideprojectv1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "already-blocked-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected BudgetBlocked=True after idempotent call; got %v", cond)
	}
}

func TestSetBudgetBlockedIfNeeded_NilProject_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	// Must not panic and must return nil.
	if err := setBudgetBlockedIfNeeded(context.Background(), c, nil, 0); err != nil {
		t.Fatalf("expected nil error for nil project; got %v", err)
	}
}

// TestSetBudgetBlockedIfNeeded_CapRaised_ClearsCondition verifies the cap-raise
// recovery path: when the condition is currently True but IsCapExceeded is now false
// (operator raised the cap), the condition must be patched to False.
func TestSetBudgetBlockedIfNeeded_CapRaised_ClearsCondition(t *testing.T) {
	s := fakeSchemeWithAll(t)
	// cap=2000 (raised), spent=1001 — no longer exceeded.
	project := projectWithCap("raised-cap-project", 2000, 1001)
	// Pre-stamp the condition as True (was previously exceeded).
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBudgetBlocked,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonBudgetCapReached,
		Message:            "was exceeded",
		LastTransitionTime: metav1.Now(),
	})
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setBudgetBlockedIfNeeded(context.Background(), c, project, 0); err != nil {
		t.Fatalf("setBudgetBlockedIfNeeded: %v", err)
	}

	var got tideprojectv1alpha1.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "raised-cap-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha1.ConditionBudgetBlocked)
	if cond == nil {
		t.Fatal("expected BudgetBlocked condition after clear; got nil")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("cap-raise recovery: expected BudgetBlocked=False; got %q", cond.Status)
	}
	if cond.Reason != tideprojectv1alpha1.ReasonBudgetCapCleared {
		t.Errorf("cap-raise recovery: expected Reason=%q; got %q", tideprojectv1alpha1.ReasonBudgetCapCleared, cond.Reason)
	}
}
