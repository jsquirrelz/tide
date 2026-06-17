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

// Plan 25-02 Task 1 — TDD RED tests for the shared coarse-ref fan-out resolver.
// Tests: buildScopeResolver.resolveScope, buildGlobalEdges, edge de-dup.
package controller

import (
	"sort"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// ---------- helpers ----------

func makeTestTask(name, planRef string) tideprojectv1alpha2.Task {
	return tideprojectv1alpha2.Task{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       tideprojectv1alpha2.TaskSpec{PlanRef: planRef},
	}
}

func makeTestPlan(name, phaseRef string) tideprojectv1alpha2.Plan {
	return tideprojectv1alpha2.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       tideprojectv1alpha2.PlanSpec{PhaseRef: phaseRef},
	}
}

func makeTestPhase(name, msRef string) tideprojectv1alpha2.Phase {
	return tideprojectv1alpha2.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: msRef},
	}
}

func makeTestMilestone(name string) tideprojectv1alpha2.Milestone {
	return tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

func sortedStrings(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}

type edgeKey struct{ from, to string }

func edgeSetFrom(edges []dag.Edge) map[edgeKey]struct{} {
	m := make(map[edgeKey]struct{}, len(edges))
	for _, e := range edges {
		m[edgeKey{e.From, e.To}] = struct{}{}
	}
	return m
}

// ---------- buildScopeResolver.resolveScope tests ----------

func TestResolveScope_DirectTaskRef(t *testing.T) {
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-1"),
		makeTestTask("task-b", "plan-1"),
	}
	resolver := buildScopeResolver(tasks, nil, nil, nil)

	got := resolver.resolveScope("task-a")
	want := []string{"task-a"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("resolveScope(task-a) = %v; want %v", got, want)
	}
}

func TestResolveScope_PlanRef_FansOutToMemberTasks(t *testing.T) {
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-alpha"),
		makeTestTask("task-b", "plan-alpha"),
		makeTestTask("task-c", "plan-beta"),
	}
	plans := []tideprojectv1alpha2.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		makeTestPlan("plan-beta", "phase-1"),
	}
	resolver := buildScopeResolver(tasks, plans, nil, nil)

	got := sortedStrings(resolver.resolveScope("plan-alpha"))
	want := []string{"task-a", "task-b"}
	if len(got) != len(want) {
		t.Fatalf("resolveScope(plan-alpha) = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveScope(plan-alpha)[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestResolveScope_PhaseRef_FansOutAcrossPlans(t *testing.T) {
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-1"),
		makeTestTask("task-b", "plan-2"),
		makeTestTask("task-c", "plan-other"),
	}
	plans := []tideprojectv1alpha2.Plan{
		makeTestPlan("plan-1", "phase-x"),
		makeTestPlan("plan-2", "phase-x"),
		makeTestPlan("plan-other", "phase-y"),
	}
	phases := []tideprojectv1alpha2.Phase{
		makeTestPhase("phase-x", "ms-1"),
		makeTestPhase("phase-y", "ms-1"),
	}
	resolver := buildScopeResolver(tasks, plans, phases, nil)

	got := sortedStrings(resolver.resolveScope("phase-x"))
	want := []string{"task-a", "task-b"}
	if len(got) != len(want) {
		t.Fatalf("resolveScope(phase-x) = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveScope(phase-x)[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestResolveScope_MilestoneRef_FansOutTransitively(t *testing.T) {
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-1"),
		makeTestTask("task-b", "plan-2"),
		makeTestTask("task-c", "plan-3"), // different milestone
	}
	plans := []tideprojectv1alpha2.Plan{
		makeTestPlan("plan-1", "phase-1"),
		makeTestPlan("plan-2", "phase-2"),
		makeTestPlan("plan-3", "phase-3"),
	}
	phases := []tideprojectv1alpha2.Phase{
		makeTestPhase("phase-1", "ms-alpha"),
		makeTestPhase("phase-2", "ms-alpha"),
		makeTestPhase("phase-3", "ms-beta"),
	}
	milestones := []tideprojectv1alpha2.Milestone{
		makeTestMilestone("ms-alpha"),
		makeTestMilestone("ms-beta"),
	}
	resolver := buildScopeResolver(tasks, plans, phases, milestones)

	got := sortedStrings(resolver.resolveScope("ms-alpha"))
	want := []string{"task-a", "task-b"}
	if len(got) != len(want) {
		t.Fatalf("resolveScope(ms-alpha) = %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("resolveScope(ms-alpha)[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}

func TestResolveScope_UnresolvedRef_ReturnsEmpty(t *testing.T) {
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-1"),
	}
	resolver := buildScopeResolver(tasks, nil, nil, nil)

	got := resolver.resolveScope("nonexistent-plan")
	if len(got) != 0 {
		t.Errorf("resolveScope(nonexistent) = %v; want empty", got)
	}
}

// ---------- buildGlobalEdges tests ----------

func TestBuildGlobalEdges_DirectTaskDependsOn(t *testing.T) {
	// task-b depends on task-a (direct name ref)
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-1"),
		{
			ObjectMeta: metav1.ObjectMeta{Name: "task-b"},
			Spec: tideprojectv1alpha2.TaskSpec{
				PlanRef:   "plan-1",
				DependsOn: []string{"task-a"},
			},
		},
	}
	plans := []tideprojectv1alpha2.Plan{makeTestPlan("plan-1", "phase-1")}
	resolver := buildScopeResolver(tasks, plans, nil, nil)
	edges := buildGlobalEdges(resolver, tasks, plans, nil, nil)

	es := edgeSetFrom(edges)
	if _, ok := es[edgeKey{"task-a", "task-b"}]; !ok {
		t.Errorf("expected edge task-a → task-b; got edges: %v", edges)
	}
	if len(edges) != 1 {
		t.Errorf("expected exactly 1 edge; got %d: %v", len(edges), edges)
	}
}

func TestBuildGlobalEdges_CoarsePlanDependsOn_FansOut(t *testing.T) {
	// task-c (in plan-beta) has DependsOn=["plan-alpha"]
	// plan-alpha contains task-a and task-b
	// expected edges: task-a → task-c, task-b → task-c
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-alpha"),
		makeTestTask("task-b", "plan-alpha"),
		{
			ObjectMeta: metav1.ObjectMeta{Name: "task-c"},
			Spec: tideprojectv1alpha2.TaskSpec{
				PlanRef:   "plan-beta",
				DependsOn: []string{"plan-alpha"},
			},
		},
	}
	plans := []tideprojectv1alpha2.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		makeTestPlan("plan-beta", "phase-1"),
	}
	resolver := buildScopeResolver(tasks, plans, nil, nil)
	edges := buildGlobalEdges(resolver, tasks, plans, nil, nil)

	es := edgeSetFrom(edges)
	if _, ok := es[edgeKey{"task-a", "task-c"}]; !ok {
		t.Errorf("expected edge task-a → task-c; edges: %v", edges)
	}
	if _, ok := es[edgeKey{"task-b", "task-c"}]; !ok {
		t.Errorf("expected edge task-b → task-c; edges: %v", edges)
	}
}

func TestBuildGlobalEdges_PlanLevelDependsOn_FansOut(t *testing.T) {
	// plan-beta.DependsOn = ["plan-alpha"]: all tasks in plan-beta depend on all tasks in plan-alpha
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-alpha"),
		makeTestTask("task-b", "plan-beta"),
	}
	plans := []tideprojectv1alpha2.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		{
			ObjectMeta: metav1.ObjectMeta{Name: "plan-beta"},
			Spec: tideprojectv1alpha2.PlanSpec{
				PhaseRef:  "phase-1",
				DependsOn: []string{"plan-alpha"},
			},
		},
	}
	resolver := buildScopeResolver(tasks, plans, nil, nil)
	edges := buildGlobalEdges(resolver, tasks, plans, nil, nil)

	es := edgeSetFrom(edges)
	if _, ok := es[edgeKey{"task-a", "task-b"}]; !ok {
		t.Errorf("expected edge task-a → task-b (plan-level dep); edges: %v", edges)
	}
}

func TestBuildGlobalEdges_EdgeDeDup_SameEdgeFromDirectAndPlanRef(t *testing.T) {
	// task-b depends on "task-a" AND on "plan-alpha" (which also contains task-a).
	// Should produce exactly ONE edge task-a → task-b, not two.
	tasks := []tideprojectv1alpha2.Task{
		makeTestTask("task-a", "plan-alpha"),
		{
			ObjectMeta: metav1.ObjectMeta{Name: "task-b"},
			Spec: tideprojectv1alpha2.TaskSpec{
				PlanRef:   "plan-beta",
				DependsOn: []string{"task-a", "plan-alpha"},
			},
		},
	}
	plans := []tideprojectv1alpha2.Plan{
		makeTestPlan("plan-alpha", "phase-1"),
		makeTestPlan("plan-beta", "phase-1"),
	}
	resolver := buildScopeResolver(tasks, plans, nil, nil)
	edges := buildGlobalEdges(resolver, tasks, plans, nil, nil)

	// Count how many times task-a → task-b appears.
	count := 0
	for _, e := range edges {
		if e.From == "task-a" && e.To == "task-b" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 task-a → task-b edge (de-dup); got %d edges total: %v", count, edges)
	}
}
