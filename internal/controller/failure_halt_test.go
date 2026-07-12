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
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// ---------- checkFailureHalt ----------

func TestCheckFailureHalt_TrueWhenConditionPresent(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionFailureHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonTaskFailedHalt,
		LastTransitionTime: metav1.Now(),
	})
	if !checkFailureHalt(project) {
		t.Error("expected checkFailureHalt=true when FailureHalt=True condition present")
	}
}

func TestCheckFailureHalt_FalseWhenConditionAbsent(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
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
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "strict-project", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileStrict,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setFailureHaltIfNeeded(context.Background(), c, project, time.Time{}); err != nil {
		t.Fatalf("setFailureHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "strict-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		t.Errorf("expected NO FailureHalt for strict profile; got %+v", cond)
	}
}

func TestSetFailureHaltIfNeeded_ConservativeStampsHalt(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "my-project", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setFailureHaltIfNeeded(context.Background(), c, project, time.Time{}); err != nil {
		t.Fatalf("setFailureHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "my-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected FailureHalt=True; got %v", cond)
	}
	if cond.Reason != tideprojectv1alpha3.ReasonTaskFailedHalt {
		t.Errorf("expected Reason=%q; got %q", tideprojectv1alpha3.ReasonTaskFailedHalt, cond.Reason)
	}
}

func TestSetFailureHaltIfNeeded_IdempotentSecondCall(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "idem-project", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	// First call stamps the condition.
	if err := setFailureHaltIfNeeded(context.Background(), c, project, time.Time{}); err != nil {
		t.Fatalf("first setFailureHaltIfNeeded: %v", err)
	}

	// Re-fetch so the in-memory object is fresh.
	var refreshed tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "idem-project"}, &refreshed); err != nil {
		t.Fatalf("re-get project: %v", err)
	}

	// Second call must be a no-op (no patch churn).
	if err := setFailureHaltIfNeeded(context.Background(), c, &refreshed, time.Time{}); err != nil {
		t.Fatalf("second setFailureHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "idem-project"}, &got); err != nil {
		t.Fatalf("get project after idempotent call: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected FailureHalt=True after idempotent second call; got %v", cond)
	}
}

func TestSetFailureHaltIfNeeded_NilProject_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	// Must not panic and must return nil.
	if err := setFailureHaltIfNeeded(context.Background(), c, nil, time.Time{}); err != nil {
		t.Fatalf("expected nil error for nil project; got %v", err)
	}
}

// CR-02 resume time-fence: a failure that predates AnnotationFailureResumedAt is
// a pre-resume straggler and must NOT re-stamp the halt.
func TestSetFailureHaltIfNeeded_StaleFailureBeforeResume_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	resumedAt := time.Now()
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fenced-project",
			Namespace: "default",
			Annotations: map[string]string{
				tideprojectv1alpha3.AnnotationFailureResumedAt: resumedAt.UTC().Format(time.RFC3339),
			},
		},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	// Failure completed an hour before the resume fence → no re-stamp.
	stale := resumedAt.Add(-time.Hour)
	if err := setFailureHaltIfNeeded(context.Background(), c, project, stale); err != nil {
		t.Fatalf("setFailureHaltIfNeeded (stale): %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "fenced-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		t.Errorf("expected NO FailureHalt for pre-resume straggler; got %+v", cond)
	}
}

// CR-02: a fresh failure AFTER the resume fence must still stamp the halt.
func TestSetFailureHaltIfNeeded_FreshFailureAfterResume_Stamps(t *testing.T) {
	s := fakeSchemeWithAll(t)
	resumedAt := time.Now()
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fresh-project",
			Namespace: "default",
			Annotations: map[string]string{
				tideprojectv1alpha3.AnnotationFailureResumedAt: resumedAt.UTC().Format(time.RFC3339),
			},
		},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			FailureProfile: tideprojectv1alpha3.FailureProfileConservative,
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	fresh := resumedAt.Add(time.Hour)
	if err := setFailureHaltIfNeeded(context.Background(), c, project, fresh); err != nil {
		t.Fatalf("setFailureHaltIfNeeded (fresh): %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "fresh-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionFailureHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected FailureHalt=True for fresh post-resume failure; got %v", cond)
	}
}
