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

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// TestStatusPhaseOrDepsChangedPredicate verifies the WR-02 predicate firing matrix.
// The predicate should fire (return true) for UpdateEvents that carry a Status.Phase
// change or a Spec.DependsOn change, and return false for no-op resourceVersion-only
// bumps. CreateFunc and DeleteFunc must return true; GenericFunc must return false.
func TestStatusPhaseOrDepsChangedPredicate(t *testing.T) {
	// statusPhaseOrDepsChanged is defined in SetupWithManager; we construct it here
	// using the same logic to validate the firing matrix before the implementation
	// lands.  When the implementation is present the test will call the real predicate.
	//
	// NOTE (RED): this test currently fails because statusPhaseOrDepsChanged does not
	// exist in the package yet. Implementing it in task_controller.go turns this GREEN.

	// Helper: build a Task with the supplied phase + dependsOn.
	makeTask := func(name, phase string, deps []string) *tideprojectv1alpha3.Task {
		return &tideprojectv1alpha3.Task{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				ResourceVersion: "12345",
			},
			Spec: tideprojectv1alpha3.TaskSpec{
				DependsOn: deps,
			},
			Status: tideprojectv1alpha3.TaskStatus{
				Phase: phase,
			},
		}
	}

	// Access the predicate under test via the exported test helper.
	pred := newStatusPhaseOrDepsChangedPredicate()

	t.Run("phase change returns true", func(t *testing.T) {
		old := makeTask("t1", "Pending", nil)
		nw := makeTask("t1", "Running", nil)
		nw.ResourceVersion = "12346"
		got := pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: nw})
		if !got {
			t.Error("expected predicate to return true when Status.Phase changes Pending→Running")
		}
	})

	t.Run("dependsOn change returns true", func(t *testing.T) {
		old := makeTask("t2", "Pending", nil)
		nw := makeTask("t2", "Pending", []string{"other-task"})
		nw.ResourceVersion = "12346"
		got := pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: nw})
		if !got {
			t.Error("expected predicate to return true when Spec.DependsOn changes")
		}
	})

	t.Run("no-op resourceVersion-only update returns false", func(t *testing.T) {
		old := makeTask("t3", "Running", []string{"dep-a"})
		nw := makeTask("t3", "Running", []string{"dep-a"})
		nw.ResourceVersion = "99999" // only ResourceVersion changed
		got := pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: nw})
		if got {
			t.Error("expected predicate to return false for no-op resourceVersion-only update")
		}
	})

	t.Run("type-assert failure returns true (conservative)", func(t *testing.T) {
		// Pass a non-Task object (nil concrete types via ObjectOld/New interfaces).
		got := pred.Update(event.UpdateEvent{ObjectOld: nil, ObjectNew: nil})
		if !got {
			t.Error("expected predicate to return true on type-assert failure (conservative fall-through)")
		}
	})

	t.Run("CreateFunc returns true", func(t *testing.T) {
		task := makeTask("t4", "Pending", nil)
		got := pred.Create(event.CreateEvent{Object: task})
		if !got {
			t.Error("expected CreateFunc to return true")
		}
	})

	t.Run("DeleteFunc returns true", func(t *testing.T) {
		task := makeTask("t5", "Succeeded", nil)
		got := pred.Delete(event.DeleteEvent{Object: task})
		if !got {
			t.Error("expected DeleteFunc to return true")
		}
	})

	t.Run("GenericFunc returns false", func(t *testing.T) {
		task := makeTask("t6", "Running", nil)
		got := pred.Generic(event.GenericEvent{Object: task})
		if got {
			t.Error("expected GenericFunc to return false")
		}
	})
}
