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

// Plan 25-01 Task 2 — RED tests for FailureHalt condition vocabulary + shared helpers.
// Tests: checkFailureHalt, setFailureHaltIfNeeded.
// These tests are RED until 25-03 Task 1 implements failure_halt.go.
package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// ---------- checkFailureHalt ----------

func TestCheckFailureHalt_TrueWhenConditionPresent(t *testing.T) {
	project := &tideprojectv1alpha2.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha2.ConditionFailureHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha2.ReasonTaskFailedHalt,
		LastTransitionTime: metav1.Now(),
	})
	if !checkFailureHalt(project) {
		t.Error("expected checkFailureHalt=true when FailureHalt=True condition present")
	}
}

func TestCheckFailureHalt_FalseWhenConditionAbsent(t *testing.T) {
	project := &tideprojectv1alpha2.Project{}
	if checkFailureHalt(project) {
		t.Error("expected checkFailureHalt=false when no conditions")
	}
}

func TestCheckFailureHalt_FalseForNilProject(t *testing.T) {
	if checkFailureHalt(nil) {
		t.Error("expected checkFailureHalt=false for nil project")
	}
}

// ---------- setFailureHaltIfNeeded ----------

func TestSetFailureHaltIfNeeded_StrictProfile_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "strict-project", Namespace: "default"},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha2.FailureProfileStrict,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setFailureHaltIfNeeded(context.Background(), c, project); err != nil {
		t.Fatalf("setFailureHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha2.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "strict-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionFailureHalt)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		t.Errorf("expected NO FailureHalt for strict profile; got %+v", cond)
	}
}

func TestSetFailureHaltIfNeeded_ConservativeStampsHalt(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "my-project", Namespace: "default"},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha2.FailureProfileConservative,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setFailureHaltIfNeeded(context.Background(), c, project); err != nil {
		t.Fatalf("setFailureHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha2.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionFailureHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected FailureHalt=True; got %v", cond)
	}
	if cond.Reason != tideprojectv1alpha2.ReasonTaskFailedHalt {
		t.Errorf("expected Reason=%q; got %q", tideprojectv1alpha2.ReasonTaskFailedHalt, cond.Reason)
	}
}

func TestSetFailureHaltIfNeeded_IdempotentSecondCall(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "idem-project", Namespace: "default"},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha2.FailureProfileConservative,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	// First call stamps the condition.
	if err := setFailureHaltIfNeeded(context.Background(), c, project); err != nil {
		t.Fatalf("first setFailureHaltIfNeeded: %v", err)
	}

	// Re-fetch so the in-memory object is fresh.
	var refreshed tideprojectv1alpha2.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "idem-project"}, &refreshed); err != nil {
		t.Fatalf("re-get project: %v", err)
	}

	// Second call must be a no-op (no patch churn).
	if err := setFailureHaltIfNeeded(context.Background(), c, &refreshed); err != nil {
		t.Fatalf("second setFailureHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha2.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "idem-project"}, &got); err != nil {
		t.Fatalf("get project after idempotent call: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionFailureHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected FailureHalt=True after idempotent second call; got %v", cond)
	}
}

func TestSetFailureHaltIfNeeded_NilProject_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	// Must not panic and must return nil.
	if err := setFailureHaltIfNeeded(context.Background(), c, nil); err != nil {
		t.Fatalf("expected nil error for nil project; got %v", err)
	}
}
