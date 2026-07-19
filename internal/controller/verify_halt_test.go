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

// Plan 51-05 Task 1 — tests for VerifyHalt condition vocabulary + shared helpers.
// Tests: checkVerifyHalt, setVerifyHaltIfNeeded. Mirrors failure_halt_test.go,
// diverging only where setVerifyHaltIfNeeded's trigger diverges from
// setFailureHaltIfNeeded's (no FailureProfile gate — the exhaustion trigger
// lives at the Plan 07 call site, not inside this helper).
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

// ---------- checkVerifyHalt ----------

func TestCheckVerifyHalt_TrueWhenConditionPresent(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionVerifyHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonVerifyExhausted,
		LastTransitionTime: metav1.Now(),
	})
	if !checkVerifyHalt(project) {
		t.Error("expected checkVerifyHalt=true when VerifyHalt=True condition present")
	}
}

func TestCheckVerifyHalt_FalseWhenConditionAbsent(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	if checkVerifyHalt(project) {
		t.Error("expected checkVerifyHalt=false when no conditions")
	}
}

func TestCheckVerifyHalt_FalseForNilProject(t *testing.T) {
	if checkVerifyHalt(nil) {
		t.Error("expected checkVerifyHalt=false for nil project")
	}
}

// ---------- setVerifyHaltIfNeeded ----------

// Divergence from setFailureHaltIfNeeded: there is no FailureProfile gate.
// Exhaustion stamps the halt regardless of profile — the exhaustion trigger
// lives at the Plan 07 call site, not inside this helper.
func TestSetVerifyHaltIfNeeded_NoFailureProfileGate_StampsUnderStrictProfile(t *testing.T) {
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

	if err := setVerifyHaltIfNeeded(context.Background(), c, project, time.Time{}); err != nil {
		t.Fatalf("setVerifyHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "strict-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected VerifyHalt=True even under strict profile (no FailureProfile gate); got %v", cond)
	}
	if cond.Reason != tideprojectv1alpha3.ReasonVerifyExhausted {
		t.Errorf("expected Reason=%q; got %q", tideprojectv1alpha3.ReasonVerifyExhausted, cond.Reason)
	}
}

func TestSetVerifyHaltIfNeeded_IdempotentSecondCall(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "idem-project", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	// First call stamps the condition.
	if err := setVerifyHaltIfNeeded(context.Background(), c, project, time.Time{}); err != nil {
		t.Fatalf("first setVerifyHaltIfNeeded: %v", err)
	}

	// Re-fetch so the in-memory object is fresh.
	var refreshed tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "idem-project"}, &refreshed); err != nil {
		t.Fatalf("re-get project: %v", err)
	}

	// Second call must be a no-op (no patch churn).
	if err := setVerifyHaltIfNeeded(context.Background(), c, &refreshed, time.Time{}); err != nil {
		t.Fatalf("second setVerifyHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "idem-project"}, &got); err != nil {
		t.Fatalf("get project after idempotent call: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected VerifyHalt=True after idempotent second call; got %v", cond)
	}
}

func TestSetVerifyHaltIfNeeded_NilProject_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	// Must not panic and must return nil.
	if err := setVerifyHaltIfNeeded(context.Background(), c, nil, time.Time{}); err != nil {
		t.Fatalf("expected nil error for nil project; got %v", err)
	}
}

// CR-02 resume time-fence: an exhaustion that predates AnnotationVerifyResumedAt
// is a pre-resume straggler and must NOT re-stamp the halt.
func TestSetVerifyHaltIfNeeded_StaleExhaustionBeforeResume_NoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	resumedAt := time.Now()
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fenced-project",
			Namespace: "default",
			Annotations: map[string]string{
				tideprojectv1alpha3.AnnotationVerifyResumedAt: resumedAt.UTC().Format(time.RFC3339),
			},
		},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	// Exhaustion completed an hour before the resume fence → no re-stamp.
	stale := resumedAt.Add(-time.Hour)
	if err := setVerifyHaltIfNeeded(context.Background(), c, project, stale); err != nil {
		t.Fatalf("setVerifyHaltIfNeeded (stale): %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "fenced-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		t.Errorf("expected NO VerifyHalt for pre-resume straggler; got %+v", cond)
	}
}

// CR-02: a fresh exhaustion AFTER the resume fence must still stamp the halt.
func TestSetVerifyHaltIfNeeded_FreshExhaustionAfterResume_Stamps(t *testing.T) {
	s := fakeSchemeWithAll(t)
	resumedAt := time.Now()
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fresh-project",
			Namespace: "default",
			Annotations: map[string]string{
				tideprojectv1alpha3.AnnotationVerifyResumedAt: resumedAt.UTC().Format(time.RFC3339),
			},
		},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	fresh := resumedAt.Add(time.Hour)
	if err := setVerifyHaltIfNeeded(context.Background(), c, project, fresh); err != nil {
		t.Fatalf("setVerifyHaltIfNeeded (fresh): %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "fresh-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected VerifyHalt=True for fresh post-resume exhaustion; got %v", cond)
	}
}

// CR-02: an unparseable resume annotation fails closed toward stamping.
func TestSetVerifyHaltIfNeeded_UnparseableResumeAnnotation_FallsThroughToStamp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unparseable-project",
			Namespace: "default",
			Annotations: map[string]string{
				tideprojectv1alpha3.AnnotationVerifyResumedAt: "not-a-timestamp",
			},
		},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project).
		WithStatusSubresource(project).
		Build()

	if err := setVerifyHaltIfNeeded(context.Background(), c, project, time.Now()); err != nil {
		t.Fatalf("setVerifyHaltIfNeeded: %v", err)
	}

	var got tideprojectv1alpha3.Project
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "unparseable-project"}, &got); err != nil {
		t.Fatalf("get project: %v", err)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha3.ConditionVerifyHalt)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Errorf("expected VerifyHalt=True on unparseable annotation (fail-closed); got %v", cond)
	}
}
