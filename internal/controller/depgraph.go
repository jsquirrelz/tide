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

// depgraph.go — shared coarse-ref fan-out resolver for the global Execution DAG.
//
// Plan 25-02 Task 1: extracted from assembleProjectDepGraph (project_controller.go)
// so that both ProjectReconciler wave derivation and TaskReconciler dispatch
// indegree resolve coarse-scope DependsOn entries through the SAME logic,
// satisfying the D-01 "never disagree" clause from 25-CONTEXT.md.
//
// Design rules:
//   - Package controller only; do NOT import pkg/dag into this file — the dag
//     package must remain k8s-free (verify-dag-imports guard).
//   - All resolution is in-memory; nothing is written to CRDs (D-05 /
//     verify-no-aggregates).
//   - An unresolved ref returns empty (conservative — never invents an edge, D-06).
//   - Edges are de-duplicated by the caller via buildGlobalEdges.
package controller

import (
	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// scopeResolver resolves a scope name (Task, Plan, Phase, or Milestone name)
// to the set of member Task names in that scope. Built once from in-memory
// lists and reused for both wave derivation and dispatch readiness.
type scopeResolver struct {
	// taskNames is the set of all known Task names for O(1) lookup.
	taskNames map[string]struct{}
	// tasksByPlan maps plan.Name → []taskName.
	tasksByPlan map[string][]string
	// planToPhase maps plan.Name → phase.Name.
	planToPhase map[string]string
	// phaseToMS maps phase.Name → milestone.Name.
	phaseToMS map[string]string
	// collisions records scope names that matched at more than one Kind level
	// (e.g. a Task and a Plan sharing a name). WR-04: resolveScope unions all
	// matching levels rather than silently dropping the lower-precedence ones;
	// this set lets callers surface the ambiguity (a namespace-wide name
	// collision across Task/Plan/Phase/Milestone) at V(1) for diagnosis.
	collisions map[string]struct{}
}

// buildScopeResolver constructs a scopeResolver from the four list slices.
// The slices are read-only; no allocations beyond the resolver's own maps.
// Pass nil for any list that is not available (e.g., phases=nil when only
// Task/Plan resolution is needed).
func buildScopeResolver(
	tasks []tideprojectv1alpha2.Task,
	plans []tideprojectv1alpha2.Plan,
	phases []tideprojectv1alpha2.Phase,
	ms []tideprojectv1alpha2.Milestone,
) *scopeResolver {
	r := &scopeResolver{
		taskNames:   make(map[string]struct{}, len(tasks)),
		tasksByPlan: make(map[string][]string),
		planToPhase: make(map[string]string),
		phaseToMS:   make(map[string]string),
		collisions:  make(map[string]struct{}),
	}
	for i := range tasks {
		t := &tasks[i]
		r.taskNames[t.Name] = struct{}{}
		r.tasksByPlan[t.Spec.PlanRef] = append(r.tasksByPlan[t.Spec.PlanRef], t.Name)
	}
	for i := range plans {
		p := &plans[i]
		r.planToPhase[p.Name] = p.Spec.PhaseRef
	}
	for i := range phases {
		ph := &phases[i]
		r.phaseToMS[ph.Name] = ph.Spec.MilestoneRef
	}
	// milestones are indexed transitively through phaseToMS; no direct map needed.
	_ = ms
	return r
}

// resolveScope expands scopeName to the set of member Task names across EVERY
// Kind level the name matches (Task, Plan, Phase, Milestone).
//
// WR-04: previously this returned on the FIRST matching level (task → plan →
// phase → milestone), so a name shared across Kinds (K8s permits a Task and a
// Plan with the same name in one namespace) silently dropped the lower-precedence
// scope's members — a fail-open dispatch (a dependent could run before a true
// predecessor). It now unions the members of all matching levels so no true edge
// is ever dropped (conservative: a name collision over-connects rather than
// under-connects), and records the collision in r.collisions so callers can
// surface the ambiguity. Edges are still de-duplicated by buildGlobalEdges.
//
// Returns empty when scopeName matches no level (unresolved — never invents an
// edge, D-06).
func (r *scopeResolver) resolveScope(scopeName string) []string {
	var result []string
	matchedLevels := 0

	// 1. Direct task name.
	if _, isTask := r.taskNames[scopeName]; isTask {
		result = append(result, scopeName)
		matchedLevels++
	}
	// 2. Plan name.
	if tasks, ok := r.tasksByPlan[scopeName]; ok {
		result = append(result, tasks...)
		matchedLevels++
	}
	// 3. Phase name.
	var phaseMatched bool
	for planName, phaseName := range r.planToPhase {
		if phaseName == scopeName {
			result = append(result, r.tasksByPlan[planName]...)
			phaseMatched = true
		}
	}
	if phaseMatched {
		matchedLevels++
	}
	// 4. Milestone name (transitive: phase → plan → tasks).
	var msMatched bool
	for phaseName, msName := range r.phaseToMS {
		if msName == scopeName {
			for planName, ph2 := range r.planToPhase {
				if ph2 == phaseName {
					result = append(result, r.tasksByPlan[planName]...)
				}
			}
			msMatched = true
		}
	}
	if msMatched {
		matchedLevels++
	}

	if matchedLevels > 1 {
		r.collisions[scopeName] = struct{}{}
	}
	return result // empty if unresolved — skip (conservative)
}

// collisionNames returns the scope names that matched at more than one Kind
// level during resolveScope calls (WR-04 observability). Callers log these at
// V(1) so a cross-Kind name collision is diagnosable rather than silent.
func (r *scopeResolver) collisionNames() []string {
	if len(r.collisions) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.collisions))
	for n := range r.collisions {
		names = append(names, n)
	}
	return names
}

// ancestorScopeNames returns all scope identifiers that contain task as a
// member, including the task's own Plan, the Phase of that Plan, and the
// Milestone of that Phase. Used by globalDependentsMapper to identify all
// coarse-ref scope names a dependent's DependsOn might reference.
//
// Returns at most three names: planName, phaseName, milestoneName (any may
// be "" if the resolver has no record for that level).
func (r *scopeResolver) ancestorScopeNames(taskPlanRef string) (planName, phaseName, msName string) {
	planName = taskPlanRef
	phaseName = r.planToPhase[taskPlanRef]
	if phaseName != "" {
		msName = r.phaseToMS[phaseName]
	}
	return planName, phaseName, msName
}

// buildGlobalEdges constructs the full task-level edge set by iterating the
// three DependsOn carriers (Task / Plan / Phase) and resolving each entry
// through the shared resolver. De-duplicates via an in-process edgeSet
// (key = "from\x00to") to prevent double-indegree counting when a task's
// DependsOn mixes a direct name and a coarse ref that resolves to the same
// predecessor.
//
// Milestone.DependsOn is a PLANNING-DAG edge — it governs planning order and
// gate-descent but contributes zero execution edges. Cross-milestone execution
// coupling is expressed only via task-level (or plan/phase-level) DependsOn
// that crosses milestone boundaries. §6d (Milestone fan-out) was removed in
// Phase 26; §6a–6c (task / plan / phase fan-out) are preserved unchanged.
//
// This is a pure extraction of the edge-building logic from
// assembleProjectDepGraph sections 6a–6c. The output is byte-identical to
// the original — the only change is that the resolution now goes through the
// shared scopeResolver rather than the inline tasksForScope closure.
func buildGlobalEdges(
	resolver *scopeResolver,
	tasks []tideprojectv1alpha2.Task,
	plans []tideprojectv1alpha2.Plan,
	phases []tideprojectv1alpha2.Phase,
) []dag.Edge {
	var edges []dag.Edge
	edgeSet := make(map[string]struct{})
	addEdge := func(from, to string) {
		key := from + "\x00" + to
		if _, dup := edgeSet[key]; !dup {
			edgeSet[key] = struct{}{}
			edges = append(edges, dag.Edge{From: from, To: to})
		}
	}

	// 6a. Task-level DependsOn fan-out.
	for i := range tasks {
		t := &tasks[i]
		for _, dep := range t.Spec.DependsOn {
			for _, from := range resolver.resolveScope(dep) {
				addEdge(from, t.Name)
			}
		}
	}

	// 6b. Plan-level DependsOn fan-out: all tasks in THIS plan depend on all
	// tasks in the referenced scope.
	for i := range plans {
		p := &plans[i]
		for _, dep := range p.Spec.DependsOn {
			fromTasks := resolver.resolveScope(dep)
			toTasks := resolver.tasksByPlan[p.Name]
			for _, from := range fromTasks {
				for _, to := range toTasks {
					addEdge(from, to)
				}
			}
		}
	}

	// 6c. Phase-level DependsOn fan-out: all tasks in THIS phase depend on all
	// tasks in the referenced scope.
	for i := range phases {
		ph := &phases[i]
		for _, dep := range ph.Spec.DependsOn {
			fromTasks := resolver.resolveScope(dep)
			// Collect all tasks in this phase (union of tasks in plans in this phase).
			var toTasks []string
			for planName, phaseName := range resolver.planToPhase {
				if phaseName == ph.Name {
					toTasks = append(toTasks, resolver.tasksByPlan[planName]...)
				}
			}
			for _, from := range fromTasks {
				for _, to := range toTasks {
					addEdge(from, to)
				}
			}
		}
	}

	return edges
}
