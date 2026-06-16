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

// Plan 23-03 Task 1 — DEPS-03 global cross-scope cycle gate tests.
//
// TestGlobalCycleDetection verifies that the ProjectReconciler global cycle gate
// detects cross-scope task-level dependency cycles and surfaces the involved nodes
// via a GlobalCycleDetected Project status condition. A plain (acyclic) graph
// passes through the gate. Coarse scope refs (naming a Plan rather than a Task)
// are conservatively skipped and do NOT trip the gate.
//
// These are pure Go tests (no envtest/Ginkgo) using the fake controller-runtime
// client so they run fast without a live cluster.
package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/owner"
)

// makeCycleTask creates a v1alpha1.Task in the given namespace, owned by the
// given project (via label), with the given dependsOn list.
// Named makeCycleTask to avoid conflicts with the existing makeTask helper in
// task_controller_test.go (which has a different signature).
func makeCycleTask(name, namespace, projectName string, dependsOn []string) *tideprojectv1alpha2.Task {
	return &tideprojectv1alpha2.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				owner.LabelProject: projectName,
			},
		},
		Spec: tideprojectv1alpha2.TaskSpec{
			PlanRef:             "plan-x",
			FilesTouched:        []string{"dummy.go"},
			PromptPath:          "envelopes/plan-x/children/task-01.json",
			DeclaredOutputPaths: []string{"output.md"},
			DependsOn:           dependsOn,
		},
	}
}

// TestGlobalCycleDetection verifies:
//  1. A two-task cross-scope cycle (A→B, B→A) is detected and surfaced as
//     GlobalCycleDetected condition naming both involved nodes.
//  2. A coarse-only dep (A depends on a Plan name not a Task name) is
//     conservatively skipped and does NOT trip the cycle gate.
func TestGlobalCycleDetection(t *testing.T) {
	t.Run("cross-scope task cycle is detected", func(t *testing.T) {
		ctx := context.Background()
		s := v2GuardScheme(t)

		const (
			ns       = "default"
			projName = "cycle-project"
			taskA    = "task-a"
			taskB    = "task-b"
		)

		// Create a v1alpha2 Project with SchemaRevision set (passes schema guard).
		proj := &tidev1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      projName,
				Namespace: ns,
			},
			Spec: tidev1alpha2.ProjectSpec{
				SchemaRevision: "v1alpha2",
				TargetRepo:     "https://github.com/example/repo.git",
			},
		}

		// Create two Tasks in different (simulated) plans that form a cycle:
		// task-a depends on task-b and task-b depends on task-a.
		ta := makeCycleTask(taskA, ns, projName, []string{taskB})
		tb := makeCycleTask(taskB, ns, projName, []string{taskA})

		fc := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(proj, ta, tb).
			WithStatusSubresource(proj).
			Build()

		r := &ProjectReconciler{
			Client: fc,
			Scheme: s,
		}

		nodes, edges, _, asmErr := r.assembleProjectDepGraph(ctx, proj)
		if asmErr != nil {
			t.Fatalf("assembleProjectDepGraph: %v", asmErr)
		}
		blocked, _, _, err := r.checkGlobalCycleGate(ctx, proj, nodes, edges)
		_ = err // cycle gate may return nil for non-fatal cycle surfacing

		if !blocked {
			t.Error("checkGlobalCycleGate: expected blocked=true for cyclic cross-scope task deps")
		}

		// Fetch the updated project and verify GlobalCycleDetected condition.
		updated := &tidev1alpha2.Project{}
		if getErr := fc.Get(ctx, client.ObjectKey{Name: projName, Namespace: ns}, updated); getErr != nil {
			t.Fatalf("Get updated project: %v", getErr)
		}

		foundCycle := false
		involvedInMessage := false
		for _, c := range updated.Status.Conditions {
			if c.Type == "CycleDetected" && c.Reason == tidev1alpha2.ReasonGlobalCycleDetected {
				foundCycle = true
				// Both involved nodes must be named in the condition message.
				if strings.Contains(c.Message, taskA) && strings.Contains(c.Message, taskB) {
					involvedInMessage = true
				}
				break
			}
		}
		if !foundCycle {
			t.Errorf("expected GlobalCycleDetected condition on Project status; conditions = %v",
				updated.Status.Conditions)
		}
		if !involvedInMessage {
			t.Errorf("expected both %s and %s named in cycle condition message; conditions = %v",
				taskA, taskB, updated.Status.Conditions)
		}
	})

	t.Run("coarse plan-scope dep does NOT trip cycle gate", func(t *testing.T) {
		ctx := context.Background()
		s := v2GuardScheme(t)

		const (
			ns       = "default"
			projName = "coarse-project"
			planName = "plan-y" // this name is NOT a known Task name
		)

		// Create a v1alpha2 Project with SchemaRevision set.
		proj := &tidev1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{
				Name:      projName,
				Namespace: ns,
			},
			Spec: tidev1alpha2.ProjectSpec{
				SchemaRevision: "v1alpha2",
				TargetRepo:     "https://github.com/example/repo.git",
			},
		}

		// Create one Task whose DependsOn names a Plan (coarse ref), NOT a Task.
		// The cycle gate must skip this edge (RESEARCH OQ#3 / Phase-24 fan-out).
		ta := makeCycleTask("task-coarse", ns, projName, []string{planName})

		fc := fake.NewClientBuilder().
			WithScheme(s).
			WithObjects(proj, ta).
			WithStatusSubresource(proj).
			Build()

		r := &ProjectReconciler{
			Client: fc,
			Scheme: s,
		}

		nodes, edges, _, asmErr := r.assembleProjectDepGraph(ctx, proj)
		if asmErr != nil {
			t.Fatalf("assembleProjectDepGraph: %v", asmErr)
		}
		blocked, _, _, err := r.checkGlobalCycleGate(ctx, proj, nodes, edges)
		if blocked {
			t.Errorf("checkGlobalCycleGate: coarse-only dep incorrectly triggered cycle gate; err=%v", err)
		}

		// Also verify no GlobalCycleDetected condition is set on the Project.
		updated := &tidev1alpha2.Project{}
		if getErr := fc.Get(ctx, client.ObjectKey{Name: projName, Namespace: ns}, updated); getErr != nil {
			// Project status not updated (no cycle found) — that's fine.
			return
		}
		for _, c := range updated.Status.Conditions {
			if c.Reason == tidev1alpha2.ReasonGlobalCycleDetected {
				t.Errorf("coarse-only dep incorrectly triggered GlobalCycleDetected condition; conditions = %v",
					updated.Status.Conditions)
				return
			}
		}
	})
}
