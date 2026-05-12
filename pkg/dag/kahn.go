package dag

import (
	"fmt"
	"sort"
)

// NodeID is the unique identifier of a node in the DAG. Generic strings —
// callers project domain identifiers (Task names, artifact names) into this
// type.
type NodeID = string

// Edge expresses "From must complete before To."
type Edge struct {
	From NodeID
	To   NodeID
}

// ComputeWaves returns the layered topological sort of (nodes, edges).
//
// Each returned wave is sorted lexicographically for determinism.
// Returns *CycleError if the graph contains a cycle; the error's
// InvolvedNodes lists every node whose indegree never resolved to zero,
// sorted lexicographically. Returns a plain error (not a CycleError) when an
// edge references a node that was not declared in nodes.
//
// Complexity: O(V + E). Per the spec's "schedule is derived, not cached"
// principle this is intentionally cheap so the reconciler can call it on
// every reconcile.
func ComputeWaves(nodes []NodeID, edges []Edge) ([][]NodeID, error) {
	indegree := make(map[NodeID]int, len(nodes))
	nodeSet := make(map[NodeID]struct{}, len(nodes))
	for _, n := range nodes {
		indegree[n] = 0
		nodeSet[n] = struct{}{}
	}

	succ := make(map[NodeID][]NodeID)
	for _, e := range edges {
		if _, ok := nodeSet[e.From]; !ok {
			return nil, fmt.Errorf("edge references unknown node: %s", e.From)
		}
		if _, ok := nodeSet[e.To]; !ok {
			return nil, fmt.Errorf("edge references unknown node: %s", e.To)
		}
		indegree[e.To]++
		succ[e.From] = append(succ[e.From], e.To)
	}

	var waves [][]NodeID
	remaining := make(map[NodeID]struct{}, len(nodes))
	for _, n := range nodes {
		remaining[n] = struct{}{}
	}

	for len(remaining) > 0 {
		var current []NodeID
		for id := range remaining {
			if indegree[id] == 0 {
				current = append(current, id)
			}
		}
		if len(current) == 0 {
			involved := make([]NodeID, 0, len(remaining))
			for id := range remaining {
				involved = append(involved, id)
			}
			sort.Strings(involved)
			return nil, &CycleError{InvolvedNodes: involved}
		}
		sort.Strings(current)
		waves = append(waves, current)
		for _, id := range current {
			delete(remaining, id)
			for _, s := range succ[id] {
				indegree[s]--
			}
		}
	}
	return waves, nil
}
