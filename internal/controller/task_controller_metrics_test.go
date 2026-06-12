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

// Phase 16 Plan 02 Task 2: unit tests for resolveWave and emitTaskMetrics.
// Plain go tests with fake controller-runtime client (no envtest needed) —
// follows the billing_halt_test.go + task_controller_extracted_test.go pattern.
package controller

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- resolveWave ----------

// TestResolveWave_WaveOwner asserts that a Task with a Wave OwnerReference
// returns the Wave CRD name as the wave label value (D-09).
func TestResolveWave_WaveOwner(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-wave-owner",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Wave", Name: "tide-wave-abc-0", APIVersion: "tideproject.k8s/v1alpha1"},
			},
		},
	}
	got := r.resolveWave(task)
	if got != "tide-wave-abc-0" {
		t.Errorf("resolveWave: got %q, want %q", got, "tide-wave-abc-0")
	}
}

// TestResolveWave_NoWaveOwner asserts that a Task with no Wave OwnerReference
// returns "unknown" (D-09 sentinel, RESEARCH Pitfall 4).
func TestResolveWave_NoWaveOwner(t *testing.T) {
	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-no-wave",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Plan", Name: "some-plan", APIVersion: "tideproject.k8s/v1alpha1"},
			},
		},
	}
	got := r.resolveWave(task)
	if got != "unknown" {
		t.Errorf("resolveWave with no Wave owner: got %q, want %q", got, "unknown")
	}
}

// ---------- emitTaskMetrics ----------

// newFakeSchemeForMetrics returns a scheme for the metrics test helpers.
// Reuses fakeSchemeWithAll from task_controller_extracted_test.go.
func newFakeSchemeForMetrics(t *testing.T) *runtime.Scheme {
	t.Helper()
	return fakeSchemeWithAll(t)
}

// TestEmitTaskMetrics_EndToEnd exercises the full happy path:
// - Plan named "plan-m1" with PhaseRef "phase-m1"
// - Task in "default" namespace with PlanRef "plan-m1" and Wave OwnerRef "tide-wave-x-0"
// - Project "proj-m1"
// - Usage with known token and cost values
// - StartedAt 90s before completedAt
// Asserts all six metrics land with correct label values.
func TestEmitTaskMetrics_EndToEnd(t *testing.T) {
	s := newFakeSchemeForMetrics(t)

	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-m1", Namespace: "default"},
		Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: "phase-m1"},
	}
	now := time.Now().UTC()
	startedAt := metav1.NewTime(now.Add(-90 * time.Second))
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-m1",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Wave", Name: "tide-wave-x-0", APIVersion: "tideproject.k8s/v1alpha1"},
			},
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             "plan-m1",
			FilesTouched:        []string{"foo"},
			DeclaredOutputPaths: []string{"foo"},
			PromptPath:          "envelopes/test/children/task-01.json",
		},
		Status: tideprojectv1alpha1.TaskStatus{
			StartedAt: &startedAt,
		},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-m1", Namespace: "default"},
		Spec:       tideprojectv1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
	}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(plan, task, project).
		WithStatusSubresource(task, project).
		Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	usage := pkgdispatch.Usage{
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     10,
		CacheCreationTokens: 5,
		EstimatedCostCents:  42,
	}

	if err := r.emitTaskMetrics(context.Background(), task, project, usage, now); err != nil {
		t.Fatalf("emitTaskMetrics: %v", err)
	}

	labels := []string{"proj-m1", "phase-m1", "plan-m1", "tide-wave-x-0"}

	if got := testutil.ToFloat64(tidemetrics.TokensInputTotal.WithLabelValues(labels...)); got != 100 {
		t.Errorf("TokensInputTotal: got %v, want 100", got)
	}
	if got := testutil.ToFloat64(tidemetrics.TokensOutputTotal.WithLabelValues(labels...)); got != 50 {
		t.Errorf("TokensOutputTotal: got %v, want 50", got)
	}
	if got := testutil.ToFloat64(tidemetrics.TokensCacheReadTotal.WithLabelValues(labels...)); got != 10 {
		t.Errorf("TokensCacheReadTotal: got %v, want 10", got)
	}
	if got := testutil.ToFloat64(tidemetrics.TokensCacheCreationTotal.WithLabelValues(labels...)); got != 5 {
		t.Errorf("TokensCacheCreationTotal: got %v, want 5", got)
	}
	if got := testutil.ToFloat64(tidemetrics.CostCentsTotal.WithLabelValues(labels...)); got != 42 {
		t.Errorf("CostCentsTotal: got %v, want 42", got)
	}
	// Histogram: verify at least one observation landed.
	if count := testutil.CollectAndCount(tidemetrics.TaskDurationSeconds); count < 1 {
		t.Errorf("TaskDurationSeconds: no observations collected")
	}
}

// TestEmitTaskMetrics_PhaseMissSentinel asserts that when the Task's PlanRef
// names a nonexistent Plan, the phase label falls back to "unknown".
func TestEmitTaskMetrics_PhaseMissSentinel(t *testing.T) {
	s := newFakeSchemeForMetrics(t)
	// Seed no Plan — get will return NotFound.
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj-miss", Namespace: "default"},
		Spec:       tideprojectv1alpha1.ProjectSpec{TargetRepo: "https://example.com/repo.git"},
	}
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-miss",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Wave", Name: "tide-wave-miss-0", APIVersion: "tideproject.k8s/v1alpha1"},
			},
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             "nonexistent-plan",
			FilesTouched:        []string{"bar"},
			DeclaredOutputPaths: []string{"bar"},
			PromptPath:          "envelopes/test/children/task-02.json",
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, task).
		WithStatusSubresource(task, project).
		Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	usage := pkgdispatch.Usage{
		InputTokens:        77,
		EstimatedCostCents: 3,
	}
	completedAt := time.Now().UTC()

	if err := r.emitTaskMetrics(context.Background(), task, project, usage, completedAt); err != nil {
		t.Fatalf("emitTaskMetrics with missing plan: %v", err)
	}

	// Phase label must be "unknown" on Plan miss — never empty string.
	unknownLabels := []string{"proj-miss", "unknown", "nonexistent-plan", "tide-wave-miss-0"}
	if got := testutil.ToFloat64(tidemetrics.TokensInputTotal.WithLabelValues(unknownLabels...)); got != 77 {
		t.Errorf("TokensInputTotal with phase=unknown: got %v, want 77", got)
	}
}
