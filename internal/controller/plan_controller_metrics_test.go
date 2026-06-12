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

// Phase 16 Plan 07 Task 2: unit tests for materializeWaves WavesDispatchedTotal emission.
// Plain go tests with fake controller-runtime client (no envtest needed) —
// replicates the 16-02 fake-client pattern from task_controller_metrics_test.go.
package controller

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// TestMaterializeWaves_CreateOnce asserts that materializeWaves with 2 layers against
// a fake client containing no Waves creates 2 Waves and increments
// tide_waves_dispatched_total{project,phase,plan} by exactly 2.
// Uses unique label values (proj-mw1) to avoid cross-test counter interference.
func TestMaterializeWaves_CreateOnce(t *testing.T) {
	s := fakeSchemeWithAll(t)
	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plan-mw1",
			Namespace: "default",
			UID:       types.UID("uid-mw1"),
			Labels: map[string]string{
				"tideproject.k8s/project": "proj-mw1",
			},
		},
		Spec: tideprojectv1alpha1.PlanSpec{
			PhaseRef: "phase-mw1",
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(plan).Build()
	r := &PlanReconciler{Client: c, Scheme: s}

	layers := [][]dag.NodeID{
		{"task-a"},
		{"task-b"},
	}

	if err := r.materializeWaves(context.Background(), plan, nil, layers); err != nil {
		t.Fatalf("materializeWaves: %v", err)
	}

	labels := []string{"proj-mw1", "phase-mw1", "plan-mw1"}
	if got := testutil.ToFloat64(tidemetrics.WavesDispatchedTotal.WithLabelValues(labels...)); got != 2 {
		t.Errorf("WavesDispatchedTotal: got %v, want 2 (one per layer)", got)
	}
}

// TestMaterializeWaves_IdempotentReplay asserts that calling materializeWaves a second
// time with the same Plan and layers (Waves now exist in the fake client) leaves the
// counter at 2 — no double count on reconcile replay.
// Uses unique label values (proj-mw2) to avoid cross-test counter interference.
func TestMaterializeWaves_IdempotentReplay(t *testing.T) {
	s := fakeSchemeWithAll(t)
	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plan-mw2",
			Namespace: "default",
			UID:       types.UID("uid-mw2"),
			Labels: map[string]string{
				"tideproject.k8s/project": "proj-mw2",
			},
		},
		Spec: tideprojectv1alpha1.PlanSpec{
			PhaseRef: "phase-mw2",
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(plan).Build()
	r := &PlanReconciler{Client: c, Scheme: s}

	layers := [][]dag.NodeID{
		{"task-x"},
		{"task-y"},
	}

	// First call — should create 2 Waves and count 2.
	if err := r.materializeWaves(context.Background(), plan, nil, layers); err != nil {
		t.Fatalf("materializeWaves (first call): %v", err)
	}
	labels := []string{"proj-mw2", "phase-mw2", "plan-mw2"}
	if got := testutil.ToFloat64(tidemetrics.WavesDispatchedTotal.WithLabelValues(labels...)); got != 2 {
		t.Errorf("WavesDispatchedTotal after first call: got %v, want 2", got)
	}

	// Second call — Waves now exist; counter must stay at 2 (no double count).
	if err := r.materializeWaves(context.Background(), plan, nil, layers); err != nil {
		t.Fatalf("materializeWaves (second call): %v", err)
	}
	if got := testutil.ToFloat64(tidemetrics.WavesDispatchedTotal.WithLabelValues(labels...)); got != 2 {
		t.Errorf("WavesDispatchedTotal after second call: got %v, want 2 (replay must not double-count)", got)
	}
}

// TestMaterializeWaves_UnknownSentinel asserts that a Plan with empty PhaseRef and no
// resolvable Project still increments WavesDispatchedTotal with phase="unknown" and
// project="unknown" — never an empty label value (Metric Label Sentinel, Pitfall 4).
// Uses unique label values (plan-mw3) to avoid cross-test counter interference.
func TestMaterializeWaves_UnknownSentinel(t *testing.T) {
	s := fakeSchemeWithAll(t)
	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "plan-mw3",
			Namespace: "default",
			UID:       types.UID("uid-mw3"),
			// No project label, no PhaseRef — both should fall back to "unknown".
		},
		Spec: tideprojectv1alpha1.PlanSpec{
			PhaseRef: "", // empty
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(plan).Build()
	r := &PlanReconciler{Client: c, Scheme: s}

	layers := [][]dag.NodeID{
		{"task-sentinel"},
	}

	if err := r.materializeWaves(context.Background(), plan, nil, layers); err != nil {
		t.Fatalf("materializeWaves (sentinel): %v", err)
	}

	// Sentinel labels — never empty strings.
	labels := []string{"unknown", "unknown", "plan-mw3"}
	if got := testutil.ToFloat64(tidemetrics.WavesDispatchedTotal.WithLabelValues(labels...)); got != 1 {
		t.Errorf("WavesDispatchedTotal{project=unknown,phase=unknown}: got %v, want 1", got)
	}
}
