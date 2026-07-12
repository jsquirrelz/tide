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

// Plan 25-02 Task 2 — TDD RED tests for global TaskReconciler dispatch.
// Tests: listProjectTasks, computeGlobalIndegree, globalDependentsMapper.
// RED until Task 2 implementation is in place.
package controller

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/owner"
)

// makeProjectTask builds a Task with the owner.LabelProject label set.
func makeProjectTask(name, ns, projectName, planRef string, dependsOn []string, phase string) *tideprojectv1alpha3.Task {
	return &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("uid-" + name),
			Labels: map[string]string{
				owner.LabelProject: projectName,
			},
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:   planRef,
			DependsOn: dependsOn,
		},
		Status: tideprojectv1alpha3.TaskStatus{
			Phase: phase,
		},
	}
}

// ---------- listProjectTasks ----------

func TestListProjectTasks_ReturnsAllProjectTasks(t *testing.T) {
	const ns = "default"
	const proj = "test-project"
	taskA := makeProjectTask("task-a", ns, proj, "plan-1", nil, "")
	taskB := makeProjectTask("task-b", ns, proj, "plan-2", nil, "")
	taskOther := makeProjectTask("task-other", ns, "other-project", "plan-1", nil, "")

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(taskA, taskB, taskOther).
		WithStatusSubresource(&tideprojectv1alpha3.Task{}).
		Build()

	r := &TaskReconciler{Client: c, Scheme: s, Deps: TaskReconcilerDeps{Budget: budget.NewStore(), Defaults: budget.Limits{}, SigningKey: []byte("tide-test-signing-key-32-bytes!!")}}

	tasks, err := r.listProjectTasks(context.Background(), taskA, proj)
	if err != nil {
		t.Fatalf("listProjectTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("listProjectTasks returned %d tasks; want 2 (task-a, task-b)", len(tasks))
	}
}

func TestListProjectTasks_EmptyProjectName_ReturnsError(t *testing.T) {
	const ns = "default"
	taskA := makeProjectTask("task-a", ns, "proj", "plan-1", nil, "")

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(taskA).Build()
	r := &TaskReconciler{Client: c, Scheme: s, Deps: TaskReconcilerDeps{Budget: budget.NewStore(), Defaults: budget.Limits{}, SigningKey: []byte("tide-test-signing-key-32-bytes!!")}}

	_, err := r.listProjectTasks(context.Background(), taskA, "")
	if err == nil {
		t.Error("expected error for empty projectName; got nil")
	}
}

// ---------- computeGlobalIndegree ----------

func TestComputeGlobalIndegree_NoDeps_ReturnsZero(t *testing.T) {
	tasks := []tideprojectv1alpha3.Task{
		*makeProjectTask("task-a", "default", "proj", "plan-1", nil, "Succeeded"),
	}
	plans := []tideprojectv1alpha3.Plan{makeTestPlan("plan-1", "phase-1")}

	r := &TaskReconciler{}
	indegree := r.computeGlobalIndegree(context.Background(), tasks[0], tasks, plans, nil, nil)
	if indegree != 0 {
		t.Errorf("computeGlobalIndegree = %d; want 0 for no deps", indegree)
	}
}

func TestComputeGlobalIndegree_DirectDepSucceeded_ReturnsZero(t *testing.T) {
	taskA := makeProjectTask("task-a", "default", "proj", "plan-1", nil, "Succeeded")
	taskB := makeProjectTask("task-b", "default", "proj", "plan-1", []string{"task-a"}, "")
	tasks := []tideprojectv1alpha3.Task{*taskA, *taskB}
	plans := []tideprojectv1alpha3.Plan{makeTestPlan("plan-1", "phase-1")}

	r := &TaskReconciler{}
	indegree := r.computeGlobalIndegree(context.Background(), *taskB, tasks, plans, nil, nil)
	if indegree != 0 {
		t.Errorf("computeGlobalIndegree = %d; want 0 (predecessor Succeeded)", indegree)
	}
}

func TestComputeGlobalIndegree_DirectDepNotSucceeded_ReturnsOne(t *testing.T) {
	taskA := makeProjectTask("task-a", "default", "proj", "plan-1", nil, "Running")
	taskB := makeProjectTask("task-b", "default", "proj", "plan-1", []string{"task-a"}, "")
	tasks := []tideprojectv1alpha3.Task{*taskA, *taskB}
	plans := []tideprojectv1alpha3.Plan{makeTestPlan("plan-1", "phase-1")}

	r := &TaskReconciler{}
	indegree := r.computeGlobalIndegree(context.Background(), *taskB, tasks, plans, nil, nil)
	if indegree != 1 {
		t.Errorf("computeGlobalIndegree = %d; want 1 (predecessor Running)", indegree)
	}
}

func TestComputeGlobalIndegree_CrossPlanDepNotSucceeded_ReturnsOne(t *testing.T) {
	// task-a in plan-alpha; task-b in plan-beta depends on task-a (cross-plan).
	taskA := makeProjectTask("task-a", "default", "proj", "plan-alpha", nil, "Running")
	taskB := makeProjectTask("task-b", "default", "proj", "plan-beta", []string{"task-a"}, "")
	tasks := []tideprojectv1alpha3.Task{*taskA, *taskB}
	plans := []tideprojectv1alpha3.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		makeTestPlan("plan-beta", "phase-1"),
	}

	r := &TaskReconciler{}
	indegree := r.computeGlobalIndegree(context.Background(), *taskB, tasks, plans, nil, nil)
	if indegree != 1 {
		t.Errorf("computeGlobalIndegree cross-plan = %d; want 1 (cross-plan dep Running)", indegree)
	}
}

func TestComputeGlobalIndegree_CoarsePlanRef_AllMembersSucceeded_ReturnsZero(t *testing.T) {
	// task-b has DependsOn=["plan-alpha"]; plan-alpha has task-a (Succeeded).
	taskA := makeProjectTask("task-a", "default", "proj", "plan-alpha", nil, "Succeeded")
	taskB := makeProjectTask("task-b", "default", "proj", "plan-beta", []string{"plan-alpha"}, "")
	tasks := []tideprojectv1alpha3.Task{*taskA, *taskB}
	plans := []tideprojectv1alpha3.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		makeTestPlan("plan-beta", "phase-1"),
	}

	r := &TaskReconciler{}
	indegree := r.computeGlobalIndegree(context.Background(), *taskB, tasks, plans, nil, nil)
	if indegree != 0 {
		t.Errorf("computeGlobalIndegree coarse plan ref (all succeeded) = %d; want 0", indegree)
	}
}

func TestComputeGlobalIndegree_CoarsePlanRef_OneMemberNotSucceeded_ReturnsPositive(t *testing.T) {
	// task-b has DependsOn=["plan-alpha"]; plan-alpha has task-a (Running) and task-c (Succeeded).
	// indegree must be > 0 since task-a is not yet Succeeded.
	taskA := makeProjectTask("task-a", "default", "proj", "plan-alpha", nil, "Running")
	taskC := makeProjectTask("task-c", "default", "proj", "plan-alpha", nil, "Succeeded")
	taskB := makeProjectTask("task-b", "default", "proj", "plan-beta", []string{"plan-alpha"}, "")
	tasks := []tideprojectv1alpha3.Task{*taskA, *taskC, *taskB}
	plans := []tideprojectv1alpha3.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		makeTestPlan("plan-beta", "phase-1"),
	}

	r := &TaskReconciler{}
	indegree := r.computeGlobalIndegree(context.Background(), *taskB, tasks, plans, nil, nil)
	if indegree <= 0 {
		t.Errorf("computeGlobalIndegree coarse plan ref (one not succeeded) = %d; want > 0", indegree)
	}
}

// ---------- globalDependentsMapper ----------

func TestGlobalDependentsMapper_DirectDep_ReenqueuesDependent(t *testing.T) {
	const ns = "default"
	const proj = "test-proj"
	// task-a is the changed task; task-b depends on task-a directly.
	taskA := makeProjectTask("task-a", ns, proj, "plan-1", nil, "Succeeded")
	taskB := makeProjectTask("task-b", ns, proj, "plan-1", []string{"task-a"}, "")

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(taskA, taskB).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	reqs := r.globalDependentsMapper(context.Background(), taskA)
	found := false
	for _, req := range reqs {
		if req.Name == "task-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("globalDependentsMapper did not re-enqueue task-b (direct dep on task-a); got %v", reqs)
	}
}

func TestGlobalDependentsMapper_SelfSkip(t *testing.T) {
	const ns = "default"
	const proj = "test-proj"
	// task-a has itself in DependsOn (pathological), should be skipped.
	taskA := makeProjectTask("task-a", ns, proj, "plan-1", []string{"task-a"}, "")

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(taskA).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	reqs := r.globalDependentsMapper(context.Background(), taskA)
	for _, req := range reqs {
		if req.Namespace == ns && req.Name == "task-a" {
			t.Errorf("globalDependentsMapper included self-enqueue for task-a; should skip self (UID guard)")
		}
	}
}

func TestGlobalDependentsMapper_EmptyProjectLabel_NoDependents_ReturnsEmpty(t *testing.T) {
	const ns = "default"
	// task without owner.LabelProject set and no dependents anywhere.
	taskNoLabel := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "task-no-label", Namespace: ns, UID: types.UID("uid-no-label")},
	}

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(taskNoLabel).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	reqs := r.globalDependentsMapper(context.Background(), taskNoLabel)
	if len(reqs) != 0 {
		t.Errorf("globalDependentsMapper must enqueue nothing for an unlabeled task with no dependents; got %v", reqs)
	}
}

// WR-01: an unlabeled predecessor (label not yet stamped / informer lag) must
// still re-enqueue its DIRECT-name dependents — they would otherwise stall until
// the ~10h periodic resync. Coarse-ref resolution still requires the label, but
// direct-name liveness must not be silently dropped.
func TestGlobalDependentsMapper_EmptyProjectLabel_ReenqueuesDirectDependents(t *testing.T) {
	const ns = "default"
	// Predecessor has no project label; a labeled dependent names it directly.
	taskNoLabel := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "pred-no-label", Namespace: ns, UID: types.UID("uid-pred")},
	}
	dependent := makeProjectTask("dependent", ns, "test-proj", "plan-1", []string{"pred-no-label"}, "")

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(taskNoLabel, dependent).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	reqs := r.globalDependentsMapper(context.Background(), taskNoLabel)
	found := false
	for _, req := range reqs {
		if req.Name == "dependent" {
			found = true
		}
	}
	if !found {
		t.Errorf("globalDependentsMapper WR-01: unlabeled predecessor must re-enqueue direct-name dependent; got %v", reqs)
	}
}

// TestGlobalDependentsMapper_CoarseRefDep_ReenqueuesDependent is the critical
// coarse-ref test: a Task whose DependsOn names the PLAN of the changed task
// (not the task directly) must be re-enqueued when the changed task's status
// changes. This proves the mapper and computeGlobalIndegree resolve edges
// identically (D-01 "never disagree" clause).
func TestGlobalDependentsMapper_CoarseRefDep_ReenqueuesDependent(t *testing.T) {
	const ns = "default"
	const proj = "test-proj"
	// task-a is in plan-alpha; task-b has DependsOn=["plan-alpha"] (coarse ref).
	taskA := makeProjectTask("task-a", ns, proj, "plan-alpha", nil, "Succeeded")
	taskB := makeProjectTask("task-b", ns, proj, "plan-beta", []string{"plan-alpha"}, "")

	// plans needed for the resolver to expand "plan-alpha" → {task-a}.
	planAlpha := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-alpha", Namespace: ns},
		Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
	}
	planBeta := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: "plan-beta", Namespace: ns},
		Spec:       tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
	}

	s := fakeSchemeWithAll(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(taskA, taskB, planAlpha, planBeta).Build()
	r := &TaskReconciler{Client: c, Scheme: s}

	// mapper is called with taskA (the changed task).
	reqs := r.globalDependentsMapper(context.Background(), taskA)
	found := false
	for _, req := range reqs {
		if req.Name == "task-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("globalDependentsMapper COARSE-REF: did not re-enqueue task-b (DependsOn=[plan-alpha]); got %v", reqs)
	}
}
